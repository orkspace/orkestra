package webhook

import (
	"fmt"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	admissionv1 "k8s.io/api/admissionregistration/v1"
)

var (
	setFieldPath   = utils.SetNestedPath
	convertToType  = orktypes.ConvertToType
	readLocal      = utils.ReadLocal
	scalarToString = orktypes.ScalarToString
	resolveScalar  = orktypes.ResolveScalarField
)

// ── Pointer helpers ───────────────────────────────────────────────────────────

func int32Ptr(i int32) *int32                                                         { return &i }
func int64Ptr(i int64) *int64                                                         { return &i }
func matchPolicyPtr(p admissionv1.MatchPolicyType) *admissionv1.MatchPolicyType       { return &p }
func failurePolicyPtr(p admissionv1.FailurePolicyType) *admissionv1.FailurePolicyType { return &p }
func reinvocationPolicyPtr(p admissionv1.ReinvocationPolicyType) *admissionv1.ReinvocationPolicyType {
	return &p
}
func stringPtr(s string) *string { return &s }

// admissionv1FailurePolicyType converts a string failure policy to the typed form.
// Unrecognised values default to Ignore.
func admissionv1FailurePolicyType(policy string) admissionv1.FailurePolicyType {
	switch strings.ToLower(policy) {
	case "fail":
		return admissionv1.Fail
	default:
		return admissionv1.Ignore
	}
}

// runtimeEndpoint builds the in-cluster URL for this Orkestra instance's own
// runtime — the same service the gateway is deployed alongside, not an
// operator-declared cross: endpoint. Port comes from konfig's ORK_PORT
// (default "8080") — the runtime and gateway both read the same env var
// convention for their own /katalog server, so the gateway's own configured
// value is the runtime's too.
func (ws *WebhookServer) runtimeEndpoint() string {
	svc := ws.katalog.RuntimeServiceName()
	ns := ws.konfig.Cluster().Namespace()
	port := ws.konfig.Health().Port()
	return fmt.Sprintf("http://%s.%s.svc:%s", svc, ns, port)
}
