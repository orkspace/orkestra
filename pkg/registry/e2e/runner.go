// Package e2e implements the orchestration loop for `ork e2e`.
//
// A Runner executes a declarative E2E spec through its full lifecycle:
//
//  1. Cluster provisioning (kind) — skipped when --use-current or --cluster is set
//  2. CRD apply
//  3. Optional setup manifests
//  4. Bundle generate + apply
//  5. Orkestra helm install
//  6. CR apply
//  7. Expectation polling
//  8. Teardown — always runs for non-owned clusters (--use-current, --cluster);
//     for owned clusters only when --keep-cluster is absent
//
// Teardown reverses every applied resource in the correct order:
// CR delete → helm uninstall → bundle delete → setup helm (reverse) → setup files (reverse) → CRDs.
// This keeps borrowed clusters clean regardless of pass/fail.
//
// Run returns a *Result with per-case timings that callers (e.g. registry push)
// embed as OCI annotations.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/registry"
	"github.com/orkspace/orkestra/pkg/registry/motif"
	orktmpl "github.com/orkspace/orkestra/pkg/template"
	"github.com/orkspace/orkestra/pkg/tools/cluster"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultClusterName = "ork-e2e"
	defaultProvider    = "kind"
	defaultTimeout     = "60s"
)

// Runner executes a single E2E spec end-to-end.
type Runner struct {
	e2e           orktypes.E2E
	e2eDir        string // directory of the e2e.yaml file — resolves relative paths
	keepCluster   bool
	useCurrentCtx bool   // Default (false) - means whether to use the current context, skip cluster creation
	clusterCtx    string // non-empty means use this context, skip cluster creation
	workers       int    // number of kind worker nodes to provision (0 = control-plane only)
	kindVersion   string // kind binary version to use ("" = DefaultKindVersion)

	katalogFile string
	crFiles     []string

	// Orkestra installation options
	orkestraVersion string
	valueFiles      []string
	helmArgs        []string

	// devServer deploys the mock dev server into the cluster as part of setup.
	devServer bool

	// reportFile is the path to write the markdown results report to.
	// Empty means no file is written (stdout only).
	reportFile string

	// kubernetesTarget skips bundle generation and Orkestra helm install/uninstall.
	// Set when spec.custom.target == "kubernetes" — the file is the source of truth.
	kubernetesTarget bool

	// sharedOrkestra means Orkestra is managed by the parent runImports coordinator.
	// The sub-runner must not delete the bundle from the cluster (the bundle contains
	// the orkestra-system namespace; deleting it cascades to the Orkestra deployment).
	// It also suppresses all sync/health-check output — only the coordinator's single
	// install and uninstall messages are visible.
	sharedOrkestra bool

	// noRuntime skips starting the Orkestra runtime. Only the gateway is started
	// when the katalog enables it. The CR lifecycle steps (AfterCRApplied, AfterCRDeleted)
	// become no-ops — the CR is optional and gateway intents drive the expectations instead.
	noRuntime bool

	// cs and cfg are the Go Kubernetes client and REST config, built once after the
	// cluster context is ready. Used for operations that don't need kubectl:
	// Lease reads, port-forward+HTTP, SubjectAccessReview.
	cs  kubernetes.Interface
	cfg *rest.Config
}

// Options configures a Runner. All fields are optional — zero values produce
// the same behaviour as the previous positional defaults.
type Options struct {
	ClusterCtx    string   // use an existing kubectl context, skip cluster creation
	UseCurrentCtx bool     // use the current kubectl context as-is
	KeepCluster   bool     // do not delete the kind cluster after the run
	Workers       int      // number of kind worker nodes (0 = control-plane only)
	KindVersion   string   // kind binary version to download ("" = DefaultKindVersion)
	DevServer     bool     // deploy the mock dev server into the cluster
	OrkVersion    string   // Orkestra helm chart version to install
	ValueFiles    []string // additional Helm values files
	HelmArgs      []string // additional helm --set arguments
	ReportFile    string   // write results as markdown to this path (in addition to stdout)
	NoRuntime     bool     // skip the Orkestra runtime; gateway only; CR is optional
}

// New loads an E2E spec from a YAML file and constructs a Runner.
func New(e2eFile string, opts Options) (*Runner, error) {
	data, err := readLocal(e2eFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", e2eFile, err)
	}

	var e2e orktypes.E2E
	if err := strictUnmarshal(data, &e2e); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", e2eFile, err)
	}
	if e2e.Kind != "E2E" {
		return nil, fmt.Errorf("%s: expected kind E2E, got %q", e2eFile, e2e.Kind)
	}

	e2eDir, err := filepath.Abs(filepath.Dir(e2eFile))
	if err != nil {
		return nil, fmt.Errorf("resolving e2e directory: %w", err)
	}
	allValueFiles := make([]string, 0, len(e2e.Spec.ValuesFiles)+len(opts.ValueFiles))
	for _, f := range e2e.Spec.ValuesFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(e2eDir, f)
		}
		allValueFiles = append(allValueFiles, f)
	}
	allValueFiles = append(allValueFiles, opts.ValueFiles...)

	r := &Runner{
		e2e:              e2e,
		e2eDir:           e2eDir,
		keepCluster:      opts.KeepCluster,
		clusterCtx:       opts.ClusterCtx,
		useCurrentCtx:    opts.UseCurrentCtx,
		workers:          opts.Workers,
		kindVersion:      opts.KindVersion,
		devServer:        opts.DevServer,
		reportFile:       opts.ReportFile,
		orkestraVersion:  opts.OrkVersion,
		valueFiles:       allValueFiles,
		helmArgs:         opts.HelmArgs,
		kubernetesTarget: e2e.Spec.Custom != nil && e2e.Spec.Custom.Target == orktypes.CustomTargetKubernetes,
		noRuntime:        opts.NoRuntime,
	}

	if e2e.Spec.Custom != nil && e2e.Spec.Custom.Target == orktypes.CustomTargetContainer {
		return nil, fmt.Errorf("spec.custom.target \"container\" is coming soon — not yet supported in this version")
	}

	if err := r.resolveSource(); err != nil {
		return nil, err
	}
	if err := r.validateImports(); err != nil {
		return nil, err
	}

	return r, nil
}

// resolveSource resolves the katalog and CR file paths from the spec.
func (r *Runner) resolveSource() error {
	spec := r.e2e.Spec

	switch {
	case spec.Init != nil:
		// Example pack — resolve from examples/ relative to e2e file or cwd.
		base := r.e2eDir
		candidate := filepath.Join(base, "examples", spec.Init.Pack, spec.Init.Example)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			// Try from cwd
			cwd, _ := os.Getwd()
			candidate = filepath.Join(cwd, "examples", spec.Init.Pack, spec.Init.Example)
		}
		r.katalogFile = filepath.Join(candidate, "katalog.yaml")
		r.crFiles = []string{filepath.Join(candidate, "cr.yaml")}

	case spec.Katalog != "" && len(spec.AllCRPaths()) > 0:
		r.katalogFile = r.abs(spec.Katalog)
		for _, p := range spec.AllCRPaths() {
			r.crFiles = append(r.crFiles, r.abs(p))
		}

	case spec.Katalog != "" && r.noRuntime:
		// --no-runtime: katalog without a CR is valid — the gateway drives expectations.
		r.katalogFile = r.abs(spec.Katalog)

	case spec.Custom != nil && spec.Custom.Target != "":
		// custom.target: both katalog and cr are optional.
		for _, p := range spec.AllCRPaths() {
			r.crFiles = append(r.crFiles, r.abs(p))
		}

	case len(r.e2e.Imports) > 0:
		// Pure aggregator — no own katalog/CR, just orchestrates imports.
		return nil

	default:
		return fmt.Errorf("e2e spec must declare either (katalog + cr) or init, or have imports")
	}

	if r.katalogFile != "" {
		if _, err := os.Stat(r.katalogFile); err != nil {
			return fmt.Errorf("katalog file not found: %s", r.katalogFile)
		}
	}
	for _, p := range r.crFiles {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("CR file not found: %s", p)
		}
	}
	return nil
}

// Run executes the full E2E test pipeline and returns a structured Result.
func (r *Runner) Run(ctx context.Context) (*Result, error) {
	name := r.e2e.Metadata.Name
	start := time.Now()
	if desc := r.e2e.Metadata.Description; desc != "" {
		fmt.Printf("\nRunning E2E: %s — %s\n\n", name, desc)
	} else {
		fmt.Printf("\nRunning E2E: %s\n\n", name)
	}

	// Capture the original kubectl context so we can restore it after the
	// test completes (the cluster creation or --cluster flag switches context).
	if origCtx, err := currentKubectlContext(); err == nil && origCtx != "" {
		defer func() {
			if out, err := exec.Command("kubectl", "config", "use-context", origCtx).CombinedOutput(); err != nil {
				fmt.Printf("  ! Could not restore kubectl context to %q: %v\n%s\n", origCtx, err, out)
			} else {
				fmt.Printf("\n→ kubectl context restored to %q\n", origCtx)
			}
		}()
	}

	// When e2e runs against an existing cluster (--cluster flag, --current-context or --keep-cluster),
	// it owns the resources it applied but not the cluster itself. Track everything
	// applied so it can be torn down in reverse order when the test completes.
	//
	// When e2e creates and deletes its own ephemeral cluster, teardown is handled
	// by the cluster deletion — no per-resource cleanup is needed.
	isPureAgg := r.isPureAggregator()

	ownsCluster := r.clusterCtx == "" && !r.keepCluster && !r.useCurrentCtx
	var (
		appliedCRDPaths   []string
		appliedBundlePath string
		appliedSetupPaths []string
		installedOrkestra bool
	)
	if !ownsCluster {
		defer func() {
			r.teardown(context.Background(),
				appliedCRDPaths, appliedBundlePath, appliedSetupPaths, installedOrkestra)
		}()
	}

	// ── 1. Cluster ───────────────────────────────────────────────────────
	if err := r.ensureCluster(ctx); err != nil {
		return nil, fmt.Errorf("cluster: %w", err)
	}
	if err := r.buildClient(); err != nil {
		return nil, fmt.Errorf("k8s client: %w", err)
	}

	// Steps 2–9 are skipped for pure aggregators (no spec — imports only).
	// Each imported E2E runs its own full lifecycle against the shared cluster.
	var cases []CaseResult

	if !isPureAgg {
		// ── 2. Dependencies ──────────────────────────────────────────────
		fmt.Println("→ Ensuring dependencies...")
		if err := cluster.EnsureDependencies(); err != nil {
			return nil, fmt.Errorf("dependencies: %w", err)
		}

		// ── 3. Apply operator CRD ────────────────────────────────────────
		crdPaths, err := r.applyCRD(ctx)
		if err != nil {
			return nil, fmt.Errorf("applying CRD: %w", err)
		}
		appliedCRDPaths = crdPaths

		// ── 4. Pre-pull OCI imports ──────────────────────────────────────
		if !r.kubernetesTarget {
			if err := r.pullOCIImports(ctx); err != nil {
				return nil, fmt.Errorf("pulling OCI imports: %w", err)
			}
		}

		// ── 5. Generate and apply bundle ─────────────────────────────────
		if !r.kubernetesTarget {
			bundleFile, err := r.generateBundle(ctx)
			if err != nil {
				return nil, fmt.Errorf("generate bundle: %w", err)
			}
			if ownsCluster {
				defer os.Remove(bundleFile)
			} else {
				appliedBundlePath = bundleFile
			}

			fmt.Printf("→ Applying bundle...\n")
			if out, err := kubectl(ctx, "apply", "-f", bundleFile); err != nil {
				return nil, fmt.Errorf("apply bundle: %w\n%s", err, out)
			}
			fmt.Printf("  %s Bundle applied\n", successMark())
		}

		// ── 6. Setup ─────────────────────────────────────────────────────
		setupPaths, err := r.applySetup(ctx)
		if err != nil {
			return nil, fmt.Errorf("setup: %w", err)
		}
		appliedSetupPaths = setupPaths

		// ── 6b. Dev server ───────────────────────────────────────────────
		if r.devServer {
			devManifest, err := applyDevServer(ctx)
			if err != nil {
				return nil, fmt.Errorf("dev server: %w", err)
			}
			appliedSetupPaths = append(appliedSetupPaths, devManifest)
			fmt.Printf("→ Waiting for dev server to be ready...\n")
			if err := checkDevServerHealth(); err != nil {
				return nil, err
			}
			fmt.Printf("  %s Dev server ready\n", successMark())
		}

		// ── 7. Install Orkestra ──────────────────────────────────────────
		// ── 8. Wait for Orkestra ready ───────────────────────────────────
		// Both steps skipped when custom.target is set.
		if !r.kubernetesTarget {
			text := "..."

			// Control center is never needed in e2e — disable it unconditionally.
			r.helmArgs = append(r.helmArgs, "--set", "controlCenter.enabled=false")

			gatewayEnabled, err := resolveGatewayEnabled(r.katalogFile)
			if err != nil {
				return nil, err
			}
			if gatewayEnabled {
				fmt.Printf("→ Gateway enabled...\n")
				r.helmArgs = append(r.helmArgs, "--set", "gateway.enabled=true")
				text = " with gateway..."
			}
			if r.noRuntime {
				r.helmArgs = append(r.helmArgs, "--set", "runtime.enabled=false")
				text = " gateway only..."
			}

			if !cluster.RuntimeInstalled() {
				sp := startSpinner("Installing Orkestra" + text)
				if err := cluster.InstallOrUpgradeOrkestra(r.orkestraVersion, r.valueFiles, r.helmArgs...); err != nil {
					sp.Failure()
					return nil, fmt.Errorf("helm install: %w", err)
				}
				sp.Success()
				installedOrkestra = true
			} else {
				// Orkestra is already running from a previous import.
				// Two steps are always needed:
				// 1. helm upgrade — applies this import's valueFiles (image, features).
				//    When values match the previous import the pod is not restarted.
				// 2. SyncRuntime — restarts the pod so it loads the new bundle ConfigMap.
				//    Without this, a values-identical upgrade leaves the old bundle in memory.
				sp := startSpinner("Upgrading Orkestra" + text)
				if err := cluster.InstallOrUpgradeOrkestra(r.orkestraVersion, r.valueFiles, r.helmArgs...); err != nil {
					sp.Failure()
					return nil, fmt.Errorf("helm upgrade: %w", err)
				}
				sp.Success()
				if !r.noRuntime {
					if err := cluster.SyncRuntime(); err != nil {
						return nil, fmt.Errorf("syncing Orkestra runtime: %w", err)
					}
				}
				if gatewayEnabled {
					if cluster.GatewayInstalled() {
						if err := cluster.SyncGateway(); err != nil {
							return nil, fmt.Errorf("syncing Orkestra gateway: %w", err)
						}
					} else {
						sp := startSpinner("Upgrading Orkestra to enable gateway...")
						if err := cluster.InstallOrUpgradeOrkestra(r.orkestraVersion, r.valueFiles, r.helmArgs...); err != nil {
							sp.Failure()
							return nil, fmt.Errorf("helm upgrade (gateway): %w", err)
						}
						sp.Success()
					}
				}
				installedOrkestra = true
			}

			if installedOrkestra {
				if !r.noRuntime {
					status := cluster.CheckRuntimeHealth()
					if !status.Running {
						return nil, fmt.Errorf("Orkestra runtime not ready: %s", status.Reason)
					}
				}
				if gatewayEnabled {
					status := cluster.CheckGatewayHealth()
					if !status.Running {
						return nil, fmt.Errorf("Orkestra gateway not ready: %s", status.Reason)
					}
				}
			}
		}

		// ── 9. Run expectations ──────────────────────────────────────────
		if err := ensureTools(r.e2e); err != nil {
			return nil, err
		}

		expects, err := ExpandExpectIncludes(r.e2e.Spec.Expect, r.e2eDir)
		if err != nil {
			return nil, err
		}

		// Build a template evaluator from spec.notes so when:/or: expressions
		// on expect blocks can reference user-defined note functions.
		noteEval := orktmpl.NewResolverFromMap(nil).
			WithUserNotes(r.e2e.Spec.Notes).
			TemplateEvaluator()

		crApplied := false
		crDeleted := false

		for _, exp := range expects {
			after := exp.After
			if after == "" {
				after = orktypes.AfterSetupComplete
			}
			switch after {
			case orktypes.AfterSetupComplete:
				// Infrastructure assertions — no CR lifecycle action needed.

			case orktypes.AfterCRApplied:
				if !crApplied && !r.noRuntime {
					fmt.Printf("→ Applying CR(s)...\n")
					for _, p := range r.crFiles {
						if out, err := kubectl(ctx, "apply", "-f", p); err != nil {
							return nil, fmt.Errorf("apply CR %s: %w\n%s", p, err, out)
						}
					}
					fmt.Printf("  %s CR(s) applied\n\n", successMark())
					crApplied = true
				}

			case orktypes.AfterCRDeleted:
				if !crDeleted && !r.noRuntime {
					fmt.Printf("→ Deleting CR(s)...\n")
					for _, p := range r.crFiles {
						if out, err := kubectl(ctx, "delete", "-f", p, "--ignore-not-found"); err != nil {
							return nil, fmt.Errorf("delete CR %s: %w\n%s", p, err, out)
						}
					}
					fmt.Printf("  %s CR(s) deleted\n\n", successMark())
					crDeleted = true
				}

			default:
				return nil, fmt.Errorf("unknown after: %q — valid values: %v", after, orktypes.ValidAfterValues)
			}

			to := exp.Timeout
			if to == "" {
				to = defaultTimeout
			}
			fmt.Printf("  Waiting for %q (timeout: %s)...\n", exp.Name, to)
			caseStart := time.Now()
			waitDone := make(chan struct{})
			go func() {
				t := time.NewTicker(10 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-waitDone:
						return
					case <-t.C:
						fmt.Printf("    still waiting [%s]...\n", time.Since(caseStart).Round(time.Second))
					}
				}
			}()
			workDir := r.e2eDir
			if exp.WorkDir != "" {
				workDir = exp.WorkDir
			}
			verifyErr := verifyExpectation(ctx, exp, workDir, r.cs, r.cfg, noteEval)
			close(waitDone)
			caseElapsed := time.Since(caseStart)

			skipped := verifyErr == errSkipped
			cr := CaseResult{
				Name:    exp.Name,
				Passed:  verifyErr == nil,
				Skipped: skipped,
				Elapsed: caseElapsed,
				Err:     verifyErr,
			}
			if skipped {
				cr.Err = nil
			}
			cases = append(cases, cr)
			switch {
			case skipped:
				fmt.Printf("  ~ %s (skipped)\n", exp.Name)
			case verifyErr != nil:
				fmt.Printf("  %s %s (%s): %v\n", failureMark(), exp.Name, caseElapsed.Round(time.Millisecond), verifyErr)
				runOnFailure(ctx, exp.OnFailure, r.e2eDir, r.cs)
			default:
				fmt.Printf("  %s %s (%s)\n", successMark(), exp.Name, caseElapsed.Round(time.Millisecond))
			}
		}
	}

	result := &Result{
		Name:    name,
		Cases:   cases,
		Elapsed: time.Since(start),
	}

	// ── Report ───────────────────────────────────────────────────────────
	if !isPureAgg {
		fmt.Printf("\nE2E Results: %s\n\n", name)

		for _, c := range cases {
			if c.Passed {
				fmt.Printf("  %s %-40s (%s)\n", successMark(), c.Name, c.Elapsed.Round(time.Millisecond))
			}
		}

		var failures []CaseResult
		for _, c := range cases {
			if !c.Passed && !c.Skipped {
				failures = append(failures, c)
			}
		}
		if len(failures) > 0 {
			fmt.Printf("\n")
			for _, c := range failures {
				fmt.Printf("  %s %-40s (%s)\n", failureMark(), c.Name, c.Elapsed.Round(time.Millisecond))
				if c.Err != nil {
					for _, line := range strings.Split(strings.TrimSpace(c.Err.Error()), "\n") {
						fmt.Printf("      %s\n", line)
					}
				}
			}
		}

		var skippedCases []CaseResult
		for _, c := range cases {
			if c.Skipped {
				skippedCases = append(skippedCases, c)
			}
		}
		if len(skippedCases) > 0 {
			fmt.Printf("\n")
			for _, c := range skippedCases {
				fmt.Printf("  ~ %-40s (skipped)\n", c.Name)
			}
		}

		clusterInfo := r.clusterName()
		fmt.Printf("\n  %s\n", result.Summary())
		if clusterInfo != "" {
			fmt.Printf("  Cluster: %s (%s)\n", clusterInfo, r.provider())
		}

		if len(failures) > 0 {
			runOnFailure(ctx, r.e2e.Spec.OnFailure, r.e2eDir, r.cs)
		}

		if r.reportFile != "" {
			if err := os.WriteFile(r.reportFile, []byte(result.Markdown()), 0644); err != nil {
				fmt.Printf("  ! could not write report file: %v\n", err)
			} else {
				fmt.Printf("  Report written to %s\n", r.reportFile)
			}
		}
	}

	// ── 10. Imports ──────────────────────────────────────────────────────
	var importErr error
	importCount := len(r.e2e.Imports)
	importText := "imports"

	if importCount == 1 {
		importText = "import"
	}

	if importCount > 0 {
		fmt.Printf("\n─── Running %d %s ───\n", importCount, importText)
		importResults := r.runImports(ctx)
		importErr = printImportSummary(r.e2e.Metadata.Name, importResults)
	}

	// ── 11. Cleanup ──────────────────────────────────────────────────────
	if !r.useCurrentCtx && !r.keepCluster && r.clusterCtx == "" {
		fmt.Printf("\n→ Deleting cluster '%s'...\n", r.clusterName())
		if err := r.deleteCluster(ctx); err != nil {
			fmt.Printf("  ! Could not delete cluster: %v\n", err)
		} else {
			fmt.Printf("  %s Cluster deleted\n", successMark())
		}
	}

	if !result.AllPassed() {
		return result, fmt.Errorf("%d of %d expectations failed", result.Total()-result.Passed(), result.Total())
	}
	return result, importErr
}

// resolveGatewayEnabled inspects the katalog file and returns true if the
// gateway block is present. This allows Helm installation to automatically
// enable the gateway chart when required by the katalog.
func resolveGatewayEnabled(katalogFile string) (bool, error) {
	var raw struct {
		Gateway *orktypes.GatewayConfig `yaml:"gateway,omitempty"`
	}

	data, err := readLocal(katalogFile)
	if err != nil {
		return false, err
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false, err
	}
	return raw.Gateway != nil, nil
}

// buildClient constructs the Go Kubernetes client and REST config from the active
// kubeconfig after the cluster context has been established by ensureCluster.
func (r *Runner) buildClient() error {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	r.cfg = cfg
	r.cs = cs
	return nil
}

// ensureCluster sets up the cluster according to the spec.
func (r *Runner) ensureCluster(ctx context.Context) error {
	if r.useCurrentCtx {
		fmt.Printf("→ Using current cluster context...\n")
		return nil
	}

	if r.clusterCtx != "" {
		fmt.Printf("→ Using existing cluster context %q...\n", r.clusterCtx)
		if out, err := exec.CommandContext(ctx, "kubectl", "config", "use-context", r.clusterCtx).CombinedOutput(); err != nil {
			return fmt.Errorf("switching context: %w\n%s", err, out)
		}
		fmt.Printf("  %s Using context %s\n", successMark(), r.clusterCtx)
		return nil
	}

	provider := r.provider()
	if provider != "kind" {
		return fmt.Errorf("provider %q not supported — only 'kind' is available", provider)
	}

	name := r.clusterName()
	spec := r.e2e.Spec.Cluster

	if !spec.Reuse {
		// Delete if already exists for a clean state
		if clusterExists(name) {
			fmt.Printf("→ Recreating cluster '%s' (reuse: false)...\n", name)
			if err := deleteKindCluster(ctx, name); err != nil {
				return fmt.Errorf("deleting old cluster: %w", err)
			}
		}
	}

	return cluster.EnsureKindCluster(name, r.workers, r.kindVersion)
}

// applyCRD applies the operator's CRD to the cluster and returns the paths applied.
// Uses spec.AllCRDPaths() if declared; falls back to crdFile entries in the katalog.
func (r *Runner) applyCRD(ctx context.Context) ([]string, error) {
	if crdPaths := r.e2e.Spec.AllCRDPaths(); len(crdPaths) > 0 {
		var applied []string
		for _, crd := range crdPaths {
			path := r.abs(crd)
			fmt.Printf("→ Applying CRD from %s...\n", crd)
			if out, err := kubectl(ctx, "apply", "-f", path); err != nil {
				return nil, fmt.Errorf("applying CRD %s: %w\n%s", crd, err, out)
			}
			applied = append(applied, path)
		}
		fmt.Printf("  %s CRD(s) applied\n", successMark())
		return applied, nil
	}

	// Fallback: read crdFile references from the katalog.
	// When kubernetesTarget is true and no katalog is provided, there is nothing to fall back to.
	if r.katalogFile == "" {
		return nil, nil
	}
	var raw struct {
		Spec struct {
			CRDs map[string]struct {
				CRDFile string `yaml:"crdFile"`
			} `yaml:"crds"`
		} `yaml:"spec"`
	}
	data, err := readLocal(r.katalogFile)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	katalogDir := filepath.Dir(r.katalogFile)
	var applied []string
	for name, entry := range raw.Spec.CRDs {
		if entry.CRDFile == "" {
			continue
		}
		path := entry.CRDFile
		if !filepath.IsAbs(path) && !strings.HasPrefix(path, "http") {
			path = filepath.Join(katalogDir, path)
		}
		fmt.Printf("→ Applying CRD '%s' from %s...\n", name, entry.CRDFile)
		if out, err := kubectl(ctx, "apply", "-f", path); err != nil {
			fmt.Printf("  ! CRD apply failed (continuing): %v\n%s\n", err, out)
		} else {
			fmt.Printf("  %s CRD applied\n", successMark())
			applied = append(applied, path)
		}
	}
	return applied, nil
}

func (r *Runner) generateBundle(ctx context.Context) (string, error) {
	// Anchor the katalog directory as an absolute path. r.katalogFile may be
	// relative when ork e2e is invoked without an explicit -f path; all temp
	// file creation and cmd.Dir must use an absolute base to avoid double-nested
	// paths when cmd.Dir is set.
	katalogDir, err := filepath.Abs(filepath.Dir(r.katalogFile))
	if err != nil {
		return "", fmt.Errorf("resolving katalog directory: %w", err)
	}

	// Resolve any crdFile references to inline apiTypes before bundling.
	// The Orkestra runtime runs inside a container and cannot read local files —
	// all type information must be embedded in the ConfigMap.
	resolved, err := katalog.ResolveCRDFiles(r.katalogFile)
	if err != nil {
		return "", fmt.Errorf("resolving crdFile references: %w", err)
	}

	// Create the temp file in the katalog's directory (absolute) so that
	// relative imports.files paths resolve correctly when ork generate bundle runs.
	resolvedKatalog, err := os.CreateTemp(katalogDir, "ork-e2e-katalog-*.yaml")
	if err != nil {
		return "", err
	}
	if _, err := resolvedKatalog.Write(resolved); err != nil {
		resolvedKatalog.Close()
		os.Remove(resolvedKatalog.Name())
		return "", err
	}
	resolvedKatalog.Close()
	defer os.Remove(resolvedKatalog.Name())

	bundleFile, err := os.CreateTemp("", "ork-e2e-bundle-*.yaml")
	if err != nil {
		return "", err
	}
	bundleFile.Close()

	fmt.Printf("→ Generating bundle from %s...\n", r.katalogFile)
	orkBin, err := os.Executable()
	if err != nil {
		orkBin = "ork"
	}
	cmd := exec.CommandContext(ctx, orkBin, "generate", "bundle",
		"-f", resolvedKatalog.Name(),
		"-o", bundleFile.Name(),
	)
	// Run from the katalog's directory (absolute) so relative imports.files
	// paths (e.g. ./platform-team/katalog.yaml) resolve correctly.
	cmd.Dir = katalogDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(bundleFile.Name())
		return "", fmt.Errorf("ork generate bundle: %w", err)
	}
	fmt.Printf("  %s Bundle generated\n", successMark())
	return bundleFile.Name(), nil
}

// pullOCIImports pre-pulls all OCI motif and registry imports referenced in
// the katalog file so that bundle generation never needs to do OCI calls.
// Uses the same resolution logic as the merge path, including bare-name shorthands.
func (r *Runner) pullOCIImports(_ context.Context) error {
	imports, err := registry.ExtractOCIImports(r.katalogFile)
	if err != nil {
		return fmt.Errorf("extracting OCI imports from %s: %w", r.katalogFile, err)
	}
	if imports.Empty() {
		return nil
	}

	fmt.Printf("→ Pulling OCI imports...\n")

	for _, imp := range imports.MotifImports {
		fmt.Printf("  → motif %s\n", imp.Motif)
		if err := motif.PullImport(&imp); err != nil {
			return fmt.Errorf("pulling motif %q: %w", imp.Motif, err)
		}
		fmt.Printf("  %s %s\n", successMark(), imp.Motif)
	}

	client, err := registry.NewClient()
	if err != nil {
		return fmt.Errorf("initializing registry client: %w", err)
	}
	_ = client // used for registry source pulls below when Komposer support is needed

	return nil
}

func (r *Runner) applySetup(ctx context.Context) ([]string, error) {
	s := r.e2e.Spec.Setup
	if s == nil {
		return nil, nil
	}

	var applied []string

	// ── Phase 1: apply ────────────────────────────────────────────────────────
	for _, entry := range s.Apply {
		abs := r.abs(entry.Path)
		fmt.Printf("→ Applying setup %s...\n", entry.Path)
		if out, err := kubectl(ctx, "apply", "-f", abs); err != nil {
			return applied, fmt.Errorf("setup apply %s: %w\n%s", entry.Path, err, out)
		}
		fmt.Printf("  %s Applied\n", successMark())
		applied = append(applied, abs)
		for _, w := range entry.Wait {
			if err := runSetupWait(ctx, w); err != nil {
				return applied, fmt.Errorf("setup apply %s wait: %w", entry.Path, err)
			}
		}
	}

	// ── Phase 2: helm ─────────────────────────────────────────────────────────
	for _, h := range s.Helm {
		if h.IsLocalChart() && !filepath.IsAbs(h.Chart) {
			h.Chart = r.abs(h.Chart)
		}
		for i, f := range h.ValueFiles {
			if f != "" && !filepath.IsAbs(f) {
				h.ValueFiles[i] = r.abs(f)
			}
		}
		sp := startSpinner(fmt.Sprintf("Installing %s...", h.ReleaseName()))
		if err := cluster.HelmInstall(ctx, h); err != nil {
			sp.Failure()
			return applied, fmt.Errorf("setup helm %s: %w", h.Chart, err)
		}
		sp.Success()
		for _, w := range h.Wait {
			if err := runSetupWait(ctx, w); err != nil {
				return applied, fmt.Errorf("setup helm %s wait: %w", h.ReleaseName(), err)
			}
		}
	}

	// ── Phase 3: wait ─────────────────────────────────────────────────────────
	for _, w := range s.Wait {
		if err := runSetupWait(ctx, w); err != nil {
			return applied, fmt.Errorf("setup wait: %w", err)
		}
	}

	return applied, nil
}

func runSetupWait(ctx context.Context, w orktypes.SetupWait) error {
	loc := w.Kind + " " + w.Name
	if w.Namespace != "" {
		loc += " (" + w.Namespace + ")"
	}
	sp := startSpinner(fmt.Sprintf("Waiting for %s...", loc))
	if err := cluster.WaitForResource(ctx, w); err != nil {
		sp.Failure()
		return err
	}
	sp.Success()
	return nil
}

func (r *Runner) deleteCluster(ctx context.Context) error {
	return deleteKindCluster(ctx, r.clusterName())
}

func (r *Runner) clusterName() string {
	if r.e2e.Spec.Cluster.Name != "" {
		return r.e2e.Spec.Cluster.Name
	}
	return defaultClusterName
}

func (r *Runner) provider() string {
	if r.e2e.Spec.Cluster.Provider != "" {
		return r.e2e.Spec.Cluster.Provider
	}
	return defaultProvider
}

// isPureAggregator returns true when this E2E has no spec of its own —
// it exists only to run imported E2E files.
// A kubernetesTarget spec is never a pure aggregator even when cr and katalog are omitted.
func (r *Runner) isPureAggregator() bool {
	return r.katalogFile == "" && len(r.crFiles) == 0 && !r.kubernetesTarget
}

func (r *Runner) abs(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.e2eDir, path)
}

func currentKubectlContext() (string, error) {
	out, err := exec.Command("kubectl", "config", "current-context").Output()
	return strings.TrimSpace(string(out)), err
}

// kubectl runs a kubectl command and returns combined output.
func kubectl(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func clusterExists(name string) bool {
	out, _ := exec.Command("kind", "get", "clusters").Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// teardown cleans up every resource applied to an existing cluster.
// Called via defer when e2e runs against --current-context, --cluster or --keep-cluster, where
// deleting the cluster itself is not an option.
// Teardown order is the reverse of apply order: CR → Orkestra → bundle → setup → CRDs.
// The CR is expected to already be deleted by the cr-deleted expectation block;
// this handles everything else.
func (r *Runner) teardown(ctx context.Context, crdPaths []string, bundlePath string, setupPaths []string, uninstallOrkestra bool) {
	fmt.Printf("\n→ Cleaning up resources...\n")

	// Helm uninstall orkestra — must happen before bundle delete so the
	// runtime is stopped before its RBAC and ConfigMap are removed.
	if uninstallOrkestra && !r.sharedOrkestra {
		fmt.Printf("  → Uninstalling Orkestra...\n")
		cmd := exec.CommandContext(ctx, "helm", "uninstall", cluster.Orkestra,
			"--namespace", cluster.OrkestraNamespace, "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("  ! helm uninstall failed: %v\n%s\n", err, out)
		} else {
			fmt.Printf("  %s Orkestra uninstalled\n", successMark())
		}
	}

	// Bundle (RBAC, ConfigMap, Namespace created by ork generate bundle).
	// sharedOrkestra: skip kubectl delete — the bundle contains orkestra-system namespace;
	// deleting it cascades to the Orkestra deployment managed by the coordinator.
	// The temp file is still removed.
	if bundlePath != "" {
		if !r.sharedOrkestra {
			fmt.Printf("  → Deleting bundle resources...\n")
			if out, err := kubectl(ctx, "delete", "-f", bundlePath, "--ignore-not-found"); err != nil {
				fmt.Printf("  ! bundle delete failed: %v\n%s\n", err, out)
			} else {
				fmt.Printf("  %s Bundle resources deleted\n", successMark())
			}
		}
		os.Remove(bundlePath)
	}

	// Setup helm releases in reverse order.
	if r.e2e.Spec.Setup != nil {
		helms := r.e2e.Spec.Setup.Helm
		for i := len(helms) - 1; i >= 0; i-- {
			h := helms[i]
			fmt.Printf("  → Uninstalling setup helm %s...\n", h.ReleaseName())
			if err := cluster.HelmUninstall(ctx, h); err != nil {
				fmt.Printf("  ! setup helm uninstall failed (%s): %v\n", h.ReleaseName(), err)
			} else {
				fmt.Printf("  %s %s uninstalled\n", successMark(), h.ReleaseName())
			}
		}
	}

	// Setup files in reverse order.
	for i := len(setupPaths) - 1; i >= 0; i-- {
		path := setupPaths[i]
		fmt.Printf("  → Deleting setup %s...\n", filepath.Base(path))
		if out, err := kubectl(ctx, "delete", "-f", path, "--ignore-not-found"); err != nil {
			fmt.Printf("  ! setup delete failed (%s): %v\n%s\n", path, err, out)
		}
	}

	// CRDs last — deleting a CRD cascades to all CRs of that type.
	for _, path := range crdPaths {
		fmt.Printf("  → Deleting CRD %s...\n", filepath.Base(path))
		if out, err := kubectl(ctx, "delete", "-f", path, "--ignore-not-found"); err != nil {
			fmt.Printf("  ! CRD delete failed (%s): %v\n%s\n", path, err, out)
		} else {
			fmt.Printf("  %s CRD deleted\n", successMark())
		}
	}

	fmt.Printf("  %s Cleanup complete\n", successMark())
}

// validateImports is a backstop that delegates to the exported ValidateImports.
// ork e2e always calls this at startup so malformed imports are caught before
// the cluster is provisioned. ork validate calls ValidateImports directly for
// earlier, friendlier feedback.
func (r *Runner) validateImports() error {
	errs := ValidateImports(r.e2eDir, r.e2e.Imports)
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("invalid imports: %s", strings.Join(msgs, "; "))
}

// runImports runs each imported E2E file after the main test completes.
//
// Cluster strategy for shared-cluster imports (freshCluster: false, the default):
//   - Parent is a pure aggregator: imports use the cluster ensureCluster already set up.
//   - Parent used --use-current or --cluster: imports reuse the same active context.
//   - Parent ran its own test and created a kind cluster: a separate kind cluster
//     is created so imports don't share state with the parent's live resources.
//
// Imports with freshCluster: true always provision their own independent cluster.
func (r *Runner) runImports(ctx context.Context) []ImportResult {
	parentOwnsCluster := r.clusterCtx == "" && !r.useCurrentCtx

	if parentOwnsCluster && !r.isPureAggregator() {
		// Non-aggregator parent created its own cluster — provision a separate
		// one so imports don't run alongside the parent's Orkestra install.
		importCluster := r.clusterName() + "-imports"
		fmt.Printf("→ Creating imports cluster '%s'...\n", importCluster)
		if err := cluster.EnsureKindCluster(importCluster, r.workers, r.kindVersion); err != nil {
			return []ImportResult{{Path: importCluster, Err: fmt.Errorf("creating imports cluster: %w", err)}}
		}
		if !r.keepCluster {
			defer func() {
				fmt.Printf("→ Deleting imports cluster '%s'...\n", importCluster)
				_ = deleteKindCluster(ctx, importCluster)
			}()
		}
	}

	// Each sub-runner installs or upgrades Orkestra with its own valueFiles so
	// fixture-specific values (image, features) are applied correctly. The
	// coordinator only owns cleanup — it defers an uninstall that runs after all
	// imports complete. --ignore-not-found makes it safe if no import ran.
	if !r.kubernetesTarget {
		hasShared := false
		for _, imp := range r.e2e.Imports {
			if !imp.FreshCluster {
				hasShared = true
				break
			}
		}
		if hasShared {
			defer func() {
				fmt.Printf("→ Uninstalling Orkestra...\n")
				cmd := exec.CommandContext(ctx, "helm", "uninstall", cluster.Orkestra,
					"--namespace", cluster.OrkestraNamespace, "--ignore-not-found")
				if out, err := cmd.CombinedOutput(); err != nil {
					fmt.Printf("  ! helm uninstall failed: %v\n%s\n", err, out)
				} else {
					fmt.Printf("  %s Orkestra uninstalled\n", successMark())
				}
			}()
		}
	}

	var results []ImportResult
	for _, imp := range r.e2e.Imports {
		absPath := r.abs(imp.Path)
		ir := ImportResult{Path: imp.Path}

		if imp.Wait != "" {
			d, _ := parseTimeDuration(imp.Wait) // already validated at load time
			fmt.Printf("→ Waiting %s before %s...\n", imp.Wait, filepath.Base(imp.Path))
			time.Sleep(d)
		}

		var sub *Runner
		var err error
		if imp.FreshCluster {
			sub, err = New(absPath, Options{KeepCluster: r.keepCluster, Workers: r.workers, KindVersion: r.kindVersion, DevServer: r.devServer, OrkVersion: r.orkestraVersion, ValueFiles: r.valueFiles})
		} else {
			sub, err = New(absPath, Options{UseCurrentCtx: true, Workers: r.workers, KindVersion: r.kindVersion, DevServer: r.devServer, OrkVersion: r.orkestraVersion, ValueFiles: r.valueFiles})
			if err == nil {
				sub.sharedOrkestra = true
			}
		}
		// kubernetesTarget is declared in the sub-file itself; the parent's value
		// does not override it — each import is authoritative about its own mode.
		if err != nil {
			ir.Err = fmt.Errorf("loading import %s: %w", imp.Path, err)
			results = append(results, ir)
			continue
		}
		res, runErr := sub.Run(ctx)
		ir.Result = res
		ir.Err = runErr
		results = append(results, ir)
	}
	return results
}

func deleteKindCluster(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
