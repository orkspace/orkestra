package reconciler

import (
	"context"

	orkexternal "github.com/orkspace/orkestra/pkg/external"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"k8s.io/client-go/kubernetes"
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
