// pkg/runtime/kordinator/contract
// Shared contracts and types for Kordinator subpackages.

// This package defines the types used at dependency boundaries between Kordinator
// and its subpackages. It exists to avoid import cycles while keeping those
// dependencies explicit.
package contract

import (
	"github.com/orkspace/orkestra/domain"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/client-go/tools/cache"
)

// RegistryEntry contains the runtime state registered for a CRD.
type RegistryEntry struct {
	// CRD is the CRD declaration associated with this registry entry.
	CRD orktypes.CRDEntry

	// Informer watches the primary resources for this CRD.
	Informer cache.SharedIndexInformer

	// ReconcilerFactory creates a reconciler instance for the CRD.
	ReconcilerFactory func() domain.Reconciler

	// FailureThreshold is the number of consecutive reconciliation failures
	// allowed before the resource is considered unhealthy.
	FailureThreshold int
}

// RuntimeResourceKatalog provides read-only access to registered runtime
// resources by their group-version-kind.
type RuntimeResourceKatalog interface {
	// Get returns the registry entry for the given GVK.
	Get(gvk string) (RegistryEntry, bool)
}
