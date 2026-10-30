package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kuli-dev/kulidev/internal/auth"
	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/scan"
	"github.com/kuli-dev/kulidev/internal/store"

	"github.com/coder/websocket"
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

func TestTerminals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("drives /bin/sh; ConPTY is covered in internal/term")
	}
	t.Setenv("SHELL", "/bin/sh")
	ts, _ := setupWith(t, Options{Loopback: true, Terminals: true})
	post := func(path string, header bool) (int, map[string]any) {
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(`{"cols":80,"rows":24}`))
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
	if code, _ := post("/api/terminals", false); code != http.StatusForbidden {
		t.Errorf("start without header = %d", code)
	}
	code, info := post("/api/terminals", true)
	if code != 200 || info["id"] == nil {
		t.Fatalf("start = %d %v", code, info)
	}
	id := info["id"].(string)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/terminals/" + id + "/ws"

	// A page on another origin cannot attach to the shell.
	if _, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}}); err == nil {
		t.Error("cross-origin websocket accepted")
	}

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.Write(ctx, websocket.MessageText, []byte(`{"type":"input","data":"echo from-$((6*7))\n"}`))
	var got strings.Builder
	for !strings.Contains(got.String(), "from-42") {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, got.String())
		}
		got.Write(b)
	}

	// Input without a socket (Pulse answering an agent) reaches the shell.
	in, _ := http.NewRequest("POST", ts.URL+"/api/terminals/"+id+"/input", strings.NewReader(`{"data":"echo via-$((40+1))-input\r"}`))
	in.Header.Set("X-Kuli-Request", "1")
	if resp, err := http.DefaultClient.Do(in); err != nil || resp.StatusCode != 200 {
		t.Fatalf("input = %v %v", resp, err)
	}
	for !strings.Contains(got.String(), "via-41-input") {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read after input: %v (got %q)", err, got.String())
		}
		got.Write(b)
	}

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/terminals/"+id, nil)
	req.Header.Set("X-Kuli-Request", "1")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("kill = %d", resp.StatusCode)
	}

	// Read-only workspaces never get a shell.
	ro, _ := setupWith(t, Options{Loopback: true, ReadOnly: true, Terminals: true})
	req, _ = http.NewRequest("POST", ro.URL+"/api/terminals", nil)
	req.Header.Set("X-Kuli-Request", "1")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("read-only terminal = %d", resp.StatusCode)
	}
}

func TestPairingRequired(t *testing.T) {
	devices, _ := auth.Open(filepath.Join(t.TempDir(), "devices.json"))
	ts, _ := setupWith(t, Options{Auth: devices})
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	do := func(method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("X-Kuli-Request", "1")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// Unpaired: the code is invisible, only the auth endpoints answer.
	if code, out := do("GET", "/api/file?path=go.mod", ""); code != 401 || out["auth"] != true {
		t.Errorf("unpaired file read = %d %v", code, out)
	}
	if _, out := do("GET", "/api/auth/state", ""); out["required"] != true || out["paired"] != false {
		t.Errorf("state = %v", out)
	}
	if code, _ := do("POST", "/api/pair", `{"code":"AAAA-AAAA","name":"x"}`); code != 403 {
		t.Errorf("wrong code = %d", code)
	}

	pairCode := devices.NewCode()
	code, out := do("POST", "/api/pair", `{"code":"`+pairCode+`","name":"Test phone"}`)
	if code != 200 {
		t.Fatalf("pair = %d %v", code, out)
	}
	u, _ := url.Parse(ts.URL)
	var session *http.Cookie
	for _, c := range jar.Cookies(u) {
		if c.Name == "kuli_session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie")
	}
	if code, _ := do("GET", "/api/file?path=go.mod", ""); code != 200 {
		t.Errorf("paired file read = %d", code)
	}

	// A paired device can invite another, and list/revoke devices.
	if code, out := do("POST", "/api/devices/code", ""); code != 200 || len(out["code"].(string)) != 9 {
		t.Errorf("new code = %d %v", code, out)
	}
	_, list := do("GET", "/api/devices", "")
	devs := list["devices"].([]any)
	if len(devs) != 1 || list["current"] != devs[0].(map[string]any)["id"] || devs[0].(map[string]any)["tokenHash"] != nil && devs[0].(map[string]any)["tokenHash"] != "" {
		t.Errorf("devices = %v", list)
	}

	// Logging out revokes this device: the old cookie no longer works.
	do("POST", "/api/logout", "")
	req, _ := http.NewRequest("GET", ts.URL+"/api/overview", nil)
	req.AddCookie(session)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("revoked session = %d", resp.StatusCode)
	}
}

func TestTunnelHostNeedsPairing(t *testing.T) {
	devices, _ := auth.Open(filepath.Join(t.TempDir(), "devices.json"))
	ts, _ := setupWith(t, Options{Loopback: true, Auth: devices, TLS: true, AllowHosts: []string{"abc.ngrok-free.app"}})
	req := func(host string) int {
		r, _ := http.NewRequest("GET", ts.URL+"/api/overview", nil)
		r.Host = host
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := req("abc.ngrok-free.app"); code != 401 {
		t.Errorf("tunnel host, unpaired = %d, want 401 (allowed host, still needs pairing)", code)
	}
	if code := req("evil.ngrok-free.app"); code != 403 {
		t.Errorf("other host = %d, want 403", code)
	}
}

func TestGitAPI(t *testing.T) {
	ts, root := setup(t)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("add", "-A", "--", ".", ":!.kulidev")
	run("commit", "-q", "-m", "init")
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.23\n"), 0o644)

	var st struct {
		Status struct {
			Git     bool
			Branch  string
			Changes []struct{ Path, Index, Worktree string }
		}
		Writable bool
	}
	get(t, ts, "/api/git/status", &st)
	found := false
	for _, c := range st.Status.Changes {
		if c.Path == "go.mod" && c.Worktree == "M" {
			found = true
		}
	}
	if !st.Status.Git || st.Status.Branch != "main" || !found || !st.Writable {
		t.Fatalf("status = %+v", st)
	}
	var d struct{ Original, Modified string }
	get(t, ts, "/api/git/diff?path=go.mod", &d)
	if !strings.Contains(d.Modified, "go 1.23") || strings.Contains(d.Original, "go 1.23") {
		t.Errorf("diff = %+v", d)
	}
	if code := get(t, ts, "/api/git/diff?path=../../etc/passwd", nil); code != 400 {
		t.Errorf("traversal diff = %d", code)
	}

	post := func(op, body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/git/"+op, strings.NewReader(body))
		req.Header.Set("X-Kuli-Request", "1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, out := post("stage", `{"paths":["go.mod"]}`); code != 200 {
		t.Fatalf("stage = %d %v", code, out)
	}
	if code, out := post("commit", `{"message":"bump go"}`); code != 200 || out["commit"] == "" {
		t.Fatalf("commit = %d %v", code, out)
	}
	if code, _ := post("stage", `{"paths":["--all"]}`); code != 400 {
		t.Errorf("option as path = %d", code)
	}
	if code, out := post("push", `{}`); code != 400 || !strings.Contains(fmt.Sprint(out["error"]), "no remote") {
		t.Errorf("push without remote = %d %v", code, out)
	}
	if code, _ := post("rebase", `{}`); code != 404 {
		t.Errorf("unknown op = %d", code)
	}

	ro, _ := setupWith(t, Options{Loopback: true, ReadOnly: true})
	req, _ := http.NewRequest("POST", ro.URL+"/api/git/commit", strings.NewReader(`{"message":"x"}`))
	req.Header.Set("X-Kuli-Request", "1")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("read-only commit = %d", resp.StatusCode)
	}
}

func TestRawImagesAndSymlinks(t *testing.T) {
	ts, root := setup(t)
	png := []byte("\x89PNG\r\n\x1a\nfake")
	os.WriteFile(filepath.Join(root, "web/logo.png"), png, 0o644)
	os.WriteFile(filepath.Join(root, "web/bad.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0o644)
	linked := runtime.GOOS != "windows" && os.Symlink(filepath.Join(filepath.Dir(root), "secret.txt"), filepath.Join(root, "web/leak.png")) == nil
	req, _ := http.NewRequest("POST", ts.URL+"/api/rescan", nil)
	req.Header.Set("X-Kuli-Request", "1")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}

	resp, err := http.Get(ts.URL + "/api/raw?path=web/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(b, png) {
		t.Errorf("png = %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), b)
	}
	resp, _ = http.Get(ts.URL + "/api/raw?path=web/bad.svg")
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("svg not sandboxed: %v", resp.Header)
	}
	if code := get(t, ts, "/api/raw?path=web/app.ts", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("non-image = %d", code)
	}
	if code := get(t, ts, "/api/raw?path=../secret.png", nil); code != http.StatusNotFound {
		t.Errorf("traversal = %d", code)
	}
	if linked {
		if code := get(t, ts, "/api/raw?path=web/leak.png", nil); code == 200 {
			t.Errorf("symlink out of the folder was served")
		}
		// Git lists symlinks, so the scan may record one: the path check must refuse it.
		srv := &Server{root: root}
		if _, ok := srv.inside("web/leak.png"); ok {
			t.Error("inside() accepted a link that leaves the folder")
		}
		if _, ok := srv.inside("web/logo.png"); !ok {
			t.Error("inside() refused a plain file")
		}
	}
}
