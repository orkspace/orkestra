//go:build !runtime && !gateway

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/orkspace/orkestra/pkg/katalog"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// servePlayCmd runs the full gateway apply chain locally from a flat intent
// file — no cluster, no HTTP server. Useful for testing token permissions,
// field routing, provenance stamping, and response payload shaping before
// wiring up a real delivery surface.
var servePlayCmd = &cobra.Command{
	Use:   "play",
	Short: "Run a serve intent locally through the full gateway chain",
	Long: `Run a serve intent locally through the full gateway chain.

For create/update: reads a flat YAML or JSON intent file (default: intent.yaml
or intent.json in the current directory) and runs all five chain stages.

For get/list/delete: no intent file needed — pass --target, --namespace, and
(for get/delete) --name. Runs target resolution, token check, and shows the
response config that would apply.

Files ending in .json are parsed as JSON. Everything else is parsed as YAML.

No cluster connection is required. No CR is applied or fetched.

Stages (create/update):
  1. Target resolution    — resolve target/alias from katalog
  2. Token check          — verify the named token can perform the operation
  3. CR construction      — build the full CR from field declarations
  4. Provenance           — stamp serve-target and serve-alias annotations
  5. Admission validation — evaluate validation.rules (deny/warn) against the built CR
  6. Response payload     — evaluate serve.config.response.payload expressions

Stages (get/list/delete):
  1. Target resolution  — resolve target/alias from katalog
  2. Token check        — verify the named token can perform the operation
  3. Response config    — show what the caller would receive

Example intent.yaml:
  target: apifixture
  name: my-payment-service
  workloadType: app
  team: platform
  environment: staging
  repoURL: https://github.com/myorg/payments
  productionApproval: JIRA-1234

Example intent.json:
  {"target":"apifixture","name":"my-payment-service","workloadType":"app","team":"platform","environment":"staging","repoURL":"https://github.com/myorg/payments","productionApproval":"JIRA-1234"}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		intentFile, _ := cmd.Flags().GetString("intent")
		tokenName, _ := cmd.Flags().GetString("token")
		targetOverride, _ := cmd.Flags().GetString("target")
		operation, _ := cmd.Flags().GetString("operation")
		namespace, _ := cmd.Flags().GetString("namespace")
		name, _ := cmd.Flags().GetString("name")
		simulateRaw, _ := cmd.Flags().GetString("simulate")
		simulateFlag := cmd.Flags().Changed("simulate")
		simulateConfig := simulateRaw
		if simulateConfig == "-" {
			simulateConfig = "" // op-print mode — no spec file
		}

		if tokenName == "" {
			return fmt.Errorf("%s --token is required", failureMark())
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return err
		}

		op := orktypes.ServeOperation(operation)

		if simulateFlag && op != orktypes.ServeOpCreate && op != orktypes.ServeOpUpdate {
			return fmt.Errorf("%s --simulate is only valid for create and update operations", failureMark())
		}

		if op == orktypes.ServeOpCreate || op == orktypes.ServeOpUpdate {
			// Resolve first katalog path for simulate handoff (--file is a StringSlice on serve commands)
			katalogFile := ""
			paths, _ := cmd.Flags().GetStringSlice("file")
			if len(paths) == 0 {
				paths = defaultFilePaths()
			}
			if len(paths) == 0 {
				return fmt.Errorf(errNoKatalog)
			}
			katalogFile = paths[0]

			return playWrite(cmd.Context(), k, katalogFile, intentFile, targetOverride, tokenName, op, simulateFlag, simulateConfig)
		}
		return playRead(k, targetOverride, tokenName, op, namespace, name)
	},
}

// ── create / update path ─────────────────────────────────────────────────────

func playWrite(ctx context.Context, k *katalog.Katalog, katalogFile, intentFile, targetOverride, tokenName string, op orktypes.ServeOperation, doSimulate bool, simulateConfig string) error {
	if intentFile == "" {
		intentFile = resolveDefaultIntentFile()
		if intentFile == "" {
			return fmt.Errorf("%s no intent file found — create intent.yaml or intent.json, or pass --intent <file>", failureMark())
		}
	}

	raw, err := readIntentFile(intentFile)
	if err != nil {
		return fmt.Errorf("%s reading intent file %q: %w", failureMark(), intentFile, err)
	}
	if targetOverride != "" {
		raw["target"] = targetOverride
	}

	target, _ := raw["target"].(string)
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf(`%s "target" must be set in the intent file or via --target`, failureMark())
	}

	printPlayHeader(intentFile, target, tokenName, string(op))

	obj, crd, alias, err := runCreateUpdateChain(k, raw, tokenName, "", op)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("%s Intent would be accepted\n", successMark())
	fmt.Printf("  %s\n", gray(fmt.Sprintf("POST /api/v1/apply  →  %s/%s in %s", crd.Kind(), obj.GetName(), obj.GetNamespace())))
	if alias != "" {
		fmt.Printf("  %s\n", gray(fmt.Sprintf("surface: %s (alias of %s)", alias, crd.ServeTarget())))
	}
	fmt.Println()

	if doSimulate {
		return playRunSimulate(ctx, katalogFile, obj, simulateConfig)
	}
	return nil
}

// ── get / list / delete path ─────────────────────────────────────────────────

func playRead(k *katalog.Katalog, target, tokenName string, op orktypes.ServeOperation, namespace, name string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("%s --target is required for %s", failureMark(), op)
	}
	if op != orktypes.ServeOpList && strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s --name is required for %s", failureMark(), op)
	}

	printPlayHeaderRead(target, tokenName, string(op), namespace, name)

	// Stage 1: Target resolution
	printStage(1, "Target resolution")
	resolution := k.LookupByTargetOrAlias(target)
	if resolution == nil {
		printStageError(fmt.Sprintf("unknown target %q\n  available: %s", target, strings.Join(k.AvailableTargets(), ", ")))
		return fmt.Errorf("target %q not found", target)
	}
	crd, alias := resolution.CRD, resolution.Alias
	printStageOK(fmt.Sprintf("kind=%s  target=%s  alias=%s", bold(crd.Kind()), green(crd.ServeTarget()), aliasOrNone(alias)))

	// Stage 2: Token check
	printStage(2, "Token check")
	allowed, denyReason := crd.TokenAllowedFor(alias, tokenName, string(op), namespace, orktypes.ServeClassResources)
	if !allowed {
		msg := denyReason.Message(tokenName, string(op), crd.Kind(), namespace)
		printStageError(msg)
		return fmt.Errorf("token %q denied: %s", tokenName, msg)
	}
	printStageOK(fmt.Sprintf("token %s can %s on %s", bold(tokenName), op, bold(crd.Kind())))

	// Stage 3: Response config
	printStage(3, "Response config")
	if op == orktypes.ServeOpDelete {
		printStageDetail(gray(fmt.Sprintf("DELETE %s/%s in %s", crd.Kind(), name, namespaceOrAny(namespace))))
		printStageOK("would be deleted")
	} else {
		cfg := crd.ServeResponseConfigFor(alias)
		if cfg == nil {
			printStageDetail(gray("no serve.config.response declared — full CR returned"))
		} else {
			if !cfg.UseDefault() {
				printStageDetail(gray("default: false — raw CR omitted from response"))
			} else {
				printStageDetail(gray("default: true — full CR included in response"))
			}
			if cfg.HasPayload() {
				printStageDetail(gray(fmt.Sprintf("payload fields: %s", strings.Join(payloadKeys(cfg.Payload), ", "))))
			}
			if len(cfg.Exclude) > 0 {
				printStageDetail(gray(fmt.Sprintf("excluded paths: %s", strings.Join(cfg.Exclude, ", "))))
			}
		}
		if op == orktypes.ServeOpGet {
			printStageOK(fmt.Sprintf("GET %s/%s in %s — response config above applies", crd.Kind(), name, namespaceOrAny(namespace)))
		} else {
			printStageOK(fmt.Sprintf("LIST %s in %s — response config applies to all items", crd.Kind(), namespaceOrAny(namespace)))
		}
	}

	fmt.Println()
	fmt.Printf("%s Request would be allowed\n", successMark())
	fmt.Println()
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func resolveDefaultIntentFile() string {
	for _, name := range []string{"intent.yaml", "intent.json"} {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}
	return ""
}

func readIntentFile(path string) (map[string]interface{}, error) {
	data, err := readLocal(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	if strings.HasSuffix(path, ".json") {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
	}
	return raw, nil
}

func namespaceOrAny(ns string) string {
	if ns == "" {
		return "(all namespaces)"
	}
	return ns
}

func payloadKeys(payload map[string]string) []string {
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ── print helpers ─────────────────────────────────────────────────────────────

func printPlayHeader(file, target, token, op string) {
	fmt.Println()
	fmt.Printf("%s  ork serve play\n", bold("▶"))
	fmt.Printf("  %s %s\n", gray("intent:"), cyan(file))
	fmt.Printf("  %s %s\n", gray("target:"), bold(target))
	fmt.Printf("  %s %s\n", gray("token: "), bold(token))
	fmt.Printf("  %s %s\n", gray("op:    "), bold(op))
	fmt.Println()
}

func printPlayHeaderRead(target, token, op, namespace, name string) {
	fmt.Println()
	fmt.Printf("%s  ork serve play\n", bold("▶"))
	fmt.Printf("  %s %s\n", gray("target:   "), bold(target))
	fmt.Printf("  %s %s\n", gray("token:    "), bold(token))
	fmt.Printf("  %s %s\n", gray("op:       "), bold(op))
	if namespace != "" {
		fmt.Printf("  %s %s\n", gray("namespace:"), bold(namespace))
	}
	if name != "" {
		fmt.Printf("  %s %s\n", gray("name:     "), bold(name))
	}
	fmt.Println()
}

func printStage(n int, name string) {
	fmt.Printf("%s  stage %d · %s\n", cyan("→"), n, bold(name))
}

func printStageOK(detail string) {
	fmt.Printf("   %s %s\n", successMark(), detail)
}

func printStageError(detail string) {
	fmt.Printf("   %s %s\n", failureMark(), red(detail))
}

func printStageDetail(detail string) {
	fmt.Printf("      %s\n", detail)
}

func printIndentedJSON(v interface{}) {
	b, err := json.MarshalIndent(v, "      ", "  ")
	if err != nil {
		fmt.Printf("      %s\n", gray("(could not marshal)"))
		return
	}
	fmt.Printf("      %s\n", gray(string(b)))
}

func aliasOrNone(alias string) string {
	if alias == "" {
		return gray("(none)")
	}
	return cyan(alias)
}

func init() {
	servePlayCmd.Flags().StringP("intent", "i", "", "Intent file to play (YAML or JSON; default: intent.yaml or intent.json in cwd)")
	servePlayCmd.Flags().StringP("token", "t", "", "Token name to authenticate with")
	servePlayCmd.Flags().StringP("target", "T", "", "Target or alias name (required for get/list/delete; overrides intent file for create/update)")
	servePlayCmd.Flags().StringP("operation", "o", string(orktypes.ServeOpCreate), "Operation to simulate ("+validServeOperations+")")
	servePlayCmd.Flags().StringP("namespace", "N", "", "Namespace (for get/list/delete)")
	servePlayCmd.Flags().StringP("name", "n", "", "Resource name (for get/delete)")
	servePlayCmd.Flags().StringP("simulate", "S", "", "After play, hand the built CR to ork simulate; pass a simulate.yaml path to use assert mode")
	servePlayCmd.Flags().Lookup("simulate").NoOptDefVal = "-"

	_ = servePlayCmd.MarkFlagRequired("token")

	serveCmd.AddCommand(servePlayCmd)
}
