//go:build !runtime && !gateway

package cli

// gate_dev.go — Local admission evaluator for dev builds.
//
// In a gateway build (//go:build gateway), ork gate starts the full gateway
// server (TLS, webhooks, cluster-required). In dev builds there is no gateway
// process to start, but operators still need to validate admission rules before
// deploying. This command fills that gap:
//
//	ork gate -f katalog.yaml --cr cr.yaml
//
// It evaluates validation.rules and previews mutation.rules in-process, using
// the same EvaluateConditions + EvaluateValidationRule logic as the webhook and
// reconciler. No cluster connection, no TLS, no webhook server.
//
// Limitations vs. the real webhook:
//   - operator: unique  — skipped; no informer cache without a cluster.
//   - external: calls   — skipped (both validation and mutation); no endpoint.
//   Both produce a note in the output so the user knows they weren't checked.

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/cmd/internal"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/merger"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

var gatewayCmd = &cobra.Command{
	Use:   "gate",
	Short: "Evaluate admission rules locally against a CR (no cluster required)",
	Long: `Evaluate admission rules locally against a CR.

Reads the validation.rules declared in the Katalog and runs them against the
provided CR using the same evaluation logic as the admission webhook and the
reconciler. No cluster, no TLS, no webhook server required.

Limitations:
  operator: unique  — skipped (no live informer cache)
  external: calls   — skipped (no real endpoint to call)
Both are noted in the output.

Example:
  ork gate -f katalog.yaml --cr cr.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		crFile, _ := cmd.Flags().GetString("cr")
		if crFile == "" {
			crFile = fileCr
		}

		paths, _ := cmd.Flags().GetStringSlice("file")
		if len(paths) == 0 {
			paths = defaultFilePaths()
		}
		if len(paths) == 0 {
			return fmt.Errorf(errNoKatalog)
		}

		m := merger.New(paths...)
		if err := m.Merge(); err != nil {
			return fmt.Errorf("merging Katalog: %w", err)
		}
		kat, err := pipeline.BuildExpanded(kfg, m)
		if err != nil {
			return fmt.Errorf("parsing Katalog: %w", err)
		}

		crData, err := readLocal(crFile)
		if err != nil {
			return fmt.Errorf("reading CR %q: %w", crFile, err)
		}
		crs := parseMultiDocCRs(crData)
		if len(crs) == 0 {
			return fmt.Errorf("no valid CR documents found in %s", crFile)
		}

		fmt.Println()
		fmt.Printf("%s  ork gate\n", bold("▶"))
		fmt.Printf("  %s %s\n", gray("cr:     "), cyan(crFile))
		fmt.Printf("  %s %s\n", gray("katalog:"), cyan(strings.Join(paths, ", ")))
		fmt.Println()

		var anyDenied bool
		for _, name := range kat.CRDNames() {
			crdEntry, ok := kat.CRDEntry(name)
			if !ok {
				continue
			}
			in, ok := resolveCRInputs(crs, crdEntry.APITypes.Kind)
			if !ok {
				continue
			}
			anyDenied = gateEvalCRD(kat, &crdEntry, in.cr.Object) || anyDenied
		}

		if anyDenied {
			return fmt.Errorf("admission denied")
		}
		return nil
	},
}

// gateEvalCRD evaluates validation rules and previews mutation rules for one
// CRD+CR pair. Returns true if any deny-action validation rule fired.
func gateEvalCRD(kat *katalog.Katalog, crd *orktypes.CRDEntry, obj map[string]interface{}) bool {
	fmt.Printf("%s  %s  %s\n", cyan("◆"), bold(crd.Kind()), gray(fmt.Sprintf("(%s)", crd.ServeTarget())))

	if !crd.HasValidationRules() && !crd.HasMutationRules() {
		fmt.Printf("  %s no admission rules declared\n\n", dim("note:"))
		return false
	}

	// Limitation notes — printed once for the CRD.
	hasUnique := hasUniqueRule(crd)
	hasValidationExternal := crd.Validation != nil && len(crd.Validation.AdmissionExternal()) > 0
	hasMutationExternal := crd.Mutation != nil && len(crd.Mutation.AdmissionExternal()) > 0
	if hasUnique {
		fmt.Printf("  %s operator: unique — skipped (no live cluster)\n", dim("note:"))
	}
	if hasValidationExternal || hasMutationExternal {
		fmt.Printf("  %s external: calls — skipped (no endpoint)\n", dim("note:"))
	}

	resolver := orktmpl.NewResolverFromMap(obj).WithUserNotes(kat.Notes)
	eval := resolver.TemplateEvaluator()

	// When mutateFirst is set the real webhook applies mutations before
	// validation runs. Mirror that here so local results match cluster behaviour.
	validationObj := obj
	var mutResult admissionMutationResult
	if crd.ShouldMutateFirst() && crd.HasMutationRules() {
		mutResult = evalAdmissionMutation(obj, crd, resolver, eval)
		if len(mutResult.previews) > 0 {
			validationObj = applyMutationPreviews(obj, mutResult.previews)
			validationResolver := orktmpl.NewResolverFromMap(validationObj).WithUserNotes(kat.Notes)
			resolver = validationResolver
			eval = resolver.TemplateEvaluator()
		}
	}

	// ── Validation rules ──────────────────────────────────────────────────────
	denied := gateValidate(validationObj, crd, resolver, eval)

	// ── Mutation rules ────────────────────────────────────────────────────────
	// If we already evaluated mutations for mutateFirst, print those results
	// directly rather than re-evaluating against the original object.
	if crd.ShouldMutateFirst() && crd.HasMutationRules() {
		printGateMutateResult(mutResult)
	} else {
		gateMutate(obj, crd, resolver, eval)
	}

	fmt.Println()
	return denied
}

func gateValidate(obj map[string]interface{}, crd *orktypes.CRDEntry, resolver *orktmpl.Resolver, eval orktypes.TemplateEvaluator) bool {
	if !crd.HasValidationRules() {
		return false
	}
	r := evalAdmissionValidation(obj, crd, resolver, eval)
	for _, v := range r.violations {
		if v.deny {
			fmt.Printf("  %s %s  %s\n", failureMark(), red(v.field), red(v.message))
		} else {
			fmt.Printf("  %s %s  %s\n", yellow("⚠"), yellow(v.field), yellow(v.message))
		}
	}
	denied, warned := r.denied(), r.warned()
	denialTxt, warningTxt := "denials", "warnings"
	if denied == 1 {
		denialTxt = "denial"
	}
	if warned == 1 {
		warningTxt = "warning"
	}
	if denied > 0 {
		fmt.Printf("\n  %s %d/%d validation rules passed · %s %d %s\n",
			failureMark(), r.passed, r.total, red("✗"), denied, red(denialTxt))
		return true
	}
	if warned > 0 {
		fmt.Printf("  %s %d/%d validation rules passed · %s %d %s\n",
			successMark(), r.passed, r.total, yellow("⚠"), warned, yellow(warningTxt))
		return false
	}
	fmt.Printf("  %s %d/%d validation rules passed\n", successMark(), r.passed, r.total)
	return false
}

func gateMutate(obj map[string]interface{}, crd *orktypes.CRDEntry, resolver *orktmpl.Resolver, eval orktypes.TemplateEvaluator) {
	if !crd.HasMutationRules() {
		return
	}
	printGateMutateResult(evalAdmissionMutation(obj, crd, resolver, eval))
}

func printGateMutateResult(r admissionMutationResult) {
	for _, p := range r.previews {
		fromStr := gray("(absent)")
		if p.found {
			fromStr = gray(p.from)
		}
		fmt.Printf("  %s %s  %s → %s  %s\n",
			cyan("↳"), cyan(p.field), fromStr, cyan(fmt.Sprintf("%v", p.to)), gray(p.mutType))
	}
	mutTxt := "mutations"
	if len(r.previews) == 1 {
		mutTxt = "mutation"
	}
	if len(r.previews) > 0 {
		fmt.Printf("  %s %d %s would be applied\n", successMark(), len(r.previews), mutTxt)
	} else {
		fmt.Printf("  %s no mutations apply\n", dim("note:"))
	}
}

func hasUniqueRule(crd *orktypes.CRDEntry) bool {
	if crd.Validation == nil {
		return false
	}
	for _, r := range crd.Validation.Rules {
		if r.Operator == orktypes.ConditionUnique {
			return true
		}
	}
	return false
}

var gateRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the gateway locally (HTTP only, no TLS, no admission webhooks)",
	Long: `Start the Orkestra Gateway in local HTTP mode.

The Gateway API (POST /api/v1/apply, GET /api/v1/resources/, intake webhooks)
runs on the health port (default :8080). Admission and conversion webhooks are
disabled — TLS and a live cluster are required for those.

Use this to test serve routing, apply flows, and intake payloads without a
Helm deployment.

Example:
  ork gate run -f katalog.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths, _ := cmd.Flags().GetStringSlice("file")
		if len(paths) == 0 {
			paths = defaultFilePaths()
		}
		if len(paths) == 0 {
			return fmt.Errorf(errNoKatalog)
		}

		m := merger.New(paths...)
		if err := m.Merge(); err != nil {
			return fmt.Errorf("merging katalogs: %w", err)
		}

		internal.KonductGatewayDev(kfg, m, ctx)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(gatewayCmd)
	gatewayCmd.AddCommand(gateRunCmd)
	gatewayCmd.Flags().StringSliceP("file", "f", nil, "Path(s) to katalog.yaml (repeatable)")
	gatewayCmd.Flags().String("cr", "", "CR file to evaluate (default: cr.yaml)")
	gateRunCmd.Flags().StringSliceP("file", "f", nil, "Path(s) to katalog.yaml (repeatable)")
	shadowGlobalCommandFlags(gatewayCmd)
	shadowGlobalCommandFlags(gateRunCmd)
}
