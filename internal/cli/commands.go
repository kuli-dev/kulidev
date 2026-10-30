package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kulidev/kulidev/internal/scan"
	"github.com/kulidev/kulidev/internal/store"
)

func newScanCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "scan [path]",
		Short: "Analyse a repository and build its knowledge base",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				o.repo = args[0]
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "kuli lagi kerja… scanning %s\n", o.repo)

			res, err := scan.Scan(o.repo)
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
			if _, err := st.Save(res); err != nil {
				return fmt.Errorf("save knowledge base: %w", err)
			}
			stats, err := st.Stats()
			if err != nil {
				return err
			}
			printStats(out, stats)
			rel, _ := filepath.Rel(res.Root, filepath.Join(dir, store.DBFile))
			if strings.HasPrefix(rel, "..") {
				rel = filepath.Join(dir, store.DBFile)
			}
			fmt.Fprintf(out, "\nSaved      %s\n", rel)
			return nil
		},
	}
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
			printStats(cmd.OutOrStdout(), stats)
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

func printStats(out io.Writer, s *store.Stats) {
	commit := s.Run.Commit
	if len(commit) > 10 {
		commit = commit[:10]
	}
	if commit == "" {
		commit = "not a git repo"
	}
	fmt.Fprintf(out, "\nRepo       %s (%s)\n", s.Run.Root, commit)
	fmt.Fprintf(out, "Files      %s total · %s parsed · %d errors · %s\n",
		num(s.Run.Files), num(s.Parsed), s.Errors, (time.Duration(s.Run.DurationMS) * time.Millisecond).String())

	var langs []string
	for i, l := range s.Languages {
		if i == 8 {
			langs = append(langs, "…")
			break
		}
		langs = append(langs, fmt.Sprintf("%s %s files/%s lines", l.Label, num(l.N), num(l.Lines)))
	}
	section(out, "Languages", langs)

	var syms []string
	for _, c := range s.Symbols {
		syms = append(syms, fmt.Sprintf("%s %s", num(c.N), plural(c.Label)))
	}
	section(out, "Symbols", syms)
	fmt.Fprintf(out, "Imports    %s internal edges · %s external modules\n", num(s.InternalEdges), num(s.ExternalMods))

	var infra []string
	for _, m := range s.Manifests {
		infra = append(infra, fmt.Sprintf("%s ×%d", m.Label, m.N))
	}
	section(out, "Infra/Build", infra)

	if len(s.TopInternal) > 0 {
		fmt.Fprintln(out, "\nMost depended-on (internal):")
		for _, c := range s.TopInternal {
			fmt.Fprintf(out, "  %4d ← %s\n", c.N, c.Label)
		}
	}
	if len(s.TopExternal) > 0 {
		fmt.Fprintln(out, "\nMost used external modules:")
		for _, c := range s.TopExternal {
			fmt.Fprintf(out, "  %4d ← %s\n", c.N, c.Label)
		}
	}
}

func section(out io.Writer, title string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(out, "%-10s —\n", title)
		return
	}
	// wrap at ~3 items per line to stay readable in a terminal
	for i := 0; i < len(items); i += 3 {
		end := min(i+3, len(items))
		label := ""
		if i == 0 {
			label = title
		}
		fmt.Fprintf(out, "%-10s %s\n", label, strings.Join(items[i:end], " · "))
	}
}

func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "s"):
		return s + "es"
	case s == "":
		return s
	}
	return s + "s"
}

func num(n int64) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
