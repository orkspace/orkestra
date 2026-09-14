package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const portForwardTimeout = 15 * time.Second

// verifyExpectation polls until all conditions pass or timeout expires.
// workDir is the working directory for command checks — relative paths in
// commands and resource file refs resolve from there.
// errSkipped is a sentinel returned when a when:/or: gate is not met.
// The runner detects it to record a skipped case rather than a failure.
var errSkipped = fmt.Errorf("skipped")

func verifyExpectation(ctx context.Context, exp orktypes.E2EExpectation, workDir string, cs kubernetes.Interface, cfg *rest.Config, noteEval orktypes.TemplateEvaluator) error {
	if !orktypes.EvaluateConditions(nil, exp.When, exp.Or, noteEval) {
		return errSkipped
	}
	if exp.Wait != "" {
		if d, err := parseTimeDuration(exp.Wait); err == nil && d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	timeout, err := parseTimeDuration(exp.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 60 * time.Second
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		if err := checkAll(ctx, exp, workDir, cs, cfg); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s waiting for %q: %w", timeout, exp.Name, checkAll(ctx, exp, workDir, cs, cfg))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func checkAll(ctx context.Context, exp orktypes.E2EExpectation, workDir string, cs kubernetes.Interface, cfg *rest.Config) error {
	// Commands run before resources so that action commands (e.g. cleanup deletes)
	// execute before the resource state is checked.
	for _, cmd := range exp.Commands {
		if err := checkCommand(ctx, cmd, workDir); err != nil {
			return err
		}
	}
	if exp.Kubectl != nil {
		if err := checkKubectl(ctx, exp.Kubectl, workDir, cs, cfg); err != nil {
			return err
		}
	}
	for _, r := range exp.Resources {
		if err := checkResource(ctx, r, workDir); err != nil {
			return err
		}
	}
	return nil
}

// checkResource asserts the state of any Kubernetes resource using kubectl.
// Kind can be any built-in or custom resource kind.
func checkResource(ctx context.Context, r orktypes.E2EResourceCheck, workDir string) error {
	ns := r.Namespace
	if ns == "" {
		ns = "default"
	}

	// count=0: assert resource(s) do NOT exist.
	// A kubectl error (CRD unknown, type not registered) also means nothing exists — pass.
	if r.Count != nil && *r.Count == 0 {
		var args []string
		if r.Name != "" {
			args = []string{"get", r.Kind, r.Name, "-n", ns, "--ignore-not-found", "-o", "name"}
		} else {
			args = []string{"get", r.Kind, "-n", ns, "--ignore-not-found", "-o", "name"}
		}
		out, err := runKubectl(ctx, workDir, args...)
		if err != nil {
			return nil // kubectl error = CRD unknown or type missing = nothing exists
		}
		if strings.TrimSpace(out) != "" {
			if r.Name != "" {
				return fmt.Errorf("%s/%s still exists in %s", r.Kind, r.Name, ns)
			}
			return fmt.Errorf("%s still exists in namespace %s:\n%s", r.Kind, ns, out)
		}
		return nil
	}

	// Named resource: assert it exists.
	if r.Name != "" {
		out, err := runKubectl(ctx, workDir, "get", r.Kind, r.Name, "-n", ns, "--ignore-not-found", "-o", "name")
		if err != nil || strings.TrimSpace(out) == "" {
			return fmt.Errorf("%s/%s not found in %s", r.Kind, r.Name, ns)
		}
		if r.Ready {
			return checkReady(ctx, workDir, r.Kind, r.Name, ns)
		}
		return nil
	}

	// Unnamed: assert at least one (or exact count) exists.
	out, err := runKubectl(ctx, workDir, "get", r.Kind, "-n", ns, "-o", "name")
	if err != nil || strings.TrimSpace(out) == "" {
		return fmt.Errorf("no %s found in namespace %s", r.Kind, ns)
	}
	if r.Count != nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != *r.Count {
			return fmt.Errorf("%s count in %s: want %d, got %d", r.Kind, ns, *r.Count, len(lines))
		}
	}
	if r.Ready {
		// Check readiness of the first result.
		first := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
		if parts := strings.SplitN(first, "/", 2); len(parts) == 2 {
			first = parts[1]
		}
		return checkReady(ctx, workDir, r.Kind, first, ns)
	}
	return nil
}

// checkReady checks whether a resource has available replicas.
// Uses jsonpath to read status.availableReplicas — covers Deployments and
// StatefulSets. Returns nil (ready) if the field is absent (e.g. Services).
func checkReady(ctx context.Context, workDir, kind, name, ns string) error {
	out, err := runKubectl(ctx, workDir,
		"get", kind, name, "-n", ns, "-o", "jsonpath={.status.availableReplicas}")
	if err != nil {
		return fmt.Errorf("%s/%s in %s: not ready (%s)", kind, name, ns, strings.TrimSpace(out))
	}
	val := strings.TrimSpace(out)
	if val == "0" {
		return fmt.Errorf("%s/%s in %s: not ready (availableReplicas=0)", kind, name, ns)
	}
	// val == "" means field doesn't exist on this kind — treat as ready (e.g. Service).
	return nil
}

func checkCommand(ctx context.Context, c orktypes.E2ECommand, workDir string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Run)
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return fmt.Errorf("running %q: %w", c.Run, err)
		}
	}

	if exitCode != c.ExitCode {
		return fmt.Errorf("command %q: expected exit code %d, got %d\noutput: %s",
			c.Run, c.ExitCode, exitCode, strings.TrimSpace(string(out)))
	}
	if err := applyAssertions(string(out), commandAssertions(c)); err != nil {
		return fmt.Errorf("command %q: %w\noutput: %s", c.Run, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// assertions holds all output assertion fields shared across e2e command and
// kubectl subcommand types. Each non-empty/non-zero field is checked
// independently — every assertion set on one entry applies, same as before.
type assertions struct {
	Equals             string
	NotEquals          string
	OutputContains     string
	OutputNotContains  string
	Regex              string
	GreaterThan        string
	LessThan           string
	GreaterThanOrEqual string
	LessThanOrEqual    string
	Between            string
	NotBetween         string
	OneOf              []string
	NotOneOf           []string
	Exists             bool
	NotExists          bool
}

// applyAssertions checks all assertion fields against output. Empty fields
// are skipped; every set field is checked independently so several can
// combine on one entry, same as before.
//
// Each check builds a single-field orktypes.Condition around the trimmed
// output and evaluates it with the same EvaluateOneCond the reconciler and
// webhook use for when:/or: — comparison logic (numeric parsing, regex,
// ranges) lives in exactly one place instead of being reimplemented here,
// and gains every operator pkg/types supports for free.
func applyAssertions(output string, a assertions) error {
	trimmed := strings.TrimSpace(output)
	data := map[string]interface{}{"output": trimmed}

	check := func(cond orktypes.Condition, format string, args ...interface{}) error {
		if !orktypes.EvaluateOneCond(data, cond, nil) {
			return fmt.Errorf(format, args...)
		}
		return nil
	}

	if a.OutputContains != "" {
		if err := check(orktypes.Condition{Field: "output", Contains: a.OutputContains},
			"output does not contain %q", a.OutputContains); err != nil {
			return err
		}
	}
	if a.OutputNotContains != "" {
		if err := check(orktypes.Condition{Field: "output", NotContains: a.OutputNotContains},
			"output must not contain %q", a.OutputNotContains); err != nil {
			return err
		}
	}
	if a.Regex != "" {
		if err := check(orktypes.Condition{Field: "output", Regex: a.Regex},
			"output %q does not match pattern %q", trimmed, a.Regex); err != nil {
			return err
		}
	}
	if a.Equals != "" {
		if err := check(orktypes.Condition{Field: "output", Equals: a.Equals},
			"output: want %q got %q", a.Equals, trimmed); err != nil {
			return err
		}
	}
	if a.NotEquals != "" {
		if err := check(orktypes.Condition{Field: "output", NotEquals: a.NotEquals},
			"output must not equal %q", a.NotEquals); err != nil {
			return err
		}
	}
	if len(a.OneOf) > 0 {
		if err := check(orktypes.Condition{Field: "output", In: strings.Join(a.OneOf, ",")},
			"output: want one of %v, got %q", a.OneOf, trimmed); err != nil {
			return err
		}
	}
	if len(a.NotOneOf) > 0 {
		if err := check(orktypes.Condition{Field: "output", NotIn: strings.Join(a.NotOneOf, ",")},
			"output: must not be one of %v, got %q", a.NotOneOf, trimmed); err != nil {
			return err
		}
	}
	if a.Exists {
		t := true
		if err := check(orktypes.Condition{Field: "output", Exists: &t},
			"output: field is empty or missing (exists assertion failed)"); err != nil {
			return err
		}
	}
	if a.NotExists {
		t := true
		if err := check(orktypes.Condition{Field: "output", NotExists: &t},
			"output: field must be absent but got %q (notExists assertion failed)", trimmed); err != nil {
			return err
		}
	}
	if a.GreaterThan != "" {
		if err := check(orktypes.Condition{Field: "output", GreaterThan: a.GreaterThan},
			"output: want > %s, got %s", a.GreaterThan, trimmed); err != nil {
			return err
		}
	}
	if a.LessThan != "" {
		if err := check(orktypes.Condition{Field: "output", LessThan: a.LessThan},
			"output: want < %s, got %s", a.LessThan, trimmed); err != nil {
			return err
		}
	}
	if a.GreaterThanOrEqual != "" {
		if err := check(orktypes.Condition{Field: "output", GreaterThanOrEqual: a.GreaterThanOrEqual},
			"output: want >= %s, got %s", a.GreaterThanOrEqual, trimmed); err != nil {
			return err
		}
	}
	if a.LessThanOrEqual != "" {
		if err := check(orktypes.Condition{Field: "output", LessThanOrEqual: a.LessThanOrEqual},
			"output: want <= %s, got %s", a.LessThanOrEqual, trimmed); err != nil {
			return err
		}
	}
	if a.Between != "" {
		if err := check(orktypes.Condition{Field: "output", Between: a.Between},
			"output: want between %s, got %s", a.Between, trimmed); err != nil {
			return err
		}
	}
	if a.NotBetween != "" {
		if err := check(orktypes.Condition{Field: "output", NotBetween: a.NotBetween},
			"output: want outside range %s, got %s", a.NotBetween, trimmed); err != nil {
			return err
		}
	}
	return nil
}

// checkKubectl runs all kubectl DSL subcommands in the block.
// Order: mutations first (apply → patch → restart → scale → delete), then assertions
// (get, logs, describe, …). This mirrors the commands: ordering rule — actions
// before checks — so that mutations take effect before assertions evaluate them.
func checkKubectl(ctx context.Context, k *orktypes.E2EKubectl, workDir string, cs kubernetes.Interface, cfg *rest.Config) error {
	for i, e := range k.Apply {
		if err := checkKubectlApply(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.apply[%d]: %w", i, err)
		}
	}
	for i, e := range k.Patch {
		if err := checkKubectlPatch(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.patch[%d]: %w", i, err)
		}
	}
	for i, e := range k.Restart {
		if err := checkKubectlRestart(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.restart[%d]: %w", i, err)
		}
	}
	for i, e := range k.Scale {
		if err := checkKubectlScale(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.scale[%d]: %w", i, err)
		}
	}
	for i, e := range k.Delete {
		if err := checkKubectlDelete(ctx, cs, e, workDir); err != nil {
			return fmt.Errorf("kubectl.delete[%d]: %w", i, err)
		}
	}
	for i, e := range k.Get {
		if err := checkKubectlGet(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.get[%d]: %w", i, err)
		}
	}
	for i, e := range k.Logs {
		if err := checkKubectlLogs(ctx, cs, e, workDir); err != nil {
			return fmt.Errorf("kubectl.logs[%d]: %w", i, err)
		}
	}
	for i, e := range k.Describe {
		if err := checkKubectlDescribe(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.describe[%d]: %w", i, err)
		}
	}
	for i, e := range k.Exec {
		if err := checkKubectlExec(ctx, cs, e, workDir); err != nil {
			return fmt.Errorf("kubectl.exec[%d]: %w", i, err)
		}
	}
	for i, e := range k.PortForward {
		if err := checkKubectlPortForward(ctx, cs, cfg, e, workDir); err != nil {
			return fmt.Errorf("kubectl.port-forward[%d]: %w", i, err)
		}
	}
	for i, e := range k.Events {
		if err := checkKubectlEvents(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.events[%d]: %w", i, err)
		}
	}
	for i, e := range k.Auth {
		if err := checkKubectlAuth(ctx, cs, e); err != nil {
			return fmt.Errorf("kubectl.auth[%d]: %w", i, err)
		}
	}
	for i, e := range k.Cp {
		if err := checkKubectlCp(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.cp[%d]: %w", i, err)
		}
	}
	for i, e := range k.Top {
		if err := checkKubectlTop(ctx, e, workDir); err != nil {
			return fmt.Errorf("kubectl.top[%d]: %w", i, err)
		}
	}
	return nil
}

func checkKubectlRestart(ctx context.Context, e orktypes.E2EKubectlRestart, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}
	target := e.Kind + "/" + e.Name
	if _, err := runKubectl(ctx, workDir, "rollout", "restart", target, "-n", ns); err != nil {
		return fmt.Errorf("kubectl rollout restart %s: %w", target, err)
	}
	if e.Ready == nil || *e.Ready {
		if out, err := runKubectl(ctx, workDir, "rollout", "status", target, "-n", ns); err != nil {
			return fmt.Errorf("kubectl rollout status %s: %s", target, out)
		}
	}
	return nil
}

func checkKubectlScale(ctx context.Context, e orktypes.E2EKubectlScale, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}
	target := e.Kind + "/" + e.Name
	replicas := fmt.Sprintf("--replicas=%d", e.Replicas)
	if _, err := runKubectl(ctx, workDir, "scale", target, replicas, "-n", ns); err != nil {
		return fmt.Errorf("kubectl scale %s: %w", target, err)
	}
	if e.Ready == nil || *e.Ready {
		if out, err := runKubectl(ctx, workDir, "rollout", "status", target, "-n", ns); err != nil {
			return fmt.Errorf("kubectl rollout status %s: %s", target, out)
		}
	}
	return nil
}

func checkKubectlDelete(ctx context.Context, cs kubernetes.Interface, e orktypes.E2EKubectlDelete, workDir string) error {
	var args []string
	if e.LeaderElection != nil {
		ns := e.Namespace
		if ns == "" {
			ns = "default"
		}
		pod, leaseNs, err := resolveLeaderHolder(ctx, cs, e.LeaderElection, ns)
		if err != nil {
			return fmt.Errorf("kubectl delete: %w", err)
		}
		args = []string{"delete", "pod", pod, "-n", leaseNs}
	} else if e.File != "" {
		file := e.File
		if !filepath.IsAbs(file) && workDir != "" {
			file = filepath.Join(workDir, file)
		}
		args = []string{"delete", "-f", file}
	} else {
		ns := e.Namespace
		if ns == "" {
			ns = "default"
		}
		args = []string{"delete", e.Kind, e.Name, "-n", ns}
	}
	if e.IgnoreNotFound {
		args = append(args, "--ignore-not-found")
	}
	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		ref := e.File
		if ref == "" {
			ref = e.Kind + "/" + e.Name
		}
		return fmt.Errorf("kubectl delete %s: %s", ref, out)
	}
	return nil
}

func checkKubectlApply(ctx context.Context, e orktypes.E2EKubectlApply, workDir string) error {
	var label string
	var out []byte
	var runErr error

	if e.Inline != "" {
		label = "kubectl apply (inline)"
		args := []string{"apply", "-f", "-"}
		if e.Namespace != "" {
			args = append(args, "-n", e.Namespace)
		}
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		if workDir != "" {
			cmd.Dir = workDir
		}
		cmd.Stdin = strings.NewReader(e.Inline)
		out, runErr = cmd.CombinedOutput()
	} else {
		file := e.File
		if !filepath.IsAbs(file) && workDir != "" {
			file = filepath.Join(workDir, file)
		}
		label = fmt.Sprintf("kubectl apply -f %s", e.File)
		args := []string{"apply", "-f", file}
		if e.Namespace != "" {
			args = append(args, "-n", e.Namespace)
		}
		var s string
		s, runErr = runKubectl(ctx, workDir, args...)
		out = []byte(s)
	}

	return assertKubectlApplyOutput(label, out, runErr, e)
}

// assertKubectlApplyOutput checks the captured exit code and output of a
// kubectl apply run against e's expectations. Split out from
// checkKubectlApply so the exit-code/assertion logic is testable without
// actually shelling out to kubectl — runErr is whatever
// exec.Cmd.CombinedOutput() (or an equivalent) returned.
func assertKubectlApplyOutput(label string, out []byte, runErr error, e orktypes.E2EKubectlApply) error {
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return fmt.Errorf("%s: %w", label, runErr)
		}
	}

	if exitCode != e.ExitCode {
		return fmt.Errorf("%s: expected exit code %d, got %d\noutput: %s",
			label, e.ExitCode, exitCode, strings.TrimSpace(string(out)))
	}
	if err := applyAssertions(string(out), kubectlApplyAssertions(e)); err != nil {
		return fmt.Errorf("%s: %w\noutput: %s", label, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func checkKubectlPatch(ctx context.Context, e orktypes.E2EKubectlPatch, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}
	patchType := e.Type
	if patchType == "" {
		patchType = "merge"
	}
	args := []string{"patch", e.Kind, e.Name, "-n", ns, "--type", patchType, "-p", e.Patch}
	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		return fmt.Errorf("kubectl patch %s/%s: %s", e.Kind, e.Name, out)
	}
	return nil
}

func checkKubectlGet(ctx context.Context, e orktypes.E2EKubectlGet, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}

	var args []string
	if e.Field != "" {
		field := e.Field
		if !strings.HasPrefix(field, ".") {
			field = "." + field
		}
		args = []string{"get", e.Kind, e.Name, "-n", ns, "-o", "jsonpath={" + field + "}"}
	} else {
		format := e.Format
		if format == "" {
			format = "json"
		}
		args = []string{"get", e.Kind, e.Name, "-n", ns, "-o", format}
	}

	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		return fmt.Errorf("kubectl get %s/%s: %s", e.Kind, e.Name, out)
	}

	out, err = applyExtract(ctx, workDir, out, e.JQ, e.YQ)
	if err != nil {
		return fmt.Errorf("kubectl get %s/%s: %w", e.Kind, e.Name, err)
	}

	return assertKubectlGetOutput(out, e)
}

// assertKubectlGetOutput applies e's assertions to already-fetched (and
// jq/yq-extracted) output. Split out from checkKubectlGet so this logic is
// testable without kubectl or a cluster.
func assertKubectlGetOutput(out string, e orktypes.E2EKubectlGet) error {
	if err := applyAssertions(out, kubectlGetAssertions(e)); err != nil {
		return fmt.Errorf("kubectl get %s/%s: %w\noutput: %s", e.Kind, e.Name, err, out)
	}
	return nil
}

// resolveLeaderHolder looks up the holder of a Kubernetes Lease and returns the
// pod name and the namespace the lease lives in. leaseNs defaults to ns when
// the LeaderElection struct leaves it empty.
func resolveLeaderHolder(ctx context.Context, cs kubernetes.Interface, le *orktypes.E2EKubectlLeaderElection, ns string) (pod, leaseNs string, err error) {
	leaseNs = le.Namespace
	if leaseNs == "" {
		leaseNs = ns
	}
	lease, err := cs.CoordinationV1().Leases(leaseNs).Get(ctx, le.Lease, metav1.GetOptions{})
	if err != nil {
		return "", leaseNs, fmt.Errorf("lease %s/%s: %w", leaseNs, le.Lease, err)
	}
	if lease.Spec.HolderIdentity == nil || strings.TrimSpace(*lease.Spec.HolderIdentity) == "" {
		return "", leaseNs, fmt.Errorf("lease %s/%s has no holder yet", leaseNs, le.Lease)
	}
	return strings.TrimSpace(*lease.Spec.HolderIdentity), leaseNs, nil
}

func checkKubectlLogs(ctx context.Context, cs kubernetes.Interface, e orktypes.E2EKubectlLogs, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}

	args := []string{"logs", "-n", ns}
	if e.LeaderElection != nil {
		pod, leaseNs, err := resolveLeaderHolder(ctx, cs, e.LeaderElection, ns)
		if err != nil {
			return fmt.Errorf("kubectl logs: %w", err)
		}
		args = []string{"logs", "-n", leaseNs, pod}
	} else if e.LabelSelector != "" {
		args = append(args, "-l", e.LabelSelector)
	} else {
		args = append(args, e.Name)
	}
	if e.Container != "" {
		args = append(args, "-c", e.Container)
	}
	if e.Since != "" {
		args = append(args, "--since="+e.Since)
	}

	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		return fmt.Errorf("kubectl logs: %s", out)
	}

	out, err = applyExtract(ctx, workDir, out, e.JQ, "")
	if err != nil {
		return fmt.Errorf("kubectl logs: %w", err)
	}

	return assertKubectlLogsOutput(out, e)
}

// assertKubectlLogsOutput applies e's assertions to already-fetched (and
// jq-extracted) log output. Split out so this logic is testable without
// kubectl or a cluster.
func assertKubectlLogsOutput(out string, e orktypes.E2EKubectlLogs) error {
	if err := applyAssertions(out, kubectlLogsAssertions(e)); err != nil {
		return fmt.Errorf("kubectl logs: %w\noutput: %s", err, out)
	}
	return nil
}

func checkKubectlDescribe(ctx context.Context, e orktypes.E2EKubectlDescribe, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}

	args := []string{"describe", e.Kind, "-n", ns}
	if e.Name != "" {
		args = append(args, e.Name)
	} else if e.LabelSelector != "" {
		args = append(args, "-l", e.LabelSelector)
	}

	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		return fmt.Errorf("kubectl describe %s: %s", e.Kind, out)
	}

	return assertKubectlDescribeOutput(out, e)
}

// assertKubectlDescribeOutput applies e's assertions to already-fetched
// describe output. Split out so this logic is testable without kubectl or
// a cluster.
func assertKubectlDescribeOutput(out string, e orktypes.E2EKubectlDescribe) error {
	if err := applyAssertions(out, kubectlDescribeAssertions(e)); err != nil {
		return fmt.Errorf("kubectl describe %s: %w\noutput: %s", e.Kind, err, out)
	}
	return nil
}

func checkKubectlExec(ctx context.Context, cs kubernetes.Interface, e orktypes.E2EKubectlExec, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}

	var pod string
	if e.LeaderElection != nil {
		var leaseNs string
		var err error
		pod, leaseNs, err = resolveLeaderHolder(ctx, cs, e.LeaderElection, ns)
		if err != nil {
			return fmt.Errorf("kubectl exec: %w", err)
		}
		ns = leaseNs
	} else if e.LabelSelector != "" {
		var err error
		pod, err = runKubectl(ctx, workDir,
			"get", "pod", "-n", ns, "-l", e.LabelSelector,
			"-o", "jsonpath={.items[0].metadata.name}")
		if err != nil || strings.TrimSpace(pod) == "" {
			return fmt.Errorf("kubectl exec: no pod found for selector %q in %s", e.LabelSelector, ns)
		}
		pod = strings.TrimSpace(pod)
	} else {
		pod = e.Name
	}

	args := []string{"exec", "-n", ns, pod}
	if e.Container != "" {
		args = append(args, "-c", e.Container)
	}
	args = append(args, "--")
	args = append(args, e.Command...)

	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		return fmt.Errorf("kubectl exec %s: %s", pod, out)
	}

	out, err = applyExtract(ctx, workDir, out, e.JQ, e.YQ)
	if err != nil {
		return fmt.Errorf("kubectl exec %s: %w", pod, err)
	}

	return assertKubectlExecOutput(pod, out, e)
}

// assertKubectlExecOutput applies e's assertions to already-captured (and
// jq/yq-extracted) exec output. Split out so this logic is testable
// without kubectl or a cluster.
func assertKubectlExecOutput(pod, out string, e orktypes.E2EKubectlExec) error {
	if err := applyAssertions(out, kubectlExecAssertions(e)); err != nil {
		return fmt.Errorf("kubectl exec %s: %w\noutput: %s", pod, err, out)
	}
	return nil
}

func checkKubectlPortForward(ctx context.Context, cs kubernetes.Interface, cfg *rest.Config, e orktypes.E2EKubectlPortForward, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}

	// Resolve to a pod — the k8s portforward API operates on pods, not services.
	var pod, podNs string
	if e.LeaderElection != nil {
		p, ln, err := resolveLeaderHolder(ctx, cs, e.LeaderElection, ns)
		if err != nil {
			return fmt.Errorf("port-forward: %w", err)
		}
		pod, podNs = p, ln
	} else if e.Service != "" {
		p, err := resolveServicePod(ctx, cs, ns, e.Service)
		if err != nil {
			return fmt.Errorf("port-forward svc/%s: %w", e.Service, err)
		}
		pod, podNs = p, ns
	} else {
		pod, podNs = e.Pod, ns
	}

	raw, err := doPortForwardGoRaw(ctx, cfg, podNs, pod, e, workDir)
	if err != nil {
		return err
	}
	if e.StatusCode != 0 {
		got, _ := strconv.Atoi(strings.TrimSpace(raw))
		if got != e.StatusCode {
			return fmt.Errorf("port-forward pod/%s%s: expected HTTP %d, got %s", pod, e.Path, e.StatusCode, raw)
		}
		return nil
	}
	return assertPortForwardOutput(ctx, workDir, raw, e, "pod/"+pod)
}

// resolveServicePod returns a running pod name backing the given service.
func resolveServicePod(ctx context.Context, cs kubernetes.Interface, ns, svcName string) (string, error) {
	svc, err := cs.CoreV1().Services(ns).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get service %s: %w", svcName, err)
	}
	if len(svc.Spec.Selector) == 0 {
		return "", fmt.Errorf("service %s has no selector", svcName)
	}
	var parts []string
	for k, v := range svc.Spec.Selector {
		parts = append(parts, k+"="+v)
	}
	pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: strings.Join(parts, ",")})
	if err != nil {
		return "", fmt.Errorf("list pods for %s: %w", svcName, err)
	}
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no running pod for service %s", svcName)
}

// doPortForwardGoRaw opens a Go port-forward to a pod, makes an HTTP request,
// and returns the raw response body (or status code string when e.StatusCode != 0).
func doPortForwardGoRaw(ctx context.Context, cfg *rest.Config, ns, pod string, e orktypes.E2EKubectlPortForward, workDir string) (string, error) {
	localPort, err := freeLocalPort()
	if err != nil {
		return "", fmt.Errorf("port-forward: free port: %w", err)
	}

	pfURL := fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s/portforward", cfg.Host, ns, pod)
	req, err := http.NewRequest(http.MethodPost, pfURL, nil)
	if err != nil {
		return "", fmt.Errorf("port-forward url: %w", err)
	}
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return "", fmt.Errorf("port-forward transport: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL)

	stopChan := make(chan struct{})
	readyChan := make(chan struct{}, 1)
	fw, err := portforward.New(dialer,
		[]string{fmt.Sprintf("%s:%d", localPort, e.Port)},
		stopChan, readyChan, io.Discard, io.Discard,
	)
	if err != nil {
		return "", fmt.Errorf("port-forward: %w", err)
	}

	fwErrCh := make(chan error, 1)
	go func() { fwErrCh <- fw.ForwardPorts() }()
	defer close(stopChan)

	select {
	case <-readyChan:
	case err := <-fwErrCh:
		return "", fmt.Errorf("port-forward pod/%s: %w", pod, err)
	case <-time.After(portForwardTimeout):
		return "", fmt.Errorf("port-forward pod/%s: timed out waiting for ready", pod)
	case <-ctx.Done():
		return "", ctx.Err()
	}

	if e.Path == "" {
		return "", nil
	}

	if e.Wait != "" {
		if d, err := parseTimeDuration(e.Wait); err == nil && d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}

	method := e.Method
	if method == "" {
		method = "GET"
	}
	reqURL := fmt.Sprintf("http://localhost:%s%s", localPort, e.Path)
	sp := startSpinner(fmt.Sprintf("%s %s (→ pod/%s)", method, reqURL, pod))

	var reqBody io.Reader
	if e.Body != "" {
		reqBody = strings.NewReader(os.ExpandEnv(e.Body))
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		sp.Stop()
		return "", fmt.Errorf("port-forward request: %w", err)
	}
	for k, v := range e.Headers {
		httpReq.Header.Set(k, os.ExpandEnv(v))
	}
	resp, err := http.DefaultClient.Do(httpReq)
	sp.Stop()
	if err != nil {
		return "", fmt.Errorf("port-forward %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if e.StatusCode != 0 {
		return strconv.Itoa(resp.StatusCode), nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("port-forward %s: reading response: %w", reqURL, err)
	}
	return strings.TrimSpace(string(body)), nil
}

// assertPortForwardOutput applies jq/yq extraction and assertions to raw curl output.
func assertPortForwardOutput(ctx context.Context, workDir, raw string, e orktypes.E2EKubectlPortForward, target string) error {
	out, err := applyExtract(ctx, workDir, raw, e.JQ, e.YQ)
	if err != nil {
		return fmt.Errorf("kubectl port-forward %s%s: %w", target, e.Path, err)
	}

	if err := applyAssertions(out, kubectlPortForwardAssertions(e)); err != nil {
		return fmt.Errorf("kubectl port-forward %s%s: %w\noutput: %s", target, e.Path, err, out)
	}
	return nil
}

func checkKubectlEvents(ctx context.Context, e orktypes.E2EKubectlEvents, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}
	args := []string{"events", "--for", e.Kind + "/" + e.Name, "-n", ns}
	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		return fmt.Errorf("kubectl events %s/%s: %s", e.Kind, e.Name, out)
	}
	return assertKubectlEventsOutput(out, e)
}

// assertKubectlEventsOutput applies e's assertions to already-fetched
// events output. Split out so this logic is testable without kubectl or a
// cluster.
func assertKubectlEventsOutput(out string, e orktypes.E2EKubectlEvents) error {
	if err := applyAssertions(out, kubectlEventsAssertions(e)); err != nil {
		return fmt.Errorf("kubectl events %s/%s: %w\noutput: %s", e.Kind, e.Name, err, out)
	}
	return nil
}

func checkKubectlAuth(ctx context.Context, cs kubernetes.Interface, e orktypes.E2EKubectlAuth) error {
	attrs := &authorizationv1.ResourceAttributes{
		Verb:      e.Verb,
		Resource:  e.Resource,
		Namespace: e.Namespace,
	}

	var allowed bool
	if e.As == "" {
		// No impersonation: use SelfSubjectAccessReview (what kubectl auth can-i does by default).
		ssar := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: attrs,
			},
		}
		result, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, ssar, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("auth can-i %s %s: %w", e.Verb, e.Resource, err)
		}
		allowed = result.Status.Allowed
	} else {
		sar := &authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: attrs,
				User:               e.As,
			},
		}
		result, err := cs.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("auth can-i %s %s --as %s: %w", e.Verb, e.Resource, e.As, err)
		}
		allowed = result.Status.Allowed
	}

	out := "no"
	if allowed {
		out = "yes"
	}
	if err := applyAssertions(out, kubectlAuthAssertions(e)); err != nil {
		return fmt.Errorf("auth can-i %s %s: %w\nresult: %s", e.Verb, e.Resource, err, out)
	}
	return nil
}

func checkKubectlCp(ctx context.Context, e orktypes.E2EKubectlCp, workDir string) error {
	ns := e.Namespace
	if ns == "" {
		ns = "default"
	}

	pod := e.Name
	if pod == "" && e.LabelSelector != "" {
		var err error
		pod, err = runKubectl(ctx, workDir,
			"get", "pod", "-n", ns, "-l", e.LabelSelector,
			"-o", "jsonpath={.items[0].metadata.name}")
		if err != nil || strings.TrimSpace(pod) == "" {
			return fmt.Errorf("kubectl cp: no pod found for selector %q in %s", e.LabelSelector, ns)
		}
	}

	tmp, err := os.CreateTemp("", "ork-e2e-cp-*")
	if err != nil {
		return fmt.Errorf("kubectl cp: creating temp file: %w", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	src := fmt.Sprintf("%s/%s:%s", ns, pod, e.Src)
	args := []string{"cp", src, tmp.Name()}
	if e.Container != "" {
		args = append(args, "-c", e.Container)
	}
	if out, err := runKubectl(ctx, workDir, args...); err != nil {
		return fmt.Errorf("kubectl cp %s: %s", src, out)
	}

	raw, err := readLocal(tmp.Name())
	if err != nil {
		return fmt.Errorf("kubectl cp: reading temp file: %w", err)
	}
	out := string(raw)

	out, err = applyExtract(ctx, workDir, out, e.JQ, e.YQ)
	if err != nil {
		return fmt.Errorf("kubectl cp %s: %w", src, err)
	}

	return assertKubectlCpOutput(src, out, e)
}

// assertKubectlCpOutput applies e's assertions to the already-copied (and
// jq/yq-extracted) file content. Split out so this logic is testable
// without kubectl or a cluster.
func assertKubectlCpOutput(src, out string, e orktypes.E2EKubectlCp) error {
	if err := applyAssertions(out, kubectlCpAssertions(e)); err != nil {
		return fmt.Errorf("kubectl cp %s: %w\noutput: %s", src, err, out)
	}
	return nil
}

func checkKubectlTop(ctx context.Context, e orktypes.E2EKubectlTop, workDir string) error {
	kind := strings.ToLower(e.Kind)
	args := []string{"top", kind}
	if kind == "pod" || kind == "pods" {
		ns := e.Namespace
		if ns == "" {
			ns = "default"
		}
		args = append(args, "-n", ns)
		if e.LabelSelector != "" {
			args = append(args, "-l", e.LabelSelector)
		} else if e.Name != "" {
			args = append(args, e.Name)
		}
		if e.Containers {
			args = append(args, "--containers")
		}
	} else if e.Name != "" {
		args = append(args, e.Name)
	}
	out, err := runKubectl(ctx, workDir, args...)
	if err != nil {
		return fmt.Errorf("kubectl top %s: %s", kind, out)
	}
	return assertKubectlTopOutput(kind, out, e)
}

// assertKubectlTopOutput applies e's assertions to already-fetched `kubectl
// top` output. Split out so this logic is testable without kubectl or a
// cluster.
func assertKubectlTopOutput(kind, out string, e orktypes.E2EKubectlTop) error {
	if err := applyAssertions(out, kubectlTopAssertions(e)); err != nil {
		return fmt.Errorf("kubectl top %s: %w\noutput: %s", kind, err, out)
	}
	return nil
}

// freeLocalPort finds an available local TCP port by binding to :0 and releasing it.
func freeLocalPort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	return port, err
}

// applyExtract pipes output through jq or yq if set.
func applyExtract(ctx context.Context, workDir, output, jq, yq string) (string, error) {
	if jq != "" {
		if !strings.HasPrefix(jq, ".") {
			jq = "." + jq
		}
		cmd := exec.CommandContext(ctx, "jq", "-r", jq)
		if workDir != "" {
			cmd.Dir = workDir
		}
		cmd.Stdin = strings.NewReader(output)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("jq %q: %w\ninput: %s", jq, err, output)
		}
		return strings.TrimSpace(string(out)), nil
	}
	if yq != "" {
		cmd := exec.CommandContext(ctx, "yq", "e", yq, "-")
		if workDir != "" {
			cmd.Dir = workDir
		}
		cmd.Stdin = strings.NewReader(output)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("yq %q: %w\ninput: %s", yq, err, output)
		}
		return strings.TrimSpace(string(out)), nil
	}
	return output, nil
}

func runKubectl(ctx context.Context, workDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
