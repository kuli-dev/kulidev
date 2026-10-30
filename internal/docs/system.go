package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kuli-dev/kulidev/internal/systems"
)

// SystemID is the document about how the projects in a folder fit together.
const SystemID = "system-overview"

var kindText = map[string]string{
	systems.Imports:   "imports code from",
	systems.CallsAPI:  "calls HTTP routes of",
	systems.DependsOn: "declares a dependency on",
	systems.Config:    "has deploy or config files that refer to",
}

// SystemOverview writes system-overview.md for a folder of several projects, from
// the Systems map: what each project is, how they connect (every link cites the code
// that shows it) and where to start. It needs no model. The document is added to the
// manifest in outDir, so it shows in the Docs list and goes stale with its evidence.
func SystemOverview(m *systems.Map, root, outDir, version string) (*DocResult, error) {
	if m == nil || len(m.Projects) < 2 {
		return nil, nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	base, err := filepath.Rel(outDir, root)
	if err != nil {
		base = root
	}
	ref := func(e systems.Evidence) string {
		target := filepath.ToSlash(filepath.Join(base, e.Path))
		label := e.Path
		if e.Line > 0 {
			target += fmt.Sprintf("#L%d", e.Line)
			label += fmt.Sprintf(":%d", e.Line)
		}
		return fmt.Sprintf("[%s](%s)", label, strings.ReplaceAll(target, " ", "%20"))
	}
	name := map[string]string{}
	for _, p := range m.Projects {
		name[p.Path] = p.Name
		if p.Path == "" {
			name[p.Path] = p.Name + " (folder files)"
		}
	}
	evidence := map[string]string{}
	cite := func(e systems.Evidence) string {
		if _, ok := evidence[e.Path]; !ok {
			if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Path))); err == nil {
				sum := sha256.Sum256(b)
				evidence[e.Path] = hex.EncodeToString(sum[:])
			}
		}
		return ref(e)
	}

	var b strings.Builder
	folder := filepath.Base(root)
	pairs := map[[2]string][]systems.Link{}
	var order [][2]string
	for _, l := range m.Links {
		k := [2]string{l.From, l.To}
		if pairs[k] == nil {
			order = append(order, k)
		}
		pairs[k] = append(pairs[k], l)
	}
	total := func(k [2]string) (n int) {
		for _, l := range pairs[k] {
			n += l.Count
		}
		return n
	}
	sort.SliceStable(order, func(i, j int) bool { return total(order[i]) > total(order[j]) })

	fmt.Fprintf(&b, "%s holds %d projects. KuliDev found %d connections between them; each one below links to the code that shows it.\n\n",
		folder, len(m.Projects), len(order))

	b.WriteString("## Projects\n\n| Project | What it is | Size | Mostly |\n|---|---|---|---|\n")
	for _, p := range m.Projects {
		what := []string{}
		if p.Git {
			what = append(what, "git repository")
		}
		if len(p.Markers) > 0 {
			what = append(what, strings.Join(p.Markers, ", "))
		}
		// What others can import it as; a Python module and package of the same name once.
		ecoName := map[string]string{"py": "Python package", "pydist": "Python package", "npm": "npm package",
			"go": "Go module", "java": "Java package", "composer": "Composer package"}
		said := map[string]bool{}
		for _, pk := range p.Packages {
			eco, n, _ := strings.Cut(pk, ":")
			label := fmt.Sprintf("provides %s `%s`", ecoName[eco], n)
			if ecoName[eco] == "" || said[label] {
				continue
			}
			said[label] = true
			what = append(what, label)
		}
		var langs []string
		total := 0
		for _, l := range p.Langs {
			total += l.Lines
		}
		for _, l := range p.Langs {
			if pct := l.Lines * 100 / max(total, 1); pct >= 1 {
				langs = append(langs, fmt.Sprintf("%s %d%%", l.Lang, pct))
			}
		}
		where := "`" + p.Path + "/`"
		if p.Path == "" {
			where = "files directly in the folder"
		}
		fmt.Fprintf(&b, "| **%s** (%s) | %s | %d files | %s |\n", name[p.Path], where, strings.Join(what, "; "), p.Files, strings.Join(langs, ", "))
	}

	b.WriteString("\n## How they connect\n\n")
	if len(order) == 0 {
		b.WriteString("No connections found: the projects share no imports, dependencies, HTTP calls or config references.\n")
	}
	citations := 0
	for _, k := range order {
		fmt.Fprintf(&b, "### %s → %s\n\n", name[k[0]], name[k[1]])
		links := pairs[k]
		sort.SliceStable(links, func(i, j int) bool { return links[i].Count > links[j].Count })
		for _, l := range links {
			places := "place"
			if l.Count != 1 {
				places = "places"
			}
			fmt.Fprintf(&b, "- %s %s %s (%d %s), for example:\n", name[k[0]], kindText[l.Kind], name[k[1]], l.Count, places)
			for i, e := range l.Evidence {
				if i == 3 {
					break
				}
				line := "  - " + cite(e)
				if e.Text != "" {
					line += " `" + strings.ReplaceAll(e.Text, "`", "'") + "`"
				}
				if e.Target != "" {
					route, rest, _ := strings.Cut(e.Target, "  ")
					p, ln, _ := strings.Cut(route, ":")
					var n int
					fmt.Sscanf(ln, "%d", &n)
					line += " reaches " + cite(systems.Evidence{Path: p, Line: n})
					if rest != "" {
						line += " `" + strings.ReplaceAll(rest, "`", "'") + "`"
					}
				}
				b.WriteString(line + "\n")
				citations++
			}
		}
		b.WriteString("\n")
	}

	// Where to start: what the others build on, and what pulls the others together.
	usedBy := map[string]map[string]bool{}
	uses := map[string]map[string]bool{}
	for _, k := range order {
		if usedBy[k[1]] == nil {
			usedBy[k[1]] = map[string]bool{}
		}
		if uses[k[0]] == nil {
			uses[k[0]] = map[string]bool{}
		}
		usedBy[k[1]][k[0]] = true
		uses[k[0]][k[1]] = true
	}
	b.WriteString("## Where to start\n\n")
	type count struct {
		path string
		n    int
	}
	rank := func(set map[string]map[string]bool) []count {
		var out []count
		for p, s := range set {
			out = append(out, count{p, len(s)})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].n > out[j].n || out[i].n == out[j].n && out[i].path < out[j].path })
		return out
	}
	if r := rank(usedBy); len(r) > 0 {
		fmt.Fprintf(&b, "- **%s** is used by %d of the other projects: read it first, the others build on it.\n", name[r[0].path], r[0].n)
	}
	if r := rank(uses); len(r) > 0 {
		fmt.Fprintf(&b, "- **%s** uses %d of the others: it is where they come together (an app, a service or deployment).\n", name[r[0].path], r[0].n)
	}
	var alone []string
	for _, p := range m.Projects {
		if usedBy[p.Path] == nil && uses[p.Path] == nil {
			alone = append(alone, name[p.Path])
		}
	}
	if len(alone) > 0 {
		fmt.Fprintf(&b, "- Not connected to the others: %s.\n", strings.Join(alone, ", "))
	}
	b.WriteString("\nEach project has its own documents: pick it in the Docs panel.\n")

	res := &DocResult{ID: SystemID, File: SystemID + ".md", Title: "System overview", Mode: "deterministic",
		Valid: citations, Coverage: 1, Evidence: evidence}
	now := time.Now().UTC().Format(time.RFC3339)
	doc := fmt.Sprintf("---\ntitle: %q\ngenerated_by: \"kulidev %s\"\ngenerated_at: %q\nmode: deterministic\ncitations: %d\nevidence_files: %d\nstatus: generated\n---\n",
		res.Title, version, now, citations, len(evidence)) +
		"# System overview\n\n" + b.String() +
		"\n---\n_Generated by [KuliDev](https://github.com/kuli-dev/kulidev) — docs-nya mana? Ini._\n"
	if err := os.WriteFile(filepath.Join(outDir, res.File), []byte(doc), 0o644); err != nil {
		return nil, err
	}

	// Record it in the folder's manifest next to the other documents.
	man, err := ReadManifest(outDir)
	if err != nil {
		man = &Manifest{Generator: "kulidev " + version, GeneratedAt: now}
	}
	kept := man.Docs[:0]
	for _, d := range man.Docs {
		if d.ID != SystemID {
			kept = append(kept, d)
		}
	}
	man.Docs = append([]DocResult{*res}, kept...)
	mb, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, ManifestFile), mb, 0o644); err != nil {
		return nil, err
	}
	return res, nil
}
