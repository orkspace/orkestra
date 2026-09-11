// pkg/runtime/informer/observe/observer.go
package observe

import (
	"context"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/runtime/informer"
	"github.com/orkspace/orkestra/pkg/runtime/queue"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// Dependencies are the runtime services required by secondary observers.
//
// Supplied at construction time by the runtime constructor when New() is called.
type Dependencies struct {
	Katalog       domain.Katalog
	Kube          *kubeclient.Kubeclient
	Informer      *informer.Factory
	QueueRegistry *queue.QueueRegistry
}

// Observer creates and manages secondary informers for a CRD.
//
// Observers are intentionally subordinate to the primary CRD informer:
//
//	secondary occurrence
//	    ↓
//	resolve primary key
//	    ↓
//	informer.AllowAndEnqueueKey
//	    ↓
//	primary CR queue
//
// The observer never calls a reconciler directly.
type Observer struct {
	deps Dependencies
}

// New creates an Observer.
func New(deps Dependencies) *Observer {
	return &Observer{deps: deps}
}

// Observe starts all secondary observers declared by the CRD.
//
// This currently covers:
//   - operatorBox.watch
//   - operatorBox.events
//
// Observers are started immediately against ctx.
func (o *Observer) Observe(ctx context.Context, crd orktypes.CRDEntry) {
	o.observeWatches(ctx, crd)
	o.observeEvents(ctx, crd)
}

func (o *Observer) queueFor(crd orktypes.CRDEntry) (*queue.Workqueue, bool) {
	if o.deps.QueueRegistry == nil {
		return nil, false
	}

	return o.deps.QueueRegistry.For(crd.GVKString())
}
