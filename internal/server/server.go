// Package server serves the KuliDev web UI and its JSON API.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kuli-dev/kulidev/internal/ask"
	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/scan"
	"github.com/kuli-dev/kulidev/internal/store"
)

// maxFileBytes caps what the file viewer will send.
const maxFileBytes = 2 << 20

// Server holds one repository's knowledge base.
type Server struct {
	root    string
	dataDir string
	store   *store.Store
	llm     docs.Chatter // nil when no model is configured
	ui      fs.FS        // built web app; nil serves an API-only notice

	mu    sync.RWMutex
	comps []*docs.Component
	snap  *store.Snapshot
	scanM sync.Mutex
}

// New loads the snapshot and component graph.
func New(root, dataDir string, st *store.Store, llm docs.Chatter, ui fs.FS) (*Server, error) {
	s := &Server{root: root, dataDir: dataDir, store: st, llm: llm, ui: ui}
	return s, s.reload()
}

func (s *Server) reload() error {
	snap, err := s.store.LoadSnapshot()
	if err != nil {
		return err
	}
	comps, _ := docs.BuildComponents(snap)
	s.mu.Lock()
	s.snap, s.comps = snap, comps
	s.mu.Unlock()
	return nil
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("GET /api/graph", s.graph)
	mux.HandleFunc("GET /api/component", s.component)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/file", s.file)
	mux.HandleFunc("GET /api/deps", s.deps)
	mux.HandleFunc("GET /api/broken", s.broken)
	mux.HandleFunc("GET /api/docs", s.docsList)
	mux.HandleFunc("GET /api/docs/{file}", s.docsGet)
	mux.HandleFunc("POST /api/ask", s.ask)
	mux.HandleFunc("POST /api/rescan", s.rescan)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "no such endpoint") })
	if s.ui != nil {
		files := http.FileServerFS(s.ui)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Hashed assets never change; the page itself must always be revalidated,
			// or a browser keeps running an old UI after kuli is upgraded.
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "KuliDev API is running; the web UI was not built into this binary (make web).")
		})
	}
	return securityHeaders(mux)
}

// securityHeaders keeps the UI from being framed or loading foreign scripts, and rejects
// cross-site API writes (a page on another origin must not trigger rescans or LLM calls).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		if r.Method == http.MethodPost {
			if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
				fail(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameHost(origin, host string) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	return o == host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---- overview & graph ------------------------------------------------------

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	head := scan.GitCommit(s.root)
	writeJSON(w, map[string]any{
		"repo":          filepath.Base(s.root),
		"root":          s.root,
		"commit":        st.Run.Commit,
		"head":          head,
		"stale":         head != "" && st.Run.Commit != "" && head != st.Run.Commit,
		"scannedAt":     st.Run.StartedAt,
		"files":         st.Run.Files,
		"parsed":        st.Parsed,
		"languages":     st.Languages,
		"symbols":       st.Symbols,
		"internalEdges": st.InternalEdges,
		"externalMods":  st.ExternalMods,
		"topInternal":   st.TopInternal,
		"topExternal":   st.TopExternal,
		"manifests":     st.Manifests,
		"llm":           s.llm != nil,
	})
}

type compJSON struct {
	Name         string `json:"name"`
	Files        int    `json:"files"`
	Lines        int    `json:"lines"`
	Lang         string `json:"lang"`
	Hub          string `json:"hub"`
	HubImporters int    `json:"hubImporters"`
	HubDoc       string `json:"hubDoc,omitempty"`
}

type edgeJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
	N    int    `json:"n"`
}

func (s *Server) graph(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 40, 1, 300)
	s.mu.RLock()
	comps := s.comps
	s.mu.RUnlock()
	if len(comps) > limit {
		comps = comps[:limit]
	}
	in := map[string]bool{}
	var nodes []compJSON
	for _, c := range comps {
		in[c.Name] = true
		hub := c.Hub()
		nodes = append(nodes, compJSON{c.Name, len(c.Files), c.Lines, c.MainLang(), hub.Path, c.ImportersOf(hub), hub.Doc})
	}
	var edges []edgeJSON
	for _, c := range comps {
		for to, n := range c.DependsOn {
			if in[to] {
				edges = append(edges, edgeJSON{c.Name, to, n})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].N > edges[j].N })
	writeJSON(w, map[string]any{"components": nodes, "edges": edges, "total": len(s.comps)})
}

func (s *Server) component(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.comps {
		if c.Name != name {
			continue
		}
		type fileJSON struct {
			Path      string `json:"path"`
			Lines     int    `json:"lines"`
			Importers int    `json:"importers"`
			Doc       string `json:"doc,omitempty"`
		}
		var files []fileJSON
		for _, f := range c.Files {
			files = append(files, fileJSON{f.Path, f.Lines, c.ImportersOf(f), f.Doc})
		}
		sort.SliceStable(files, func(i, j int) bool { return files[i].Importers > files[j].Importers })
		syms := append([]store.SymbolInfo(nil), c.Symbols...)
		sort.SliceStable(syms, func(i, j int) bool { return (syms[i].Doc != "") && (syms[j].Doc == "") })
		if len(syms) > 60 {
			syms = syms[:60]
		}
		writeJSON(w, map[string]any{
			"name": c.Name, "lines": c.Lines, "lang": c.MainLang(), "langs": c.Langs,
			"files": files, "symbols": syms, "dependsOn": c.DependsOn, "usedBy": c.Importers,
		})
		return
	}
	fail(w, http.StatusNotFound, "unknown component")
}

// ---- search, files, deps ---------------------------------------------------

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, map[string]any{"hits": []any{}})
		return
	}
	hits, err := s.store.Find(q, intParam(r, "limit", 50, 1, 200))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"hits": nonNil(hits)})
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	// Only files recorded by the scan are served: no path tricks reach the disk.
	fi, err := s.store.File(p)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "file not in the knowledge base")
		return
	} else if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	full := filepath.Join(s.root, filepath.FromSlash(fi.Path))
	if rel, err := filepath.Rel(s.root, full); err != nil || strings.HasPrefix(rel, "..") {
		fail(w, http.StatusForbidden, "outside repository")
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		fail(w, http.StatusNotFound, "file no longer on disk (rescan)")
		return
	}
	resp := map[string]any{"path": fi.Path, "lang": fi.Lang, "doc": fi.Doc, "lines": []string{}}
	if info.Size() > maxFileBytes {
		resp["tooLarge"] = true
	} else if b, err := os.ReadFile(full); err == nil {
		resp["lines"] = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	syms, _ := s.store.FileSymbols(fi.Path)
	_, importers, _ := s.store.Deps(fi.Path, true)
	_, imports, _ := s.store.Deps(fi.Path, false)
	resp["symbols"], resp["importers"], resp["imports"] = nonNil(syms), nonNil(importers), nonNil(imports)
	writeJSON(w, resp)
}

func (s *Server) deps(w http.ResponseWriter, r *http.Request) {
	name, hits, err := s.store.Deps(r.URL.Query().Get("target"), r.URL.Query().Get("reverse") == "1")
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, map[string]any{"target": name, "hits": nonNil(hits)})
}

func (s *Server) broken(w http.ResponseWriter, r *http.Request) {
	hits, err := s.store.BrokenImports()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"hits": nonNil(hits)})
}

// ---- docs ------------------------------------------------------------------

func (s *Server) knowledgeDir() string { return filepath.Join(s.dataDir, "knowledge") }

func (s *Server) docsList(w http.ResponseWriter, r *http.Request) {
	fr, err := docs.Status(s.knowledgeDir(), s.root)
	if err != nil {
		writeJSON(w, map[string]any{"docs": []any{}, "hint": "No docs yet — run `kuli docs`."})
		return
	}
	type item struct {
		docs.DocResult
		Status  string   `json:"status"`
		Changed []string `json:"changed,omitempty"`
	}
	var out []item
	for _, f := range fr {
		out = append(out, item{f.Doc, f.Status, f.Changed})
	}
	writeJSON(w, map[string]any{"docs": out})
}

var mdLinkRe = regexp.MustCompile(`\]\(([^)\s]+)\)`)

func (s *Server) docsGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if name != path.Base(name) || !strings.HasSuffix(name, ".md") {
		fail(w, http.StatusBadRequest, "bad document name")
		return
	}
	b, err := os.ReadFile(filepath.Join(s.knowledgeDir(), name))
	if err != nil {
		fail(w, http.StatusNotFound, "no such document")
		return
	}
	md := stripFrontMatter(string(b))
	// Citation links are relative to the knowledge dir; point them at the file viewer.
	md = mdLinkRe.ReplaceAllStringFunc(md, func(m string) string {
		target := mdLinkRe.FindStringSubmatch(m)[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			return m
		}
		file, anchor, _ := strings.Cut(target, "#")
		abs := filepath.Clean(filepath.Join(s.knowledgeDir(), filepath.FromSlash(strings.ReplaceAll(file, "%20", " "))))
		rel, err := filepath.Rel(s.root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return m
		}
		out := "](#/file/" + filepath.ToSlash(rel)
		if anchor != "" {
			out += "#" + anchor
		}
		return out + ")"
	})
	writeJSON(w, map[string]any{"name": name, "markdown": md})
}

func stripFrontMatter(md string) string {
	if !strings.HasPrefix(md, "---\n") {
		return md
	}
	if i := strings.Index(md[4:], "\n---\n"); i >= 0 {
		return md[4+i+5:]
	}
	return md
}

// ---- ask & rescan ----------------------------------------------------------

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	if s.llm == nil {
		fail(w, http.StatusServiceUnavailable, "no model configured: set KULI_LLM_BASE_URL and KULI_LLM_MODEL, then restart kuli serve")
		return
	}
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		fail(w, http.StatusBadRequest, "question required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	a, err := ask.Ask(ctx, s.store, req.Question, ask.Options{Root: s.root, LLM: s.llm, LinkBase: "#/file"})
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"answer": a.Text, "keywords": a.Keywords, "citations": a.Stats.Valid,
		"removed": a.Stats.Removed, "coverage": a.Stats.Coverage(), "hits": a.Hits,
	})
}

func (s *Server) rescan(w http.ResponseWriter, r *http.Request) {
	if !s.scanM.TryLock() {
		fail(w, http.StatusConflict, "a scan is already running")
		return
	}
	defer s.scanM.Unlock()
	res, err := scan.Scan(s.root)
	if err == nil {
		_, err = s.store.Save(res)
	}
	if err == nil {
		err = s.reload()
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("rescanned %s: %d files in %s", s.root, len(res.Files), res.Duration.Round(time.Millisecond))
	writeJSON(w, map[string]any{"files": len(res.Files), "ms": res.Duration.Milliseconds()})
}

func intParam(r *http.Request, name string, def, lo, hi int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return max(lo, min(hi, n))
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
