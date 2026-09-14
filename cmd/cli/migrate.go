//go:build !runtime && !gateway

package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orkspace/orkestra/pkg/tools/migrate"
	"github.com/orkspace/orkestra/pkg/version"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate <file>",
	Short: "Migrate a controller-runtime operator to Orkestra",
	Long: `Migrates a controller-runtime reconciler to Orkestra. Your Reconcile logic
is untouched — Orkestra takes over the infrastructure.

Default mode (--mode toclient): zero changes to your Reconcile signature or
call sites. SetupWithManager is removed; a two-line constructor using
orkadapter.ToClient and domain.ReconcilerFrom is injected. Your reconciler
compiles and runs inside Orkestra with no other edits.

  ork migrate ./controller/webapp_controller.go -o ./my-operator

The output directory receives the rewritten file plus scaffolding:
katalog.yaml, simulate.yaml, e2e.yaml, go.mod, Makefile, Dockerfile.

For a full rewrite to idiomatic Orkestra style (new Reconcile signature,
struct fields, call sites), use --mode native.

Examples:
  ork migrate ./controller/webapp_controller.go -o ./my-operator
  ork migrate ./controller/webapp_controller.go --mode native -o ./out
  ork migrate ./controller/webapp_controller.go  # prompts before replacing`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath := args[0]
		outputDir, _ := cmd.Flags().GetString("output")
		modulePath, _ := cmd.Flags().GetString("module")
		operatorName, _ := cmd.Flags().GetString("name")
		modeFlag, _ := cmd.Flags().GetString("mode")

		var mode migrate.Mode
		switch modeFlag {
		case "native":
			mode = migrate.ModeNative
		case "toclient", "":
			mode = migrate.ModeToClient
		default:
			return fmt.Errorf("unknown --mode %q: valid values are toclient, native", modeFlag)
		}

		src, err := readLocal(inputPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", inputPath, err)
		}

		res, err := migrate.Rewrite(src, mode)
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}

		if len(res.Warnings) > 0 {
			fmt.Fprintf(os.Stderr, "\n%s\n", yellow("Warnings — review these before running:"))
			for _, w := range res.Warnings {
				fmt.Fprintf(os.Stderr, "  %s %s\n", yellow("⚠"), w)
			}
			fmt.Fprintln(os.Stderr)
		}

		opts := migrate.Options{
			ModulePath:   modulePath,
			OperatorName: operatorName,
			OrkVersion:   version.Short(),
		}
		files := migrate.Generate(res, opts)

		if outputDir != "" {
			return writeOutputDir(outputDir, inputPath, res, files)
		}

		return replaceInPlace(inputPath, res, files)
	},
}

func writeOutputDir(dir, inputPath string, res *migrate.Result, files migrate.Files) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	reconcilerDest := filepath.Join(dir, filepath.Base(inputPath))
	if err := os.WriteFile(reconcilerDest, res.Source, 0o644); err != nil {
		return fmt.Errorf("write reconciler: %w", err)
	}
	fmt.Printf("  %s %s\n", green("→"), reconcilerDest)

	type namedFile struct {
		name    string
		content string
	}
	generated := []namedFile{
		{"katalog.yaml", files.Katalog},
		{"simulate.yaml", files.Simulate},
		{"e2e.yaml", files.E2E},
		{"go.mod", files.GoMod},
		{"Makefile", files.Makefile},
		{"Dockerfile", files.Dockerfile},
		{"README.md", files.README},
	}
	for _, f := range generated {
		dest := filepath.Join(dir, f.name)
		if err := os.WriteFile(dest, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
		fmt.Printf("  %s %s\n", green("→"), dest)
	}

	fmt.Printf("\n%s Search for %s in %s to see what needs attention.\n",
		green("✓ Done."), bold("TODO(ork migrate)"), dir)
	return nil
}

func replaceInPlace(inputPath string, res *migrate.Result, files migrate.Files) error {
	dir := filepath.Dir(inputPath)

	fmt.Printf("This will replace %s and write %s alongside it.\n",
		bold(inputPath), bold("katalog.yaml, simulate.yaml, e2e.yaml, go.mod, Makefile, Dockerfile, README.md"))
	fmt.Printf("%s [y/N] ", yellow("Continue?"))

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		fmt.Println("Aborted.")
		return nil
	}

	if err := os.WriteFile(inputPath, res.Source, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", inputPath, err)
	}
	fmt.Printf("  %s %s\n", green("→"), inputPath)

	type namedFile struct {
		name    string
		content string
	}
	generated := []namedFile{
		{"katalog.yaml", files.Katalog},
		{"simulate.yaml", files.Simulate},
		{"e2e.yaml", files.E2E},
		{"go.mod", files.GoMod},
		{"Makefile", files.Makefile},
		{"Dockerfile", files.Dockerfile},
	}
	for _, f := range generated {
		dest := filepath.Join(dir, f.name)
		if err := os.WriteFile(dest, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
		fmt.Printf("  %s %s\n", green("→"), dest)
	}

	fmt.Printf("\n%s Search for %s to see what needs attention.\n",
		green("✓ Done."), bold("TODO(ork migrate)"))
	return nil
}

func init() {
	rootCmd.AddCommand(migrateCmd)

	migrateCmd.Flags().StringP("output", "o", "", "Write output to this directory (non-destructive; skips confirmation)")
	migrateCmd.Flags().String("module", "", "Go module path for the migrated operator (e.g. github.com/myorg/my-operator)")
	migrateCmd.Flags().String("name", "", "Operator name in kebab-case (e.g. my-operator); derived from receiver type if omitted")
	migrateCmd.Flags().String("mode", "toclient", "Migration mode: toclient (default, zero Reconcile changes) or native (full rewrite)")

	// Shadow global flags so they don't appear under `ork migrate`
	shadowGlobalCommandFlags(migrateCmd, "file")
}
