package katalog

import (
	"context"
	"time"

	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EvaluateRequeue computes the requeue duration for a declarative operatorBox
// after a successful reconcile. Returns 0 when no requeue is needed.
//
// Evaluation order:
//  1. If requeue: is absent or effectively empty, return 0.
//  2. Evaluate when/or conditions — if conditions are declared and fail, return 0.
//  3. Render the after: template against the live CR; parse as a Go duration.
//  4. Return the parsed duration (0 on parse failure — fail-open for requeue).
func (k *Katalog) EvaluateRequeue(ctx context.Context, crdName string, obj *unstructured.Unstructured) time.Duration {
	if obj == nil {
		return 0
	}
	entry, ok := k.CRDEntry(crdName)
	if !ok {
		return 0
	}
	target := orktarget.ResolveTargetFromAnnotations(obj.GetAnnotations())
	box := entry.EffectiveOperatorBox(target)
	rc := box.Reconciler
	if rc.IsRequeueEmpty() {
		return 0
	}
	rq := rc.Requeue

	resolver, err := orktmpl.NewResolver(ctx, obj)
	if err != nil {
		return 0
	}
	if !k.Profiles.Empty() {
		resolver = resolver.WithProfiles(k.Profiles)
	}
	if !k.Notes.Empty() {
		resolver = resolver.WithUserNotes(k.Notes)
	}

	eval := resolver.TemplateEvaluator()
	if !orktypes.EvaluateConditions(resolver.Data(), rq.When, rq.Or, eval) {
		return 0
	}

	if rq.After == "" {
		return 0
	}
	rendered, ok := resolver.RenderString(rq.After)
	if !ok || rendered == "" || rendered == "0s" {
		return 0
	}
	d, err := parseTimeDuration(rendered)
	if err != nil {
		return 0
	}
	return d
}
