// Package term runs persistent shell sessions on pseudo-terminals. A session keeps
// running when its viewers disconnect; reconnecting replays recent output.
package term

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/creack/pty"
)

// scrollback is how much recent output a session keeps for reconnecting viewers.
const scrollback = 256 << 10

// MaxSessions bounds how many shells one workspace can hold open.
const MaxSessions = 12

// Session is one shell on a PTY.
type Session struct {
	ID      string
	Title   string
	Started time.Time

	cmd  *exec.Cmd
	ptmx *os.File

	mu     sync.Mutex
	buf    []byte // ring of the last `scrollback` bytes
	subs   map[chan []byte]struct{}
	exited bool
	code   int
}

// Info is a session's public state.
type Info struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Started  time.Time `json:"started"`
	Exited   bool      `json:"exited"`
	ExitCode int       `json:"exitCode"`
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{s.ID, s.Title, s.Started, s.exited, s.code}
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
	_, err := s.ptmx.Write(p)
	return err
}

// Resize changes the terminal size.
func (s *Session) Resize(cols, rows int) error {
	if cols < 2 || rows < 2 || cols > 1000 || rows > 1000 {
		return errors.New("bad terminal size")
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (s *Session) pump() {
	b := make([]byte, 32<<10)
	for {
		n, err := s.ptmx.Read(b)
		if n > 0 {
			chunk := append([]byte(nil), b[:n]...)
			s.mu.Lock()
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
	err := s.cmd.Wait()
	s.mu.Lock()
	s.exited = true
	if s.cmd.ProcessState != nil {
		s.code = s.cmd.ProcessState.ExitCode()
	} else if err != nil {
		s.code = -1
	}
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
	s.ptmx.Close()
}

// Manager owns a workspace's sessions.
type Manager struct {
	dir   string
	shell []string

	mu   sync.Mutex
	next int
	sess map[string]*Session
}

// NewManager starts shells in dir. The shell is $SHELL (login, interactive) or /bin/sh.
func NewManager(dir string) *Manager {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return &Manager{dir: dir, shell: []string{sh, "-l"}, sess: map[string]*Session{}}
}

// ErrTooMany is returned when MaxSessions are already running.
var ErrTooMany = errors.New("too many terminals open; close one first")

// Start opens a new shell session.
func (m *Manager) Start(cols, rows int) (*Session, error) {
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
	cmd := exec.Command(m.shell[0], m.shell[1:]...)
	cmd.Dir = m.dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "KULIDEV=1")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	m.next++
	s := &Session{
		ID: fmt.Sprintf("t%d", m.next), Title: fmt.Sprintf("Terminal %d", m.next), Started: time.Now(),
		cmd: cmd, ptmx: ptmx, subs: map[chan []byte]struct{}{},
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
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
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
