// Package docs is DocsMana: it turns the knowledge graph into handover documentation
// where every claim links back to the code that supports it.
package docs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kuli-dev/kulidev/internal/llm"
	"github.com/kuli-dev/kulidev/internal/store"
)

// Chatter is the subset of the LLM client the generator needs.
type Chatter interface {
	Chat(ctx context.Context, msgs []llm.Message) (string, llm.Usage, error)
	Model() string
}

// Options configures a generation run.
type Options struct {
	OutDir  string    // where the Markdown is written
	LLM     Chatter   // nil → facts-only documents
	Only    []string  // doc IDs to generate; empty = all
	Version string    // generator version for front matter
	Log     io.Writer // progress (stderr)
}

// DocResult summarises one generated document.
type DocResult struct {
	ID       string            `json:"id"`
	File     string            `json:"file"`
	Title    string            `json:"title"`
	Mode     string            `json:"mode"` // "llm", "llm+fallback", "facts-only" or "deterministic"
	Fallback int               `json:"fallback_sections,omitempty"`
	Valid    int               `json:"citations"`
	Removed  int               `json:"citations_removed"`
	Coverage float64           `json:"citation_coverage"`
	Evidence map[string]string `json:"evidence"` // cited file → sha256 at scan time
}

// Manifest is written next to the documents and drives `kuli docs status`.
type Manifest struct {
	Generator   string      `json:"generator"`
	GeneratedAt string      `json:"generated_at"`
	Commit      string      `json:"source_commit"`
	Model       string      `json:"model,omitempty"`
	Docs        []DocResult `json:"docs"`
}

// ManifestFile is the manifest's file name inside OutDir.
const ManifestFile = "manifest.json"

// DocIDs lists the documents in generation order.
var DocIDs = []string{"overview", "architecture", "dependencies", "infrastructure", "getting-started"}

const systemPrompt = `You are DocsMana, KuliDev's documentation writer. You write engineering handover
documentation in Markdown for a developer who just inherited this codebase.

Rules:
- Use ONLY the FACTS provided. Never invent files, functions, services, endpoints, commands or behaviour.
- End every factual sentence or bullet with citations copied from the facts, e.g. [E12] or [E12 L40-52]
  (line-qualified cites must stay inside the evidence span shown in the EVIDENCE INDEX).
- If the facts do not show something, say it could not be determined from the code.
- Inferences (e.g. what a component is probably for, judged from names) must be worded as inferences.
- Start with ## headings (no # title). Be concise; prefer short paragraphs and bullet lists.
- Do not wrap the whole answer in a code fence. Do not add a sources section.`

type generator struct {
	r     *repo
	opts  Options
	usage llm.Usage
}

// Generate writes the documents and the manifest, returning per-document results.
func Generate(ctx context.Context, snap *store.Snapshot, opts Options) (*Manifest, error) {
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, err
	}
	g := &generator{r: newRepo(snap), opts: opts}
	want := map[string]bool{}
	for _, id := range opts.Only {
		want[id] = true
	}

	m := &Manifest{
		Generator:   "kulidev " + opts.Version,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Commit:      snap.Run.Commit,
	}
	if opts.LLM != nil {
		m.Model = opts.LLM.Model()
	}
	// Keep entries for documents not regenerated this run.
	if prev, err := ReadManifest(opts.OutDir); err == nil {
		for _, d := range prev.Docs {
			if len(want) > 0 && !want[d.ID] {
				m.Docs = append(m.Docs, d)
			}
		}
	}

	for _, id := range DocIDs {
		if len(want) > 0 && !want[id] {
			continue
		}
		fmt.Fprintf(opts.Log, "kuli: writing %s…\n", id)
		res, err := g.doc(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		if res == nil {
			fmt.Fprintf(opts.Log, "kuli: skipped %s (nothing to document)\n", id)
			continue
		}
		m.Docs = append(m.Docs, *res)
	}
	sort.SliceStable(m.Docs, func(i, j int) bool { return docOrder(m.Docs[i].ID) < docOrder(m.Docs[j].ID) })

	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(opts.OutDir, ManifestFile), b, 0o644); err != nil {
		return nil, err
	}
	if g.usage.Prompt+g.usage.Completion > 0 {
		fmt.Fprintf(opts.Log, "kuli: LLM usage %d prompt + %d completion tokens\n", g.usage.Prompt, g.usage.Completion)
	}
	return m, nil
}

func docOrder(id string) int {
	for i, d := range DocIDs {
		if d == id {
			return i
		}
	}
	return len(DocIDs)
}

// section is one part of a document: either fixed Markdown or facts for the LLM.
type section struct {
	markdown string
	facts    *Facts
	task     string // what the LLM should write from the facts
}

func (g *generator) doc(ctx context.Context, id string) (*DocResult, error) {
	r := g.r
	var title, file string
	var parts []section
	switch id {
	case "overview":
		title, file = "Project overview", "project-overview.md"
		parts = []section{{facts: r.overviewFacts(), task: `Write: "## What this is" (purpose, judged from README and names), ` +
			`"## Main components" (bullets), "## Tech stack", "## Where to start reading" (the most depended-on files and why).`}}
	case "architecture":
		title, file = "Architecture", "architecture.md"
		parts = append(parts, section{markdown: "## Component map\n\nDirectories as components; arrows are imports, " +
			"labelled with the number of importing files.\n\n" + r.mermaid(16, 30)})
		comps := r.comps
		if len(comps) > maxDetailedComponents {
			comps = comps[:maxDetailedComponents]
		}
		parts = append(parts, section{markdown: "## Components"})
		for i := 0; i < len(comps); i += 4 {
			f := NewFacts()
			var names []string
			for _, c := range comps[i:min(i+4, len(comps))] {
				r.componentFacts(f, c)
				names = append(names, c.Name)
			}
			parts = append(parts, section{facts: f, task: "For each of these components (" + strings.Join(names, ", ") +
				`) write "### <component name>" then 2-5 sentences: its likely responsibility (worded as inference), ` +
				`its key types/functions and which components it depends on or serves. Do not write a ## heading.`})
		}
		if n := len(r.comps) - len(comps); n > 0 {
			parts = append(parts, section{markdown: fmt.Sprintf("_%d smaller components are listed in the dependency map._", n)})
		}
	case "dependencies":
		title, file = "Dependency map", "dependency-map.md"
		parts = []section{{markdown: r.dependencyTables()}}
	case "infrastructure":
		f := r.infraFacts()
		if f == nil {
			return nil, nil
		}
		title, file = "Infrastructure & deployment", "infrastructure.md"
		parts = []section{{facts: f, task: `Write: "## Overview" (how the system is built and deployed, judged from these files), ` +
			`"## Containers & images", "## Orchestration" (Kubernetes/Compose/Helm, if present), "## CI/CD" (if present), ` +
			`"## Configuration & secrets" (which config/env is expected; never print secret values). Skip sections with no facts.`}}
	case "getting-started":
		title, file = "Getting started", "getting-started.md"
		parts = []section{{facts: r.gettingStartedFacts(), task: `Write: "## Prerequisites", "## Install", "## Run locally", ` +
			`"## Tests", "## Configuration". Use the exact commands shown in the facts in fenced shell blocks, with the citation ` +
			`on the line before the block. If a step cannot be determined, say so instead of guessing.`}}
	default:
		return nil, fmt.Errorf("unknown document %q", id)
	}

	linkBase, err := filepath.Rel(g.opts.OutDir, r.snap.Run.Root)
	if err != nil {
		linkBase = r.snap.Run.Root
	}
	res := &DocResult{ID: id, File: file, Title: title, Mode: "facts-only", Evidence: map[string]string{}}
	if g.opts.LLM != nil {
		res.Mode = "llm"
	}
	var body strings.Builder
	var paras, grounded int
	for _, p := range parts {
		if p.facts == nil {
			body.WriteString(p.markdown + "\n\n")
			continue
		}
		raw, err := g.write(ctx, title, p)
		if err != nil {
			if ctx.Err() != nil || !recoverable(err) {
				return nil, err
			}
			// Keep going: this section falls back to its raw facts, and the doc says so.
			fmt.Fprintf(g.opts.Log, "kuli: %s: %v — using facts for this section\n", id, err)
			raw = factsMarkdown(p.facts.Body())
			res.Fallback++
			res.Mode = "llm+fallback"
		}
		out, st := p.facts.Resolve(raw, linkBase)
		res.Valid += st.Valid
		res.Removed += st.Removed
		paras += st.Paragraphs
		grounded += st.Grounded
		for _, path := range p.facts.Files() {
			res.Evidence[path] = r.sha[path]
		}
		body.WriteString(strings.TrimSpace(out) + "\n\n")
	}
	if !hasFacts(parts) {
		res.Mode = "deterministic" // built from the graph alone; nothing for a model to ground
	}
	res.Coverage = 1
	if res.Mode != "facts-only" && paras == 0 && hasFacts(parts) {
		res.Coverage = 0
	}
	if paras > 0 {
		res.Coverage = float64(grounded) / float64(paras)
	}

	doc := g.frontMatter(res) + "# " + title + "\n\n" + modeNote(res) + body.String() +
		"---\n_Generated by [KuliDev](https://github.com/kuli-dev/kulidev) — docs-nya mana? Ini._\n"
	if err := os.WriteFile(filepath.Join(g.opts.OutDir, file), []byte(doc), 0o644); err != nil {
		return nil, err
	}
	return res, nil
}

// write asks the LLM for prose, or renders the raw facts when no LLM is configured.
func (g *generator) write(ctx context.Context, title string, p section) (string, error) {
	if g.opts.LLM == nil {
		return factsMarkdown(p.facts.Body()), nil
	}
	msgs := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Document: %s\nTask: %s\n\nFACTS\n%s", title, p.task, p.facts.Text())},
	}
	out, u, err := g.opts.LLM.Chat(ctx, msgs)
	g.usage.Prompt += u.Prompt
	g.usage.Completion += u.Completion
	return out, err
}

// factsMarkdown renders fact lines as a readable list; numbered source excerpts become code.
func factsMarkdown(body string) string {
	var b strings.Builder
	inCode := false
	for _, ln := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		excerpt := len(ln) > 5 && ln[4] == '|'
		switch {
		case excerpt && !inCode:
			b.WriteString("```text\n")
			inCode = true
		case !excerpt && inCode:
			b.WriteString("```\n")
			inCode = false
		}
		switch {
		case excerpt, strings.TrimSpace(ln) == "":
			b.WriteString(ln + "\n")
		case strings.HasPrefix(ln, "- ") || strings.HasPrefix(ln, "  "):
			b.WriteString(ln + "\n")
		default:
			b.WriteString("\n" + ln + "\n\n")
		}
	}
	if inCode {
		b.WriteString("```\n")
	}
	return b.String()
}

func hasFacts(parts []section) bool {
	for _, p := range parts {
		if p.facts != nil {
			return true
		}
	}
	return false
}

func recoverable(err error) bool {
	return errors.Is(err, llm.ErrThinkingBudget) || errors.Is(err, llm.ErrEmptyReply)
}

func modeNote(res *DocResult) string {
	if res.Mode == "llm+fallback" {
		return fmt.Sprintf("> **Partly facts-only** — the model returned no usable text for %d section(s), "+
			"which list the raw evidence instead.\n\n", res.Fallback)
	}
	if res.Mode == "facts-only" {
		return "> **Facts-only mode** — no LLM was configured, so this page lists the raw evidence. " +
			"Set `KULI_LLM_BASE_URL` and `KULI_LLM_MODEL` to get written documentation.\n\n"
	}
	return ""
}

func (g *generator) frontMatter(res *DocResult) string {
	model := ""
	if g.opts.LLM != nil {
		model = g.opts.LLM.Model()
	}
	return fmt.Sprintf(`---
title: %q
generated_by: "kulidev %s"
generated_at: %q
source_commit: %q
mode: %s
model: %q
citations: %d
citations_removed: %d
citation_coverage: %.2f
evidence_files: %d
status: generated
---

`, res.Title, g.opts.Version, time.Now().UTC().Format(time.RFC3339), g.r.snap.Run.Commit, res.Mode, model,
		res.Valid, res.Removed, res.Coverage, len(res.Evidence))
}

// dependencyTables is fully deterministic: no LLM needed.
func (r *repo) dependencyTables() string {
	var b strings.Builder
	b.WriteString("## Most depended-on internal files\n\n| File | Importing files |\n|---|---|\n")
	for _, t := range topN(r.inDegree, 25) {
		fmt.Fprintf(&b, "| `%s` | %d |\n", t.key, t.n)
	}
	b.WriteString("\n## External modules\n\n| Module | Importing files |\n|---|---|\n")
	for _, t := range topN(r.snap.External, 40) {
		fmt.Fprintf(&b, "| `%s` | %d |\n", t.key, t.n)
	}
	b.WriteString("\n## Component dependencies\n\n| Component | Depends on (importing files) |\n|---|---|\n")
	for i, c := range r.comps {
		if i == 25 {
			break
		}
		var d []string
		for _, t := range topN(c.DependsOn, 6) {
			d = append(d, fmt.Sprintf("%s (%d)", t.key, t.n))
		}
		if len(d) == 0 {
			d = []string{"—"}
		}
		fmt.Fprintf(&b, "| `%s` | %s |\n", c.Name, strings.Join(d, ", "))
	}
	return b.String()
}

// ReadManifest loads the manifest from a docs directory.
func ReadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	var m Manifest
	return &m, json.Unmarshal(b, &m)
}

// Freshness of one document.
type Freshness struct {
	Doc     DocResult
	Status  string   // CURRENT, STALE
	Changed []string // evidence files changed or deleted since generation
}

// Status compares each document's evidence with the files on disk now.
func Status(dir, root string) ([]Freshness, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("no generated docs in %s (run `kuli docs` first)", dir)
	}
	var out []Freshness
	for _, d := range m.Docs {
		fr := Freshness{Doc: d, Status: "CURRENT"}
		for _, p := range sortedKeys(d.Evidence) {
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
			if err != nil {
				fr.Changed = append(fr.Changed, p+" (deleted)")
				continue
			}
			sum := sha256.Sum256(b)
			if hex.EncodeToString(sum[:]) != d.Evidence[p] {
				fr.Changed = append(fr.Changed, p)
			}
		}
		if len(fr.Changed) > 0 {
			fr.Status = "STALE"
		}
		out = append(out, fr)
	}
	return out, nil
}
