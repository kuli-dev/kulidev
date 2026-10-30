package scan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Target kinds an import can resolve to.
const (
	TargetFile     = "FILE"
	TargetPackage  = "PACKAGE"  // a Go package directory inside the repo
	TargetExternal = "EXTERNAL" // a third-party or stdlib module
)

// Target is where an import points.
type Target struct {
	Kind string
	Ref  string // file path, package dir, or "<ecosystem>:<module>"
}

type resolver struct {
	files     map[string]bool
	dirs      map[string]bool
	goPkgs    map[string]bool // dirs containing .go files
	goModules []goModule      // longest module path first
	readFile  func(string) []byte
	byBase    map[string][]string // lower-case base name → files, for suffix matching (Java, PHP)
	psr4      []psr4Root          // from composer.json autoload, longest prefix first
}

type psr4Root struct{ prefix, dir string }

var (
	psr4EntryRe = regexp.MustCompile(`'((?:[^'\\]|\\.)+)'\s*=>\s*array\(([^)]*)\)`)
	psr4DirRe   = regexp.MustCompile(`\$(vendorDir|baseDir)\s*\.\s*'([^']*)'`)
)

// generatedPSR4 reads Composer's generated vendor/composer/autoload_psr4.php, which is
// all many distributed packages (WordPress plugins) ship: no composer.json.
func generatedPSR4(projectDir string, data []byte) []psr4Root {
	var out []psr4Root
	for _, m := range psr4EntryRe.FindAllStringSubmatch(string(data), -1) {
		prefix := strings.ReplaceAll(m[1], `\\`, `\`) // PHP '\\' is one backslash
		for _, d := range psr4DirRe.FindAllStringSubmatch(m[2], -1) {
			base := projectDir
			if d[1] == "vendorDir" {
				base = path.Join(projectDir, "vendor")
			}
			out = append(out, psr4Root{prefix: prefix, dir: path.Clean(path.Join(base, strings.TrimPrefix(d[2], "/")))})
		}
	}
	return out
}

// composerPSR4 reads autoload / autoload-dev "psr-4" maps from a composer.json.
func composerPSR4(rel string, data []byte) []psr4Root {
	var c struct {
		Autoload struct {
			PSR4 map[string]json.RawMessage `json:"psr-4"`
		} `json:"autoload"`
		AutoloadDev struct {
			PSR4 map[string]json.RawMessage `json:"psr-4"`
		} `json:"autoload-dev"`
	}
	if json.Unmarshal(data, &c) != nil {
		return nil
	}
	var out []psr4Root
	for _, m := range []map[string]json.RawMessage{c.Autoload.PSR4, c.AutoloadDev.PSR4} {
		for prefix, raw := range m {
			var dirs []string
			var one string
			if json.Unmarshal(raw, &one) == nil {
				dirs = []string{one}
			} else {
				json.Unmarshal(raw, &dirs)
			}
			for _, d := range dirs {
				out = append(out, psr4Root{prefix: strings.TrimPrefix(prefix, "\\"), dir: path.Clean(path.Join(path.Dir(rel), d))})
			}
		}
	}
	return out
}

type goModule struct {
	path string // module path from go.mod
	dir  string // directory of go.mod relative to root ("." for root)
}

func newResolver(paths []string, readFile func(string) []byte) *resolver {
	r := &resolver{files: map[string]bool{}, dirs: map[string]bool{}, goPkgs: map[string]bool{}, readFile: readFile, byBase: map[string][]string{}}
	probed := map[string]bool{}
	for _, p := range paths {
		r.files[p] = true
		if path.Base(p) == "composer.json" && readFile != nil {
			r.psr4 = append(r.psr4, composerPSR4(p, readFile(p))...)
		}
		if path.Ext(p) == ".php" && readFile != nil && !probed[path.Dir(p)] {
			// vendor/ is never scanned, so look for Composer's generated map directly.
			d := path.Dir(p)
			probed[d] = true
			if b := readFile(path.Join(d, "vendor/composer/autoload_psr4.php")); len(b) > 0 {
				r.psr4 = append(r.psr4, generatedPSR4(d, b)...)
			}
		}
		if ext := path.Ext(p); ext == ".java" || ext == ".php" {
			b := strings.ToLower(path.Base(p))
			r.byBase[b] = append(r.byBase[b], p)
		}
		for d := path.Dir(p); d != "." && !r.dirs[d]; d = path.Dir(d) {
			r.dirs[d] = true
		}
		if strings.HasSuffix(p, ".go") {
			r.goPkgs[path.Dir(p)] = true
		}
		if path.Base(p) == "go.mod" {
			if mod := goModulePath(readFile(p)); mod != "" {
				r.goModules = append(r.goModules, goModule{mod, path.Dir(p)})
			}
		}
	}
	sort.Slice(r.goModules, func(i, j int) bool { return len(r.goModules[i].path) > len(r.goModules[j].path) })
	sort.SliceStable(r.psr4, func(i, j int) bool { return len(r.psr4[i].prefix) > len(r.psr4[j].prefix) })
	return r
}

func goModulePath(gomod []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(gomod))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], "\"")
		}
	}
	return ""
}

func (r *resolver) resolve(grammarLang, from, spec string, maybeSymbol bool) Target {
	switch grammarLang {
	case "Go":
		return r.resolveGo(spec)
	case "Python":
		return r.resolvePython(from, spec, maybeSymbol)
	case "JavaScript", "TypeScript":
		return r.resolveJS(from, spec)
	case "Java":
		return r.resolveJava(spec)
	case "PHP":
		return r.resolvePHP(from, spec)
	}
	return Target{TargetExternal, spec}
}

func (r *resolver) resolveGo(spec string) Target {
	for _, m := range r.goModules {
		if spec == m.path || strings.HasPrefix(spec, m.path+"/") {
			dir := path.Clean(path.Join(m.dir, strings.TrimPrefix(spec, m.path)))
			if r.goPkgs[dir] {
				return Target{TargetPackage, dir}
			}
		}
	}
	return Target{TargetExternal, "go:" + spec}
}

var jsExts = []string{"", ".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"}

func (r *resolver) resolveJS(from, spec string) Target {
	var bases []string
	switch {
	case strings.HasPrefix(spec, "."):
		bases = []string{path.Join(path.Dir(from), spec)}
	case strings.HasPrefix(spec, "@/"), strings.HasPrefix(spec, "~/"):
		// Common tsconfig path aliases; try the usual source roots.
		rest := spec[2:]
		bases = []string{path.Join("src", rest), rest}
		if top := topDir(from); top != "" {
			bases = append(bases, path.Join(top, "src", rest), path.Join(top, rest))
		}
	}
	for _, b := range bases {
		for _, ext := range jsExts {
			if r.files[b+ext] {
				return Target{TargetFile, b + ext}
			}
		}
		// ESM-style "./x.js" that actually points at x.ts
		if trimmed := strings.TrimSuffix(b, path.Ext(b)); trimmed != b {
			for _, ext := range jsExts[1:] {
				if r.files[trimmed+ext] {
					return Target{TargetFile, trimmed + ext}
				}
			}
		}
		if entry := r.packageEntry(b); entry != "" {
			return Target{TargetFile, entry}
		}
		for _, ext := range jsExts[1:] {
			if idx := path.Join(b, "index"+ext); r.files[idx] {
				return Target{TargetFile, idx}
			}
		}
	}
	switch {
	case strings.HasPrefix(spec, "."):
		return unresolvedOrSkipped(path.Join(path.Dir(from), spec))
	case len(bases) > 0: // "@/x" or "~/x" alias; never a valid npm package name
		return Target{TargetExternal, "unresolved:" + spec}
	}
	return Target{TargetExternal, "npm:" + npmPackage(spec)}
}

func npmPackage(spec string) string {
	if strings.HasPrefix(spec, "node:") {
		return spec
	}
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func (r *resolver) resolvePython(from, spec string, maybeSymbol bool) Target {
	var candidates []string
	if strings.HasPrefix(spec, ".") {
		dots := len(spec) - len(strings.TrimLeft(spec, "."))
		dir := path.Dir(from)
		for i := 1; i < dots; i++ {
			dir = path.Dir(dir)
		}
		rest := strings.ReplaceAll(strings.TrimLeft(spec, "."), ".", "/")
		candidates = []string{path.Join(dir, rest)}
	} else {
		rest := strings.ReplaceAll(spec, ".", "/")
		candidates = []string{rest, path.Join("src", rest)}
		if top := topDir(from); top != "" {
			candidates = append(candidates, path.Join(top, rest), path.Join(top, "src", rest))
		}
	}
	for _, c := range candidates {
		t, ok := r.pyModule(c)
		// A bare folder (no __init__.py) named like a top-level import is usually a
		// coincidence — e.g. an "alembic/" migrations folder vs. the alembic library.
		if ok && t.Kind == TargetPackage && !strings.HasPrefix(spec, ".") && !strings.Contains(spec, ".") {
			continue
		}
		if ok {
			return t
		}
	}
	// "from . import name" where name is defined in the package's __init__.py.
	if maybeSymbol {
		if init := path.Join(path.Dir(candidates[0]), "__init__.py"); r.files[init] {
			return Target{TargetFile, init}
		}
	}
	if strings.HasPrefix(spec, ".") {
		return unresolvedOrSkipped(candidates[0])
	}
	return Target{TargetExternal, "py:" + strings.SplitN(spec, ".", 2)[0]}
}

func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return ""
}

// pyModule resolves a slash path to a module file, package __init__, or namespace package dir.
func (r *resolver) pyModule(p string) (Target, bool) {
	switch {
	case r.files[p+".py"]:
		return Target{TargetFile, p + ".py"}, true
	case r.files[path.Join(p, "__init__.py")]:
		return Target{TargetFile, path.Join(p, "__init__.py")}, true
	case r.dirs[p]:
		return Target{TargetPackage, p}, true // PEP 420 namespace package
	}
	return Target{}, false
}

// packageEntry resolves a directory import through its package.json (types, module, main).
func (r *resolver) packageEntry(dir string) string {
	pj := path.Join(dir, "package.json")
	if !r.files[pj] {
		return ""
	}
	var pkg struct{ Types, Typings, Module, Main string }
	if json.Unmarshal(r.readFile(pj), &pkg) != nil {
		return ""
	}
	for _, f := range []string{pkg.Types, pkg.Typings, pkg.Module, pkg.Main} {
		if f == "" {
			continue
		}
		p := path.Join(dir, f)
		if r.files[p] {
			return p
		}
		for _, ext := range jsExts[1:] {
			if r.files[p+ext] {
				return p + ext
			}
		}
	}
	return ""
}

// bySuffix finds the file whose path ends with suffix (compared case-insensitively,
// because PSR-4 maps App\ to app/), preferring the shortest path.
func (r *resolver) bySuffix(suffix string) string {
	low := strings.ToLower(suffix)
	best := ""
	for _, p := range r.byBase[path.Base(low)] {
		lp := strings.ToLower(p)
		if (lp == low || strings.HasSuffix(lp, "/"+low)) && (best == "" || len(p) < len(best)) {
			best = p
		}
	}
	return best
}

// resolveJava maps com.acme.billing.Invoice to …/com/acme/billing/Invoice.java under any
// source root. Static imports and nested classes fall back to the enclosing class; an
// on-demand import (com.acme.model.*) resolves to the package directory.
func (r *resolver) resolveJava(spec string) Target {
	parts := strings.Split(spec, ".")
	for n := len(parts); n >= max(1, len(parts)-2); n-- {
		if p := r.bySuffix(strings.Join(parts[:n], "/") + ".java"); p != "" {
			return Target{TargetFile, p}
		}
	}
	dir := strings.Join(parts, "/")
	for d := range r.dirs {
		if d == dir || strings.HasSuffix(d, "/"+dir) {
			return Target{TargetPackage, d}
		}
	}
	return Target{TargetExternal, "java:" + strings.Join(parts[:min(2, len(parts))], ".")}
}

// resolvePHP handles `use A\B\C` (composer.json PSR-4 maps first, then a path-suffix
// guess) and require/include paths: relative to this file for plain paths and __DIR__,
// anywhere in the repo for other bases (marked "@", e.g. ABSPATH . 'wp-admin/x.php').
func (r *resolver) resolvePHP(from, spec string) Target {
	if unknownBase := strings.HasPrefix(spec, "@"); unknownBase || strings.Contains(spec, "/") || strings.HasSuffix(spec, ".php") {
		rel := strings.TrimPrefix(strings.TrimPrefix(spec, "@"), "/")
		if strings.HasPrefix(rel, "vendor/") || strings.Contains(rel, "/vendor/") {
			return Target{TargetExternal, "composer:vendor"}
		}
		if !unknownBase {
			if p := path.Clean(path.Join(path.Dir(from), rel)); r.files[p] {
				return Target{TargetFile, p}
			}
			if p := path.Clean(rel); r.files[p] {
				return Target{TargetFile, p}
			}
			return unresolvedOrSkipped(path.Clean(path.Join(path.Dir(from), rel)))
		}
		if p := r.bySuffix(path.Clean(rel)); p != "" {
			return Target{TargetFile, p}
		}
		return Target{TargetExternal, "php:include"} // base unknown: not provably broken
	}
	for _, root := range r.psr4 {
		if rest, ok := strings.CutPrefix(spec, root.prefix); ok && rest != "" {
			if p := path.Join(root.dir, strings.ReplaceAll(rest, "\\", "/")+".php"); r.files[p] {
				return Target{TargetFile, p}
			}
		}
	}
	parts := strings.Split(spec, "\\")
	for i := 0; i < len(parts); i++ {
		if p := r.bySuffix(strings.Join(parts[i:], "/") + ".php"); p != "" && (i == 0 || len(parts)-i >= 2 || len(parts) == 1) {
			return Target{TargetFile, p}
		}
	}
	return Target{TargetExternal, "php:" + parts[0]}
}

// unresolvedOrSkipped reports a missing import target — unless it lies in a folder the
// scan skips on purpose (build/, dist/, vendor/, …): then it may well exist.
func unresolvedOrSkipped(p string) Target {
	if skipped(p + "/x") {
		return Target{TargetExternal, "skipped:" + p}
	}
	return Target{TargetExternal, "unresolved:" + p}
}
