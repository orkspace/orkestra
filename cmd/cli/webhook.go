//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

// ── ork webhook ───────────────────────────────────────────────────────────────

var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Inspect and locally test gateway.webhooks intake sources",
	Long: `Inspect and locally test gateway.webhooks — inbound intent-delivery
sources (GitHub, GitLab, Slack, generic HTTP) that resolve through the same
target-mode pipeline POST /api/v1/apply does.

Subcommands:
  list    List all configured webhook entries
  play    Run a webhook payload locally through the full apply chain`,
}

// ── ork webhook list ──────────────────────────────────────────────────────────

var webhookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured webhook entries",
	Long: `List every entry declared under gateway.webhooks, across all four
sources, with its path and what's configured.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}
		if !k.IsGatewayAPIEnabled() || k.Gateway.Webhooks.Empty() {
			fmt.Println("no gateway.webhooks entries configured")
			return nil
		}
		w := k.Gateway.Webhooks

		type row struct {
			source, name, path, extra string
			enabled                   bool
		}
		var rows []row
		for _, e := range w.GitHub {
			rows = append(rows, row{"github", e.Name, e.Path, "branch=" + e.Branch, e.Enabled})
		}
		for _, e := range w.GitLab {
			rows = append(rows, row{"gitlab", e.Name, e.Path, "branch=" + e.Branch, e.Enabled})
		}
		for _, e := range w.Slack {
			rows = append(rows, row{"slack", e.Name, e.Path, "commands=" + fmt.Sprint(e.Commands), e.Enabled})
		}
		for _, e := range w.Generic {
			rows = append(rows, row{"generic", e.Name, e.Path, "", e.Enabled})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].source != rows[j].source {
				return rows[i].source < rows[j].source
			}
			return rows[i].name < rows[j].name
		})

		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "SOURCE\tNAME\tPATH\tENABLED\t")
		for _, r := range rows {
			enabled := "false"
			if r.enabled {
				enabled = "true"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.source, r.name, r.path, enabled, r.extra)
		}
		return tw.Flush()
	},
}

// ── shared lookup ─────────────────────────────────────────────────────────────

// findWebhookEntry locates a webhook entry by name within the given source
// list, returning a helpful error listing available names when not found.
func findGitWebhookEntry(entries []orktypes.GitWebhookConfig, source, name string) (orktypes.GitWebhookConfig, error) {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Name == name {
			return e, nil
		}
	}
	return orktypes.GitWebhookConfig{}, fmt.Errorf(
		"%s no %s webhook entry named %q — configured: %s",
		failureMark(), source, name, joinOrNone(names),
	)
}

func findSlackWebhookEntry(entries []orktypes.SlackWebhookConfig, name string) (orktypes.SlackWebhookConfig, error) {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Name == name {
			return e, nil
		}
	}
	return orktypes.SlackWebhookConfig{}, fmt.Errorf(
		"%s no slack webhook entry named %q — configured: %s",
		failureMark(), name, joinOrNone(names),
	)
}

func findGenericWebhookEntry(entries []orktypes.GenericWebhookConfig, name string) (orktypes.GenericWebhookConfig, error) {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Name == name {
			return e, nil
		}
	}
	return orktypes.GenericWebhookConfig{}, fmt.Errorf(
		"%s no generic webhook entry named %q — configured: %s",
		failureMark(), name, joinOrNone(names),
	)
}

func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "(none declared)"
	}
	return strings.Join(names, ", ")
}

func init() {
	webhookCmd.AddCommand(webhookListCmd)
	rootCmd.AddCommand(webhookCmd)
	shadowGlobalCommandFlags(webhookCmd)
}
