package constructor

import (
	"context"
	"fmt"

	apiv1 "github.com/orkspace/orkestra-args-hooks-targets/api/v1alpha1"
	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	orkdeploy "github.com/orkspace/orkestra/pkg/resources/deployments"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
)

// BlockchainAppWithTargetsReconciler is the per-target constructor reconciler
// for the BlockchainAppWithTargets CRD. It reads featureEnabled from args
// (declared in katalog serve.target.<name>.operatorBox.reconciler.constructor.args)
// rather than calling a live feature-flag endpoint.
type BlockchainAppWithTargetsReconciler struct {
	kube kubeclient.Interface
}

// NewBlockchainAppWithTargetsReconciler is the constructor registered in
// serve.target.v2-ctor.operatorBox.reconciler.constructor.
func NewBlockchainAppWithTargetsReconciler(kube kubeclient.Interface) domain.Reconciler {
	return &BlockchainAppWithTargetsReconciler{kube: kube}
}

func (r *BlockchainAppWithTargetsReconciler) Reconcile(ctx context.Context, req domain.Request) (domain.Result, error) {
	key := req.Key
	raw, exists, err := r.kube.GetInformer().GetIndexer().GetByKey(key)
	if err != nil {
		return domain.Result{}, fmt.Errorf("cache lookup %q: %w", key, err)
	}
	if !exists {
		return domain.Result{}, nil
	}

	app, ok := raw.(*apiv1.BlockchainAppWithTargets)
	if !ok {
		return domain.Result{}, fmt.Errorf("unexpected type %T for key %q", raw, key)
	}
	app = app.DeepCopyObject().(*apiv1.BlockchainAppWithTargets)

	if app.DeletionTimestamp != nil {
		return domain.Result{}, nil
	}

	resolver, err := orktmpl.NewResolver(ctx, app)
	if err != nil {
		return domain.Result{}, fmt.Errorf("building resolver: %w", err)
	}
	kube := r.kube.ScopedFor(resolver.TemplateEvaluator())

	featureEnabled := kube.Args().String("featureEnabled")

	annotation := "false"
	if featureEnabled == "true" {
		annotation = "true"
	}

	replicas := int32(app.Spec.Replicas)
	if replicas == 0 {
		replicas = 1
	}

	spec := orkdeploy.ResolvedDeploymentSpec{
		Name:      app.Name,
		Namespace: app.Namespace,
		Image:     app.Spec.Image,
		Replicas:  replicas,
		Annotations: map[string]string{
			"feature.demo/v2-enabled": annotation,
			"orkestra.io/target":      "v2-ctor",
		},
	}
	if err := orkdeploy.Apply(ctx, kube, app, spec); err != nil {
		return domain.Result{}, fmt.Errorf("blockchainappwithtargets deployment: %w", err)
	}

	return domain.Result{}, kube.PatchStatus(ctx, app, map[string]any{
		"phase":          "Running",
		"network":        app.Spec.Network,
		"featureEnabled": annotation,
	})
}
