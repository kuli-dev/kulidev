// Package git runs the user's git for the workbench's source-control view, so hooks,
// identity, .gitignore and config behave exactly as on the command line.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is a git working tree.
type Repo struct{ Root string }

// Change is one changed path.
type Change struct {
	Path     string `json:"path"`
	OrigPath string `json:"origPath,omitempty"` // for renames
	Index    string `json:"index"`              // staged status letter: M A D R C or "."
	Worktree string `json:"worktree"`           // unstaged status letter, "?" untracked
	Conflict bool   `json:"conflict,omitempty"`
}

// Staged reports whether the change has staged content.
func (c Change) Staged() bool { return c.Index != "." && c.Index != "?" }

// Unstaged reports whether the working tree differs from the index.
func (c Change) Unstaged() bool { return c.Worktree != "." }

// Status is the branch state plus changed files.
type Status struct {
	Git      bool     `json:"git"`
	Branch   string   `json:"branch"`
	Upstream string   `json:"upstream,omitempty"`
	Ahead    int      `json:"ahead"`
	Behind   int      `json:"behind"`
	Initial  bool     `json:"initial"` // no commits yet
	Changes  []Change `json:"changes"`
}

var ErrNotRepo = errors.New("not a git repository")

func (r Repo) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.Root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(msg, "not a git repository") {
			return nil, ErrNotRepo
		}
		return out.Bytes(), errors.New(msg)
	}
	return out.Bytes(), nil
}

// CleanPath validates a repo-relative path from a client: relative, inside the tree,
// never an option, never inside .git.
func (r Repo) CleanPath(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || filepath.IsAbs(p) || strings.HasPrefix(p, "-") {
		return "", fmt.Errorf("bad path %q", p)
	}
	c := path.Clean(filepath.ToSlash(p))
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || c == ".git" || strings.HasPrefix(c, ".git/") {
		return "", fmt.Errorf("bad path %q", p)
	}
	return c, nil
}

func (r Repo) cleanAll(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("no paths given")
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		c, err := r.CleanPath(p)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Status parses `git status --porcelain=v2`.
func (r Repo) Status(ctx context.Context) (*Status, error) {
	b, err := r.run(ctx, nil, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if errors.Is(err, ErrNotRepo) {
		return &Status{Git: false}, nil
	} else if err != nil {
		return nil, err
	}
	st := &Status{Git: true, Changes: []Change{}}
	recs := strings.Split(string(b), "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		switch {
		case strings.HasPrefix(rec, "# branch.head "):
			st.Branch = strings.TrimPrefix(rec, "# branch.head ")
		case strings.HasPrefix(rec, "# branch.oid (initial)"):
			st.Initial = true
		case strings.HasPrefix(rec, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(rec, "# branch.upstream ")
		case strings.HasPrefix(rec, "# branch.ab "):
			f := strings.Fields(strings.TrimPrefix(rec, "# branch.ab "))
			if len(f) == 2 {
				st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[0], "+"))
				st.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[1], "-"))
			}
		case strings.HasPrefix(rec, "1 "): // ordinary: 1 XY sub mH mI mW hH hI path
			f := strings.SplitN(rec, " ", 9)
			if len(f) == 9 {
				st.Changes = append(st.Changes, Change{Path: f[8], Index: f[1][:1], Worktree: f[1][1:]})
			}
		case strings.HasPrefix(rec, "2 "): // rename/copy: … Xscore path, then origPath record
			f := strings.SplitN(rec, " ", 10)
			if len(f) == 10 {
				c := Change{Path: f[9], Index: f[1][:1], Worktree: f[1][1:]}
				if i+1 < len(recs) {
					c.OrigPath = recs[i+1]
					i++
				}
				st.Changes = append(st.Changes, c)
			}
		case strings.HasPrefix(rec, "u "): // unmerged
			f := strings.SplitN(rec, " ", 11)
			if len(f) == 11 {
				st.Changes = append(st.Changes, Change{Path: f[10], Index: "U", Worktree: "U", Conflict: true})
			}
		case strings.HasPrefix(rec, "? "):
			st.Changes = append(st.Changes, Change{Path: rec[2:], Index: "?", Worktree: "?"})
		}
	}
	return st, nil
}

// Diff returns the two sides to compare for a path: index vs working tree, or
// (staged) HEAD vs index. Missing sides are empty (added or deleted files).
func (r Repo) Diff(ctx context.Context, p string, staged bool) (orig, mod string, binary bool, err error) {
	if p, err = r.CleanPath(p); err != nil {
		return "", "", false, err
	}
	show := func(rev string) string {
		b, err := r.run(ctx, nil, "show", rev+":"+p)
		if err != nil {
			return ""
		}
		return string(b)
	}
	if staged {
		orig, mod = show("HEAD"), show("")
	} else {
		orig = show("") // index
		if b, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(p))); err == nil {
			mod = string(b)
		}
	}
	binary = strings.ContainsRune(orig, 0) || strings.ContainsRune(mod, 0)
	if binary {
		orig, mod = "", ""
	}
	return orig, mod, binary, nil
}

// Stage adds paths to the index (including deletions).
func (r Repo) Stage(ctx context.Context, paths []string) error {
	ps, err := r.cleanAll(paths)
	if err != nil {
		return err
	}
	_, err = r.run(ctx, nil, append([]string{"add", "-A", "--"}, ps...)...)
	return err
}

// Unstage removes paths from the index, keeping working-tree changes.
func (r Repo) Unstage(ctx context.Context, paths []string) error {
	ps, err := r.cleanAll(paths)
	if err != nil {
		return err
	}
	st, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if st.Initial { // no HEAD to restore from
		_, err = r.run(ctx, nil, append([]string{"rm", "--cached", "-r", "-q", "--"}, ps...)...)
		return err
	}
	_, err = r.run(ctx, nil, append([]string{"restore", "--staged", "--"}, ps...)...)
	return err
}

// Discard throws away working-tree changes to tracked paths. Untracked files are only
// deleted when untracked is true — that cannot be undone.
func (r Repo) Discard(ctx context.Context, paths []string, untracked bool) error {
	ps, err := r.cleanAll(paths)
	if err != nil {
		return err
	}
	st, err := r.Status(ctx)
	if err != nil {
		return err
	}
	isUntracked := map[string]bool{}
	for _, c := range st.Changes {
		if c.Worktree == "?" {
			isUntracked[c.Path] = true
		}
	}
	var tracked, fresh []string
	for _, p := range ps {
		if isUntracked[p] {
			fresh = append(fresh, p)
		} else {
			tracked = append(tracked, p)
		}
	}
	if len(fresh) > 0 && !untracked {
		return fmt.Errorf("%s is untracked: discarding deletes it permanently", fresh[0])
	}
	if len(tracked) > 0 {
		if _, err := r.run(ctx, nil, append([]string{"restore", "--worktree", "--"}, tracked...)...); err != nil {
			return err
		}
	}
	if len(fresh) > 0 {
		if _, err := r.run(ctx, nil, append([]string{"clean", "-f", "-q", "--"}, fresh...)...); err != nil {
			return err
		}
	}
	return nil
}

// Commit records the index with message (from stdin, so any text is safe). Hooks run.
func (r Repo) Commit(ctx context.Context, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("write a commit message first")
	}
	if _, err := r.run(ctx, []byte(message), "commit", "-q", "-F", "-"); err != nil {
		return "", err
	}
	b, err := r.run(ctx, nil, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(string(b)), err
}
