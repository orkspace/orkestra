//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/registry"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	"github.com/spf13/cobra"
)

// ── utils aliases ────────────────────────────────────────────────────────────
// Import utils once here. All other files in this package use these names
// directly — no per-file utils import needed.

var (
	// colors / styles
	gray    = utils.Gray
	bold    = utils.Bold
	dim     = utils.Dim
	cyan    = utils.Cyan
	green   = utils.Green
	blue    = utils.Blue
	white   = utils.White
	yellow  = utils.Yellow
	red     = utils.Red
	magenta = utils.Magenta

	// marks and icons
	successMark     = utils.SuccessMark
	failureMark     = utils.FailureMark
	warningMark     = utils.WarningMark
	infoMark        = utils.InfoMark
	secureMark      = utils.SecureMark
	someSecureMark  = utils.SomeSecureMark
	noSecurityMark  = utils.NoSecurityMark
	healthIcon      = utils.HealthIcon
	healthIconReady = utils.HealthIconReady
	healthIconWarn  = utils.HealthIconWarning

	// other cli utilities
	orkestraLogo        = utils.OrkestraLogoCLI
	isRunningInCluster  = utils.IsRunningInCluster
	writeFileAndFormat  = utils.WriteFileAndFormat
	splitCommaSeparated = utils.SplitCommaSeparated
	readLocal           = utils.ReadLocal
	strictUnmarshal     = utils.StrictUnmarshal
)

// sortedKeys returns a sorted slice of all keys from the given map.
func sortedKeys[V any](m map[string]V) []string {
	return utils.SortedKeys(m)
}

// toAbsPath converts p to an absolute path.
// If p is already absolute, it is returned unchanged.
// If p is relative, it is resolved relative to the current working directory.
func toAbsPath(p string) (string, error) {
	return filepath.Abs(p)
}

// isAbsPath reports whether p is an absolute filesystem path.
func isAbsPath(p string) bool { return filepath.IsAbs(p) }

// joinPath resolves p relative to the directory that contains base.
// If p is already absolute, it is returned unchanged.
func joinPath(base, p string) string {
	if isAbsPath(p) {
		return p
	}
	return filepath.Join(filepath.Dir(base), p)
}

// Shadow global command flags
func shadowGlobalCommandFlags(cmd *cobra.Command, flags ...string) {
	// Shadow standard global flags for all commands
	if cmd.Parent() != nil {
		if cmd.Flags().Lookup("debug") == nil {
			cmd.Flags().Bool("debug", false, "")
			cmd.Flags().MarkHidden("debug")
		}
		if cmd.Flags().Lookup("kubeconfig") == nil {
			cmd.Flags().String("kubeconfig", "", "")
			cmd.Flags().MarkHidden("kubeconfig")
		}
		if cmd.Flags().Lookup("verbose") == nil {
			cmd.Flags().Bool("verbose", false, "")
			cmd.Flags().MarkHidden("verbose")
		}
	}

	// Shadow specific flags passed as arguments
	for _, f := range flags {
		if cmd.PersistentFlags().Lookup(f) == nil {
			switch f {
			case "file":
				cmd.PersistentFlags().StringSlice("file", nil, "")
				cmd.PersistentFlags().MarkHidden("file")
				// case "placeholder":
				// 	cmd.PersistentFlags().StringP("placeholder", "p", "", "")
				// 	cmd.PersistentFlags().MarkHidden("placeholder")
				// Add other flags as needed
			}
		}
	}

	// Process all subcommands
	for _, subCmd := range cmd.Commands() {
		shadowGlobalCommandFlags(subCmd, flags...)
	}
}

// ── printTemplateSummary ──────────────────────────────────────────────────────

// printTemplateSummary prints the human-readable default output of `ork template`.
// CRDs are listed in startup order so the user sees the dependency sequence.
func printTemplateSummary(k *katalog.Katalog, crds map[string]orktypes.CRDEntry, startupOrder []string) {
	meta := k.Metadata()

	fmt.Printf("\n%s", cyan(bold("Katalog")))
	if meta.Name != "" {
		fmt.Printf(": %s", bold(meta.Name))
	}
	if meta.Version != "" {
		fmt.Printf(" %s", dim("("+meta.Version+")"))
	}
	if k.APIVersion != "" {
		fmt.Printf("  %s", dim(k.APIVersion))
	}
	fmt.Println()

	fmt.Printf("  %d CRD(s) — startup order: %s\n\n",
		len(crds),
		yellow(strings.Join(startupOrder, " → ")),
	)

	for i, name := range startupOrder {
		crd, ok := crds[name]
		if !ok {
			continue
		}
		isLast := i == len(startupOrder)-1
		connector := "├─"
		if isLast {
			connector = "└─"
		}

		gvk := fmt.Sprintf("%s/%s, Kind=%s", crd.APITypes.Group, crd.APITypes.Version, crd.APITypes.Kind)
		fmt.Printf("  %s %s  %s\n",
			connector,
			bold(name),
			dim(gvk),
		)

		indent := "  │   "
		if isLast {
			indent = "      "
		}

		// Workers / resync
		fmt.Printf("%sworkers:%s  resync:%s",
			indent,
			green(fmt.Sprintf("%d", crd.OperatorBox.Reconciler.Workers)),
			green(crd.OperatorBox.Reconciler.Resync.String()),
		)
		if crd.OperatorBox.Reconciler.Queue.MaxDepth > 0 {
			fmt.Printf("  queue:%s", green(fmt.Sprintf("%d", crd.OperatorBox.Reconciler.Queue.MaxDepth)))
		}
		fmt.Println()

		// DependsOn
		if len(crd.DependsOn) > 0 {
			deps := make([]string, 0, len(crd.DependsOn))
			for depName, d := range crd.DependsOn {
				if d.Condition != "" && d.Condition != "started" {
					deps = append(deps, fmt.Sprintf("%s(%s)", depName, d.Condition))
				} else {
					deps = append(deps, depName)
				}
			}
			fmt.Printf("%s%s %s\n",
				indent,
				yellow("dependsOn:"),
				strings.Join(deps, ", "),
			)
		}

		// Mode / reconciler
		mode := "default"
		if crd.Mode != "" {
			mode = string(crd.Mode)
		}
		if !crd.DefaultReconcile() {
			if crd.CustomHooksEnabled() {
				mode = "hooks"
			} else if crd.ConstructorEnabled() {
				mode = "constructor"
			}
		}
		fmt.Printf("%smode: %s\n", indent, mode)

		// onCreate resources
		if crd.OperatorBox.OnCreate != nil && !crd.OperatorBox.OnCreate.Empty() {
			fmt.Printf("%s%s  %s\n", indent, cyan("onCreate:"),
				summarizeHookTemplates(crd.OperatorBox.OnCreate))
		}

		// onReconcile resources
		if crd.OperatorBox.OnReconcile != nil && !crd.OperatorBox.OnReconcile.Empty() {
			fmt.Printf("%s%s  %s\n", indent, cyan("onReconcile:"),
				summarizeHookTemplates(crd.OperatorBox.OnReconcile))
		}

		// Status fields
		if crd.OperatorBox.Status != nil && len(crd.OperatorBox.Status.Fields) > 0 {
			fieldNames := make([]string, 0, len(crd.OperatorBox.Status.Fields))
			for _, f := range crd.OperatorBox.Status.Fields {
				fieldNames = append(fieldNames, f.Path)
			}
			fmt.Printf("%s%s  %s\n", indent, cyan("status:"),
				strings.Join(fieldNames, ", "))
		}

		// Autoscale
		if crd.AutoscaleEnabled() && crd.OperatorBox.Autoscale != nil {
			a := crd.OperatorBox.Autoscale
			if a.Profile != "" {
				fmt.Printf("%s%s  profile=%s\n", indent, magenta("autoscale:"), a.Profile)
			} else {
				parts := []string{}
				if len(a.Conditions.When) > 0 {
					triggers := make([]string, 0, len(a.Conditions.When))
					for _, c := range a.Conditions.When {
						triggers = append(triggers, c.Field)
					}
					parts = append(parts, "when("+strings.Join(triggers, ", ")+")")
				}
				if a.Do.Workers != nil {
					parts = append(parts, fmt.Sprintf("workers→%d", *a.Do.Workers))
				}
				if a.Do.Resync != nil {
					parts = append(parts, fmt.Sprintf("resync→%s", a.Do.Resync.String()))
				}
				fmt.Printf("%s%s  %s  interval:%s  cooldown:%s\n",
					indent, magenta("autoscale:"),
					strings.Join(parts, "  "),
					a.EffectiveInterval(), a.EffectiveCooldown(),
				)
			}
		}

		fmt.Println()
	}

	fmt.Printf("  %s\n\n", green("✓ Katalog is valid"))
}

// ── printDependencyGraph ──────────────────────────────────────────────────────

// printDependencyGraph prints a two-part dependency view:
// 1. Ordered startup list (flat)
// 2. Tree view showing the dependency hierarchy
func printDependencyGraph(crds map[string]orktypes.CRDEntry, g *katalog.DependencyGraph, startupOrder []string) {
	fmt.Printf("\n%s\n\n", cyan(bold("Dependency Graph")))

	// ── Part 1: startup order list ────────────────────────────────────────────
	fmt.Printf("  %s\n", bold("Startup order:"))
	for i, name := range startupOrder {
		crd, ok := crds[name]
		if !ok {
			continue
		}
		deps := crd.DependsOn.Names()
		depStr := ""
		if len(deps) > 0 {
			formatted := make([]string, 0, len(deps))
			for depName, d := range crd.DependsOn {
				if d.Condition != "" && d.Condition != "started" {
					formatted = append(formatted, fmt.Sprintf("%s(%s)",
						yellow(depName),
						dim(d.Condition)))
				} else {
					formatted = append(formatted, yellow(depName))
				}
			}
			depStr = fmt.Sprintf("  %s %s", dim("←"), strings.Join(formatted, ", "))
		}
		gvk := fmt.Sprintf("%s/%s, Kind=%s", crd.APITypes.Group, crd.APITypes.Version, crd.APITypes.Kind)
		fmt.Printf("    %s %-20s %s%s\n",
			bold(fmt.Sprintf("%d.", i+1)),
			name,
			dim(gvk),
			depStr,
		)
	}

	fmt.Println()

	// ── Part 2: tree view ─────────────────────────────────────────────────────
	fmt.Printf("  %s\n", bold("Tree view:"))

	// Find roots (CRDs with no dependencies)
	roots := []string{}
	for _, name := range startupOrder {
		crd, ok := crds[name]
		if !ok {
			continue
		}
		if len(crd.DependsOn) == 0 {
			roots = append(roots, name)
		}
	}

	printed := map[string]bool{}
	for _, root := range roots {
		printGraphNode(crds, g, root, "    ", "", printed)
	}
	fmt.Println()
}

// printGraphNode recursively prints one CRD node and its dependents in tree form.
func printGraphNode(crds map[string]orktypes.CRDEntry, g *katalog.DependencyGraph, name, indent, connector string, printed map[string]bool) {
	crd, ok := crds[name]
	if !ok {
		return
	}

	gvk := fmt.Sprintf("%s/%s", crd.APITypes.Group, crd.APITypes.Version)
	fmt.Printf("%s%s%s  %s\n",
		indent, connector,
		bold(name),
		dim(gvk),
	)

	if printed[name] {
		return
	}
	printed[name] = true

	dependents := g.GetDependents(name)
	sort.Strings(dependents)

	for i, dep := range dependents {
		isLast := i == len(dependents)-1
		childConnector := "├── "
		childIndent := indent + "│   "
		if isLast {
			childConnector = "└── "
			childIndent = indent + "    "
		}

		// Show the condition for this edge
		depCrd, ok := crds[dep]
		if ok {
			if d, exists := depCrd.DependsOn[name]; exists && d.Condition != "" && d.Condition != "started" {
				childConnector += dim("("+d.Condition+")") + " "
			}
		}
		printGraphNode(crds, g, dep, childIndent, childConnector, printed)
	}
}

// ── printCRDDetail ────────────────────────────────────────────────────────────

// printCRDDetail prints the full expanded state of a single CRD as a
// human-readable document — what the runtime will use for this CRD.
func printCRDDetail(crd orktypes.CRDEntry, g *katalog.DependencyGraph) {
	fmt.Printf("\n%s\n", bold(crd.Name))
	fmt.Printf("  %s %s/%s\n", cyan("APIVersion:"), crd.APITypes.Group, crd.APITypes.Version)
	fmt.Printf("  %s %s  (plural: %s)\n", cyan("Kind:     "), crd.APITypes.Kind, crd.APITypes.Plural)
	fmt.Printf("  %s %v", cyan("Namespaced:"), crd.IsNamespaced())
	if crd.Namespace != "" {
		fmt.Printf("  (namespace: %s)", crd.Namespace)
	}
	fmt.Println()
	fmt.Printf("  %s %v\n", cyan("Enabled:  "), crd.IsEnabled())
	fmt.Printf("  %s %s\n", cyan("Mode:     "), crdModeLabel(crd))
	fmt.Println()

	// ── Runtime config ───────────────────────────────────────────────────────
	fmt.Printf("  %s\n", cyan(bold("Runtime")))
	fmt.Printf("    Workers:       %s\n", green(fmt.Sprintf("%d", crd.OperatorBox.Reconciler.Workers)))
	fmt.Printf("    Resync:        %s\n", green(crd.OperatorBox.Reconciler.Resync.String()))
	if crd.OperatorBox.Reconciler.Queue.MaxDepth > 0 {
		fmt.Printf("    MaxDepth: %s\n", green(fmt.Sprintf("%d", crd.OperatorBox.Reconciler.Queue.MaxDepth)))
	}
	fmt.Println()

	// ── DependsOn ─────────────────────────────────────────────────────────────
	if len(crd.DependsOn) > 0 {
		fmt.Printf("  %s\n", yellow(bold("DependsOn")))
		for depName, dep := range crd.DependsOn {
			cond := dep.Condition
			if cond == "" {
				cond = "started"
			}
			fmt.Printf("    - %s  condition: %s\n", yellow(depName), cond)
		}
		fmt.Println()
	}

	// ── OperatorBox.OnCreate ──────────────────────────────────────────────────
	if crd.OperatorBox.OnCreate != nil && !crd.OperatorBox.OnCreate.Empty() {
		fmt.Printf("  %s\n", cyan(bold("onCreate")))
		printHookTemplateDetail("    ", crd.OperatorBox.OnCreate)
		fmt.Println()
	}

	// ── OperatorBox.OnReconcile ───────────────────────────────────────────────
	if crd.OperatorBox.OnReconcile != nil && !crd.OperatorBox.OnReconcile.Empty() {
		fmt.Printf("  %s\n", cyan(bold("onReconcile")))
		printHookTemplateDetail("    ", crd.OperatorBox.OnReconcile)
		fmt.Println()
	}

	// ── Status ────────────────────────────────────────────────────────────────
	if crd.OperatorBox.Status != nil && len(crd.OperatorBox.Status.Fields) > 0 {
		fmt.Printf("  %s\n", cyan(bold("Status Fields")))
		for _, f := range crd.OperatorBox.Status.Fields {
			fmt.Printf("    - %s: %s\n", green(f.Path), dim(f.Value))
		}
		fmt.Println()
	}

	// ── Autoscale ─────────────────────────────────────────────────────────────
	if crd.AutoscaleEnabled() && crd.OperatorBox.Autoscale != nil {
		a := crd.OperatorBox.Autoscale
		fmt.Printf("  %s\n", magenta(bold("Autoscale")))
		if a.Profile != "" {
			fmt.Printf("    Profile:  %s\n", magenta(a.Profile))
		} else {
			fmt.Printf("    Interval: %s\n", a.EffectiveInterval())
			fmt.Printf("    Cooldown: %s\n", a.EffectiveCooldown())
			if len(a.Conditions.When) > 0 {
				fmt.Printf("    When (AND):\n")
				for _, cond := range a.Conditions.When {
					printConditionLine("      ", cond)
				}
			}
			if len(a.Conditions.Or) > 0 {
				fmt.Printf("    Or (OR):\n")
				for _, cond := range a.Conditions.Or {
					printConditionLine("      ", cond)
				}
			}
			if a.Do.Workers != nil {
				fmt.Printf("    Do.Workers:    %s\n", magenta(fmt.Sprintf("%d", *a.Do.Workers)))
			}
			if a.Do.QueueDepth != nil {
				fmt.Printf("    Do.QueueDepth: %s\n", magenta(fmt.Sprintf("%d", *a.Do.QueueDepth)))
			}
			if a.Do.Resync != nil {
				fmt.Printf("    Do.Resync:     %s\n", magenta(a.Do.Resync.String()))
			}
		}
		fmt.Println()
	}

	// ── Finalizers ────────────────────────────────────────────────────────────
	if len(crd.OperatorBox.Finalizers) > 0 {
		fmt.Printf("  %s\n", cyan(bold("Finalizers")))
		for _, f := range crd.OperatorBox.Finalizers {
			fmt.Printf("    - %s\n", f)
		}
		fmt.Println()
	}

	// ── Dependents (what depends on this CRD) ────────────────────────────────
	if g != nil {
		dependents := g.GetDependents(crd.Name)
		if len(dependents) > 0 {
			fmt.Printf("  %s\n", yellow(bold("Required by")))
			for _, dep := range dependents {
				fmt.Printf("    - %s\n", yellow(dep))
			}
			fmt.Println()
		}
	}
}

// printHookTemplateDetail prints a detailed breakdown of resource declarations.
func printHookTemplateDetail(indent string, ht *orktypes.HookTemplates) {
	if ht == nil {
		return
	}

	printResources := func(kind string, names []string) {
		if len(names) == 0 {
			return
		}
		fmt.Printf("%s%s\n", indent, green(fmt.Sprintf("%s(%d):", kind, len(names))))
		for _, n := range names {
			fmt.Printf("%s  %s\n", indent, dim("- "+n))
		}
	}

	printResources("deployments", deploymentNameList(ht.Deployments))
	printResources("statefulsets", statefulSetNameList(ht.StatefulSets))
	printResources("services", serviceNameList(ht.Services))
	printResources("configmaps", configMapNameList(ht.ConfigMaps))
	printResources("secrets", secretNameList(ht.Secrets))
	printResources("jobs", jobNameList(ht.Jobs))
	printResources("cronJobs", cronJobNameList(ht.CronJobs))
	printResources("serviceAccounts", serviceAccountNameList(ht.ServiceAccounts))
	printResources("ingresses", ingressNameList(ht.Ingresses))
	printResources("pvcs", pvcNameList(ht.PersistentVolumeClaims))
	printResources("namespaces", namespaceNameList(ht.Namespaces))
	printResources("roles", roleNameList(ht.Roles))
	printResources("clusterRoles", clusterRoleNameList(ht.ClusterRoles))

	if len(ht.CustomResource) > 0 {
		fmt.Printf("%s%s\n", indent, green(fmt.Sprintf("custom(%d):", len(ht.CustomResource))))
		for _, cr := range ht.CustomResource {
			fmt.Printf("%s  %s\n", indent, dim(fmt.Sprintf("- %s/%s  name: %s", cr.APIVersion, cr.Kind, cr.Metadata.Name)))
		}
	}
}

// ── summarizeHookTemplates ────────────────────────────────────────────────────

// summarizeHookTemplates returns a compact one-line resource summary.
func summarizeHookTemplates(ht *orktypes.HookTemplates) string {
	if ht == nil {
		return ""
	}
	var parts []string
	add := func(n int, label string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(len(ht.Deployments), "deployment(s)")
	add(len(ht.StatefulSets), "statefulset(s)")
	add(len(ht.Services), "service(s)")
	add(len(ht.ConfigMaps), "configmap(s)")
	add(len(ht.Secrets), "secret(s)")
	add(len(ht.Jobs), "job(s)")
	add(len(ht.CronJobs), "cronjob(s)")
	add(len(ht.Ingresses), "ingress(es)")
	add(len(ht.PersistentVolumeClaims), "pvc(s)")
	add(len(ht.Namespaces), "namespace(s)")
	add(len(ht.ServiceAccounts), "serviceaccount(s)")
	add(len(ht.Roles), "role(s)")
	add(len(ht.ClusterRoles), "clusterrole(s)")
	if len(ht.CustomResource) > 0 {
		kinds := make([]string, 0, len(ht.CustomResource))
		for _, cr := range ht.CustomResource {
			kinds = append(kinds, cr.Kind)
		}
		parts = append(parts, fmt.Sprintf("custom(%s)", strings.Join(kinds, ", ")))
	}
	return strings.Join(parts, ", ")
}

// ── Condition printer ─────────────────────────────────────────────────────────

func printConditionLine(indent string, cond orktypes.Condition) {
	if cond.Field != "" {
		line := fmt.Sprintf("%s- %s", indent, cond.Field)
		if cond.GreaterThan != "" {
			line += fmt.Sprintf(" > %s", dim(cond.GreaterThan))
		}
		if cond.LessThan != "" {
			line += fmt.Sprintf(" < %s", dim(cond.LessThan))
		}
		if cond.Equals != "" {
			line += fmt.Sprintf(" == %s", dim(cond.Equals))
		}
		fmt.Println(line)
	}
}

// ── Name list helpers (for printHookTemplateDetail) ───────────────────────────

func deploymentNameList(srcs []orktypes.DeploymentTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		if s.Name != "" {
			out = append(out, s.Name)
		} else {
			out = append(out, fmt.Sprintf("image:%s", s.Image))
		}
	}
	return out
}

func statefulSetNameList(srcs []orktypes.StatefulSetTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "statefulset"))
	}
	return out
}

func serviceNameList(srcs []orktypes.ServiceTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "service"))
	}
	return out
}

func configMapNameList(srcs []orktypes.ConfigMapTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "configmap"))
	}
	return out
}

func secretNameList(srcs []orktypes.SecretTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "secret"))
	}
	return out
}

func jobNameList(srcs []orktypes.JobTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "job"))
	}
	return out
}

func cronJobNameList(srcs []orktypes.CronJobTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "cronjob"))
	}
	return out
}

func serviceAccountNameList(srcs []orktypes.ServiceAccountTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "serviceaccount"))
	}
	return out
}

func ingressNameList(srcs []orktypes.IngressTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "ingress"))
	}
	return out
}

func pvcNameList(srcs []orktypes.PVCTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "pvc"))
	}
	return out
}

func namespaceNameList(srcs []orktypes.NamespaceTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "namespace"))
	}
	return out
}

func roleNameList(srcs []orktypes.RoleTemplateSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, nameOrFallback(s.Name, "role"))
	}
	return out
}

func clusterRoleNameList(srcs []orktypes.ClusterRoleTemplateSource) []string {
	out := make([]string, len(srcs))
	for i, s := range srcs {
		if s.Name != "" {
			out[i] = s.Name
		} else {
			out[i] = fmt.Sprintf("<clusterrole-%d>", i+1)
		}
	}
	return out
}

func nameOrFallback(name, fallback string) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("<%s>", fallback)
}

// ── printTypedOperatorHint ────────────────────────────────────────────────────

// printTypedBuildSteps prints the build steps for a custom runtime.
// hasMakefile=true shows the make path; false shows ork generate + go build.
func printTypedBuildSteps(hasMakefile bool) {
	if hasMakefile {
		fmt.Printf("    make registry && make build\n")
	} else {
		fmt.Printf("    ork generate registry\n")
		fmt.Printf("    go build .\n")
	}
}

// printTypedOperatorHint is called when a registry-sourced typed operator fails
// validation or simulate. Tells the user to pull the pattern, build the custom
// runtime, then re-run the same command.
func printTypedOperatorHint(err *katalog.TypedOperatorError, command string) {
	fmt.Printf("\n%s  This operator is typed — requires a custom runtime.\n\n", yellow("⚠"))
	fmt.Printf("  Pull and build, then re-run:\n")
	fmt.Printf("    ork pull %s -o .\n", err.Ref)
	printTypedBuildSteps(false) // Makefile presence unknown until pulled
	fmt.Printf("    %s\n\n", command)
}

// ── crdModeLabel ──────────────────────────────────────────────────────────────

// printKatalogDeprecation prints the deprecation notice for a locally validated
// katalog. Reads timeline state from the KatalogDeprecation methods.
// Prints nothing if the block is nil or today is before timeline.from.
func printKatalogDeprecation(d *orktypes.KatalogDeprecation) {
	printKatalogDeprecationWithHint(d, "")
}

func printKatalogDeprecationWithHint(d *orktypes.KatalogDeprecation, hint string) {
	if d == nil {
		return
	}
	today := time.Now()
	state := d.DeprecationState(today)
	if state == "none" {
		return
	}
	printDeprecationBlock(state, d.Message, d.MigratedTo, d.TimelineTo(), hint, d.DaysUntilEOL(today))
}

// printPatternDeprecation prints the deprecation notice for a registry pattern
// (inspect / pull). Reads timeline from PatternDeprecated fields.
func printPatternDeprecation(dep *registry.PatternDeprecated) {
	if dep == nil {
		return
	}
	d := &orktypes.KatalogDeprecation{
		MigratedTo: dep.MigratedTo,
		Message:    dep.Message,
	}
	if dep.TimelineFrom != "" || dep.TimelineTo != "" {
		d.Timeline = &orktypes.DeprecationTimeline{
			From: dep.TimelineFrom,
			To:   dep.TimelineTo,
		}
	}
	printKatalogDeprecation(d)
}

// printDeprecationBlock renders the deprecation block for a given state.
func printDeprecationBlock(state, message, migrateTo, eolDate, hint string, daysLeft int) {
	switch state {
	case "eol":
		fmt.Printf("\n%s  END OF LIFE\n", red("✗"))
		if eolDate != "" {
			fmt.Printf("  This pattern reached end of life on %s.\n", bold(eolDate))
		}
	default:
		fmt.Printf("\n%s  DEPRECATION WARNING\n", yellow("⚠"))
		if daysLeft > 0 {
			fmt.Printf("  End of life in %s (%s).\n", bold(fmt.Sprintf("%d days", daysLeft)), eolDate)
		} else if eolDate != "" {
			fmt.Printf("  End of life: %s.\n", bold(eolDate))
		}
	}
	if message != "" {
		fmt.Printf("  %s\n", message)
	}
	if migrateTo != "" {
		fmt.Printf("  Migrate to:  %s\n", bold(migrateTo))
	}
	if hint != "" {
		fmt.Printf("  %s\n", hint)
	}
	fmt.Println()
}

func crdModeLabel(crd orktypes.CRDEntry) string {
	if crd.DefaultReconcile() {
		return "default"
	}
	if crd.CustomHooksEnabled() {
		return fmt.Sprintf("hooks(%s)", crd.OperatorBox.Reconciler.Hooks.Function)
	}
	if crd.ConstructorEnabled() {
		return fmt.Sprintf("constructor(%s)", crd.OperatorBox.Reconciler.ConstructorDecl.Function)
	}
	if crd.Mode != "" {
		return string(crd.Mode)
	}
	return "default"
}
