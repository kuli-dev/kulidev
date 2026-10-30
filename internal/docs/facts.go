package docs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kuli-dev/kulidev/internal/store"
)

// repo bundles the snapshot with lookups the fact builders share.
type repo struct {
	snap     *store.Snapshot
	comps    []*Component
	lines    map[string]int
	sha      map[string]string
	inDegree map[string]int    // file or Go package dir → distinct importing files
	dirFile  map[string]string // Go package dir → a representative file in it
}

func newRepo(snap *store.Snapshot) *repo {
	r := &repo{snap: snap, lines: map[string]int{}, sha: map[string]string{}, inDegree: map[string]int{}, dirFile: map[string]string{}}
	for _, f := range snap.Files {
		r.lines[f.Path] = f.Lines
		r.sha[f.Path] = f.SHA256
		if d := path.Dir(f.Path); f.Parsed && strings.HasSuffix(f.Path, ".go") && !strings.HasSuffix(f.Path, "_test.go") {
			if cur, ok := r.dirFile[d]; !ok || f.Lines > r.lines[cur] {
				r.dirFile[d] = f.Path
			}
		}
	}
	importers := map[string]map[string]bool{}
	for _, im := range snap.Imports {
		if importers[im.To] == nil {
			importers[im.To] = map[string]bool{}
		}
		importers[im.To][im.From] = true
	}
	for t, s := range importers {
		r.inDegree[t] = len(s)
	}
	r.comps, _ = BuildComponents(snap)
	return r
}

func (r *repo) name() string { return filepath.Base(r.snap.Run.Root) }

// whole cites an entire file. A Go package directory is cited through its largest file,
// because evidence must always be a file that can be opened and hashed.
func (r *repo) whole(f *Facts, p string) string {
	if _, ok := r.lines[p]; !ok {
		if file, ok := r.dirFile[p]; ok {
			p = file
		}
	}
	n := r.lines[p]
	if n <= 0 {
		return f.Cite(p, 0, 0)
	}
	return f.Cite(p, 1, n)
}

// read returns a file's lines (nil when unreadable).
func (r *repo) read(p string) []string {
	b, err := os.ReadFile(filepath.Join(r.snap.Run.Root, filepath.FromSlash(p)))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

type ranked struct {
	key string
	n   int
}

func topN(m map[string]int, n int) []ranked {
	out := make([]ranked, 0, len(m))
	for k, v := range m {
		out = append(out, ranked{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].key < out[j].key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// ---- overview -------------------------------------------------------------

func (r *repo) overviewFacts() *Facts {
	f := newFacts()
	parsed, langLines := 0, map[string]int{}
	for _, fi := range r.snap.Files {
		if fi.Parsed {
			parsed++
		}
		if fi.Lang != "" {
			langLines[fi.Lang] += fi.Lines
		}
	}
	f.Line("Repository: %s (commit %.10s)", r.name(), r.snap.Run.Commit)
	f.Line("Size: %d files, %d parsed source files.", len(r.snap.Files), parsed)
	var ls []string
	for _, l := range topN(langLines, 8) {
		ls = append(ls, fmt.Sprintf("%s %d lines", l.key, l.n))
	}
	f.Line("Languages by lines: %s.", strings.Join(ls, ", "))

	f.Line("\nComponents (directories, largest first):")
	for i, c := range r.comps {
		if i == 15 {
			f.Line("- … %d smaller components", len(r.comps)-15)
			break
		}
		hub := c.Hub()
		f.Line("- %s: %d source files, %d lines, mainly %s; most imported file %s (%d importers) %s",
			c.Name, len(c.Files), c.Lines, c.MainLang(), hub.Path, r.inDegree[hub.Path], r.whole(f, hub.Path))
	}

	f.Line("\nMost depended-on internal files:")
	for _, t := range topN(r.inDegree, 8) {
		what := t.key
		if _, isFile := r.lines[t.key]; !isFile {
			what = "Go package " + t.key
		}
		f.Line("- %s imported by %d files %s", what, t.n, r.whole(f, t.key))
	}

	f.Line("\nMost used external modules (importing files): %s", externalList(r.snap.External, 20))
	for _, k := range []string{"package.json", "go.mod", "requirements.txt", "pyproject.toml", "Pipfile", "pom.xml", "Cargo.toml", "composer.json", "Gemfile"} {
		for i, p := range r.snap.Manifests[k] {
			if i == 3 {
				break
			}
			f.Line("- dependency manifest %s %s", p, r.whole(f, p))
		}
	}

	f.Line("\nInfrastructure and build files:")
	for _, k := range sortedKeys(r.snap.Manifests) {
		if isDependencyManifest(k) {
			continue
		}
		ps := r.snap.Manifests[k]
		var cites []string
		for i, p := range ps {
			if i == 4 {
				break
			}
			cites = append(cites, p+" "+r.whole(f, p))
		}
		f.Line("- %s ×%d: %s", k, len(ps), strings.Join(cites, "; "))
	}

	if readme := r.findReadme(); readme != "" {
		lines := r.read(readme)
		n := min(len(lines), 80)
		f.Line("\nREADME excerpt %s:", f.Cite(readme, 1, n))
		for i := 0; i < n; i++ {
			f.Line("%4d| %s", i+1, redact(lines[i]))
		}
	}
	return f
}

func (r *repo) findReadme() string {
	for _, c := range []string{"README.md", "README.rst", "README.txt", "README", "readme.md"} {
		if _, ok := r.lines[c]; ok {
			return c
		}
	}
	return ""
}

func externalList(ext map[string]int, n int) string {
	var parts []string
	for _, e := range topN(ext, n) {
		parts = append(parts, fmt.Sprintf("%s (%d)", e.key, e.n))
	}
	return strings.Join(parts, ", ")
}

func isDependencyManifest(kind string) bool {
	switch kind {
	case "package.json", "go.mod", "requirements.txt", "pyproject.toml", "Pipfile", "setup.py",
		"pom.xml", "build.gradle", "build.gradle.kts", "Cargo.toml", "composer.json", "Gemfile":
		return true
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- architecture ---------------------------------------------------------

const maxDetailedComponents = 12

// componentFacts describes one component for the architecture narrative.
func (r *repo) componentFacts(f *Facts, c *Component) {
	f.Line("COMPONENT %s — %d source files, %d lines, mainly %s", c.Name, len(c.Files), c.Lines, c.MainLang())
	files := make([]string, 0, len(c.Files))
	for _, fi := range c.Files {
		files = append(files, strings.TrimPrefix(fi.Path, c.Name+"/"))
	}
	if len(files) > 20 {
		files = append(files[:20], fmt.Sprintf("… +%d more", len(c.Files)-20))
	}
	f.Line("  files: %s", strings.Join(files, ", "))

	syms := append([]store.SymbolInfo(nil), c.Symbols...)
	weight := map[string]int{"class": 0, "struct": 0, "interface": 1, "enum": 2, "type": 3, "function": 4}
	sort.SliceStable(syms, func(i, j int) bool {
		if weight[syms[i].Kind] != weight[syms[j].Kind] {
			return weight[syms[i].Kind] < weight[syms[j].Kind]
		}
		return r.inDegree[syms[i].Path] > r.inDegree[syms[j].Path]
	})
	if len(syms) > 20 {
		syms = syms[:20]
	}
	for _, y := range syms {
		if strings.HasPrefix(y.Name, "_") {
			continue
		}
		f.Line("  defines %s %s in %s %s", y.Kind, y.Name, y.Path, f.Cite(y.Path, y.Line, y.EndLine))
	}
	if hub := c.Hub(); r.inDegree[hub.Path] > 0 {
		f.Line("  most imported file: %s (%d importers) %s", hub.Path, r.inDegree[hub.Path], r.whole(f, hub.Path))
	}
	if len(c.DependsOn) > 0 {
		var d []string
		for _, t := range topN(c.DependsOn, 6) {
			d = append(d, fmt.Sprintf("%s (%d files)", t.key, t.n))
		}
		f.Line("  depends on: %s", strings.Join(d, ", "))
	}
	if len(c.Importers) > 0 {
		var d []string
		for _, t := range topN(c.Importers, 6) {
			d = append(d, fmt.Sprintf("%s (%d files)", t.key, t.n))
		}
		f.Line("  used by: %s", strings.Join(d, ", "))
	}
	f.Line("")
}

// mermaid renders the component dependency graph deterministically.
func (r *repo) mermaid(maxNodes, maxEdges int) string {
	comps := r.comps
	if len(comps) > maxNodes {
		comps = comps[:maxNodes]
	}
	id := map[string]string{}
	var b strings.Builder
	b.WriteString("```mermaid\nflowchart LR\n")
	for i, c := range comps {
		id[c.Name] = fmt.Sprintf("c%d", i)
		label := strings.NewReplacer(`"`, "'", "[", "(", "]", ")").Replace(c.Name)
		fmt.Fprintf(&b, "  c%d[\"%s<br/>%d files · %s\"]\n", i, label, len(c.Files), c.MainLang())
	}
	type edge struct {
		from, to string
		n        int
	}
	var edges []edge
	for _, c := range comps {
		for to, n := range c.DependsOn {
			if _, ok := id[to]; ok {
				edges = append(edges, edge{c.Name, to, n})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].n != edges[j].n {
			return edges[i].n > edges[j].n
		}
		return edges[i].from+edges[i].to < edges[j].from+edges[j].to
	})
	if len(edges) > maxEdges {
		edges = edges[:maxEdges]
	}
	for _, e := range edges {
		fmt.Fprintf(&b, "  %s -->|%d| %s\n", id[e.from], e.n, id[e.to])
	}
	b.WriteString("```\n")
	return b.String()
}

// ---- infrastructure -------------------------------------------------------

var infraKinds = []struct {
	kind string
	max  int
}{
	{"Dockerfile", 4}, {"Docker Compose", 2}, {"GitHub Actions", 3}, {"Jenkinsfile", 1},
	{".gitlab-ci.yml", 1}, {"Kubernetes manifest", 4}, {"kustomization.yaml", 2},
	{"Chart.yaml", 2}, {"Terraform", 2}, {"Makefile", 1},
}

const infraLines = 80

func (r *repo) infraFacts() *Facts {
	f := newFacts()
	found := false
	for _, k := range infraKinds {
		ps := r.snap.Manifests[k.kind]
		if len(ps) == 0 {
			continue
		}
		found = true
		f.Line("%s files (%d total): %s", k.kind, len(ps), strings.Join(firstN(ps, 12), ", "))
		for _, p := range pickInfra(ps, k.max) {
			lines := r.read(p)
			n := min(len(lines), infraLines)
			f.Line("\nFILE %s %s (first %d of %d lines; secrets redacted):", p, f.Cite(p, 1, n), n, len(lines))
			for i := 0; i < n; i++ {
				f.Line("%4d| %s", i+1, redact(lines[i]))
			}
		}
		f.Line("")
	}
	if !found {
		return nil
	}
	return f
}

// pickInfra prefers shallow paths and names that look like main deployments.
func pickInfra(ps []string, n int) []string {
	cp := append([]string(nil), ps...)
	score := func(p string) int {
		s := strings.Count(p, "/") * 10
		for _, w := range []string{"deploy", "prod", "main", "api", "backend", "app"} {
			if strings.Contains(strings.ToLower(p), w) {
				s -= 3
			}
		}
		return s
	}
	sort.SliceStable(cp, func(i, j int) bool { return score(cp[i]) < score(cp[j]) })
	return firstN(cp, n)
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// redact masks values that look like credentials before anything leaves the machine:
// key=value / key: value assignments for secret-ish keys, and passwords embedded in URLs.
// Variable references such as ${TOKEN} or $TOKEN are left alone; they hold no secret.
var (
	secretRe  = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credentials?|auth)[A-Za-z0-9_\-]*["']?\s*[:=]\s*)(["']?)([^\s"',}]+)`)
	urlCredRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://[^:/\s@]*:)([^@\s/]+)@`)
)

func redact(line string) string {
	line = urlCredRe.ReplaceAllStringFunc(line, func(m string) string {
		sub := urlCredRe.FindStringSubmatch(m)
		if strings.HasPrefix(sub[2], "$") {
			return m
		}
		return sub[1] + "<redacted>@"
	})
	return secretRe.ReplaceAllStringFunc(line, func(m string) string {
		sub := secretRe.FindStringSubmatch(m)
		if strings.HasPrefix(sub[3], "$") || strings.HasPrefix(sub[3], "<redacted>") {
			return m
		}
		return sub[1] + sub[2] + "<redacted>"
	})
}

// ---- getting started ------------------------------------------------------

var (
	makeTargetRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:([^=]|$)`)
	composeSvcRe = regexp.MustCompile(`^  ([A-Za-z0-9_.-]+):\s*$`)
	envNameRe    = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	setupHeadRe  = regexp.MustCompile(`(?i)^#+\s*.*(install|setup|set up|getting started|quick ?start|running|run |development|local|usage|build|deploy)`)
)

func (r *repo) gettingStartedFacts() *Facts {
	f := newFacts()
	f.Line("Repository: %s", r.name())

	for _, p := range firstN(r.snap.Manifests["package.json"], 4) {
		r.packageScripts(f, p)
	}
	for _, p := range firstN(r.snap.Manifests["Makefile"], 2) {
		lines := r.read(p)
		var targets []string
		for i, ln := range lines {
			if m := makeTargetRe.FindStringSubmatch(ln); m != nil && m[1] != ".PHONY" {
				targets = append(targets, m[1]+" "+f.Cite(p, i+1, i+1))
			}
		}
		if len(targets) > 0 {
			f.Line("make targets in %s: %s", p, strings.Join(firstN(targets, 25), ", "))
		}
	}
	for _, p := range firstN(r.snap.Manifests["Docker Compose"], 2) {
		lines := r.read(p)
		inServices := false
		var svcs []string
		for i, ln := range lines {
			if strings.HasPrefix(ln, "services:") {
				inServices = true
				continue
			}
			if inServices && len(ln) > 0 && ln[0] != ' ' && ln[0] != '#' {
				inServices = false
			}
			if m := composeSvcRe.FindStringSubmatch(ln); inServices && m != nil {
				svcs = append(svcs, m[1]+" "+f.Cite(p, i+1, i+1))
			}
		}
		if len(svcs) > 0 {
			f.Line("docker compose services in %s: %s", p, strings.Join(svcs, ", "))
		}
	}
	r.toolchainVersions(f)
	r.envExamples(f)

	if readme := r.findReadme(); readme != "" {
		lines := r.read(readme)
		for i := 0; i < len(lines); i++ {
			if !setupHeadRe.MatchString(lines[i]) {
				continue
			}
			end := i + 1
			for end < len(lines) && end-i < 50 && !strings.HasPrefix(strings.TrimSpace(lines[end]), "#") {
				end++
			}
			f.Line("\nREADME section %s:", f.Cite(readme, i+1, end))
			for j := i; j < end; j++ {
				f.Line("%4d| %s", j+1, redact(lines[j]))
			}
			i = end - 1
		}
	}
	return f
}

func (r *repo) packageScripts(f *Facts, p string) {
	lines := r.read(p)
	var pkg struct {
		Name    string            `json:"name"`
		Scripts map[string]string `json:"scripts"`
		Engines map[string]string `json:"engines"`
		PM      string            `json:"packageManager"`
	}
	if json.Unmarshal([]byte(strings.Join(lines, "\n")), &pkg) != nil {
		return
	}
	lineOf := func(key string) int {
		needle := `"` + key + `"`
		for i, ln := range lines {
			if strings.Contains(ln, needle) {
				return i + 1
			}
		}
		return 0
	}
	dir := path.Dir(p)
	f.Line("package.json %s (package %q, run from %s):", p, pkg.Name, dir)
	for _, name := range sortedKeys(pkg.Scripts) {
		f.Line("  script %q = %s %s", name, pkg.Scripts[name], f.Cite(p, lineOf(name), lineOf(name)))
	}
	for _, name := range sortedKeys(pkg.Engines) {
		f.Line("  engine %s %s %s", name, pkg.Engines[name], f.Cite(p, lineOf(name), lineOf(name)))
	}
	if pkg.PM != "" {
		f.Line("  packageManager %s %s", pkg.PM, f.Cite(p, lineOf("packageManager"), lineOf("packageManager")))
	}
}

func (r *repo) toolchainVersions(f *Facts) {
	for _, p := range r.snap.Manifests["go.mod"] {
		for i, ln := range r.read(p) {
			if strings.HasPrefix(ln, "go ") || strings.HasPrefix(ln, "toolchain ") {
				f.Line("Go version in %s: %s %s", p, strings.TrimSpace(ln), f.Cite(p, i+1, i+1))
			}
		}
	}
	for _, p := range r.snap.Manifests["pyproject.toml"] {
		for i, ln := range r.read(p) {
			if strings.Contains(ln, "requires-python") || strings.HasPrefix(strings.TrimSpace(ln), "python =") {
				f.Line("Python requirement in %s: %s %s", p, strings.TrimSpace(ln), f.Cite(p, i+1, i+1))
			}
		}
	}
	for _, name := range []string{".nvmrc", ".node-version", ".python-version", ".tool-versions"} {
		if _, ok := r.lines[name]; ok {
			if ls := r.read(name); len(ls) > 0 {
				f.Line("%s: %s %s", name, strings.Join(firstN(ls, 5), "; "), f.Cite(name, 1, min(len(ls), 5)))
			}
		}
	}
}

// envExamples lists variable NAMES from example env files; values are never read out.
func (r *repo) envExamples(f *Facts) {
	for _, fi := range r.snap.Files {
		base := path.Base(fi.Path)
		if !(strings.HasPrefix(base, ".env.") || strings.HasPrefix(base, "env.")) ||
			!(strings.Contains(base, "example") || strings.Contains(base, "sample") || strings.Contains(base, "template")) {
			continue
		}
		file, err := os.Open(filepath.Join(r.snap.Run.Root, filepath.FromSlash(fi.Path)))
		if err != nil {
			continue
		}
		var names []string
		sc := bufio.NewScanner(file)
		for sc.Scan() {
			if m := envNameRe.FindStringSubmatch(sc.Text()); m != nil {
				names = append(names, m[1])
			}
		}
		file.Close()
		if len(names) > 0 {
			f.Line("environment variables expected by %s (names only): %s %s", fi.Path, strings.Join(firstN(names, 40), ", "), r.whole(f, fi.Path))
		}
	}
}
