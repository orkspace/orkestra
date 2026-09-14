package cluster

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const (
	metricsInstallURL = "https://github.com/kubernetes-sigs/metrics-server#installation"
	metricsHelmRepo   = "https://kubernetes-sigs.github.io/metrics-server/"
)

// EnsureMetricsServer ensures the Kubernetes metrics-server is installed.
// Checks for the metrics-server Deployment in kube-system; installs via Helm
// if missing, with --kubelet-insecure-tls when running on kind. Failures are
// non-fatal — the user is prompted to install manually instead.
func EnsureMetricsServer() error {
	if exec.Command("kubectl", "get", "deployment", "metrics-server",
		"-n", "kube-system", "-o", "json").Run() == nil {
		return nil
	}

	spin := startSpinner("Installing metrics-server (required by kubectl.top)...")

	ctxOut, err := exec.Command("kubectl", "config", "current-context").Output()
	if err != nil {
		spin.Failure()
		fmt.Printf("  %s Could not determine current context: %v\n", warningMark(), err)
		fmt.Printf("  %s Install metrics-server manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}
	isKind := strings.Contains(string(ctxOut), "kind")

	if err := exec.Command("helm", "repo", "add", "metrics-server", metricsHelmRepo).Run(); err != nil {
		spin.Failure()
		fmt.Printf("  %s Failed to add Helm repo: %v\n", warningMark(), err)
		fmt.Printf("  %s Install manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}
	_ = exec.Command("helm", "repo", "update").Run()

	args := []string{
		"upgrade", "--install", "metrics-server",
		"metrics-server/metrics-server",
		"--namespace", "kube-system",
		"--wait", "--timeout=120s",
	}
	if isKind {
		args = append(args, "--set", "args={--kubelet-insecure-tls}")
	}
	installCmd := exec.Command("helm", args...)
	installCmd.Stdout = io.Discard
	installCmd.Stderr = io.Discard
	if err := installCmd.Run(); err != nil {
		spin.Failure()
		fmt.Printf("  %s Failed to install metrics-server: %v\n", warningMark(), err)
		fmt.Printf("  %s Install manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}

	if exec.Command("kubectl", "get", "deployment", "metrics-server",
		"-n", "kube-system", "-o", "json").Run() != nil {
		spin.Failure()
		fmt.Printf("  %s metrics-server install could not be verified\n", warningMark())
		fmt.Printf("  %s Install manually: %s\n", infoMark(), metricsInstallURL)
		return nil
	}

	spin.Success()
	return nil
}
