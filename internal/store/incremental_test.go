package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, root, p, body string, mtime time.Time) {
	t.Helper()
	full := filepath.Join(root, p)
	os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(full, mtime, mtime)
}

// dump renders a knowledge base canonically, independent of node ids.
func dump(t *testing.T, st *Store) string {
	t.Helper()
	var lines []string
	q := func(query string) {
		rows, err := st.DB.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rows.Scan(ptrs...)
			lines = append(lines, fmt.Sprint(vals...))
		}
	}
	q(`SELECT 'N', kind, qualname, path, line_start, line_end, lang, container, doc FROM nodes`)
	q(`SELECT 'E', e.rel, s.kind, s.qualname, d.kind, d.qualname, e.path, e.line_start, e.line_end, e.spec, e.maybe_sym
		FROM edges e JOIN nodes s ON s.id = e.src JOIN nodes d ON d.id = e.dst`)
	q(`SELECT 'F', path, lang, size, lines, sha256, parsed, doc FROM files`)
	q(`SELECT 'M', path, kind FROM manifests`)
	q(`SELECT 'T', f.qualname, f.doc, f.body FROM nodes_fts f`)
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestIncrementalEqualsFullRebuild(t *testing.T) {
	root := t.TempDir()
	t0 := time.Now().Add(-time.Hour)
	write(t, root, "go.mod", "module example.com/shop\n", t0)
	write(t, root, "cmd/shop/main.go", "package main\nimport \"example.com/shop/internal/pay\"\nfunc main() { pay.Charge() }\n", t0)
	write(t, root, "internal/pay/pay.go", "// Package pay charges.\npackage pay\nfunc Charge() {}\n", t0)
	write(t, root, "app/models.py", "class User:\n    pass\n", t0)
	write(t, root, "app/legacy.py", "def old():\n    pass\n", t0)
	write(t, root, "app/api.py", "from app.models import User\nfrom . import legacy\n\ndef handler():\n    return User()\n", t0)
	write(t, root, "web/app.ts", "import { later } from './later';\nexport function App() {}\n", t0)
	write(t, root, "README.md", "# shop\n", t0)

	inc, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer inc.Close()
	if sum, err := inc.Refresh(root, false); err != nil || !sum.Full {
		t.Fatalf("first refresh = %+v %v", sum, err)
	}

	// A nothing-changed refresh parses nothing.
	if sum, err := inc.Refresh(root, false); err != nil || sum.Full || sum.Parsed != 0 || sum.Deleted != 0 {
		t.Fatalf("no-op refresh = %+v %v", sum, err)
	}

	t1 := time.Now()
	// Edit: new function, new doc, different import.
	write(t, root, "internal/pay/pay.go", "// Package pay charges cards.\npackage pay\nimport \"fmt\"\nfunc Charge() {}\n// Refund gives money back.\nfunc Refund() { fmt.Println() }\n", t1)
	// Add a file that fixes web/app.ts's broken import.
	write(t, root, "web/later.ts", "export const later = () => 1;\n", t1)
	// Delete a module others import: their import must become unresolved.
	os.Remove(filepath.Join(root, "app/legacy.py"))
	// Touch without changing content.
	readme := filepath.Join(root, "README.md")
	os.Chtimes(readme, t1, t1)
	// New infrastructure file.
	write(t, root, "Dockerfile", "FROM golang\n", t1)

	sum, err := inc.Refresh(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Full || sum.Parsed != 3 || sum.Deleted != 1 || sum.Touched != 1 {
		t.Errorf("incremental summary = %+v (want 3 parsed, 1 deleted, 1 touched)", sum)
	}

	full, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	if _, err := full.Refresh(root, true); err != nil {
		t.Fatal(err)
	}

	a, b := dump(t, inc), dump(t, full)
	if a != b {
		al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
		inB := map[string]bool{}
		for _, l := range bl {
			inB[l] = true
		}
		inA := map[string]bool{}
		for _, l := range al {
			inA[l] = true
		}
		for _, l := range al {
			if !inB[l] {
				t.Errorf("only in incremental: %s", l)
			}
		}
		for _, l := range bl {
			if !inA[l] {
				t.Errorf("only in full rebuild: %s", l)
			}
		}
	}

	// Spot checks on behaviour, not just equality.
	if _, users, _ := inc.Deps("web/later.ts", true); len(users) != 1 {
		t.Errorf("web/later.ts should now be imported by app.ts: %+v", users)
	}
	broken, _ := inc.BrokenImports()
	found := false
	for _, h := range broken {
		if h.Path == "app/api.py" {
			found = true
		}
	}
	if !found {
		t.Errorf("deleted legacy.py should leave a broken import in app/api.py: %+v", broken)
	}
	if hits, _ := inc.Find("refund", 5); len(hits) == 0 || hits[0].Detail != "Refund gives money back." {
		t.Errorf("new symbol not searchable: %+v", hits)
	}
}
