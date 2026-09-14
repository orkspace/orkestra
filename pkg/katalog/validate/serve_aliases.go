package validate

import (
	"fmt"
	"sort"
	"strings"
)

// validateServeAliases confirms:
//
//  1. The target map in map form has exactly one primary: true entry.
//  2. Every non-primary entry name is a valid target format (lowercase alphanumeric + hyphens).
//  3. No entry name collides with a primary target or another entry across the katalog.
//  4. Every token name declared on a non-primary entry is a subset of the CRD's
//     serve.tokens — an alias is a view of the CRD, not an independent token registry.
//     When the CRD has no token restrictions (all gateway tokens allowed), alias tokens
//     are validated against gateway.api.auth.tokens as the fallback authority.
//  5. (Warning) Primary surface disabled and no enabled aliases declared — the CRD
//     is not reachable via the Gateway API at all.
func (e *executor) validateServeAliases() error {
	gatewayTokens := e.k.GatewayTokenNames()
	knownTokens := make(map[string]struct{}, len(gatewayTokens))
	for _, name := range gatewayTokens {
		knownTokens[name] = struct{}{}
	}

	// Build the full routing namespace: primary targets first, then aliases.
	// Key = name, value = "primary target on <crd>" or "alias on <crd>".
	routing := make(map[string]string)
	for crdName, crd := range e.k.EnabledCRDs() {
		if crd.HasServeTarget() {
			routing[crd.ServeTarget()] = fmt.Sprintf("primary target on %q", crdName)
		}
	}

	for crdName, crd := range e.k.EnabledCRDs() {
		if !crd.ServeEnabled() || !crd.TargetModeEnabled() {
			continue
		}

		entries := crd.Serve.Target.Entries

		// 1. Map form: exactly one entry must be primary: true.
		// (Scalar shorthand has already been expanded to map form by this point.)
		if len(entries) > 0 {
			var primaries []string
			for name, cfg := range entries {
				if cfg != nil && cfg.Primary {
					primaries = append(primaries, name)
				}
			}
			if len(primaries) == 0 {
				return errServeTargetNoPrimary(crdName)
			}
			if len(primaries) > 1 {
				sort.Strings(primaries)
				return errServeTargetMultiplePrimaries(crdName, primaries)
			}
		}

		// 5. Warn when primary target is disabled and no enabled aliases are declared.
		if !crd.HasServeTarget() && !crd.HasServeAliases() {
			crd.Warnings.AddWarning(fmt.Sprintf(
				"crd %q: primary target is disabled and no enabled aliases are declared — "+
					"this CRD is not reachable via the Gateway API",
				crdName,
			))
			e.k.EnabledCRDs()[crdName] = crd
			continue
		}

		// Sort non-primary entry names for deterministic error messages.
		aliases := make([]string, 0, len(entries))
		for name, cfg := range entries {
			if cfg == nil || cfg.Primary {
				continue
			}
			aliases = append(aliases, name)
		}
		sort.Strings(aliases)

		for _, aliasName := range aliases {
			aliasCfg := entries[aliasName]

			// 2. Format.
			if !isValidServeTarget(aliasName) {
				return errServeAliasInvalidName(crdName, aliasName)
			}

			// 3. Uniqueness across routing namespace.
			if owner, clash := routing[aliasName]; clash {
				return errServeAliasCollision(crdName, aliasName, owner)
			}
			routing[aliasName] = fmt.Sprintf("alias on %q", crdName)

			// 4. Token validation.
			if aliasCfg == nil || !aliasCfg.HasTokenRestrictions() {
				continue
			}
			for tokenName := range aliasCfg.Tokens {
				if crd.Serve.HasTokenRestrictions() {
					if _, ok := crd.Serve.Tokens[tokenName]; !ok {
						crdTokenNames := make([]string, 0, len(crd.Serve.Tokens))
						for n := range crd.Serve.Tokens {
							crdTokenNames = append(crdTokenNames, n)
						}
						sort.Strings(crdTokenNames)
						return errServeAliasTokenNotInCRD(crdName, aliasName, tokenName, crdTokenNames)
					}
				} else {
					if _, ok := knownTokens[tokenName]; !ok {
						return fmt.Errorf(
							"%s crd %q: serve.target[%q].tokens[%q] — token %q is not declared in gateway.api.auth.tokens\n"+
								"  Available tokens: %s",
							failureMark(), crdName, aliasName, tokenName, tokenName,
							yellow(strings.Join(gatewayTokens, ", ")),
						)
					}
				}
			}
		}
	}

	return nil
}

// ── error helpers ────────────────────────────────────────────────────────────

func errServeTargetNoPrimary(crd string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.target map has no primary: true entry
   CRD: %s

Exactly one entry in the map must declare primary: true to identify the
primary target. Or use the scalar shorthand:
  serve:
    target: myapp
──────────────────────────────────────────────`, failureMark(), crd)
}

func errServeTargetMultiplePrimaries(crd string, primaries []string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.target map has multiple primary: true entries: %s
   CRD: %s

Exactly one entry may declare primary: true.

Remove the extra primary: true flags:
  serve:
    target:
      app:
        primary: true   # keep this one
      preview:
        primary: false  # or omit the field
──────────────────────────────────────────────`, failureMark(), strings.Join(primaries, ", "), crd)
}

func errServeAliasInvalidName(crd, alias string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s Invalid serve.target key: %q
   CRD: %s

Target and alias names must be lowercase alphanumeric with optional hyphens (a-z, 0-9, -).
──────────────────────────────────────────────`, failureMark(), alias, crd)
}

func errServeAliasTokenNotInCRD(crd, alias, token string, crdTokens []string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.target[%q].tokens[%q] not in CRD tokens
   CRD: %s

Alias tokens must be a subset of the CRD's serve.tokens — an alias is a
view of the CRD, not an independent token registry.
   CRD tokens: %s
──────────────────────────────────────────────`, failureMark(), alias, token, crd, strings.Join(crdTokens, ", "))
}

func errServeAliasCollision(crd, alias, owner string) error {
	return fmt.Errorf(`
──────────────────────────────────────────────
%s serve.target name collision: %q
   CRD: %s
   Already claimed by: %s

Target and alias names share the same routing namespace.
Rename the alias or the target to make them unique.
──────────────────────────────────────────────`, failureMark(), alias, crd, owner)
}
