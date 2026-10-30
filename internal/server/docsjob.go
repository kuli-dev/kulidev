package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kuli-dev/kulidev/internal/docs"
)

// Generating docs from the Docs pane: one run at a time per folder, in the background,
// with progress and a way to stop it. Same engine as `kuli docs`.

type docsJob struct {
	mu       sync.Mutex
	running  bool
	current  string // document being written
	done     int    // documents finished
	total    int
	model    string
	lines    []string // recent progress lines
	err      string
	started  time.Time
	finished time.Time
	cancel   context.CancelFunc
}

type docsJobView struct {
	Running  bool      `json:"running"`
	Current  string    `json:"current,omitempty"`
	Done     int       `json:"done"`
	Total    int       `json:"total"`
	Model    string    `json:"model,omitempty"`
	Lines    []string  `json:"lines"`
	Error    string    `json:"error,omitempty"`
	Started  time.Time `json:"started,omitempty"`
	Finished time.Time `json:"finished,omitempty"`
}

func (j *docsJob) view() docsJobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return docsJobView{j.running, j.current, j.done, j.total, j.model, append([]string{}, j.lines...), j.err, j.started, j.finished}
}

// Write receives the generator's progress lines ("kuli: writing overview…").
func (j *docsJob) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "kuli: ")
		if line == "" {
			continue
		}
		if id, ok := strings.CutPrefix(line, "writing "); ok {
			if j.current != "" {
				j.done++
			}
			j.current = strings.TrimSuffix(id, "…")
		}
		j.lines = append(j.lines, line)
		if len(j.lines) > 40 {
			j.lines = j.lines[len(j.lines)-40:]
		}
	}
	return len(p), nil
}

func (s *Server) docsJobStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.docs.view())
}

func (s *Server) docsGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Only      []string `json:"only"`
		FactsOnly bool     `json:"factsOnly"` // no model: documents built from the facts alone
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req)
	for _, id := range req.Only {
		if !contains(docs.DocIDs, id) {
			fail(w, http.StatusBadRequest, "unknown document "+id)
			return
		}
	}
	model := s.chatter()
	if req.FactsOnly {
		model = nil
	}
	s.mu.RLock()
	snap := s.snap
	s.mu.RUnlock()

	j := &s.docs
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		fail(w, http.StatusConflict, "docs are already being written")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	total := len(docs.DocIDs)
	if len(req.Only) > 0 {
		total = len(req.Only)
	}
	// Reset field by field: the struct holds the mutex that is locked right now.
	j.running, j.current, j.done, j.total, j.lines, j.err = true, "", 0, total, []string{}, ""
	j.started, j.finished, j.cancel, j.model = time.Now(), time.Time{}, cancel, ""
	if model != nil {
		j.model = model.Model()
	}
	j.mu.Unlock()

	go func() {
		defer cancel()
		_, err := docs.Generate(ctx, snap, docs.Options{OutDir: s.knowledgeDir(), LLM: model, Only: req.Only, Version: s.version, Log: j})
		j.mu.Lock()
		defer j.mu.Unlock()
		j.running, j.finished, j.current = false, time.Now(), ""
		switch {
		case ctx.Err() != nil:
			j.err = "stopped"
		case err != nil:
			j.err = err.Error()
		default:
			j.done = j.total
		}
	}()
	writeJSON(w, s.docs.view())
}

func (s *Server) docsCancel(w http.ResponseWriter, r *http.Request) {
	s.docs.mu.Lock()
	if s.docs.cancel != nil {
		s.docs.cancel()
	}
	s.docs.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
