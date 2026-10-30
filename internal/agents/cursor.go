package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cursor's CLI (`agent`, formerly `cursor-agent`) has no flag for an MCP config: it
// reads the editor's files (.cursor/mcp.json in the project, then ~/.cursor/mcp.json).
// KuliDev adds one entry to the user file, with the user's consent, that works for any
// folder: `kuli mcp -C ${workspaceFolder}`. Other servers in the file are left as they
// are, and a file KuliDev cannot read is never rewritten.

// homeDir is replaceable in tests.
var homeDir = os.UserHomeDir

// CursorConfigPath is the user-level Cursor MCP config.
func CursorConfigPath() string {
	h, err := homeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".cursor", "mcp.json")
}

// cursorBin finds the CLI. "agent" is a generic name, so it only counts when it is
// Cursor's (installed under a cursor folder).
func cursorBin() (string, error) {
	if p, err := lookPath("cursor-agent"); err == nil {
		return p, nil
	}
	if p, err := lookPath("agent"); err == nil {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			real = p
		}
		if strings.Contains(strings.ToLower(real), "cursor") {
			return p, nil
		}
	}
	return "", errors.New("not found")
}

// CursorEntry is the server KuliDev adds for the given kuli binary.
func CursorEntry(kuli string) map[string]any {
	return map[string]any{"command": kuli, "args": []string{"mcp", "-C", "${workspaceFolder}"}}
}

func readCursorConfig() (top map[string]json.RawMessage, servers map[string]json.RawMessage, exists bool, err error) {
	path := CursorConfigPath()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, map[string]json.RawMessage{}, false, nil
	}
	if err != nil {
		return nil, nil, true, err
	}
	top = map[string]json.RawMessage{}
	if len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &top); err != nil {
			return nil, nil, true, fmt.Errorf("%s is not valid JSON (%v); KuliDev will not change it", path, err)
		}
	}
	servers = map[string]json.RawMessage{}
	if raw, ok := top["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, nil, true, fmt.Errorf("mcpServers in %s is not an object; KuliDev will not change it", path)
		}
	}
	return top, servers, true, nil
}

// CursorConfigured reports whether the user's Cursor config has the kulidev server.
func CursorConfigured() bool {
	_, servers, _, err := readCursorConfig()
	return err == nil && servers["kulidev"] != nil
}

// AddCursorConfig adds (or updates) the kulidev server in ~/.cursor/mcp.json, keeping
// everything else. The first change backs the file up to mcp.json.kulidev-backup.
func AddCursorConfig(kuli string) error {
	path := CursorConfigPath()
	if path == "" {
		return errors.New("no home folder")
	}
	top, servers, exists, err := readCursorConfig()
	if err != nil {
		return err
	}
	entry, _ := json.Marshal(CursorEntry(kuli))
	if bytes.Equal(servers["kulidev"], entry) {
		return nil
	}
	servers["kulidev"] = entry
	if top["mcpServers"], err = json.Marshal(servers); err != nil {
		return err
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if exists {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		backup := path + ".kulidev-backup"
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			if b, err := os.ReadFile(path); err == nil {
				os.WriteFile(backup, b, mode)
			}
		}
	}
	tmp := path + ".kulidev-tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RefreshCursorConfig points an existing kulidev entry at the current kuli binary
// (KuliDev moved or was updated). It never adds an entry.
func RefreshCursorConfig(kuli string) error {
	if !CursorConfigured() {
		return nil
	}
	return AddCursorConfig(kuli)
}

// The CLI starts in the workspace; the MCP server comes from the user's config.
func cursorArgs(ws Workspace) ([]string, error) {
	return []string{"--workspace", ws.Root}, nil
}
