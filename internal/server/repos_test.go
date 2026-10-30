package server

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuli-dev/kulidev/internal/store"
)

// A folder holding two repositories: git calls are routed per repository.
func TestGitAcrossRepos(t *testing.T) {
	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	for _, r := range []string{"api", "group/web"} {
		dir := filepath.Join(root, filepath.FromSlash(r))
		os.MkdirAll(dir, 0o755)
		git(dir, "init", "-q", "-b", "main")
		git(dir, "config", "core.autocrlf", "false")
		os.WriteFile(filepath.Join(dir, "main.py"), []byte("x = 1\n"), 0o644)
		git(dir, "add", ".")
		git(dir, "commit", "-q", "-m", "init")
	}
	os.WriteFile(filepath.Join(root, "group/web/main.py"), []byte("x = 2\n"), 0o644)

	st, err := store.Open(filepath.Join(root, ".kulidev"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Refresh(root, true)
	srv, err := New(root, filepath.Join(root, ".kulidev"), st, nil, nil, Options{Loopback: true})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var status struct {
		Repo   string `json:"repo"`
		Status struct {
			Changes []struct{ Path string } `json:"changes"`
		} `json:"status"`
		Repos []repoSummary `json:"repos"`
	}
	get(t, ts, "/api/git/status?repo=group/web", &status)
	if len(status.Repos) != 2 || status.Repos[0].Path != "api" || status.Repos[1].Changes != 1 || status.Repos[0].Changes != 0 {
		t.Fatalf("repos = %+v", status.Repos)
	}
	// Paths are relative to the folder, so the editor can open them.
	if len(status.Status.Changes) != 1 || status.Status.Changes[0].Path != "group/web/main.py" {
		t.Fatalf("changes = %+v", status.Status.Changes)
	}
	var diff struct{ Original, Modified string }
	get(t, ts, "/api/git/diff?path=group/web/main.py", &diff)
	if diff.Original != "x = 1\n" || diff.Modified != "x = 2\n" {
		t.Errorf("diff = %+v", diff)
	}
	if code, _ := call(t, ts, "POST", "/api/git/stage", `{"paths":["group/web/main.py"]}`); code != 200 {
		t.Fatalf("stage = %d", code)
	}
	if code, out := call(t, ts, "POST", "/api/git/commit", `{"repo":"group/web","message":"two"}`); code != 200 {
		t.Fatalf("commit = %d %v", code, out)
	}
	if log := git(filepath.Join(root, "group/web"), "log", "--oneline"); !strings.Contains(log, "two") {
		t.Errorf("commit landed elsewhere: %s", log)
	}
	if log := git(filepath.Join(root, "api"), "log", "--oneline"); strings.Contains(log, "two") {
		t.Error("commit went to the wrong repository")
	}
	// Escapes and unknown repositories are refused.
	if code, _ := call(t, ts, "POST", "/api/git/stage", `{"paths":["api/../group/web/main.py"]}`); code != 400 {
		t.Errorf("stage via .. = %d", code)
	}
	if code, _ := call(t, ts, "POST", "/api/git/commit", `{"repo":"nope","message":"x"}`); code != 400 {
		t.Errorf("commit to unknown repo = %d", code)
	}
}
