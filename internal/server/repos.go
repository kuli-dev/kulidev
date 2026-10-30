package server

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kuli-dev/kulidev/internal/store"
)

// repoRef is a git repository inside the open folder.
type repoRef struct {
	Path string // relative to the folder; "" for the folder itself
	Name string
}

// maxRepoDepth bounds how deep repositories are looked for inside a folder.
const maxRepoDepth = 8

// findRepos lists the repositories in root: root itself when it is one, otherwise the
// outermost repositories among the scanned files' folders.
func findRepos(root string, snap *store.Snapshot) []repoRef {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return []repoRef{{Path: "", Name: filepath.Base(root)}}
	}
	dirs := map[string]bool{}
	for _, f := range snap.Files {
		parts := strings.Split(f.Path, "/")
		for d := 1; d < len(parts) && d <= maxRepoDepth; d++ {
			dirs[strings.Join(parts[:d], "/")] = true
		}
	}
	cands := make([]string, 0, len(dirs))
	for d := range dirs {
		cands = append(cands, d)
	}
	sort.Slice(cands, func(i, j int) bool {
		di, dj := strings.Count(cands[i], "/"), strings.Count(cands[j], "/")
		if di != dj {
			return di < dj
		}
		return strings.ToLower(cands[i]) < strings.ToLower(cands[j])
	})
	var out []repoRef
	taken := map[string]bool{}
	for _, d := range cands {
		inside := false
		for p := path.Dir(d); p != "." && p != "/"; p = path.Dir(p) {
			if taken[p] {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d), ".git")); err == nil {
			taken[d] = true
			out = append(out, repoRef{Path: d, Name: path.Base(d)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	return out
}

func (s *Server) repoList() []repoRef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repos
}
