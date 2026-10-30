# Changelog

## 0.3.0 (2026-10-30)

The desktop app, multi-repository folders, Settings, and a new look.

- **Desktop app** for Windows and macOS: `KuliDev.exe` opens the workbench in a native
  window (WebView2 / WebKit) — no browser, no commands.
- **Open folder**: start without a repository and pick one (recent folders, a folder browser
  that marks repositories, drive letters on Windows); switch folders from the workbench.
  Local only — remote and paired devices stay on the folder they were given.
- Opening a folder shows the full workbench straight away with a progress bar (files
  read of total); a folder opened before is usable at once while it catches up.
- **Systems**: open a folder that holds several repositories (or a monorepo's apps) and
  KuliDev finds the projects and draws how they connect: code imports (including a
  package another project provides), declared dependencies, and deploy/config files that
  refer to another project (compose services, Kubernetes, env URLs). Every arrow opens
  the file:line evidence. Nested repositories are listed with their own .gitignore.
- **Systems** also matches HTTP calls to routes: FastAPI/Flask, Express/Nest, Go
  (net/http, gin, echo), Spring and Laravel routes against the API paths other projects
  call, segment by segment; each call shows the route it reaches. Projects are found up
  to six folders deep.
- **Git in a folder of several repositories**: the Changes panel lists every repository
  with its branch and change count; pick one to commit, switch branches, pull or push.
  Diffs, gutter markers and staging work for files in any of them.
- **Markdown preview** for .md files (Code / Split / Preview), updating as you type, with
  Mermaid diagrams drawn in place; relative links open the linked file. Docs and Ask
  answers draw their diagrams too. .mmd files preview as a diagram.
- **Diagram editor**: Edit / Delete on every diagram, Insert diagram in the bar. Code,
  canvas and live preview side by side; on the canvas drag shapes in from the palette,
  move nodes, drag between nodes to connect, rename and restyle in the side panel,
  Delete to remove. Changes go into the file as one undoable edit (Apply, or Apply and
  save). Flowcharts are visual; other diagram types are edited as code with the preview.
- **Title bar** like VS Code: File, Edit, View, Go, Terminal and Help menus; a command
  center that opens Go to file; local/remote status; toggles for the side bar, terminal
  panel and Ask panel; Settings. **Command palette** (Ctrl+Shift+P) runs any menu
  command by name.
- **New look and icon**: the KuliDev design system (dark industrial). Charcoal surfaces,
  industrial orange for actions, focus and what is active, yellow kept for evidence and
  waiting agents, red for errors; dark is the default (light and match-system in
  Settings); editor and terminal themes to match. The K logo is the app icon (exe,
  taskbar, window), favicon, phone home-screen icon and activity-bar mark. The Windows
  apps carry version info and are DPI-aware.
- **Settings** (gear at the bottom left, Ctrl+,): the AI model connection (base URL,
  model, API key, reasoning effort, max tokens) with Test connection, applied without a
  restart and saved per user (the key is never shown again); light/dark/system theme;
  folder actions; how KuliDev is running. Only on the computer running KuliDev.
- **Switch folders** with Ctrl+R or the folder name in the status bar: recent folders,
  filtered as you type.
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
