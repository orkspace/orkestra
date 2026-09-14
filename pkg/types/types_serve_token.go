package types

import "slices"

// ServeOperation is a valid Gateway API operation string.
type ServeOperation = string

const (
	// ServeOpGet lists or reads a single CR via GET /api/v1/resources/...
	ServeOpGet ServeOperation = "get"

	// ServeOpList lists all CRs via GET /api/v1/resources/{kind}/{ns}
	ServeOpList ServeOperation = "list"

	// ServeOpCreate creates a new CR via POST /api/v1/apply when the CR does
	// not already exist in the cluster.
	ServeOpCreate ServeOperation = "create"

	// ServeOpUpdate updates an existing CR via POST /api/v1/apply when the CR
	// already exists in the cluster.
	ServeOpUpdate ServeOperation = "update"

	// ServeOpDelete deletes a CR via DELETE /api/v1/resources/...
	ServeOpDelete ServeOperation = "delete"

	// ServeOpAll is a wildcard that grants every operation. When present in the
	// permissions list, all other entries are redundant.
	ServeOpAll ServeOperation = "*"
)

// serveValidOperations is the authoritative set used by ork validate and ork serve.
var serveValidOperations = []string{
	ServeOpGet, ServeOpList, ServeOpCreate, ServeOpUpdate, ServeOpDelete, ServeOpAll,
}

// serveValidClasses is the authoritative set used by ork serve
var serveValidClasses = []string{
	ServeClassSchema, ServeClassResources,
}

// ServeEndpointClass identifies which Gateway API endpoint group a permission
// applies to.
type ServeEndpointClass = string

const (
	// ServeClassSchema covers GET /api/v1/schema and GET /api/v1/raw-schema.
	// The relevant operation is always "get".
	ServeClassSchema ServeEndpointClass = "schema"

	// ServeClassResources covers all /api/v1/resources/... endpoints:
	// get, list, create, update, delete.
	ServeClassResources ServeEndpointClass = "resources"
)

// ServePermissionSet declares operations permitted across endpoint classes.
//
// Resolution priority per class:
//  1. Class-specific list (schema: or resources:) when non-empty.
//  2. Global list when class-specific is empty.
//  3. No access when both are empty.
//
// When global is non-empty, class-specific lists must be subsets of it.
// ork validate enforces this. When global is empty, each class list is
// fully independent — fine-grained mode.
//
// Example — one token, two different access levels:
//
//	permissions:
//	  schema:    [get]              # can browse the catalog
//	  resources: [create, update]   # can create and update CRs
//
// Example — global with a narrower schema:
//
//	permissions:
//	  global:    [get, list, create, update]
//	  schema:    [get]              # further restricted for schema
//	  # resources inherits global
//
// Example — full access (star):
//
//	permissions:
//	  global: ["*"]
type ServePermissionSet struct {
	// Global applies to all endpoint classes that do not declare their own list.
	// When non-empty, schema and resources must be subsets of this list.
	Global []string `yaml:"global,omitempty" json:"global,omitempty"`

	// Schema controls access to the schema endpoints.
	// Falls back to global when empty.
	Schema []string `yaml:"schema,omitempty" json:"schema,omitempty"`

	// Resources controls access to the resource endpoints.
	// Falls back to global when empty.
	Resources []string `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// ServeTokenPermissions declares the access a named token has on a CRD.
type ServeTokenPermissions struct {
	// Namespaces restricts the token to specific namespaces.
	// Empty means all namespaces. Ignored for cluster-scoped CRDs.
	Namespaces []string `yaml:"namespaces,omitempty" json:"namespaces,omitempty"`

	// Permissions declares the operations this token may perform, split by
	// endpoint class. See ServePermissionSet for the resolution rules.
	Permissions ServePermissionSet `yaml:"permissions,omitempty" json:"permissions,omitempty"`
}

// activePerms returns the permission list that applies to the given endpoint
// class for this token. Returns nil when no permissions apply (no access).
func (p ServeTokenPermissions) activePerms(class ServeEndpointClass) []string {
	switch class {
	case ServeClassSchema:
		if len(p.Permissions.Schema) > 0 {
			return p.Permissions.Schema
		}
	case ServeClassResources:
		if len(p.Permissions.Resources) > 0 {
			return p.Permissions.Resources
		}
	}
	// Fall back to global.
	return p.Permissions.Global
}

// Empty reports whether no permissions at all are declared.
// A token with empty permissions can authenticate but cannot do anything.
func (p ServePermissionSet) Empty() bool {
	return len(p.Global) == 0 && len(p.Schema) == 0 && len(p.Resources) == 0
}

// IsGlobalEmpty reports whether no global permissions are declared.
func IsGlobalEmpty(p ServePermissionSet) bool {
	return len(p.Global) == 0
}

// IsSchemaEmpty reports whether no schema permissions are declared.
func IsSchemaEmpty(p ServePermissionSet) bool {
	return len(p.Schema) == 0
}

// IsResourcesEmpty reports whether no resource permissions are declared.
func IsResourcesEmpty(p ServePermissionSet) bool {
	return len(p.Resources) == 0
}

// IsValidServeOperation reports whether s is one of the declared ServeOperation
// constants. Defined here to avoid importing types into the validate file.
func IsValidServeOperation(s string) bool {
	for _, op := range serveValidOperations {
		if s == op {
			return true
		}
	}
	return false
}

// ValidServeOperations returns the list of valid serve operations.
func ValidServeOperations() []string {
	return serveValidOperations
}

// IsValidServeEndpointClass reports whether is one of the declared ServeClass constants.
func IsValidServeEndpointClass(s string) bool {
	for _, class := range serveValidClasses {
		if s == class {
			return true
		}
	}
	return false
}

// ValidServeEndpointClasses returns the list of valid serve endpoint classes.
func ValidServeEndpointClasses() []string {
	return serveValidClasses
}

// HasNamespace returns true if the token is allowed in the given namespace.
// Empty namespaces list means all namespaces are allowed.
// For cluster-scoped CRDs, namespace checks are skipped.
func (p ServeTokenPermissions) HasNamespace(namespace string) bool {
	if len(p.Namespaces) == 0 {
		return true // All namespaces allowed
	}
	for _, ns := range p.Namespaces {
		if ns == namespace {
			return true
		}
	}
	return false
}

// TokensMap returns the map of allowed tokens.
func (serve *ServeConfig) TokensMap() map[string]ServeTokenPermissions {
	if serve == nil {
		return nil
	}
	return serve.Tokens
}

// Namespaces returns the list of allowed namespaces.
// Empty slice means all namespaces are allowed.
func (p ServeTokenPermissions) TokenNamespaces() []string {
	return p.Namespaces
}

// IsNamespaceRestricted returns true if the token has namespace restrictions.
func (p ServeTokenPermissions) IsNamespaceRestricted() bool {
	return len(p.Namespaces) > 0
}

// ─── Scope Existence Methods ──────────────────────────────────────────────

// HasGlobalPermissions returns true if global permissions are declared.
func (p ServeTokenPermissions) HasGlobalPermissions() bool {
	return len(p.Permissions.Global) > 0
}

// HasSchemaPermissions returns true if schema permissions are declared.
func (p ServeTokenPermissions) HasSchemaPermissions() bool {
	return len(p.Permissions.Schema) > 0
}

// HasResourcesPermissions returns true if resource permissions are declared.
func (p ServeTokenPermissions) HasResourcesPermissions() bool {
	return len(p.Permissions.Resources) > 0
}

// HasAnyPermissions returns true if any permissions are declared.
func (p ServeTokenPermissions) HasAnyPermissions() bool {
	return p.HasGlobalPermissions() || p.HasSchemaPermissions() || p.HasResourcesPermissions()
}

// HasGlobalWildcard returns true if the permissions include "*" (all operations) at global level.
func (p ServeTokenPermissions) HasGlobalWildcard() bool {
	return slices.Contains(p.Permissions.Global, ServeOpAll)
}

// HasResourcesWildcard returns true if the permissions include "*" (all operations) at resource level.
func (p ServeTokenPermissions) HasResourcesWildcard() bool {
	return slices.Contains(p.Permissions.Resources, ServeOpAll)
}

// HasSchemaWildcard returns true if the permissions include "*" (all operations) at schema level.
func (p ServeTokenPermissions) HasSchemaWildcard() bool {
	return slices.Contains(p.Permissions.Schema, ServeOpAll)
}

// HasWildcard returns true if any permission list contains "*".
func (p ServeTokenPermissions) HasWildcard() bool {
	return p.HasGlobalWildcard() || p.HasSchemaWildcard() || p.HasResourcesWildcard()
}

// HasOperation returns true if the permission set includes the given operation
// for the specified endpoint class.
//
// If class-specific permissions are empty, falls back to global.
func (p ServeTokenPermissions) HasOperation(class ServeEndpointClass, op string) bool {
	perms := p.activePerms(class)
	if len(perms) == 0 {
		return false
	}
	for _, perm := range perms {
		if perm == ServeOpAll || perm == op {
			return true
		}
	}
	return false
}

// HasGlobalOperation checks if the operation is in the global list.
func (p ServeTokenPermissions) HasGlobalOperation(op string) bool {
	for _, perm := range p.Permissions.Global {
		if perm == ServeOpAll || perm == op {
			return true
		}
	}
	return false
}

// HasSchemaOperation checks if the operation is in the schema list.
func (p ServeTokenPermissions) HasSchemaOperation(op string) bool {
	for _, perm := range p.Permissions.Schema {
		if perm == ServeOpAll || perm == op {
			return true
		}
	}
	return false
}

// HasResourcesOperation checks if the operation is in the resources list.
func (p ServeTokenPermissions) HasResourcesOperation(op string) bool {
	for _, perm := range p.Permissions.Resources {
		if perm == ServeOpAll || perm == op {
			return true
		}
	}
	return false
}
