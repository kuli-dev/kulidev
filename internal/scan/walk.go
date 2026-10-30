package scan

import (
	"bytes"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Directories never worth scanning, even when they are not git-ignored.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".next": true, "dist": true, "build": true, "target": true,
	".kulidev": true, ".idea": true, ".vscode": true, "coverage": true, ".tox": true,
	".mypy_cache": true, ".pytest_cache": true, ".turbo": true,
}

func skipped(rel string) bool {
	for _, part := range strings.Split(path.Dir(rel), "/") {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

// listFiles returns slash-separated paths relative to root. Inside a git work tree it
// honours .gitignore via `git ls-files`; otherwise it walks the directory.
func listFiles(root string) ([]string, error) {
	if files, ok := gitFiles(root); ok {
		return files, nil
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func gitFiles(root string) ([]string, bool) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	b, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var out []string
	for _, f := range bytes.Split(b, []byte{0}) {
		if len(f) == 0 {
			continue
		}
		rel := string(f)
		if skipped(rel) {
			continue
		}
		out = append(out, rel)
	}
	return out, true
}

// GitCommit returns HEAD's commit hash, or "" outside a git repo.
func GitCommit(root string) string {
	b, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
