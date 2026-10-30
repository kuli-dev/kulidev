package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakePath(installed ...string) func(string) (string, error) {
	return func(bin string) (string, error) {
		for _, b := range installed {
			if b == bin {
				return "/usr/bin/" + bin, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestListAndCommands(t *testing.T) {
	orig := lookPath
	lookPath = fakePath("claude", "codex")
	t.Cleanup(func() { lookPath = orig })

	got := map[string]Agent{}
	for _, a := range List() {
		got[a.ID] = a
	}
	if !got["claude"].Installed || !got["codex"].Installed || got["aider"].Installed || !got["claude"].MCP {
		t.Errorf("list = %+v", got)
	}

	ws := Workspace{Root: "/repo", DataDir: t.TempDir(), Kuli: "/bin/kuli"}
	name, argv, err := Command("claude", ws)
	if err != nil || name != "Claude Code" || argv[0] != "/usr/bin/claude" || argv[1] != "--mcp-config" {
		t.Fatalf("claude = %q %v %v", name, argv, err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(argv[2])
	if err := json.Unmarshal(b, &cfg); err != nil || cfg.MCPServers["kulidev"].Command != "/bin/kuli" ||
		strings.Join(cfg.MCPServers["kulidev"].Args, " ") != "mcp -C /repo --data-dir "+ws.DataDir {
		t.Errorf("claude mcp config = %s", b)
	}
	if filepath.Dir(argv[2]) != filepath.Join(ws.DataDir, "agents") {
		t.Errorf("config written to %s", argv[2])
	}

	_, argv, _ = Command("codex", ws)
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, `mcp_servers.kulidev.command="/bin/kuli"`) || !strings.Contains(joined, `"--data-dir"`) {
		t.Errorf("codex argv = %v", argv)
	}

	if _, _, err := Command("aider", ws); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("aider err = %v", err)
	}
	if _, _, err := Command("nope", ws); err == nil {
		t.Error("unknown agent accepted")
	}
}
