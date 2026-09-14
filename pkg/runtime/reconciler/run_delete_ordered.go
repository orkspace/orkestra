// pkg/reconciler/run_delete_ordered.go
//
// Sequential deletion with completion gates.
//
// When onDelete.ordered: true the reconciler deletes resources in stages
// rather than relying on garbage collection. Each stage is a HookTemplates
// block. After submitting all deletes in a stage the reconciler polls the
// API server until every resource is confirmed gone, then advances to the
// next stage.
//
// Deletion order within a stage is undefined; order is enforced between stages.
package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/runners"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const defaultOrderedDeleteTimeout = 5 * time.Minute
const orderedDeletePollInterval = 3 * time.Second

// orderedDeleteEntry identifies a single resource to wait on after deletion.
type orderedDeleteEntry struct {
	gvr       schema.GroupVersionResource
	namespace string
	name      string
}

// runOrderedDelete deletes resource groups sequentially.
// For each group: submit all deletes, then poll until every resource is gone.
func (r *GenericReconciler[PTR]) runOrderedDelete(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	t *orktypes.HookTemplates,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
) error {
	log := logger.FromContext(ctx)
	log.Info().Str("name", obj.GetName()).Msg("ordered delete: starting sequential cleanup")

	timeout := defaultOrderedDeleteTimeout
	if t.Timeout != nil && t.Timeout.Duration > 0 {
		timeout = t.Timeout.Duration
	}

	// Build the list of stages. If Groups is declared use those; otherwise
	// treat the flat resource fields as a single implicit group.
	stages := t.Groups
	if len(stages) == 0 {
		stages = []orktypes.HookTemplates{*t}
	}

	for i, stage := range stages {
		stageLog := log.With().Int("stage", i+1).Int("total_stages", len(stages)).Logger()
		stageLog.Info().Msg("ordered delete: processing stage")

		s := stage // capture for closure
		pending, err := r.submitGroupDeletion(ctx, kube, resolver, obj, &s, guard)
		if err != nil {
			return fmt.Errorf("ordered delete stage %d: submit: %w", i+1, err)
		}

		if len(pending) == 0 {
			stageLog.Debug().Msg("ordered delete: stage has no resources, advancing")
			continue
		}

		stageLog.Info().Int("waiting_for", len(pending)).Msg("ordered delete: waiting for resources to be gone")
		if err := waitForDeletion(ctx, kube, pending, timeout); err != nil {
			return fmt.Errorf("ordered delete stage %d: wait: %w", i+1, err)
		}
		stageLog.Info().Msg("ordered delete: stage complete")
	}

	return nil
}

// submitGroupDeletion issues Delete calls for all resources in a HookTemplates
// block and returns the list of resources to poll for.
func (r *GenericReconciler[PTR]) submitGroupDeletion(
	ctx context.Context,
	kube kubeclient.Interface,
	resolver *orktmpl.Resolver,
	obj domain.Object,
	t *orktypes.HookTemplates,
	guard func(ctx context.Context, obj domain.Object, ns string) bool,
) ([]orderedDeleteEntry, error) {
	ns := obj.GetNamespace()
	dc := kube.DynamicClient()

	propagation := metav1.DeletePropagationForeground
	delOpts := metav1.DeleteOptions{PropagationPolicy: &propagation}

	var pending []orderedDeleteEntry

	expanded := r.expandAllForDelete(resolver, t)
	for _, rd := range expanded {
		targetNS := ns
		if !rd.namespaced {
			targetNS = ""
		}
		if targetNS != "" && guard != nil && !guard(ctx, obj, targetNS) {
			continue
		}
		for _, item := range rd.names {
			itemNS := targetNS
			if item.namespace != "" {
				itemNS = item.namespace
			}
			var err error
			if itemNS != "" {
				// Namespaced
				err = dc.Resource(rd.gvr).Namespace(itemNS).Delete(ctx, item.name, delOpts)
			} else {
				// Cluster-scoped
				err = dc.Resource(rd.gvr).Delete(ctx, item.name, delOpts)
			}
			if err != nil && !runners.IsNotFoundErr(err) {
				return nil, fmt.Errorf("delete %s/%s: %w", rd.gvr.Resource, item.name, err)
			}
			pending = append(pending, orderedDeleteEntry{
				gvr:       rd.gvr,
				namespace: itemNS,
				name:      item.name,
			})
		}
	}
	return pending, nil
}

// namedResource holds the resolved name and optional namespace override.
type namedResource struct {
	name      string
	namespace string
}

// expandedResourceDef pairs a GVR with the list of resource names derived
// from the template block (after forEach expansion).
type expandedResourceDef struct {
	gvr        schema.GroupVersionResource
	namespaced bool
	names      []namedResource
}

// expandAllForDelete resolves template source names for every resource type
// in the HookTemplates block. Only types with entries are included.
func (r *GenericReconciler[PTR]) expandAllForDelete(
	resolver *orktmpl.Resolver,
	t *orktypes.HookTemplates,
) []expandedResourceDef {
	var out []expandedResourceDef

	out = append(out, resolveNames(resolver, deploymentGVR, true, t.Deployments, func(s orktypes.DeploymentTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, statefulSetGVR, true, t.StatefulSets, func(s orktypes.StatefulSetTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, serviceGVR, true, t.Services, func(s orktypes.ServiceTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, secretGVR, true, t.Secrets, func(s orktypes.SecretTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, configMapGVR, true, t.ConfigMaps, func(s orktypes.ConfigMapTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, serviceAccountGVR, true, t.ServiceAccounts, func(s orktypes.ServiceAccountTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, jobGVR, true, t.Jobs, func(s orktypes.JobTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, cronJobGVR, true, t.CronJobs, func(s orktypes.CronJobTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, ingressGVR, true, t.Ingresses, func(s orktypes.IngressTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, pvcGVR, true, t.PersistentVolumeClaims, func(s orktypes.PVCTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, pvGVR, false, t.PersistentVolumes, func(s orktypes.PVTemplateSource) (string, string) { return s.Name, "" })...)
	out = append(out, resolveNames(resolver, hpaGVR, true, t.HorizontalPodAutoscalers, func(s orktypes.HPATemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, pdbGVR, true, t.PodDisruptionBudgets, func(s orktypes.PDBTemplateSource) (string, string) { return s.Name, s.Namespace })...)
	out = append(out, resolveNames(resolver, namespaceGVR, false, t.Namespaces, func(s orktypes.NamespaceTemplateSource) (string, string) { return s.Name, "" })...)

	return out
}

// resolveNames is a generic helper that resolves template source names via
// the resolver and returns an expandedResourceDef when any names are found.
func resolveNames[S any](
	resolver *orktmpl.Resolver,
	gvr schema.GroupVersionResource,
	namespaced bool,
	sources []S,
	nameNS func(S) (string, string),
) []expandedResourceDef {
	if len(sources) == 0 {
		return nil
	}
	var items []namedResource
	for _, src := range sources {
		rawName, rawNS := nameNS(src)
		name, _ := resolver.Resolve(rawName)
		ns, _ := resolver.Resolve(rawNS)
		if name == "" {
			continue
		}
		items = append(items, namedResource{name: name, namespace: ns})
	}
	if len(items) == 0 {
		return nil
	}
	return []expandedResourceDef{{gvr: gvr, namespaced: namespaced, names: items}}
}

// waitForDeletion polls the API server until all resources in pending are gone
// or the timeout elapses. Uses Get (not informer) so the answer is authoritative.
func waitForDeletion(ctx context.Context, kube kubeclient.Interface, pending []orderedDeleteEntry, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	dc := kube.DynamicClient()

	for time.Now().Before(deadline) {
		var remaining []orderedDeleteEntry
		for _, e := range pending {
			var err error
			if e.namespace != "" {
				_, err = dc.Resource(e.gvr).Namespace(e.namespace).Get(ctx, e.name, metav1.GetOptions{})
			} else {
				_, err = dc.Resource(e.gvr).Get(ctx, e.name, metav1.GetOptions{})
			}
			if err == nil {
				remaining = append(remaining, e) // still exists
			} else if !runners.IsNotFoundErr(err) {
				return fmt.Errorf("polling %s/%s: %w", e.gvr.Resource, e.name, err)
			}
		}
		if len(remaining) == 0 {
			return nil
		}
		pending = remaining

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(orderedDeletePollInterval):
		}
	}

	names := make([]string, 0, len(pending))
	for _, e := range pending {
		names = append(names, e.gvr.Resource+"/"+e.name)
	}
	return fmt.Errorf("timed out after %s waiting for deletion of: %v", timeout, names)
}
