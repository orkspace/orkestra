package e2e

import (
	"fmt"
	"path/filepath"
	"strings"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"gopkg.in/yaml.v3"
)

// ValidateImports checks that every file listed in imports exists and declares
// kind: E2E. baseDir is the directory that relative paths are resolved against.
// Returns one error per invalid import — callers may print all of them.
func ValidateImports(baseDir string, imports []orktypes.E2EImport) []error {
	var errs []error
	for _, imp := range imports {
		path := imp.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", imp.Path, err))
			continue
		}
		var head struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.Unmarshal(data, &head); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", imp.Path, err))
			continue
		}
		if head.Kind != "E2E" {
			errs = append(errs, fmt.Errorf("%s: expected kind E2E, got %q", imp.Path, head.Kind))
		}
		if imp.Wait != "" {
			if _, err := parseTimeDuration(imp.Wait); err != nil {
				errs = append(errs, fmt.Errorf("%s: wait %q is not a valid duration: %w", imp.Path, imp.Wait, err))
			}
		}
	}
	return errs
}

// ExpandExpectIncludes resolves include: entries in the expect list, loading each
// referenced file and splicing its entries in place. Relative paths are resolved
// against baseDir. Nested includes are not supported.
func ExpandExpectIncludes(expects []orktypes.E2EExpectation, baseDir string) ([]orktypes.E2EExpectation, error) {
	var result []orktypes.E2EExpectation
	for _, exp := range expects {
		if exp.Include == "" {
			result = append(result, exp)
			continue
		}
		path := exp.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", exp.Include, err)
		}
		var wrapper struct {
			Expect []orktypes.E2EExpectation `yaml:"expect"`
		}
		if err := yaml.Unmarshal(data, &wrapper); err != nil {
			return nil, fmt.Errorf("include %s: %w", exp.Include, err)
		}
		includeDir := filepath.Dir(path)
		for i := range wrapper.Expect {
			wrapper.Expect[i].WorkDir = includeDir
		}
		result = append(result, wrapper.Expect...)
	}
	return result, nil
}

// ValidateKubectl checks every kubectl block across all expect entries.
// Returns one error per violation — callers collect and print all of them.
func ValidateKubectl(expects []orktypes.E2EExpectation) []error {
	var errs []error
	for i, exp := range expects {
		if exp.Kubectl == nil {
			continue
		}
		loc := func(sub string, idx int) string {
			return fmt.Sprintf("expect[%d] %q kubectl.%s[%d]", i, exp.Name, sub, idx)
		}
		for j, g := range exp.Kubectl.Get {
			errs = append(errs, validateKubectlGet(loc("get", j), g)...)
		}
		for j, l := range exp.Kubectl.Logs {
			errs = append(errs, validateKubectlLogs(loc("logs", j), l)...)
		}
		for j, d := range exp.Kubectl.Describe {
			errs = append(errs, validateKubectlDescribe(loc("describe", j), d)...)
		}
		for j, e := range exp.Kubectl.Exec {
			errs = append(errs, validateKubectlExec(loc("exec", j), e)...)
		}
		for j, p := range exp.Kubectl.PortForward {
			errs = append(errs, validateKubectlPortForward(loc("port-forward", j), p)...)
		}
		for j, a := range exp.Kubectl.Apply {
			errs = append(errs, validateKubectlApply(loc("apply", j), a)...)
		}
		for j, d := range exp.Kubectl.Delete {
			errs = append(errs, validateKubectlDelete(loc("delete", j), d)...)
		}
		for j, p := range exp.Kubectl.Patch {
			errs = append(errs, validateKubectlPatch(loc("patch", j), p)...)
		}
		for j, e := range exp.Kubectl.Events {
			errs = append(errs, validateKubectlEvents(loc("events", j), e)...)
		}
		for j, a := range exp.Kubectl.Auth {
			errs = append(errs, validateKubectlAuth(loc("auth", j), a)...)
		}
		for j, c := range exp.Kubectl.Cp {
			errs = append(errs, validateKubectlCp(loc("cp", j), c)...)
		}
		for j, t := range exp.Kubectl.Top {
			errs = append(errs, validateKubectlTop(loc("top", j), t)...)
		}
		for j, r := range exp.Kubectl.Restart {
			errs = append(errs, validateKubectlRestart(loc("restart", j), r)...)
		}
		for j, s := range exp.Kubectl.Scale {
			errs = append(errs, validateKubectlScale(loc("scale", j), s)...)
		}
	}
	return errs
}

func validateKubectlGet(loc string, g orktypes.E2EKubectlGet) []error {
	var errs []error
	if g.Kind == "" {
		errs = append(errs, fmt.Errorf("%s: kind is required", loc))
	}
	if g.Name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", loc))
	}
	if g.Field == "" && g.Format == "" {
		errs = append(errs, fmt.Errorf("%s: field or format is required", loc))
	}
	if g.Format != "" && g.Format != "json" && g.Format != "yaml" {
		errs = append(errs, fmt.Errorf("%s: format must be json or yaml, got %q", loc, g.Format))
	}
	if g.JQ != "" && g.Format != "json" {
		errs = append(errs, fmt.Errorf("%s: jq requires format: json", loc))
	}
	if g.YQ != "" && g.Format != "yaml" {
		errs = append(errs, fmt.Errorf("%s: yq requires format: yaml", loc))
	}
	if !hasAssertion(kubectlGetAssertions(g)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlLogs(loc string, l orktypes.E2EKubectlLogs) []error {
	var errs []error
	if l.LeaderElection != nil {
		if l.Name != "" || l.LabelSelector != "" {
			errs = append(errs, fmt.Errorf("%s: leaderElection is mutually exclusive with name and labelSelector", loc))
		}
		if l.LeaderElection.Lease == "" {
			errs = append(errs, fmt.Errorf("%s: leaderElection.lease is required", loc))
		}
	} else if l.Name == "" && l.LabelSelector == "" {
		errs = append(errs, fmt.Errorf("%s: name, labelSelector, or leaderElection is required", loc))
	}
	if !hasAssertion(kubectlLogsAssertions(l)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlDescribe(loc string, d orktypes.E2EKubectlDescribe) []error {
	var errs []error
	if d.Kind == "" {
		errs = append(errs, fmt.Errorf("%s: kind is required", loc))
	}
	if d.Name == "" && d.LabelSelector == "" {
		errs = append(errs, fmt.Errorf("%s: name or labelSelector is required", loc))
	}
	if !hasAssertion(kubectlDescribeAssertions(d)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlExec(loc string, e orktypes.E2EKubectlExec) []error {
	var errs []error
	if e.LeaderElection != nil {
		if e.Name != "" || e.LabelSelector != "" {
			errs = append(errs, fmt.Errorf("%s: leaderElection is mutually exclusive with name and labelSelector", loc))
		}
		if e.LeaderElection.Lease == "" {
			errs = append(errs, fmt.Errorf("%s: leaderElection.lease is required", loc))
		}
	} else if e.Name == "" && e.LabelSelector == "" {
		errs = append(errs, fmt.Errorf("%s: name, labelSelector, or leaderElection is required", loc))
	}
	if len(e.Command) == 0 {
		errs = append(errs, fmt.Errorf("%s: command is required", loc))
	}
	if !hasAssertion(kubectlExecAssertions(e)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlPortForward(loc string, p orktypes.E2EKubectlPortForward) []error {
	var errs []error
	if p.LeaderElection != nil {
		if p.LeaderElection.Lease == "" {
			errs = append(errs, fmt.Errorf("%s: leaderElection.lease is required", loc))
		}
	} else if p.Service == "" && p.Pod == "" {
		errs = append(errs, fmt.Errorf("%s: service, pod, or leaderElection is required", loc))
	}
	if p.Port <= 0 {
		errs = append(errs, fmt.Errorf("%s: port must be > 0", loc))
	}
	hasAny := hasAssertion(kubectlPortForwardAssertions(p))
	if hasAny && p.Path == "" {
		errs = append(errs, fmt.Errorf("%s: path is required when assertions are set", loc))
	}
	return errs
}

func validateKubectlDelete(loc string, d orktypes.E2EKubectlDelete) []error {
	var errs []error
	if d.LeaderElection != nil {
		if d.File != "" || d.Kind != "" || d.Name != "" {
			errs = append(errs, fmt.Errorf("%s: leaderElection is mutually exclusive with file, kind, and name", loc))
		}
		if d.LeaderElection.Lease == "" {
			errs = append(errs, fmt.Errorf("%s: leaderElection.lease is required", loc))
		}
	} else {
		if d.File == "" && (d.Kind == "" || d.Name == "") {
			errs = append(errs, fmt.Errorf("%s: file or (kind + name) is required", loc))
		}
		if d.File != "" && (d.Kind != "" || d.Name != "") {
			errs = append(errs, fmt.Errorf("%s: file and kind/name are mutually exclusive", loc))
		}
	}
	return errs
}

func validateKubectlApply(loc string, a orktypes.E2EKubectlApply) []error {
	var errs []error
	if a.File == "" && a.Inline == "" {
		errs = append(errs, fmt.Errorf("%s: file or inline is required", loc))
	}
	if a.File != "" && a.Inline != "" {
		errs = append(errs, fmt.Errorf("%s: file and inline are mutually exclusive", loc))
	}
	return errs
}

func validateKubectlPatch(loc string, p orktypes.E2EKubectlPatch) []error {
	var errs []error
	if p.Kind == "" {
		errs = append(errs, fmt.Errorf("%s: kind is required", loc))
	}
	if p.Name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", loc))
	}
	if p.Patch == "" {
		errs = append(errs, fmt.Errorf("%s: patch is required", loc))
	}
	if p.Type != "" && p.Type != "merge" && p.Type != "strategic" && p.Type != "json" {
		errs = append(errs, fmt.Errorf("%s: type must be merge, strategic, or json, got %q", loc, p.Type))
	}
	return errs
}

func validateKubectlEvents(loc string, e orktypes.E2EKubectlEvents) []error {
	var errs []error
	if e.Kind == "" {
		errs = append(errs, fmt.Errorf("%s: kind is required", loc))
	}
	if e.Name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", loc))
	}
	if !hasAssertion(kubectlEventsAssertions(e)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlAuth(loc string, a orktypes.E2EKubectlAuth) []error {
	var errs []error
	if a.Verb == "" {
		errs = append(errs, fmt.Errorf("%s: verb is required", loc))
	}
	if a.Resource == "" {
		errs = append(errs, fmt.Errorf("%s: resource is required", loc))
	}
	if !hasAssertion(kubectlAuthAssertions(a)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlCp(loc string, c orktypes.E2EKubectlCp) []error {
	var errs []error
	if c.Name == "" && c.LabelSelector == "" {
		errs = append(errs, fmt.Errorf("%s: name or labelSelector is required", loc))
	}
	if c.Src == "" {
		errs = append(errs, fmt.Errorf("%s: src is required", loc))
	}
	if !hasAssertion(kubectlCpAssertions(c)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlTop(loc string, t orktypes.E2EKubectlTop) []error {
	var errs []error
	if t.Kind == "" {
		errs = append(errs, fmt.Errorf("%s: kind is required (pod or node)", loc))
	} else {
		k := strings.ToLower(t.Kind)
		if k != "pod" && k != "pods" && k != "node" && k != "nodes" {
			errs = append(errs, fmt.Errorf("%s: kind must be pod or node, got %q", loc, t.Kind))
		}
	}
	if !hasAssertion(kubectlTopAssertions(t)) {
		errs = append(errs, fmt.Errorf("%s: %s", loc, assertionListMsg))
	}
	return errs
}

func validateKubectlRestart(loc string, r orktypes.E2EKubectlRestart) []error {
	var errs []error
	if r.Kind == "" {
		errs = append(errs, fmt.Errorf("%s: kind is required", loc))
	}
	if r.Name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", loc))
	}
	return errs
}

func validateKubectlScale(loc string, s orktypes.E2EKubectlScale) []error {
	var errs []error
	if s.Kind == "" {
		errs = append(errs, fmt.Errorf("%s: kind is required", loc))
	}
	if s.Name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", loc))
	}
	return errs
}

func hasAssertion(a assertions) bool {
	return a.Equals != "" || a.NotEquals != "" || a.OutputContains != "" || a.OutputNotContains != "" ||
		a.Regex != "" || a.GreaterThan != "" || a.LessThan != "" || a.GreaterThanOrEqual != "" || a.LessThanOrEqual != "" ||
		a.Between != "" || a.NotBetween != "" || len(a.OneOf) > 0 || len(a.NotOneOf) > 0 || a.Exists || a.NotExists
}
