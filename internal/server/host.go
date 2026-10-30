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
}

// NewHost starts with no workspace open.
func NewHost(o HostOptions) *Host {
	return &Host{ui: o.UI, llm: o.LLM, opts: o.Options, canOpen: o.CanOpen && o.Options.Auth == nil, recent: o.RecentFile}
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
// workspace, closing the previous one. dataDir "" means root/.kulidev.
func (h *Host) Open(root, dataDir string) (store.Summary, error) {
	h.opening.Lock()
	defer h.opening.Unlock()
	root, err := filepath.Abs(root)
	if err != nil {
		return store.Summary{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return store.Summary{}, errors.New("that folder does not exist")
	}
	if dataDir == "" {
		dataDir = filepath.Join(root, ".kulidev")
	}
	st, err := store.Open(dataDir)
	if err != nil {
		return store.Summary{}, err
	}
	sum, err := st.Refresh(root, false)
	if err != nil {
		st.Close()
		return store.Summary{}, err
	}
	srv, err := New(root, dataDir, st, h.llm, h.ui, h.opts)
	if err != nil {
		st.Close()
		return store.Summary{}, err
	}
	h.mu.Lock()
	old, oldSt := h.cur, h.curSt
	h.cur, h.curH, h.curSt = srv, srv.Handler(), st
	h.mu.Unlock()
	if old != nil {
		old.Close()
		oldSt.Close()
	}
	h.remember(root)
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
	}
	// Launcher endpoints exist only when switching is allowed; otherwise the workspace's
	// own (authenticated) handler answers everything.
	guarded := hostGuard(launcher)
	var files http.Handler
	if h.ui != nil {
		files = staticUI(h.ui)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.canOpen && (r.URL.Path == "/api/workspace" || strings.HasPrefix(r.URL.Path, "/api/workspace/") || strings.HasPrefix(r.URL.Path, "/api/fs/")) {
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
		"canOpen": h.canOpen, "recent": h.loadRecent(), "os": runtime.GOOS,
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
	sum, err := h.Open(strings.TrimSpace(req.Path), "")
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"root": h.Root(), "files": sum.Files, "ms": sum.Duration.Milliseconds(), "full": sum.Full})
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
