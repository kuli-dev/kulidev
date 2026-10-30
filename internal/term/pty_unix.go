//go:build !windows

package term

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// proc is a process on a pseudo-terminal.
type proc struct {
	cmd *exec.Cmd
	f   *os.File
}

func startPTY(argv []string, dir string, env []string, cols, rows int) (*proc, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &proc{cmd: cmd, f: f}, nil
}

func (p *proc) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *proc) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *proc) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Wait waits for the process to exit and returns its exit code (-1 if unknown).
func (p *proc) Wait() int {
	err := p.cmd.Wait()
	if p.cmd.ProcessState != nil {
		return p.cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

func (p *proc) Kill() {
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
}

func (p *proc) Close() error { return p.f.Close() }

// defaultShell is the user's login shell.
func defaultShell() []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-l"}
}
