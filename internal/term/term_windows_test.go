package term

import (
	"strings"
	"testing"
	"time"
)

// ConPTY: a command's output reaches the session and its exit code is reported; an
// interactive shell takes input and resizes.
func TestConPTY(t *testing.T) {
	m := NewManager(t.TempDir())
	defer m.Close()
	s, err := m.StartCommand("echo", "shell", []string{"cmd.exe", "/c", "echo kuli-42 & exit 3"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	_, ch, cancel := s.Subscribe()
	defer cancel()
	var got strings.Builder
	deadline := time.After(20 * time.Second)
	for !strings.Contains(got.String(), "exited with code 3") {
		select {
		case b, ok := <-ch:
			if !ok {
				if !strings.Contains(got.String(), "exited with code 3") {
					t.Fatalf("closed early: %q", got.String())
				}
				break
			}
			got.Write(b)
		case <-deadline:
			t.Fatalf("timed out: %q", got.String())
		}
	}
	if !strings.Contains(got.String(), "kuli-42") {
		t.Errorf("output = %q", got.String())
	}
	if info := s.Info(); !info.Exited || info.ExitCode != 3 {
		t.Errorf("info = %+v", info)
	}

	sh, err := m.StartCommand("", "shell", []string{"cmd.exe"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	_, ch2, cancel2 := sh.Subscribe()
	defer cancel2()
	if err := sh.Resize(120, 40); err != nil {
		t.Errorf("resize: %v", err)
	}
	sh.Write([]byte("echo typed-%USERNAME%-ok\r\n"))
	got.Reset()
	deadline = time.After(20 * time.Second)
	for !strings.Contains(got.String(), "-ok") || !strings.Contains(got.String(), "typed-") {
		select {
		case b := <-ch2:
			got.Write(b)
		case <-deadline:
			t.Fatalf("interactive shell: %q", got.String())
		}
	}
	if !m.Kill(sh.ID) {
		t.Error("kill failed")
	}
}

func TestCommandLine(t *testing.T) {
	if got := commandLine([]string{`C:\Program Files\x\tool.exe`, "a b", `q"x`}); got != `"C:\Program Files\x\tool.exe" "a b" q\"x` {
		t.Errorf("exe: %s", got)
	}
	if got := commandLine([]string{`C:\npm\claude.cmd`, "--mcp-config", `C:\a b\c.json`}); !strings.HasSuffix(got, ` /d /s /c "C:\npm\claude.cmd --mcp-config "C:\a b\c.json""`) {
		t.Errorf("cmd shim: %s", got)
	}
}
