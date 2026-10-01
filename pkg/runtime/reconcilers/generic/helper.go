package generic

import (
	"context"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/children"
	orkexternal "github.com/orkspace/orkestra/pkg/external"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/runtime/kordinator/prepare"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GVR aliases used by reconciler-internal files (run_delete_ordered.go).
// All authoritative GVR definitions live in pkg/children.
var (
	deploymentGVR         = children.DeploymentGVR
	statefulSetGVR        = children.StatefulSetGVR
	replicaSetGVR         = children.ReplicaSetGVR
	serviceGVR            = children.ServiceGVR
	secretGVR             = children.SecretGVR
	configMapGVR          = children.ConfigMapGVR
	serviceAccountGVR     = children.ServiceAccountGVR
	jobGVR                = children.JobGVR
	cronJobGVR            = children.CronJobGVR
	ingressGVR            = children.IngressGVR
	pvcGVR                = children.PersistentVolumeClaimGVR
	pvGVR                 = children.PersistentVolumeGVR
	hpaGVR                = children.HorizontalPodAutoscalerGVR
	pdbGVR                = children.PodDisruptionBudgetGVR
	namespaceGVR          = children.NamespaceGVR
	podGVR                = children.PodGVR
	roleGVR               = children.RoleGVR
	roleBindingGVR        = children.RoleBindingGVR
	clusterRoleGVR        = children.ClusterRoleGVR
	clusterRoleBindingGVR = children.ClusterRoleBindingGVR
	networkPolicyGVR      = children.NetworkPolicyGVR
	limitRangeGVR         = children.LimitRangeGVR
	resourceQuotaGVR      = children.ResourceQuotaGVR
)

func runExternal(
	ctx context.Context,
	gvk string,
	resolver *orktmpl.Resolver,
	calls []orktypes.ExternalCallSpec,
	cs kubernetes.Interface,
) (*orktmpl.Resolver, error) {
	return orkexternal.Run(ctx, gvk, resolver, calls, cs)
}

// hooksFor returns the ObjectHooks for the given target name.
// If the target has a distinct hook binary (registered in TargetHookFactories),
// those hooks are returned. Otherwise falls back to the CRD-level hooks.
func (r *Reconciler[PTR]) hooksFor(target string) domain.ObjectHooks {
	if target != "" {
		if h, ok := r.targetHooks[target]; ok {
			return h
		}
	}
	return r.hooks
}

// withTargetArgs returns a context whose kube client carries the per-target
// merged hooks.args for this reconcile cycle. When the effective box has no
// args override, the context is returned unchanged.
func (r *Reconciler[PTR]) withTargetArgs(ctx context.Context, box orktypes.OperatorBoxConfig) context.Context {
	var args map[string]interface{}
	if box.Reconcile != nil {
		args = box.Reconcile.HooksArgs()
	}
	if len(args) == 0 {
		return ctx
	}
	return kubeclient.WithKubeclient(ctx, r.kube.WithArgs(kubeclient.Args(args)))
}

// namespaceGuardFunc returns a guard closure pre-bound to this CRD's
// namespace restrictions, or nil when no restrictions are configured.
func (r *Reconciler[PTR]) namespaceGuardFunc() func(ctx context.Context, obj domain.Object, ns string) bool {
	restricted := r.crd.AllRestrictedNamespaces()
	allowed := r.crd.AllAllowedNamespaces()
	if len(restricted) == 0 && len(allowed) == 0 {
		return nil
	}
	kind := r.crd.APITypes.Kind
	return func(ctx context.Context, obj domain.Object, ns string) bool {
		return prepare.CheckNamespace(ctx, obj, ns, restricted, allowed, kind).Allowed
	}
}

// patchStripFinalizers removes all box-declared finalizers from obj and patches the API server.
func (r *Reconciler[PTR]) patchStripFinalizers(ctx context.Context, obj PTR, box orktypes.OperatorBoxConfig) error {
	if len(obj.GetFinalizers()) == 0 {
		return nil
	}
	if !labels.StripFinalizers(obj, box.EffectiveFinalizers()) {
		return nil
	}
	logger.Debug().
		Str("name", obj.GetName()).
		Msgf("removing finalizers → %v", obj.GetFinalizers())
	return r.kube.PatchFinalizers(ctx, obj, obj.GetFinalizers(), metav1.PatchOptions{})
}
