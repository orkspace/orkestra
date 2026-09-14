//go:build !runtime && !gateway

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/gateway/api/intake"
	"github.com/orkspace/orkestra/pkg/katalog"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

// webhookPlayCmd runs a simulated webhook payload locally through the exact
// same engine "ork serve play" uses — no cluster, no HTTP server, no real
// GitHub/GitLab/Slack account required.
var webhookPlayCmd = &cobra.Command{
	Use:   "play",
	Short: "Run a webhook payload locally through the full apply chain",
	Long: `Run a webhook payload locally through the full apply chain — the same
target resolution, token check, CR construction, provenance stamping,
admission validation, and response payload evaluation "ork serve play" runs.

Signature/token verification is skipped — play mode tests the LOGIC each
source applies to its payload (branch filtering, watch-pattern matching,
command parsing), not the HTTP transport. --webhook always identifies a
real entry from gateway.webhooks so its declared Branch/Watch/Commands are
exercised for real, not re-typed on the command line.

--source is optional — webhook entry names are unique across all four
sources, so it's resolved from --webhook when omitted. Pass --source only
to disambiguate, or if you'd rather be explicit.

  --source generic  --body <file>
      <file> is the flat intent directly (YAML or JSON).

  --source slack    --command </cmd> --text "<target> key=value ..."
      Parsed exactly like a real slash command's text field.

  --source github|gitlab  --event <push-event.json> [--fetch path=file ...]
      <push-event.json> is the real push event shape (see
      intake.GitHubPushEvent / intake.GitLabPushEvent). Branch and watch
      filtering run for real against the event. Content can't be fetched
      from a real repo in play mode — pass --fetch <matched-path>=<local-file>
      once per matched path to supply what the Contents/Repository Files API
      would have returned; a matched path with no override is reported and
      skipped rather than failing the whole run.

--simulate hands each successfully built CR to ork simulate after the chain
passes, exactly like "ork serve play --simulate" — pass a simulate.yaml path
to use assert mode, or omit the value for op-print mode. For github/gitlab,
a push matching several files runs simulate once per matched, fetched file.

Examples:
  ork webhook play -f katalog.yaml --webhook pagerduty --body body.json
  ork webhook play --source slack -f katalog.yaml --webhook platform-workspace \
    --command /deploy --text "servicerequest name=payments-api team=platform image=nginx"
  ork webhook play --source github -f katalog.yaml --webhook payments-repo \
    --event push-event.json --fetch services/payments/intent.yaml=repo-example/services/payments/intent.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		source, _ := cmd.Flags().GetString("source")
		webhookName, _ := cmd.Flags().GetString("webhook")
		bodyFile, _ := cmd.Flags().GetString("body")
		eventFile, _ := cmd.Flags().GetString("event")
		fetchFlags, _ := cmd.Flags().GetStringSlice("fetch")
		command, _ := cmd.Flags().GetString("command")
		text, _ := cmd.Flags().GetString("text")
		simulateRaw, _ := cmd.Flags().GetString("simulate")
		simulateFlag := cmd.Flags().Changed("simulate")
		simulateConfig := simulateRaw
		if simulateConfig == "-" {
			simulateConfig = "" // op-print mode — no spec file
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}
		if !k.IsGatewayAPIEnabled() || k.Gateway.Webhooks.Empty() {
			return fmt.Errorf("%s no gateway.webhooks entries configured", failureMark())
		}

		if source == "" {
			resolved, ok := k.LookupWebhookSource(webhookName)
			if !ok {
				return fmt.Errorf("%s no webhook entry named %q — pass --source to disambiguate, or check the name against 'ork webhook list'", failureMark(), webhookName)
			}
			source = resolved
		}

		sim := webhookSimulate{ctx: cmd.Context(), enabled: simulateFlag, config: simulateConfig}
		if simulateFlag {
			paths, _ := cmd.Flags().GetStringSlice("file")
			if len(paths) == 0 {
				paths = defaultFilePaths()
			}
			if len(paths) == 0 {
				return fmt.Errorf(errNoKatalog)
			}
			sim.katalogFile = paths[0]
		}

		switch source {
		case "generic":
			return playGenericWebhook(k, webhookName, bodyFile, sim)
		case "slack":
			return playSlackWebhook(k, webhookName, command, text, sim)
		case "github", "gitlab":
			return playGitPushWebhook(k, source, webhookName, eventFile, fetchFlags, sim)
		default:
			return fmt.Errorf("%s --source must be one of: github, gitlab, slack, generic", failureMark())
		}
	},
}

func playGenericWebhook(k *katalog.Katalog, webhookName, bodyFile string, sim webhookSimulate) error {
	entry, err := findGenericWebhookEntry(k.Gateway.Webhooks.Generic, webhookName)
	if err != nil {
		return err
	}
	if bodyFile == "" {
		return fmt.Errorf("%s --body is required for --source generic", failureMark())
	}
	raw, err := readIntentFile(bodyFile)
	if err != nil {
		return fmt.Errorf("%s reading body file %q: %w", failureMark(), bodyFile, err)
	}

	printWebhookPlayHeader("generic", entry.Name, entry.Path)
	obj, _, _, err := runCreateUpdateChain(k, raw, entry.Name, entry.Name, orktypes.ServeOpCreate)
	if err != nil {
		return err
	}
	printWebhookPlaySuccess()
	return playSimulate(sim, obj)
}

func playSlackWebhook(k *katalog.Katalog, webhookName, command, text string, sim webhookSimulate) error {
	entry, err := findSlackWebhookEntry(k.Gateway.Webhooks.Slack, webhookName)
	if err != nil {
		return err
	}
	if command == "" || text == "" {
		return fmt.Errorf("%s --command and --text are required for --source slack", failureMark())
	}

	printWebhookPlayHeader("slack", entry.Name, entry.Path)

	printPreStage("Command check")
	allowed := false
	for _, c := range entry.Commands {
		if c == command {
			allowed = true
			break
		}
	}
	if !allowed {
		printStageError(fmt.Sprintf("unknown command %q — allowed: %s", command, strings.Join(entry.Commands, ", ")))
		return fmt.Errorf("command not allowed")
	}
	printStageOK(fmt.Sprintf("%s is an allowed command", bold(command)))

	printPreStage("Argument parsing")
	raw, err := intake.ParseSlackArgs(text)
	if err != nil {
		printStageError(err.Error())
		return err
	}
	printStageOK(fmt.Sprintf("parsed %d field(s)", len(raw)))
	fmt.Println()

	obj, _, _, err := runCreateUpdateChain(k, raw, entry.Name, entry.Name, orktypes.ServeOpCreate)
	if err != nil {
		return err
	}
	printWebhookPlaySuccess()
	return playSimulate(sim, obj)
}

func playGitPushWebhook(k *katalog.Katalog, source, webhookName, eventFile string, fetchFlags []string, sim webhookSimulate) error {
	var entry orktypes.GitWebhookConfig
	var err error
	if source == "github" {
		entry, err = findGitWebhookEntry(k.Gateway.Webhooks.GitHub, "github", webhookName)
	} else {
		entry, err = findGitWebhookEntry(k.Gateway.Webhooks.GitLab, "gitlab", webhookName)
	}
	if err != nil {
		return err
	}
	if eventFile == "" {
		return fmt.Errorf("%s --event is required for --source %s", failureMark(), source)
	}
	overrides, err := parseFetchOverrides(fetchFlags)
	if err != nil {
		return err
	}

	data, err := readLocal(eventFile)
	if err != nil {
		return fmt.Errorf("%s reading event file %q: %w", failureMark(), eventFile, err)
	}

	var ref string
	var groups [][]string
	if source == "github" {
		var event intake.GitHubPushEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("%s parsing event file: %w", failureMark(), err)
		}
		ref = event.Ref
		for _, c := range event.Commits {
			groups = append(groups, c.Added, c.Modified)
		}
	} else {
		var event intake.GitLabPushEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("%s parsing event file: %w", failureMark(), err)
		}
		ref = event.Ref
		for _, c := range event.Commits {
			groups = append(groups, c.Added, c.Modified)
		}
	}

	printWebhookPlayHeader(source, entry.Name, entry.Path)

	printPreStage("Branch filter")
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch != entry.Branch {
		printStageError(fmt.Sprintf("push is on %q, entry watches %q", branch, entry.Branch))
		return fmt.Errorf("branch not watched")
	}
	printStageOK(fmt.Sprintf("push on %s matches watched branch", bold(branch)))

	printPreStage("Watch pattern match")
	matched := intake.MatchedWatchFiles(entry.Watch, intake.CollectChangedFiles(groups...))
	if len(matched) == 0 {
		printStageError("no changed file matches a declared watch pattern")
		return fmt.Errorf("no watched files changed")
	}
	printStageOK(fmt.Sprintf("%d matched file(s): %s", len(matched), strings.Join(matched, ", ")))

	var ran int
	for _, path := range matched {
		fmt.Println()
		printPreStage("Content fetch — " + path)
		localFile, ok := overrides[path]
		if !ok {
			printStageDetail(gray(fmt.Sprintf(
				"no --fetch override for %q — skipping (would call the real %s API at runtime)", path, source,
			)))
			continue
		}
		content, err := readLocal(localFile)
		if err != nil {
			printStageError(fmt.Sprintf("reading override file %q: %v", localFile, err))
			continue
		}
		fields, err := intake.ParseIntentContent(path, content)
		if err != nil {
			printStageError(err.Error())
			continue
		}
		printStageOK(fmt.Sprintf("read %s (%d bytes) from %s", bold(path), len(content), localFile))
		fmt.Println()

		obj, _, _, err := runCreateUpdateChain(k, fields, entry.Name, entry.Name, orktypes.ServeOpCreate)
		if err != nil {
			return err
		}
		if err := playSimulate(sim, obj); err != nil {
			return err
		}
		ran++
	}

	if ran == 0 {
		return fmt.Errorf("%s no matched file had a --fetch override — nothing to play", failureMark())
	}
	printWebhookPlaySuccess()
	return nil
}

// parseFetchOverrides parses "path=local-file" pairs into a lookup map.
func parseFetchOverrides(raw []string) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for _, r := range raw {
		parts := strings.SplitN(r, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("%s --fetch must be <path>=<local-file>, got %q", failureMark(), r)
		}
		out[parts[0]] = parts[1]
	}
	return out, nil
}

func printWebhookPlayHeader(source, name, path string) {
	fmt.Println()
	fmt.Printf("%s  ork webhook play\n", bold("▶"))
	fmt.Printf("  %s %s\n", gray("source: "), bold(source))
	fmt.Printf("  %s %s\n", gray("webhook:"), bold(name))
	fmt.Printf("  %s %s\n", gray("path:   "), cyan(path))
	fmt.Println()
}

func printWebhookPlaySuccess() {
	fmt.Println()
	fmt.Printf("%s Webhook payload would be accepted\n", successMark())
	fmt.Println()
}

// printPreStage prints a source-specific pre-filtering step — branch/watch
// matching, command checks — visually distinct from the numbered 1-6
// runCreateUpdateChain stages that follow once a payload becomes an intent.
func printPreStage(name string) {
	fmt.Printf("%s  %s\n", cyan("→"), bold(name))
}

func init() {
	webhookPlayCmd.Flags().StringP("source", "s", "", "Source type: github, gitlab, slack, or generic — resolved from --webhook when omitted")
	webhookPlayCmd.Flags().StringP("webhook", "w", "", "Name of the configured gateway.webhooks entry to play (required)")
	webhookPlayCmd.Flags().StringP("body", "b", "", "Body file for --source generic (YAML or JSON)")
	webhookPlayCmd.Flags().StringP("command", "c", "", "Slash command for --source slack, e.g. /deploy")
	webhookPlayCmd.Flags().StringP("text", "t", "", `Command text for --source slack, e.g. "servicerequest name=foo team=bar"`)
	webhookPlayCmd.Flags().StringP("event", "e", "", "Push event JSON file for --source github/gitlab")
	webhookPlayCmd.Flags().StringSliceP("fetch", "F", nil, "Simulated content-fetch override <path>=<local-file>, repeatable, for --source github/gitlab")
	webhookPlayCmd.Flags().StringP("simulate", "S", "", "After play, hand each built CR to ork simulate; pass a simulate.yaml path to use assert mode")
	webhookPlayCmd.Flags().Lookup("simulate").NoOptDefVal = "-"

	_ = webhookPlayCmd.MarkFlagRequired("webhook")

	webhookCmd.AddCommand(webhookPlayCmd)
}
