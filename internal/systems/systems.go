// Package systems finds the projects inside an opened folder (several repositories, or
// the apps of a monorepo) and how they relate: code imports, declared dependencies and
// deploy/config files that refer to each other. Every link carries file:line evidence.
package systems

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kuli-dev/kulidev/internal/store"
)

// Project is one repository or app inside the folder. Path "" is the folder itself
// (files that belong to no project).
type Project struct {
	Path     string      `json:"path"`
	Name     string      `json:"name"`
	Git      bool        `json:"git"`
	Markers  []string    `json:"markers"`  // go.mod, package.json, … at its top
	Packages []string    `json:"packages"` // names other code uses to import it
	Files    int         `json:"files"`
	Lines    int         `json:"lines"`
	Langs    []LangShare `json:"langs"`
}

type LangShare struct {
	Lang  string `json:"lang"`
	Lines int    `json:"lines"`
}

// Evidence is one place in the code that shows a link.
type Evidence struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
	// Target is where the other side is, when known (the route a call reaches).
	Target string `json:"target,omitempty"`
}

// Link kinds.
const (
	Imports   = "imports"    // code in From imports code of To (CallsAPI: calls its HTTP routes)
	DependsOn = "depends on" // From's manifest declares To's package
	Config    = "configures" // From's deploy/config files refer to To
)

// Link is a relation between two projects.
type Link struct {
	From     string     `json:"from"`
	To       string     `json:"to"`
	Kind     string     `json:"kind"`
	Count    int        `json:"count"`
	Evidence []Evidence `json:"evidence"`
}

// Map is the folder's projects and their links.
type Map struct {
	Projects []Project `json:"projects"`
	Links    []Link    `json:"links"`
}

const maxEvidence = 12

var markerFiles = []string{"go.mod", "package.json", "pyproject.toml", "setup.py", "composer.json",
	"pom.xml", "build.gradle", "build.gradle.kts", "Cargo.toml", "Gemfile"}

// Folders that hold samples and fixtures, not projects of their own.
var notProject = map[string]bool{"test": true, "tests": true, "testdata": true, "fixtures": true,
	"examples": true, "example": true, "__tests__": true, "e2e": true, "docs": true}

// Names too common to prove a link when they appear as a folder name or a word.
var generic = map[string]bool{"app": true, "src": true, "lib": true, "core": true, "utils": true,
	"common": true, "test": true, "tests": true, "scripts": true, "config": true, "api": true,
	"web": true, "server": true, "client": true, "main": true, "docs": true, "data": true,
	"shared": true, "tools": true, "build": true, "deploy": true, "infra": true, "service": true}

// Build maps the projects under root from a scan snapshot.
func Build(root string, snap *store.Snapshot) Map {
	files := map[string]bool{}
	for _, f := range snap.Files {
		files[f.Path] = true
	}
	projects := detect(root, snap, files)
	if len(projects) < 2 {
		return Map{Projects: projects, Links: []Link{}}
	}
	b := &builder{root: root, projects: projects, byDir: map[string]int{}, links: map[[3]string]*Link{}}
	for i, p := range projects {
		b.byDir[p.Path] = i
	}
	b.describe(snap, files)
	b.imports(snap)
	b.dependencies(snap)
	b.config(snap)
	b.http(snap)

	out := Map{Projects: b.projects, Links: []Link{}}
	for _, l := range b.links {
		out.Links = append(out.Links, *l)
	}
	sort.Slice(out.Links, func(i, j int) bool {
		a, c := out.Links[i], out.Links[j]
		if a.Count != c.Count {
			return a.Count > c.Count
		}
		return a.From+a.To+a.Kind < c.From+c.To+c.Kind
	})
	return out
}

// maxDepth bounds how deep projects are looked for.
const maxDepth = 6

// detect finds projects: the outermost folders (up to maxDepth deep) that are a git
// repository or have a project file such as go.mod or package.json at their top.
func detect(root string, snap *store.Snapshot, files map[string]bool) []Project {
	dirs := map[string]bool{}
	for _, f := range snap.Files {
		parts := strings.Split(f.Path, "/")
		for d := 1; d < len(parts) && d <= maxDepth; d++ {
			dirs[strings.Join(parts[:d], "/")] = true
		}
	}
	cands := make([]string, 0, len(dirs))
	for d := range dirs {
		cands = append(cands, d)
	}
	sort.Slice(cands, func(i, j int) bool {
		di, dj := strings.Count(cands[i], "/"), strings.Count(cands[j], "/")
		if di != dj {
			return di < dj
		}
		return cands[i] < cands[j]
	})
	var found []Project
	taken := map[string]bool{}
	for _, d := range cands {
		if under(d, taken) || skippedDir(d) {
			continue
		}
		p := Project{Path: d, Name: path.Base(d), Markers: []string{}, Packages: []string{}}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(d), ".git")); err == nil && info != nil {
			p.Git = true
		}
		for _, m := range markerFiles {
			if files[d+"/"+m] {
				p.Markers = append(p.Markers, m)
			}
		}
		if p.Git || len(p.Markers) > 0 {
			taken[d] = true
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		return []Project{}
	}
	// Files outside every project (a top-level compose file, the main app of a repo
	// whose sub-folders are apps) form the folder's own entry.
	rest := false
	for _, f := range snap.Files {
		if !under(f.Path, taken) {
			rest = true
			break
		}
	}
	if rest {
		self := Project{Path: "", Name: filepath.Base(root), Markers: []string{}, Packages: []string{}}
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			self.Git = true
		}
		for _, m := range markerFiles {
			if files[m] {
				self.Markers = append(self.Markers, m)
			}
		}
		found = append([]Project{self}, found...)
	}
	return found
}

func skippedDir(d string) bool {
	for _, part := range strings.Split(d, "/") {
		if notProject[strings.ToLower(part)] {
			return true
		}
	}
	return false
}

// under reports whether p is inside (or is) one of the dirs.
func under(p string, dirs map[string]bool) bool {
	for d := p; d != "." && d != "" && d != "/"; d = path.Dir(d) {
		if dirs[d] {
			return true
		}
	}
	return false
}

type builder struct {
	root     string
	projects []Project
	byDir    map[string]int
	links    map[[3]string]*Link
	lines    map[string][]string // file cache for evidence text
}

// of returns the index of the project a path belongs to (-1: none).
func (b *builder) of(p string) int {
	for d := p; ; d = path.Dir(d) {
		if i, ok := b.byDir[d]; ok && d != "" {
			return i
		}
		if d == "." || d == "/" || d == "" {
			break
		}
	}
	if i, ok := b.byDir[""]; ok {
		return i
	}
	return -1
}

func (b *builder) add(from, to int, kind string, ev Evidence) {
	if from < 0 || to < 0 || from == to {
		return
	}
	k := [3]string{b.projects[from].Path, b.projects[to].Path, kind}
	l := b.links[k]
	if l == nil {
		l = &Link{From: k[0], To: k[1], Kind: kind, Evidence: []Evidence{}}
		b.links[k] = l
	}
	l.Count++
	if len(l.Evidence) < maxEvidence {
		if ev.Text == "" {
			ev.Text = b.line(ev.Path, ev.Line)
		}
		l.Evidence = append(l.Evidence, ev)
	}
}

func (b *builder) line(p string, n int) string {
	if b.lines == nil {
		b.lines = map[string][]string{}
	}
	ls, ok := b.lines[p]
	if !ok {
		if data, err := os.ReadFile(filepath.Join(b.root, filepath.FromSlash(p))); err == nil && len(data) < 2<<20 {
			ls = strings.Split(string(data), "\n")
		}
		b.lines[p] = ls
	}
	if n < 1 || n > len(ls) {
		return ""
	}
	t := strings.TrimSpace(ls[n-1])
	if len(t) > 160 {
		t = t[:160] + "…"
	}
	return t
}

func (b *builder) read(p string) []byte {
	data, err := os.ReadFile(filepath.Join(b.root, filepath.FromSlash(p)))
	if err != nil || len(data) > 1<<20 {
		return nil
	}
	return data
}

// describe fills sizes, languages and the package names each project is imported by.
func (b *builder) describe(snap *store.Snapshot, files map[string]bool) {
	langs := make([]map[string]int, len(b.projects))
	for i := range langs {
		langs[i] = map[string]int{}
	}
	for _, f := range snap.Files {
		i := b.of(f.Path)
		if i < 0 {
			continue
		}
		b.projects[i].Files++
		b.projects[i].Lines += f.Lines
		if f.Lang != "" && f.Parsed {
			langs[i][f.Lang] += f.Lines
		}
	}
	for i := range b.projects {
		p := &b.projects[i]
		for l, n := range langs[i] {
			p.Langs = append(p.Langs, LangShare{l, n})
		}
		sort.Slice(p.Langs, func(x, y int) bool { return p.Langs[x].Lines > p.Langs[y].Lines })
		if len(p.Langs) > 3 {
			p.Langs = p.Langs[:3]
		}
		if p.Langs == nil {
			p.Langs = []LangShare{}
		}
		join := func(name string) string {
			if p.Path == "" {
				return name
			}
			return p.Path + "/" + name
		}
		pkgs := map[string]bool{}
		if data := b.read(join("go.mod")); data != nil {
			if m := goModuleRe.FindSubmatch(data); m != nil {
				pkgs["go:"+string(m[1])] = true
			}
		}
		if data := b.read(join("package.json")); data != nil {
			var pj struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &pj) == nil && pj.Name != "" {
				pkgs["npm:"+pj.Name] = true
			}
		}
		for _, f := range []string{"pyproject.toml", "setup.py"} {
			if data := b.read(join(f)); data != nil {
				if m := pyNameRe.FindSubmatch(data); m != nil {
					pkgs["pydist:"+normPy(string(m[1]))] = true
				}
			}
		}
		if data := b.read(join("pom.xml")); data != nil {
			if m := groupIDRe.FindSubmatch(data); m != nil {
				if parts := strings.Split(string(m[1]), "."); len(parts) >= 2 {
					pkgs["java:"+parts[0]+"."+parts[1]] = true
				}
			}
		}
		if data := b.read(join("composer.json")); data != nil {
			var cj struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &cj) == nil && cj.Name != "" {
				pkgs["composer:"+cj.Name] = true
			}
		}
		for _, p2 := range []string{p.Path, join("src")} {
			for f := range files {
				dir := path.Dir(f)
				if path.Base(f) != "__init__.py" || path.Dir(dir) != p2 && !(p2 == "" && path.Dir(dir) == ".") {
					continue
				}
				if name := path.Base(dir); !generic[name] && !strings.HasPrefix(name, "test") && b.of(f) == i {
					pkgs["py:"+name] = true
				}
			}
		}
		for k := range pkgs {
			p.Packages = append(p.Packages, k)
		}
		sort.Strings(p.Packages)
	}
}

var (
	goModuleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	pyNameRe   = regexp.MustCompile(`(?m)(?:^|[\s,(])name\s*=\s*["']([A-Za-z0-9_.\-]+)["']`)
	groupIDRe  = regexp.MustCompile(`<groupId>\s*([^<\s]+)\s*</groupId>`)
)

func isComment(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "<!--")
}

func normPy(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "_", ".", "_").Replace(s))
}

// imports links projects whose code imports each other: internal imports across a
// project boundary, and "external" imports of a package another project provides.
func (b *builder) imports(snap *store.Snapshot) {
	for _, im := range snap.Imports {
		b.add(b.of(im.From), b.of(im.To), Imports, Evidence{Path: im.From, Line: im.Line})
	}
	provides := map[string]int{}
	var goMods []string
	for i, p := range b.projects {
		for _, k := range p.Packages {
			if strings.HasPrefix(k, "pydist:") {
				k = "py:" + strings.TrimPrefix(k, "pydist:")
			}
			provides[k] = i
			if strings.HasPrefix(k, "go:") {
				goMods = append(goMods, k)
			}
		}
	}
	for _, im := range snap.ExternalImports {
		to, ok := provides[im.To]
		if !ok && strings.HasPrefix(im.To, "go:") {
			for _, m := range goMods {
				if strings.HasPrefix(im.To, m+"/") {
					to, ok = provides[m], true
					break
				}
			}
		}
		if !ok && strings.HasPrefix(im.To, "npm:") {
			to, ok = provides[im.To]
		}
		if ok {
			b.add(b.of(im.From), to, Imports, Evidence{Path: im.From, Line: im.Line})
		}
	}
}

var depManifest = map[string]bool{"go.mod": true, "package.json": true, "pyproject.toml": true, "setup.py": true,
	"Pipfile": true, "composer.json": true, "pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"Cargo.toml": true, "Gemfile": true}

func isDepFile(p string) bool {
	base := path.Base(p)
	return depManifest[base] || strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt")
}

var tokenRe = regexp.MustCompile(`[@A-Za-z0-9_.\-/]+`)

// dependencies links a project to another whose package its manifests declare.
func (b *builder) dependencies(snap *store.Snapshot) {
	names := map[string]int{} // comparable dependency name → project
	for i, p := range b.projects {
		for _, k := range p.Packages {
			eco, name, _ := strings.Cut(k, ":")
			if generic[strings.ToLower(path.Base(name))] {
				continue
			}
			switch eco {
			case "npm", "go", "composer":
				names[strings.ToLower(name)] = i
			case "pydist":
				names[name] = i // already normalised
			}
		}
	}
	if len(names) == 0 {
		return
	}
	for _, f := range snap.Files {
		if !isDepFile(f.Path) {
			continue
		}
		from := b.of(f.Path)
		data := b.read(f.Path)
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for n := 1; sc.Scan(); n++ {
			text := sc.Text()
			if isComment(text) {
				continue
			}
			for _, tok := range tokenRe.FindAllString(text, -1) {
				tok = strings.Trim(tok, ".-/")
				to, ok := names[strings.ToLower(tok)]
				if !ok {
					to, ok = names[normPy(tok)]
				}
				if ok && to != from && !strings.Contains(strings.ToLower(text), `"name"`) {
					b.add(from, to, DependsOn, Evidence{Path: f.Path, Line: n})
					break
				}
			}
		}
	}
}

var configKinds = map[string]bool{"Dockerfile": true, "Docker Compose": true, "Kubernetes manifest": true,
	"Jenkinsfile": true, "GitHub Actions": true, "Chart.yaml": true, "kustomization.yaml": true,
	"Terraform": true, "Makefile": true, ".gitlab-ci.yml": true}

func isEnvFile(p string) bool {
	base := strings.ToLower(path.Base(p))
	return strings.HasPrefix(base, ".env") || strings.HasSuffix(base, ".env") || strings.Contains(base, "env.example")
}

// config links a project to another that its deploy and configuration files name:
// a compose service built from ../api, an env var pointing at http://billing-api:8080.
func (b *builder) config(snap *store.Snapshot) {
	type target struct {
		re     *regexp.Regexp // the name as a word: sales-manager-api, ./shop-web
		envKey *regexp.Regexp // a setting that locates it: SALES_MANAGER_API_URL=
		to     int
	}
	var targets []target
	for i, p := range b.projects {
		if p.Path == "" {
			continue
		}
		words := map[string]bool{}
		addWord := func(w string) {
			w = strings.ToLower(w)
			if len(w) >= 4 && !generic[w] {
				words[w] = true
				words[strings.ReplaceAll(w, "_", "-")] = true
				words[strings.ReplaceAll(w, "-", "_")] = true
			}
		}
		addWord(p.Name)
		for _, k := range p.Packages {
			if eco, name, _ := strings.Cut(k, ":"); eco == "npm" || eco == "pydist" {
				addWord(path.Base(name))
			}
		}
		var alts []string
		for w := range words {
			alts = append(alts, regexp.QuoteMeta(w))
		}
		if len(alts) == 0 {
			continue
		}
		sort.Strings(alts)
		// "_" joins identifiers: DISABLE_NEXUS_CORE is a flag, not a reference.
		re := regexp.MustCompile(`(?i)(^|[^a-z0-9_])(` + strings.Join(alts, "|") + `)([^a-z0-9_]|$)`)
		envKey := regexp.MustCompile(`(?i)(^|[^a-z0-9_])(` + strings.Join(alts, "|") + `)_[a-z0-9_]*(url|uri|host|endpoint|addr|address)([^a-z0-9_]|$)`)
		targets = append(targets, target{re, envKey, i})
	}
	configFiles := map[string]bool{}
	for kind, paths := range snap.Manifests {
		if configKinds[kind] {
			for _, p := range paths {
				configFiles[p] = true
			}
		}
	}
	for _, f := range snap.Files {
		if isEnvFile(f.Path) {
			configFiles[f.Path] = true
		}
	}
	for p := range configFiles {
		from := b.of(p)
		data := b.read(p)
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for n := 1; sc.Scan(); n++ {
			text := sc.Text()
			if isComment(text) {
				continue
			}
			for _, t := range targets {
				if t.to != from && (t.re.MatchString(text) || t.envKey.MatchString(text)) {
					b.add(from, t.to, Config, Evidence{Path: p, Line: n})
				}
			}
		}
	}
}
