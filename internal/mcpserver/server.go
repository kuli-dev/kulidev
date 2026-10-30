// Package mcpserver exposes the KuliDev knowledge base to coding agents over MCP.
package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kuli-dev/kulidev/internal/report"
	"github.com/kuli-dev/kulidev/internal/scan"
	"github.com/kuli-dev/kulidev/internal/store"
)

const instructions = `KuliDev has pre-indexed this repository into a knowledge graph of files,
symbols (functions, methods, classes, interfaces, types) and imports. Every result carries
file:line evidence.

Prefer these tools over grep/find when you need to:
- get oriented in an unfamiliar repo (kuli_overview)
- locate where something is defined (kuli_search)
- know who depends on a file/package/module before changing it (kuli_dependencies, direction "importers")
- list what a file defines without reading all of it (kuli_file_symbols)

If a result says the knowledge base is stale, call kuli_rescan.`

// Server serves one repository's knowledge base.
type Server struct {
	root  string
	store *store.Store
	mu    sync.Mutex // serialises rescans
}

// New wraps an open store for the repository at root.
func New(root string, st *store.Store) *Server {
	return &Server{root: root, store: st}
}

// Run serves MCP over stdin/stdout until the client disconnects.
func (s *Server) Run(ctx context.Context, version string) error {
	return s.mcpServer(version).Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) mcpServer(version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "kulidev", Title: "KuliDev", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	s.register(srv)
	return srv
}

type overviewIn struct{}

type searchIn struct {
	Query string `json:"query" jsonschema:"words to search for in symbol, file and module names; camelCase and snake_case are split (e.g. 'payment service')"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results (default 25, max 200)"`
}

type depsIn struct {
	Target    string `json:"target" jsonschema:"repo-relative file path, Go package directory, or external module name (e.g. 'app/core/db.py', 'internal/store', 'react')"`
	Direction string `json:"direction,omitempty" jsonschema:"'imports' (what target depends on, default) or 'importers' (what depends on target)"`
}

type fileIn struct {
	Path string `json:"path" jsonschema:"repo-relative file path"`
}

type noIn struct{}

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true}
}

func (s *Server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "kuli_overview",
		Description: "Summary of the repository: languages, symbol counts, infrastructure/build files, most depended-on internal files and most used external modules.",
		Annotations: readOnly("Repository overview"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ overviewIn) (*mcp.CallToolResult, any, error) {
		st, err := s.store.Stats()
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		report.Stats(&b, st)
		return s.text(b.String()), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "kuli_search",
		Description: "Full-text search over definitions (functions, methods, classes, interfaces, types), files, Go packages and external modules — by name and by doc comment/docstring text. Returns kind, qualified name, file:line and the doc summary.",
		Annotations: readOnly("Search code knowledge"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 25
		}
		limit = min(limit, 200)
		hits, err := s.store.Find(in.Query, limit)
		if err != nil {
			return nil, nil, err
		}
		if len(hits) == 0 {
			return s.text("no matches for " + in.Query), nil, nil
		}
		return s.text(formatHits(hits)), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "kuli_dependencies",
		Description: "Import relationships with evidence. direction='imports' lists what the target imports; direction='importers' lists every file that imports the target (impact analysis before a change). A Go file's importers are the importers of its package.",
		Annotations: readOnly("Dependencies"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in depsIn) (*mcp.CallToolResult, any, error) {
		reverse := false
		switch in.Direction {
		case "", "imports":
		case "importers":
			reverse = true
		default:
			return nil, nil, fmt.Errorf("direction must be 'imports' or 'importers', got %q", in.Direction)
		}
		name, hits, err := s.store.Deps(in.Target, reverse)
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		if reverse {
			fmt.Fprintf(&b, "%d importers of %s (file:line of the import statement):\n", len(hits), name)
			for _, h := range hits {
				fmt.Fprintf(&b, "%s:%d\n", h.Path, h.Line)
			}
		} else {
			fmt.Fprintf(&b, "%s imports %d:\n", name, len(hits))
			for _, h := range hits {
				fmt.Fprintf(&b, "%-8s %s  (%s:%d)\n", h.Kind, h.Name, h.Path, h.Line)
			}
		}
		return s.text(b.String()), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "kuli_file_symbols",
		Description: "Outline of one file: every definition with its kind, line range and doc summary, in source order.",
		Annotations: readOnly("File outline"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fileIn) (*mcp.CallToolResult, any, error) {
		hits, err := s.store.FileSymbols(in.Path)
		if err != nil {
			return nil, nil, err
		}
		if len(hits) == 0 {
			return s.text("no definitions recorded for " + in.Path + " (unknown file, unsupported language, or nothing defined)"), nil, nil
		}
		var b strings.Builder
		for _, h := range hits {
			fmt.Fprintf(&b, "%-10s %-45s %s\n", h.Kind, h.Name, h.Detail)
		}
		return s.text(b.String()), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "kuli_broken_imports",
		Description: "Relative or aliased imports whose target file does not exist in the repository (dead or broken references).",
		Annotations: readOnly("Broken imports"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noIn) (*mcp.CallToolResult, any, error) {
		hits, err := s.store.BrokenImports()
		if err != nil {
			return nil, nil, err
		}
		if len(hits) == 0 {
			return s.text("no broken imports found"), nil, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d broken imports:\n", len(hits))
		for _, h := range hits {
			fmt.Fprintf(&b, "%s:%d → %s (missing)\n", h.Path, h.Line, h.Name)
		}
		return s.text(b.String()), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "kuli_rescan",
		Description: "Re-scan the repository and rebuild the knowledge base. Use when results are reported stale or after large changes.",
		Annotations: &mcp.ToolAnnotations{Title: "Rescan repository", IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noIn) (*mcp.CallToolResult, any, error) {
		if err := s.Rescan(); err != nil {
			return nil, nil, err
		}
		st, err := s.store.Stats()
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		b.WriteString("rescan complete\n")
		report.Stats(&b, st)
		return s.text(b.String()), nil, nil
	})
}

// Rescan rebuilds the knowledge base from the working tree.
func (s *Server) Rescan() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := scan.Scan(s.root)
	if err != nil {
		return err
	}
	_, err = s.store.Save(res)
	return err
}

// text wraps a tool answer, prefixing a staleness warning when HEAD moved since the scan.
func (s *Server) text(body string) *mcp.CallToolResult {
	if note := s.staleNote(); note != "" {
		body = note + "\n\n" + body
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: body}}}
}

func (s *Server) staleNote() string {
	scanned := s.store.ScannedCommit()
	head := scan.GitCommit(s.root)
	if scanned == "" || head == "" || scanned == head {
		return ""
	}
	return fmt.Sprintf("⚠ knowledge base is from commit %.10s but HEAD is %.10s — results may be stale; call kuli_rescan to refresh.", scanned, head)
}

func formatHits(hits []store.Hit) string {
	var b strings.Builder
	for _, h := range hits {
		loc := h.Path
		if h.Line > 0 {
			loc = fmt.Sprintf("%s:%d", h.Path, h.Line)
		}
		fmt.Fprintf(&b, "%-10s %-45s %s\n", h.Kind, h.Name, loc)
		if h.Detail != "" {
			fmt.Fprintf(&b, "           %s\n", h.Detail)
		}
	}
	return b.String()
}
