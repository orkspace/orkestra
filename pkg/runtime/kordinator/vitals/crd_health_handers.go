// pkg/runtime/kordinator/vitals/crd_health_handlers.go
package vitals

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/orkspace/orkestra/pkg/health"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/konfig"
	ork_autoscaler "github.com/orkspace/orkestra/pkg/runtime/autoscaler"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator/contract"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"github.com/orkspace/orkestra/pkg/version"
	"k8s.io/client-go/tools/cache"
)

// ─────────────────────────────────────────────────────────────────────────────
// CRD Health Response
// ─────────────────────────────────────────────────────────────────────────────

type CRDHealthResponse struct {
	Name                     string                      `json:"name"`
	State                    string                      `json:"state"` // "not started", "pending", "started", "healthy", "degraded"
	Status                   int                         `json:"status"`
	IsKonductor              bool                        `json:"isKonductor"`
	Healthy                  bool                        `json:"healthy"`
	Started                  bool                        `json:"started"`
	Pending                  bool                        `json:"pending"`
	StartedAt                string                      `json:"startedAt"`
	Uptime                   string                      `json:"uptime"`
	QueueDepth               int                         `json:"queueDepth"`
	ErrorRate                float64                     `json:"errorRate"`
	ConsecutiveFails         int64                       `json:"consecutiveFails"`
	TotalReconciles          int64                       `json:"totalReconciles"`
	ResourceCount            int                         `json:"resourceCount"`
	LastError                string                      `json:"lastError"`
	LastReconcile            string                      `json:"lastReconcile"`
	HasUnhealthyDependencies bool                        `json:"hasUnhealthyDependencies"`
	Dependencies             map[string]DependencyStatus `json:"dependencies,omitempty"`
	Missing                  bool                        `json:"missing,omitempty"`
	Gated                    bool                        `json:"gated,omitempty"`
	GatedReason              string                      `json:"gatedReason,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// CRD Health Handler
// Returns the live health status of a single CRD reconciler.
// This endpoint is used by:
//   - /katalog/<crd>/health
//   - dashboards
//   - readiness/liveness checks
//   - operator self‑diagnostics
//
// The handler exposes:
//   - health state (healthy/degraded)
//   - startup state
//   - uptime
//   - reconcile counters
//   - last error
//   - last reconcile timestamp
//
// ─────────────────────────────────────────────────────────────────────────────
func BuildCRDHealthHandler(
	crd orktypes.CRDEntry,
	kfg *konfig.Konfig,
	inf cache.SharedIndexInformer,
	h *CRDHealth,
	o *RuntimeHealth,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, status := h.StateAndStatus()
		v := resolveCRDDisplayValues(crd, kfg, inf)

		response := CRDHealthResponse{
			Name:                     crd.Name,
			State:                    state,
			Status:                   status,
			IsKonductor:              o.IsKonductor(),
			Healthy:                  h.IsHealthy(),
			Started:                  h.Started(),
			Pending:                  h.Pending(),
			StartedAt:                h.StartedAt(),
			Uptime:                   h.Uptime(),
			QueueDepth:               h.QueueDepth(crd.GVKString()),
			ErrorRate:                h.ErrorRatePercent(),
			ConsecutiveFails:         h.ConsecutiveFails(),
			TotalReconciles:          h.TotalReconciles(),
			ResourceCount:            v.resourceCount,
			LastError:                h.LastError(),
			LastReconcile:            h.LastReconcile(),
			Dependencies:             h.GetDependencyStatuses(),
			HasUnhealthyDependencies: h.HasUnhealthyDependencies(),
			Missing:                  h.IsMissing(),
			Gated:                    h.IsGated(),
			GatedReason:              h.GatedReason(),
		}

		utils.WriteJSON(w, status, response)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CRD Info Response
// ─────────────────────────────────────────────────────────────────────────────
type WorkerStats struct {
	Workers           int32             `json:"workers"`
	WorkersActive     int32             `json:"workersActive"`
	WorkersIdle       int32             `json:"workersIdle"`
	WorkersProcessing int32             `json:"workersProcessing"`
	WorkerDetails     map[string]string `json:"workerDetails,omitempty"`
}

type CRDInfoResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	GVK         string `json:"gvk"`
	GVR         string `json:"gvr"`
	// Target is the identifier callers use against the Gateway API and schema
	// API (serve.target, or the lowercased kind when unset). Empty when serve is
	// not enabled for this CRD.
	Target            string                     `json:"target,omitempty"`
	Aliases           []string                   `json:"aliases,omitempty"`
	Namespaced        bool                       `json:"namespaced"`
	Namespace         string                     `json:"namespace"`
	DependsOn         []string                   `json:"dependsOn,omitempty"`
	IsKonductor       bool                       `json:"isKonductor"`
	Workers           int                        `json:"workers"`
	WorkersActive     int32                      `json:"workersActive"`
	WorkersIdle       int32                      `json:"workersIdle"`
	WorkersProcessing int32                      `json:"workersProcessing"`
	WorkerDetails     map[string]string          `json:"workerDetails,omitempty"`
	WorkersSource     string                     `json:"workersSource"`
	Resync            string                     `json:"resync"`
	ResyncSource      string                     `json:"resyncSource"`
	QueueDepth        int                        `json:"queueDepth"`
	MaxDepth          int                        `json:"maxDepth"`
	MaxDepthSource    string                     `json:"maxDepthSource"`
	ResourceCount     int                        `json:"resourceCount"`
	TotalReconciles   int64                      `json:"totalReconciles"`
	OperatorBox       OperatorBoxInfo            `json:"operatorBox"`
	Healthy           bool                       `json:"healthy"`
	Started           bool                       `json:"started"`
	Pending           bool                       `json:"pending"`
	ErrorRate         float64                    `json:"errorRate"`
	Providers         []ProviderInfoResponse     `json:"providers,omitempty"`
	RBAC              RBACInfo                   `json:"rbac,omitempty"`
	AutoscalerEnabled bool                       `json:"autoscalerEnabled,omitempty"`
	AutoscalerWorkers *ork_autoscaler.WorkerInfo `json:"autoscalerWorkers,omitempty"`
	Rollback          *RollbackStats             `json:"rollback,omitempty"`
	// Metrics is the live AutoMetrics map for this operatorbox.
	// Populated only when autoscale: is declared. Used by cross-binary autoscale
	// conditions via the source.endpoint HTTP fallback — the remote autoscaler
	// calls this endpoint and reads "metrics.*" fields from the response.
	Metrics     map[string]interface{} `json:"metrics,omitempty"`
	Gated       bool                   `json:"gated,omitempty"`
	GatedReason string                 `json:"gatedReason,omitempty"`
}

type OperatorBoxInfo struct {
	Type        string                 `json:"type"`
	Finalizers  FinalizersInfo         `json:"finalizers"`
	Hooks       HooksInfo              `json:"hooks"`
	Constructor ConstructorInfo        `json:"constructor"`
	Templates   map[string]interface{} `json:"templates,omitempty"`
}

type FinalizersInfo struct {
	Source string   `json:"source"`
	Values []string `json:"values"`
}

type HooksInfo struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source,omitempty"`
	Location   string `json:"location,omitempty"`
	Function   string `json:"function,omitempty"`
}

type ConstructorInfo struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source,omitempty"`
	Location   string `json:"location,omitempty"`
	Function   string `json:"function,omitempty"`
}

// ProviderInfoResponse exposes per-provider metadata and error rate for one CRD.
// No auth, URLs, or credentials are exposed — metadata only.
type ProviderInfoResponse struct {
	Name      string   `json:"name"`
	Kinds     []string `json:"kinds"`     // declared resource kinds (static, from Katalog)
	Total     int64    `json:"total"`     // reconcile calls since startup
	Errors    int64    `json:"errors"`    // failed reconcile calls since startup
	ErrorRate float64  `json:"errorRate"` // errors / total, 0 when no calls yet
}

// ─────────────────────────────────────────────────────────────────────────────
// CRD Info Handler
// Returns static + dynamic metadata about a CRD:
//   - GVK/GVR
//   - mode (dynamic/typed)
//   - workers, resync, queue depth (with source: default or configured)
//   - reconciler configuration (hooks, finalizers, constructor)
//   - resource count from informer
//   - health summary
//
// This endpoint powers:
//   - /katalog/<crd>
//   - dashboards
//   - operator introspection
//
// ─────────────────────────────────────────────────────────────────────────────
func BuildCRDInfoHandler(
	crd orktypes.CRDEntry,
	kfg *konfig.Konfig,
	inf cache.SharedIndexInformer,
	h *CRDHealth,
	o *RuntimeHealth,
	provStats *health.ProviderStats,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := resolveCRDDisplayValues(crd, kfg, inf)

		// Generate RBAC info for this CRD
		rbacInfo := generateRBACInfo(crd, v)

		// Always expose live metrics so any CRD can be observed via cross.*.metrics.*
		// by a sibling in a different binary — the observed CRD does not need to
		// declare autoscale: itself. AutoMetrics is initialised for every reconciler.
		autoMetrics := h.GetAutoMetrics()

		// When autoscaler is enabled, use WorkerInfo as the authoritative source
		// for all worker and queue fields — the legacy health counters only track
		// the initial configured pool and go negative when extra workers spawn.
		wi := h.GetWorkerInfo()
		workers := v.workers
		workersActive := h.GetActiveWorkers()
		workersIdle := h.GetIdleWorkers()
		workersProcessing := h.GetProcessingWorkers()
		workersSource := v.workersSource
		queueDepth := h.QueueDepth(crd.GVKString())
		maxDepth := v.maxDepth
		maxDepthSource := v.maxDepthSource
		resync := v.resync
		resyncSource := v.resyncSource

		autoscalerSource := "autoscaler"
		if wi != nil {
			// workers — wi is always authoritative: legacy counters go negative when
			// autoscale spawns extra goroutines beyond the initial configured pool.
			workers = wi.Configured
			workersActive = int32(wi.Effective)
			workersIdle = int32(wi.Idle)
			workersProcessing = int32(wi.InFlight)
			queueDepth = int(wi.QueueDepth)

			// queue limit and resync — only override when autoscaler is actively
			// applying an override. Baseline values come from resolveCRDDisplayValues
			// which includes the konfig default fallback (e.g. 100 queue depth).
			if wi.OverrideActive {
				maxDepth = int(wi.QueueDepthEffective)
				resync = wi.ResyncEffective

				maxDepthSource = autoscalerSource
				resyncSource = autoscalerSource
				workersSource = autoscalerSource
			}
		}

		response := CRDInfoResponse{
			Name:              crd.Name,
			Description:       crd.Description,
			Mode:              crd.Mode.String(),
			GVK:               crd.GVKString(),
			GVR:               crd.GroupVersionResource.String(),
			Target:            crd.ServeTargetOrEmpty(),
			Aliases:           crd.AliasNames(),
			Namespaced:        crd.IsNamespaced(),
			Namespace:         crd.Namespace,
			DependsOn:         crd.DependsOn.Names(),
			IsKonductor:       o.IsKonductor(),
			Workers:           workers,
			WorkersActive:     workersActive,
			WorkersIdle:       workersIdle,
			WorkersProcessing: workersProcessing,
			WorkerDetails:     h.GetWorkerStates(),
			WorkersSource:     workersSource,
			Resync:            resync,
			ResyncSource:      resyncSource,
			QueueDepth:        queueDepth,
			MaxDepth:          maxDepth,
			MaxDepthSource:    maxDepthSource,
			ResourceCount:     v.resourceCount,
			TotalReconciles:   h.TotalReconciles(),
			OperatorBox:       operatorBoxInfoStruct(crd),
			Healthy:           h.IsHealthy(),
			Started:           h.Started(),
			Pending:           h.Pending(),
			ErrorRate:         h.ErrorRatePercent(),
			RBAC:              rbacInfo,
			AutoscalerEnabled: crd.AutoscaleEnabled(),
			AutoscalerWorkers: wi,
			Metrics:           autoMetrics,
			Gated:             h.IsGated(),
			GatedReason:       h.GatedReason(),
		}

		if crd.HasRollbackRules() {
			stats := h.RollbackStats()
			response.Rollback = &stats
		}

		// Provider stats
		if crd.HasProviders() {
			// Build a lookup of runtime stats by provider name.
			statsByProvider := make(map[string]health.ProviderStatEntry)
			if provStats != nil {
				for _, e := range provStats.GetSnapshot() {
					statsByProvider[e.Provider] = e
				}
			}

			box := crd.OperatorBox
			providers := make([]ProviderInfoResponse, 0, len(box.ProviderBlocks))
			for _, block := range box.ProviderBlocks {
				kinds := make([]string, 0, len(block.Declarations))
				seen := make(map[string]struct{})
				for _, decl := range block.Declarations {
					if _, ok := seen[decl.Kind]; !ok {
						seen[decl.Kind] = struct{}{}
						kinds = append(kinds, decl.Kind)
					}
				}
				e := statsByProvider[block.Name]
				providers = append(providers, ProviderInfoResponse{
					Name:      block.Name,
					Kinds:     kinds,
					Total:     e.Total,
					Errors:    e.Errors,
					ErrorRate: e.ErrorRate,
				})
			}
			response.Providers = providers
		}

		utils.WriteJSON(w, http.StatusOK, response)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Katalog Response
// ─────────────────────────────────────────────────────────────────────────────

// KatalogNamespaceSummary groups CRDs that share the same katalog namespace.
// Namespaces are declared in katalog.metadata.namespace — "default" when not set.
type KatalogNamespaceSummary struct {
	CRDs         []string     `json:"crds"`
	StatusCounts StatusCounts `json:"statusCounts"`
	Healthy      bool         `json:"healthy"`
	Description  string       `json:"description,omitempty"`
	Version      string       `json:"version,omitempty"`
	Workers      int          `json:"workers"`
	Resources    int          `json:"resources"`
}

type KatalogResponse struct {
	CRDs               []CRDSummaryResponse   `json:"crds"`
	Total              int                    `json:"total"`
	TotalEnabled       int                    `json:"totalEnabled"`
	OrkReady           bool                   `json:"OrkReady"`
	IsKonductor        bool                   `json:"isKonductor"`
	DeletionProtection bool                   `json:"deletionProtection"`
	Healthy            bool                   `json:"healthy"`
	Status             int                    `json:"status"`
	DegradedReason     string                 `json:"degradedReason,omitempty"`
	StatusCounts       StatusCounts           `json:"statusCounts"`
	Name               string                 `json:"name,omitempty"`
	Version            string                 `json:"version,omitempty"`
	CreatedBy          string                 `json:"createdBy,omitempty"`
	Author             string                 `json:"author,omitempty"`
	Description        string                 `json:"description,omitempty"`
	License            string                 `json:"license,omitempty"`
	RuntimeVersion     string                 `json:"runtimeVersion,omitempty"`
	ClusterName        string                 `json:"clusterName,omitempty"`
	Projects           map[string]interface{} `json:"projects,omitempty"`
	// Namespaces groups CRDs by katalog namespace. Always present — at minimum
	// contains "default" when no katalog declares an explicit namespace.
	Namespaces map[string]KatalogNamespaceSummary `json:"namespaces,omitempty"`
	// GatewayEndpoint is the HTTP base URL of the companion gateway process.
	// Set via ORK_GATEWAY_ENDPOINT on the runtime. The control center reads
	// this field and fetches gateway:/katalog to merge per-CRD webhook stats.
	// Empty when no gateway is paired with this runtime.
	GatewayEndpoint string `json:"gatewayEndpoint,omitempty"`
}

type CRDSummaryResponse struct {
	Name                     string             `json:"name"`
	Description              string             `json:"description"`
	Mode                     string             `json:"mode"`
	GVK                      string             `json:"gvk"`
	GVR                      string             `json:"gvr"`
	Namespaced               bool               `json:"namespaced"`
	Namespace                string             `json:"namespace"`
	CrossAccess              bool               `json:"crossAccess"`
	DependsOn                []string           `json:"dependsOn,omitempty"`
	HasUnhealthyDependencies bool               `json:"hasUnhealthyDependencies"`
	Workers                  int                `json:"workers"`
	WorkersSource            string             `json:"workersSource"`
	WorkersActive            int32              `json:"workersActive"`
	Resync                   string             `json:"resync"`
	ResyncSource             string             `json:"resyncSource"`
	QueueDepth               int                `json:"queueDepth"`
	MaxDepth                 int                `json:"maxDepth"`
	MaxDepthSource           string             `json:"maxDepthSource"`
	ResourceCount            int                `json:"resourceCount"`
	OperatorBox              OperatorBoxSummary `json:"operatorBox"`
	Healthy                  bool               `json:"healthy"`
	State                    string             `json:"state"`
	Started                  bool               `json:"started"`
	Pending                  bool               `json:"pending"`
	StartedAt                string             `json:"startedAt"`
	Uptime                   string             `json:"uptime"`
	ErrorRate                float64            `json:"errorRate"`
	Endpoints                EndpointInfo       `json:"endpoints"`
	RBACCount                int                `json:"rbacCount,omitempty"`
	DeletionProtection       bool               `json:"deletionProtection"`
	ProviderCount            int                `json:"providerCount,omitempty"`
	KatalogNamespace         string             `json:"katalogNamespace,omitempty"`
	ServeEnabled             bool               `json:"serveEnabled,omitempty"`
	RequireServeName         bool               `json:"requireServeName,omitempty"`
	// Target is the identifier callers use against the Gateway API and schema
	// API (serve.target, or the lowercased kind when unset). Empty when serve is
	// not enabled for this CRD.
	Target      string   `json:"target,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Gated       bool     `json:"gated,omitempty"`
	GatedReason string   `json:"gatedReason,omitempty"`
}

type OperatorBoxSummary struct {
	Type           string `json:"type"`
	HasTemplates   bool   `json:"hasTemplates,omitempty"`
	HasHooks       bool   `json:"hasHooks,omitempty"`
	HasConstructor bool   `json:"hasConstructor,omitempty"`
}

type EndpointInfo struct {
	Health        string `json:"health"`
	Info          string `json:"info"`
	HealthEnabled bool   `json:"healthEnabled"`
	InfoEnabled   bool   `json:"infoEnabled"`
}

type StatusCounts struct {
	Healthy  int `json:"healthy"`
	Degraded int `json:"degraded"`
	Started  int `json:"started"`
	Pending  int `json:"pending"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Katalog Handler
// Returns a full list of CRDs in the running operator, including:
//   - metadata (GVK, mode, namespace, dependencies)
//   - resolved operational values (workers, resync, queue depth)
//   - reconciler configuration summary
//   - health summary (uptime, error rate, startedAt)
//   - per‑CRD endpoints (/health, /info)
//
// This is the top‑level endpoint for dashboards and operator UIs.
// ─────────────────────────────────────────────────────────────────────────────
func BuildKatalogHandler(
	kat *katalog.Katalog,
	kfg *konfig.Konfig,
	reg contract.RuntimeResourceKatalog,
	healthMap map[string]*CRDHealth,
	o *RuntimeHealth,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		crds := make([]CRDSummaryResponse, 0)
		statusCounts := StatusCounts{}
		namespaces := make(map[string]KatalogNamespaceSummary)
		deletionProtectedCRDs := kat.DeletionProtectedCRDNames()

		for _, crd := range kat.Enabled() {
			gvk := crd.GVKString()
			h := healthMap[gvk]

			entry, ok := reg.Get(gvk)
			var inf cache.SharedIndexInformer
			if ok {
				inf = entry.Informer
			}

			v := resolveCRDDisplayValues(crd, kfg, inf)

			isStarted := h.Started()
			isPending := h.Pending()
			isHealthy := h.IsHealthy()

			var state string

			switch {
			case !isStarted && !isPending:
				state = "not started"
				statusCounts.Pending++
			case isPending:
				state = "pending"
				statusCounts.Pending++
			case isStarted && !isHealthy:
				state = "degraded"
				statusCounts.Degraded++
			case isHealthy && h.IsGated():
				state = "gated"
				statusCounts.Healthy++
			case isHealthy:
				state = "healthy"
				statusCounts.Healthy++
			default:
				state = "pending"
				statusCounts.Pending++
			}

			box := crd.OperatorBox
			crds = append(crds, CRDSummaryResponse{
				Name:                     crd.Name,
				State:                    state,
				HasUnhealthyDependencies: h.HasUnhealthyDependencies(),
				Description:              crd.Description,
				Mode:                     crd.Mode.String(),
				GVK:                      gvk,
				GVR:                      crd.GroupVersionResource.String(),
				Namespaced:               crd.IsNamespaced(),
				Namespace:                crd.Namespace,
				DependsOn:                crd.DependsOn.Names(),
				Workers:                  v.workers,
				WorkersSource:            v.workersSource,
				WorkersActive:            h.GetActiveWorkers(),
				Resync:                   v.resync,
				ResyncSource:             v.resyncSource,
				QueueDepth:               h.QueueDepth(gvk),
				MaxDepth:                 v.maxDepth,
				MaxDepthSource:           v.maxDepthSource,
				RBACCount:                generateRBACInfo(crd, v).TotalRules,
				ResourceCount:            v.resourceCount,
				DeletionProtection:       isCRDProtected(deletionProtectedCRDs, crd.APITypes.Plural, crd.APITypes.Group),
				ProviderCount:            len(box.ProviderBlocks),
				OperatorBox: OperatorBoxSummary{
					Type:           "generic",
					HasTemplates:   box.OnCreate != nil,
					HasHooks:       crd.HasHooks(),
					HasConstructor: crd.HasConstructor(),
				},
				Healthy:          isHealthy,
				Started:          isStarted,
				Pending:          isPending,
				StartedAt:        h.StartedAt(),
				Uptime:           h.Uptime(),
				ErrorRate:        h.ErrorRatePercent(),
				Gated:            h.IsGated(),
				GatedReason:      h.GatedReason(),
				CrossAccess:      crd.CrossAccessEnabled(),
				KatalogNamespace: crd.KatalogNamespace,
				ServeEnabled:     crd.ServeEnabled(),
				RequireServeName: crd.RequireServeName(),
				Target:           crd.ServeTargetOrEmpty(),
				Aliases:          crd.AliasNames(),
				Endpoints: EndpointInfo{
					Health:        "/katalog/" + strings.ToLower(crd.Name) + "/health",
					Info:          "/katalog/" + strings.ToLower(crd.Name),
					HealthEnabled: crd.Endpoints.IsHealthEnabled(),
					InfoEnabled:   crd.Endpoints.IsInfoEnabled(),
				},
			})

			// Build namespace grouping for the Control Center.
			ns := crd.KatalogNamespace
			if ns == "" {
				ns = "default"
			}
			nsSummary := namespaces[ns]
			nsSummary.CRDs = append(nsSummary.CRDs, crd.Name)
			switch {
			case isHealthy:
				nsSummary.StatusCounts.Healthy++
			case isStarted && !isHealthy:
				nsSummary.StatusCounts.Degraded++
			default:
				nsSummary.StatusCounts.Pending++
			}
			nsSummary.Healthy = nsSummary.StatusCounts.Degraded == 0
			nsSummary.Workers += v.workers
			nsSummary.Resources += v.resourceCount
			if nsSummary.Description == "" {
				nsSummary.Description = crd.KatalogDescription
			}
			if nsSummary.Version == "" {
				nsSummary.Version = crd.KatalogVersion
			}
			namespaces[ns] = nsSummary
		}

		healthy := statusCounts.Degraded == 0
		status := http.StatusOK
		degradedReason := ""

		if !healthy {
			status = http.StatusServiceUnavailable
			var parts []string
			if statusCounts.Degraded > 0 {
				parts = append(parts, fmt.Sprintf("%d degraded", statusCounts.Degraded))
			}
			if statusCounts.Pending > 0 {
				parts = append(parts, fmt.Sprintf("%d pending", statusCounts.Pending))
			}
			if statusCounts.Started > 0 {
				parts = append(parts, fmt.Sprintf("%d started", statusCounts.Started))
			}
			degradedReason = strings.Join(parts, ", ")
		}

		deletionProtection := false
		if kat.IsDeletionProtectionEnabled() && kat.DeletionProtectionGVRs() != nil {
			deletionProtection = true
		}

		utils.WriteJSON(w, status, KatalogResponse{
			CRDs:               crds,
			Total:              len(kat.All()),
			TotalEnabled:       len(kat.Enabled()),
			OrkReady:           o.IsOrkReady(),
			IsKonductor:        o.IsKonductor(),
			DeletionProtection: deletionProtection,
			Healthy:            status == http.StatusOK, // workaround. TODO: standard from crd_health
			Status:             status,
			DegradedReason:     degradedReason,
			StatusCounts:       statusCounts,
			Name:               kat.Meta().Name,
			Version:            kat.Meta().Version,
			Author:             kat.Meta().Author,
			CreatedBy:          kat.Meta().CreatedBy,
			License:            kat.Meta().License,
			Description:        kat.Meta().Description,
			Projects:           kat.Projects(),
			RuntimeVersion:     version.Short(),
			ClusterName:        kat.ClusterName(),
			Namespaces:         namespaces,
			GatewayEndpoint:    kat.GatewayEndpoint(),
		})
	}
}

// isCRDProtected returns true when the CRD identified by (plural, group) is
// present in the protected names set built from the Katalog at startup.
// Returns false when the set is nil (deletion protection not enabled) or when
// the CRD is not managed by this operator.
func isCRDProtected(protected map[string]struct{}, plural, group string) bool {
	if protected == nil {
		return false
	}
	_, ok := protected[plural+"."+group]
	return ok
}

// Helper function to convert to struct-based reconciler info
func operatorBoxInfoStruct(crd orktypes.CRDEntry) OperatorBoxInfo {
	box := crd.OperatorBox

	reconcilerType := "generic"
	if !crd.DefaultReconcile() {
		reconcilerType = "custom"
	}

	finalizersInfo := FinalizersInfo{
		Source: "default",
		Values: []string{},
	}
	if len(box.Finalizers) > 0 {
		finalizersInfo.Source = "configured"
		finalizersInfo.Values = box.Finalizers
	}

	hooksInfo := HooksInfo{Configured: false}
	if box.Reconciler != nil && box.Reconciler.Hooks != nil {
		hooksInfo = HooksInfo{
			Configured: true,
			Source:     "yaml",
			Location:   box.Reconciler.Hooks.Location,
			Function:   box.Reconciler.Hooks.Function,
		}
	} else if box.HookFactory != nil {
		hooksInfo = HooksInfo{
			Configured: true,
			Source:     "go",
		}
	}

	constructorInfo := ConstructorInfo{Configured: false}
	if box.Reconciler != nil && box.Reconciler.ConstructorDecl != nil {
		constructorInfo = ConstructorInfo{
			Configured: true,
			Source:     "yaml",
			Location:   box.Reconciler.ConstructorDecl.Location,
			Function:   box.Reconciler.ConstructorDecl.Function,
		}
	} else if box.Constructor != nil {
		constructorInfo = ConstructorInfo{
			Configured: true,
			Source:     "go",
		}
	}

	result := OperatorBoxInfo{
		Type:        reconcilerType,
		Finalizers:  finalizersInfo,
		Hooks:       hooksInfo,
		Constructor: constructorInfo,
	}

	if box.OnCreate != nil || box.OnReconcile != nil || box.OnDelete != nil {
		result.Templates = make(map[string]interface{})
		if box.OnCreate != nil {
			onCreate := templateSummary(box.OnCreate)
			if hasAutoReconcile(box.OnCreate) {
				result.Templates["onReconcile"] = map[string]interface{}{
					"source": "auto",
					"from":   "onCreate[reconcile:true]",
				}
			}
			result.Templates["onCreate"] = onCreate
		}
		if box.OnReconcile != nil {
			result.Templates["onReconcile"] = templateSummary(box.OnReconcile)
		}
		if box.OnDelete != nil {
			result.Templates["onDelete"] = templateSummary(box.OnDelete)
		}
	}

	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// templateSummary
// Produces a compact summary of declarative templates for a CRD.
// Only counts are returned — not full templates — to keep API responses small.
// ─────────────────────────────────────────────────────────────────────────────

func templateSummary(t *orktypes.HookTemplates) map[string]interface{} {
	if t == nil {
		return map[string]interface{}{}
	}

	summary := map[string]interface{}{}

	if len(t.Deployments) > 0 {
		summary["deployments"] = len(t.Deployments)
	}
	if len(t.StatefulSets) > 0 {
		summary["statefulSets"] = len(t.StatefulSets)
	}
	if len(t.DaemonSets) > 0 {
		summary["daemonSets"] = len(t.DaemonSets)
	}
	if len(t.ReplicaSets) > 0 {
		summary["replicaSets"] = len(t.ReplicaSets)
	}
	if len(t.Services) > 0 {
		summary["services"] = len(t.Services)
	}
	if len(t.Pods) > 0 {
		summary["pods"] = len(t.Pods)
	}
	if len(t.Jobs) > 0 {
		summary["jobs"] = len(t.Jobs)
	}
	if len(t.CronJobs) > 0 {
		summary["cronJobs"] = len(t.CronJobs)
	}
	if len(t.ConfigMaps) > 0 {
		summary["configMaps"] = len(t.ConfigMaps)
	}
	if len(t.ServiceAccounts) > 0 {
		summary["serviceAccounts"] = len(t.ServiceAccounts)
	}
	if len(t.Secrets) > 0 {
		summary["secrets"] = len(t.Secrets)
	}
	if len(t.PersistentVolumes) > 0 {
		summary["persistentVolumes"] = len(t.PersistentVolumes)
	}
	if len(t.PersistentVolumeClaims) > 0 {
		summary["persistentVolumeClaims"] = len(t.PersistentVolumeClaims)
	}
	if len(t.Roles) > 0 {
		summary["roles"] = len(t.Roles)
	}
	if len(t.RoleBindings) > 0 {
		summary["roleBindings"] = len(t.RoleBindings)
	}
	if len(t.ClusterRoles) > 0 {
		summary["clusterRoles"] = len(t.ClusterRoles)
	}
	if len(t.ClusterRoleBindings) > 0 {
		summary["clusterRoleBindings"] = len(t.ClusterRoleBindings)
	}
	if len(t.Ingresses) > 0 {
		summary["ingresses"] = len(t.Ingresses)
	}
	if len(t.NetworkPolicies) > 0 {
		summary["networkPolicies"] = len(t.NetworkPolicies)
	}
	if len(t.PodDisruptionBudgets) > 0 {
		summary["podDisruptionBudgets"] = len(t.PodDisruptionBudgets)
	}
	if len(t.LimitRanges) > 0 {
		summary["limitRanges"] = len(t.LimitRanges)
	}
	if len(t.ResourceQuotas) > 0 {
		summary["resourceQuotas"] = len(t.ResourceQuotas)
	}
	if len(t.PriorityClasses) > 0 {
		summary["priorityClasses"] = len(t.PriorityClasses)
	}
	if len(t.CustomResource) > 0 {
		summary["customResource"] = len(t.CustomResource)
	}
	if len(t.HorizontalPodAutoscalers) > 0 {
		summary["horizontalPodAutoscalers"] = len(t.HorizontalPodAutoscalers)
	}

	return summary
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveCRDDisplayValues
// Resolves operational values for a CRD, including:
//   - workers (configured or default)
//   - resync period (configured or default)
//   - queue depth (configured or default)
//   - resource count from informer
//
// This ensures the API always shows the *effective* values the operator uses.
// ─────────────────────────────────────────────────────────────────────────────

type crdDisplayValues struct {
	resync         string
	resyncSource   string
	workers        int
	workersSource  string
	maxDepth       int
	maxDepthSource string
	resourceCount  int
}

func resolveCRDDisplayValues(
	crd orktypes.CRDEntry,
	kfg *konfig.Konfig,
	inf cache.SharedIndexInformer,
) crdDisplayValues {
	box := crd.OperatorBox

	// Queue depth
	maxDepth := box.Reconciler.Queue.MaxDepth
	maxDepthSource := "configured"
	if maxDepth == 0 {
		maxDepth = kfg.Katalog().DefaultQueueDepth()
		maxDepthSource = "default"
	}

	// Resync
	resync := box.Reconciler.Resync.String()
	resyncSource := "configured"
	if box.Reconciler.Resync.Duration == 0 {
		resyncSource = "default"
		resync = kfg.Katalog().DefaultResync().String()
	}

	// Workers
	workers := box.Reconciler.Workers
	workersSource := "configured"
	if box.Reconciler.Workers == 0 {
		workers = kfg.Katalog().DefaultWorkers()
		workersSource = "default"
	}

	// Resource count from informer
	resourceCount := 0
	if inf != nil {
		resourceCount = len(inf.GetStore().List())
	}

	return crdDisplayValues{
		resync:         resync,
		resyncSource:   resyncSource,
		workers:        workers,
		workersSource:  workersSource,
		maxDepth:       maxDepth,
		maxDepthSource: maxDepthSource,
		resourceCount:  resourceCount,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hasAutoReconcile
// Returns true if any declarative template has `reconcile: true`,
// meaning onCreate implicitly triggers onReconcile.
// ─────────────────────────────────────────────────────────────────────────────

func hasAutoReconcile(t *orktypes.HookTemplates) bool {
	if t == nil {
		return false
	}
	for _, d := range t.Deployments {
		if d.Reconcile {
			return true
		}
	}
	for _, s := range t.Services {
		if s.Reconcile {
			return true
		}
	}
	return false
}
