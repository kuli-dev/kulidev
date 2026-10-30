package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kuli-dev/kulidev/internal/auth"
)

func mkRepo(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755) // marks it as a repository for the browser
	os.WriteFile(filepath.Join(root, "main.py"), []byte("def "+name+"_entry():\n    pass\n"), 0o644)
	return root
}

func call(t *testing.T, ts *httptest.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
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

func TestHostOpenSwitchClose(t *testing.T) {
	a, b := mkRepo(t, "alpha"), mkRepo(t, "beta")
	recent := filepath.Join(t.TempDir(), "recent.json")
	h := NewHost(HostOptions{CanOpen: true, RecentFile: recent, Options: Options{Loopback: true}})
	defer h.Close()
	ts := httptest.NewServer(h.Handler())
	defer ts.Close()

	// No folder yet: the launcher answers, workspace APIs say so.
	if _, info := call(t, ts, "GET", "/api/workspace", ""); info["open"] != false || info["canOpen"] != true {
		t.Fatalf("workspace = %v", info)
	}
	if code, out := call(t, ts, "GET", "/api/overview", ""); code != http.StatusConflict || out["noWorkspace"] != true {
		t.Errorf("overview without folder = %d %v", code, out)
	}

	// Browse to the parent of alpha: alpha is listed and marked as a repository.
	_, list := call(t, ts, "GET", "/api/fs/list?path="+filepath.Dir(a), "")
	dirs, _ := list["dirs"].([]any)
	if len(dirs) != 1 || dirs[0].(map[string]any)["isRepo"] != true {
		t.Errorf("fs list = %v", list)
	}

	if code, out := call(t, ts, "POST", "/api/workspace/open", `{"path":`+jsonStr(a)+`}`); code != 200 || out["opening"] == nil {
		t.Fatalf("open alpha = %d %v", code, out)
	}
	if j := waitOpened(t, h); !j.First || j.Total != 1 || j.Done != 1 {
		t.Errorf("first open progress = %+v", j)
	}
	if _, sr := call(t, ts, "GET", "/api/search?q=alpha_entry", ""); !strings.Contains(asJSON(sr), "alpha_entry") {
		t.Errorf("search in alpha = %v", sr)
	}
	// Switch to beta: alpha's code is gone, beta's is there.
	call(t, ts, "POST", "/api/workspace/open", `{"path":`+jsonStr(b)+`}`)
	waitOpened(t, h)
	if _, sr := call(t, ts, "GET", "/api/search?q=beta_entry", ""); !strings.Contains(asJSON(sr), "beta_entry") {
		t.Errorf("search in beta = %v", sr)
	}
	if _, sr := call(t, ts, "GET", "/api/search?q=alpha_entry", ""); strings.Contains(asJSON(sr), "alpha_entry") {
		t.Error("alpha still answering after switching to beta")
	}
	// Recent folders: newest first.
	_, info := call(t, ts, "GET", "/api/workspace", "")
	rec := info["recent"].([]any)
	if len(rec) != 2 || rec[0].(map[string]any)["path"] != b {
		t.Errorf("recent = %v", rec)
	}
	if code, _ := call(t, ts, "POST", "/api/workspace/open", `{"path":"/definitely/not/here"}`); code != 400 {
		t.Errorf("open missing folder = %d", code)
	}
	// Reopening a folder read before is usable at once and only re-reads changes.
	call(t, ts, "POST", "/api/workspace/open", `{"path":`+jsonStr(a)+`}`)
	if j := waitOpened(t, h); j.First || j.Done != 0 {
		t.Errorf("reopen progress = %+v", j)
	}
	call(t, ts, "POST", "/api/workspace/close", "")
	if _, info := call(t, ts, "GET", "/api/workspace", ""); info["open"] != false {
		t.Errorf("after close = %v", info)
	}
}

func TestHostLauncherIsLocalOnly(t *testing.T) {
	// Without the CSRF header, or from a rebinding host name, nothing opens.
	h := NewHost(HostOptions{CanOpen: true, Options: Options{Loopback: true}})
	ts := httptest.NewServer(h.Handler())
	defer ts.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/workspace/open", strings.NewReader(`{"path":"/"}`))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("open without header = %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", ts.URL+"/api/fs/list", nil)
	req.Host = "evil.example"
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("fs list via foreign host = %d", resp.StatusCode)
	}

	// A remote (paired) host never offers folder browsing, even if asked to.
	devices, _ := auth.Open(filepath.Join(t.TempDir(), "d.json"))
	remote := NewHost(HostOptions{CanOpen: true, Options: Options{Auth: devices}})
	rs := httptest.NewServer(remote.Handler())
	defer rs.Close()
	if code, _ := call(t, rs, "GET", "/api/fs/list", ""); code == 200 {
		t.Error("remote host lists folders")
	}
}

func waitOpened(t *testing.T, h *Host) OpenJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if j := h.Job(); j != nil && (j.Phase == "done" || j.Phase == "error") {
			if j.Phase == "error" || !j.Ready {
				t.Fatalf("open failed: %+v", j)
			}
			return *j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("open did not finish")
	return OpenJob{}
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
func asJSON(v any) string     { b, _ := json.Marshal(v); return string(b) }
