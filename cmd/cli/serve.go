//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/orkspace/orkestra/pkg/katalog/validate"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

// errRequiresCRDSelector is returned by serve subcommands that require the
// caller to identify a CRD via at least one of the standard selector flags.
var errRequiresCRDSelector = fmt.Errorf("%s one of --target, --kind, --name, or --alias is required", failureMark())

// ── serve ─────────────────────────────────────────────────────────────────────

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Inspect and validate Serve configurations",
	Long: `Inspect and validate Internal Developer Platform (Serve) configurations.

The Serve is the contract between platform teams and developers. It defines
what fields developers can submit, what the gateway builds, and what
callers see.

Subcommands:
  validate    Validate Serve configuration in a Katalog
  schema      Show the flat schema for a Serve target
  fields      List all Serve fields with their paths and types
  tokens      Show token permissions for a CRD
  targets     List all Serve targets in a Katalog
  aliases     List serve aliases in a Katalog
  can-i       Check if a token can perform an operation
  response    Show the Serve response configuration`,
}

// ── serve validate ────────────────────────────────────────────────────────────

var serveValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate Serve configuration",
	Long: `Validate Serve configuration in a Katalog.

This runs the same Serve-specific validations as ork validate, but only for
Serve concerns: fields, paths, tokens, response config, and namespace rules.

It does not check the full Katalog schema — only the Serve portions.

With --full, shows a detailed breakdown of the Serve configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		full, _ := cmd.Flags().GetBool("full")

		if err := validate.ValidateServe(k); err != nil {
			return err
		}

		if full {
			printServeValidationSummary(k)
		} else {
			fmt.Printf("\n%s Serve configuration is valid\n", successMark())
		}

		return nil
	},
}

// ── serve schema ─────────────────────────────────────────────────────────────

var serveSchemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Show the flat schema for a Serve target",
	Long: `Show the flat schema for a Serve target.

This displays the fields that callers can submit for a target, including
their labels, types, enums, and paths.

The output is the same flat field structure returned by the gateway's
GET /api/v1/schema?target=<t> endpoint.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, _ := cmd.Flags().GetString("target")
		kind, _ := cmd.Flags().GetString("kind")
		name, _ := cmd.Flags().GetString("name")
		alias, _ := cmd.Flags().GetString("alias")

		if target == "" && kind == "" && name == "" && alias == "" {
			return errRequiresCRDSelector
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		var crd *orktypes.CRDEntry
		header := ""

		if alias != "" && target == "" && kind == "" && name == "" {
			var resolvedAlias string
			crd, resolvedAlias, err = resolveCRDByAnyTarget(k, alias)
			if err != nil {
				return err
			}
			if resolvedAlias == "" {
				return fmt.Errorf("%s %q is a primary target, not an alias — use --target instead", failureMark(), alias)
			}
			header = fmt.Sprintf("\nSchema for: %s (alias: %s → target: %s)\n", crd.Name, alias, crd.ServeTarget())
		} else {
			crd, err = resolveCRD(k, target, kind, name)
			if err != nil {
				return err
			}
			header = fmt.Sprintf("\nSchema for: %s (target: %s)\n", crd.Name, crd.ServeTarget())
		}

		if !crd.ServeEnabled() {
			return fmt.Errorf("%s CRD %q is not Serve-enabled", failureMark(), crd.Name)
		}

		fields := crd.AllServeFields()
		if len(fields) == 0 {
			fmt.Printf("\nNo fields defined for %q\n", crd.Name)
			return nil
		}

		fmt.Print(header)
		fmt.Printf("%s\n", strings.Repeat("─", 70))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "FIELD\tLABEL\tTYPE\tPATH\tREQUIRED")

		for name, config := range fields {
			specPath := config.SpecPath(name)
			required := ""
			if config.Required {
				required = "✓"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				name,
				config.Label,
				config.FieldType(),
				specPath,
				required,
			)
		}
		w.Flush()
		fmt.Println()
		return nil
	},
}

// ── serve fields ─────────────────────────────────────────────────────────────

var serveFieldsCmd = &cobra.Command{
	Use:   "fields",
	Short: "List Serve fields with their paths and types",
	Long: `List Serve fields in a Katalog with their paths and types.

This shows fields declared in serve.fields, serve.labels and serve.annotations
across all Serve-enabled CRDs.

With --target, --kind, or --name, shows fields for a specific CRD.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, _ := cmd.Flags().GetString("target")
		kind, _ := cmd.Flags().GetString("kind")
		name, _ := cmd.Flags().GetString("name")
		alias, _ := cmd.Flags().GetString("alias")
		sortBy, _ := cmd.Flags().GetString("sort-by")

		// Validate sortBy
		if sortBy != "" && sortBy != "name" && sortBy != "order" {
			return fmt.Errorf("%s --sort-by must be 'name' or 'order'", failureMark())
		}
		if sortBy == "" {
			sortBy = "name"
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		// ── If a specific CRD is requested ──────────────────────────────────
		if target != "" || kind != "" || name != "" || alias != "" {
			var crd *orktypes.CRDEntry
			header := ""

			if alias != "" && target == "" && kind == "" && name == "" {
				var resolvedAlias string
				crd, resolvedAlias, err = resolveCRDByAnyTarget(k, alias)
				if err != nil {
					return err
				}
				if resolvedAlias == "" {
					return fmt.Errorf("%s %q is a primary target, not an alias — use --target instead", failureMark(), alias)
				}
				header = fmt.Sprintf("\nFields for: %s (alias: %s → target: %s)\n", crd.Name, alias, crd.ServeTarget())
			} else {
				crd, err = resolveCRD(k, target, kind, name)
				if err != nil {
					return err
				}
				header = fmt.Sprintf("\nFields for: %s (target: %s)\n", crd.Name, crd.ServeTarget())
			}

			if !crd.ServeEnabled() {
				return fmt.Errorf("%s CRD %q is not Serve-enabled", failureMark(), crd.Name)
			}

			entries := sortedFieldEntries(crd, sortBy)
			if len(entries) == 0 {
				fmt.Printf("\nNo fields defined for CRD %q\n", crd.Name)
				return nil
			}

			fmt.Print(header)
			fmt.Printf("%s\n", strings.Repeat("─", 70))

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "FIELD\tTYPE\tPATH\tSOURCE\tREQUIRED")

			for _, entry := range entries {
				required := ""
				if entry.Config.Required {
					required = "✓"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					entry.Name,
					entry.Config.FieldType(),
					entry.SpecPath,
					entry.Source,
					required,
				)
			}
			w.Flush()
			fmt.Println()
			return nil
		}

		// ── All fields across all CRDs ──────────────────────────────────────
		crds := k.ServeEnabledCRDs()
		sort.Slice(crds, func(i, j int) bool {
			if crds[i] == nil || crds[j] == nil {
				return false
			}
			return crds[i].Name < crds[j].Name
		})

		var totalFields int
		fmt.Printf("\nServe Fields\n")
		fmt.Printf("%s\n", strings.Repeat("─", 70))

		for _, crd := range crds {
			if crd == nil {
				continue
			}
			entries := sortedFieldEntries(crd, sortBy)
			if len(entries) == 0 {
				continue
			}

			fmt.Printf("\nCRD: %s (target: %s)\n", crd.Name, crd.ServeTarget())
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  FIELD\tTYPE\tPATH\tSOURCE")

			for _, entry := range entries {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
					entry.Name,
					entry.Config.FieldType(),
					entry.SpecPath,
					entry.Source,
				)
				totalFields++
			}
			w.Flush()
		}

		if totalFields == 0 {
			fmt.Println("\nNo Serve fields found.")
		} else {
			fmt.Printf("\nTotal: %d fields across %d CRDs\n", totalFields, len(crds))
		}
		fmt.Println()
		return nil
	},
}

// ── serve tokens ─────────────────────────────────────────────────────────────

var serveTokensCmd = &cobra.Command{
	Use:   "tokens",
	Short: "Show token permissions for a CRD",
	Long: `Show token permissions for a Serve-enabled CRD.

This displays the tokens configuration, including which tokens
have access, their permissions (global/schema/resources), and namespace
restrictions.

With --alias, shows the effective token config for that alias entry.
When the alias declares its own tokens, those are shown directly.
When it inherits from the CRD, the CRD-level tokens are shown with a note.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, _ := cmd.Flags().GetString("target")
		kind, _ := cmd.Flags().GetString("kind")
		name, _ := cmd.Flags().GetString("name")
		alias, _ := cmd.Flags().GetString("alias")

		if target == "" && kind == "" && name == "" && alias == "" {
			return errRequiresCRDSelector
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		var crd *orktypes.CRDEntry
		if alias != "" && target == "" && kind == "" && name == "" {
			// --alias alone: resolve CRD from alias name
			var resolvedAlias string
			crd, resolvedAlias, err = resolveCRDByAnyTarget(k, alias)
			if err != nil {
				return err
			}
			if resolvedAlias == "" {
				return fmt.Errorf("%s %q is a primary target, not an alias — use --target instead", failureMark(), alias)
			}
		} else {
			crd, err = resolveCRD(k, target, kind, name)
			if err != nil {
				return err
			}
		}

		if !crd.ServeEnabled() {
			return fmt.Errorf("%s CRD %q is not Serve-enabled", failureMark(), crd.Name)
		}

		tokensMap := crd.ServeTokensFor(alias)
		source := "CRD-level serve.tokens"
		if alias != "" {
			if entry := crd.LookupTarget(alias); entry != nil && entry.HasTokenRestrictions() {
				source = fmt.Sprintf("alias %q (own tokens)", alias)
			} else {
				source = fmt.Sprintf("alias %q (inherits CRD-level tokens)", alias)
			}
		}

		if len(tokensMap) == 0 {
			if alias != "" {
				fmt.Printf("\nNo token restrictions for alias %q (CRD: %s) — all tokens allowed\n\n", alias, crd.Name)
			} else {
				fmt.Printf("\nNo token restrictions for CRD %q — all tokens allowed\n\n", crd.Name)
			}
			return nil
		}

		if alias != "" {
			fmt.Printf("\nToken permissions for alias %q (CRD: %s)\n", alias, crd.Name)
			fmt.Printf("Source: %s\n", source)
		} else {
			fmt.Printf("\nToken permissions for CRD: %s (target: %s)\n", crd.Name, crd.ServeTarget())
		}
		fmt.Printf("%s\n", strings.Repeat("─", 70))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TOKEN\tGLOBAL\tSCHEMA\tRESOURCES\tNAMESPACES")

		for tokenName, perms := range tokensMap {
			global := strings.Join(perms.Permissions.Global, ",")
			schema := strings.Join(perms.Permissions.Schema, ",")
			resources := strings.Join(perms.Permissions.Resources, ",")
			namespaces := strings.Join(perms.Namespaces, ",")
			if namespaces == "" {
				namespaces = "*"
			}
			if global == "" && schema == "" && resources == "" {
				global = "—"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				tokenName,
				global,
				schema,
				resources,
				namespaces,
			)
		}
		w.Flush()
		fmt.Println()
		return nil
	},
}

// ── serve targets ─────────────────────────────────────────────────────────────

var serveTargetsCmd = &cobra.Command{
	Use:   "targets",
	Short: "List all Serve targets in a Katalog",
	Long: `List all Serve-enabled targets in a Katalog.

This shows each target, its CRD kind, and whether it has fields defined.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		var entries []struct {
			Target   string
			Kind     string
			Fields   int
			HasToken bool
			Aliases  int
		}

		for _, crd := range k.ServeEnabledCRDs() {
			if crd == nil {
				continue
			}
			entries = append(entries, struct {
				Target   string
				Kind     string
				Fields   int
				HasToken bool
				Aliases  int
			}{
				Target:   crd.ServeTarget(),
				Kind:     crd.Kind(),
				Fields:   len(crd.AllServeFields()),
				HasToken: crd.HasServeTokenRestrictions(),
				Aliases:  len(crd.ServeAliases()),
			})
		}

		if len(entries) == 0 {
			fmt.Println("\nNo Serve targets found.")
			return nil
		}

		fmt.Printf("\nServe Targets\n")
		fmt.Printf("%s\n", strings.Repeat("─", 50))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TARGET\tKIND\tFIELDS\tTOKENS\tALIASES")

		for _, e := range entries {
			tokens := "no"
			if e.HasToken {
				tokens = "yes"
			}
			aliases := "—"
			if e.Aliases > 0 {
				aliases = fmt.Sprintf("%d", e.Aliases)
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
				e.Target,
				e.Kind,
				e.Fields,
				tokens,
				aliases,
			)
		}
		w.Flush()
		fmt.Printf("\n%d target(s)\n", len(entries))
		fmt.Println()
		return nil
	},
}

// ── serve can-i ──────────────────────────────────────────────────────────────

var serveCanICmd = &cobra.Command{
	Use:   "can-i",
	Short: "Check if a token can perform an operation",
	Long: `Check if a token can perform an operation on a target.

This evaluates the same permission checks that the gateway applies to
incoming Gateway API requests. It considers:

  - Token existence in gateway.api.auth.tokens
  - Token permissions (global/schema/resources scopes)
  - Namespace restrictions
  - Target existence

Useful for debugging permission issues and auditing token capabilities.

Examples:
  ork serve can-i --token control-center --target smartapp --operation create
  ork serve can-i --token ci-pipeline --target smartapp --operation delete --namespace staging
  ork serve can-i --token monitoring --target smartapp --operation list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		token, _ := cmd.Flags().GetString("token")
		op, _ := cmd.Flags().GetString("operation")
		namespace, _ := cmd.Flags().GetString("namespace")
		classFlag, _ := cmd.Flags().GetString("class")
		target, _ := cmd.Flags().GetString("target")
		kind, _ := cmd.Flags().GetString("kind")
		name, _ := cmd.Flags().GetString("name")
		aliasFlag, _ := cmd.Flags().GetString("alias")

		if token == "" {
			return fmt.Errorf("%s --token is required", failureMark())
		}
		if op == "" {
			return fmt.Errorf("%s --operation is required (%s)", failureMark(), validServeOperations)
		}
		if !orktypes.IsValidServeOperation(op) {
			return fmt.Errorf("%s --operation must be one of %s", failureMark(), validServeOperations)
		}
		if classFlag != "" && !orktypes.IsValidServeEndpointClass(classFlag) {
			return fmt.Errorf("%s --class must be one of %s", failureMark(), validServeEndpointClasses)
		}
		if target == "" && kind == "" && name == "" && aliasFlag == "" {
			return errRequiresCRDSelector
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		// When --target is given, use LookupByTargetOrAlias so alias names work
		// as targets. For --kind / --name, alias is supplied separately via --alias.
		// When --alias alone is given, resolve via alias directly.
		var (
			crd   *orktypes.CRDEntry
			alias string
		)
		if target != "" {
			crd, alias, err = resolveCRDByAnyTarget(k, target)
			if err != nil {
				return err
			}
			// --alias overrides the alias resolved from the target lookup
			// (e.g. user passes --target smartapp --alias public explicitly).
			if aliasFlag != "" {
				alias = aliasFlag
			}
		} else if aliasFlag != "" && kind == "" && name == "" {
			var resolvedAlias string
			crd, resolvedAlias, err = resolveCRDByAnyTarget(k, aliasFlag)
			if err != nil {
				return err
			}
			if resolvedAlias == "" {
				return fmt.Errorf("%s %q is a primary target, not an alias — use --target instead", failureMark(), aliasFlag)
			}
			alias = resolvedAlias
		} else {
			crd, err = resolveCRD(k, target, kind, name)
			if err != nil {
				return err
			}
			alias = aliasFlag
		}

		if !crd.ServeEnabled() {
			return fmt.Errorf("%s CRD %q is not Serve-enabled", failureMark(), crd.Name)
		}

		// Verify the token exists at the gateway level first.
		gatewayTokens := k.GatewayTokenNames()
		tokenKnown := false
		for _, t := range gatewayTokens {
			if t == token {
				tokenKnown = true
				break
			}
		}
		if !tokenKnown {
			printCanIResult(false, token, op, crd, namespace, alias,
				fmt.Sprintf("token %q is not defined in gateway.api.auth.tokens", token),
				gatewayTokens)
			return nil
		}

		// Determine endpoint class.
		class := orktypes.ServeClassResources
		if strings.EqualFold(classFlag, "schema") {
			class = orktypes.ServeClassSchema
		}

		// CRD-level namespace guard (independent of token restrictions).
		if namespace != "" && crd.IsNamespaceRestricted() && !crd.IsNamespaceAuthorized(namespace) {
			printCanIResult(false, token, op, crd, namespace, alias,
				fmt.Sprintf("namespace %q is not allowed for CRD %q", namespace, crd.Name),
				crd.AllowedNamespaces)
			return nil
		}

		// Alias-aware token permission check — delegates to ServeConfig.TokenAllowed
		// with the resolved token set (alias-specific → CRD-level → allow all).
		allowed, reason := crd.TokenAllowedFor(alias, token, op, namespace, class)
		if allowed {
			printCanIResult(true, token, op, crd, namespace, alias,
				fmt.Sprintf("token %q has %q permission", token, op), nil)
		} else {
			printCanIResult(false, token, op, crd, namespace, alias,
				reason.Message(token, op, crd.Kind(), namespace), nil)
		}
		return nil
	},
}

// ── serve response ────────────────────────────────────────────────────────────

var serveResponseCmd = &cobra.Command{
	Use:   "response",
	Short: "Show the Serve response configuration",
	Long: `Show the Serve response configuration for a target.

This displays what callers will see in the Gateway API response based on
serve.config.response. It shows:

  - default: true/false
  - Payload fields (with their template expressions)
  - Excluded paths
  - Poll URL configuration

No cluster access is required — this reads the Katalog directly.

Examples:
  ork serve response --target smartapp
  ork serve response --target app --preview`,
	RunE: func(cmd *cobra.Command, args []string) error {
		preview, _ := cmd.Flags().GetBool("preview")
		target, _ := cmd.Flags().GetString("target")
		kind, _ := cmd.Flags().GetString("kind")
		name, _ := cmd.Flags().GetString("name")
		alias, _ := cmd.Flags().GetString("alias")

		if target == "" && kind == "" && name == "" && alias == "" {
			return errRequiresCRDSelector
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		var crd *orktypes.CRDEntry
		if alias != "" && target == "" && kind == "" && name == "" {
			var resolvedAlias string
			crd, resolvedAlias, err = resolveCRDByAnyTarget(k, alias)
			if err != nil {
				return err
			}
			if resolvedAlias == "" {
				return fmt.Errorf("%s %q is a primary target, not an alias — use --target instead", failureMark(), alias)
			}
		} else {
			crd, err = resolveCRD(k, target, kind, name)
			if err != nil {
				return err
			}
		}

		if !crd.ServeEnabled() {
			return fmt.Errorf("%s CRD %q is not Serve-enabled", failureMark(), crd.Name)
		}

		cfg := crd.ServeResponseConfigFor(alias)
		if cfg == nil {
			if alias != "" {
				fmt.Printf("\nNo response configuration for alias %q (CRD: %s) — uses default CR response\n\n", alias, crd.Name)
			} else {
				fmt.Printf("\nNo response configuration for CRD %q — callers receive the full CR as-is.\n", crd.Name)
				fmt.Println("  Add serve.config.response to customize what callers see.")
				fmt.Println()
			}
			return nil
		}

		if alias != "" {
			fmt.Printf("\nResponse configuration for alias %q (CRD: %s)\n", alias, crd.Name)
		} else {
			fmt.Printf("\nResponse configuration for: %s (target: %s)\n", crd.Name, crd.ServeTarget())
		}
		fmt.Printf("%s\n", strings.Repeat("─", 70))

		fmt.Printf("\ndefault: %v\n", cfg.UseDefault())
		if cfg.UseDefault() && !cfg.HasExclude() {
			if cfg.HasPayload() {
				fmt.Println("  Callers receive the full CR object plus the payload fields below.")
			} else {
				fmt.Println("  Callers receive the full CR object.")
			}
		}

		if cfg.HasPayload() {
			fmt.Println("\nPayload fields:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  FIELD\tEXPRESSION")
			for key, expr := range cfg.Payload {
				display := expr
				if len(display) > 55 {
					display = display[:52] + "..."
				}
				fmt.Fprintf(w, "  %s\t%s\n", key, display)
			}
			w.Flush()
		} else {
			fmt.Println("\nPayload fields: none")
		}

		if cfg.HasExclude() {
			fmt.Println("\nExcluded paths:")
			for _, path := range cfg.Exclude {
				fmt.Printf("  ✗ %s\n", path)
			}
		} else {
			fmt.Println("\nExcluded paths: none")
		}

		if cfg.HasPoll() {
			fmt.Println("\nPoll URL:")
			if cfg.Poll.URL != "" {
				fmt.Printf("  url:   %q\n", cfg.Poll.URL)
			}
			if cfg.Poll.Field != "" {
				fmt.Printf("  field: %q\n", cfg.Poll.Field)
			}
		}

		if preview {
			fmt.Printf("\n%s\n", strings.Repeat("─", 70))
			fmt.Println("\nResponse preview (templates shown as-is, not resolved):")

			if cfg.HasPayload() {
				fmt.Println()
				fmt.Printf("%s\n", blue("{"))

				keys := make([]string, 0, len(cfg.Payload))
				for k := range cfg.Payload {
					keys = append(keys, k)
				}
				sort.Strings(keys)

				for i, key := range keys {
					expr := cfg.Payload[key]
					comma := ","
					if i == len(keys)-1 {
						comma = ""
					}
					fmt.Printf("  %s: %s%s\n",
						blue(fmt.Sprintf("%q", key)),
						green(fmt.Sprintf("%q", expr)),
						white(comma),
					)
				}
				fmt.Printf("%s\n", blue("}"))
			} else {
				fmt.Println("\n  No payload fields configured.")
			}

			if cfg.HasExclude() {
				fmt.Println("\nExcluded fields:")
				for _, path := range cfg.Exclude {
					fmt.Printf("  ✗ %s\n", path)
				}
			}
		}

		fmt.Println()
		return nil
	},
}

// ── serve aliases ─────────────────────────────────────────────────────────────

var serveAliasesCmd = &cobra.Command{
	Use:   "aliases",
	Short: "List serve aliases in a Katalog",
	Long: `List serve aliases across all serve-enabled CRDs, or for a specific CRD.

Aliases are additional named entry points for a CRD. Each alias can independently
override token permissions and response configuration, falling back to CRD-level
defaults when not set.

With --target, --kind, or --name, shows aliases for that specific CRD only.

Examples:
  ork serve aliases
  ork serve aliases --target smartapp`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, _ := cmd.Flags().GetString("target")
		kind, _ := cmd.Flags().GetString("kind")
		name, _ := cmd.Flags().GetString("name")
		alias, _ := cmd.Flags().GetString("alias")

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		// ── Specific CRD ────────────────────────────────────────────────────
		if target != "" || kind != "" || name != "" || alias != "" {
			var crd *orktypes.CRDEntry

			if alias != "" && target == "" && kind == "" && name == "" {
				// Resolve by alias — show all aliases for the same CRD.
				crd, _, err = resolveCRDByAnyTarget(k, alias)
				if err != nil {
					return err
				}
			} else {
				crd, err = resolveCRD(k, target, kind, name)
				if err != nil {
					return err
				}
			}
			if !crd.ServeEnabled() {
				return fmt.Errorf("%s CRD %q is not Serve-enabled", failureMark(), crd.Name)
			}
			printAliasesForCRD(crd)
			return nil
		}

		// ── All CRDs ────────────────────────────────────────────────────────
		crds := k.ServeEnabledCRDs()
		sort.Slice(crds, func(i, j int) bool { return crds[i].Name < crds[j].Name })

		var total int
		fmt.Printf("\nServe Aliases\n")
		fmt.Printf("%s\n", strings.Repeat("─", 60))

		for _, crd := range crds {
			if crd == nil || !crd.HasServeAliases() {
				continue
			}
			printAliasesForCRD(crd)
			total += len(crd.ServeAliases())
		}

		if total == 0 {
			fmt.Println("\nNo serve aliases found.")
		} else {
			fmt.Printf("\n%d alias(es) across %d CRD(s)\n", total, len(crds))
		}
		fmt.Println()
		return nil
	},
}

// ── init ────────────────────────────────────────────────────────────────────

func init() {
	// Validate
	serveValidateCmd.Flags().Bool("full", false, "Show a detailed breakdown of the Serve configuration")
	serveCmd.AddCommand(serveValidateCmd)

	// Schema
	serveSchemaCmd.Flags().StringP("target", "t", "", "Target to show schema for")
	serveSchemaCmd.Flags().StringP("kind", "k", "", "Kind to show schema for")
	serveSchemaCmd.Flags().StringP("name", "n", "", "CRD name to show schema for")
	serveSchemaCmd.Flags().StringP("alias", "a", "", "Alias to show schema for")
	serveCmd.AddCommand(serveSchemaCmd)

	// Fields
	serveFieldsCmd.Flags().StringP("target", "t", "", "Target to show fields for")
	serveFieldsCmd.Flags().StringP("kind", "k", "", "Kind to show fields for")
	serveFieldsCmd.Flags().StringP("name", "n", "", "CRD name to show fields for")
	serveFieldsCmd.Flags().StringP("alias", "a", "", "Alias to show fields for")
	serveFieldsCmd.Flags().String("sort-by", "name", "Sort fields by 'name' (default) or 'order'")
	serveCmd.AddCommand(serveFieldsCmd)

	// Tokens
	serveTokensCmd.Flags().StringP("target", "t", "", "Target to show tokens for")
	serveTokensCmd.Flags().StringP("kind", "k", "", "Kind to show tokens for")
	serveTokensCmd.Flags().StringP("name", "n", "", "CRD name to show tokens for")
	serveTokensCmd.Flags().StringP("alias", "a", "", "Alias to show effective tokens for (can be used instead of --target)")
	serveCmd.AddCommand(serveTokensCmd)

	// Targets
	serveCmd.AddCommand(serveTargetsCmd)

	// CanI
	serveCanICmd.Flags().StringP("token", "T", "", "Token name to check")
	serveCanICmd.Flags().StringP("target", "t", "", "Target or alias to check")
	serveCanICmd.Flags().StringP("kind", "k", "", "Kind to check")
	serveCanICmd.Flags().StringP("name", "n", "", "CRD name to check")
	serveCanICmd.Flags().StringP("operation", "o", "", "Operation to check ("+validServeOperations+")")
	serveCanICmd.Flags().StringP("namespace", "N", "", "Namespace to check (default: all namespaces)")
	serveCanICmd.Flags().StringP("class", "c", "resources", "Endpoint class to check (resources, schema)")
	serveCanICmd.Flags().StringP("alias", "a", "", "Alias to check permissions for (overrides alias resolved from --target)")
	serveCmd.AddCommand(serveCanICmd)

	// Aliases
	serveAliasesCmd.Flags().StringP("target", "t", "", "Target to show aliases for")
	serveAliasesCmd.Flags().StringP("kind", "k", "", "Kind to show aliases for")
	serveAliasesCmd.Flags().StringP("name", "n", "", "CRD name to show aliases for")
	serveAliasesCmd.Flags().StringP("alias", "a", "", "Alias name — shows all aliases for the same CRD")
	serveCmd.AddCommand(serveAliasesCmd)

	// Response
	serveResponseCmd.Flags().StringP("target", "t", "", "Target to show response for")
	serveResponseCmd.Flags().StringP("kind", "k", "", "Kind to show response for")
	serveResponseCmd.Flags().StringP("name", "n", "", "CRD name to show response for")
	serveResponseCmd.Flags().StringP("alias", "a", "", "Alias to show effective response config for (can be used instead of --target)")
	serveResponseCmd.Flags().BoolP("preview", "p", false, "Show a sample response preview")
	serveCmd.AddCommand(serveResponseCmd)

	rootCmd.AddCommand(serveCmd)

	// Shadow global flags
	shadowGlobalCommandFlags(serveCmd)
}
