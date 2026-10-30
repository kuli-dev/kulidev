// Package agents launches coding-agent CLIs inside a workspace, with KuliDev's MCP
// server attached when the agent supports it.
package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Agent describes one supported CLI.
type Agent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Bin       string `json:"-"`
	Installed bool   `json:"installed"`
	MCP       bool   `json:"mcp"` // KuliDev knowledge is attached when launched
	Hint      string `json:"hint,omitempty"`
}

// Workspace is what a launch needs to know about where it runs.
type Workspace struct {
	Root    string // repository root
	DataDir string // .kulidev directory (knowledge base + agent config)
	Kuli    string // path to the kuli binary, for the MCP server command
}

type adapter struct {
	Agent
	args func(ws Workspace) ([]string, error)
}

var adapters = []adapter{
	{Agent{ID: "claude", Name: "Claude Code", Bin: "claude", MCP: true}, claudeArgs},
	{Agent{ID: "codex", Name: "OpenAI Codex", Bin: "codex", MCP: true}, codexArgs},
	{Agent{ID: "opencode", Name: "OpenCode", Bin: "opencode", Hint: "add kuli mcp to opencode.json to give it the knowledge graph"}, noArgs},
	{Agent{ID: "aider", Name: "Aider", Bin: "aider", Hint: "Aider has no MCP support; it runs without the knowledge graph"}, noArgs},
}

// lookPath is replaceable in tests.
var lookPath = exec.LookPath

// List reports every supported agent and whether its CLI is on PATH.
func List() []Agent {
	out := make([]Agent, 0, len(adapters))
	for _, a := range adapters {
		ag := a.Agent
		_, err := lookPath(a.Bin)
		ag.Installed = err == nil
		out = append(out, ag)
	}
	return out
}

// Command returns the argv that starts agent id in the workspace.
func Command(id string, ws Workspace) (name string, argv []string, err error) {
	for _, a := range adapters {
		if a.ID != id {
			continue
		}
		bin, err := lookPath(a.Bin)
		if err != nil {
			return "", nil, fmt.Errorf("%s is not installed (no %q on PATH)", a.Name, a.Bin)
		}
		args, err := a.args(ws)
		if err != nil {
			return "", nil, err
		}
		return a.Name, append([]string{bin}, args...), nil
	}
	return "", nil, fmt.Errorf("unknown agent %q", id)
}

func mcpServer(ws Workspace) (string, []string) {
	return ws.Kuli, []string{"mcp", "-C", ws.Root, "--data-dir", ws.DataDir}
}

// Claude Code reads MCP servers from a JSON file given with --mcp-config.
func claudeArgs(ws Workspace) ([]string, error) {
	cmd, args := mcpServer(ws)
	cfg := map[string]any{"mcpServers": map[string]any{"kulidev": map[string]any{"command": cmd, "args": args}}}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	dir := filepath.Join(ws.DataDir, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "claude-mcp.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return nil, err
	}
	return []string{"--mcp-config", path}, nil
}

// Codex takes TOML overrides with -c key=value.
func codexArgs(ws Workspace) ([]string, error) {
	cmd, args := mcpServer(ws)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = strconv.Quote(a)
	}
	return []string{
		"-c", "mcp_servers.kulidev.command=" + strconv.Quote(cmd),
		"-c", "mcp_servers.kulidev.args=[" + strings.Join(quoted, ",") + "]",
	}, nil
}

func noArgs(Workspace) ([]string, error) { return nil, nil }
