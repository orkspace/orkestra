//go:build !runtime && !gateway

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/katalog/pipeline"
	"github.com/orkspace/orkestra/pkg/merger"
	orke2e "github.com/orkspace/orkestra/pkg/registry/e2e"
	"github.com/orkspace/orkestra/pkg/registry/simulate"
	"github.com/orkspace/orkestra/pkg/tools/devserver"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

// cliSimulateOptions groups the CLI-level flags that flow through all simulate helpers.
type cliSimulateOptions struct {
	CRDName      string
	MaxCycles    int
	Target       string
	SkipExternal bool
	DebugOps     bool
	UseEnvtest   bool
	K8sVersion   string
}

var simulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Simulate operator reconciliation in memory — no cluster required",
	Long: `Runs the operator reconcile loop against a fake in-memory cluster.
Shows resource creation and state transitions across reconcile cycles.

The recommended entry point is simulate.yaml — it records what your operator
should produce so the run is repeatable and verifiable:

  ork simulate                              # simulate.yaml auto-detected
  ork simulate -f simulate.yaml             # explicit — assert mode when expect: is set
  ork simulate -f katalog.yaml --cr cr.yaml # direct flags; op-print only
  ork simulate ./...                        # discovers all simulate.yaml files recursively`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cliOpts := cliSimulateOptions{}
		cliOpts.CRDName, _ = cmd.Flags().GetString("crd")
		cliOpts.MaxCycles, _ = cmd.Flags().GetInt("cycles")
		cliOpts.Target, _ = cmd.Flags().GetString("target")
		cliOpts.SkipExternal, _ = cmd.Flags().GetBool("skip-external")
		cliOpts.DebugOps, _ = cmd.Flags().GetBool("debug-ops")
		cliOpts.UseEnvtest, _ = cmd.Flags().GetBool("envtest")
		cliOpts.K8sVersion, _ = cmd.Flags().GetString("k8s-version")

		if devServer, _ := cmd.Flags().GetBool("dev-server"); devServer {
			devServerPort, _ := cmd.Flags().GetInt("dev-server-port")
			if err := devserver.Start(devServerPort); err != nil {
				return fmt.Errorf("starting dev server: %w", err)
			}
		}

		// Discovery mode: ork simulate ./...
		if len(args) > 0 && args[0] == "./..." {
			skipRaw, _ := cmd.Flags().GetStringSlice("skip")
			return runSimulateDiscovery(cmd.Context(), ".", skipRaw, cliOpts)
		}

		katalogFile, _ := cmd.Flags().GetString("file")
		if katalogFile == "" {
			// Auto-detect: simulate.yaml → katalog.yaml/komposer.yaml
			switch {
			case fileExists(fileSimulate):
				katalogFile = fileSimulate
			default:
				if d := defaultFilePaths(); len(d) > 0 {
					katalogFile = d[0]
				}
			}
		}
		if katalogFile == "" {
			return fmt.Errorf(errNoKatalog)
		}

		// Simulate kind: assert mode
		if isSimulateDoc(katalogFile) {
			return runSimulateFromSpec(cmd.Context(), katalogFile, cliOpts)
		}

		// Reject E2E files with a clear message
		if isE2EDoc(katalogFile) {
			return fmt.Errorf("%s is an E2E file — use 'ork e2e' for cluster testing, or run 'ork simulate init' to generate a simulate.yaml", katalogFile)
		}

		crFile, _ := cmd.Flags().GetString("cr")
		if crFile == "" {
			crFile = fileCr
		}
		if crFile == "" {
			return fmt.Errorf("--cr is required")
		}

		return runSimulate(cmd.Context(), katalogFile, crFile, cliOpts)
	},
}

func runSimulate(ctx context.Context, katalogFile, crFile string, cliOpts cliSimulateOptions) error {
	maxCycles := cliOpts.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 10
	}

	m := merger.New(katalogFile)
	if err := m.Merge(); err != nil {
		return fmt.Errorf("merging Katalog: %w", err)
	}
	kat, err := pipeline.BuildExpanded(kfg, m)
	if err != nil {
		var typedErr *katalog.TypedOperatorError
		if errors.As(err, &typedErr) {
			printTypedOperatorHint(typedErr, "ork simulate")
		}
		return fmt.Errorf("parsing Katalog: %w", err)
	}

	crData, err := readLocal(crFile)
	if err != nil {
		return fmt.Errorf("reading CR: %w", err)
	}

	// Parse all documents; key by lowercase kind so each CRD gets its own CR.
	crs := parseMultiDocCRs(crData)
	if len(crs) == 0 {
		return fmt.Errorf("reading CR: no valid documents found in %s", crFile)
	}

	// If --crd is given, simulate that CRD only. Otherwise simulate all.
	var targets []string
	if cliOpts.CRDName != "" {
		targets = []string{cliOpts.CRDName}
	} else {
		targets = kat.CRDNames()
	}

	baseOpts := simulate.RunOptions{SkipExternal: cliOpts.SkipExternal, Target: cliOpts.Target}

	for _, name := range targets {
		crdEntry, ok := kat.CRDEntry(name)
		if !ok {
			continue
		}
		in, ok := resolveCRInputs(crs, crdEntry.APITypes.Kind)
		if !ok {
			if len(targets) > 1 {
				fmt.Printf("  %s no CR found for %s — skipped\n\n", dim("note:"), crdEntry.APITypes.Kind)
				continue
			}
			return fmt.Errorf("no CR found for CRD %q (kind: %s) in %s", name, crdEntry.APITypes.Kind, crFile)
		}
		crdOpts := baseOpts
		crdOpts.Peers = in.peers
		crdOpts.ExistingInstances = in.existing
		if err := simulateOne(ctx, kat, name, in.cr, maxCycles, crdOpts, cliOpts, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func simulateOne(ctx context.Context, kat *katalog.Katalog, crdName string, cr *unstructured.Unstructured, maxCycles int, opts simulate.RunOptions, cliOpts cliSimulateOptions, crdPaths []string, expect *orktypes.SimulateExpect) error {
	fmt.Printf("Simulating %s/%s\n", crdName, cr.GetName())

	// Emit notes for operatorBox blocks that cannot execute in the fake cluster.
	crdEntry, _ := kat.CRDEntry(crdName)
	if crdEntry.OperatorBox.OnReconcile != nil && len(crdEntry.OperatorBox.OnReconcile.External) > 0 {
		if opts.SkipExternal {
			fmt.Printf("  %s external: calls stubbed — result fields will be empty\n", dim("note:"))
		} else {
			fmt.Printf("  %s external: calls will hit the real network (pass --skip-external to stub)\n", dim("note:"))
		}
	}
	if len(crdEntry.OperatorBox.Cross) > 0 && len(opts.Peers) == 0 {
		fmt.Printf("  %s cross: peer CRs not provided — cross.* fields will be empty (add sibling CRs to the CR file)\n", dim("note:"))
	}
	printSimulateAutoscaleSummary(crdEntry)
	fmt.Println()

	spin := StartSpinner(fmt.Sprintf("Running %d cycles...", maxCycles))
	start := time.Now()
	var result *simulate.Result
	var err error
	if cliOpts.UseEnvtest {
		if len(crdPaths) == 0 {
			return fmt.Errorf("%s --envtest requires spec.crd or spec.crdFiles to be set", failureMark())
		}
		result, err = simulate.RunWithEnvtest(ctx, kat, crdName, cr, maxCycles, opts, crdPaths, cliOpts.K8sVersion)
	} else {
		result, err = simulate.Run(ctx, kat, crdName, cr, maxCycles, opts)
	}
	if err != nil {
		spin.Failure()
		fmt.Printf("\n  %s %v\n", red("error:"), err)
		return err
	}
	spin.Stop()
	elapsed := time.Since(start)

	if cliOpts.DebugOps {
		fmt.Printf("  [debug-ops] %d total ops recorded across all cycles:\n", len(result.AllOps))
		for _, op := range result.AllOps {
			fmt.Printf("  [debug-ops]   cycle=%-2d  verb=%-8s  resource=%-20s  name=%s\n",
				op.Cycle, op.Verb, op.Resource, op.Name)
		}
		fmt.Println()
	}

	for _, note := range result.Notes {
		fmt.Printf("  %s %s\n", dim("note:"), note)
	}
	if len(result.Notes) > 0 {
		fmt.Println()
	}

	var prevKey string
	var repeatStart int
	flush := func(upTo int) {
		if repeatStart > 0 && upTo > repeatStart {
			fmt.Printf("  %s\n", gray(fmt.Sprintf("(cycles %d–%d: identical)", repeatStart, upTo)))
			repeatStart = 0
		}
	}

	for _, cycle := range result.Cycles {
		meaningful := filterOps(cycle.Ops, "create", "update", "delete", "patch", "apply")
		if len(meaningful) == 0 && cycle.Error == nil {
			continue
		}

		key := opsKey(meaningful)
		if key == prevKey && cycle.Error == nil {
			if repeatStart == 0 {
				repeatStart = cycle.Cycle
			}
			continue
		}
		flush(cycle.Cycle - 1)
		prevKey = key

		fmt.Printf("  Cycle %d:\n", cycle.Cycle)
		printCycleOps(meaningful)
		if cycle.Error != nil {
			fmt.Printf("    %s %v\n", failureMark(), cycle.Error)
		}
	}
	flush(result.Cycles[len(result.Cycles)-1].Cycle)

	if result.Steady {
		fmt.Printf("\n  %s Steady state at cycle %d in %s\n\n", successMark(), result.SteadyAt, elapsed.Round(time.Millisecond))
	} else {
		fmt.Printf("\n  ~ Max cycles reached (%d) in %s\n\n", maxCycles, elapsed.Round(time.Millisecond))
	}

	for _, c := range result.Cycles {
		if c.Error != nil {
			return fmt.Errorf("simulation completed with cycle errors")
		}
	}

	if expect != nil {
		errs := simulate.Assert(result, expect)
		printAssertions(errs, expect)
		if len(errs) > 0 {
			return fmt.Errorf("assertions failed (%d/%d passed)", len(expect.Ops)+boolInt(expect.Steady != nil)+boolInt(expect.NoErrors)-len(errs), len(expect.Ops)+boolInt(expect.Steady != nil)+boolInt(expect.NoErrors))
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func printAssertions(errs []simulate.AssertionError, expect *orktypes.SimulateExpect) {
	failSet := map[string]bool{}
	for _, e := range errs {
		failSet[e.Field] = true
	}

	if expect.Steady != nil {
		if failSet["steady"] {
			fmt.Printf("  %s steady\n", failureMark())
		} else {
			fmt.Printf("  %s steady\n", successMark())
		}
	}
	if expect.SteadyAt != nil {
		field := "steadyAt"
		if failSet[field] {
			fmt.Printf("  %s steadyAt ≤%d\n", failureMark(), *expect.SteadyAt)
		} else {
			fmt.Printf("  %s steadyAt ≤%d\n", successMark(), *expect.SteadyAt)
		}
	}
	if expect.NoErrors {
		hasErr := false
		for f := range failSet {
			if len(f) > 6 && f[:6] == "cycles" {
				hasErr = true
				break
			}
		}
		if hasErr {
			fmt.Printf("  %s noErrors\n", failureMark())
		} else {
			fmt.Printf("  %s noErrors\n", successMark())
		}
	}
	for i, rule := range expect.Ops {
		field := fmt.Sprintf("ops[%d]", i)
		desc := rule.Verb + " " + rule.Resource
		if rule.Name != "" {
			desc += "/" + rule.Name
		}
		desc += fmt.Sprintf(" (cycle %d)", rule.Cycle)
		if failSet[field] {
			fmt.Printf("  %s %s\n", failureMark(), desc)
		} else {
			fmt.Printf("  %s %s\n", successMark(), desc)
		}
	}
	for i, rule := range expect.Absent {
		field := fmt.Sprintf("absent[%d]", i)
		desc := "absent: " + rule.Verb + " " + rule.Resource
		if rule.Name != "" {
			desc += "/" + rule.Name
		}
		desc += fmt.Sprintf(" (cycle %d)", rule.Cycle)
		if failSet[field] {
			fmt.Printf("  %s %s\n", failureMark(), desc)
		} else {
			fmt.Printf("  %s %s\n", successMark(), desc)
		}
	}
	fmt.Println()

	if len(errs) > 0 {
		fmt.Printf("  %s\n\n", red("FAIL"))
	} else {
		fmt.Printf("  %s\n\n", green("PASS"))
	}
}

// printSimulateAutoscaleSummary prints a one-line autoscale marker for each workload
// that declares autoscale:, so the policy is visible in simulate output.
func printSimulateAutoscaleSummary(entry orktypes.CRDEntry) {
	type workload struct {
		name      string
		autoscale *orktypes.WorkloadAutoscale
	}
	var workloads []workload
	for _, ht := range []*orktypes.HookTemplates{entry.OperatorBox.OnCreate, entry.OperatorBox.OnReconcile} {
		if ht == nil {
			continue
		}
		for _, d := range ht.Deployments {
			if d.Autoscale != nil {
				workloads = append(workloads, workload{d.Name, d.Autoscale})
			}
		}
		for _, s := range ht.StatefulSets {
			if s.Autoscale != nil {
				workloads = append(workloads, workload{s.Name, s.Autoscale})
			}
		}
		for _, r := range ht.ReplicaSets {
			if r.Autoscale != nil {
				workloads = append(workloads, workload{r.Name, r.Autoscale})
			}
		}
	}
	for _, w := range workloads {
		a := w.autoscale
		min := int32(0)
		if a.Min != nil {
			min = *a.Min
		}
		cooldown := a.EffectiveCooldown().Duration.String()
		fmt.Printf("  %s %s  %s\n", dim("autoscale:"), gray(w.name),
			gray(fmt.Sprintf("min=%d max=%d cooldown=%s", min, a.Max, cooldown)))
	}
}

// printCycleOps prints one cycle's ops, coalescing duplicate resource+name entries.
// If a resource is both created and updated in the same cycle (e.g. reconcile: true),
// only the create icon (+) is shown.
func printCycleOps(ops []simulate.Op) {
	type entry struct {
		resource  string
		name      string
		hasCreate bool
		hasDelete bool
	}
	seen := map[string]*entry{}
	var order []string
	for _, op := range ops {
		key := op.Resource + "/" + op.Name
		if _, ok := seen[key]; !ok {
			seen[key] = &entry{resource: op.Resource, name: op.Name}
			order = append(order, key)
		}
		switch op.Verb {
		case "create":
			seen[key].hasCreate = true
		case "delete":
			seen[key].hasDelete = true
		case "apply":
			// SSA ops display as ~ (changed), same as update/patch
		}
	}
	for _, key := range order {
		e := seen[key]
		var icon string
		switch {
		case e.hasCreate:
			icon = iconAdded()
		case e.hasDelete:
			icon = iconRemoved()
		default:
			icon = iconChanged()
		}
		fmt.Printf("    %s %s/%s\n", icon, e.resource, e.name)
	}
}

// opsKey returns a stable string key for a slice of ops, used to detect identical cycles.
func opsKey(ops []simulate.Op) string {
	var b strings.Builder
	for _, op := range ops {
		b.WriteString(op.Verb)
		b.WriteByte('/')
		b.WriteString(op.Resource)
		b.WriteByte('/')
		b.WriteString(op.Name)
		b.WriteByte('|')
	}
	return b.String()
}

func filterOps(ops []simulate.Op, verbs ...string) []simulate.Op {
	verbSet := make(map[string]bool)
	for _, v := range verbs {
		verbSet[v] = true
	}
	var result []simulate.Op
	for _, op := range ops {
		if verbSet[op.Verb] {
			result = append(result, op)
		}
	}
	return result
}

// parseMultiDocCRs splits a YAML file on document separators and returns all
// valid CR documents grouped by lowercase kind, in file order. Supports
// single- and multi-doc CR files (multiple CRs separated by ---).
//
// Multiple documents of the SAME kind are all kept (not last-one-wins) —
// see resolveCRInputs for how the extras are used.
func parseMultiDocCRs(data []byte) map[string][]*unstructured.Unstructured {
	crs := map[string][]*unstructured.Unstructured{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		if err := dec.Decode(&node); err != nil {
			break // EOF or parse error
		}
		// YAML → JSON → Unstructured: go-yaml can produce non-JSON types
		// (!!timestamp, int64, map[interface{}]interface{}) that Unstructured's
		// map[string]interface{} does not accept. YAMLToJSON normalises them to
		// JSON-compatible types before json.Unmarshal populates the object.
		b, err := yaml.Marshal(&node)
		if err != nil {
			continue
		}
		j, err := sigsyaml.YAMLToJSON(b)
		if err != nil {
			continue
		}
		var cr unstructured.Unstructured
		if err := json.Unmarshal(j, &cr.Object); err != nil {
			continue
		}
		if kind := cr.GetKind(); kind != "" {
			key := strings.ToLower(kind)
			crs[key] = append(crs[key], &cr)
		}
	}
	return crs
}

// crSimInputs is one target CRD's resolved simulation inputs, derived from a
// multi-doc CR file by resolveCRInputs.
type crSimInputs struct {
	cr       *unstructured.Unstructured            // the CR under test — reconciled
	peers    map[string]*unstructured.Unstructured // other kinds, first doc each — for cross:
	existing []*unstructured.Unstructured          // same kind, docs after the first — NOT reconciled
}

// resolveCRInputs picks the CR to reconcile for crdEntry's kind out of a
// multi-doc CR file, plus its simulation context.
//
// The FIRST document of the target kind is the CR under test — unchanged
// behavior from a single-CR file. Any FURTHER documents of that same kind
// are pre-existing instances: seeded into the fake dynamic client so
// reconcile-time checks that list other instances (operator: unique) can
// see them, but never reconciled themselves — same convention already used
// for cross: peers, extended from "one doc per other kind" to "one or more
// docs of the CRD's own kind". Documents of OTHER kinds become cross: peers,
// one per kind (first document wins), exactly as before.
func resolveCRInputs(crs map[string][]*unstructured.Unstructured, kind string) (crSimInputs, bool) {
	kindKey := strings.ToLower(kind)
	docs, ok := crs[kindKey]
	if !ok || len(docs) == 0 {
		return crSimInputs{}, false
	}
	out := crSimInputs{cr: docs[0], existing: docs[1:]}
	if len(crs) > 1 {
		out.peers = make(map[string]*unstructured.Unstructured, len(crs)-1)
		for k, v := range crs {
			if k != kindKey && len(v) > 0 {
				out.peers[k] = v[0]
			}
		}
	}
	return out, true
}

// ── Simulate kind entry points ─────────────────────────────────────────────────

// isSimulateDoc returns true when the file's kind is "Simulate".
func isSimulateDoc(path string) bool {
	data, err := readLocal(path)
	if err != nil {
		return false
	}
	var head struct {
		Kind string `yaml:"kind"`
	}
	_ = yaml.Unmarshal(data, &head)
	return head.Kind == "Simulate"
}

// runSimulateFromSpec loads a simulate.yaml and runs it in assert mode.
// Aggregator form (imports, no spec) expands each imported file in order.
func runSimulateFromSpec(ctx context.Context, path string, cliOpts cliSimulateOptions) error {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}

	data, err := readLocal(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var doc orktypes.Simulate
	if err := strictUnmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing %s:\n%s", path, err)
	}

	dir := filepath.Dir(path)

	// Aggregator: imports but no spec.
	if doc.Spec == nil && len(doc.Imports) > 0 {
		for _, imp := range doc.Imports {
			impPath := imp
			if !filepath.IsAbs(impPath) {
				impPath = filepath.Join(dir, impPath)
			}
			if err := runSimulateFromSpec(ctx, impPath, cliOpts); err != nil {
				return err
			}
		}
		return nil
	}

	if doc.Spec == nil {
		return fmt.Errorf("%s: missing spec", path)
	}

	if err := validateSimulateFileQuiet(path); err != nil {
		return err
	}

	if err := orktypes.ExpandSimulateOpsIncludes(doc.Spec.Expect, dir); err != nil {
		return fmt.Errorf("expanding simulate ops includes in %s: %w", path, err)
	}

	cycles := doc.Spec.Cycles
	if cycles <= 0 {
		cycles = cliOpts.MaxCycles
	}

	// CLI flag wins over spec field for both target and skipExternal.
	effectiveTarget := cliOpts.Target
	if effectiveTarget == "" {
		effectiveTarget = doc.Spec.Target
	}
	opts := simulate.RunOptions{
		SkipExternal: cliOpts.SkipExternal || doc.Spec.SkipExternal,
		Target:       effectiveTarget,
	}

	katalogPath := filepath.Join(dir, doc.Spec.Katalog)

	// Resolve CRD paths (for --envtest) relative to the simulate.yaml directory.
	crdPaths := make([]string, 0, len(doc.Spec.AllCRDPaths()))
	for _, p := range doc.Spec.AllCRDPaths() {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		crdPaths = append(crdPaths, abs)
	}

	m := merger.New(katalogPath)
	if err := m.Merge(); err != nil {
		return fmt.Errorf("merging Katalog: %w", err)
	}

	kat, err := pipeline.BuildExpanded(kfg, m)
	if err != nil {
		var typedErr *katalog.TypedOperatorError
		if errors.As(err, &typedErr) {
			printTypedOperatorHint(typedErr, "ork simulate")
		}
		return fmt.Errorf("parsing Katalog: %w", err)
	}

	// Read all CR files (cr: + crFiles:) and concatenate for multi-doc parsing.
	var crBuf []byte
	for _, p := range doc.Spec.AllCRPaths() {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		data, err := readLocal(abs)
		if err != nil {
			return fmt.Errorf("reading CR %s: %w", abs, err)
		}
		if len(crBuf) > 0 {
			crBuf = append(crBuf, '\n')
		}
		crBuf = append(crBuf, data...)
	}
	if len(crBuf) == 0 {
		return fmt.Errorf("%s: spec.cr or spec.crFiles is required", path)
	}
	crs := parseMultiDocCRs(crBuf)
	if len(crs) == 0 {
		return fmt.Errorf("no valid CR documents in CR file(s)")
	}

	var targets []string
	if cliOpts.CRDName != "" {
		targets = []string{cliOpts.CRDName}
	} else {
		targets = kat.CRDNames()
	}

	var failed []string
	for _, name := range targets {
		crdEntry, ok := kat.CRDEntry(name)
		if !ok {
			continue
		}
		in, ok := resolveCRInputs(crs, crdEntry.APITypes.Kind)
		if !ok {
			if len(targets) > 1 {
				continue
			}
			return fmt.Errorf("no CR found for CRD %q (kind: %s)", name, crdEntry.APITypes.Kind)
		}
		crdOpts := opts
		crdOpts.Peers = in.peers
		crdOpts.ExistingInstances = in.existing
		expect := simulate.ExpectForCRD(doc.Spec.Expect, name)
		if err := simulateOne(ctx, kat, name, in.cr, cycles, crdOpts, cliOpts, crdPaths, expect); err != nil {
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("simulate failed for: %s", strings.Join(failed, ", "))
	}
	return nil
}

// isE2EDoc returns true when the file's kind is "E2E".
func isE2EDoc(path string) bool {
	data, err := readLocal(path)
	if err != nil {
		return false
	}
	var head struct {
		Kind string `yaml:"kind"`
	}
	_ = yaml.Unmarshal(data, &head)
	return head.Kind == "E2E"
}

// ── Discovery mode ─────────────────────────────────────────────────────────────

type simulateFileResult struct {
	path      string
	skipped   bool
	skipMsg   string
	steady    bool
	cycle     int
	elapsed   time.Duration
	cycleErrs bool
}

// runSimulateDiscovery finds all simulate.yaml files under root, simulates each,
// and prints an aggregate summary.
func runSimulateDiscovery(ctx context.Context, root string, skip []string, cliOpts cliSimulateOptions) error {
	var patterns []string
	for _, s := range skip {
		patterns = append(patterns, s)
	}
	paths, err := orke2e.DiscoverSimulateFiles(root, patterns)
	if err != nil {
		return fmt.Errorf("discovering simulate files: %w", err)
	}
	if len(paths) == 0 {
		fmt.Printf("no simulate.yaml files found under %s\n", root)
		return nil
	}

	fmt.Printf("Simulating %d file(s) under %s\n\n", len(paths), root)

	absRoot, _ := filepath.Abs(root)

	// In discovery mode each file declares its own target; don't let a CLI
	// --target flag override every file in the suite.
	fileOpts := cliOpts
	fileOpts.Target = ""

	var results []simulateFileResult
	for _, p := range paths {
		rel, _ := filepath.Rel(absRoot, p)

		start := time.Now()
		err := runSimulateFromSpec(ctx, p, fileOpts)
		elapsed := time.Since(start)

		var res simulateFileResult
		res.path = rel
		res.elapsed = elapsed
		if err != nil {
			fmt.Printf("  %-55s %s  %s\n", rel, red("✗ "+err.Error()), dim("[assert]"))
			res.cycleErrs = true
		} else {
			fmt.Printf("  %-55s %s  %s\n", rel, green(fmt.Sprintf("✓ passed (%s)", elapsed.Round(time.Millisecond))), dim("[assert]"))
			res.steady = true
		}
		results = append(results, res)
	}

	// Aggregate summary
	var simulated, skipped int
	var slowest simulateFileResult
	for _, r := range results {
		if r.skipped {
			skipped++
		} else {
			simulated++
			if r.elapsed > slowest.elapsed {
				slowest = r
			}
		}
	}
	fmt.Printf("\n  %d file(s) — %d simulated, %d skipped\n", len(results), simulated, skipped)
	if slowest.path != "" {
		fmt.Printf("  Slowest: %s (%s)\n", slowest.path, slowest.elapsed.Round(time.Millisecond))
	}

	var errFiles []simulateFileResult
	for _, r := range results {
		if r.cycleErrs {
			errFiles = append(errFiles, r)
		}
	}
	if len(errFiles) > 0 {
		fmt.Printf("\n  %s — run directly for full output:\n", yellow("Files with errors"))
		for _, r := range errFiles {
			fmt.Printf("    ork simulate -f %s\n", r.path)
		}
		return fmt.Errorf("simulation failed in %d file(s)", len(errFiles))
	}

	return nil
}

// ── ork simulate init ──────────────────────────────────────────────────────────

type crdOps struct {
	name string
	ops  []simulate.Op
}

var simulateInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a simulate.yaml pre-filled with the observed cycle-1 ops",
	Long: `Runs the reconciler once and generates a simulate.yaml with the
observed cycle-1 create operations as expect: rules. Edit and refine from there.

  ork simulate init
  ork simulate init -f katalog.yaml --cr cr.yaml
  ork simulate init --force              # overwrite existing simulate.yaml
  ork simulate init --suite              # aggregate all simulate.yaml files under .
  ork simulate init --suite ./examples/  # aggregate under a specific dir`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if suite, _ := cmd.Flags().GetBool("suite"); suite {
			return simulateInitSuite(cmd, args)
		}

		katalogFile, _ := cmd.Flags().GetString("file")
		crFile, _ := cmd.Flags().GetString("cr")
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		var err error
		katalogFile, err = resolveKatalogFile(katalogFile)
		if err != nil {
			return err
		}
		if crFile == "" {
			crFile = filepath.Join(filepath.Dir(katalogFile), fileCr)
		} else if abs, err := filepath.Abs(crFile); err == nil {
			crFile = abs
		}

		kat, err := katalog.ParseFile(katalogFile)
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

		outPath := fileSimulate
		if !dryRun && !force {
			if fileExists(outPath) {
				return fmt.Errorf("%s already exists — use --force to overwrite", outPath)
			}
		}

		opts := simulate.RunOptions{}
		targets := kat.CRDNames()

		var results []crdOps

		for _, name := range targets {
			crdEntry, ok := kat.CRDEntry(name)
			if !ok {
				continue
			}
			in, ok := resolveCRInputs(crs, crdEntry.APITypes.Kind)
			if !ok {
				continue
			}
			crdOpts := opts
			crdOpts.Peers = in.peers
			crdOpts.ExistingInstances = in.existing
			result, err := simulate.Run(cmd.Context(), kat, name, in.cr, 10, crdOpts)
			if err != nil {
				return fmt.Errorf("simulating %s: %w", name, err)
			}
			var cycle1Creates []simulate.Op
			for _, op := range result.AllOps {
				if op.Cycle == 1 && op.Verb == "create" && op.Resource != "namespaces" {
					cycle1Creates = append(cycle1Creates, op)
				}
			}
			results = append(results, crdOps{name: name, ops: cycle1Creates})
		}

		// Store relative paths in the output so the generated file is portable.
		cwd, _ := os.Getwd()
		relKatalog := katalogFile
		relCR := crFile
		if r, err := filepath.Rel(cwd, katalogFile); err == nil {
			relKatalog = "./" + r
		}
		if r, err := filepath.Rel(cwd, crFile); err == nil {
			relCR = "./" + r
		}

		doc := generateSimulateDoc(relKatalog, relCR, kat.Metadata().Name, results)

		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(doc); err != nil {
			return fmt.Errorf("encoding simulate.yaml: %w", err)
		}

		output := injectAbsentComment(buf.Bytes(), results)
		output = append([]byte("# Schema reference: "+SchemaRefSimulate+"\n"), output...)

		if dryRun {
			fmt.Print(string(output))
			return nil
		}

		if err := os.WriteFile(outPath, output, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}

		fmt.Printf("%s Generated %s\n", successMark(), outPath)
		fmt.Printf("  %d CRD(s), %d op rule(s)\n", len(results), countRules(results))
		fmt.Printf("\n  Run %s to verify.\n", bold("ork simulate"))
		return nil
	},
}

// simulateInitSuite discovers all simulate.yaml leaf files under root, builds a
// pure aggregator, and writes (or prints) simulate.yaml in the current directory.
func simulateInitSuite(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	skipRaw, _ := cmd.Flags().GetStringSlice("skip")

	root := "."
	if len(args) > 0 {
		root = args[0]
	}

	var patterns []string
	for _, s := range skipRaw {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				patterns = append(patterns, p)
			}
		}
	}

	paths, err := orke2e.DiscoverSimulateFiles(root, patterns)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	if len(paths) == 0 {
		fmt.Printf("No simulate.yaml files found under %s\n", root)
		return nil
	}

	cwd, _ := os.Getwd()
	relPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := filepath.Rel(cwd, p)
		if err != nil {
			rel = p
		}
		relPaths = append(relPaths, "./"+rel)
	}

	outPath := fileSimulate
	if !dryRun && !force {
		if fileExists(outPath) {
			return fmt.Errorf("%s already exists — use --force to overwrite", outPath)
		}
	}

	type suiteMeta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description,omitempty"`
	}
	type suiteDoc struct {
		APIVersion string    `yaml:"apiVersion"`
		Kind       string    `yaml:"kind"`
		Metadata   suiteMeta `yaml:"metadata"`
		Imports    []string  `yaml:"imports"`
	}

	doc := suiteDoc{
		APIVersion: "orkestra.orkspace.io/v1",
		Kind:       "Simulate",
		Metadata: suiteMeta{
			Name:        "suite",
			Description: fmt.Sprintf("Generated by ork simulate init --suite — %d file(s) discovered", len(relPaths)),
		},
		Imports: relPaths,
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encoding suite: %w", err)
	}

	output := append([]byte("# Schema reference: "+SchemaRefSimulateSuite+"\n"), buf.Bytes()...)

	if dryRun {
		fmt.Print(string(output))
		return nil
	}

	if err := os.WriteFile(outPath, output, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	absRoot, _ := filepath.Abs(root)
	fmt.Printf("%s Generated %s\n", successMark(), outPath)
	fmt.Printf("  %d file(s) discovered under %s\n", len(relPaths), absRoot)
	const maxShow = 10
	for i, p := range relPaths {
		if i >= maxShow {
			fmt.Printf("  ... %d more\n", len(relPaths)-maxShow)
			break
		}
		fmt.Printf("    %s\n", dim(p))
	}
	fmt.Printf("\n  Run %s to verify.\n", bold("ork simulate"))
	return nil
}

// generateSimulateDoc builds a Simulate document from observed cycle-1 creates.
func generateSimulateDoc(katalogPath, crPath, katalogName string, results []crdOps) map[string]interface{} {
	trueVal := true

	makeOps := func(ops []simulate.Op) []map[string]interface{} {
		var rules []map[string]interface{}
		for _, op := range ops {
			rule := map[string]interface{}{
				"cycle":    1,
				"verb":     op.Verb,
				"resource": op.Resource,
			}
			if op.Name != "" {
				rule["name"] = op.Name
			}
			rules = append(rules, rule)
		}
		return rules
	}

	spec := map[string]interface{}{
		"katalog": katalogPath,
		"cr":      crPath,
		"cycles":  5,
	}

	if len(results) == 1 {
		expect := map[string]interface{}{
			"steady":   trueVal,
			"noErrors": trueVal,
		}
		if ops := makeOps(results[0].ops); len(ops) > 0 {
			expect["ops"] = ops
		}
		spec["expect"] = expect
	} else {
		crds := map[string]interface{}{}
		for _, r := range results {
			crdExpect := map[string]interface{}{
				"steady":   trueVal,
				"noErrors": trueVal,
			}
			if ops := makeOps(r.ops); len(ops) > 0 {
				crdExpect["ops"] = ops
			}
			crds[r.name] = crdExpect
		}
		spec["expect"] = map[string]interface{}{
			"noErrors": trueVal,
			"crds":     crds,
		}
	}

	return map[string]interface{}{
		"apiVersion": "orkestra.orkspace.io/v1",
		"kind":       "Simulate",
		"metadata": map[string]interface{}{
			"name":        katalogName + "-sim",
			"description": "Generated by ork simulate init — edit to refine",
		},
		"spec": spec,
	}
}

func countRules(results []crdOps) int {
	n := 0
	for _, r := range results {
		n += len(r.ops)
	}
	return n
}

// injectAbsentComment parses the encoded YAML, adds a HeadComment on every
// "steady" key hinting at the absent: block, then re-encodes with 2-space indent.
func injectAbsentComment(data []byte, results []crdOps) []byte {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return data
	}

	resource := "deployments"
	if len(results) > 0 && len(results[0].ops) > 0 {
		resource = results[0].ops[0].Resource
	}

	comment := "# absent:   # ops that must NOT appear — fill in for failure-path coverage\n" +
		"#   - cycle: 1\n" +
		"#     verb: create\n" +
		"#     resource: " + resource

	addHeadComment(&root, "steady", comment)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return data
	}
	return buf.Bytes()
}

// addHeadComment recursively sets HeadComment on every mapping key matching name.
func addHeadComment(node *yaml.Node, name, comment string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content)-1; i += 2 {
			if node.Content[i].Value == name {
				node.Content[i].HeadComment = comment
			}
		}
	}
	for _, child := range node.Content {
		addHeadComment(child, name, comment)
	}
}

func init() {
	rootCmd.AddCommand(simulateCmd)
	simulateCmd.AddCommand(simulateInitCmd)

	simulateInitCmd.Flags().StringP("file", "f", "", "Path to katalog.yaml or komposer.yaml")
	simulateInitCmd.Flags().String("cr", "", "Path to the CR YAML file")
	simulateInitCmd.Flags().Bool("force", false, "Overwrite existing simulate.yaml")
	simulateInitCmd.Flags().Bool("dry-run", false, "Print the generated simulate.yaml to stdout instead of writing the file")
	simulateInitCmd.Flags().Bool("suite", false, "Aggregate all simulate.yaml leaf files found under the given dir (default: .)")
	simulateInitCmd.Flags().StringSlice("skip", []string{}, "Comma-separated path patterns to exclude from suite discovery")

	simulateCmd.Flags().StringP("file", "f", "", "Path to katalog.yaml")
	simulateCmd.Flags().String("cr", "", "Path to the CR YAML file to simulate")
	simulateCmd.Flags().String("crd", "", "CRD name to simulate (default: all CRDs in Katalog)")
	simulateCmd.Flags().Int("cycles", 10, "Maximum number of reconcile cycles")
	simulateCmd.Flags().StringP("target", "t", "", "Target that provides the operatorbox for simulate")
	simulateCmd.Flags().StringSlice("skip", []string{}, "Comma-separated path patterns to skip during ./... discovery (e.g. vendor,cr-e2e.yaml)")
	simulateCmd.Flags().Bool("skip-external", false, "Stub external: HTTP calls with empty 200 responses instead of hitting the real network")
	simulateCmd.Flags().Bool("debug-ops", false, "Print every recorded op with its cycle number (diagnostic)")
	simulateCmd.Flags().Bool("dev-server", false, "Start the mock dev server for external: examples")
	simulateCmd.Flags().Int("dev-server-port", devserver.Port, "Port for the mock dev server")
	simulateCmd.Flags().Bool("envtest", false, "Run against a local kube-apiserver + etcd instead of fake clients (binaries auto-downloaded to ~/.ork/envtest-bins on first use)")
	simulateCmd.Flags().String("k8s-version", simulate.DefaultEnvtestK8sVersion, "Kubernetes version for envtest binaries (e.g. 1.31, 1.32); only used with --envtest")

	// Shadow global flags so they don't appear under `ork simulate`
	shadowGlobalCommandFlags(simulateCmd)
}
