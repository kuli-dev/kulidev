package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/report"
	"github.com/kuli-dev/kulidev/internal/store"
)

func newScanCmd(o *options) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Analyse a repository and build or update its knowledge base",
		Long: `Builds the knowledge base on first run; afterwards only files whose size, mtime or
content changed are re-parsed. Use --full to rebuild from scratch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				o.repo = args[0]
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "kuli lagi kerja… scanning %s\n", o.repo)
			root, err := filepath.Abs(o.repo)
			if err != nil {
				return err
			}
			dir, err := o.resolvedDataDir()
			if err != nil {
				return err
			}
			st, err := store.Open(dir)
			if err != nil {
				return err
			}
			defer st.Close()
			sum, err := st.Refresh(root, full)
			if err != nil {
				return fmt.Errorf("update knowledge base: %w", err)
			}
			stats, err := st.Stats()
			if err != nil {
				return err
			}
			report.Stats(out, stats)
			rel, _ := filepath.Rel(root, filepath.Join(dir, store.DBFile))
			if strings.HasPrefix(rel, "..") {
				rel = filepath.Join(dir, store.DBFile)
			}
			fmt.Fprintf(out, "\nUpdated    %s (%s)\n", rel, sum)
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "rebuild the knowledge base from scratch")
	return cmd
}

func newStatsCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Summarise the knowledge base from the last scan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := o.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			stats, err := st.Stats()
			if err != nil {
				return err
			}
			report.Stats(cmd.OutOrStdout(), stats)
			return nil
		},
	}
}

func newFindCmd(o *options) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "find <query>",
		Short: "Search symbols, files and modules (camelCase and snake_case aware)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := o.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			hits, err := st.Find(strings.Join(args, " "), limit)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(hits) == 0 {
				fmt.Fprintln(out, "nothing found")
				return nil
			}
			for _, h := range hits {
				fmt.Fprintf(out, "%-10s %-40s %s\n", h.Kind, h.Name, location(h.Path, h.Line))
				if h.Detail != "" {
					fmt.Fprintf(out, "           %s\n", truncate(h.Detail, 110))
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 25, "maximum results")
	return cmd
}

func newDepsCmd(o *options) *cobra.Command {
	var reverse bool
	cmd := &cobra.Command{
		Use:   "deps <file|package|module>",
		Short: "Show what something imports, or with --reverse who imports it",
		Example: `  kuli deps src/api/client.ts
  kuli deps internal/store --reverse
  kuli deps axios --reverse`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := o.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			name, hits, err := st.Deps(args[0], reverse)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			verb := "imports"
			if reverse {
				verb = "is imported by"
			}
			fmt.Fprintf(out, "%s %s %d:\n", name, verb, len(hits))
			for _, h := range hits {
				target := h.Name
				if reverse {
					target = ""
				}
				fmt.Fprintf(out, "  %-50s %s\n", location(h.Path, h.Line), target)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&reverse, "reverse", "r", false, "list importers instead of imports")
	return cmd
}

func location(path string, line int) string {
	if path == "" {
		return ""
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d", path, line)
	}
	return path
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
