// Package ask answers questions about a codebase from the knowledge graph, with every
// claim cited to file:line.
package ask

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/llm"
	"github.com/kuli-dev/kulidev/internal/store"
)

// Options configures one question.
type Options struct {
	Root string       // repository root, for reading code
	LLM  docs.Chatter // nil → retrieval only
	Log  io.Writer
	// LinkBase renders citations as links (e.g. "#/file" for the web UI);
	// empty renders plain "path:line".
	LinkBase string
}

// Answer is the grounded reply.
type Answer struct {
	Text     string // with citations rendered as path:line
	Stats    docs.CiteStats
	Keywords []string
	Hits     int
	Facts    string // what the model was shown (for --show-evidence / retrieval-only)
}

const (
	maxHits     = 30 // definitions kept after re-ranking
	maxSnippets = 6  // definitions whose code is shown
	snippetCap  = 45 // lines of code per definition
	maxRelated  = 4  // files whose import relationships are shown
)

const systemPrompt = `You answer a developer's question about their codebase.

Rules:
- Use ONLY the FACTS. Never invent files, functions, endpoints, config or behaviour.
- End every factual sentence or bullet with citations copied from the facts, e.g. [E3] or [E3 L10-14]
  (line-qualified cites must stay inside the evidence span shown in the EVIDENCE INDEX).
- If the facts are not enough to answer, say so plainly and name the files most worth reading next.
- Be direct and concise: answer first, then supporting detail. Markdown is fine; no top-level title.`

const keywordPrompt = `List 6-10 search keywords that would appear in function, class, file or variable
names, or in doc comments, of code relevant to this question about a codebase. Include likely
identifier spellings (e.g. "invoice", "reconcile", "payment"). Output ONLY the words, comma separated.

Question: %s`

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the is are was were be been being do does did how what where which who whom why when
		this that these those it its of in on at to for from by with about into as and or not no can could should would will
		i we you they me my our your there here does doing any all some code codebase repo repository file files function
		apa yang bagaimana gimana di ini itu dan untuk dari ke kenapa mana siapa kapan dengan atau tidak bisa ada`) {
		stop[w] = true
	}
}

var wordRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}`)

// questionWords extracts candidate search terms from the question itself.
func questionWords(q string) []string {
	var out []string
	for _, w := range wordRe.FindAllString(q, -1) {
		if !stop[strings.ToLower(w)] {
			out = append(out, w)
		}
	}
	return out
}

// Ask retrieves evidence for the question and, when a model is configured, answers it.
func Ask(ctx context.Context, st *store.Store, question string, opts Options) (*Answer, error) {
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	ans := &Answer{Keywords: questionWords(question)}
	if opts.LLM != nil {
		if kw := modelKeywords(ctx, opts.LLM, question); len(kw) > 0 {
			ans.Keywords = append(ans.Keywords, kw...)
		}
	}
	if len(ans.Keywords) == 0 {
		return nil, fmt.Errorf("could not find anything to search for in %q", question)
	}
	fmt.Fprintf(opts.Log, "kuli: searching for %s\n", strings.Join(dedupe(ans.Keywords), ", "))

	hits, err := st.FindAny(ans.Keywords, maxHits*3)
	if err != nil {
		return nil, err
	}
	hits = rerank(hits, opts.Root, ans.Keywords, question)
	ans.Hits = len(hits)
	f := buildFacts(st, opts.Root, question, hits, ans.Keywords)
	ans.Facts = f.Text()

	if opts.LLM == nil || len(hits) == 0 {
		if len(hits) == 0 {
			ans.Text = "Nothing in the knowledge base matched. Try other words, or run `kuli scan` if the code changed."
		}
		return ans, nil
	}
	reply, _, err := opts.LLM.Chat(ctx, []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "QUESTION: " + question + "\n\nFACTS\n" + ans.Facts},
	})
	if err != nil {
		return nil, err
	}
	ans.Text, ans.Stats = f.Resolve(reply, opts.LinkBase)
	return ans, nil
}

// modelKeywords asks the model for identifier-like search terms. Failures are not
// fatal: the question's own words still drive retrieval.
func modelKeywords(ctx context.Context, c docs.Chatter, question string) []string {
	out, _, err := c.Chat(ctx, []llm.Message{{Role: "user", Content: fmt.Sprintf(keywordPrompt, question)}})
	if err != nil {
		return nil
	}
	var kw []string
	for _, w := range regexp.MustCompile(`[,\n;]+`).Split(out, -1) {
		w = strings.Trim(strings.TrimSpace(w), "`\"'*-.")
		if w != "" && len(w) <= 40 && !strings.Contains(w, " ") && !stop[strings.ToLower(w)] {
			kw = append(kw, w)
		}
		if len(kw) == 10 {
			break
		}
	}
	return kw
}

func buildFacts(st *store.Store, root, question string, hits []store.Hit, keywords []string) *docs.Facts {
	f := docs.NewFacts()
	f.Line("QUESTION: %s", question)
	f.Line("\nMATCHING DEFINITIONS (best first):")
	var code []store.Hit
	files := []string{}
	seenFile := map[string]bool{}
	for _, h := range hits {
		if h.Path == "" {
			continue // external modules have no source here
		}
		cite := f.Cite(h.Path, h.Line, h.EndLine)
		doc := ""
		if h.Detail != "" {
			doc = fmt.Sprintf(" — documented as %q", h.Detail)
		}
		f.Line("- %s %s in %s%s %s", strings.ToLower(h.Kind), h.Name, h.Path, doc, cite)
		if h.Kind != "FILE" && h.Kind != "PACKAGE" && len(code) < maxSnippets {
			code = append(code, h)
		}
		if !seenFile[h.Path] {
			seenFile[h.Path] = true
			files = append(files, h.Path)
		}
	}

	if len(code) > 0 {
		f.Line("\nCODE:")
	}
	for _, h := range code {
		lines := readLines(root, h.Path)
		if len(lines) == 0 || h.Line < 1 {
			continue
		}
		end := min(h.EndLine, len(lines))
		f.Line("\n%s %s (%s lines %d-%d) %s:", strings.ToLower(h.Kind), h.Name, h.Path, h.Line, end, f.Cite(h.Path, h.Line, end))
		prev := 0
		for _, i := range excerpt(lines, h.Line, end, keywords) {
			if prev != 0 && i != prev+1 {
				f.Line("  ...|")
			}
			f.Line("%5d| %s", i, docs.Redact(lines[i-1]))
			prev = i
		}
	}

	if len(files) > 0 {
		f.Line("\nRELATIONSHIPS:")
	}
	for i, p := range files {
		if i == maxRelated {
			break
		}
		if _, users, err := st.Deps(p, true); err == nil && len(users) > 0 {
			var ex []string
			for j, u := range users {
				if j == 3 {
					break
				}
				ex = append(ex, u.Path+" "+f.Cite(u.Path, u.Line, u.Line))
			}
			f.Line("- %s is imported by %d files, e.g. %s", p, len(users), strings.Join(ex, "; "))
		}
		if _, deps, err := st.Deps(p, false); err == nil && len(deps) > 0 {
			var names []string
			for j, d := range deps {
				if j == 8 {
					break
				}
				names = append(names, strings.TrimPrefix(d.Name, "unresolved:"))
			}
			f.Line("- %s imports %s %s", p, strings.Join(names, ", "), f.Cite(p, deps[0].Line, deps[len(deps)-1].Line))
		}
	}
	return f
}

// excerpt picks which lines of a definition to show: all of it when short, otherwise
// its head plus a few lines around every line mentioning a keyword, up to snippetCap.
func excerpt(lines []string, start, end int, keywords []string) []int {
	var out []int
	if end-start+1 <= snippetCap {
		for i := start; i <= end; i++ {
			out = append(out, i)
		}
		return out
	}
	stems := stemsOf(keywords)
	keep := map[int]bool{}
	for i := start; i < start+12 && i <= end; i++ {
		keep[i] = true
	}
	for i := start; i <= end && len(keep) < snippetCap; i++ {
		low := strings.ToLower(lines[i-1])
		for _, st := range stems {
			if strings.Contains(low, st) {
				for j := max(start, i-3); j <= min(end, i+3); j++ {
					keep[j] = true
				}
				break
			}
		}
	}
	keep[end] = true
	for i := start; i <= end; i++ {
		if keep[i] {
			out = append(out, i)
		}
	}
	if len(out) > snippetCap+8 {
		out = out[:snippetCap+8]
	}
	return out
}

// rerank orders search hits by how much of the question each definition covers:
// distinct keywords found in its name, doc and code, preferring functions over
// declarations, and pushing tests down unless the question is about tests.
func rerank(hits []store.Hit, root string, keywords []string, question string) []store.Hit {
	stems := stemsOf(keywords)
	aboutTests := strings.Contains(strings.ToLower(question), "test")
	cache := map[string][]string{}
	type scored struct {
		h store.Hit
		s int
	}
	var all []scored
	for i, h := range hits {
		text := strings.ToLower(h.Name + " " + h.Detail)
		if h.Path != "" && h.Line > 0 && h.Kind != "FILE" {
			lines, ok := cache[h.Path]
			if !ok {
				lines = readLines(root, h.Path)
				cache[h.Path] = lines
			}
			for j := h.Line; j <= h.EndLine && j <= len(lines); j++ {
				text += "\n" + strings.ToLower(lines[j-1])
			}
		}
		score := len(hits) - i // search rank still counts
		for _, st := range stems {
			if strings.Contains(text, st) {
				score += 15
			}
		}
		switch h.Kind {
		case "function", "method":
			score += 5
		}
		if !aboutTests && isTestFile(h.Path) {
			score -= 40
		}
		all = append(all, scored{h, score})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].s > all[j].s })
	out := make([]store.Hit, 0, maxHits)
	for _, a := range all {
		if len(out) == maxHits {
			break
		}
		out = append(out, a.h)
	}
	return out
}

func isTestFile(p string) bool {
	b := strings.ToLower(path.Base(p))
	return strings.HasSuffix(b, "_test.go") || strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.py") ||
		strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") || strings.Contains(p, "/tests/") || strings.HasPrefix(p, "tests/")
}

// stemsOf lowercases keywords and cuts them to a crude 5-letter stem so that
// "redaction" and "redact" (or "empty" and "emptied") meet.
func stemsOf(keywords []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range keywords {
		k = strings.ToLower(k)
		if len(k) > 5 {
			k = k[:5]
		}
		if len(k) >= 3 && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func readLines(root, rel string) []string {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

func dedupe(ws []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range ws {
		if k := strings.ToLower(w); !seen[k] {
			seen[k] = true
			out = append(out, w)
		}
	}
	return out
}
