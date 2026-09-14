//go:build !runtime && !gateway

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orkspace/orkestra/pkg/gateway/api"
	orktarget "github.com/orkspace/orkestra/pkg/intent/target"
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/merger"
	"github.com/orkspace/orkestra/pkg/registry/simulate"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// runCreateUpdateChain runs stages 1-6 of the play chain against an
// already-resolved intent map, printing each stage exactly like
// "ork serve play" does. Shared with "ork webhook play" — same engine,
// different front door: serve play reads its intent from a file, webhook
// play builds one from a source payload (push event, slash command, JSON
// body), then hands off here.
//
// source is stamped as the serve-source provenance annotation — "" for
// serve play (no caller identity beyond the token), or a webhook entry's
// own Name when called from webhook play, matching what ApplyTargetFields
// does for a real delivery. op is ServeOpCreate or ServeOpUpdate — webhook
// play always passes ServeOpCreate, since a delivered push/command has no
// --operation flag of its own.
func runCreateUpdateChain(k *katalog.Katalog, raw map[string]interface{}, tokenName, source string, op orktypes.ServeOperation) (*unstructured.Unstructured, *orktypes.CRDEntry, string, error) {
	target, _ := raw["target"].(string)
	if strings.TrimSpace(target) == "" {
		return nil, nil, "", fmt.Errorf(`"target" must be set in the intent`)
	}

	// Stage 1: Target resolution
	printStage(1, "Target resolution")
	resolution := k.LookupByTargetOrAlias(target)
	if resolution == nil {
		printStageError(fmt.Sprintf("unknown target %q\n  available: %s", target, strings.Join(k.AvailableTargets(), ", ")))
		return nil, nil, "", fmt.Errorf("target %q not found", target)
	}
	crd, alias := resolution.CRD, resolution.Alias
	printStageOK(fmt.Sprintf("kind=%s  target=%s  alias=%s", bold(crd.Kind()), green(crd.ServeTarget()), aliasOrNone(alias)))

	// Stage 2: Token check
	printStage(2, "Token check")
	allowed, denyReason := crd.TokenAllowedFor(alias, tokenName, string(op), "", orktypes.ServeClassResources)
	if !allowed {
		msg := denyReason.Message(tokenName, string(op), crd.Kind(), "")
		printStageError(msg)
		return nil, nil, "", fmt.Errorf("token %q denied: %s", tokenName, msg)
	}
	printStageOK(fmt.Sprintf("token %s can %s on %s", bold(tokenName), op, bold(crd.Kind())))

	// Stage 3: CR construction
	printStage(3, "CR construction")
	notes := k.Notes
	obj, err := orktarget.BuildCRFromTarget(raw, crd, notes)
	if err != nil {
		printStageError(err.Error())
		return nil, nil, "", err
	}
	if obj.GetName() == "" {
		msg := "name is required — declare serve.name on the CRD or include \"name\" in the intent"
		printStageError(msg)
		return nil, nil, "", fmt.Errorf("%s", msg)
	}
	printStageOK(fmt.Sprintf("name=%s  namespace=%s", bold(obj.GetName()), bold(obj.GetNamespace())))

	// Stage 4: Provenance — stamp first, then print the CR so annotations are visible
	printStage(4, "Provenance annotations")
	api.InjectProvenanceAnnotations(obj, crd.ServeTarget(), alias, source)
	api.InjectServeIntentAnnotation(obj, raw)
	ann := obj.GetAnnotations()
	keys := make([]string, 0, len(ann))
	for k := range ann {
		if strings.HasPrefix(k, "orkestra.") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		printStageDetail(fmt.Sprintf("%s: %s", gray(k), cyan(ann[k])))
	}
	printStageOK("provenance stamped — full CR:")
	printIndentedJSON(obj.Object)

	// Stage 5: Admission validation — same rules the webhook enforces at SSA time.
	// unique: operator is skipped (no live cluster); external: calls are skipped.
	printStage(5, "Admission validation")
	{
		resolver := orktmpl.NewResolverFromMap(obj.Object).WithUserNotes(k.Notes).WithRequest(raw)
		eval := resolver.TemplateEvaluator()
		r := evalAdmissionValidation(obj.Object, crd, resolver, eval)
		for _, v := range r.violations {
			if v.deny {
				printStageDetail(fmt.Sprintf("%s %s: %s", red("deny"), gray(v.field), red(v.message)))
			} else {
				printStageDetail(fmt.Sprintf("%s %s: %s", yellow("warn"), gray(v.field), yellow(v.message)))
			}
		}
		denied := r.denied()
		if denied > 0 {
			denialTxt := "denials"
			violationTxt := "violations"
			if denied == 1 {
				denialTxt = "denial"
				violationTxt = "violation"
			}

			printStageError(fmt.Sprintf("%d %s — this CR would be rejected at admission", denied, denialTxt))
			return nil, nil, "", fmt.Errorf("admission denied: %d rule %s", denied, violationTxt)
		}
		warned := r.warned()
		if warned > 0 {
			warningTxt := "warnings"
			if warned == 1 {
				warningTxt = "warning"
			}
			printStageOK(fmt.Sprintf("passed (%d %s)", warned, warningTxt))
		} else if r.total > 0 {
			printStageOK("passed — no violations")
		} else {
			printStageOK("no validation rules declared")
		}

		m := evalAdmissionMutation(obj.Object, crd, resolver, eval)
		for _, p := range m.previews {
			fromStr := gray("(absent)")
			if p.found {
				fromStr = gray(p.from)
			}
			printStageDetail(fmt.Sprintf("%s %s: %s → %s  %s", cyan("mut"), gray(p.field), fromStr, cyan(fmt.Sprintf("%v", p.to)), gray(p.mutType)))
		}
		if len(m.previews) > 0 {
			mutTxt := "mutations"
			if len(m.previews) == 1 {
				mutTxt = "mutation"
			}
			printStageOK(fmt.Sprintf("%d %s would be applied by the webhook", len(m.previews), mutTxt))
		}
	}

	// Stage 6: Response payload
	printStage(6, "Response payload")
	payload := api.EvaluatePayload(obj.Object, crd, alias, notes)
	if payload == nil {
		printStageDetail(gray("no serve.config.response declared — default CR response"))
	} else {
		printIndentedJSON(payload)
	}
	printStageOK("payload evaluated")

	return obj, crd, alias, nil
}

// playRunSimulate writes the built CR to a temp file and hands it to simulate.
// simulateConfig is the path to a simulate.yaml; empty means op-print mode with
// the same katalog play used. Shared by "ork serve play --simulate" and
// "ork webhook play --simulate" via playSimulate below.
func playRunSimulate(ctx context.Context, katalogFile string, obj *unstructured.Unstructured, simulateConfig string) error {
	b, err := yaml.Marshal(obj.Object)
	if err != nil {
		return fmt.Errorf("marshalling CR for simulate: %w", err)
	}
	tmp, err := os.CreateTemp("", "play-cr-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp CR file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp CR file: %w", err)
	}
	tmp.Close()

	fmt.Printf("%s  handoff to simulate\n", cyan("─────────────────────────────────────────────────────────"))
	fmt.Println()

	if simulateConfig != "" {
		return runSimulateWithCR(ctx, simulateConfig, tmp.Name())
	}
	return runSimulate(ctx, katalogFile, tmp.Name(), cliSimulateOptions{MaxCycles: 10, SkipExternal: true})
}

// runSimulateWithCR runs simulate using the spec file for katalog/cycles/expect
// but substitutes crFile (the play-built CR) instead of spec.cr.
// The expect: block is fully evaluated — this is assert mode, not op-print mode.
func runSimulateWithCR(ctx context.Context, specPath, crFile string) error {
	if abs, err := filepath.Abs(specPath); err == nil {
		specPath = abs
	}
	data, err := readLocal(specPath)
	if err != nil {
		return fmt.Errorf("reading simulate config %q: %w", specPath, err)
	}
	var doc orktypes.Simulate
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing simulate config %q: %w", specPath, err)
	}
	if doc.Spec == nil {
		return fmt.Errorf("simulate config %q: missing spec", specPath)
	}

	katalogPath := joinPath(specPath, doc.Spec.Katalog)
	cycles := doc.Spec.Cycles
	if cycles <= 0 {
		cycles = 10
	}
	opts := simulate.RunOptions{SkipExternal: doc.Spec.SkipExternal}

	m := merger.New(katalogPath)
	if err := m.Merge(); err != nil {
		return fmt.Errorf("merging Katalog: %w", err)
	}
	kat, err := pipeline.BuildExpanded(kfg, m)
	if err != nil {
		return fmt.Errorf("parsing Katalog: %w", err)
	}

	crData, err := readLocal(crFile)
	if err != nil {
		return fmt.Errorf("reading CR: %w", err)
	}
	crs := parseMultiDocCRs(crData)
	if len(crs) == 0 {
		return fmt.Errorf("no valid CR documents in %s", crFile)
	}

	var failed []string
	for _, name := range kat.CRDNames() {
		crdEntry, ok := kat.CRDEntry(name)
		if !ok {
			continue
		}
		in, ok := resolveCRInputs(crs, crdEntry.APITypes.Kind)
		if !ok {
			if len(kat.CRDNames()) > 1 {
				fmt.Printf("  %s no CR found for %s — skipped\n\n", dim("note:"), crdEntry.APITypes.Kind)
				continue
			}
			return fmt.Errorf("no CR found for CRD %q (kind: %s)", name, crdEntry.APITypes.Kind)
		}
		crdOpts := opts
		crdOpts.Peers = in.peers
		crdOpts.ExistingInstances = in.existing
		expect := simulate.ExpectForCRD(doc.Spec.Expect, name)
		if err := simulateOne(ctx, kat, name, in.cr, cycles, crdOpts, cliSimulateOptions{}, nil, expect); err != nil {
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("simulate failed for: %s", strings.Join(failed, ", "))
	}
	return nil
}

// runIntentPlay builds a Katalog from katalogPath, runs the play chain
// against intentFile without printing stage output, and returns the resolved
// target name on success.
func runIntentPlay(katalogPath, intentFile string) (string, error) {
	m := merger.New(katalogPath)
	if err := m.Merge(); err != nil {
		return "", fmt.Errorf("merging katalog: %w", err)
	}
	k, err := pipeline.BuildExpanded(kfg, m)
	if err != nil {
		return "", fmt.Errorf("building katalog: %w", err)
	}

	raw, err := readIntentFile(intentFile)
	if err != nil {
		return "", fmt.Errorf("reading intent file: %w", err)
	}

	target, _ := raw["target"].(string)
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf(`intent file must declare a "target"`)
	}

	resolution := k.LookupByTargetOrAlias(target)
	if resolution == nil {
		return target, fmt.Errorf("unknown target %q — available: %s", target, strings.Join(k.AvailableTargets(), ", "))
	}
	crd, alias := resolution.CRD, resolution.Alias

	if tokenName, _ := raw["token"].(string); tokenName != "" {
		allowed, denyReason := crd.TokenAllowedFor(alias, tokenName, string(orktypes.ServeOpCreate), "", orktypes.ServeClassResources)
		if !allowed {
			return target, fmt.Errorf("token %q denied: %s", tokenName, denyReason.Message(tokenName, string(orktypes.ServeOpCreate), crd.Kind(), ""))
		}
	} else {
		return target, fmt.Errorf("intent file must declare a 'token' — token: <name>")
	}

	obj, err := orktarget.BuildCRFromTarget(raw, crd, k.Notes)
	if err != nil {
		return target, fmt.Errorf("CR construction: %w", err)
	}

	resolver := orktmpl.NewResolverFromMap(obj.Object).WithUserNotes(k.Notes).WithRequest(raw)
	eval := resolver.TemplateEvaluator()
	r := evalAdmissionValidation(obj.Object, crd, resolver, eval)
	denied := r.denied()
	violationTxt := "violations"
	if denied > 0 {
		if denied == 1 {
			violationTxt = "violation"
		}
		return target, fmt.Errorf("admission denied: %d rule %s", denied, violationTxt)
	}

	return target, nil
}

// webhookSimulate carries the --simulate handoff arguments through
// "ork webhook play" to wherever a play chain successfully builds a CR —
// same handoff "ork serve play --simulate" does (playRunSimulate), just
// reached from a different front door, and potentially more than once per
// run: a GitHub/GitLab push can match several files, each its own build.
type webhookSimulate struct {
	ctx         context.Context
	katalogFile string
	enabled     bool
	config      string
}

// playSimulate hands obj to ork simulate when sim.enabled — a no-op
// otherwise. Called after every successful runCreateUpdateChain in
// "ork webhook play".
func playSimulate(sim webhookSimulate, obj *unstructured.Unstructured) error {
	if !sim.enabled {
		return nil
	}
	return playRunSimulate(sim.ctx, sim.katalogFile, obj, sim.config)
}
