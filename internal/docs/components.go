package docs

import (
	"path"
	"sort"
	"strings"

	"github.com/kuli-dev/kulidev/internal/store"
)

// Component is a directory-level unit of the codebase.
type Component struct {
	Name      string
	Files     []store.FileInfo // parsed source files
	Lines     int
	Langs     map[string]int // language → lines
	Symbols   []store.SymbolInfo
	Importers map[string]int // other component → importing files
	DependsOn map[string]int // other component → importing files
	inDegree  map[string]int // file → distinct importing files (any component)
}

// Hub returns the component's most imported file, falling back to its largest.
func (c *Component) Hub() store.FileInfo {
	best, bestN := c.Files[0], -1
	for _, f := range c.Files {
		n := c.inDegree[f.Path]
		if n > bestN || (n == bestN && f.Lines > best.Lines) {
			best, bestN = f, n
		}
	}
	return best
}

// MainLang returns the language with the most lines.
func (c *Component) MainLang() string {
	best, n := "", -1
	for l, v := range c.Langs {
		if v > n || (v == n && l < best) {
			best, n = l, v
		}
	}
	return best
}

const rootComponent = "(root)"

// splitThreshold: a component holding more than this share of source files is split
// one directory level deeper, up to maxDepth.
const (
	splitThreshold = 0.20
	maxDepth       = 4
)

// BuildComponents groups parsed files into components and computes the
// component-level dependency graph from internal imports.
func BuildComponents(snap *store.Snapshot) ([]*Component, map[string]string) {
	var src []store.FileInfo
	for _, f := range snap.Files {
		if f.Parsed {
			src = append(src, f)
		}
	}
	split := map[string]bool{}
	keyOf := func(p string) string {
		dir := path.Dir(p)
		if dir == "." {
			return rootComponent
		}
		segs := strings.Split(dir, "/")
		key := segs[0]
		for i := 1; i < len(segs) && i < maxDepth && split[key]; i++ {
			key += "/" + segs[i]
		}
		return key
	}
	for round := 0; round < maxDepth; round++ {
		counts := map[string]int{}
		deeper := map[string]bool{} // component has files in subdirectories
		for _, f := range src {
			k := keyOf(f.Path)
			counts[k]++
			if k != rootComponent && path.Dir(f.Path) != k {
				deeper[k] = true
			}
		}
		changed := false
		for k, n := range counts {
			if !split[k] && deeper[k] && float64(n) > splitThreshold*float64(len(src)) && strings.Count(k, "/") < maxDepth-1 {
				split[k], changed = true, true
			}
		}
		if !changed {
			break
		}
	}

	byName := map[string]*Component{}
	fileComp := map[string]string{}
	get := func(name string) *Component {
		c := byName[name]
		if c == nil {
			c = &Component{Name: name, Langs: map[string]int{}, Importers: map[string]int{}, DependsOn: map[string]int{}, inDegree: map[string]int{}}
			byName[name] = c
		}
		return c
	}
	for _, f := range src {
		k := keyOf(f.Path)
		c := get(k)
		c.Files = append(c.Files, f)
		c.Lines += f.Lines
		c.Langs[f.Lang] += f.Lines
		fileComp[f.Path] = k
	}
	for _, y := range snap.Symbols {
		if k, ok := fileComp[y.Path]; ok {
			byName[k].Symbols = append(byName[k].Symbols, y)
		}
	}

	// Go imports point at package dirs; map them to the component of a file inside.
	dirComp := map[string]string{}
	for p, k := range fileComp {
		dirComp[path.Dir(p)] = k
	}
	type pair struct{ from, to string }
	seen := map[pair]map[string]bool{}
	importedBy := map[string]map[string]bool{}
	for _, im := range snap.Imports {
		to, ok := fileComp[im.To]
		if !ok {
			to, ok = dirComp[im.To]
		}
		from, ok2 := fileComp[im.From]
		if !ok || !ok2 {
			continue
		}
		if importedBy[im.To] == nil {
			importedBy[im.To] = map[string]bool{}
		}
		importedBy[im.To][im.From] = true
		if from == to {
			continue
		}
		pr := pair{from, to}
		if seen[pr] == nil {
			seen[pr] = map[string]bool{}
		}
		seen[pr][im.From] = true
	}
	for pr, files := range seen {
		byName[pr.from].DependsOn[pr.to] = len(files)
		byName[pr.to].Importers[pr.from] = len(files)
	}
	for target, files := range importedBy {
		if k, ok := fileComp[target]; ok {
			byName[k].inDegree[target] = len(files)
		}
	}

	comps := make([]*Component, 0, len(byName))
	for _, c := range byName {
		comps = append(comps, c)
	}
	sort.Slice(comps, func(i, j int) bool {
		if comps[i].Lines != comps[j].Lines {
			return comps[i].Lines > comps[j].Lines
		}
		return comps[i].Name < comps[j].Name
	})
	return comps, fileComp
}
