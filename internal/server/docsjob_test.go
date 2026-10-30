package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
