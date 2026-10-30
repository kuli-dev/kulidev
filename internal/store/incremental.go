package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/kuli-dev/kulidev/internal/scan"
)

// Summary describes what a refresh did.
type Summary struct {
	Full                     bool
	Files                    int
	Parsed, Touched, Deleted int
	Duration                 time.Duration
}

func (s Summary) String() string {
	if s.Full {
		return fmt.Sprintf("full scan of %d files in %s", s.Files, s.Duration.Round(time.Millisecond))
	}
	return fmt.Sprintf("%d files: %d re-parsed, %d deleted, %d unchanged-content touched, in %s",
		s.Files, s.Parsed, s.Deleted, s.Touched, s.Duration.Round(time.Millisecond))
}

// Refresh brings the knowledge base up to date with the tree at root: incrementally
// when a previous scan exists, otherwise (or when full is set) by rebuilding.
func (s *Store) Refresh(root string, full bool) (Summary, error) {
	start := time.Now()
	prev, err := s.Stamps()
	if err != nil {
		return Summary{}, err
	}
	if full || len(prev) == 0 {
		res, err := scan.Scan(root)
		if err != nil {
			return Summary{}, err
		}
		if _, err := s.Save(res); err != nil {
			return Summary{}, err
		}
		return Summary{Full: true, Files: len(res.Files), Parsed: len(res.Files), Duration: time.Since(start)}, nil
	}
	d, err := scan.Incremental(root, prev)
	if err != nil {
		return Summary{}, err
	}
	if err := s.ApplyDelta(d); err != nil {
		return Summary{}, err
	}
	return Summary{Files: d.Total, Parsed: len(d.Changed), Touched: len(d.Touched), Deleted: len(d.Deleted),
		Duration: time.Since(start)}, nil
}

// Stamps returns what the last scan recorded per file (empty when there is none).
func (s *Store) Stamps() (map[string]scan.Stamp, error) {
	var runs int
	s.DB.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&runs)
	out := map[string]scan.Stamp{}
	if runs == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(`SELECT path, COALESCE(size,0), COALESCE(mtime_ns,0), COALESCE(sha256,'') FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var st scan.Stamp
		if err := rows.Scan(&p, &st.Size, &st.MtimeNS, &st.SHA256); err != nil {
			return nil, err
		}
		out[p] = st
	}
	return out, rows.Err()
}

// ApplyDelta updates only what changed. Node ids of files that still exist are kept,
// so other files' import edges pointing at them stay valid; when files appear or
// disappear, every stored import is re-resolved from its raw spec (no re-parsing).
func (s *Store) ApplyDelta(d *scan.Delta) (err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	ins := &inserter{tx: tx, ids: map[string]int64{}}
	if err = ins.prepare(); err != nil {
		return err
	}
	defer ins.close()
	if err = ins.loadIDs(); err != nil {
		return err
	}

	// Order matters: deleting a FILE node cascades to the import edges pointing at it,
	// so imports are re-resolved (re-pointed, e.g. to "unresolved") before any file
	// node is dropped. Otherwise a deleted module's importers would silently lose the
	// import instead of showing it as broken.
	drop := append([]string(nil), d.Deleted...)
	fileSetChanged := len(d.Added) > 0 || len(d.Deleted) > 0

	// Changed files: replace symbols and outgoing imports; keep the FILE node.
	for i := range d.Changed {
		f := &d.Changed[i]
		id := ins.ids["FILE\x00"+f.Path]
		switch {
		case id != 0 && !f.Parsed:
			drop = append(drop, f.Path) // no longer parseable: its FILE node goes later
			fileSetChanged = true
			if _, err = tx.Exec(`DELETE FROM edges WHERE src = ? AND rel = 'IMPORTS'`, id); err != nil {
				return err
			}
		case id != 0:
			if err = ins.dropPath(f.Path, false); err != nil {
				return err
			}
			if _, err = tx.Exec(`DELETE FROM edges WHERE src = ? AND rel = 'IMPORTS'`, id); err != nil {
				return err
			}
			if _, err = tx.Exec(`UPDATE nodes SET line_end = ?, lang = ?, doc = ? WHERE id = ?`, f.Lines, nullable(f.Lang), nullable(f.Doc), id); err != nil {
				return err
			}
			if _, err = tx.Exec(`UPDATE nodes_fts SET doc = ? WHERE rowid = ?`, f.Doc, id); err != nil {
				return err
			}
		case f.Parsed:
			fileSetChanged = true // a FILE node appears
		}
		if !(id != 0 && !f.Parsed) {
			if err = ins.writeFile(f); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(`DELETE FROM manifests WHERE path = ?`, f.Path); err != nil {
			return err
		}
		if kind := d.Manifests[f.Path]; kind != "" {
			if _, err = tx.Exec(`INSERT INTO manifests(path, kind) VALUES(?,?)`, f.Path, kind); err != nil {
				return err
			}
		}
	}
	for _, t := range d.Touched {
		if _, err = tx.Exec(`UPDATE files SET mtime_ns = ? WHERE path = ?`, t.MtimeNS, t.Path); err != nil {
			return err
		}
	}

	// The set of files changed: every existing import may now resolve differently.
	if fileSetChanged {
		for _, p := range drop {
			delete(ins.ids, "FILE\x00"+p) // so nothing resolves to a file that is going away
		}
		if err = ins.reresolve(d); err != nil {
			return err
		}
	}

	// Now drop deleted (and no-longer-parseable) files; nothing points at them any more.
	for _, p := range drop {
		if err = ins.dropPath(p, true); err != nil {
			return err
		}
	}
	for _, p := range d.Deleted {
		if _, err = tx.Exec(`DELETE FROM files WHERE path = ?`, p); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM manifests WHERE path = ?`, p); err != nil {
			return err
		}
	}
	for _, p := range drop {
		for i := range d.Changed {
			if d.Changed[i].Path == p { // re-record the unparsed file's row (lang, size, …)
				if err = ins.writeFile(&d.Changed[i]); err != nil {
					return err
				}
			}
		}
	}

	// Imports of the changed files themselves.
	for _, f := range d.Changed {
		if !f.Parsed {
			continue
		}
		from := ins.ids["FILE\x00"+f.Path]
		for _, imp := range f.Imports {
			to, e := ins.target(d.Resolve(f.Lang, f.Path, imp.Spec, imp.MaybeSymbol))
			if e != nil {
				return e
			}
			if err = ins.importEdge(from, to, f.Path, imp.Line, imp.Spec, imp.MaybeSymbol); err != nil {
				return err
			}
		}
	}
	if err = ins.dropOrphans(); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO runs(root, commit_hash, started_at, duration_ms, files, status) VALUES(?,?,?,?,?, 'completed')`,
		d.Root, d.Commit, d.Started.UTC().Format(time.RFC3339), d.Duration.Milliseconds(), d.Total); err != nil {
		return err
	}
	return tx.Commit()
}

func (in *inserter) loadIDs() error {
	rows, err := in.tx.Query(`SELECT id, kind, qualname FROM nodes WHERE kind IN ('FILE','PACKAGE','EXTERNAL')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var kind, q string
		if err := rows.Scan(&id, &kind, &q); err != nil {
			return err
		}
		in.ids[kind+"\x00"+q] = id
	}
	return rows.Err()
}

// dropPath deletes a file's symbol nodes (and its FILE node when withFile), with their
// full-text rows; edges go with them via ON DELETE CASCADE.
func (in *inserter) dropPath(path string, withFile bool) error {
	q := `SELECT id FROM nodes WHERE path = ? AND kind NOT IN ('FILE','PACKAGE','EXTERNAL')`
	if withFile {
		q = `SELECT id FROM nodes WHERE path = ? AND kind NOT IN ('PACKAGE','EXTERNAL')`
	}
	return in.deleteNodes(q, path)
}

func (in *inserter) deleteNodes(query string, args ...any) error {
	rows, err := in.tx.Query(query, args...)
	if err != nil {
		return err
	}
	var ids []any
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for len(ids) > 0 {
		n := min(len(ids), 500)
		ph := strings.TrimSuffix(strings.Repeat("?,", n), ",")
		if _, err := in.tx.Exec(`DELETE FROM nodes_fts WHERE rowid IN (`+ph+`)`, ids[:n]...); err != nil {
			return err
		}
		if _, err := in.tx.Exec(`DELETE FROM nodes WHERE id IN (`+ph+`)`, ids[:n]...); err != nil {
			return err
		}
		ids = ids[n:]
	}
	return nil
}

// reresolve recomputes the target of every stored import from its raw spec.
func (in *inserter) reresolve(d *scan.Delta) error {
	type row struct {
		id, dst          int64
		from, spec, lang string
		maybe            bool
	}
	rows, err := in.tx.Query(`SELECT e.id, e.dst, e.path, COALESCE(e.spec,''), COALESCE(n.lang,''), COALESCE(e.maybe_sym,0)
		FROM edges e JOIN nodes n ON n.id = e.src WHERE e.rel = 'IMPORTS'`)
	if err != nil {
		return err
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.dst, &r.from, &r.spec, &r.lang, &r.maybe); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	upd, err := in.tx.Prepare(`UPDATE edges SET dst = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer upd.Close()
	for _, r := range all {
		to, err := in.target(d.Resolve(r.lang, r.from, r.spec, r.maybe))
		if err != nil {
			return err
		}
		if to != r.dst {
			if _, err := upd.Exec(to, r.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// dropOrphans removes package/external nodes nothing refers to any more.
func (in *inserter) dropOrphans() error {
	err := in.deleteNodes(`SELECT id FROM nodes n WHERE kind IN ('EXTERNAL','PACKAGE')
		AND NOT EXISTS (SELECT 1 FROM edges e WHERE e.dst = n.id OR e.src = n.id)`)
	if err != nil {
		return err
	}
	for k, id := range in.ids {
		var x int64
		if in.tx.QueryRow(`SELECT id FROM nodes WHERE id = ?`, id).Scan(&x) == sql.ErrNoRows {
			delete(in.ids, k)
		}
	}
	return nil
}
