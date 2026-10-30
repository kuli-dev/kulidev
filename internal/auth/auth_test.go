package auth

import (
	"os"
	"runtime"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPairCheckRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	code := s.NewCode()
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("code = %q", code)
	}
	tok, d, err := s.Pair(strings.ToLower(code), "Pixel 8")
	if err != nil || tok == "" || d.Name != "Pixel 8" {
		t.Fatalf("pair = %q %+v %v", tok, d, err)
	}
	if _, _, err := s.Pair(code, "again"); err != ErrBadCode {
		t.Errorf("code reused: %v", err)
	}
	if got, ok := s.Check(tok); !ok || got.ID != d.ID {
		t.Error("token not accepted")
	}
	if _, ok := s.Check(tok + "x"); ok {
		t.Error("wrong token accepted")
	}

	// Persisted with the hash only, file not world-readable, and survives a restart.
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), tok) {
		t.Error("plain token stored on disk")
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 { // Windows: profile ACLs, no Unix modes
		t.Errorf("devices file mode = %v", info.Mode().Perm())
	}
	s2, _ := Open(path)
	if _, ok := s2.Check(tok); !ok {
		t.Error("token lost after reopen")
	}
	if len(s2.List()) != 1 || s2.List()[0].TokenHash != "" {
		t.Errorf("list leaks hash or wrong: %+v", s2.List())
	}

	if !s2.Revoke(d.ID) {
		t.Fatal("revoke failed")
	}
	if _, ok := s2.Check(tok); ok {
		t.Error("revoked token still works")
	}
}

func TestCodesExpireAndRateLimit(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "d.json"))
	clock := time.Now()
	s.now = func() time.Time { return clock }

	code := s.NewCode()
	clock = clock.Add(CodeTTL + time.Second)
	if _, _, err := s.Pair(code, "late"); err != ErrBadCode {
		t.Errorf("expired code: %v", err)
	}

	good := s.NewCode()
	for i := 0; i < maxFailures; i++ {
		s.Pair("WRONG-CODE", "x")
	}
	if _, _, err := s.Pair(good, "me"); err != ErrRateLimited {
		t.Errorf("after %d failures: %v, want rate limited", maxFailures, err)
	}
	clock = clock.Add(failureWindow + time.Second)
	if _, _, err := s.Pair(good, "me"); err != nil {
		t.Errorf("after the window the valid code should work: %v", err)
	}
}

func TestQRAndCert(t *testing.T) {
	q, err := TerminalQR("https://192.168.1.20:7878/#/pair?code=ABCD-EFGH")
	if err != nil || strings.Count(q, "\n") < 12 || !strings.Contains(q, "▀") {
		t.Errorf("qr: %v (%d lines)", err, strings.Count(q, "\n"))
	}
	dir := t.TempDir()
	c, k, err := SelfSignedCert(dir, []string{"100.64.0.5", "devbox"})
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(k); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("key mode = %v", info.Mode().Perm())
	}
	c2, _, _ := SelfSignedCert(dir, nil) // reused, not regenerated
	b1, _ := os.ReadFile(c)
	b2, _ := os.ReadFile(c2)
	if string(b1) != string(b2) {
		t.Error("certificate regenerated on second call")
	}
}
