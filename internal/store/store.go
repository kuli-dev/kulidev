// Package store persists the knowledge graph in SQLite.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kulidev/kulidev/internal/scan"
)

//go:embed schema.sql
var schema string

// DBFile is the database file name inside the data directory.
const DBFile = "kulidev.db"

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
	if _, err := db.Exec(schema); err != nil {
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
	return Open(dataDir)
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

	for _, f := range res.Files {
		if _, err = tx.Exec(`INSERT INTO files(path, lang, size, lines, sha256, parsed, error) VALUES(?,?,?,?,?,?,?)`,
			f.Path, f.Lang, f.Size, f.Lines, f.SHA256, f.Parsed, nullable(f.Err)); err != nil {
			return 0, err
		}
		if !f.Parsed {
			continue
		}
		fileID, e := ins.node("FILE", f.Path, f.Path, f.Path, 1, f.Lines, f.Lang)
		if e != nil {
			return 0, e
		}
		if f.Lang == "Go" {
			pkg := filepath.ToSlash(filepath.Dir(f.Path))
			pkgID, e := ins.node("PACKAGE", pkg, "pkg:"+pkg, pkg, 0, 0, "Go")
			if e != nil {
				return 0, e
			}
			if e := ins.edge(pkgID, fileID, "CONTAINS", f.Path, 0, 0); e != nil {
				return 0, e
			}
		}
		for _, sym := range f.Symbols {
			q := f.Path + "#" + sym.QualifiedName()
			id, e := ins.symbol(sym.Kind, sym.Name, q, f.Path, sym.LineStart, sym.LineEnd, f.Lang, sym.Container)
			if e != nil {
				return 0, e
			}
			if e := ins.edge(fileID, id, "CONTAINS", f.Path, sym.LineStart, sym.LineEnd); e != nil {
				return 0, e
			}
		}
	}

	for _, imp := range res.Imports {
		fromID := ins.ids["FILE\x00"+imp.From]
		var toID int64
		switch imp.Target.Kind {
		case scan.TargetFile:
			toID = ins.ids["FILE\x00"+imp.Target.Ref]
		case scan.TargetPackage:
			// Go packages already exist; Python namespace packages are created on first use.
			if toID, err = ins.node("PACKAGE", imp.Target.Ref, "pkg:"+imp.Target.Ref, imp.Target.Ref, 0, 0, ""); err != nil {
				return 0, err
			}
		}
		if toID == 0 {
			if toID, err = ins.node("EXTERNAL", imp.Target.Ref, "ext:"+imp.Target.Ref, "", 0, 0, ""); err != nil {
				return 0, err
			}
		}
		if err = ins.edge(fromID, toID, "IMPORTS", imp.From, imp.Line, imp.Line); err != nil {
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
	ftsSt          *sql.Stmt
}

func (in *inserter) prepare() (err error) {
	if in.nodeSt, err = in.tx.Prepare(`INSERT INTO nodes(kind, name, qualname, path, line_start, line_end, lang, container) VALUES(?,?,?,?,?,?,?,?)`); err != nil {
		return err
	}
	if in.edgeSt, err = in.tx.Prepare(`INSERT INTO edges(src, dst, rel, path, line_start, line_end, origin, confidence) VALUES(?,?,?,?,?,?, 'static_analysis', 1.0)`); err != nil {
		return err
	}
	in.ftsSt, err = in.tx.Prepare(`INSERT INTO nodes_fts(rowid, name, words, qualname, path) VALUES(?,?,?,?,?)`)
	return err
}

func (in *inserter) close() {
	for _, st := range []*sql.Stmt{in.nodeSt, in.edgeSt, in.ftsSt} {
		if st != nil {
			st.Close()
		}
	}
}

// node inserts a deduplicated FILE/PACKAGE/EXTERNAL node.
func (in *inserter) node(kind, name, qualname, path string, ls, le int, lang string) (int64, error) {
	key := kind + "\x00" + qualname
	if id, ok := in.ids[key]; ok {
		return id, nil
	}
	id, err := in.symbol(kind, name, qualname, path, ls, le, lang, "")
	if err == nil {
		in.ids[key] = id
	}
	return id, err
}

func (in *inserter) symbol(kind, name, qualname, path string, ls, le int, lang, container string) (int64, error) {
	r, err := in.nodeSt.Exec(kind, name, qualname, nullable(path), ls, le, nullable(lang), nullable(container))
	if err != nil {
		return 0, err
	}
	id, _ := r.LastInsertId()
	_, err = in.ftsSt.Exec(id, name, splitWords(name+" "+container), qualname, path)
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
