// Package scan walks a repository, parses its source files and resolves imports.
package scan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/kulidev/kulidev/internal/model"
	"github.com/kulidev/kulidev/internal/parse"
)

// MaxParseBytes is the largest file that gets parsed; bigger files are only counted.
const MaxParseBytes = 1 << 20

// ImportEdge is a resolved import with its source evidence.
type ImportEdge struct {
	From   string // importing file
	Line   int
	Spec   string
	Target Target
}

// Result is the outcome of one scan.
type Result struct {
	Root      string
	Commit    string
	Started   time.Time
	Duration  time.Duration
	Files     []model.FileResult
	Manifests []model.Manifest
	Imports   []ImportEdge
}

// Scan analyses the repository at root.
func Scan(root string) (*Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	paths, err := listFiles(root)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	res := &Result{Root: root, Commit: gitCommit(root), Started: start}
	res.Files = make([]model.FileResult, len(paths))

	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := parse.NewParser()
			if err != nil {
				mu.Lock()
				firstErr = err
				mu.Unlock()
				for range jobs {
				}
				return
			}
			defer p.Close()
			for i := range jobs {
				fr, man := analyzeFile(p, root, paths[i])
				res.Files[i] = fr
				if man != "" {
					mu.Lock()
					res.Manifests = append(res.Manifests, model.Manifest{Path: paths[i], Kind: man})
					mu.Unlock()
				}
			}
		}()
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(res.Manifests, func(i, j int) bool { return res.Manifests[i].Path < res.Manifests[j].Path })

	r := newResolver(paths, func(rel string) []byte {
		b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		return b
	})
	for _, f := range res.Files {
		for _, imp := range f.Imports {
			res.Imports = append(res.Imports, ImportEdge{
				From: f.Path, Line: imp.Line, Spec: imp.Spec,
				Target: r.resolve(f.Lang, f.Path, imp.Spec),
			})
		}
	}
	res.Duration = time.Since(start)
	return res, nil
}

func analyzeFile(p *parse.Parser, root, rel string) (model.FileResult, string) {
	fr := model.FileResult{Path: rel}
	li := detectLang(rel)
	fr.Lang = li.name

	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		fr.Err = err.Error()
		return fr, ""
	}
	fr.Size = int64(len(b))
	sum := sha256.Sum256(b)
	fr.SHA256 = hex.EncodeToString(sum[:])

	head := b
	if len(head) > 8192 {
		head = head[:8192]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		fr.Lang = "" // binary
		return fr, ""
	}
	fr.Lines = bytes.Count(b, []byte{'\n'})
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fr.Lines++
	}
	man := manifestKind(rel, head)

	if li.grammar == "" || len(b) > MaxParseBytes {
		return fr, man
	}
	syms, imps, err := p.Parse(li.grammar, b)
	if err != nil {
		fr.Err = err.Error()
		return fr, man
	}
	fr.Parsed, fr.Symbols, fr.Imports = true, syms, imps
	return fr, man
}
