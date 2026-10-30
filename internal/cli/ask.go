package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/ask"
)

func newAskCmd(o *options) *cobra.Command {
	var (
		lf           llmFlags
		showEvidence bool
	)
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask a question about the codebase; the answer cites file:line",
		Example: `  kuli ask "how are invoices reconciled with bank statements?"
  kuli ask "where is the database session created and who uses it?"
  kuli ask "kenapa upload dokumen lambat?"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := o.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			root, err := filepath.Abs(o.repo)
			if err != nil {
				return err
			}
			opts := ask.Options{Root: root, Log: os.Stderr}
			if c, err := lf.client(); err != nil {
				fmt.Fprintf(os.Stderr, "kuli: %v\nkuli: showing the evidence it found instead of an answer\n", err)
				showEvidence = true
			} else {
				opts.LLM = c
			}
			a, err := ask.Ask(cmd.Context(), st, strings.Join(args, " "), opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if showEvidence {
				fmt.Fprintln(out, a.Facts)
			}
			if a.Text != "" {
				fmt.Fprintln(out, a.Text)
			}
			if opts.LLM != nil && a.Hits > 0 {
				fmt.Fprintf(os.Stderr, "\nkuli: %.0f%% of claims cited (%d citations", a.Stats.Coverage()*100, a.Stats.Valid)
				if a.Stats.Removed > 0 {
					fmt.Fprintf(os.Stderr, ", %d invented ones removed", a.Stats.Removed)
				}
				fmt.Fprintln(os.Stderr, ")")
			}
			return nil
		},
	}
	lf.add(cmd, 2048)
	cmd.Flags().BoolVar(&showEvidence, "show-evidence", false, "print the evidence the answer was built from")
	return cmd
}
