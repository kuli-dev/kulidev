package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/kuli-dev/kulidev/internal/model"
	"github.com/kuli-dev/kulidev/internal/parse"
)

// Stamp is what the last scan recorded about a file.
type Stamp struct {
	Size    int64
	MtimeNS int64
	SHA256  string
}

// Touch updates a file's stamp without re-parsing (its content is unchanged).
type Touch struct {
	Path    string
	MtimeNS int64
}

// Delta is the difference between the working tree and the last scan.
type Delta struct {
	Root      string
	Commit    string
	Started   time.Time
	Duration  time.Duration
	Total     int                // files in the tree now
	Changed   []model.FileResult // new or modified files, parsed
	Added     map[string]bool    // which of Changed are new paths
	Touched   []Touch            // mtime moved but content identical
	Deleted   []string
	Manifests map[string]string // path → manifest kind ("" = not a manifest) for Changed

	resolver *resolver
}

// Resolve resolves an import against the tree as it is now.
func (d *Delta) Resolve(lang, from, spec string, maybeSymbol bool) Target {
	return d.resolver.resolve(lang, from, spec, maybeSymbol)
}

// Incremental compares the tree with prev and parses only what changed.
func Incremental(root string, prev map[string]Stamp) (*Delta, error) {
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
	d := &Delta{Root: root, Commit: GitCommit(root), Started: start, Total: len(paths),
		Added: map[string]bool{}, Manifests: map[string]string{}}

	present := make(map[string]bool, len(paths))
	var todo []string
	for _, p := range paths {
		present[p] = true
		old, known := prev[p]
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		if known && info.Size() == old.Size && info.ModTime().UnixNano() == old.MtimeNS {
			continue // unchanged: no read, no parse
		}
		if known && info.Size() == old.Size {
			// Same size, new mtime (checkout, touch, editor save without edits): compare content.
			if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p))); err == nil {
				sum := sha256.Sum256(b)
				if hex.EncodeToString(sum[:]) == old.SHA256 {
					d.Touched = append(d.Touched, Touch{p, info.ModTime().UnixNano()})
					continue
				}
			}
		}
		if !known {
			d.Added[p] = true
		}
		todo = append(todo, p)
	}
	for p := range prev {
		if !present[p] {
			d.Deleted = append(d.Deleted, p)
		}
	}
	sort.Strings(d.Deleted)

	d.Changed = make([]model.FileResult, len(todo))
	manifests := make([]string, len(todo))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex
	for w := 0; w < min(runtime.NumCPU(), max(1, len(todo))); w++ {
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
				d.Changed[i], manifests[i] = analyzeFile(p, root, todo[i])
			}
		}()
	}
	for i := range todo {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	for i, p := range todo {
		d.Manifests[p] = manifests[i]
	}
	d.resolver = newResolver(paths, func(rel string) []byte {
		b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		return b
	})
	d.Duration = time.Since(start)
	return d, nil
}
