// pkg/autoscaler/autoscaler.go
//
// Operatorbox autoscaler — evaluates conditions on a ticker and applies or
// restores worker/queue/resync overrides.
//
// One Autoscaler is created per operatorbox that declares autoscale: in its
// operatorBox: block. It runs a single goroutine for the lifetime of the
// operatorbox and stops cleanly when the context is cancelled.
//
// The autoscaler has no persistent state. A restart always begins from the
// declared CRD baseline. Overrides applied before a restart are lost — the
// next evaluation tick will re-apply them if conditions are still met.
package autoscaler

import (
	"context"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// AutoscaleTarget is implemented by the operatorbox runtime components that
// the autoscaler controls. Kept minimal — autoscaler only calls these three
// methods, nothing else.
type AutoscaleTarget interface {
	// ResizeWorkers adjusts the worker pool to the given concurrency.
	ResizeWorkers(n int)

	// SetQueueDepthLimit adjusts the maximum queue depth.
	SetQueueDepthLimit(n int)

	// SetResyncInterval adjusts how frequently all CRs are re-enqueued.
	SetResyncInterval(d time.Duration)
}

// Autoscaler evaluates conditions and applies/restores overrides for one operatorbox.
type Autoscaler struct {
	cs         kubernetes.Interface
	crdKind    string
	spec       *orktypes.AutoscaleSpec
	baseline   orktypes.AutoscaleBaseline
	target     AutoscaleTarget
	metrics    *AutoMetrics
	crossDecls []orktypes.CrossCRDDeclaration

	// state — not exported, not persisted
	state orktypes.AutoscaleState
}

// NewAutoscaler constructs an Autoscaler for one operatorbox.
// baseline is captured from the CRD's declared configuration before startup.
// crossDecls is the operatorBox.cross slice — used to resolve source fallback
// for autoscale conditions that reference cross.<crd>.metrics.* without an
// explicit source: block on the condition itself.
func NewAutoscaler(
	cs kubernetes.Interface,
	crdKind string,
	spec *orktypes.AutoscaleSpec,
	baseline orktypes.AutoscaleBaseline,
	target AutoscaleTarget,
	metrics *AutoMetrics,
	crossDecls []orktypes.CrossCRDDeclaration,
) *Autoscaler {
	return &Autoscaler{
		cs:         cs,
		crdKind:    crdKind,
		spec:       spec,
		baseline:   baseline,
		target:     target,
		metrics:    metrics,
		crossDecls: crossDecls,
		state: orktypes.AutoscaleState{
			CronWindowsOpenAt: make(map[string]time.Time),
		},
	}
}

// Run starts the autoscale evaluation loop. Blocks until ctx is cancelled.
// Intended to be called in a dedicated goroutine.
func (a *Autoscaler) Run(ctx context.Context) {
	interval := a.spec.EffectiveInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info().
		Str("crd", a.crdKind).
		Dur("interval", interval).
		Dur("cooldown", a.spec.EffectiveCooldown()).
		Int("baselineWorkers", a.baseline.Workers).
		Msg("autoscaler: started")

	for {
		select {
		case <-ctx.Done():
			// Restore baseline on clean shutdown so that the next startup
			// begins from declared values even if the target persists state.
			if a.state.OverrideActive {
				a.restoreBaseline()
			}
			logger.Info().Str("crd", a.crdKind).Msg("autoscaler: stopped")
			return

		case <-ticker.C:
			a.evaluate(ctx)
		}
	}
}

// evaluate is one evaluation tick. Applies overrides or restores baseline.
func (a *Autoscaler) evaluate(ctx context.Context) {
	met := a.conditionsMet(ctx)

	if met {
		if !a.state.OverrideActive {
			a.applyOverride()
		}
		// Reset cooldown clock — conditions are currently true
		a.state.ConditionsFalseAt = time.Time{}
		return
	}

	// Conditions are false
	if !a.state.OverrideActive {
		return // baseline already active, nothing to do
	}

	// Start or continue the cooldown clock
	if a.state.ConditionsFalseAt.IsZero() {
		a.state.ConditionsFalseAt = time.Now()
	}

	cooldown := a.spec.EffectiveCooldown()
	if time.Since(a.state.ConditionsFalseAt) >= cooldown {
		a.restoreBaseline()
		a.state.ConditionsFalseAt = time.Time{}
	}
}

// conditionsMet builds a data map from live metrics and delegates to EvaluateConditions —
// the same general condition evaluator used by the reconciler for template
// when:/or: conditions. Time-based conditions (time:, dayOfWeek:, cron:) are
// handled inside EvaluateOneCond. Metric conditions are pre-populated into the
// data map so NavigateDotPath resolves them as normal dot-paths.
func (a *Autoscaler) conditionsMet(_ context.Context) bool {
	data := a.buildConditionData()
	return orktypes.EvaluateConditions(data, a.spec.Conditions.When, a.spec.Conditions.Or, nil)
}

// buildConditionData returns the data map passed to EvaluateConditions.
// Own metrics live under "metrics.*". Cross-CRD metrics are resolved (registry
// first, HTTP fallback second) and injected under "cross.<crd>.metrics.<field>".
// Cron windows are ticked and injected under "cron.<expr>.open" so that
// EvaluateOneCond can read window state across evaluation ticks.
func (a *Autoscaler) buildConditionData() map[string]interface{} {
	now := time.Now()
	data := map[string]interface{}{
		"metrics": a.metrics.AsMap(),
	}

	all := append(a.spec.Conditions.Or, a.spec.Conditions.When...)
	for _, cond := range all {
		// Cross-metric resolution
		if orktypes.IsCrossMetricField(cond.Field) {
			src := cond.Source
			if src == nil {
				src = a.crossSourceFor(cond.Field)
			}
			val := ResolveCrossMetric(a.cs, GlobalCrossMetricsRegistry, cond.Field, src)
			if val != "" {
				injectCrossMetricValue(data, cond.Field, val)
			}
		}

		// Cron window state — tick and inject so EvaluateOneCond reads persisted state
		if cond.Cron != "" {
			open := orktypes.TickCronWindow(a.state.CronWindowsOpenAt, cond.Cron, cond.Duration.Duration, a.spec.EffectiveInterval(), now)
			injectCronWindowValue(data, cond.Cron, open)
		}
	}

	return data
}

// injectCronWindowValue injects a cron window open/closed state into the data map.
// Stored under data["_cronWindows"][cronExpr] = "true"/"false".
// EvaluateOneCond reads this key when evaluating cron: conditions so the
// stateful window tracking (CronWindowsOpenAt) is respected across ticks.
func injectCronWindowValue(data map[string]interface{}, cronExpr string, open bool) {
	windows, _ := data["_cronWindows"].(map[string]interface{})
	if windows == nil {
		windows = make(map[string]interface{})
		data["_cronWindows"] = windows
	}
	if open {
		windows[cronExpr] = "true"
	} else {
		windows[cronExpr] = "false"
	}
}

// injectCrossMetricValue writes val into data at the nested path encoded in field.
// "cross.<crd>.metrics.<name>" → data["cross"][crd]["metrics"][name] = val.
func injectCrossMetricValue(data map[string]interface{}, field, val string) {
	parts := strings.SplitN(strings.TrimPrefix(field, "cross."), ".metrics.", 2)
	if len(parts) != 2 {
		return
	}
	crd, metric := parts[0], parts[1]

	cross, _ := data["cross"].(map[string]interface{})
	if cross == nil {
		cross = make(map[string]interface{})
		data["cross"] = cross
	}
	crdMap, _ := cross[crd].(map[string]interface{})
	if crdMap == nil {
		crdMap = make(map[string]interface{})
		cross[crd] = crdMap
	}
	metricsMap, _ := crdMap["metrics"].(map[string]interface{})
	if metricsMap == nil {
		metricsMap = make(map[string]interface{})
		crdMap["metrics"] = metricsMap
	}
	metricsMap[metric] = val
}

// crossSourceFor returns the CrossSource declared in the operatorBox.cross block
// that is suitable for metrics resolution of the given cross field path.
// Only entries with a direct endpoint or type: metrics are considered — entries
// with type: cr, health, events, etc. are for different data and are skipped.
// Used as a fallback when a condition's own source: block is absent.
func (a *Autoscaler) crossSourceFor(field string) *orktypes.CrossSource {
	cf := orktypes.ParseCrossField(field)
	if cf == nil {
		return nil
	}
	for _, decl := range a.crossDecls {
		if decl.Source == nil {
			continue
		}
		// Match field CRD against the alias (as:) if set, otherwise the crd name.
		// Field paths use the alias when one is declared (e.g. cross.paymentSystem.*),
		// but some operators use the raw crd name in their field path (e.g. cross.loader.*).
		// Check both so either style resolves correctly.
		crdMatch := strings.EqualFold(decl.CRD, cf.CRD)
		aliasMatch := decl.As != "" && strings.EqualFold(decl.As, cf.CRD)
		if !crdMatch && !aliasMatch {
			continue
		}
		// Only use sources that can resolve metrics: a raw endpoint (any shape)
		// or an ONCOP host entry typed as metrics.
		if decl.Source.Endpoint != "" || decl.Source.Protocol == orktypes.ONCOPMetrics {
			return decl.Source
		}
	}
	return nil
}

// applyOverride applies the do: block to the target operatorbox.
func (a *Autoscaler) applyOverride() {
	do := a.spec.Do

	log := logger.Info().Str("crd", a.crdKind)

	if do.Workers != nil {
		a.target.ResizeWorkers(*do.Workers)
		log = log.Int("workers", *do.Workers)
		metrics.RecordAutoscaleOverride(a.crdKind, *do.Workers)
	}

	if do.QueueDepth != nil {
		a.target.SetQueueDepthLimit(*do.QueueDepth)
		log = log.Int("queueDepth", *do.QueueDepth)
	}
	if do.Resync != nil {
		a.target.SetResyncInterval(do.Resync.Duration)
		log = log.Dur("resync", do.Resync.Duration)
	}

	a.state.OverrideActive = true
	metrics.SetAutoscaleActive(a.crdKind, true)
	log.Msg("autoscaler: override applied")
}

// restoreBaseline restores the CRD's declared configuration.
func (a *Autoscaler) restoreBaseline() {
	a.target.ResizeWorkers(a.baseline.Workers)
	a.target.SetQueueDepthLimit(a.baseline.MaxDepth)
	// Pass 0 for resync: the informer's built-in resync handles the baseline
	// cadence. 0 idles the autoscaler resync goroutine so it does not add a
	// redundant second trigger on top of the informer's own period.
	a.target.SetResyncInterval(0)

	a.state.OverrideActive = false
	metrics.RecordAutoscaleRestore(a.crdKind, a.baseline.Workers)

	logger.Info().
		Str("crd", a.crdKind).
		Int("workers", a.baseline.Workers).
		Int("queueDepth", a.baseline.MaxDepth).
		Dur("resync", a.baseline.Resync).
		Msg("autoscaler: baseline restored")
}
