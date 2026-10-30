// Package term runs persistent shell sessions on pseudo-terminals. A session keeps
// running when its viewers disconnect; reconnecting replays recent output.
package term

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// scrollback is how much recent output a session keeps for reconnecting viewers.
const scrollback = 256 << 10

// MaxSessions bounds how many shells one workspace can hold open.
const MaxSessions = 12

// Session is one shell on a PTY.
type Session struct {
	ID      string
	Title   string
	Kind    string // "shell" or "agent"
	Started time.Time

	p *proc

	mu     sync.Mutex
	buf    []byte // ring of the last `scrollback` bytes
	lastIO time.Time
	subs   map[chan []byte]struct{}
	exited bool
	code   int
}

// Info is a session's public state.
type Info struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Kind       string    `json:"kind"`
	Started    time.Time `json:"started"`
	LastOutput time.Time `json:"lastOutput"`
	Exited     bool      `json:"exited"`
	ExitCode   int       `json:"exitCode"`
	Tail       []string  `json:"tail,omitempty"` // last lines of output, terminal codes removed
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{ID: s.ID, Title: s.Title, Kind: s.Kind, Started: s.Started, LastOutput: s.lastIO,
		Exited: s.exited, ExitCode: s.code, Tail: tailLines(s.buf, 6)}
}

var (
	// csiRe matches control sequences; the final byte says what they do.
	csiRe = regexp.MustCompile(`^\x1b\[([0-?]*)[ -/]*([@-~])`) // params: digits ; < = > ?
	// otherEscRe matches OSC strings (titles, links) and two-byte escapes.
	otherEscRe = regexp.MustCompile(`^\x1b(\][^\x07\x1b]*(\x07|\x1b\\)?|[()][0-9A-Za-z]|[=>78MDEc])`)
)

// tailLines returns the last n non-empty lines a person would see. TUIs such as Claude
// Code place words with cursor moves (ESC[nG / ESC[nC) and redraw lines with \r, so
// each line is rendered on a small column model instead of just stripping codes.
func tailLines(buf []byte, n int) []string {
	start := max(0, len(buf)-16384)
	var lines []string
	for _, raw := range strings.Split(string(buf[start:]), "\n") {
		if ln := renderLine(raw); strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func renderLine(s string) string {
	var cells []rune
	col := 0
	put := func(r rune) {
		for len(cells) <= col {
			cells = append(cells, ' ')
		}
		cells[col] = r
		col++
	}
	for len(s) > 0 {
		switch c := s[0]; {
		case c == 0x1b:
			if m := csiRe.FindStringSubmatch(s); m != nil {
				num := 1
				if f := strings.Split(m[1], ";"); f[0] != "" && !strings.HasPrefix(f[0], "?") {
					if v, err := strconv.Atoi(f[0]); err == nil {
						num = v
					}
				}
				switch m[2] {
				case "G": // column absolute (1-based)
					col = min(max(num, 1), 500) - 1
				case "C": // forward
					col = min(col+max(num, 1), 500)
				case "D": // back
					col = max(col-max(num, 1), 0)
				case "K": // erase to end of line
					if m[1] == "" || m[1] == "0" {
						if col < len(cells) {
							cells = cells[:col]
						}
					} else if m[1] == "2" {
						cells = cells[:0]
					}
				}
				s = s[len(m[0]):]
			} else if m := otherEscRe.FindString(s); m != "" {
				s = s[len(m):]
			} else {
				s = s[1:]
			}
		case c == '\r':
			col = 0
			s = s[1:]
		case c == '\t':
			col = (col/8 + 1) * 8
			s = s[1:]
		case c < 0x20 || c == 0x7f:
			s = s[1:]
		default:
			r, size := utf8.DecodeRuneInString(s)
			put(r)
			s = s[size:]
		}
	}
	return strings.TrimRight(string(cells), " ")
}

// Subscribe returns the scrollback so far and a channel of new output. The channel
// closes when the session exits or the subscriber falls too far behind.
func (s *Session) Subscribe() ([]byte, <-chan []byte, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan []byte, 256)
	replay := append([]byte(nil), s.buf...)
	if s.exited {
		close(ch)
		return replay, ch, func() {}
	}
	s.subs[ch] = struct{}{}
	return replay, ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
}

// Write sends keystrokes to the shell.
func (s *Session) Write(p []byte) error {
	_, err := s.p.Write(p)
	return err
}

// Resize changes the terminal size.
func (s *Session) Resize(cols, rows int) error {
	if cols < 2 || rows < 2 || cols > 1000 || rows > 1000 {
		return errors.New("bad terminal size")
	}
	return s.p.Resize(cols, rows)
}

func (s *Session) pump() {
	b := make([]byte, 32<<10)
	for {
		n, err := s.p.Read(b)
		if n > 0 {
			chunk := append([]byte(nil), b[:n]...)
			s.mu.Lock()
			s.lastIO = time.Now()
			s.buf = append(s.buf, chunk...)
			if over := len(s.buf) - scrollback; over > 0 {
				s.buf = append(s.buf[:0], s.buf[over:]...)
			}
			for ch := range s.subs {
				select {
				case ch <- chunk:
				default: // a stalled viewer is dropped rather than blocking the shell
					delete(s.subs, ch)
					close(ch)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	code := s.p.Wait()
	s.mu.Lock()
	s.exited = true
	s.code = code
	msg := fmt.Sprintf("\r\n\x1b[2m[process exited with code %d]\x1b[0m\r\n", s.code)
	s.buf = append(s.buf, msg...)
	for ch := range s.subs {
		select {
		case ch <- []byte(msg):
		default:
		}
		close(ch)
		delete(s.subs, ch)
	}
	s.mu.Unlock()
	s.p.Close()
}

// Manager owns a workspace's sessions.
type Manager struct {
	dir   string
	shell []string

	mu   sync.Mutex
	next int
	sess map[string]*Session
}

// NewManager starts shells in dir: the login shell ($SHELL, else /bin/sh), or PowerShell
// on Windows.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir, shell: defaultShell(), sess: map[string]*Session{}}
}

// ErrTooMany is returned when MaxSessions are already running.
var ErrTooMany = errors.New("too many terminals open; close one first")

// Start opens a new shell session.
func (m *Manager) Start(cols, rows int) (*Session, error) {
	return m.StartCommand("", "shell", m.shell, cols, rows)
}

// StartCommand runs argv on a PTY in the workspace (e.g. a coding agent's CLI).
// An empty title gets "Terminal N".
func (m *Manager) StartCommand(title, kind string, argv []string, cols, rows int) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	live := 0
	for _, s := range m.sess {
		if !s.Info().Exited {
			live++
		}
	}
	if live >= MaxSessions {
		return nil, ErrTooMany
	}
	if cols < 2 || rows < 2 || cols > 1000 || rows > 1000 {
		cols, rows = 100, 30
	}
	p, err := startPTY(argv, m.dir, append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "KULIDEV=1"), cols, rows)
	if err != nil {
		return nil, err
	}
	m.next++
	if title == "" {
		title = fmt.Sprintf("Terminal %d", m.next)
	}
	s := &Session{
		ID: fmt.Sprintf("t%d", m.next), Title: title, Kind: kind, Started: time.Now(), lastIO: time.Now(),
		p: p, subs: map[chan []byte]struct{}{},
	}
	m.sess[s.ID] = s
	go s.pump()
	return s, nil
}

// Get returns a session by id.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sess[id]
}

// List returns all sessions, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.sess))
	for _, s := range m.sess {
		out = append(out, s.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// Kill ends a session and forgets it.
func (m *Manager) Kill(id string) bool {
	m.mu.Lock()
	s := m.sess[id]
	delete(m.sess, id)
	m.mu.Unlock()
	if s == nil {
		return false
	}
	s.p.Kill()
	return true
}

// Close kills every session (on server shutdown).
func (m *Manager) Close() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sess))
	for id := range m.sess {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Kill(id)
	}
}
