package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/scan"
	"github.com/kuli-dev/kulidev/internal/store"
)

func setup(t *testing.T) (*httptest.Server, string) {
	return setupWith(t, Options{Loopback: true})
}

func setupWith(t *testing.T, opts Options) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	for p, b := range map[string]string{
		"go.mod":                  "module example.com/shop\n",
		"cmd/shop/main.go":        "package main\nimport \"example.com/shop/internal/payment\"\nfunc main() { payment.Charge() }\n",
		"internal/payment/pay.go": "// Package payment charges cards.\npackage payment\n// Charge takes money.\nfunc Charge() {}\n",
		"web/app.ts":              "import { x } from './missing';\n",
	} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(b), 0o644)
	}
	os.WriteFile(filepath.Join(filepath.Dir(root), "secret.txt"), []byte("TOP SECRET"), 0o644)

	dataDir := filepath.Join(root, ".kulidev")
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	res, _ := scan.Scan(root)
	st.Save(res)
	snap, _ := st.LoadSnapshot()
	if _, err := docs.Generate(context.Background(), snap, docs.Options{OutDir: filepath.Join(dataDir, "knowledge"), Version: "t"}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(root, dataDir, st, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, root
}

func get(t *testing.T, ts *httptest.Server, path string, into any) int {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if into != nil {
		json.NewDecoder(resp.Body).Decode(into)
	}
	return resp.StatusCode
}

func TestAPI(t *testing.T) {
	ts, _ := setup(t)

	var ov map[string]any
	if get(t, ts, "/api/overview", &ov) != 200 || ov["repo"] == "" || ov["llm"] != false {
		t.Errorf("overview = %v", ov)
	}

	var g struct {
		Components []compJSON `json:"components"`
		Edges      []edgeJSON `json:"edges"`
	}
	get(t, ts, "/api/graph", &g)
	if len(g.Components) < 2 || len(g.Edges) != 1 || g.Edges[0].From != "cmd/shop" {
		t.Errorf("graph = %+v", g)
	}

	var sr struct{ Hits []store.Hit }
	get(t, ts, "/api/search?q=charge", &sr)
	if len(sr.Hits) == 0 || sr.Hits[0].Name != "Charge" || sr.Hits[0].Detail != "Charge takes money." {
		t.Errorf("search = %+v", sr)
	}

	var f struct {
		Lines     []string
		Symbols   []store.Hit
		Importers []store.Hit
	}
	if get(t, ts, "/api/file?path=internal/payment/pay.go", &f) != 200 || len(f.Lines) != 4 || len(f.Importers) != 1 {
		t.Errorf("file = %+v", f)
	}

	var br struct{ Hits []store.Hit }
	get(t, ts, "/api/broken", &br)
	if len(br.Hits) != 1 || br.Hits[0].Path != "web/app.ts" {
		t.Errorf("broken = %+v", br)
	}

	var dl struct{ Docs []map[string]any }
	get(t, ts, "/api/docs", &dl)
	if len(dl.Docs) < 4 || dl.Docs[0]["status"] != "CURRENT" {
		t.Errorf("docs = %+v", dl)
	}
	var d struct{ Markdown string }
	get(t, ts, "/api/docs/architecture.md", &d)
	if !strings.Contains(d.Markdown, "](#/file/internal/payment/pay.go#L") || strings.HasPrefix(d.Markdown, "---") {
		t.Errorf("doc links not rewritten / front matter kept:\n%s", d.Markdown)
	}

	var tr struct{ Files []struct{ P string } }
	get(t, ts, "/api/tree", &tr)
	if len(tr.Files) != 4 {
		t.Errorf("tree = %+v", tr)
	}
	var def struct{ Hits []store.Hit }
	get(t, ts, "/api/definition?name=Charge", &def)
	if len(def.Hits) != 1 || def.Hits[0].Path != "internal/payment/pay.go" || def.Hits[0].Line != 4 {
		t.Errorf("definition = %+v", def)
	}

	if code := get(t, ts, "/api/nope", nil); code != 404 {
		t.Errorf("unknown endpoint = %d", code)
	}
}

func TestNoPathEscapes(t *testing.T) {
	ts, _ := setup(t)
	for _, p := range []string{"../secret.txt", "/etc/passwd", "internal/../../secret.txt", ".kulidev/kulidev.db"} {
		resp, err := http.Get(ts.URL + "/api/file?path=" + p)
		if err != nil {
			t.Fatal(err)
		}
		var body strings.Builder
		buf := make([]byte, 4096)
		n, _ := resp.Body.Read(buf)
		body.Write(buf[:n])
		resp.Body.Close()
		if resp.StatusCode == 200 || strings.Contains(body.String(), "TOP SECRET") || strings.Contains(body.String(), "root:") {
			t.Errorf("path %q served: %d %s", p, resp.StatusCode, body.String())
		}
	}
	for _, name := range []string{"..%2f..%2fsecret.txt", "manifest.json"} {
		if code := get(t, ts, "/api/docs/"+name, nil); code == 200 {
			t.Errorf("docs %q served", name)
		}
	}
}

func TestCrossOriginPostRefused(t *testing.T) {
	ts, _ := setup(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/rescan", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin rescan = %d, want 403", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", ts.URL+"/api/rescan", nil)
	req.Header.Set("Origin", ts.URL)
	req.Header.Set("X-Kuli-Request", "1")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("same-origin rescan = %d", resp.StatusCode)
	}
	// Ask without a model is a clear 503, not a crash.
	req, _ = http.NewRequest("POST", ts.URL+"/api/ask", strings.NewReader(`{"question":"x"}`))
	req.Header.Set("X-Kuli-Request", "1")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("ask without model = %d", resp.StatusCode)
	}
}

func put(t *testing.T, ts *httptest.Server, body string, header bool) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("PUT", ts.URL+"/api/file", strings.NewReader(body))
	if header {
		req.Header.Set("X-Kuli-Request", "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func saveBody(path, content, base string, force bool) string {
	b, _ := json.Marshal(map[string]any{"path": path, "content": content, "baseHash": base, "force": force})
	return string(b)
}

func TestSaveFile(t *testing.T) {
	ts, root := setup(t)
	const p = "internal/payment/pay.go"
	var f struct{ Hash, Content string }
	get(t, ts, "/api/file?path="+p, &f)
	if f.Hash == "" || !strings.Contains(f.Content, "func Charge") {
		t.Fatalf("file = %+v", f)
	}
	info, _ := os.Stat(filepath.Join(root, p))

	// Normal save.
	edited := strings.Replace(f.Content, "func Charge() {}", "func Charge() { /* edited */ }", 1)
	code, out := put(t, ts, saveBody(p, edited, f.Hash, false), true)
	if code != 200 {
		t.Fatalf("save = %d %v", code, out)
	}
	disk, _ := os.ReadFile(filepath.Join(root, p))
	if string(disk) != edited || out["hash"] != hashOf(disk) {
		t.Errorf("disk not updated")
	}
	if after, _ := os.Stat(filepath.Join(root, p)); after.Mode() != info.Mode() {
		t.Errorf("mode changed %v → %v", info.Mode(), after.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(root, "internal/payment/.*kuli-*")); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}

	// Someone else changed the file: stale base hash is a conflict, disk untouched.
	os.WriteFile(filepath.Join(root, p), []byte("package payment // changed by git pull\n"), 0o644)
	code, out = put(t, ts, saveBody(p, "mine", f.Hash, false), true)
	if code != http.StatusConflict || out["currentHash"] == nil {
		t.Errorf("stale save = %d %v", code, out)
	}
	if disk, _ := os.ReadFile(filepath.Join(root, p)); !strings.Contains(string(disk), "git pull") {
		t.Error("conflicting save overwrote the file")
	}
	// Explicit overwrite.
	if code, _ = put(t, ts, saveBody(p, "package payment\n", "", true), true); code != 200 {
		t.Errorf("forced save = %d", code)
	}

	// Without the CSRF header, or for files outside the knowledge base: refused.
	if code, _ = put(t, ts, saveBody(p, "x", "", true), false); code != http.StatusForbidden {
		t.Errorf("save without header = %d", code)
	}
	for _, bad := range []string{"../escape.go", ".kulidev/kulidev.db", "new.go"} {
		if code, _ = put(t, ts, saveBody(bad, "x", "", true), true); code == 200 {
			t.Errorf("save %q allowed", bad)
		}
	}
}

func TestSaveRefusesSymlinkAndReadOnly(t *testing.T) {
	ts, root := setup(t)
	target := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(target, []byte("outside"), 0o644)
	os.Remove(filepath.Join(root, "web/app.ts"))
	if err := os.Symlink(target, filepath.Join(root, "web/app.ts")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if code, _ := put(t, ts, saveBody("web/app.ts", "pwned", "", true), true); code == 200 {
		t.Error("wrote through a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "outside" {
		t.Error("symlink target modified")
	}

	ro, _ := setupWith(t, Options{Loopback: true, ReadOnly: true})
	if code, _ := put(t, ro, saveBody("go.mod", "x", "", true), true); code != http.StatusForbidden {
		t.Errorf("read-only save = %d", code)
	}
}

func TestDNSRebindingRefused(t *testing.T) {
	ts, _ := setup(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/file?path=go.mod", nil)
	req.Host = "evil.example:7878" // what the browser sends after rebinding evil.example → 127.0.0.1
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("rebinding read = %d, want 403", resp.StatusCode)
	}
	if code := get(t, ts, "/api/overview", nil); code != 200 {
		t.Errorf("localhost read = %d", code)
	}
}
