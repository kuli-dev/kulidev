package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/store"
)

// Host owns the open workspace and can switch it at runtime ("Open Folder"). With no
// folder open it serves the UI and the launcher API only.
type Host struct {
	ui      fs.FS
	llm     docs.Chatter
	opts    Options
	canOpen bool // folder browsing/switching: local use only, never for remote devices
	recent  string

	mu      sync.RWMutex
	cur     *Server
	curH    http.Handler
	curSt   *store.Store
	opening sync.Mutex

	settingsFile   string
	llmFromStartup bool
	version        string

	jobMu sync.Mutex
	job   *OpenJob // the latest open, for the progress bar
}

// OpenJob is the progress of opening a folder.
type OpenJob struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Phase string `json:"phase"` // listing, reading, saving, done, error
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// Ready: the workspace is usable (a folder opened before is usable at once while
	// it is brought up to date).
	Ready bool   `json:"ready"`
	First bool   `json:"first"` // never scanned before: everything is read
	Error string `json:"error,omitempty"`
}

// HostOptions configures a host.
type HostOptions struct {
	UI      fs.FS
	LLM     docs.Chatter
	Options Options // applied to every workspace opened
	// CanOpen enables the folder browser and switching. Only for loopback without auth:
	// a remote device must never be able to browse or open arbitrary folders.
	CanOpen bool
	// RecentFile stores recently opened folders (empty: not remembered).
	RecentFile string
	// SettingsFile holds what the Settings screen saves. Its model is used unless LLM
	// was given (by flags or environment at start).
	SettingsFile string
	Version      string
}

// NewHost starts with no workspace open.
func NewHost(o HostOptions) *Host {
	h := &Host{ui: o.UI, llm: o.LLM, opts: o.Options, canOpen: o.CanOpen && o.Options.Auth == nil, recent: o.RecentFile,
		settingsFile: o.SettingsFile, llmFromStartup: o.LLM != nil, version: o.Version}
	if h.llm == nil && h.settingsFile != "" {
		if c, err := LLMFromSettings(h.loadSettings().LLM); err == nil {
			h.llm = c
		} else {
			log.Printf("settings: %v", err)
		}
	}
	return h
}

// Root returns the open workspace's root ("" when none).
func (h *Host) Root() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.cur == nil {
		return ""
	}
	return h.cur.root
}

// Open scans root (incrementally when it was scanned before) and makes it the
// workspace, closing the previous one. dataDir "" means root/.kulidev. It returns when
// the folder is fully up to date; StartOpen is the asynchronous form.
func (h *Host) Open(root, dataDir string) (store.Summary, error) {
	root, err := checkFolder(root)
	if err != nil {
		return store.Summary{}, err
	}
	return h.open(root, dataDir)
}

// StartOpen opens a folder in the background; Job reports its progress.
func (h *Host) StartOpen(root string) error {
	root, err := checkFolder(root)
	if err != nil {
		return err
	}
	h.jobMu.Lock()
	if h.job != nil && h.job.Phase != "done" && h.job.Phase != "error" {
		h.jobMu.Unlock()
		return errors.New("another folder is still opening")
	}
	h.job = &OpenJob{Path: root, Name: filepath.Base(root), Phase: "listing"}
	h.jobMu.Unlock()
	go func() {
		if _, err := h.open(root, ""); err != nil {
			log.Printf("open %s: %v", root, err)
		}
	}()
	return nil
}

// Job returns the progress of the latest open (nil when none).
func (h *Host) Job() *OpenJob {
	h.jobMu.Lock()
	defer h.jobMu.Unlock()
	if h.job == nil {
		return nil
	}
	j := *h.job
	return &j
}

func (h *Host) setJob(f func(j *OpenJob)) {
	h.jobMu.Lock()
	defer h.jobMu.Unlock()
	if h.job != nil {
		f(h.job)
	}
}

func checkFolder(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", errors.New("that folder does not exist")
	}
	return root, nil
}

func (h *Host) open(root, dataDir string) (sum store.Summary, err error) {
	h.opening.Lock()
	defer h.opening.Unlock()
	defer func() {
		if err != nil {
			h.setJob(func(j *OpenJob) { j.Phase, j.Error = "error", err.Error() })
		}
	}()
	h.jobMu.Lock()
	if h.job == nil || h.job.Path != root {
		h.job = &OpenJob{Path: root, Name: filepath.Base(root), Phase: "listing"}
	}
	h.jobMu.Unlock()
	prog := func(phase string, done, total int) {
		h.setJob(func(j *OpenJob) { j.Phase, j.Done, j.Total = phase, done, total })
	}

	if dataDir == "" {
		dataDir = filepath.Join(root, ".kulidev")
	}
	st, err := store.Open(dataDir)
	if err != nil {
		return sum, err
	}
	known, err := st.Stamps()
	if err != nil {
		st.Close()
		return sum, err
	}
	first := len(known) == 0
	h.setJob(func(j *OpenJob) { j.First = first })
	if first {
		// Nothing to show yet: read everything, then switch.
		if sum, err = st.RefreshProgress(root, false, prog); err != nil {
			st.Close()
			return sum, err
		}
	}
	h.mu.RLock()
	model := h.llm
	h.mu.RUnlock()
	srv, err := New(root, dataDir, st, model, h.ui, h.opts)
	if err != nil {
		st.Close()
		return sum, err
	}
	h.mu.Lock()
	old, oldSt := h.cur, h.curSt
	h.cur, h.curH, h.curSt = srv, srv.Handler(), st
	h.mu.Unlock()
	h.setJob(func(j *OpenJob) { j.Ready = true })
	if old != nil {
		old.Close()
		oldSt.Close()
	}
	h.remember(root)
	if !first {
		// Usable already from what was read last time; now catch up with changes.
		if sum, err = srv.refresh(prog); err != nil {
			return sum, err
		}
	}
	h.setJob(func(j *OpenJob) { j.Phase = "done" })
	log.Printf("opened %s (%s)", root, sum)
	return sum, nil
}

// CloseWorkspace returns to the launcher.
func (h *Host) CloseWorkspace() {
	h.mu.Lock()
	old, oldSt := h.cur, h.curSt
	h.cur, h.curH, h.curSt = nil, nil, nil
	h.mu.Unlock()
	if old != nil {
		old.Close()
		oldSt.Close()
	}
}

// Close shuts the host down.
func (h *Host) Close() { h.CloseWorkspace() }

// Handler routes launcher calls to the host and everything else to the workspace.
func (h *Host) Handler() http.Handler {
	launcher := http.NewServeMux()
	launcher.HandleFunc("GET /api/workspace", h.workspaceInfo)
	if h.canOpen {
		launcher.HandleFunc("GET /api/fs/list", h.fsList)
		launcher.HandleFunc("POST /api/workspace/open", h.openHandler)
		launcher.HandleFunc("POST /api/workspace/close", h.closeHandler)
		if h.settingsFile != "" {
			launcher.HandleFunc("GET /api/settings", h.getSettings)
			launcher.HandleFunc("PUT /api/settings", h.putSettings)
			launcher.HandleFunc("POST /api/settings/test", h.testSettings)
		}
	}
	// Launcher endpoints exist only when switching is allowed; otherwise the workspace's
	// own (authenticated) handler answers everything.
	guarded := hostGuard(launcher)
	var files http.Handler
	if h.ui != nil {
		files = staticUI(h.ui)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.canOpen && (r.URL.Path == "/api/workspace" || strings.HasPrefix(r.URL.Path, "/api/workspace/") || strings.HasPrefix(r.URL.Path, "/api/fs/") || r.URL.Path == "/api/settings" || strings.HasPrefix(r.URL.Path, "/api/settings/")) {
			guarded.ServeHTTP(w, r)
			return
		}
		h.mu.RLock()
		cur := h.curH
		h.mu.RUnlock()
		switch {
		case cur != nil:
			cur.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			hostGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"error": "no folder is open", "noWorkspace": true})
			})).ServeHTTP(w, r)
		case files != nil:
			hostGuard(files).ServeHTTP(w, r)
		default:
			http.Error(w, "KuliDev: the web UI is not built into this binary", http.StatusNotFound)
		}
	})
}

// hostGuard is the launcher's request check: browser hardening, localhost-only Host
// (DNS rebinding), and same-origin writes carrying X-Kuli-Request.
func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:")
		if !loopbackHost(r.Host) {
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
		next.ServeHTTP(w, r)
	})
}

func staticUI(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// ---- launcher API -------------------------------------------------------------------

type recentEntry struct {
	Path   string    `json:"path"`
	Name   string    `json:"name"`
	Opened time.Time `json:"opened"`
	Exists bool      `json:"exists"`
}

func (h *Host) loadRecent() []recentEntry {
	var list []recentEntry
	if h.recent == "" {
		return list
	}
	if b, err := os.ReadFile(h.recent); err == nil {
		json.Unmarshal(b, &list)
	}
	for i := range list {
		info, err := os.Stat(list[i].Path)
		list[i].Exists = err == nil && info.IsDir()
	}
	return list
}

func (h *Host) remember(root string) {
	if h.recent == "" {
		return
	}
	list := []recentEntry{{Path: root, Name: filepath.Base(root), Opened: time.Now()}}
	for _, e := range h.loadRecent() {
		if e.Path != root && len(list) < 12 {
			list = append(list, e)
		}
	}
	if err := os.MkdirAll(filepath.Dir(h.recent), 0o700); err == nil {
		b, _ := json.MarshalIndent(list, "", "  ")
		os.WriteFile(h.recent, b, 0o600)
	}
}

func (h *Host) workspaceInfo(w http.ResponseWriter, r *http.Request) {
	root := h.Root()
	writeJSON(w, map[string]any{
		"open": root != "", "root": root, "name": filepath.Base(root),
		"canOpen": h.canOpen, "recent": h.loadRecent(), "os": runtime.GOOS, "opening": h.Job(),
	})
}

func (h *Host) openHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		fail(w, http.StatusBadRequest, "path required")
		return
	}
	if err := h.StartOpen(strings.TrimSpace(req.Path)); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"opening": h.Job()})
}

func (h *Host) closeHandler(w http.ResponseWriter, r *http.Request) {
	h.CloseWorkspace()
	writeJSON(w, map[string]bool{"ok": true})
}

type dirEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsRepo bool   `json:"isRepo"`
}

// fsList lists the sub-folders of a folder for the "Open folder" browser. Empty path:
// the home folder (plus drive letters on Windows).
func (h *Host) fsList(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	home, _ := os.UserHomeDir()
	if p == "" {
		p = home
	}
	p, err := filepath.Abs(p)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		fail(w, http.StatusBadRequest, "cannot read that folder")
		return
	}
	var dirs []dirEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
			continue
		}
		full := filepath.Join(p, name)
		if info, err := os.Stat(full); err != nil || !info.IsDir() {
			continue // follows symlinks to folders; skips files and broken links
		}
		dirs = append(dirs, dirEntry{Name: name, Path: full, IsRepo: isRepo(full)})
		if len(dirs) == 1000 {
			break
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	resp := map[string]any{"path": p, "parent": parent, "home": home, "dirs": nonNil(dirs), "isRepo": isRepo(p)}
	if runtime.GOOS == "windows" {
		var drives []string
		for c := 'A'; c <= 'Z'; c++ {
			if _, err := os.Stat(string(c) + `:\`); err == nil {
				drives = append(drives, string(c)+`:\`)
			}
		}
		resp["drives"] = drives
	}
	writeJSON(w, resp)
}

func isRepo(dir string) bool {
	for _, marker := range []string{".git", ".kulidev"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}
