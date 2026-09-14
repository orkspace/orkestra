package types_test

import (
	"testing"

	"github.com/orkspace/orkestra/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestTokenAllowed_PermissionClasses(t *testing.T) {
	cfg := func(tokens map[string]types.ServeTokenPermissions) *types.ServeConfig {
		return &types.ServeConfig{Tokens: tokens}
	}

	tests := []struct {
		name       string
		serve      *types.ServeConfig
		token      string
		op         string
		namespace  string
		class      types.ServeEndpointClass
		wantOK     bool
		wantReason types.ServeDenyReason
	}{
		{
			name:  "global wildcard allows all classes",
			serve: cfg(map[string]types.ServeTokenPermissions{"cc": {Permissions: types.ServePermissionSet{Global: []string{"*"}}}}),
			token: "cc", op: types.ServeOpDelete, class: types.ServeClassResources,
			wantOK: true,
		},
		{
			name: "schema class used for schema op, not resources",
			serve: cfg(map[string]types.ServeTokenPermissions{
				"audit": {Permissions: types.ServePermissionSet{
					Schema:    []string{"get"},
					Resources: []string{"get", "list"},
				}},
			}),
			token: "audit", op: types.ServeOpGet, class: types.ServeClassSchema,
			wantOK: true,
		},
		{
			name: "schema class denies create — not in schema perms",
			serve: cfg(map[string]types.ServeTokenPermissions{
				"audit": {Permissions: types.ServePermissionSet{
					Schema:    []string{"get"},
					Resources: []string{"get", "list"},
				}},
			}),
			// create is a resources operation, not schema — but we check class
			// correctly here: schema class, create op → schema has only get.
			token: "audit", op: types.ServeOpCreate, class: types.ServeClassSchema,
			wantOK:     false,
			wantReason: types.ServeDenyReasonOperation,
		},
		{
			name: "falls back to global when class list is empty",
			serve: cfg(map[string]types.ServeTokenPermissions{
				"ci": {Permissions: types.ServePermissionSet{
					Global: []string{"create", "update", "get"},
					// schema not set → inherits global
				}},
			}),
			token: "ci", op: types.ServeOpGet, class: types.ServeClassSchema,
			wantOK: true,
		},
		{
			name: "empty permissions denies all",
			serve: cfg(map[string]types.ServeTokenPermissions{
				"empty": {Permissions: types.ServePermissionSet{}},
			}),
			token: "empty", op: types.ServeOpGet, class: types.ServeClassResources,
			wantOK:     false,
			wantReason: types.ServeDenyReasonOperation,
		},
		{
			name: "namespace restriction respected regardless of class",
			serve: cfg(map[string]types.ServeTokenPermissions{
				"ci": {
					Namespaces:  []string{"staging"},
					Permissions: types.ServePermissionSet{Global: []string{"*"}},
				},
			}),
			token: "ci", op: types.ServeOpCreate, namespace: "production",
			class:      types.ServeClassResources,
			wantOK:     false,
			wantReason: types.ServeDenyReasonNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := tt.serve.TokenAllowed(tt.token, tt.op, tt.namespace, tt.class)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				assert.Equal(t, tt.wantReason, reason)
				assert.NotEmpty(t, reason.Message(tt.token, tt.op, "MyKind", tt.namespace))
			}
		})
	}
}

func TestServePermissionSetIsEmpty(t *testing.T) {
	assert.True(t, types.ServePermissionSet{}.Empty())
	assert.False(t, types.ServePermissionSet{Global: []string{"get"}}.Empty())
	assert.False(t, types.ServePermissionSet{Schema: []string{"get"}}.Empty())
	assert.False(t, types.ServePermissionSet{Resources: []string{"get"}}.Empty())
}
