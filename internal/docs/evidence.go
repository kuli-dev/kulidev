package docs

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Evidence is a citable source location.
type Evidence struct {
	ID            int
	Path          string
	Line, EndLine int
}

// Facts collects evidence-backed statements for one document. Every fact the
// LLM sees carries an [E#] tag it must reuse when it writes prose.
type Facts struct {
	ev    []Evidence
	byKey map[string]int
	b     strings.Builder
}

func newFacts() *Facts { return &Facts{byKey: map[string]int{}} }

// Cite registers a source location and returns its tag, e.g. "[E7]".
func (f *Facts) Cite(path string, line, end int) string {
	key := fmt.Sprintf("%s:%d-%d", path, line, end)
	if id, ok := f.byKey[key]; ok {
		return fmt.Sprintf("[E%d]", id)
	}
	id := len(f.ev) + 1
	f.ev = append(f.ev, Evidence{ID: id, Path: path, Line: line, EndLine: end})
	f.byKey[key] = id
	return fmt.Sprintf("[E%d]", id)
}

// Line appends one fact line.
func (f *Facts) Line(format string, args ...any) {
	fmt.Fprintf(&f.b, format, args...)
	f.b.WriteByte('\n')
}

// Text returns the facts followed by the evidence index.
func (f *Facts) Text() string {
	var b strings.Builder
	b.WriteString(f.b.String())
	b.WriteString("\nEVIDENCE INDEX\n")
	for _, e := range f.ev {
		fmt.Fprintf(&b, "[E%d] %s\n", e.ID, e.loc())
	}
	return b.String()
}

// Body returns just the fact lines (used when no LLM is configured).
func (f *Facts) Body() string { return f.b.String() }

// Files returns the distinct files cited.
func (f *Facts) Files() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range f.ev {
		if !seen[e.Path] {
			seen[e.Path] = true
			out = append(out, e.Path)
		}
	}
	return out
}

func (e Evidence) loc() string {
	switch {
	case e.Line <= 0:
		return e.Path
	case e.EndLine > e.Line:
		return fmt.Sprintf("%s:%d-%d", e.Path, e.Line, e.EndLine)
	}
	return fmt.Sprintf("%s:%d", e.Path, e.Line)
}

// citeRe matches [E12], [E12 L40], [E12 L40-52] and [E3, E7] style lists.
var citeRe = regexp.MustCompile(`\[(E\d+(?:\s*L\d+(?:-\d+)?)?(?:\s*[,;]\s*E\d+(?:\s*L\d+(?:-\d+)?)?)*)\]`)
var oneCiteRe = regexp.MustCompile(`E(\d+)(?:\s*L(\d+)(?:-(\d+))?)?`)

// CiteStats reports how well a generated document is grounded.
type CiteStats struct {
	Valid, Removed       int
	Paragraphs, Grounded int
}

// Coverage is the share of prose paragraphs that carry at least one valid citation.
func (s CiteStats) Coverage() float64 {
	if s.Paragraphs == 0 {
		return 1
	}
	return float64(s.Grounded) / float64(s.Paragraphs)
}

// Resolve turns [E#] tags into Markdown links relative to linkBase, drops tags that
// point at unknown evidence or out-of-range lines, and measures grounding.
func (f *Facts) Resolve(md, linkBase string) (string, CiteStats) {
	var st CiteStats
	// Grounding is judged on the model's own citations, before links are rendered,
	// so links the model invents itself never count as evidence.
	forEachClaim(md, func(claim string) {
		st.Paragraphs++
		for _, m := range citeRe.FindAllString(claim, -1) {
			for _, c := range oneCiteRe.FindAllStringSubmatch(m, -1) {
				if _, ok := f.check(c); ok {
					st.Grounded++
					return
				}
			}
		}
	})
	out := citeRe.ReplaceAllStringFunc(md, func(m string) string {
		var links []string
		for _, c := range oneCiteRe.FindAllStringSubmatch(m, -1) {
			e, ok := f.check(c)
			if !ok {
				st.Removed++
				continue
			}
			st.Valid++
			links = append(links, link(e, linkBase))
		}
		if len(links) == 0 {
			return ""
		}
		return "(" + strings.Join(links, ", ") + ")"
	})
	return out, st
}

// check validates one parsed citation and returns the evidence it points at,
// narrowed to the cited lines.
func (f *Facts) check(c []string) (Evidence, bool) {
	id, _ := strconv.Atoi(c[1])
	if id < 1 || id > len(f.ev) {
		return Evidence{}, false
	}
	e := f.ev[id-1]
	if c[2] == "" {
		return e, true
	}
	l, _ := strconv.Atoi(c[2])
	end := l
	if c[3] != "" {
		end, _ = strconv.Atoi(c[3])
	}
	// A line-qualified cite must fall inside the evidence span when one is known.
	if end < l || (e.EndLine > 0 && (l < e.Line || end > e.EndLine)) {
		return Evidence{}, false
	}
	e.Line, e.EndLine = l, end
	return e, true
}

// forEachClaim calls fn for every prose paragraph and every list item, skipping
// headings, tables and fenced code.
func forEachClaim(md string, fn func(string)) {
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			fn(strings.Join(cur, " "))
			cur = cur[:0]
		}
	}
	inFence := false
	for _, ln := range strings.Split(md, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "```"):
			flush()
			inFence = !inFence
		case inFence:
		case t == "", strings.HasPrefix(t, "#"), strings.HasPrefix(t, "|"):
			flush()
		case isListItem(t):
			flush()
			cur = append(cur, t)
		default:
			cur = append(cur, t)
		}
	}
	flush()
}

func isListItem(t string) bool {
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
		return true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i < len(t) && t[i] == '.'
}

func link(e Evidence, base string) string {
	target := filepath.ToSlash(filepath.Join(base, e.Path))
	label := e.loc()
	if e.Line > 0 {
		target += fmt.Sprintf("#L%d", e.Line)
		if e.EndLine > e.Line {
			target += fmt.Sprintf("-L%d", e.EndLine)
		}
	}
	return fmt.Sprintf("[%s](%s)", label, strings.ReplaceAll(target, " ", "%20"))
}
