// pkg/types/cross.go
//
// Cross-CRD observation declarations.
//
// The cross: block under a CRD's reconciler allows one CR to observe
// another CRD's CR instances without making API server calls — data is
// read from the target CRD's informer cache directly.
//
// YAML:
//
//	spec:
//	  crds:
//	    application:
//	      cross:
//	        - crd: database             # target CRD name (lowercase, matches spec.crds key)
//	          selector:
//	            name: "{{ .metadata.name }}"       # find by name
//	            namespace: "{{ .metadata.namespace }}"
//	          as: database              # available as .cross.database.*
//
//	    database:
//	      operatorBox: ...               # database CRD — already has an informer
//
// After ReadCross runs, the Application reconciler has access to:
//
//	{{ .cross.database.found }}                → "true" if CR was found
//	{{ .cross.database.status.phase }}         → Database CR's status.phase
//	{{ .cross.database.status.endpoint }}      → Database CR's status.endpoint
//	{{ .cross.database.spec.storageGb }}       → Database CR's spec field
//
// This is zero API calls for same-binary CRDs — data comes from the
// informer cache that is already maintained for the target CRD.
// For cross-binary or cross-cluster observation, set source.endpoint.
//
// Use cases:
//   - Wait for a Database CR to be Ready before creating the Application Deployment
//   - Copy a Database endpoint into the Application status
//   - Gate resource creation on another CRD's phase
//   - Service mesh without a service mesh: Application watches Database watches Network
package types

// ── Cross declaration type ────────────────────────────────────────────────────
// The cross: YAML block should use explicit fields rather than overloading
// the crd: string with "key=value" syntax.
//
// YAML:
//
//   cross:
//     - crd: managed-database          # name-based
//       selector:
//         name: "{{ .metadata.name }}-db"
//       as: db
//
//     - labelSelector:                  # label-based — keys the registry by CRD-entry labelSelector
//         tier: platform
//       selector:
//         name: "{{ .metadata.name }}-platform"
//       as: platform
//
// Step 1 finds the informer: IsCRDBased uses crd name, IsLabelBased uses labelSelector to key
// the registry. Step 2 finds the CR: matchLabels filters instances, then labelSelector fallback,
// then name for precision.

// CrossCRDDeclaration declares one cross-CRD observation.
// Declared in the operatorBox config under cross: for a CRD.
type CrossCRDDeclaration struct {
	// CRD is the target CRD name (lowercase, matches the map key in spec.crds).
	//   crd: database
	CRD string `yaml:"crd" json:"crd"`

	// LabelSelector keys the katalog registry by CRD-entry labelSelector for label-based informer lookup.
	LabelSelector Labels `yaml:"labelSelector,omitempty" json:"labelSelector,omitempty"`

	// Selector identifies which CR instance to observe.
	Selector CrossSelector `yaml:"selector" json:"selector"`

	// As is the key under .cross.* where the result is accessible.
	//   as: database → .cross.database.status.phase
	// Default: same as Crd.
	As string `yaml:"as,omitempty" json:"as,omitempty"`

	// Strategy controls what happens when multiple CRs match the selector.
	//   first (default) — use the first match
	//   all             — put all matches in .cross.<as>[] (array)
	// NOT YET IMPLEMENTED
	Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty"`

	// Source is the fallback for cross-binary or cross-cluster observation.
	// When absent, the informer cache is used (zero API calls).
	// When set, the endpoint is called when the informer is unavailable.
	Source *CrossSource `yaml:"source,omitempty" json:"source,omitempty"`
}

func (d *CrossCRDDeclaration) Empty() bool {
	return d == nil
}

// HasSource reports whether this cross declaration has source: block
func (d *CrossCRDDeclaration) HasSource() bool {
	if d.Empty() {
		return false
	}
	return d.Source != nil
}

// HasSelector reports whether this cross declaration has selector: block
func (d *CrossCRDDeclaration) HasSelector() bool {
	if d.Empty() {
		return false
	}

	sel := d.Selector
	if !sel.MatchLabels.Empty() {
		return true
	}
	return sel.IsNameBased()
}

// IsLabelBased reports whether this cross declaration uses labels
// for cross referencing instead of the CRD.
func (d *CrossCRDDeclaration) IsLabelBased() bool {
	if d.Empty() {
		return false
	}
	return d.LabelSelector != nil
}

// IsCRDBased reports whether this cross declaration uses CRD
// for cross referencing instead of the labels.
func (d *CrossCRDDeclaration) IsCRDBased() bool {
	if d.Empty() {
		return false
	}
	return d.CRD != ""
}

// HasCRDAndLabelDecl reports whether this cross declaration uses has both CRD and labels.
// An error enforced by ork validate
func (d *CrossCRDDeclaration) HasCRDAndLabelDecl() bool {
	if d.Empty() {
		return false
	}
	return d.IsCRDBased() && d.IsLabelBased()
}

// CrossSelector identifies a CR in the target CRD.
// The selector block on a cross: declaration.
// Exactly one of Name or LabelSelector should be set per entry.
type CrossSelector struct {
	// Name is the CR name to look up. Template expressions supported.
	//   name: "{{ .metadata.name }}"    → same name as the current CR
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Namespace is the CR namespace. Template expressions supported.
	// Default: same namespace as the current CR.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// matchLabels selects CRs by label — for 1:N or N:1 relationships.
	//   matchLabels:
	// 		tenant: {{ .spec.tenant }}
	// 		env: {{ .spec.environment }}
	// When set, Name and Namespace are ignored.
	MatchLabels Labels `yaml:"matchLabels,omitempty" json:"matchLabels,omitempty"`
}

// IsNameBased is true when matchLabels is not defined but name/namespace
func (sel CrossSelector) IsNameBased() bool {
	if !sel.MatchLabels.Empty() {
		return false
	}
	return sel.Name != ""
}

// CrossSource declares how to fetch cross-binary/cluster data for a CRD.
// If Endpoint is provided, it is used as-is (raw HTTP fetch).
// If Host is provided, Orkestra constructs the URL based on Type.
//
// Supported Type values:
//   - "info"    → /katalog/<crd>/cr/<ns>/<name>
//   - "metrics" → /katalog/<crd>
//   - "health"  → /katalog/<crd>/health
//   - "events"  → /katalog/<crd>/cr/<ns>/<name>/events
//
// The endpoint must return the same JSON shape as the informer cache path —
// i.e., the Orkestra CR detail endpoint format.
// Namespace is optional; defaults to the CR's namespace when omitted.
type CrossSource struct {
	// Endpoint is a fully-qualified URL. If set, Orkestra uses it directly
	// and ignores Host/Protocol/Namespace. Template expressions supported.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`

	// Host is the base URL of a remote Orkestra runtime, e.g.:
	//   http://orkestra-runtime.loader-system:8080
	// Combined with Type to build the final URL.
	Host string `yaml:"host,omitempty" json:"host,omitempty"`

	// Protocol selects which Orkestra-native endpoint to call.
	// One of: "info", "metrics", "health", "events".
	// Default: "info".
	Protocol ONCOProtocol `yaml:"protocol,omitempty" json:"protocol,omitempty"`

	// Namespace overrides the CR namespace when building info/events URLs.
	// Optional — defaults to the CR's own namespace.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Auth handles both ENV-based token and secret reads for cross: authentication
	// 	auth:
	// 	  secretRef:
	// 		name: cross-secret
	// 		namespace: cross-namespace
	// 		key: password
	Auth *Auth `yaml:"auth,omitempty" json:"auth,omitempty"`

	// CacheFor controls how long to cache the result before calling again.
	// Default: 30s — prevents hammering the endpoint on every evaluation.
	CacheFor string `yaml:"cacheFor,omitempty" json:"cacheFor,omitempty"`
}

// Auth is the authentication envelop for handling both ENV-based token and secret reads for cross: authentication
type Auth struct {
	// Token is a bearer token for the endpoint. $ENV_VAR syntax supported.
	Token string `yaml:"token,omitempty" json:"token,omitempty"`

	// SecretRef locates a Kubernetes Secret whose data key holds a bearer token
	// Mutually exclusive with Token. ork validate enforces this
	SecretRef *APISecretRef `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`
}

// HasEndpoint reports whether this cross read has endpoint configured
// for non-orkestra surfaces (operators, APIs)
func (s *CrossSource) HasEndpoint() bool {
	if s == nil {
		return false
	}
	return s.Host != ""
}

// HasHost reports whether this cross read has host configured for ONCOP reads
func (s *CrossSource) HasHost() bool {
	if s == nil {
		return false
	}
	return s.Host != ""
}

// HasAuth reports whether this cross read has authentication configured
func (s *CrossSource) HasAuth() bool {
	if s == nil {
		return false
	}
	return s.Auth != nil
}

// Token returns the cross token as configured
func (s *CrossSource) Token() string {
	if !s.HasAuth() {
		return ""
	}
	return s.Auth.Token
}

// HasToken reports whether token was configured for this CRD
func (s *CrossSource) HasToken() bool {
	if !s.HasAuth() {
		return false
	}
	return s.Auth.Token != ""
}

// HasTokenAndSecretRef reports whether SecretRef was configured for this CRD
func (s *CrossSource) HasTokenAndSecretRef() bool {
	if !s.HasAuth() {
		return false
	}
	return s.Auth.SecretRef != nil && s.Auth.Token != ""
}

// HasSecretRef reports whether SecretRef was configured for this CRD
func (s *CrossSource) HasSecretRef() bool {
	if !s.HasAuth() {
		return false
	}
	return s.Auth.SecretRef != nil
}
func (c *CRDEntry) HasCrossSecretRef() bool {
	if !c.HasCrossDecl() {
		return false
	}
	if c.OperatorBox.Empty() {
		return false
	}
	for _, cr := range c.OperatorBox.Cross {
		if cr.HasAnySecretRef() {
			return true
		}
	}
	return false
}
func (c *CrossCRDDeclaration) HasAnySecretRef() bool {
	if !c.HasSource() {
		return false
	}
	return c.Source.HasSecretRef()
}
