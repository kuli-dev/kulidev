package term

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/UserExistsError/conpty"
)

// proc is a process on a Windows pseudo console (ConPTY, Windows 10 1809+).
type proc struct {
	c    *conpty.ConPty
	done chan struct{}
	code int
}

func startPTY(argv []string, dir string, env []string, cols, rows int) (*proc, error) {
	c, err := conpty.Start(commandLine(argv), conpty.ConPtyDimensions(cols, rows), conpty.ConPtyWorkDir(dir), conpty.ConPtyEnv(env))
	if err != nil {
		return nil, err
	}
	p := &proc{c: c, done: make(chan struct{})}
	go func() {
		code, err := c.Wait(context.Background())
		p.code = int(code)
		if err != nil {
			p.code = -1
		}
		close(p.done)
		// Let the last output drain, then close the console: the reader sees EOF.
		time.Sleep(300 * time.Millisecond)
		c.Close()
	}()
	return p, nil
}

// commandLine quotes argv for CreateProcess. Batch files (npm installs CLIs such as
// claude and codex as .cmd shims) run through cmd.exe, which CreateProcess requires.
func commandLine(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = syscall.EscapeArg(a)
	}
	line := strings.Join(quoted, " ")
	if ext := strings.ToLower(filepath.Ext(argv[0])); ext == ".cmd" || ext == ".bat" {
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return syscall.EscapeArg(comspec) + ` /d /s /c "` + line + `"`
	}
	return line
}

func (p *proc) Read(b []byte) (int, error)  { return p.c.Read(b) }
func (p *proc) Write(b []byte) (int, error) { return p.c.Write(b) }
func (p *proc) Resize(cols, rows int) error { return p.c.Resize(cols, rows) }

// Wait waits for the process to exit and returns its exit code.
func (p *proc) Wait() int {
	<-p.done
	return p.code
}

// Kill ends the process; closing the console alone does not.
func (p *proc) Kill() {
	if pr, err := os.FindProcess(p.c.Pid()); err == nil {
		pr.Kill()
	}
}

func (p *proc) Close() error {
	select {
	case <-p.done: // the waiter closes the console
		return nil
	default:
		return p.c.Close()
	}
}

// defaultShell is PowerShell 7 when installed, else Windows PowerShell.
func defaultShell() []string {
	for _, sh := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(sh); err == nil {
			return []string{path, "-NoLogo"}
		}
	}
	return []string{"cmd.exe"}
}
