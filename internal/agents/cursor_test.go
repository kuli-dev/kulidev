package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	prev := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = prev })
	return home
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return v
}

func TestCursorConfigCreatesFile(t *testing.T) {
	home := withHome(t)
	if CursorConfigured() {
		t.Fatal("configured before adding")
	}
	if err := AddCursorConfig("/opt/kuli"); err != nil {
		t.Fatal(err)
	}
	v := readJSON(t, filepath.Join(home, ".cursor", "mcp.json"))
	k := v["mcpServers"].(map[string]any)["kulidev"].(map[string]any)
	if k["command"] != "/opt/kuli" || strings.Join(toStrings(k["args"]), " ") != "mcp -C ${workspaceFolder}" {
		t.Errorf("entry = %v", k)
	}
	if !CursorConfigured() {
		t.Error("not configured after adding")
	}
}

func TestCursorConfigKeepsOtherServers(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".cursor", "mcp.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	orig := `{"mcpServers": {"github": {"command": "gh-mcp", "env": {"TOKEN": "${env:GH}"}}}, "other": 1}`
	os.WriteFile(path, []byte(orig), 0o600)

	if err := AddCursorConfig("/opt/kuli"); err != nil {
		t.Fatal(err)
	}
	v := readJSON(t, path)
	servers := v["mcpServers"].(map[string]any)
	if servers["github"].(map[string]any)["command"] != "gh-mcp" || v["other"] != float64(1) || servers["kulidev"] == nil {
		t.Errorf("merged = %v", v)
	}
	if b, _ := os.ReadFile(path + ".kulidev-backup"); string(b) != orig {
		t.Errorf("backup = %q", b)
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %v", info.Mode().Perm())
	}
	// Moving KuliDev updates the entry; the backup stays the original.
	if err := RefreshCursorConfig("/new/kuli"); err != nil {
		t.Fatal(err)
	}
	if readJSON(t, path)["mcpServers"].(map[string]any)["kulidev"].(map[string]any)["command"] != "/new/kuli" {
		t.Error("refresh did not update the command")
	}
	if b, _ := os.ReadFile(path + ".kulidev-backup"); string(b) != orig {
		t.Error("backup overwritten")
	}
}

func TestCursorConfigLeavesBrokenFileAlone(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".cursor", "mcp.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"mcpServers": {`), 0o644)
	if err := AddCursorConfig("/opt/kuli"); err == nil || !strings.Contains(err.Error(), "will not change it") {
		t.Errorf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != `{"mcpServers": {` {
		t.Errorf("broken file rewritten: %q", b)
	}
	if err := RefreshCursorConfig("/opt/kuli"); err != nil {
		t.Errorf("refresh on a broken file should do nothing: %v", err)
	}
}

func TestCursorDetection(t *testing.T) {
	prev := lookPath
	t.Cleanup(func() { lookPath = prev })
	paths := map[string]string{}
	lookPath = func(name string) (string, error) {
		if p, ok := paths[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	paths["agent"] = "/usr/local/bin/agent" // some other tool called "agent"
	if _, err := cursorBin(); err == nil {
		t.Error("a generic agent binary was taken for Cursor")
	}
	paths["agent"] = "/home/u/.local/share/cursor-agent/versions/1/agent"
	if p, err := cursorBin(); err != nil || p != paths["agent"] {
		t.Errorf("cursor's agent = %q %v", p, err)
	}
	paths["cursor-agent"] = "/home/u/.local/bin/cursor-agent"
	if p, _ := cursorBin(); p != paths["cursor-agent"] {
		t.Errorf("cursor-agent preferred: %q", p)
	}
	_, argv, err := Command("cursor", Workspace{Root: "/repo"})
	if err != nil || strings.Join(argv, " ") != "/home/u/.local/bin/cursor-agent --workspace /repo" {
		t.Errorf("argv = %v %v", argv, err)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
