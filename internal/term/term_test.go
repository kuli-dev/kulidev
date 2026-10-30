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
