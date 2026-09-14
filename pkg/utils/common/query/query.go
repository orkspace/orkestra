package query

import (
	"context"
	"net/http"
	"time"

	"github.com/orkspace/orkestra/domain"
)

const timeout = 3 * time.Second

// runtimeQuery implements domain.RuntimeQuery at admission time by querying
// the runtime's own /katalog/{crd} endpoints.
//
// The runtime holds every CRD instance in its informer cache — queries here
// are fast in-memory lookups on the runtime side, not live List() calls
// against the API server. That's deliberate: reconcile time is the
// authoritative enforcement point. Here a momentarily stale cache is
// acceptable; worst case, a duplicate slips past admission and is caught
// on the next reconcile, which is the same guarantee every katalog had
// before this existed.
type runtimeQuery struct {
	ctx      context.Context
	client   *http.Client
	endpoint string // e.g. http://orkestra-runtime.orkestra-system.svc:8080
	crdName  string // katalog key (spec.crds.<key>), matches {crd} in /katalog/{crd}/cr
}

// NewRuntimeQuery builds a query client targeting the given runtime endpoint
// for one CRD. No I/O happens at construction — HTTP calls fire only inside
// the individual methods when their data is actually needed.
func NewRuntimeQuery(ctx context.Context, endpoint, crdName string) domain.RuntimeQuery {
	return &runtimeQuery{
		ctx:      ctx,
		client:   &http.Client{Timeout: timeout},
		endpoint: endpoint,
		crdName:  crdName,
	}
}
