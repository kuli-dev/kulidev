package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kuli-dev/kulidev/internal/store"
)

func TestGenerateDocsFromTheUI(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o644)
	os.WriteFile(filepath.Join(root, "main.go"), []byte("// Package main runs x.\npackage main\nfunc main() {}\n"), 0o644)
	st, err := store.Open(filepath.Join(root, ".kulidev"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Refresh(root, true)
	srv, err := New(root, filepath.Join(root, ".kulidev"), st, nil, nil, Options{Loopback: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	if code, _ := call(t, ts, "POST", "/api/docs-job", `{"only":["nope"]}`); code != http.StatusBadRequest {
		t.Errorf("unknown doc = %d", code)
	}
	if code, out := call(t, ts, "POST", "/api/docs-job", `{"factsOnly":true}`); code != 200 || out["running"] != true {
		t.Fatalf("start = %d %v", code, out)
	}
	var job map[string]any
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		_, job = call(t, ts, "GET", "/api/docs-job", "")
		if job["running"] == false {
			break
		}
	}
	if job["running"] != false || job["error"] != nil || job["done"] != job["total"] {
		t.Fatalf("job = %v", job)
	}
	_, list := call(t, ts, "GET", "/api/docs", "")
	if docs, _ := list["docs"].([]any); len(docs) == 0 {
		t.Errorf("no docs listed after generating: %v", list)
	}

	if code, _ := call(t, ts, "POST", "/api/docs-job/cancel", ""); code != 200 {
		t.Errorf("cancel with nothing running = %d", code)
	}
}

func TestDocsPerProject(t *testing.T) {
	root := t.TempDir()
	for _, r := range []string{"shop-api", "worker"} {
		os.MkdirAll(filepath.Join(root, r, ".git"), 0o755)
	}
	for p, body := range map[string]string{
		"shop-api/pyproject.toml":        "[project]\nname = \"shop-core\"\n",
		"shop-api/shop_core/__init__.py": "",
		"shop-api/shop_core/models.py":   "class Order:\n    \"\"\"An order.\"\"\"\n",
		"worker/pyproject.toml":          "[project]\nname = \"worker\"\n",
		"worker/run.py":                  "from shop_core import models\n\ndef main():\n    pass\n",
	} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(body), 0o644)
	}
	st, _ := store.Open(filepath.Join(root, ".kulidev"))
	defer st.Close()
	st.Refresh(root, true)
	srv, err := New(root, filepath.Join(root, ".kulidev"), st, nil, nil, Options{Loopback: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	if code, out := call(t, ts, "POST", "/api/docs-job", `{"scope":"*","factsOnly":true}`); code != 200 || out["total"] != float64(16) {
		t.Fatalf("start all = %d %v (want 3 scopes x 5 docs + system overview)", code, out)
	}
	var job map[string]any
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, job = call(t, ts, "GET", "/api/docs-job", ""); job["running"] == false {
			break
		}
	}
	if job["error"] != nil || job["done"] != job["total"] {
		t.Fatalf("job = %v", job)
	}

	_, list := call(t, ts, "GET", "/api/docs", "")
	docsList := list["docs"].([]any)
	if docsList[0].(map[string]any)["id"] != "system-overview" {
		t.Errorf("folder docs = %v", docsList)
	}
	projects := list["projects"].([]any)
	if len(projects) != 2 || projects[0].(map[string]any)["docs"].(float64) == 0 {
		t.Errorf("projects = %v", projects)
	}
	_, wl := call(t, ts, "GET", "/api/docs?scope=worker", "")
	first := wl["docs"].([]any)[0].(map[string]any)
	name := first["file"].(string)
	if !strings.HasPrefix(name, "projects/worker/") || first["status"] != "CURRENT" {
		t.Fatalf("worker doc = %v", first)
	}
	_, doc := call(t, ts, "GET", "/api/docs/"+name, "")
	if md, _ := doc["markdown"].(string); !strings.Contains(md, "#/file/worker/") {
		t.Errorf("project doc links do not open the folder's files:\n%s", md)
	}
	_, sys := call(t, ts, "GET", "/api/docs/system-overview.md", "")
	if md, _ := sys["markdown"].(string); !strings.Contains(md, "(#/file/worker/run.py#L1)") {
		t.Errorf("system overview links:\n%s", md)
	}
	for _, bad := range []string{"../secret.md", "x/../../a.md", "other/x.md", "projects/../../.kulidev.md"} {
		if code, _ := call(t, ts, "GET", "/api/docs/"+bad, ""); code == 200 {
			t.Errorf("document %q served", bad)
		}
	}
	if code, _ := call(t, ts, "POST", "/api/docs-job", `{"scope":"nope"}`); code != http.StatusBadRequest {
		t.Errorf("unknown scope = %d", code)
	}
}
