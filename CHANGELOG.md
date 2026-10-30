# Changelog

## 0.1.0

First release. KuliDev reads a codebase, builds an evidence-backed knowledge graph, and puts
it to work in the CLI, in coding agents (MCP) and in a browser workbench.

- **Knowledge engine** — `kuli scan` parses Go, Python, JavaScript and TypeScript with
  Tree-sitter: definitions, doc comments, imports (resolved across Go modules, Python
  packages, JS/TS paths and aliases), infrastructure and build files. Incremental: a 1.5M-line
  repository rescans in ~90 ms after an edit. `kuli find`, `kuli deps`, `kuli stats`.
- **DocsMana** — `kuli docs` writes overview, architecture (with a component map),
  dependency, infrastructure and getting-started docs with any OpenAI-compatible model.
  Every claim cites file:line; invented citations are removed; secrets are redacted before
  prompting; `kuli docs status` flags docs whose evidence changed.
- **Ask** — `kuli ask "…"` answers questions with validated file:line citations.
- **MCP** — `kuli mcp` gives Claude Code, Codex and other agents search, dependency and
  outline tools over the graph.
- **Workbench** — `kuli serve`: VS Code-style UI with Monaco, knowledge hover and
  cross-file go-to-definition, safe editing (conflict detection, atomic writes), persistent
  terminals, agent sessions with KuliDev attached, source control (diffs, stage, commit,
  branches, pull/push, gutter markers), Map, Docs, Ask, and Pulse for following agents
  from a phone.
- **Remote** — device pairing with one-time codes and QR, revocable sessions, TLS required
  off-loopback, `--public-url` for tunnels such as ngrok.

Known limits: definitions are matched by name (no language servers yet); four languages;
the terminal and agents need Linux or macOS (use WSL on Windows).
