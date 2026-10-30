package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotSub(t *testing.T) {
	root := t.TempDir()
	for p, b := range map[string]string{
		"api/go.mod":         "module example.com/api\n",
		"api/main.go":        "package main\nimport \"example.com/api/pay\"\nfunc main() { pay.Charge() }\n",
		"api/pay/pay.go":     "package pay\nimport \"fmt\"\nfunc Charge() { fmt.Println() }\n",
		"web/app.ts":         "import { x } from './util';\nexport function App() {}\n",
		"web/util.ts":        "export const x = 1;\n",
		"docker-compose.yml": "services: {}\n",
	} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(b), 0o644)
	}
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Refresh(root, true)
	snap, _ := st.LoadSnapshot()

	sub := snap.Sub("api")
	if sub.Run.Root != filepath.Join(root, "api") || sub.Run.Files != 3 {
		t.Errorf("root/files = %s %d", sub.Run.Root, sub.Run.Files)
	}
	for _, f := range sub.Files {
		if _, err := os.Stat(filepath.Join(sub.Run.Root, f.Path)); err != nil {
			t.Errorf("file %s not under the new root", f.Path)
		}
	}
	if len(sub.Imports) != 1 || sub.Imports[0].From != "main.go" {
		t.Errorf("imports = %+v", sub.Imports)
	}
	if sub.External["go:fmt"] != 0 && len(sub.ExternalImports) == 0 {
		t.Errorf("external = %+v", sub.External)
	}
	if len(sub.Manifests["go.mod"]) != 1 || sub.Manifests["go.mod"][0] != "go.mod" || len(sub.Manifests["Docker Compose"]) != 0 {
		t.Errorf("manifests = %+v", sub.Manifests)
	}
	if web := snap.Sub("web"); len(web.Files) != 2 || len(web.Symbols) == 0 {
		t.Errorf("web = %d files, %d symbols", len(web.Files), len(web.Symbols))
	}
}
