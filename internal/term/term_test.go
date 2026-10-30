//go:build !windows

package term

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func readUntil(t *testing.T, ch <-chan []byte, want string) string {
	t.Helper()
	var got bytes.Buffer
	deadline := time.After(5 * time.Second)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				if strings.Contains(got.String(), want) {
					return got.String()
				}
				t.Fatalf("stream closed before %q; got %q", want, got.String())
			}
			got.Write(b)
			if strings.Contains(got.String(), want) {
				return got.String()
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q; got %q", want, got.String())
		}
	}
}

func TestSessionSurvivesViewerAndReplays(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager(t.TempDir())
	defer m.Close()
	s, err := m.Start(80, 24)
	if err != nil {
		t.Fatal(err)
	}

	_, ch, cancel := s.Subscribe()
	s.Write([]byte("echo kuli-$((40+2))\n"))
	readUntil(t, ch, "kuli-42")
	cancel() // the viewer disconnects; the shell keeps running

	s.Write([]byte("echo still-here\n"))
	time.Sleep(300 * time.Millisecond)

	// A new viewer gets the scrollback, including output produced while nobody watched.
	replay, ch2, cancel2 := s.Subscribe()
	defer cancel2()
	if !strings.Contains(string(replay), "kuli-42") || !strings.Contains(string(replay), "still-here") {
		t.Errorf("replay missing output: %q", replay)
	}
	if err := s.Resize(120, 40); err != nil {
		t.Errorf("resize: %v", err)
	}

	s.Write([]byte("exit 3\n"))
	readUntil(t, ch2, "exited with code 3")
	if info := s.Info(); !info.Exited || info.ExitCode != 3 {
		t.Errorf("info = %+v", info)
	}
}

func TestLimitsAndKill(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := NewManager(t.TempDir())
	defer m.Close()
	for i := 0; i < MaxSessions; i++ {
		if _, err := m.Start(80, 24); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Start(80, 24); err != ErrTooMany {
		t.Errorf("err = %v, want ErrTooMany", err)
	}
	id := m.List()[0].ID
	if !m.Kill(id) || m.Get(id) != nil {
		t.Error("kill did not remove the session")
	}
	if _, err := m.Start(80, 24); err != nil {
		t.Errorf("start after kill: %v", err)
	}
}

func TestTailLines(t *testing.T) {
	raw := "\x1b[32mok\x1b[0m build\r\nprogress 10%\rprogress 100%\r\n\x1b]0;title\x07\n\n> waiting for input\x1b[?25h"
	got := tailLines([]byte(raw), 6)
	want := []string{"ok build", "progress 100%", "> waiting for input"}
	if got := tailLines([]byte("Claude\x1b[1CCode'll\x1b[1Cbe\x1b[3Cable\x1b[C!"), 2); len(got) != 1 || got[0] != "Claude Code'll be   able !" {
		t.Errorf("cursor-forward as spaces: %q", got)
	}
	if got := tailLines([]byte("ready\x1b[>0q\x1b[?2004h\x1b[<u"), 2); len(got) != 1 || got[0] != "ready" {
		t.Errorf("private-marker sequences leaked: %q", got)
	}
	// What Claude Code really sends: words placed by absolute column.
	real := "\x1b[2GClaude\x1b[9GCode'll\x1b[17Gbe\x1b[20Gable\x1b[25Gto\x1b[28Gread,\r\r\n\x1b[2Gthis\x1b[7Gfolder\x1b[K"
	if got := tailLines([]byte(real), 5); strings.Join(got, "|") != " Claude Code'll be able to read,| this folder" {
		t.Errorf("absolute columns: %q", got)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("tail = %q, want %q", got, want)
	}
}
