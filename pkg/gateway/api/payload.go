package api

import (
	"strings"

	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// EvaluatePayload applies serve.config.response to a CR object and returns the
// shaped result.
//
// Called in two places:
//   - POST /api/v1/apply  — after SSA succeeds; obj is the submitted CR.
//     .status is absent; payload fields that reference status resolve to "".
//   - GET  /api/v1/resources/... — after the CR is fetched from the API
//     server; obj is the full stored CR including .status written by the runtime.
//
// The caller passes obj.Object (the unstructured map). The returned map is the
// value that should be set as "payload" in the response JSON.
//
// The payload is ALWAYS a flat map of only the fields defined in
// serve.config.response.payload — not the full CR.
//
// Exclusions (serve.config.response.exclude) are applied at the resource
// GET/list level, not here. This function only builds the payload map.
func EvaluatePayload(
	obj map[string]interface{},
	crd *orktypes.CRDEntry,
	alias string,
	notes orktypes.NoteRegistry,
) map[string]interface{} {
	cfg := crd.ServeResponseConfigFor(alias)
	if cfg == nil || !cfg.HasPayload() {
		return nil
	}

	resolver := orktmpl.NewResolverFromMap(obj).WithUserNotes(notes)

	// ── Build payload map ─────────────────────────────────────────────────────
	// The payload is ALWAYS a flat map of only the declared fields.
	// It never includes the full CR.
	payload := make(map[string]interface{}, len(cfg.Payload))

	for key, expr := range cfg.Payload {
		if expr == "" {
			payload[key] = ""
			continue
		}
		resolved, err := resolver.Resolve(expr)
		if err != nil {
			payload[key] = ""
			continue
		}
		payload[key] = strings.TrimSpace(resolved)
	}

	return payload
}
