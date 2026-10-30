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
- [x] Incremental re-scan: size/mtime → hash → re-parse only changed files; imports re-resolved from stored specs (3.5 s → 90 ms on 1.5M lines); `--full` to rebuild
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
- [x] Agent runtime: CLI adapters on supervised PTYs (see v0.5)

## v0.4 — Web UI (`kuli serve`)
- [x] `kuli serve`: embedded React app — overview, component map (React Flow + dagre), search, file viewer with cited-line highlighting, docs with freshness, ask, broken imports
- [ ] Persistent terminals + agent sessions that survive client disconnects

## v0.5 — Workbench (VS Code-style)
- [x] Activity bar, explorer tree, tabs, breadcrumbs, status bar, Ctrl+P quick open
- [x] Monaco editor (read-only) with knowledge hover + cross-file go-to-definition
- [x] Outline / Imported by panels; Ask Kuli chat panel; Map, Docs, Welcome as tabs
- [x] Edit + save files: content-hash conflict detection, atomic writes, background re-index, --read-only
- [x] Integrated terminal: xterm.js + server PTY over WebSocket; sessions persist and replay scrollback on reconnect
- [x] Agent sessions in the workbench terminal: Claude Code (--mcp-config) and Codex (-c mcp_servers…) start with kuli MCP attached; OpenCode/Aider detected
- [x] Lazy-load Monaco, the map and the terminal (first download 1,026 KB → 103 KB gzipped)

## v0.7 — Source control
- [x] Changes view: status, badge, side-by-side diffs, stage/unstage/discard, commit via the user's git
- [x] `.kulidev/.gitignore` covers devices, TLS keys and agent configs (was database only)
- [ ] Branches: switch, create; pull/push with the user's credentials
- [ ] Inline change markers in the editor gutter; blame on hover

## v0.6 — Remote & mobile
- [x] Device pairing: one-time codes (10 min, single use, rate-limited) + terminal QR; per-device revocable sessions
- [x] TLS required off-loopback (self-signed generator, own cert, or explicit --insecure-http for Tailscale/WireGuard)
- [x] Devices view: list, revoke, sign out, invite another device with a QR code
- [ ] Verify and tune the phone layout on real devices (responsive rules exist, untested on a phone)
- [ ] Phone-first views: agent status, approvals, diffs

## Later
- Desktop shell (Tauri), mobile PWA, remote workspaces + device pairing, Postgres for teams,
  more languages via parser plugins.
