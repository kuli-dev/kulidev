# Roadmap

KuliDev is built knowledge-first: the engine must be useful on its own (CLI + MCP) before
the workspace, IDE and remote features are layered on top.

## v0.1 — Knowledge engine (`kuli` CLI)
- [x] Repository walk honouring `.gitignore`; language and infra/manifest detection
- [x] Tree-sitter symbol + import extraction: Go, Python, JavaScript, TypeScript/TSX
- [x] Import resolution to files / Go packages / external modules
- [x] SQLite knowledge graph with provenance on every edge; FTS search
- [x] `kuli scan`, `stats`, `find`, `deps`
- [ ] Filter language stdlibs out of "external modules" (Python, Node builtins)
- [ ] Incremental re-scan by content hash (only changed files)
- [ ] Report broken imports (targets that don't exist) as findings
- [ ] Infra graph: Compose services, K8s Deployments/Services, Dockerfile → image
- [ ] Framework detection: HTTP routes (FastAPI, Express, Next.js, Gin/Chi), ORM models

## v0.2 — DocsMana (handover docs)
- [ ] LLM provider interface: OpenAI-compatible (vLLM/Ollama/LM Studio/…) + Anthropic
- [ ] `kuli docs` → `.kulidev/knowledge/*.md` + Mermaid; every claim cites evidence
- [ ] Freshness: CURRENT / STALE / NEEDS_REVIEW per document, keyed to source commit
- [ ] `kuli ask "…"` grounded in graph + FTS, answers with file:line citations

## v0.3 — Agents
- [ ] `kuli mcp` — MCP server: search, get_dependencies, get_architecture, get_related_files…
- [ ] Agent runtime: CLI adapters (Claude Code, Codex, OpenCode, Aider) on supervised PTYs

## v0.4 — Web UI (`kuli serve`)
- [ ] Embedded React app: architecture graph (React Flow), docs browser, search
- [ ] Persistent terminals + agent sessions that survive client disconnects

## Later
- Desktop shell (Tauri), mobile PWA, remote workspaces + device pairing, Postgres for teams,
  more languages via parser plugins.
