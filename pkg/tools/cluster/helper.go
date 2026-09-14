package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/utils"
)

var (
	readLocal    = utils.ReadLocal
	startSpinner = utils.StartSpinner
	warningMark  = utils.WarningMark
	infoMark     = utils.InfoMark
)

// DeploymentHealthChecker handles health checks for any deployment
type DeploymentHealthChecker struct {
	Name      string
	Namespace string
}

// DeploymentStatus represents the status of a Kubernetes deployment
type DeploymentStatus struct {
	Running bool
	Reason  string // set when Running is false
}

// SyncDeployment restarts a Kubernetes Deployment and waits for the rollout to complete.
// Returns an error if the restart or rollout status check fails.
// Timeout is configurable (e.g., 3*time.Minute).
func SyncDeployment(deploymentName, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if out, err := exec.CommandContext(ctx, "kubectl", "rollout", "restart",
		"deploy/"+deploymentName, "-n", namespace).CombinedOutput(); err != nil {
		return fmt.Errorf("restarting deployment %s/%s: %w\n%s", namespace, deploymentName, err, out)
	}

	if out, err := exec.CommandContext(ctx, "kubectl", "rollout", "status",
		"deploy/"+deploymentName, "-n", namespace).CombinedOutput(); err != nil {
		return fmt.Errorf("waiting for rollout of %s/%s: %w\n%s", namespace, deploymentName, err, out)
	}

	return nil
}

// ResourceExists checks if a Kubernetes resource exists in the given namespace.
// Returns true if the resource exists, false otherwise.
// The resource should be specified in kubectl compatible format (e.g., "deploy", "service", "pod").

// In future if needed:
// ServiceExists := ResourceExists("service", "my-service", "default")
// ConfigMapExists := ResourceExists("configmap", "my-config", "kube-system")
func ResourceExists(resourceType, resourceName, namespace string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), resourceExistsTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "kubectl", "get", resourceType, resourceName,
		"-n", namespace, "--ignore-not-found", "-o", "name").Output()

	return err == nil && len(bytes.TrimSpace(out)) > 0
}

// CheckHealth waits up to timeout for a deployment to have at least one ready replica.
// Polls every 2 seconds. Returns immediately if a custom failure condition is detected.
func (d DeploymentHealthChecker) CheckHealth(timeout time.Duration, checkFailure func(ctx context.Context) string) DeploymentStatus {
	spin := utils.StartSpinner(fmt.Sprintf("  → Checking %s health...", d.Name))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			spin.Failure()
			return DeploymentStatus{
				Reason: fmt.Sprintf("timeout (%s) waiting for %s to become ready", timeout, d.Name),
			}
		case <-ticker.C:
			status := d.checkOnce(ctx)
			if status.Running {
				spin.Success()
				return status
			}
			// Only fail fast on crashloop — everything else
			// ("not found", "no ready replicas") is transient and should
			// keep polling until the timeout expires.
			if checkFailure != nil {
				if reason := checkFailure(ctx); reason != "" {
					spin.Failure()
					return DeploymentStatus{Reason: reason}
				}
			}
		}
	}
}

// checkOnce performs a single health check on the deployment
func (d DeploymentHealthChecker) checkOnce(ctx context.Context) DeploymentStatus {
	// Check if deployment exists
	nameOut, err := exec.CommandContext(ctx,
		"kubectl", "get", "deploy", d.Name,
		"-n", d.Namespace, "--ignore-not-found", "-o", "name",
	).Output()
	if err != nil || len(bytes.TrimSpace(nameOut)) == 0 {
		return DeploymentStatus{
			Reason: fmt.Sprintf("deployment %s not found in %s", d.Name, d.Namespace),
		}
	}

	// Check ready replicas
	readyOut, _ := exec.CommandContext(ctx,
		"kubectl", "get", "deploy", d.Name,
		"-n", d.Namespace, "-o", `jsonpath={.status.readyReplicas}`,
	).Output()
	ready := strings.TrimSpace(string(readyOut))
	if ready == "" || ready == "0" {
		return DeploymentStatus{
			Reason: fmt.Sprintf("no ready replicas for %s/%s", d.Namespace, d.Name),
		}
	}
	return DeploymentStatus{Running: true}
}

// FetchLogs saves the last maxLines from the deployment to a file and returns the last 10 lines
func (d DeploymentHealthChecker) FetchLogs(maxLines int, logDir, logPath string) (tail string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("creating log dir: %w", err)
	}

	logs := fetchDeployLogs(ctx, d.Name, d.Namespace, maxLines)
	if err := os.WriteFile(logPath, []byte(logs), 0644); err != nil {
		return "", fmt.Errorf("writing %s log: %w", d.Name, err)
	}

	lines := strings.Split(strings.TrimRight(logs, "\n"), "\n")
	if start := len(lines) - 10; start > 0 {
		lines = lines[start:]
	}
	return strings.Join(lines, "\n"), nil
}

// Exists checks if the deployment exists
func (d DeploymentHealthChecker) Exists() bool {
	return ResourceExists("deploy", d.Name, d.Namespace)
}

// HasReadyReplicas checks if the deployment has at least one ready replica
func (d DeploymentHealthChecker) HasReadyReplicas(ctx context.Context) bool {
	readyOut, err := exec.CommandContext(ctx,
		"kubectl", "get", "deploy", d.Name,
		"-n", d.Namespace, "-o", `jsonpath={.status.readyReplicas}`,
	).Output()
	if err != nil {
		return false
	}
	ready := strings.TrimSpace(string(readyOut))
	return ready != "" && ready != "0"
}

// ── private helpers ───────────────────────────────────────────────────────────
func crashLoopReason(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "kubectl", "get", "pods",
		"-n", OrkestraNamespace,
		"-o", `jsonpath={range .items[*]}{.metadata.name}{"\t"}{range .status.containerStatuses[*]}{.state.waiting.reason}{end}{"\n"}{end}`).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) == 2 && strings.Contains(parts[0], "runtime") && parts[1] == "CrashLoopBackOff" {
			return fmt.Sprintf("pod %s is in CrashLoopBackOff", parts[0])
		}
	}
	return ""
}

func fetchDeployLogs(ctx context.Context, deploy, ns string, tailLines int) string {
	out, err := exec.CommandContext(ctx, "kubectl", "logs",
		"deploy/"+deploy, "-n", ns, fmt.Sprintf("--tail=%d", tailLines)).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("[failed to fetch logs for %s: %v]", deploy, err)
	}
	return string(out)
}
