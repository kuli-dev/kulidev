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
- [x] Report broken imports (targets that don't exist) as findings
- [ ] Infra graph: Compose services, K8s Deployments/Services, Dockerfile → image
- [ ] Framework detection: HTTP routes (FastAPI, Express, Next.js, Gin/Chi), ORM models

## v0.2 — DocsMana (handover docs)
- [x] LLM provider: any OpenAI-compatible endpoint (vLLM/Ollama/LM Studio/llama.cpp/OpenRouter/OpenAI)
- [x] `kuli docs` → `.kulidev/knowledge/*.md` + Mermaid; citations validated, coverage recorded, secrets redacted
- [x] Freshness: `kuli docs status` → CURRENT / STALE per document, keyed to evidence file hashes
- [x] `kuli ask "…"`: keyword expansion, body-word recall, re-ranking, focused code excerpts, validated file:line citations
- [x] Doc comments / docstrings in the graph, search and DocsMana facts
- [ ] `kuli_ask` over MCP? (agents can already search; revisit if useful)

## v0.3 — Agents
- [x] `kuli mcp` — MCP server: overview, search, dependencies, file outline, broken imports, rescan
- [ ] MCP: get_architecture / get_related_files once the infra + route graph exists
- [ ] Agent runtime: CLI adapters (Claude Code, Codex, OpenCode, Aider) on supervised PTYs

## v0.4 — Web UI (`kuli serve`)
- [ ] Embedded React app: architecture graph (React Flow), docs browser, search
- [ ] Persistent terminals + agent sessions that survive client disconnects

## Later
- Desktop shell (Tauri), mobile PWA, remote workspaces + device pairing, Postgres for teams,
  more languages via parser plugins.
