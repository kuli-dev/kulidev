package scan

import (
	"bufio"
	"bytes"
	"path"
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
	goPkgs    map[string]bool // dirs containing .go files
	goModules []goModule      // longest module path first
}

type goModule struct {
	path string // module path from go.mod
	dir  string // directory of go.mod relative to root ("." for root)
}

func newResolver(paths []string, readFile func(string) []byte) *resolver {
	r := &resolver{files: map[string]bool{}, goPkgs: map[string]bool{}}
	for _, p := range paths {
		r.files[p] = true
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

func (r *resolver) resolve(grammarLang, from, spec string) Target {
	switch grammarLang {
	case "Go":
		return r.resolveGo(spec)
	case "Python":
		return r.resolvePython(from, spec)
	case "JavaScript", "TypeScript":
		return r.resolveJS(from, spec)
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
		for _, ext := range jsExts[1:] {
			if idx := path.Join(b, "index"+ext); r.files[idx] {
				return Target{TargetFile, idx}
			}
		}
	}
	switch {
	case strings.HasPrefix(spec, "."):
		return Target{TargetExternal, "unresolved:" + path.Join(path.Dir(from), spec)}
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

func (r *resolver) resolvePython(from, spec string) Target {
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
		if r.files[c+".py"] {
			return Target{TargetFile, c + ".py"}
		}
		if r.files[path.Join(c, "__init__.py")] {
			return Target{TargetFile, path.Join(c, "__init__.py")}
		}
	}
	if strings.HasPrefix(spec, ".") {
		return Target{TargetExternal, "unresolved:" + candidates[0]}
	}
	return Target{TargetExternal, "py:" + strings.SplitN(spec, ".", 2)[0]}
}

func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return ""
}
