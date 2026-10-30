package store

import (
	"database/sql"
	"fmt"
	"path"
	"strings"

	"github.com/kuli-dev/kulidev/internal/scan"
)

// Run describes the latest scan.
type Run struct {
	Root, Commit, StartedAt string
	DurationMS, Files       int64
}

// Count is a labelled number, optionally with a line total.
type Count struct {
	Label string
	N     int64
	Lines int64
}

// Stats summarises the knowledge base.
type Stats struct {
	Run           Run
	Parsed        int64
	Errors        int64
	Languages     []Count // N = files
	Symbols       []Count
	InternalEdges int64
	ExternalMods  int64
	TopExternal   []Count // N = importing files
	TopInternal   []Count // N = importing files
	Manifests     []Count
}

func (s *Store) Stats() (*Stats, error) {
	st := &Stats{}
	err := s.DB.QueryRow(`SELECT root, COALESCE(commit_hash,''), started_at, COALESCE(duration_ms,0), COALESCE(files,0)
		FROM runs ORDER BY id DESC LIMIT 1`).Scan(&st.Run.Root, &st.Run.Commit, &st.Run.StartedAt, &st.Run.DurationMS, &st.Run.Files)
	if err != nil {
		return nil, fmt.Errorf("read last run: %w", err)
	}
	s.DB.QueryRow(`SELECT COUNT(*) FILTER (WHERE parsed), COUNT(*) FILTER (WHERE error IS NOT NULL) FROM files`).Scan(&st.Parsed, &st.Errors)

	if st.Languages, err = s.counts(`SELECT lang, COUNT(*), SUM(lines) FROM files WHERE lang IS NOT NULL AND lang != ''
		GROUP BY lang ORDER BY SUM(lines) DESC`); err != nil {
		return nil, err
	}
	if st.Symbols, err = s.counts(`SELECT kind, COUNT(*), 0 FROM nodes WHERE kind NOT IN ('FILE','PACKAGE','EXTERNAL')
		GROUP BY kind ORDER BY COUNT(*) DESC`); err != nil {
		return nil, err
	}
	s.DB.QueryRow(`SELECT COUNT(*) FROM edges e JOIN nodes d ON d.id = e.dst WHERE e.rel='IMPORTS' AND d.kind != 'EXTERNAL'`).Scan(&st.InternalEdges)
	s.DB.QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='EXTERNAL' AND name NOT LIKE 'unresolved:%'`).Scan(&st.ExternalMods)

	importers := `SELECT d.%s, COUNT(DISTINCT e.src), 0 FROM edges e JOIN nodes d ON d.id = e.dst
		WHERE e.rel='IMPORTS' AND %s GROUP BY d.id ORDER BY COUNT(DISTINCT e.src) DESC, d.name LIMIT 8`
	if st.TopInternal, err = s.counts(fmt.Sprintf(importers, "qualname", "d.kind IN ('FILE','PACKAGE')")); err != nil {
		return nil, err
	}
	// Third-party only: unresolved relatives and standard libraries are filtered out.
	ext, err := s.counts(fmt.Sprintf(strings.Replace(importers, "LIMIT 8", "LIMIT 200", 1), "name",
		"d.kind = 'EXTERNAL' AND d.name NOT LIKE 'unresolved:%'"))
	if err != nil {
		return nil, err
	}
	for _, c := range ext {
		if !scan.IsStdlib(c.Label) && len(st.TopExternal) < 8 {
			st.TopExternal = append(st.TopExternal, c)
		}
	}
	if st.Manifests, err = s.counts(`SELECT kind, COUNT(*), 0 FROM manifests GROUP BY kind ORDER BY COUNT(*) DESC, kind`); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) counts(q string, args ...any) ([]Count, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Count
	for rows.Next() {
		var c Count
		var lines sql.NullInt64
		if err := rows.Scan(&c.Label, &c.N, &lines); err != nil {
			return nil, err
		}
		c.Label = strings.TrimPrefix(c.Label, "pkg:")
		c.Lines = lines.Int64
		out = append(out, c)
	}
	return out, rows.Err()
}

// Hit is a search result or a dependency reference, always with evidence.
type Hit struct {
	Kind, Name, Path string
	Line, EndLine    int
	Detail           string // doc text for search hits; line range (+ doc) for outlines
}

// Find runs a full-text search over symbol, file and module names and docs; every
// word must match there (body words are only used by FindAny).
func (s *Store) Find(query string, limit int) ([]Hit, error) {
	return s.search(strings.Fields(splitWords(query)), " AND ", limit, "{name words qualname path doc} : ")
}

// FindAny returns nodes matching any of the words, best matches (most words, name
// hits) first. It is the retrieval step for questions.
func (s *Store) FindAny(words []string, limit int) ([]Hit, error) {
	var all []string
	for _, w := range words {
		all = append(all, strings.Fields(splitWords(w))...)
	}
	return s.search(all, " OR ", limit, "")
}

// search builds an FTS5 query; colFilter (e.g. "{name doc} : ") restricts columns.
func (s *Store) search(words []string, op string, limit int, colFilter string) ([]Hit, error) {
	var terms []string
	seen := map[string]bool{}
	for _, w := range words {
		w = strings.Map(func(r rune) rune {
			if r == '"' || r == '*' {
				return -1
			}
			return r
		}, w)
		if w != "" && !seen[w] {
			seen[w] = true
			terms = append(terms, colFilter+`"`+w+`"*`)
		}
	}
	if len(terms) == 0 {
		return nil, fmt.Errorf("empty query")
	}
	// Name matches rank above matches that only hit the doc text.
	rows, err := s.DB.Query(`SELECT n.kind, n.qualname, COALESCE(n.path,''), COALESCE(n.line_start,0),
			COALESCE(n.line_end,0), COALESCE(n.doc,'')
		FROM nodes_fts f JOIN nodes n ON n.id = f.rowid
		WHERE nodes_fts MATCH ?
		ORDER BY (n.kind IN ('FILE','PACKAGE','EXTERNAL')), bm25(nodes_fts, 10.0, 10.0, 5.0, 2.0, 1.0, 0.3) LIMIT ?`,
		strings.Join(terms, op), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Kind, &h.Name, &h.Path, &h.Line, &h.EndLine, &h.Detail); err != nil {
			return nil, err
		}
		if i := strings.IndexByte(h.Name, '#'); i >= 0 {
			h.Name = h.Name[i+1:]
		}
		h.Name = strings.TrimPrefix(strings.TrimPrefix(h.Name, "pkg:"), "ext:")
		out = append(out, h)
	}
	return out, rows.Err()
}

// Deps lists what target imports (reverse=false) or who imports target (reverse=true).
// target may be a file path, a Go package directory, or an external module name.
func (s *Store) Deps(target string, reverse bool) (resolved string, hits []Hit, err error) {
	target = strings.TrimSuffix(path.Clean(strings.TrimPrefix(target, "./")), "/")
	var ids []int64
	var kind string
	err = s.DB.QueryRow(`SELECT id, kind, qualname FROM nodes
		WHERE (kind='FILE' AND qualname=?1) OR (kind='PACKAGE' AND qualname='pkg:'||?1)
		   OR (kind='EXTERNAL' AND (name=?1 OR name IN ('npm:'||?1,'py:'||?1,'go:'||?1)))
		ORDER BY kind='EXTERNAL' LIMIT 1`, target).Scan(new(int64), &kind, &resolved)
	if err == sql.ErrNoRows {
		return "", nil, fmt.Errorf("%q not found in the knowledge base", target)
	}
	if err != nil {
		return "", nil, err
	}
	resolved = strings.TrimPrefix(strings.TrimPrefix(resolved, "pkg:"), "ext:")

	// A Go file is imported through its package; a package imports through its files.
	q := `SELECT id FROM nodes WHERE qualname = ?`
	args := []any{map[string]string{"FILE": resolved, "PACKAGE": "pkg:" + resolved, "EXTERNAL": "ext:" + resolved}[kind]}
	if kind == "PACKAGE" && !reverse {
		q = `SELECT dst FROM edges WHERE src = (SELECT id FROM nodes WHERE qualname=?) AND rel='CONTAINS'`
	}
	if kind == "FILE" && reverse && strings.HasSuffix(resolved, ".go") {
		q = `SELECT id FROM nodes WHERE qualname IN (?, ?)`
		args = append(args, "pkg:"+path.Dir(resolved))
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return "", nil, err
	}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		return resolved, nil, nil
	}

	in := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args = args[:0]
	for _, id := range ids {
		args = append(args, id)
	}
	col, other := "e.src", "e.dst"
	if reverse {
		col, other = "e.dst", "e.src"
	}
	rows, err = s.DB.Query(fmt.Sprintf(`SELECT o.kind, o.qualname, e.path, e.line_start
		FROM edges e JOIN nodes o ON o.id = %s
		WHERE e.rel='IMPORTS' AND %s IN (%s)
		ORDER BY e.path, e.line_start`, other, col, in), args...)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Kind, &h.Name, &h.Path, &h.Line); err != nil {
			return "", nil, err
		}
		h.Name = strings.TrimPrefix(strings.TrimPrefix(h.Name, "pkg:"), "ext:")
		hits = append(hits, h)
	}
	return resolved, hits, rows.Err()
}

// FileSymbols lists the definitions in one file, in source order.
func (s *Store) FileSymbols(file string) ([]Hit, error) {
	file = path.Clean(strings.TrimPrefix(file, "./"))
	rows, err := s.DB.Query(`SELECT kind, qualname, path, line_start, line_end, COALESCE(doc,'') FROM nodes
		WHERE path = ? AND kind NOT IN ('FILE','PACKAGE','EXTERNAL') ORDER BY line_start`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		var end int
		var doc string
		if err := rows.Scan(&h.Kind, &h.Name, &h.Path, &h.Line, &end, &doc); err != nil {
			return nil, err
		}
		if i := strings.IndexByte(h.Name, '#'); i >= 0 {
			h.Name = h.Name[i+1:]
		}
		h.Detail = fmt.Sprintf("lines %d-%d", h.Line, end)
		if doc != "" {
			h.Detail += " — " + doc
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// BrokenImports lists internal-looking imports whose target does not exist in the repo.
func (s *Store) BrokenImports() ([]Hit, error) {
	rows, err := s.DB.Query(`SELECT e.path, e.line_start, substr(d.name, 12) FROM edges e
		JOIN nodes d ON d.id = e.dst
		WHERE e.rel = 'IMPORTS' AND d.kind = 'EXTERNAL' AND d.name LIKE 'unresolved:%'
		ORDER BY e.path, e.line_start`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		h := Hit{Kind: "IMPORT"}
		if err := rows.Scan(&h.Path, &h.Line, &h.Name); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ScannedCommit returns the commit recorded by the latest scan ("" if unknown).
func (s *Store) ScannedCommit() string {
	var c sql.NullString
	s.DB.QueryRow(`SELECT commit_hash FROM runs ORDER BY id DESC LIMIT 1`).Scan(&c)
	return c.String
}
