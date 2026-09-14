package domain

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcilerFrom wraps a sigs.k8s.io/controller-runtime/pkg/reconcile.Reconciler
// as a domain.Reconciler so it can be returned from a constructor function without
// any changes to the reconciler body.
//
// ctrl.Result.RequeueAfter is forwarded to domain.Result so migrated operators
// that return precise per-object requeue timing have that honored by Orkestra's queue.
//
// Usage:
//
//	func NewMyReconciler(kube kubeclient.Interface) domain.Reconciler {
//	    return domain.ReconcilerFrom(&MyReconciler{
//	        Client: orkadapter.ToClient(kube),
//	    })
//	}
func ReconcilerFrom(r reconcile.Reconciler) Reconciler {
	return &ctrlReconcilerAdapter{r: r}
}

type ctrlReconcilerAdapter struct {
	r reconcile.Reconciler
}

var _ Reconciler = (*ctrlReconcilerAdapter)(nil)

func (a *ctrlReconcilerAdapter) Reconcile(ctx context.Context, req Request) (Result, error) {
	ns, name, err := cache.SplitMetaNamespaceKey(req.Key)
	if err != nil {
		return Result{}, err
	}
	result, err := a.r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: name},
	})
	return Result{RequeueAfter: result.RequeueAfter}, err
}
