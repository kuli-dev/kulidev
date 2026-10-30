# Changelog

## Unreleased

- **Desktop app** for Windows and macOS: `KuliDev.exe` opens the workbench in a native
  window (WebView2 / WebKit) — no browser, no commands.
- **Open folder**: start without a repository and pick one (recent folders, a folder browser
  that marks repositories, drive letters on Windows); switch folders from the workbench.
  Local only — remote and paired devices stay on the folder they were given.
- Opening a folder shows the full workbench straight away with a progress bar (files
  read of total); a folder opened before is usable at once while it catches up.
- Windows: no more console windows flashing up when KuliDev runs git.
- `kuli serve` without `-C` opens the current folder if it was scanned, otherwise the
  folder picker; opening a folder scans it (incrementally after the first time).

## 0.2.0

- **Java and PHP**: definitions with Javadoc/PHPDoc; Java imports resolved by package path
  under any source root; PHP `use` resolved through composer.json PSR-4 maps or Composer's
  generated `vendor/composer/autoload_psr4.php`, `require`/`include` relative to the file
  for plain paths and `__DIR__`, found anywhere for other bases (`ABSPATH . '…'`).
  Measured on a 2.6M-line Java + PHP tree: every project-internal Java import resolves;
  PHP 69% to files.
- Imports pointing into skipped folders (build/, dist/, vendor/) are no longer reported as
  broken.
- **Pulse**: answer an agent from the card — reply box plus ↑ ↓ Enter Esc Ctrl+C.

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
