package types

import (
	"fmt"
	"strings"
)

// ONCOProtocol defines the category of cross-operator data being fetched.
type ONCOProtocol string

const (
	ONCOPMetrics ONCOProtocol = "metrics"
	ONCOPHealth  ONCOProtocol = "health"
	ONCOPInfo    ONCOProtocol = "info"
	ONCOPCR      ONCOProtocol = "cr"
	ONCOPEvents  ONCOProtocol = "events"
)

func MetricsProtocol() ONCOProtocol { return ONCOPMetrics }
func HealthProtocol() ONCOProtocol  { return ONCOPHealth }
func InfoProtocol() ONCOProtocol    { return ONCOPInfo }
func CRProtocol() ONCOProtocol      { return ONCOPCR }
func EventsProtocol() ONCOProtocol  { return ONCOPEvents }

// String returns the string representation of the ONCOProtocol.
// This ensures fmt.Printf, logs, and YAML marshaling behave predictably.
func (t ONCOProtocol) String() string {
	return string(t)
}

// ValidONCOProtocols returns the list of all valid protocol types
func (d CrossCRDDeclaration) ValidONCOProtocols() []string {
	return []string{
		ONCOPCR.String(), ONCOPEvents.String(), ONCOPHealth.String(), ONCOPInfo.String(), ONCOPMetrics.String(),
	}
}

// IsValid repors whether a given protocol is valid ONCOP
func (d CrossCRDDeclaration) IsValid(p string) bool {
	for _, proto := range d.ValidONCOProtocols() {
		if p == proto {
			return true
		}
	}
	return false
}

//
// ────────────────────────────────────────────────────────────────────────────────
//   ONCOP URL BUILDER
// ────────────────────────────────────────────────────────────────────────────────
//

// BuildONCOPURL constructs an Orkestra‑native cross‑operator URL using the
// Orkestra Native Cross‑Operator Protocol (ONCOP).
//
// ONCOP allows cross‑binary CRD observation without requiring callers to
// hard‑code full URLs. When a CrossSource specifies a Host (and no Endpoint),
// the final URL is inferred from:
//
//   - the CRD name extracted from the field path (cross.<crd>.…)
//   - the Source.Type ("info", "metrics", "health", "events")
//   - the Source.Namespace override (optional)
//   - the CR's own namespace/name when Type requires it
//
// URL shapes:
//
//	Type: "cr"    → <host>/katalog/<crd>/cr/<ns>/<name>
//	Type: "info"    → <host>/katalog/<crd>
//	Type: "metrics" → <host>/katalog/<crd>
//	Type: "health"  → <host>/katalog/<crd>/health
//	Type: "events"  → <host>/katalog/<crd>/cr/<ns>/<name>/events
//
// If Source.Endpoint is provided, ONCOP is bypassed entirely and the raw
// endpoint is used as‑is. This enables integration with non‑Orkestra operators
// or arbitrary JSON‑producing services.
//
// BuildONCOPURL centralises this logic so all cross‑CRD resolution paths
// (metrics, health, info, autoscale conditions, status.fields) share the same
// URL construction semantics.
func BuildONCOPURL(decl CrossCRDDeclaration) string {
	src := decl.Source
	crd := decl.CRD
	name := decl.Selector.Name
	ns := decl.Selector.Namespace

	host := strings.TrimSuffix(src.Host, "/")

	switch src.Protocol {
	case ONCOPMetrics, ONCOPInfo, "":
		return fmt.Sprintf("%s/katalog/%s", host, crd)

	case ONCOPHealth:
		return fmt.Sprintf("%s/katalog/%s/health", host, crd)

	case ONCOPEvents:
		if ns == "" {
			return fmt.Sprintf("%s/katalog/%s/cr/%s/events", host, crd, name)
		}
		return fmt.Sprintf("%s/katalog/%s/cr/%s/%s/events", host, crd, ns, name)

	case ONCOPCR:
		if ns == "" {
			return fmt.Sprintf("%s/katalog/%s/cr/%s", host, crd, name)
		}
		return fmt.Sprintf("%s/katalog/%s/cr/%s/%s", host, crd, ns, name)

	default:
		return fmt.Sprintf("%s/katalog/%s", host, crd)
	}
}
