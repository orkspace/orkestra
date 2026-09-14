package e2e

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/orkspace/orkestra/pkg/tools/cluster"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ensureTools checks which external tools are required by the E2E spec and
// installs any that are missing. Called once at runner start before assertions run.
func ensureTools(e2e orktypes.E2E) error {
	needCurl, needJQ, needYQ, needMetrics := scanToolRequirements(e2e)
	if needCurl {
		if err := ensureTool("curl"); err != nil {
			return err
		}
	}
	if needJQ {
		if err := ensureTool("jq"); err != nil {
			return err
		}
	}
	if needYQ {
		if err := ensureTool("yq"); err != nil {
			return err
		}
	}
	if needMetrics {
		if err := cluster.EnsureMetricsServer(); err != nil {
			return err
		}
	}
	return nil
}

// scanToolRequirements walks the full E2E spec and reports which tools are needed.
func scanToolRequirements(e2e orktypes.E2E) (needCurl, needJQ, needYQ, needMetrics bool) {
	for _, exp := range e2e.Spec.Expect {
		if exp.Kubectl == nil {
			continue
		}
		for _, e := range exp.Kubectl.PortForward {
			if e.Path != "" {
				needCurl = true
			}
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		for _, e := range exp.Kubectl.Get {
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		for _, e := range exp.Kubectl.Logs {
			if e.JQ != "" {
				needJQ = true
			}
		}
		for _, e := range exp.Kubectl.Exec {
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		for _, e := range exp.Kubectl.Cp {
			if e.JQ != "" {
				needJQ = true
			}
			if e.YQ != "" {
				needYQ = true
			}
		}
		if len(exp.Kubectl.Top) > 0 {
			needMetrics = true
		}
	}
	return
}

// ensureTool checks if tool is on PATH. If not, installs it and shows a spinner.
func ensureTool(name string) error {
	if _, err := exec.LookPath(name); err == nil {
		return nil
	}

	s := startSpinner(fmt.Sprintf("Installing %s (required by e2e.yaml)...", name))

	if err := installTool(name); err != nil {
		s.Failure()
		return fmt.Errorf("could not install %q: %w", name, err)
	}

	s.Success()
	return nil
}

// installTool installs a known tool using the system package manager.
func installTool(name string) error {
	switch runtime.GOOS {
	case "linux":
		return installLinux(name)
	case "darwin":
		return installDarwin(name)
	default:
		return fmt.Errorf("%q not found and automatic install is not supported on %s — install it manually", name, runtime.GOOS)
	}
}

func installLinux(name string) error {
	// Prefer apt-get, fall back to apk (Alpine).
	if _, err := exec.LookPath("apt-get"); err == nil {
		cmd := exec.Command("apt-get", "install", "-y", "-qq", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("apt-get install %s: %w\n%s", name, err, out)
		}
		return nil
	}
	if _, err := exec.LookPath("apk"); err == nil {
		cmd := exec.Command("apk", "add", "--no-cache", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("apk add %s: %w\n%s", name, err, out)
		}
		return nil
	}
	return fmt.Errorf("%q not found and no supported package manager (apt-get, apk) detected", name)
}

func installDarwin(name string) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("%q not found and brew is not installed — run: brew install %s", name, name)
	}
	cmd := exec.Command("brew", "install", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("brew install %s: %w\n%s", name, err, out)
	}
	return nil
}
