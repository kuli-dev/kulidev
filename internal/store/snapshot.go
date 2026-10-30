package store

import (
	"path/filepath"
	"strings"

	"github.com/kuli-dev/kulidev/internal/scan"
)

// FileInfo is one scanned file.
type FileInfo struct {
	Path, Lang, SHA256 string
	Doc                string
	Lines              int
	Parsed             bool
}

// SymbolInfo is a top-level definition (no container) or a class-like type.
type SymbolInfo struct {
	Kind, Name, Path string
	Doc              string
	Line, EndLine    int
}

// ImportPair is one internal import: From imports To (a file path or Go package dir).
type ImportPair struct {
	From, To string
	Line     int
}

// Snapshot is the whole knowledge base loaded into memory for document generation.
type Snapshot struct {
	Run      Run
	Files    []FileInfo
	Symbols  []SymbolInfo
	Imports  []ImportPair
	External map[string]int // external module → number of importing files
	// ExternalImports lists each external import (To is "<ecosystem>:<module>").
	ExternalImports []ImportPair
	Manifests       map[string][]string
}

// LoadSnapshot reads everything the docs engine needs in a handful of queries.
func (s *Store) LoadSnapshot() (*Snapshot, error) {
	snap := &Snapshot{External: map[string]int{}, Manifests: map[string][]string{}}
	st, err := s.Stats()
	if err != nil {
		return nil, err
	}
	snap.Run = st.Run

	rows, err := s.DB.Query(`SELECT path, COALESCE(lang,''), COALESCE(sha256,''), COALESCE(lines,0), parsed, COALESCE(doc,'') FROM files ORDER BY path`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f FileInfo
		if err := rows.Scan(&f.Path, &f.Lang, &f.SHA256, &f.Lines, &f.Parsed, &f.Doc); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Files = append(snap.Files, f)
	}
	rows.Close()

	rows, err = s.DB.Query(`SELECT kind, name, path, line_start, line_end, COALESCE(doc,'') FROM nodes
		WHERE kind NOT IN ('FILE','PACKAGE','EXTERNAL','method') AND container IS NULL
		ORDER BY path, line_start`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var y SymbolInfo
		if err := rows.Scan(&y.Kind, &y.Name, &y.Path, &y.Line, &y.EndLine, &y.Doc); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Symbols = append(snap.Symbols, y)
	}
	rows.Close()

	rows, err = s.DB.Query(`SELECT e.path, d.kind, d.qualname, e.line_start FROM edges e
		JOIN nodes d ON d.id = e.dst WHERE e.rel = 'IMPORTS'`)
	if err != nil {
		return nil, err
	}
	extSeen := map[[2]string]bool{}
	for rows.Next() {
		var from, kind, q string
		var line int
		if err := rows.Scan(&from, &kind, &q, &line); err != nil {
			rows.Close()
			return nil, err
		}
		switch kind {
		case "FILE":
			snap.Imports = append(snap.Imports, ImportPair{from, q, line})
		case "PACKAGE":
			snap.Imports = append(snap.Imports, ImportPair{from, strings.TrimPrefix(q, "pkg:"), line})
		case "EXTERNAL":
			name := strings.TrimPrefix(q, "ext:")
			if strings.HasPrefix(name, "unresolved:") || scan.IsStdlib(name) {
				continue
			}
			snap.ExternalImports = append(snap.ExternalImports, ImportPair{from, name, line})
			if k := [2]string{from, name}; !extSeen[k] {
				extSeen[k] = true
				snap.External[name]++
			}
		}
	}
	rows.Close()

	rows, err = s.DB.Query(`SELECT kind, path FROM manifests ORDER BY kind, path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, p string
		if err := rows.Scan(&k, &p); err != nil {
			return nil, err
		}
		snap.Manifests[k] = append(snap.Manifests[k], p)
	}
	return snap, rows.Err()
}

// Sub is the part of the snapshot under dir (a project inside the scanned folder),
// re-rooted there: paths become relative to dir. Imports reaching outside dir are left
// out; the Systems map describes those.
func (s *Snapshot) Sub(dir string) *Snapshot {
	dir = strings.Trim(dir, "/")
	pre := dir + "/"
	in := func(p string) (string, bool) {
		if strings.HasPrefix(p, pre) {
			return p[len(pre):], true
		}
		return "", false
	}
	out := &Snapshot{Run: s.Run, External: map[string]int{}, Manifests: map[string][]string{}}
	out.Run.Root = filepath.Join(s.Run.Root, filepath.FromSlash(dir))
	out.Run.Files = 0
	for _, f := range s.Files {
		if p, ok := in(f.Path); ok {
			f.Path = p
			out.Files = append(out.Files, f)
			out.Run.Files++
		}
	}
	for _, y := range s.Symbols {
		if p, ok := in(y.Path); ok {
			y.Path = p
			out.Symbols = append(out.Symbols, y)
		}
	}
	for _, im := range s.Imports {
		from, ok1 := in(im.From)
		to, ok2 := in(im.To)
		if ok1 && ok2 {
			out.Imports = append(out.Imports, ImportPair{from, to, im.Line})
		}
	}
	seen := map[[2]string]bool{}
	for _, im := range s.ExternalImports {
		if from, ok := in(im.From); ok {
			out.ExternalImports = append(out.ExternalImports, ImportPair{from, im.To, im.Line})
			if k := [2]string{from, im.To}; !seen[k] {
				seen[k] = true
				out.External[im.To]++
			}
		}
	}
	for kind, paths := range s.Manifests {
		for _, p := range paths {
			if q, ok := in(p); ok {
				out.Manifests[kind] = append(out.Manifests[kind], q)
			}
		}
	}
	return out
}
