// Package server serves the KuliDev web UI and its JSON API.
package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
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

	"github.com/kuli-dev/kulidev/internal/agents"
	"github.com/kuli-dev/kulidev/internal/ask"
	"github.com/kuli-dev/kulidev/internal/auth"
	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/git"
	"github.com/kuli-dev/kulidev/internal/scan"
	"github.com/kuli-dev/kulidev/internal/store"
	"github.com/kuli-dev/kulidev/internal/term"

	"github.com/coder/websocket"
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

	readOnly  bool
	loopback  bool            // bound to localhost: only localhost Host headers are accepted
	allowHost map[string]bool // extra Host names accepted, e.g. a tunnel's public name
	terms     *term.Manager   // nil when terminals are disabled
	auth      *auth.Store     // nil when no pairing is required (local use)
	secure    bool            // served over TLS: cookies get the Secure flag
	rescanMu  sync.Mutex
	rescanDue *time.Timer
}

// Options tunes a server.
type Options struct {
	ReadOnly  bool // refuse file writes (and terminals)
	Loopback  bool // listening on a loopback address (enables Host-header checks)
	Terminals bool // allow shell sessions
	// Auth, when set, requires every API call to come from a paired device.
	Auth *auth.Store
	TLS  bool // cookies get the Secure flag (TLS here or at a proxy in front)
	// AllowHosts are public names a tunnel (ngrok, Cloudflare Tunnel) forwards as Host.
	AllowHosts []string
}

// New loads the snapshot and component graph.
func New(root, dataDir string, st *store.Store, llm docs.Chatter, ui fs.FS, opts Options) (*Server, error) {
	s := &Server{root: root, dataDir: dataDir, store: st, llm: llm, ui: ui, readOnly: opts.ReadOnly, loopback: opts.Loopback}
	s.auth, s.secure = opts.Auth, opts.TLS
	s.allowHost = map[string]bool{}
	for _, h := range opts.AllowHosts {
		s.allowHost[strings.ToLower(h)] = true
	}
	if opts.Terminals && !opts.ReadOnly {
		s.terms = term.NewManager(root)
	}
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
	mux.HandleFunc("GET /api/tree", s.tree)
	mux.HandleFunc("GET /api/definition", s.definition)
	mux.HandleFunc("GET /api/file", s.file)
	mux.HandleFunc("PUT /api/file", s.saveFile)
	mux.HandleFunc("GET /api/deps", s.deps)
	mux.HandleFunc("GET /api/broken", s.broken)
	mux.HandleFunc("GET /api/docs", s.docsList)
	mux.HandleFunc("GET /api/docs/{file}", s.docsGet)
	mux.HandleFunc("POST /api/ask", s.ask)
	mux.HandleFunc("POST /api/rescan", s.rescan)
	mux.HandleFunc("GET /api/terminals", s.termList)
	mux.HandleFunc("POST /api/terminals", s.termStart)
	mux.HandleFunc("DELETE /api/terminals/{id}", s.termKill)
	mux.HandleFunc("GET /api/terminals/{id}/ws", s.termSocket)
	mux.HandleFunc("POST /api/terminals/{id}/input", s.termInput)
	mux.HandleFunc("GET /api/auth/state", s.authState)
	mux.HandleFunc("POST /api/pair", s.pair)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/devices", s.deviceList)
	mux.HandleFunc("POST /api/devices/code", s.deviceCode)
	mux.HandleFunc("DELETE /api/devices/{id}", s.deviceRevoke)
	mux.HandleFunc("GET /api/git/status", s.gitStatus)
	mux.HandleFunc("GET /api/git/diff", s.gitDiff)
	mux.HandleFunc("GET /api/git/branches", s.gitBranches)
	mux.HandleFunc("GET /api/git/lines", s.gitLines)
	mux.HandleFunc("GET /api/git/patch", s.gitPatch)
	mux.HandleFunc("POST /api/git/{op}", s.gitWrite)
	mux.HandleFunc("GET /api/agents", s.agentList)
	mux.HandleFunc("POST /api/agents", s.agentStart)
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
	return s.guard(mux)
}

// guard applies browser hardening and request checks to everything:
//   - Host must be localhost when bound to loopback (defeats DNS rebinding, where a
//     hostile site resolves its own name to 127.0.0.1 to read the code);
//   - writes must be same-origin and carry X-Kuli-Request, a custom header that other
//     origins cannot send without a CORS preflight we never grant.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:")
		if s.loopback && !loopbackHost(r.Host) && !s.allowHost[strings.ToLower(hostOnly(r.Host))] {
			fail(w, http.StatusForbidden, "unexpected Host header")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
				fail(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			if r.Header.Get("X-Kuli-Request") != "1" {
				fail(w, http.StatusForbidden, "missing X-Kuli-Request header")
				return
			}
		}
		if s.auth != nil && strings.HasPrefix(r.URL.Path, "/api/") && !publicAPI[r.URL.Path] {
			c, err := r.Cookie(sessionCookie)
			if err != nil {
				authRequired(w)
				return
			}
			d, ok := s.auth.Check(c.Value)
			if !ok {
				authRequired(w)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), deviceKey{}, d))
		}
		next.ServeHTTP(w, r)
	})
}

const sessionCookie = "kuli_session"

// publicAPI lists endpoints an unpaired device may call.
var publicAPI = map[string]bool{"/api/auth/state": true, "/api/pair": true}

type deviceKey struct{}

func authRequired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{"error": "pair this device first", "auth": true})
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
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
		"model":         modelName(s.llm),
		"readOnly":      s.readOnly,
		"terminals":     s.terms != nil,
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

// tree lists every scanned file (path, language, lines) for the explorer and quick open.
func (s *Server) tree(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	files := s.snap.Files
	s.mu.RUnlock()
	type entry struct {
		P string `json:"p"`
		L string `json:"l,omitempty"`
		N int    `json:"n"`
	}
	out := make([]entry, 0, len(files))
	for _, f := range files {
		out = append(out, entry{f.Path, f.Lang, f.Lines})
	}
	writeJSON(w, map[string]any{"files": out})
}

func (s *Server) definition(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" || len(name) > 200 {
		fail(w, http.StatusBadRequest, "name required")
		return
	}
	hits, err := s.store.Definitions(name, 10)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
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
		resp["content"] = string(b)
		resp["hash"] = hashOf(b)
	}
	resp["writable"] = !s.readOnly
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
	files, d, err := s.rescanNow(false)
	if errors.Is(err, errScanBusy) {
		fail(w, http.StatusConflict, err.Error())
		return
	} else if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"files": files, "ms": d.Milliseconds()})
}

var errScanBusy = errors.New("a scan is already running")

// rescanNow rebuilds the knowledge base; wait=false fails fast when one is running.
func (s *Server) rescanNow(wait bool) (int, time.Duration, error) {
	if wait {
		s.scanM.Lock()
	} else if !s.scanM.TryLock() {
		return 0, 0, errScanBusy
	}
	defer s.scanM.Unlock()
	sum, err := s.refreshLocked(nil)
	if err != nil {
		return 0, 0, err
	}
	return sum.Files, sum.Duration, nil
}

// refresh brings the knowledge base up to date, reporting progress.
func (s *Server) refresh(prog scan.Progress) (store.Summary, error) {
	s.scanM.Lock()
	defer s.scanM.Unlock()
	return s.refreshLocked(prog)
}

func (s *Server) refreshLocked(prog scan.Progress) (store.Summary, error) {
	sum, err := s.store.RefreshProgress(s.root, false, prog)
	if err == nil {
		err = s.reload()
	}
	if err != nil {
		return store.Summary{}, err
	}
	log.Printf("refreshed %s: %s", s.root, sum)
	return sum, nil
}

// scheduleRescan refreshes the knowledge base shortly after edits, coalescing bursts
// of saves into one scan.
func (s *Server) scheduleRescan() {
	s.rescanMu.Lock()
	defer s.rescanMu.Unlock()
	if s.rescanDue != nil {
		s.rescanDue.Stop()
	}
	s.rescanDue = time.AfterFunc(1500*time.Millisecond, func() {
		if _, _, err := s.rescanNow(true); err != nil {
			log.Printf("background rescan: %v", err)
		}
	})
}

const maxSaveBytes = 5 << 20

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// saveFile writes one file with optimistic concurrency: the client sends the hash of
// the version it edited, and a mismatch with the disk (someone else changed it) is a
// 409 carrying the current hash, never a silent overwrite.
func (s *Server) saveFile(w http.ResponseWriter, r *http.Request) {
	if s.readOnly {
		fail(w, http.StatusForbidden, "this workspace is read-only (kuli serve --read-only)")
		return
	}
	var req struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		BaseHash string `json:"baseHash"`
		Force    bool   `json:"force"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSaveBytes+64<<10)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "bad request body")
		return
	}
	if len(req.Content) > maxSaveBytes {
		fail(w, http.StatusRequestEntityTooLarge, "file too large to save from the editor")
		return
	}
	fi, err := s.store.File(req.Path)
	if err != nil {
		fail(w, http.StatusNotFound, "only files in the knowledge base can be saved")
		return
	}
	full := filepath.Join(s.root, filepath.FromSlash(fi.Path))
	if rel, err := filepath.Rel(s.root, full); err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(filepath.ToSlash(rel), ".git/") {
		fail(w, http.StatusForbidden, "outside the editable workspace")
		return
	}
	info, err := os.Lstat(full)
	if err != nil {
		fail(w, http.StatusNotFound, "file no longer on disk")
		return
	}
	if !info.Mode().IsRegular() {
		fail(w, http.StatusForbidden, "not a regular file") // never write through symlinks
		return
	}
	cur, err := os.ReadFile(full)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if curHash := hashOf(cur); curHash != req.BaseHash && !req.Force {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error":       "the file changed on disk since you opened it",
			"currentHash": curHash,
		})
		return
	}
	if err := writeAtomic(full, []byte(req.Content), info.Mode().Perm()); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.scheduleRescan()
	writeJSON(w, map[string]any{"hash": hashOf([]byte(req.Content)), "bytes": len(req.Content)})
}

// writeAtomic replaces path via a temp file in the same directory, so readers never see
// a half-written file and a crash leaves the old version intact.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".kuli-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ---- git ---------------------------------------------------------------------------

func (s *Server) repo() git.Repo { return git.Repo{Root: s.root} }

func (s *Server) gitStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.repo().Status(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": st, "writable": !s.readOnly})
}

func (s *Server) gitDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	orig, mod, binary, err := s.repo().Diff(r.Context(), q.Get("path"), q.Get("staged") == "1")
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(orig)+len(mod) > 2*maxFileBytes {
		fail(w, http.StatusRequestEntityTooLarge, "file too large to diff here")
		return
	}
	writeJSON(w, map[string]any{"original": orig, "modified": mod, "binary": binary})
}

func (s *Server) gitBranches(w http.ResponseWriter, r *http.Request) {
	bs, err := s.repo().Branches(r.Context())
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"branches": nonNil(bs)})
}

func (s *Server) gitLines(w http.ResponseWriter, r *http.Request) {
	lc, err := s.repo().LineChanges(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, map[string]any{"changes": []any{}}) // untracked / not a repo: no markers
		return
	}
	writeJSON(w, map[string]any{"changes": nonNil(lc)})
}

// gitPatch is the whole pending change as unified diffs, for reviewing on a phone.
func (s *Server) gitPatch(w http.ResponseWriter, r *http.Request) {
	patch, truncated, err := s.repo().Patch(r.Context(), 400<<10)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"patch": patch, "truncated": truncated})
}

// gitWrite handles stage, unstage, discard, commit, switch, fetch, pull and push.
func (s *Server) gitWrite(w http.ResponseWriter, r *http.Request) {
	if s.readOnly {
		fail(w, http.StatusForbidden, "this workspace is read-only (kuli serve --read-only)")
		return
	}
	var req struct {
		Paths     []string `json:"paths"`
		Message   string   `json:"message"`
		Untracked bool     `json:"untracked"` // discard: also delete untracked files
		Branch    string   `json:"branch"`
		Create    bool     `json:"create"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "bad request body")
		return
	}
	repo, ctx := s.repo(), r.Context()
	var err error
	resp := map[string]any{"ok": true}
	switch r.PathValue("op") {
	case "stage":
		err = repo.Stage(ctx, req.Paths)
	case "unstage":
		err = repo.Unstage(ctx, req.Paths)
	case "discard":
		err = repo.Discard(ctx, req.Paths, req.Untracked)
	case "commit":
		var hash string
		hash, err = repo.Commit(ctx, req.Message)
		resp["commit"] = hash
	case "switch":
		err = repo.Switch(ctx, req.Branch, req.Create)
	case "fetch":
		err = repo.Fetch(ctx)
	case "pull":
		err = repo.Pull(ctx)
	case "push":
		err = repo.Push(ctx)
	default:
		fail(w, http.StatusNotFound, "unknown git operation")
		return
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.scheduleRescan() // discards change files; commits change HEAD
	writeJSON(w, resp)
}

// ---- pairing & devices ------------------------------------------------------------

func (s *Server) currentDevice(r *http.Request) (auth.Device, bool) {
	if s.auth == nil {
		return auth.Device{}, false
	}
	if d, ok := r.Context().Value(deviceKey{}).(auth.Device); ok {
		return d, true
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return s.auth.Check(c.Value)
	}
	return auth.Device{}, false
}

func (s *Server) authState(w http.ResponseWriter, r *http.Request) {
	d, paired := s.currentDevice(r)
	resp := map[string]any{"required": s.auth != nil, "paired": paired}
	if paired {
		resp["device"] = d.Public()
	}
	writeJSON(w, resp)
}

func (s *Server) setSession(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		fail(w, http.StatusBadRequest, "this workspace does not use pairing")
		return
	}
	var req struct{ Code, Name string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<10)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "code required")
		return
	}
	token, d, err := s.auth.Pair(req.Code, req.Name)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		fail(w, http.StatusTooManyRequests, err.Error())
		return
	case errors.Is(err, auth.ErrBadCode):
		fail(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setSession(w, token, 365*24*3600)
	log.Printf("paired device %q (%s)", d.Name, d.ID)
	writeJSON(w, map[string]any{"device": d.Public()})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.currentDevice(r); ok {
		s.auth.Revoke(d.ID)
	}
	s.setSession(w, "", -1)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) deviceList(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, map[string]any{"required": false, "devices": []any{}})
		return
	}
	me, _ := s.currentDevice(r)
	writeJSON(w, map[string]any{"required": true, "current": me.ID, "devices": s.auth.List()})
}

// deviceCode lets a paired device invite another (e.g. the laptop shows a QR for the phone).
func (s *Server) deviceCode(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		fail(w, http.StatusBadRequest, "this workspace does not use pairing")
		return
	}
	writeJSON(w, map[string]any{"code": s.auth.NewCode(), "expiresIn": int(auth.CodeTTL.Seconds())})
}

func (s *Server) deviceRevoke(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil || !s.auth.Revoke(r.PathValue("id")) {
		fail(w, http.StatusNotFound, "no such device")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// Close stops background work (terminal sessions).
// Close stops the workspace's terminals and waits for a running scan, so the store can
// be closed after it.
func (s *Server) Close() {
	s.rescanMu.Lock()
	if s.rescanDue != nil {
		s.rescanDue.Stop()
	}
	s.rescanMu.Unlock()
	if s.terms != nil {
		s.terms.Close()
	}
	s.scanM.Lock()
	s.scanM.Unlock()
}

// ---- terminals ---------------------------------------------------------------

func (s *Server) termsOr403(w http.ResponseWriter) bool {
	if s.terms == nil {
		fail(w, http.StatusForbidden, "terminals are disabled for this workspace")
		return false
	}
	return true
}

func (s *Server) termList(w http.ResponseWriter, r *http.Request) {
	if s.terms == nil {
		writeJSON(w, map[string]any{"enabled": false, "terminals": []any{}})
		return
	}
	writeJSON(w, map[string]any{"enabled": true, "terminals": s.terms.List()})
}

func (s *Server) termStart(w http.ResponseWriter, r *http.Request) {
	if !s.termsOr403(w) {
		return
	}
	var req struct{ Cols, Rows int }
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req)
	sess, err := s.terms.Start(req.Cols, req.Rows)
	if errors.Is(err, term.ErrTooMany) {
		fail(w, http.StatusTooManyRequests, err.Error())
		return
	} else if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, sess.Info())
}

func (s *Server) termKill(w http.ResponseWriter, r *http.Request) {
	if !s.termsOr403(w) {
		return
	}
	if !s.terms.Kill(r.PathValue("id")) {
		fail(w, http.StatusNotFound, "no such terminal")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// termInput sends keystrokes to a session without a terminal attached — how Pulse
// answers an agent from a phone.
func (s *Server) termInput(w http.ResponseWriter, r *http.Request) {
	if !s.termsOr403(w) {
		return
	}
	sess := s.terms.Get(r.PathValue("id"))
	if sess == nil {
		fail(w, http.StatusNotFound, "no such terminal")
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.Data == "" {
		fail(w, http.StatusBadRequest, "data required")
		return
	}
	if sess.Info().Exited {
		fail(w, http.StatusConflict, "that session has finished")
		return
	}
	if err := sess.Write([]byte(req.Data)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// termSocket streams a session: replayed scrollback, then live output (binary frames);
// the client sends {"type":"input","data":…} and {"type":"resize","cols":…,"rows":…}.
// websocket.Accept rejects cross-origin handshakes; guard() already checked Host.
func (s *Server) termSocket(w http.ResponseWriter, r *http.Request) {
	if !s.termsOr403(w) {
		return
	}
	sess := s.terms.Get(r.PathValue("id"))
	if sess == nil {
		fail(w, http.StatusNotFound, "no such terminal")
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 10)
	ctx := r.Context()

	replay, out, cancel := sess.Subscribe()
	defer cancel()
	if len(replay) > 0 {
		if err := c.Write(ctx, websocket.MessageBinary, replay); err != nil {
			return
		}
	}
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			var msg struct {
				Type       string
				Data       string
				Cols, Rows int
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "input":
				sess.Write([]byte(msg.Data))
			case "resize":
				sess.Resize(msg.Cols, msg.Rows)
			}
		}
	}()
	for chunk := range out {
		if err := c.Write(ctx, websocket.MessageBinary, chunk); err != nil {
			return
		}
	}
	c.Close(websocket.StatusNormalClosure, "session ended")
}

func (s *Server) agentList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"enabled": s.terms != nil, "agents": agents.List()})
}

// agentStart launches a coding agent in a terminal session, attached to this
// workspace's knowledge graph over MCP where the agent supports it.
func (s *Server) agentStart(w http.ResponseWriter, r *http.Request) {
	if !s.termsOr403(w) {
		return
	}
	var req struct {
		ID         string `json:"id"`
		Cols, Rows int
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "agent id required")
		return
	}
	kuli, err := os.Executable()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	name, argv, err := agents.Command(req.ID, agents.Workspace{Root: s.root, DataDir: s.dataDir, Kuli: kuli})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	sess, err := s.terms.StartCommand(name, "agent", argv, req.Cols, req.Rows)
	if errors.Is(err, term.ErrTooMany) {
		fail(w, http.StatusTooManyRequests, err.Error())
		return
	} else if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("started agent %s in %s", name, s.root)
	writeJSON(w, sess.Info())
}

func modelName(c docs.Chatter) string {
	if c == nil {
		return ""
	}
	return c.Model()
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
