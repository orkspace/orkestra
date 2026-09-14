//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/orkspace/orkestra/pkg/merger"
	"github.com/orkspace/orkestra/pkg/note"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

var notesCmd = &cobra.Command{
	Use:   "notes",
	Short: "Browse and search Orkestra note functions",
	Long: `List, search, and inspect Orkestra notes — the template functions available
in every Katalog expression.

  ork notes                    list all notes
  ork notes --domain strings   filter by domain
  ork notes search <term>      full-text search
  ork notes show <name>        show full detail for one note`,
	RunE: func(cmd *cobra.Command, args []string) error {
		domain, _ := cmd.Flags().GetString("domain")
		noPager, _ := cmd.Flags().GetBool("no-pager")
		files, _ := cmd.Flags().GetStringSlice("file")
		expanded := parseFilePaths(files)

		// When -f is provided, show user-defined notes from the Katalog.
		if len(expanded) > 0 {
			m := merger.New(expanded...)
			if err := m.Merge(); err != nil {
				return fmt.Errorf("loading katalog: %w", err)
			}
			reg := m.ToNotes()
			if reg.Empty() {
				fmt.Println("No user-defined notes declared in this Katalog.")
				return nil
			}
			return printUserNoteTable(reg, noPager)
		}

		var notes []note.NoteInfo
		if domain != "" {
			notes = note.ListByDomain(domain)
			if len(notes) == 0 {
				return fmt.Errorf("no notes found for domain %q — run `ork notes` to see available domains", domain)
			}
		} else {
			notes = note.ListNotes()
		}
		return printNoteTable(notes, noPager)
	},
}

var notesSearchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "Search notes by name, description, or example",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		noPager, _ := cmd.Flags().GetBool("no-pager")
		results := note.SearchNotes(args[0])
		if len(results) == 0 {
			fmt.Printf("no notes match %q\n", args[0])
			return nil
		}
		fmt.Printf("Found %d note(s) matching %q:\n\n", len(results), args[0])
		return printNoteTable(results, noPager)
	},
}

var notesShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show full documentation for a single note",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		noPager, _ := cmd.Flags().GetBool("no-pager")
		n, ok := note.GetNote(args[0])
		if !ok {
			return fmt.Errorf("note %q not found — run `ork notes search %s` to find similar notes", args[0], args[0])
		}
		return printNoteDetail(n, noPager)
	},
}

var notesDomainCmd = &cobra.Command{
	Use:   "domains",
	Short: "List available note domains",
	RunE: func(cmd *cobra.Command, args []string) error {
		domains := note.Domains()
		fmt.Println("Available domains:")
		fmt.Println()
		for _, d := range domains {
			notes := note.ListByDomain(d)
			fmt.Printf("  %-16s  %d notes\n", d, len(notes))
		}
		return nil
	},
}

func init() {
	notesCmd.Flags().StringP("domain", "d", "", "Filter by domain (e.g. strings, cron, kubernetes)")
	notesCmd.Flags().Bool("no-pager", false, "Print directly without paging")
	notesSearchCmd.Flags().Bool("no-pager", false, "Print directly without paging")
	notesShowCmd.Flags().Bool("no-pager", false, "Print directly without paging")

	notesCmd.AddCommand(notesSearchCmd)
	notesCmd.AddCommand(notesShowCmd)
	notesCmd.AddCommand(notesDomainCmd)
	rootCmd.AddCommand(notesCmd)

	// Shadow global flags
	shadowGlobalCommandFlags(notesCmd)
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func printNoteTable(notes []note.NoteInfo, noPager bool) error {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "DOMAIN\tNAME\tDESCRIPTION")
	fmt.Fprintln(w, "──────\t────\t───────────")
	for _, n := range notes {
		desc := n.Description
		if len(desc) > 72 {
			desc = desc[:69] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", n.Domain, n.Name, desc)
	}
	w.Flush()
	return page(sb.String(), noPager)
}

func printNoteDetail(n note.NoteInfo, noPager bool) error {
	sep := strings.Repeat("─", 64)
	var sb strings.Builder
	fmt.Fprintln(&sb, sep)
	fmt.Fprintf(&sb, "  %s\n", n.Name)
	fmt.Fprintln(&sb, sep)
	fmt.Fprintf(&sb, "  Domain:      %s\n", n.Domain)
	if n.Description != "" {
		fmt.Fprintf(&sb, "  Description: %s\n", n.Description)
	}
	if len(n.Keywords) > 0 {
		fmt.Fprintf(&sb, "  Keywords:    %s\n", strings.Join(n.Keywords, ", "))
	}
	if len(n.SeeAlso) > 0 {
		fmt.Fprintf(&sb, "  See also:    %s\n", strings.Join(n.SeeAlso, ", "))
	}
	if n.Example != "" {
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "  Example:")
		for _, line := range strings.Split(n.Example, "\n") {
			fmt.Fprintf(&sb, "    %s\n", line)
		}
	}
	fmt.Fprintln(&sb, sep)
	return page(sb.String(), noPager)
}

// page writes output through `less` when stdout is a terminal, otherwise prints directly.
func page(content string, noPager bool) error {
	if noPager || !isTerminal() {
		fmt.Print(content)
		return nil
	}
	pager := exec.Command("less", "-RFX")
	pager.Stdin = strings.NewReader(content)
	pager.Stdout = os.Stdout
	pager.Stderr = os.Stderr
	if err := pager.Run(); err != nil {
		fmt.Print(content)
	}
	return nil
}

func printUserNoteTable(reg orktypes.NoteRegistry, noPager bool) error {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tEXPRESSION\tDESCRIPTION")
	fmt.Fprintln(w, "────\t──────────\t───────────")
	for _, n := range reg.Functions {
		expr := n.Expression
		if len(expr) > 60 {
			expr = expr[:57] + "..."
		}
		desc := n.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", n.Name, expr, desc)
	}
	w.Flush()
	return page(sb.String(), noPager)
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
