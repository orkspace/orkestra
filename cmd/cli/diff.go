//go:build !runtime && !gateway

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <file1> <file2>",
	Short: "Show a unified diff between two files",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		file1 := args[0]
		file2 := args[1]
		verbose, _ := cmd.Flags().GetBool("verbose")

		a, err := readLocal(file1)
		if err != nil {
			return fmt.Errorf("reading %s: %w", file1, err)
		}

		b, err := readLocal(file2)
		if err != nil {
			return fmt.Errorf("reading %s: %w", file2, err)
		}

		diff := unifiedDiff(file1, file2, a, b, verbose)
		fmt.Println(diff)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)

	// Shadow global flags so they don't appear under `ork diff`
	diffCmd.Flags().StringSlice("katalog", nil, "")
	diffCmd.Flags().MarkHidden("katalog")
	shadowGlobalCommandFlags(diffCmd)
}

// Helper
func unifiedDiff(nameA, nameB string, a, b []byte, verbose bool) string {
	alines := strings.Split(string(a), "\n")
	blines := strings.Split(string(b), "\n")

	var out []string
	out = append(out, gray("--- "+nameA))
	out = append(out, gray("+++ "+nameB))

	max := len(alines)
	if len(blines) > max {
		max = len(blines)
	}

	for i := 0; i < max; i++ {
		var A, B string
		if i < len(alines) {
			A = alines[i]
		}
		if i < len(blines) {
			B = blines[i]
		}

		switch {
		case A == B:
			if verbose {
				out = append(out, " "+A)
			}
		case A == "":
			out = append(out, green("+"+B))
		case B == "":
			out = append(out, red("-"+A))
		default:
			out = append(out, red("-"+A))
			out = append(out, green("+"+B))
		}
	}

	return strings.Join(out, "\n")
}
