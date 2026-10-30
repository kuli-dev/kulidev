// Package store persists the knowledge graph in SQLite.
package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kuli-dev/kulidev/internal/model"
	"github.com/kuli-dev/kulidev/internal/scan"
)

//go:embed schema.sql
var schema string

// DBFile is the database file name inside the data directory.
const DBFile = "kulidev.db"

// schemaVersion changes whenever the schema does. The database is a cache rebuilt by
// `kuli scan`, so an older one is simply dropped instead of migrated.
const schemaVersion = 5

// ErrStale is returned when the knowledge base was built by an older kuli.
var ErrStale = errors.New("knowledge base was built by an older kuli; run `kuli scan` to rebuild it")

// Store is an open knowledge database.
type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the database in dataDir.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	// Keep the database out of version control; generated docs may live alongside it.
	gi := filepath.Join(dataDir, ".gitignore")
	if _, err := os.Stat(gi); os.IsNotExist(err) {
		_ = os.WriteFile(gi, []byte(DBFile+"*\n"), 0o644)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, DBFile)+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var ver int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&ver); err != nil {
		db.Close()
		return nil, err
	}
	if ver != schemaVersion {
		for _, t := range []string{"nodes_fts", "edges", "nodes", "files", "manifests", "runs"} {
			if _, err := db.Exec("DROP TABLE IF EXISTS " + t); err != nil {
				db.Close()
				return nil, err
			}
		}
	}
	if _, err := db.Exec(schema + fmt.Sprintf("\nPRAGMA user_version = %d;", schemaVersion)); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{DB: db}, nil
}

// OpenExisting opens the database only if a scan has already created it.
func OpenExisting(dataDir string) (*Store, error) {
	if _, err := os.Stat(filepath.Join(dataDir, DBFile)); err != nil {
		return nil, fmt.Errorf("no knowledge base in %s (run `kuli scan` first)", dataDir)
	}
	st, err := Open(dataDir)
	if err != nil {
		return nil, err
	}
	var n int
	st.DB.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&n)
	if n == 0 {
		st.Close()
		return nil, ErrStale
	}
	return st, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// Save replaces the stored graph with the result of a scan. v0.1 does a full rebuild;
// incremental updates come later.
func (s *Store) Save(res *scan.Result) (runID int64, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	for _, t := range []string{"edges", "nodes_fts", "nodes", "files", "manifests"} {
		if _, err = tx.Exec("DELETE FROM " + t); err != nil {
			return 0, err
		}
	}

	r, err := tx.Exec(`INSERT INTO runs(root, commit_hash, started_at, duration_ms, status) VALUES(?,?,?,?, 'completed')`,
		res.Root, res.Commit, res.Started.UTC().Format(time.RFC3339), res.Duration.Milliseconds())
	if err != nil {
		return 0, err
	}
	runID, _ = r.LastInsertId()

	ins := &inserter{tx: tx, ids: map[string]int64{}}
	if err = ins.prepare(); err != nil {
		return 0, err
	}
	defer ins.close()

	for i := range res.Files {
		if err = ins.writeFile(&res.Files[i]); err != nil {
			return 0, err
		}
	}
	for _, imp := range res.Imports {
		toID, e := ins.target(imp.Target)
		if e != nil {
			return 0, e
		}
		if err = ins.importEdge(ins.ids["FILE\x00"+imp.From], toID, imp.From, imp.Line, imp.Spec, imp.MaybeSymbol); err != nil {
			return 0, err
		}
	}

	for _, m := range res.Manifests {
		if _, err = tx.Exec(`INSERT INTO manifests(path, kind) VALUES(?,?)`, m.Path, m.Kind); err != nil {
			return 0, err
		}
	}
	if _, err = tx.Exec(`UPDATE runs SET files = ? WHERE id = ?`, len(res.Files), runID); err != nil {
		return 0, err
	}
	return runID, tx.Commit()
}

type inserter struct {
	tx             *sql.Tx
	ids            map[string]int64 // kind\x00qualname -> id, for FILE/PACKAGE/EXTERNAL
	nodeSt, edgeSt *sql.Stmt
	ftsSt, impSt   *sql.Stmt
}

// writeFile records one file: its files row and, when parsed, its FILE node, Go
// package membership, and symbols. Shared by full and incremental saves.
func (in *inserter) writeFile(f *model.FileResult) error {
	if _, err := in.tx.Exec(`INSERT OR REPLACE INTO files(path, lang, size, lines, sha256, parsed, error, doc, mtime_ns) VALUES(?,?,?,?,?,?,?,?,?)`,
		f.Path, f.Lang, f.Size, f.Lines, f.SHA256, f.Parsed, nullable(f.Err), nullable(f.Doc), f.MtimeNS); err != nil {
		return err
	}
	if !f.Parsed {
		return nil
	}
	fileID, err := in.node("FILE", f.Path, f.Path, f.Path, 1, f.Lines, f.Lang, f.Doc)
	if err != nil {
		return err
	}
	if f.Lang == "Go" {
		pkg := filepath.ToSlash(filepath.Dir(f.Path))
		pkgID, err := in.node("PACKAGE", pkg, "pkg:"+pkg, pkg, 0, 0, "Go", "")
		if err != nil {
			return err
		}
		if err := in.edge(pkgID, fileID, "CONTAINS", f.Path, 0, 0); err != nil {
			return err
		}
	}
	for _, sym := range f.Symbols {
		q := f.Path + "#" + sym.QualifiedName()
		id, err := in.symbol(sym.Kind, sym.Name, q, f.Path, sym.LineStart, sym.LineEnd, f.Lang, sym.Container, sym.Doc, sym.Terms)
		if err != nil {
			return err
		}
		if err := in.edge(fileID, id, "CONTAINS", f.Path, sym.LineStart, sym.LineEnd); err != nil {
			return err
		}
	}
	return nil
}

// target returns the node an import resolves to, creating package/external nodes.
func (in *inserter) target(t scan.Target) (int64, error) {
	switch t.Kind {
	case scan.TargetFile:
		if id := in.ids["FILE\x00"+t.Ref]; id != 0 {
			return id, nil
		}
	case scan.TargetPackage:
		// Go packages usually exist already; Python namespace packages are created on use.
		return in.node("PACKAGE", t.Ref, "pkg:"+t.Ref, t.Ref, 0, 0, "", "")
	}
	return in.node("EXTERNAL", t.Ref, "ext:"+t.Ref, "", 0, 0, "", "")
}

func (in *inserter) importEdge(src, dst int64, path string, line int, spec string, maybe bool) error {
	_, err := in.impSt.Exec(src, dst, path, line, line, spec, maybe)
	return err
}

func (in *inserter) prepare() (err error) {
	if in.nodeSt, err = in.tx.Prepare(`INSERT INTO nodes(kind, name, qualname, path, line_start, line_end, lang, container, doc) VALUES(?,?,?,?,?,?,?,?,?)`); err != nil {
		return err
	}
	if in.edgeSt, err = in.tx.Prepare(`INSERT INTO edges(src, dst, rel, path, line_start, line_end, origin, confidence) VALUES(?,?,?,?,?,?, 'static_analysis', 1.0)`); err != nil {
		return err
	}
	if in.impSt, err = in.tx.Prepare(`INSERT INTO edges(src, dst, rel, path, line_start, line_end, origin, confidence, spec, maybe_sym) VALUES(?,?, 'IMPORTS', ?,?,?, 'static_analysis', 1.0, ?,?)`); err != nil {
		return err
	}
	in.ftsSt, err = in.tx.Prepare(`INSERT INTO nodes_fts(rowid, name, words, qualname, path, doc, body) VALUES(?,?,?,?,?,?,?)`)
	return err
}

func (in *inserter) close() {
	for _, st := range []*sql.Stmt{in.nodeSt, in.edgeSt, in.ftsSt, in.impSt} {
		if st != nil {
			st.Close()
		}
	}
}

// node inserts a deduplicated FILE/PACKAGE/EXTERNAL node.
func (in *inserter) node(kind, name, qualname, path string, ls, le int, lang, doc string) (int64, error) {
	key := kind + "\x00" + qualname
	if id, ok := in.ids[key]; ok {
		return id, nil
	}
	id, err := in.symbol(kind, name, qualname, path, ls, le, lang, "", doc, "")
	if err == nil {
		in.ids[key] = id
	}
	return id, err
}

func (in *inserter) symbol(kind, name, qualname, path string, ls, le int, lang, container, doc, terms string) (int64, error) {
	r, err := in.nodeSt.Exec(kind, name, qualname, nullable(path), ls, le, nullable(lang), nullable(container), nullable(doc))
	if err != nil {
		return 0, err
	}
	id, _ := r.LastInsertId()
	_, err = in.ftsSt.Exec(id, name, splitWords(name+" "+container), qualname, path, doc, terms)
	return id, err
}

func (in *inserter) edge(src, dst int64, rel, path string, ls, le int) error {
	_, err := in.edgeSt.Exec(src, dst, rel, path, ls, le)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])|([A-Z])([A-Z][a-z])`)

// splitWords turns "PaymentService get_user HTTPServer" into searchable lowercase words
// so FTS can match "payment" or "server".
func splitWords(s string) string {
	s = camelBoundary.ReplaceAllString(s, "$1$3 $2$4")
	s = strings.NewReplacer("_", " ", "-", " ", ".", " ", "/", " ").Replace(s)
	return strings.ToLower(s)
}
