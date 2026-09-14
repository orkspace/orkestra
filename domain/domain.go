package domain

import "context"

type Komponent interface {

	// Start() starts the komponents
	Start(context.Context) error

	// Shutdown() shuts down the komponent gracefully
	Shutdown(context.Context)

	// Name() returns the name of the komponent
	Name() string

	// Started() is set when manager starts a komponent
	Started() bool
}

// Workqueue is the per-CRD queue contract used by pkg/types, pkg/runtime/queue,
// pkg/runtime/informer, and pkg/katalog. Declared here to break the import cycle
// that would form if those packages imported each other directly.
type Workqueue interface {
	// Type, IsRatelimitedType, IsDelayedType — reserved for future queue-type-aware
	// behaviour routing (e.g. delayed queues with per-type drop semantics).
	Type() string
	IsRatelimitedType(s string) bool
	IsDelayedType(s string) bool
	IsUnlimited() bool
	HasBehaviour() bool
	HasOnLimit() bool
	HasOnThreshold() bool
	HasBehaviourCondition() bool
	HasOnLimitConditions() bool
	HasOnThresholdConditions() bool
	MaxQueueDepth() int
	ThresholdValue() int
	ThresholdReached(depth int) bool
}

type Reconciler interface {
	// Reconcile handles the actual business logic for a resource.
	// Return a non-zero Result.RequeueAfter to schedule a precise re-enqueue
	// after a successful reconcile. Return an error to trigger retryBackoff.
	Reconcile(ctx context.Context, req Request) (Result, error)
}
