// Package report renders knowledge-base data as human-readable text.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kuli-dev/kulidev/internal/store"
)

// Stats renders a knowledge-base summary for terminals and agents alike.
func Stats(out io.Writer, s *store.Stats) {
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
