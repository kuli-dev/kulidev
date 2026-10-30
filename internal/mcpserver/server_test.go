package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kulidev/kulidev/internal/store"
)

func TestTools(t *testing.T) {
	root := t.TempDir()
	for p, body := range map[string]string{
		"app/core/db.py":      "def get_session():\n    pass\n",
		"app/services/pay.py": "from app.core.db import get_session\nclass PaymentService:\n    def charge(self):\n        pass\n",
		"web/page.tsx":        "import { Card } from './card';\nexport function Page() { return null }\n",
	} {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "kd"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(root, st)
	if err := s.Rescan(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ct, stt := mcp.NewInMemoryTransports()
	if _, err := s.mcpServer("test").Connect(ctx, stt, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 6 {
		t.Errorf("got %d tools, want 6", len(tools.Tools))
	}

	call := func(name string, args map[string]any) string {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		if res.IsError {
			t.Fatalf("%s returned error: %s", name, b.String())
		}
		return b.String()
	}
	contains := func(tool, out string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("%s output missing %q:\n%s", tool, w, out)
			}
		}
	}

	contains("overview", call("kuli_overview", nil), "Python", "TypeScript")
	contains("search", call("kuli_search", map[string]any{"query": "payment"}),
		"PaymentService", "app/services/pay.py:2")
	contains("importers", call("kuli_dependencies", map[string]any{"target": "app/core/db.py", "direction": "importers"}),
		"1 importers", "app/services/pay.py:1")
	contains("outline", call("kuli_file_symbols", map[string]any{"path": "app/services/pay.py"}),
		"PaymentService.charge", "lines 3-4")
	contains("broken", call("kuli_broken_imports", nil), "web/page.tsx:1", "web/card")
	contains("rescan", call("kuli_rescan", nil), "rescan complete")
}
