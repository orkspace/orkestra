package validate

import (
	"fmt"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validateServeTokenRestrictions confirms:
//
//  1. Every token name in serve.tokens exists in gateway.api.auth.tokens.
//  2. Every permission string is one of the valid ServeOperation constants.
//  3. No permission list (global/schema/resources) repeats the same operation.
//  4. Schema permissions only support get/list — create/update/delete are ignored
//     when schema inherits from global.
//  5. When global is non-empty, class lists must be subsets of it.
//  6. (Warning) All three permission lists are empty — the token grants nothing.
//  7. (Warning) Namespace restrictions on a cluster-scoped CRD are ignored.
//  8. Every serve permitted namespace must be allowed at crd level.
//  9. (Warning) A token entry with an empty permissions list grants no access.
func (e *executor) validateServeTokenRestrictions() error {
	// A serve.tokens key can be authorized two ways: a gateway.api.auth.tokens
	// entry (a Bearer/OIDC caller), or a gateway.webhooks entry's own Name (a
	// push/command/JSON delivery) — TokenAllowedFor treats both identically at
	// runtime, so both count as "known" here.
	gatewayTokens := e.k.GatewayTokenNames()
	webhookNames := e.GatewayWebhookEntryNames()
	knownTokens := make(map[string]struct{}, len(gatewayTokens)+len(webhookNames))
	for _, name := range gatewayTokens {
		knownTokens[name] = struct{}{}
	}
	for _, name := range webhookNames {
		knownTokens[name] = struct{}{}
	}

	allowedNamespaces := make(map[string]map[string]bool)
	restrictedNamespaces := make(map[string]map[string]bool)

	// Keyed by the enabledCRDs map key.
	for crdName, crd := range e.k.EnabledCRDs() {
		allowedNamespaces[crdName] = make(map[string]bool)
		for _, ns := range crd.AllowedNamespaces {
			allowedNamespaces[crdName][ns] = true
		}
		restrictedNamespaces[crdName] = make(map[string]bool)
		for _, ns := range crd.RestrictedNamespaces {
			restrictedNamespaces[crdName][ns] = true
		}
	}

	validSchemaOps := map[string]bool{
		orktypes.ServeOpGet:  true,
		orktypes.ServeOpList: true,
	}

	for crdName, crd := range e.k.EnabledCRDs() {
		if crd.Serve == nil || !crd.Serve.HasTokenRestrictions() {
			continue
		}

		for tokenName, perms := range crd.Serve.TokensMap() {
			knownTokensStr := strings.Join(append(append([]string{}, gatewayTokens...), webhookNames...), ", ")

			// 1. Token must exist at the gateway level, either as a
			// gateway.api.auth.tokens entry or a gateway.webhooks entry's Name.
			if _, ok := knownTokens[tokenName]; !ok {
				return fmt.Errorf(
					"%s crd %q: serve.tokens[%q] — token %q is not declared in gateway.api.auth.tokens "+
						"or gateway.webhooks\n"+
						"  Add it there or remove it from tokens.\n"+
						"  Available: %s",
					failureMark(), crdName, tokenName, tokenName, yellow(knownTokensStr),
				)
			}

			// 2. Validate every operation string in all three lists.
			permissionLists := []struct {
				name string
				ops  []string
			}{
				{"global", perms.Permissions.Global},
				{"schema", perms.Permissions.Schema},
				{"resources", perms.Permissions.Resources},
			}

			for _, list := range permissionLists {
				for _, op := range list.ops {
					if !orktypes.IsValidServeOperation(op) {
						return fmt.Errorf(
							"%s crd %q: serve.tokens[%q].permissions.%s: %q is not a valid operation\n"+
								"  Valid values: get, list, create, update, delete, *",
							failureMark(), crdName, tokenName, list.name, op,
						)
					}
				}
			}

			// 3. No permission list repeats the same operation.
			for _, list := range permissionLists {
				seen := make(map[string]bool, len(list.ops))
				for _, op := range list.ops {
					if seen[op] {
						return fmt.Errorf(
							"%s crd %q: serve.tokens[%q].permissions.%s: %q is listed more than once",
							failureMark(), crdName, tokenName, list.name, op,
						)
					}
					seen[op] = true
				}
			}

			// 4. Schema permissions validation.
			if len(perms.Permissions.Schema) > 0 {
				// Explicit schema list — validate each operation.
				for _, op := range perms.Permissions.Schema {
					if !validSchemaOps[op] {
						return fmt.Errorf(
							"%s crd %q: serve.tokens[%q].permissions.schema: %q is not allowed for schema endpoints\n"+
								"  Schema endpoints only support: get, list",
							failureMark(), crdName, tokenName, op,
						)
					}
				}
			} else if len(perms.Permissions.Global) > 0 {
				// Schema inherits from global — check for create/update/delete warnings.
				for _, op := range perms.Permissions.Global {
					if op == orktypes.ServeOpAll {
						continue // Wildcard is fine
					}
					if !validSchemaOps[op] {
						crd.Warnings.AddWarning(fmt.Sprintf(
							"crd %q: serve.tokens[%q].permissions.global contains %q which is not valid for schema endpoints — "+
								"it will be ignored for schema and only apply to resources",
							crdName, tokenName, op,
						))
						e.k.EnabledCRDs()[crdName] = crd
					}
				}
			}

			// 5. When global is non-empty, class lists must be subsets.
			if perms.HasGlobalPermissions() && !perms.HasGlobalWildcard() {
				globalSet := toStringSet(perms.Permissions.Global)

				for _, classEntry := range []struct {
					name string
					ops  []string
				}{
					{"schema", perms.Permissions.Schema},
					{"resources", perms.Permissions.Resources},
				} {
					for _, op := range classEntry.ops {
						if op == orktypes.ServeOpAll {
							return fmt.Errorf(
								"%s crd %q: serve.tokens[%q].permissions.%s: %q "+
									"is broader than global permissions %v\n"+
									"  Set global: [\"*\"] to allow wildcard, or remove \"*\" from %s.",
								failureMark(), crdName, tokenName, classEntry.name, op,
								perms.Permissions.Global, classEntry.name,
							)
						}
						if !globalSet[op] {
							return fmt.Errorf(
								"%s crd %q: serve.tokens[%q].permissions.%s: %q "+
									"is not in global permissions %v\n"+
									"  Class permissions must be a subset of global, "+
									"or set global to empty for fine-grained mode.",
								failureMark(), crdName, tokenName, classEntry.name, op,
								perms.Permissions.Global,
							)
						}
					}
				}
			}

			// 6. (Warning) All permissions are empty — token grants nothing.
			if perms.Permissions.Empty() {
				crd.Warnings.AddWarning(fmt.Sprintf(
					"%s crd %q: serve.tokens[%q] has no permissions declared — "+
						"the token can authenticate but cannot perform any operation on this CRD",
					failureMark(), crdName, tokenName,
				))
				e.k.EnabledCRDs()[crdName] = crd
			}

			// 7. (Warning) Namespace restrictions on a cluster-scoped CRD.
			if !crd.IsNamespaced() && perms.IsNamespaceRestricted() {
				crd.Warnings.AddWarning(fmt.Sprintf(
					"%s crd %q: serve.tokens[%q].namespaces is set but %q is cluster-scoped — "+
						"namespace restrictions are ignored for cluster-scoped resources",
					failureMark(), crdName, tokenName, crdName,
				))
				e.k.EnabledCRDs()[crdName] = crd
			}

			// 8. Every namespace must be allowed at CRD level.
			for _, ns := range perms.Namespaces {
				if crd.HasAllowedNamespaces() {
					if !allowedNamespaces[crdName][ns] {
						allowedList := strings.Join(crd.AllowedNamespaces, ", ")
						return fmt.Errorf(
							"%s crd %q: serve.tokens[%q].namespaces: %q is not in allowedNamespaces\n"+
								"  Allowed namespaces: %s",
							failureMark(), crdName, tokenName, ns, allowedList,
						)
					}
				}

				if crd.HasRestrictedNamespaces() {
					if restrictedNamespaces[crdName][ns] {
						restrictedList := strings.Join(crd.RestrictedNamespaces, ", ")
						return fmt.Errorf(
							"%s crd %q: serve.tokens[%q].namespaces: %q is in restrictedNamespaces\n"+
								"  Restricted namespaces: %s",
							failureMark(), crdName, tokenName, ns, restrictedList,
						)
					}
				}
			}
		}
	}

	return nil
}
