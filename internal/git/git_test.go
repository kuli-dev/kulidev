package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func repo(t *testing.T) (Repo, string) {
	dir := t.TempDir()
	sh(t, dir, "init", "-q", "-b", "main")
	sh(t, dir, "config", "user.email", "t@t")
	sh(t, dir, "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("bye\n"), 0o644)
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-q", "-m", "init")
	return Repo{dir}, dir
}

func byPath(st *Status) map[string]Change {
	m := map[string]Change{}
	for _, c := range st.Changes {
		m[c.Path] = c
	}
	return m
}

func TestStatusStageDiffCommit(t *testing.T) {
	r, dir := repo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	os.Remove(filepath.Join(dir, "gone.txt"))
	os.MkdirAll(filepath.Join(dir, "new dir"), 0o755)
	os.WriteFile(filepath.Join(dir, "new dir", "b.txt"), []byte("b\n"), 0o644)

	st, err := r.Status(ctx)
	if err != nil || !st.Git || st.Branch != "main" {
		t.Fatalf("status = %+v %v", st, err)
	}
	m := byPath(st)
	if m["a.txt"].Worktree != "M" || m["gone.txt"].Worktree != "D" || m["new dir/b.txt"].Worktree != "?" {
		t.Errorf("changes = %+v", st.Changes)
	}

	orig, mod, bin, err := r.Diff(ctx, "a.txt", false)
	if err != nil || bin || orig != "one\n" || mod != "one\ntwo\n" {
		t.Errorf("diff = %q %q %v %v", orig, mod, bin, err)
	}

	if err := r.Stage(ctx, []string{"a.txt", "gone.txt"}); err != nil {
		t.Fatal(err)
	}
	m = byPath(mustStatus(t, r))
	if m["a.txt"].Index != "M" || m["a.txt"].Unstaged() || m["gone.txt"].Index != "D" {
		t.Errorf("after stage = %+v", m)
	}
	if orig, mod, _, _ := r.Diff(ctx, "a.txt", true); orig != "one\n" || mod != "one\ntwo\n" {
		t.Errorf("staged diff = %q %q", orig, mod)
	}

	if err := r.Unstage(ctx, []string{"gone.txt"}); err != nil {
		t.Fatal(err)
	}
	if byPath(mustStatus(t, r))["gone.txt"].Staged() {
		t.Error("unstage did not unstage")
	}

	msg := "feat: two\n\n$(touch pwned) `rm -rf /` -- --amend"
	hash, err := r.Commit(ctx, msg)
	if err != nil || hash == "" {
		t.Fatalf("commit = %q %v", hash, err)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--format=%B").Output()
	if strings.TrimSpace(string(out)) != msg {
		t.Errorf("message mangled: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("commit message was executed")
	}
	if _, err := r.Commit(ctx, "   "); err == nil {
		t.Error("empty message accepted")
	}
}

func mustStatus(t *testing.T, r Repo) *Status {
	t.Helper()
	st, err := r.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestDiscardIsCareful(t *testing.T) {
	r, dir := repo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("mine\n"), 0o644)

	if err := r.Discard(ctx, []string{"scratch.txt"}, false); err == nil {
		t.Error("untracked file deleted without explicit consent")
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.txt")); err != nil {
		t.Error("untracked file lost")
	}
	if err := r.Discard(ctx, []string{"a.txt"}, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\n" {
		t.Errorf("a.txt = %q", b)
	}
	if err := r.Discard(ctx, []string{"scratch.txt"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.txt")); err == nil {
		t.Error("explicit untracked discard did nothing")
	}
}

func TestPathsAndNonRepo(t *testing.T) {
	r, _ := repo(t)
	for _, bad := range []string{"../x", "/etc/passwd", "-rf", "--amend", ".git/config", ".git", "", "a/../../x"} {
		if _, err := r.CleanPath(bad); err == nil {
			t.Errorf("CleanPath(%q) accepted", bad)
		}
	}
	if err := r.Stage(context.Background(), []string{"--all"}); err == nil {
		t.Error("option passed as a path")
	}
	st, err := Repo{t.TempDir()}.Status(context.Background())
	if err != nil || st.Git {
		t.Errorf("non-repo status = %+v %v", st, err)
	}
}

func TestInitialCommitUnstage(t *testing.T) {
	dir := t.TempDir()
	sh(t, dir, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x\n"), 0o644)
	r := Repo{dir}
	ctx := context.Background()
	if err := r.Stage(ctx, []string{"x.txt"}); err != nil {
		t.Fatal(err)
	}
	if st := mustStatus(t, r); !st.Initial || !st.Changes[0].Staged() {
		t.Fatalf("status = %+v", st)
	}
	if err := r.Unstage(ctx, []string{"x.txt"}); err != nil {
		t.Fatal(err)
	}
	if st := mustStatus(t, r); st.Changes[0].Worktree != "?" {
		t.Errorf("after unstage on initial commit = %+v", st.Changes)
	}
}
