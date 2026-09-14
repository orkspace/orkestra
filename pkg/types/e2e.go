package types

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SetupConfig declares prerequisite resources to apply before Orkestra starts.
//
// Shorthand — a plain list of strings is equivalent to setup.apply:
//
//	setup:
//	  - ./prereqs/secret.yaml
//
// Struct form with per-entry waits:
//
//	setup:
//	  apply:
//	    - ./prereqs/namespace.yaml
//	    - path: ./prereqs/secret.yaml
//	      wait:
//	        - kind: Secret
//	          name: my-secret
//	          namespace: default
//	          timeout: 30s
//	  helm:
//	    - repo: https://charts.cert-manager.io
//	      chart: cert-manager
//	      version: v1.14.0
//	      wait:
//	        - kind: Deployment
//	          name: cert-manager
//	          namespace: cert-manager
//	          ready: true
//	          timeout: 120s
//	  wait:
//	    - kind: Deployment
//	      name: cert-manager-webhook
//	      namespace: cert-manager
//	      ready: true
//	      timeout: 120s
type SetupConfig struct {
	// Apply is an ordered list of manifests to kubectl-apply.
	// Each entry is either a plain path string or a {path, wait} struct.
	// Applied first, before helm installs, after the CRD is installed.
	Apply []SetupApplyEntry `yaml:"apply,omitempty"`

	// Helm is an ordered list of Helm charts to install before Orkestra starts.
	// Executed as helm upgrade --install — not rendered for Katalog extraction.
	Helm []SetupHelmInstall `yaml:"helm,omitempty"`

	// Wait blocks until all listed resources exist and satisfy conditions.
	// Runs after all apply and helm steps. Use per-entry wait for ordered checks.
	Wait []SetupWait `yaml:"wait,omitempty"`
}

// UnmarshalYAML allows setup: to be written as either a plain list of strings
// (backward-compatible shorthand for setup.apply) or a full struct.
func (s *SetupConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.SequenceNode {
		for _, item := range value.Content {
			if item.Kind != yaml.ScalarNode {
				break
			}
			s.Apply = append(s.Apply, SetupApplyEntry{Path: item.Value})
		}
		if len(s.Apply) > 0 {
			return nil
		}
	}
	type plain SetupConfig
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(s))
}

// SetupApplyEntry is a single manifest to kubectl-apply during setup.
// It is either a plain path string or a struct with an optional per-entry wait.
//
//	# flat form
//	- ./prereqs/secret.yaml
//
//	# structured form
//	- path: ./prereqs/secret.yaml
//	  wait:
//	    - kind: Secret
//	      name: my-secret
//	      namespace: default
//	      timeout: 30s
type SetupApplyEntry struct {
	// Path is the YAML file path to kubectl-apply.
	Path string `yaml:"path"`
	// Wait blocks after this apply until all listed resources satisfy their conditions.
	Wait []SetupWait `yaml:"wait,omitempty"`
}

// UnmarshalYAML allows a SetupApplyEntry to be written as either a plain string
// (the path) or a full {path, wait} struct.
func (e *SetupApplyEntry) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		e.Path = value.Value
		return nil
	}
	type plain SetupApplyEntry
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(e))
}

// SetupHelmInstall installs a Helm chart as a real release into the cluster.
// Unlike HelmSource (which renders charts to extract Katalog documents),
// this runs helm upgrade --install for a prerequisite chart.
type SetupHelmInstall struct {
	// Repo is the Helm repository URL. Omit for local chart paths.
	Repo string `yaml:"repo,omitempty"`
	// Chart is the chart name within the repository, or a local path (e.g. ./).
	Chart string `yaml:"chart"`
	// Release is the Helm release name. Defaults to the chart name when empty.
	Release string `yaml:"release,omitempty"`
	// Namespace for the release. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Version pins the chart version. Leave empty for latest or local charts.
	Version string `yaml:"version,omitempty"`
	// ValueFiles is an ordered list of values files (local paths or URLs).
	ValueFiles []string `yaml:"valueFiles,omitempty"`
	// Values are inline key-value overrides, equivalent to helm --set.
	Values map[string]interface{} `yaml:"values,omitempty"`
	// CreateNamespace passes --create-namespace to helm.
	CreateNamespace bool `yaml:"createNamespace,omitempty"`
	// Wait blocks after this helm install until all listed resources satisfy conditions.
	Wait []SetupWait `yaml:"wait,omitempty"`
}

// ReleaseName returns the effective Helm release name.
func (h SetupHelmInstall) ReleaseName() string {
	if h.Release != "" {
		return h.Release
	}
	return h.Chart
}

// EffectiveNamespace returns the effective namespace, defaulting to "default".
func (h SetupHelmInstall) EffectiveNamespace() string {
	if h.Namespace != "" {
		return h.Namespace
	}
	return "default"
}

// IsLocalChart reports whether the chart field is a local filesystem path.
func (h SetupHelmInstall) IsLocalChart() bool {
	return strings.HasPrefix(h.Chart, "./") || strings.HasPrefix(h.Chart, "/") || h.Chart == "."
}

// Validate returns an error when required fields are missing.
func (h SetupHelmInstall) Validate() error {
	if h.Chart == "" {
		return fmt.Errorf("setup.helm: chart is required")
	}
	if !h.IsLocalChart() && h.Repo == "" {
		return fmt.Errorf("setup.helm: repo is required for remote charts")
	}
	return nil
}

// SetupWait describes a single resource to wait for before the operator starts.
type SetupWait struct {
	// Kind is the Kubernetes resource kind (e.g. Deployment, Secret, Namespace).
	Kind string `yaml:"kind"`
	// Name is the exact resource name.
	Name string `yaml:"name"`
	// Namespace to look in. Omit for cluster-scoped resources.
	Namespace string `yaml:"namespace,omitempty"`
	// Ready waits for an available/ready condition, not just existence.
	Ready bool `yaml:"ready,omitempty"`
	// Timeout is a Go duration string. Default: "30s".
	Timeout string `yaml:"timeout,omitempty"`
}

// E2E is the top-level document type for declarative end-to-end tests.
// Committed alongside the katalog, it drives `ork e2e` — the same command
// that runs locally, in CI, and inside the GitHub Action.
type E2E struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   E2EMeta     `yaml:"metadata"`
	Spec       E2ESpec     `yaml:"spec"`
	Imports    []E2EImport `yaml:"imports,omitempty"`
}

// E2EImport references another E2E file to run after this one completes.
// By default imports share the same cluster. Set freshCluster: true to
// provision a new cluster for that import instead.
//
// Shorthand — a plain path string is equivalent to {path: <string>}:
//
//	imports:
//	  - ./auth-e2e.yaml
//	  - ./rbac-e2e.yaml
//	  - path: ./infra-e2e.yaml
//	    freshCluster: true
type E2EImport struct {
	// Path is the path to another E2E spec file (must be kind: E2E).
	Path string `yaml:"path"`
	// FreshCluster provisions a new kind cluster for this import instead of
	// reusing the parent's cluster. Default: false (share parent cluster).
	FreshCluster bool `yaml:"freshCluster,omitempty"`
	// Wait is an optional duration to sleep before this import starts.
	// Useful when the previous test leaves cluster state that needs time to
	// clear — webhook deregistration, namespace termination, cert provisioning.
	// Must be a valid Go duration string (e.g. "10s", "1m30s").
	Wait string `yaml:"wait,omitempty"`
}

// UnmarshalYAML allows imports to be written as a plain string path.
func (i *E2EImport) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		i.Path = value.Value
		return nil
	}
	type plain E2EImport
	return value.Decode((*plain)(i))
}

type E2EMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

// CustomTarget identifies the runtime environment being tested when Orkestra
// is not the operator. Validation rejects values outside the known set.
type CustomTarget string

const (
	// CustomTargetKubernetes tests a workload that runs on Kubernetes — an operator,
	// Helm chart, or raw manifests. Orkestra manages the cluster lifecycle and
	// assertions; bundle generation and Orkestra helm install/uninstall are skipped.
	CustomTargetKubernetes CustomTarget = "kubernetes"

	// CustomTargetContainer tests a container image directly without a cluster.
	// Future: build image → run container gate → embed OCI annotations → push.
	// Not yet implemented; validation accepts the value but no runner path exists.
	CustomTargetContainer CustomTarget = "container"
)

// String implements fmt.Stringer.
func (t CustomTarget) String() string { return string(t) }

// E2ECustomConfig declares the target runtime when testing non-Orkestra workloads.
type E2ECustomConfig struct {
	// Target is the runtime environment under test. Supported values: "kubernetes".
	// "container" is reserved for future use.
	Target CustomTarget `yaml:"target"`
}

type E2ESpec struct {
	// Core operator spec — the three files that define every Orkestra operator.

	// Katalog is the path to the katalog.yaml file.
	// Optional when spec.custom.target is set.
	Katalog string `yaml:"katalog,omitempty"`
	// CRD is the path to the CRD YAML file for this operator.
	// Applied before the bundle and before Orkestra starts.
	CRD string `yaml:"crd,omitempty"`
	// CRDFiles is a list of additional CRD YAML files. Applied in declaration order
	// after CRD. Use when the operator manages multiple custom resource types.
	CRDFiles []string `yaml:"crdFiles,omitempty"`
	// CR is the path to the CR YAML file to apply during the test.
	CR string `yaml:"cr,omitempty"`
	// CRFiles is a list of additional CR YAML files. Applied in declaration order
	// after CR. Use when the test needs multiple CR instances or kinds.
	CRFiles []string `yaml:"crFiles,omitempty"`

	// Custom declares the target runtime for this e2e test when it is not an
	// Orkestra-managed operator. Orkestra still owns the cluster lifecycle, setup,
	// CR apply, assertions, and cleanup — only the bundle generation and
	// Orkestra helm install/uninstall are skipped.
	//
	// Use this when your operator, Helm chart, or any Kubernetes workload is
	// installed via setup.helm or is already present in the cluster.
	// See documentation/reference/schema/04-e2e/05-custom-target.md.
	Custom *E2ECustomConfig `yaml:"custom,omitempty"`

	// Init uses an example pack — for Orkestra's own CI.
	Init *E2EInit `yaml:"init,omitempty"`

	// Cluster controls which cluster to use.
	Cluster E2ECluster `yaml:"cluster"`

	// Setup declares prerequisite resources to apply before Orkestra starts.
	// Shorthand: a plain list of strings applies each file (backward compatible).
	// Struct form adds helm installs and resource waiting.
	Setup *SetupConfig `yaml:"setup,omitempty"`

	// ValuesFiles is a list of Helm values files passed to the Orkestra chart
	// installation during e2e. Paths are relative to the e2e.yaml file.
	// Use this to configure custom runtime images or any other Helm values
	// without requiring --values flags on the command line — useful when e2e
	// runs automatically during ork push.
	ValuesFiles []string `yaml:"valuesFiles,omitempty"`

	// Notes declares user-defined note functions available in when:/or: expressions.
	// Same syntax as a Katalog notes block — same FuncMap registration, same functions.
	Notes NoteRegistry `yaml:"notes,omitempty"`

	// Expect is the list of expectations to check after each lifecycle event.
	Expect []E2EExpectation `yaml:"expect"`

	// OnFailure declares kubectl operations and commands to run and print to the
	// terminal when any expectation fails. Uses the same kubectl: DSL as expect
	// entries — assertions are never evaluated; output is always printed.
	OnFailure *E2EOnFailure `yaml:"onFailure,omitempty"`
}

// AllCRPaths returns all CR file paths in declaration order (cr then crFiles).
func (s *E2ESpec) AllCRPaths() []string {
	var paths []string
	if s.CR != "" {
		paths = append(paths, s.CR)
	}
	paths = append(paths, s.CRFiles...)
	return paths
}

// AllCRDPaths returns all CRD file paths in declaration order (crd then crdFiles).
func (s *E2ESpec) AllCRDPaths() []string {
	var paths []string
	if s.CRD != "" {
		paths = append(paths, s.CRD)
	}
	paths = append(paths, s.CRDFiles...)
	return paths
}

// E2EOnFailure declares diagnostic operations to run when any expectation fails.
// kubectl: accepts the same DSL as expect entries; assertion fields are ignored.
// commands: are raw shell strings run via sh -c.
type E2EOnFailure struct {
	Kubectl  *E2EKubectl `yaml:"kubectl,omitempty"`
	Commands []string    `yaml:"commands,omitempty"`
}

// E2EInit selects a built-in example pack as the test source.
type E2EInit struct {
	Pack    string `yaml:"pack"`
	Example string `yaml:"example"`
}

// E2ECluster controls cluster creation and selection.
type E2ECluster struct {
	// Provider is the cluster provider — currently only "kind" is supported.
	Provider string `yaml:"provider"` // default: "kind"
	// Name is the kind cluster name. Default: "ork-e2e".
	Name string `yaml:"name"`
	// Reuse controls whether an existing cluster is reused or recreated.
	// false (default) — delete and recreate for a clean state.
	// true — reuse existing cluster if it exists.
	Reuse bool `yaml:"reuse"`
}

// E2EAfter is the lifecycle event that triggers an expectation block.
type E2EAfter string

const (
	// AfterSetupComplete runs the expectation after all setup steps finish,
	// before the CR is applied. Use for infrastructure assertions.
	AfterSetupComplete E2EAfter = "setup-complete"

	// AfterCRApplied runs the expectation after the CR is applied to the cluster.
	AfterCRApplied E2EAfter = "cr-applied"

	// AfterCRDeleted runs the expectation after the CR is deleted from the cluster.
	AfterCRDeleted E2EAfter = "cr-deleted"
)

// ValidAfterValues is the set of valid values for E2EExpectation.After.
var ValidAfterValues = []E2EAfter{AfterSetupComplete, AfterCRApplied, AfterCRDeleted}

// E2EExpectation is one named assertion block.
type E2EExpectation struct {
	// Include is a path to a YAML file containing a list of E2EExpectation entries.
	// When set, the file is loaded and its entries are expanded in place of this entry.
	// All other fields must be empty when include is set.
	// Relative paths resolve from the e2e.yaml directory.
	Include string `yaml:"include,omitempty"`

	// Name is printed in the results table.
	Name string `yaml:"name"`
	// After is the lifecycle event that triggers this expectation.
	// Valid values: "setup-complete", "cr-applied", "cr-deleted".
	After E2EAfter `yaml:"after"`
	// Timeout is the maximum time to wait for the expectation to pass.
	Timeout string `yaml:"timeout"` // e.g. "60s"
	// Wait is an optional duration to sleep before the polling loop starts.
	// Useful when the previous step needs time to take effect before assertions
	// should begin — e.g. waiting for enough reconcile cycles to accumulate.
	// Must be a valid Go duration string (e.g. "10s", "30s").
	Wait string `yaml:"wait,omitempty"`

	// Resources is a unified list of resource checks across any kind.
	Resources []E2EResourceCheck `yaml:"resources,omitempty"`

	// Commands are shell commands checked in the same polling loop as resources.
	Commands []E2ECommand `yaml:"commands,omitempty"`

	// Kubectl is the structured kubectl DSL block.
	// An alternative to commands: for common kubectl operations — get, logs,
	// describe, exec, port-forward. Compiles to kubectl invocations internally.
	// Use commands: for anything that doesn't fit a subcommand.
	Kubectl *E2EKubectl `yaml:"kubectl,omitempty"`

	// When / Or gate this expectation using the same []Condition type as
	// katalog when:/or:. An expectation whose conditions do not pass is
	// skipped — not failed. Notes declared in spec.notes are available as
	// template expressions: field: '{{ inBusinessHours }}'.
	// Empty blocks always pass.
	When []Condition `yaml:"when,omitempty"`
	Or   []Condition `yaml:"or,omitempty"`

	// OnFailure declares diagnostic operations to run immediately when this
	// specific expectation fails. Runs before moving to the next expectation.
	OnFailure *E2EOnFailure `yaml:"onFailure,omitempty"`

	// WorkDir is the directory used to resolve relative file paths in this
	// expectation. Set by ExpandExpectIncludes to the directory of the include
	// file; empty means use the e2e.yaml directory.
	WorkDir string `yaml:"-"`
}

// E2EResourceCheck asserts the state of any Kubernetes resource.
// Set kind to: Deployment, Service, Secret, ConfigMap.
type E2EResourceCheck struct {
	// Kind is the resource kind: Deployment, Service, Secret, ConfigMap.
	Kind string `yaml:"kind"`
	// Name is the resource name. Empty means any resource of this kind in the namespace.
	Name string `yaml:"name,omitempty"`
	// Namespace to look in. Empty means "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Count asserts the exact number of matching resources. nil means "at least 1".
	// Set to 0 to assert none exist (cleanup check).
	Count *int `yaml:"count,omitempty"`
	// Ready asserts at least one available replica (Deployments only).
	Ready bool `yaml:"ready,omitempty"`
}

// E2ECommand runs a shell command and asserts its result.
type E2ECommand struct {
	// Run is a shell command string executed via sh -c.
	Run string `yaml:"run"`
	// ExitCode is the expected exit code. Default 0 (success).
	// Set to non-zero to assert the command must fail — useful for
	// admission webhook rejection tests.
	ExitCode int `yaml:"exitCode,omitempty"`
	// Equals asserts the combined stdout+stderr (trimmed) exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the combined stdout+stderr (trimmed) does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the combined stdout+stderr contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the combined stdout+stderr does NOT contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the combined stdout+stderr (trimmed) matches this RE2 pattern.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output (trimmed, parsed as a number) is within an
	// inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectl is the structured kubectl DSL block.
// Sits alongside resources: and commands: in each expect entry.
// Each subcommand maps directly to the kubectl command people already know.
type E2EKubectl struct {
	// Get asserts field values on Kubernetes resources.
	// Generates: kubectl get <kind> <name> -n <ns> -o jsonpath='<field>'
	Get []E2EKubectlGet `yaml:"get,omitempty"`
	// Logs asserts container log output.
	// Generates: kubectl logs -n <ns> -l <selector> -c <container> --since=<since>
	Logs []E2EKubectlLogs `yaml:"logs,omitempty"`
	// Describe asserts kubectl describe output — useful for events and conditions.
	// Generates: kubectl describe <kind> <name> -n <ns>
	Describe []E2EKubectlDescribe `yaml:"describe,omitempty"`
	// Exec runs a command inside a running container and asserts its output.
	// Generates: kubectl exec -n <ns> <pod> -c <container> -- <command>
	Exec []E2EKubectlExec `yaml:"exec,omitempty"`
	// PortForward opens a port-forward to a service or pod, makes an HTTP request,
	// and asserts the response. EnsureCurl is called automatically when any entry
	// declares a path. Generates: kubectl port-forward + curl.
	PortForward []E2EKubectlPortForward `yaml:"port-forward,omitempty"`
	// Apply applies manifests from a file path or inline YAML/JSON content.
	// Generates: kubectl apply -f <file>  or  echo '<inline>' | kubectl apply -f -
	Apply []E2EKubectlApply `yaml:"apply,omitempty"`
	// Delete deletes resources by file path or by resource identity.
	// Generates: kubectl delete -f <file>  or  kubectl delete <kind> <name> -n <ns>
	Delete []E2EKubectlDelete `yaml:"delete,omitempty"`
	// Patch patches a Kubernetes resource with a merge, strategic, or JSON patch.
	// Generates: kubectl patch <kind> <name> -n <ns> --type=<type> -p '<patch>'
	Patch []E2EKubectlPatch `yaml:"patch,omitempty"`
	// Events lists Kubernetes events for a specific resource and asserts the output.
	// Generates: kubectl events --for=<kind>/<name> -n <ns>
	Events []E2EKubectlEvents `yaml:"events,omitempty"`
	// Auth checks permissions via kubectl auth can-i and asserts the result.
	// Generates: kubectl auth can-i <verb> <resource> [-n <ns>] [--as <as>]
	Auth []E2EKubectlAuth `yaml:"auth,omitempty"`
	// Cp copies a file out of a container and asserts its content.
	// Generates: kubectl cp <ns>/<pod>:<src> <tempfile>
	Cp []E2EKubectlCp `yaml:"cp,omitempty"`
	// Top queries live CPU and memory usage via kubectl top and asserts the output.
	// Requires metrics-server; installed automatically when entries are present.
	// Generates: kubectl top <kind> [-n <ns>] [<name> | -l <selector>] [--containers]
	Top []E2EKubectlTop `yaml:"top,omitempty"`

	// Restart triggers a rollout restart of a Deployment, StatefulSet, or DaemonSet.
	// Generates: kubectl rollout restart <kind>/<name> -n <ns>
	// Waits for the rollout to complete when ready: true (default).
	// The expect step's timeout governs how long to wait.
	Restart []E2EKubectlRestart `yaml:"restart,omitempty"`

	// Scale sets the replica count on a Deployment, StatefulSet, or ReplicaSet.
	// Generates: kubectl scale <kind>/<name> --replicas=<n> -n <ns>
	// Waits for the rollout to complete when ready: true (default).
	// The expect step's timeout governs how long to wait.
	Scale []E2EKubectlScale `yaml:"scale,omitempty"`
}

// E2EKubectlGet asserts a field value on a Kubernetes resource.
type E2EKubectlGet struct {
	// Kind is the Kubernetes resource kind (e.g. Deployment, ConfigMap).
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Field is a jsonpath expression to extract before asserting.
	// e.g. .spec.template.spec.containers[0].resources.requests.cpu
	Field string `yaml:"field,omitempty"`
	// Format outputs the full resource as yaml or json for looser substring assertions.
	// Ignored when field is set. Use with jq (json) or yq (yaml) for structured extraction.
	Format string `yaml:"format,omitempty"` // yaml | json
	// JQ is a jq expression applied to the output before asserting. Requires format: json.
	JQ string `yaml:"jq,omitempty"`
	// YQ is a yq expression applied to the output before asserting. Requires format: yaml.
	YQ string `yaml:"yq,omitempty"`
	// Equals asserts the output exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlLogs asserts container log output.
type E2EKubectlLogs struct {
	// Name is the pod name. Use LabelSelector or LeaderElection instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects pods by label (e.g. "app=my-service").
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// LeaderElection resolves the target pod from a Kubernetes Lease holder.
	// Mutually exclusive with Name and LabelSelector.
	LeaderElection *E2EKubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Container name. Defaults to the first container.
	Container string `yaml:"container,omitempty"`
	// Since limits log output to the given duration (e.g. "30s", "2m").
	Since string `yaml:"since,omitempty"`
	// JQ is a jq expression applied to each log line before asserting.
	// Useful when containers emit structured JSON logs.
	JQ string `yaml:"jq,omitempty"`
	// Equals asserts the output exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	// Useful for asserting no FATAL or ERROR lines were logged.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlDescribe asserts kubectl describe output.
// Useful for checking events, conditions, and resource details.
type E2EKubectlDescribe struct {
	// Kind is the Kubernetes resource kind.
	Kind string `yaml:"kind"`
	// Name is the resource name. Use LabelSelector to match by label instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects resources by label.
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Equals asserts the output exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlExec runs a command inside a running container and asserts its output.
type E2EKubectlExec struct {
	// Name is the pod name. Use LabelSelector or LeaderElection to match instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects the pod by label.
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// LeaderElection resolves the target pod from a Kubernetes Lease holder.
	// Mutually exclusive with name and labelSelector.
	LeaderElection *E2EKubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Container name. Defaults to the first container.
	Container string `yaml:"container,omitempty"`
	// Command is the command to execute inside the container.
	Command []string `yaml:"command"`
	// JQ is a jq expression applied to the output before asserting.
	JQ string `yaml:"jq,omitempty"`
	// YQ is a yq expression applied to the output before asserting.
	YQ string `yaml:"yq,omitempty"`
	// Equals asserts the output exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlApply applies one or more manifests during an expect checkpoint.
// Use file to reference a path on disk or inline to embed the manifest directly.
// kubectl apply is idempotent so re-running inside the poll loop is safe.
type E2EKubectlApply struct {
	// File is a path to a manifest file. Relative paths resolve from the e2e.yaml directory.
	// Generates: kubectl apply -f <file>
	File string `yaml:"file,omitempty"`
	// Inline is a raw YAML or JSON manifest string applied via stdin.
	// Generates: echo '<inline>' | kubectl apply -f -
	Inline string `yaml:"inline,omitempty"`
	// Namespace overrides the namespace for resources that don't declare one.
	Namespace string `yaml:"namespace,omitempty"`

	// ExitCode is the expected exit code. Default 0 (success). Set to
	// non-zero to assert the apply must be rejected — e.g. an admission
	// webhook denial — instead of treating any failure as a broken test.
	// Same convention as E2ECommand.ExitCode.
	ExitCode int `yaml:"exitCode,omitempty"`
	// Equals asserts the combined stdout+stderr (trimmed) exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the combined stdout+stderr (trimmed) does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the combined stdout+stderr contains this substring —
	// e.g. the admission webhook's denial message.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the combined stdout+stderr does NOT contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the combined stdout+stderr (trimmed) matches this RE2 pattern.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output (trimmed, parsed as a number) is within an
	// inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlDelete deletes resources during an expect checkpoint.
// Use file to delete all resources in a manifest, kind+name for a single resource,
// or leaderElection to delete the current leader pod by resolving the Lease holder.
type E2EKubectlDelete struct {
	// File is a path to a manifest file. All resources in the file are deleted.
	// Relative paths resolve from the e2e.yaml directory.
	// Generates: kubectl delete -f <file>
	File string `yaml:"file,omitempty"`
	// Kind is the Kubernetes resource kind. Used with name for single-resource deletion.
	Kind string `yaml:"kind,omitempty"`
	// Name is the resource name.
	Name string `yaml:"name,omitempty"`
	// Namespace to target. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// LeaderElection resolves the pod to delete by reading the holder of a Kubernetes Lease.
	// Mutually exclusive with file, kind, and name.
	LeaderElection *E2EKubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// IgnoreNotFound silences errors when the resource does not exist.
	IgnoreNotFound bool `yaml:"ignoreNotFound,omitempty"`
}

// E2EKubectlPatch patches a Kubernetes resource in-place.
// Useful for triggering state transitions (e.g. updating a field to drive a state machine).
type E2EKubectlPatch struct {
	// Kind is the Kubernetes resource kind (e.g. Deployment, MyResource).
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to target. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Type is the patch strategy: merge (default), strategic, or json.
	Type string `yaml:"type,omitempty"` // merge | strategic | json
	// Patch is the patch content as a YAML or JSON string.
	Patch string `yaml:"patch"`
}

// E2EKubectlEvents lists Kubernetes events for a specific resource and asserts
// the output. Useful for verifying that the operator emitted expected events
// (e.g. Reconciled, BackOff) or that no error events occurred.
type E2EKubectlEvents struct {
	// Kind is the Kubernetes resource kind to filter events for.
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Equals asserts the output exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlAuth checks permissions via kubectl auth can-i and asserts the result.
// Useful for verifying that the operator created the correct RBAC resources.
type E2EKubectlAuth struct {
	// Verb is the action to check (e.g. get, list, create, delete).
	Verb string `yaml:"verb"`
	// Resource is the Kubernetes resource type (e.g. pods, deployments, secrets).
	Resource string `yaml:"resource"`
	// Namespace scopes the check. Omit for cluster-scoped checks.
	Namespace string `yaml:"namespace,omitempty"`
	// As is the user or service account to impersonate.
	// Use the full service account form: system:serviceaccount:<ns>:<name>
	As string `yaml:"as,omitempty"`
	// Equals asserts the output exactly matches this string. Typically "yes" or "no".
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlCp copies a file out of a running container and asserts its content.
// Resolves the pod by name or label selector, copies the file to a temp path,
// reads it, applies assertions, and cleans up.
type E2EKubectlCp struct {
	// Name is the pod name. Use LabelSelector to match by label instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects the pod by label (e.g. "app=my-service").
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Container name. Defaults to the first container.
	Container string `yaml:"container,omitempty"`
	// Src is the path inside the container to copy from.
	Src string `yaml:"src"`
	// JQ is a jq expression applied to the file content before asserting.
	JQ string `yaml:"jq,omitempty"`
	// YQ is a yq expression applied to the file content before asserting.
	YQ string `yaml:"yq,omitempty"`
	// Equals asserts the file content (trimmed) exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the file content does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the file content contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the file content does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlTop queries live CPU and memory usage via kubectl top and asserts
// the output. Requires metrics-server in the cluster; the runner installs it
// automatically (via Helm) when any top entry is present.
type E2EKubectlTop struct {
	// Kind is the resource type to query: "pod" or "node".
	Kind string `yaml:"kind"`
	// Name is the pod or node name. Omit to list all.
	Name string `yaml:"name,omitempty"`
	// LabelSelector filters pods by label (e.g. "app=my-service"). Pods only.
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// Namespace to query. Applies to pods only. Default: "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Containers shows per-container metrics (--containers). Pods only.
	Containers bool `yaml:"containers,omitempty"`
	// Equals asserts the output (trimmed) exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
}

// E2EKubectlLeaderElection resolves the target pod by reading the current holder
// from a Kubernetes Lease object. Used by port-forward, logs, delete, and exec
// to pin commands to the elected leader rather than a random replica.
type E2EKubectlLeaderElection struct {
	// Lease is the name of the Kubernetes Lease object that tracks leader election.
	Lease string `yaml:"lease"`
	// Namespace of the lease. Defaults to the port-forward namespace.
	Namespace string `yaml:"namespace,omitempty"`
}

// E2EKubectlPortForward opens a port-forward to a service or pod, makes an HTTP
// request via curl, and asserts the response. The port-forward lifecycle
// (background process, wait for port, cleanup) is handled by the runner.
// EnsureCurl is called automatically when any entry declares a path.
type E2EKubectlPortForward struct {
	// Service is the service name to port-forward to. Optional when leaderElection is set.
	Service string `yaml:"service,omitempty"`
	// Pod is the pod name to port-forward to. Optional when leaderElection is set.
	Pod string `yaml:"pod,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// LeaderElection resolves the port-forward target from a Kubernetes Lease object.
	// When set, service and pod are ignored — the current lease holder is used instead.
	// The lease namespace defaults to the port-forward namespace.
	LeaderElection *E2EKubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// Port is the service or pod port to forward.
	Port int `yaml:"port"`
	// Path is the HTTP path to request after the port-forward is ready.
	// Triggers EnsureCurl pre-flight when set.
	Path string `yaml:"path,omitempty"`
	// Method is the HTTP method. Defaults to GET.
	Method string `yaml:"method,omitempty"`
	// Headers are added to the request, e.g. Authorization for a token-gated
	// endpoint like the Gateway API. Values go through os.ExpandEnv
	// (same ${VAR} syntax as gateway.api.auth.tokens.token), so a CI
	// secret never needs to be written into the YAML file.
	//
	//	headers:
	//	  Authorization: "Bearer ${ORK_CI_TOKEN}"
	Headers map[string]string `yaml:"headers,omitempty"`
	// Body is sent as the request body for methods that accept one (POST,
	// PUT, PATCH). Ignored for GET/HEAD/DELETE. Goes through os.ExpandEnv,
	// same as Headers.
	Body string `yaml:"body,omitempty"`
	// Wait is an optional duration to sleep after the port-forward is ready
	// but before the curl request is made. Useful when the endpoint needs a
	// moment to stabilise after the connection is established.
	// Must be a valid Go duration string (e.g. "2s", "5s").
	Wait string `yaml:"wait,omitempty"`
	// JQ is a jq expression to extract from the response before asserting.
	// e.g. .workers  or  .items[0].status
	JQ string `yaml:"jq,omitempty"`
	// YQ is a yq expression to extract from the response before asserting.
	// Use when the endpoint returns YAML instead of JSON.
	YQ string `yaml:"yq,omitempty"`
	// Equals asserts the output exactly matches this string.
	Equals string `yaml:"equals,omitempty"`
	// NotEquals asserts the output does not exactly match this string.
	NotEquals string `yaml:"notEquals,omitempty"`
	// OutputContains asserts the output contains this substring.
	OutputContains string `yaml:"outputContains,omitempty"`
	// OutputNotContains asserts the output does not contain this substring.
	OutputNotContains string `yaml:"outputNotContains,omitempty"`
	// Regex asserts the output (trimmed) matches this RE2 regular expression.
	Regex string `yaml:"regex,omitempty"`
	// GreaterThan asserts the output (trimmed, parsed as a number) is greater than this value — strict.
	GreaterThan string `yaml:"greaterThan,omitempty"`
	// LessThan asserts the output (trimmed, parsed as a number) is less than this value — strict.
	LessThan string `yaml:"lessThan,omitempty"`
	// GreaterThanOrEqual asserts the output is greater than or equal to this value.
	GreaterThanOrEqual string `yaml:"greaterThanOrEqual,omitempty"`
	// LessThanOrEqual asserts the output is less than or equal to this value.
	LessThanOrEqual string `yaml:"lessThanOrEqual,omitempty"`
	// Between asserts the output is numerically within an inclusive range. Value is "min,max".
	Between string `yaml:"between,omitempty"`
	// NotBetween asserts the output is numerically outside an inclusive range. Value is "min,max".
	NotBetween string `yaml:"notBetween,omitempty"`
	// Exists asserts the output (trimmed) is non-empty.
	Exists bool `yaml:"exists,omitempty"`
	// NotExists asserts the output (trimmed) is empty.
	NotExists bool `yaml:"notExists,omitempty"`
	// OneOf asserts the output (trimmed) exactly matches one of these values.
	OneOf []string `yaml:"oneOf,omitempty"`
	// NotOneOf asserts the output (trimmed) matches none of these values.
	NotOneOf []string `yaml:"notOneOf,omitempty"`
	// StatusCode asserts the HTTP response status code. When set, the response
	// body is discarded and only the numeric status is checked (e.g. 404, 200).
	// Cannot be combined with JQ, YQ, Equals, or OutputContains.
	StatusCode int `yaml:"statusCode,omitempty"`
}

// E2EKubectlRestart triggers a rollout restart of a Deployment, StatefulSet, or DaemonSet.
type E2EKubectlRestart struct {
	// Kind is the resource kind: Deployment, StatefulSet, or DaemonSet.
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to target. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Ready waits for the rollout to complete after the restart. Defaults to true.
	Ready *bool `yaml:"ready,omitempty"`
}

// E2EKubectlScale sets the replica count on a Deployment, StatefulSet, or ReplicaSet.
type E2EKubectlScale struct {
	// Kind is the resource kind: Deployment, StatefulSet, or ReplicaSet.
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to target. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Replicas is the desired replica count.
	Replicas int `yaml:"replicas"`
	// Ready waits for the rollout to complete after scaling. Defaults to true.
	Ready *bool `yaml:"ready,omitempty"`
}
