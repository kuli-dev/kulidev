# KuliDev

> **Docs-nya mana?** — every developer who ever inherited a repo.

KuliDev is your tireless dev *kuli*: it reads the codebase nobody documented, builds an
**evidence-backed knowledge graph**, and does the grunt work — so you can get on with the
actual engineering.

Every fact KuliDev records points back to the file and line that proves it. No vibes.

> *kuli* (Indonesian): labourer. As in "kuli coding" — what we call ourselves at 2 a.m.

## Status

Early. **v0.1 is the knowledge engine**: scan, search and dependency queries from the CLI,
an MCP server so coding agents can use the same knowledge, DocsMana handover docs and
`kuli ask` for cited answers, and a web UI (`kuli serve`). What's next is in [docs/ROADMAP.md](docs/ROADMAP.md).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/kuli-dev/kulidev/main/install.sh | sh
```

Linux and macOS (x86-64 and ARM); Windows zips are on the releases page (the terminal and
agents need WSL there). The script verifies the SHA-256 checksum before installing to
`~/.local/bin`. From source: `make build` (Go and a C compiler for Tree-sitter).

## Quick start

```sh

kuli scan ./my-repo   # build the knowledge base in my-repo/.kulidev/ (later runs are incremental)
kuli -C my-repo stats
kuli -C my-repo find payment service          # camelCase / snake_case aware
kuli -C my-repo deps app/core/database.py --reverse   # who depends on this?
```

Example (a 1.5M-line Python monolith: ~3.5 s for the first scan, ~90 ms to refresh after an edit):

```
Symbols    42,610 functions · 14,588 methods · 5,939 classes
Imports    17,789 internal edges · 198 external modules
Infra/Build SQL migration ×580 · Kubernetes manifest ×41 · Dockerfile ×5 …

Most depended-on (internal):
   796 ← app/core/database.py
   374 ← app/core/exceptions.py
```

## DocsMana — `kuli docs` (docs-nya mana? ini.)

```sh
export KULI_LLM_BASE_URL=http://localhost:11434/v1   # any OpenAI-compatible endpoint:
export KULI_LLM_MODEL=qwen3:8b                       # Ollama, vLLM, LM Studio, llama.cpp, OpenRouter, OpenAI…
kuli docs                 # → .kulidev/knowledge/*.md
kuli docs status          # which docs went STALE because the code they cite changed
```

Writes **project overview, architecture (with a Mermaid component map), dependency map,
infrastructure and getting-started** docs. The model only sees facts extracted from the
graph, each tagged `[E#]`; every citation it writes is validated and turned into a
`file:line` link, invented ones are removed, and each doc records its citation coverage.
Credentials in config files are redacted before anything is sent to the model. With no
model configured you still get facts-only docs.

## Ask — `kuli ask "…"`

```sh
kuli ask "what happens when the model returns an empty answer?"
```

```
When the model returns an empty answer, Client.Chat detects it and returns ErrEmptyReply.
1. Detection: after unmarshaling, Chat checks whether the stripped content is empty (internal/llm/client.go:174-178).
2. If the finish reason was "length" it returns ErrThinkingBudget instead (internal/llm/client.go:175-176).
3. Ask propagates the error without producing an answer (internal/ask/ask.go:119-120).
kuli: 67% of claims cited (5 citations)
```

The model turns your question into code-ish keywords; KuliDev searches names, doc comments
and the words used inside function bodies, re-ranks by how much of the question each
definition covers, and shows the model the relevant code (long functions are cut down to
the lines that matter). Citations are validated exactly like DocsMana's. Without a model,
`kuli ask` prints the evidence it found.

## Workbench — `kuli serve`

```sh
kuli serve            # http://127.0.0.1:7878
```

A VS Code-style workbench over the knowledge graph:

- **Explorer** tree, **Ctrl+P** quick open, editor **tabs** with breadcrumbs, and the
  Monaco editor (VS Code's editor) with syntax highlighting.
- **Knowledge in the editor**: hover a name to see where it's defined and its doc comment;
  **Ctrl+click** jumps to the definition in any file (same-language matches first).
- **Outline** and **Imported by** for the active file, docked under the explorer.
- **Ask Kuli** chat panel (**Ctrl+L**): answers cite lines; clicking a citation opens the
  file with those lines highlighted.
- **Edit and save** with **Ctrl+S**. Saves never overwrite silently: if the file changed on
  disk since you opened it (git pull, another editor, an agent), you get a conflict and
  choose *Reload from disk* or *Overwrite with mine*. Writes are atomic, and the knowledge
  graph refreshes in the background. `kuli serve --read-only` turns editing off.
- **Changes** (**Ctrl+Shift+G**): git status with a count badge, side-by-side diffs (inline on
  phones), stage / unstage / discard per file or all, commit (Ctrl+Enter), switch or create
  branches, fetch, pull (fast-forward only) and push (never forced). Changed lines are marked
  in the editor margin. It runs your own `git`, so hooks, identity, credentials and config
  apply. Discarding an untracked file asks first, since it deletes it.
- **Pulse** — built for checking from your phone: each agent's state (*working*, *waiting for
  you*, *finished*), its last lines of output, a reply box with ↑ ↓ Enter Esc Ctrl+C for its prompts, and pending changes; **Review** shows every diff
  in one scroll with *Commit all* / *Discard all*. Optional notifications when an agent goes
  quiet or exits (needs https or localhost).
- **Integrated terminal** (**Ctrl+`**): real shells on the server that keep running when
  you close the panel or the browser; reopening replays their recent output.
- **Agents**: start Claude Code or Codex in the workspace terminal with KuliDev's MCP
  server already attached, so the agent can search the code and check who depends on what
  before it changes anything. OpenCode and Aider are detected and launched too.
- **Map** (component graph), **Docs** (with stale warnings), **Issues** (broken imports) and
  a **Welcome** overview as tabs or side views. **Ctrl+Shift+F** search, **Ctrl+B** side bar.

It binds to localhost by default because it shows (and edits) your source code. Requests
must carry a localhost `Host` header (so DNS-rebinding sites can't reach it), writes must be
same-origin with a custom header, only files recorded by the scan can be read or written,
and symlinks are never written through. Terminals are off with `--read-only` or
`--no-terminal`, and on a non-loopback address unless you pass `--allow-remote-terminal`. The UI (React +
TypeScript + Monaco, `web/`) is embedded in the binary — no Node needed to run it.

## From your phone or another computer

```sh
kuli serve --addr 0.0.0.0:7878 --tls-self-signed     # or --tls-cert/--tls-key
kuli serve --addr 0.0.0.0:7878 --insecure-http       # only on an encrypted network (Tailscale, WireGuard)
```

Or put a tunnel in front (no firewall or port-forwarding changes):

```sh
ngrok http 7878                                            # note the https URL it prints
kuli serve --public-url https://<name>.ngrok-free.app      # kuli stays on localhost
```

`--public-url` treats the workspace as remote: pairing is required, only that host name is
accepted, cookies are `Secure`, and the terminal stays off unless `--allow-remote-terminal`.

On a network address every device must be **paired**: `kuli serve` prints a one-time code,
the URLs that carry it (Tailscale first) and a QR code — scan it with your phone. Codes expire
after 10 minutes and work once; wrong guesses are rate-limited. Each device gets its own
revocable session (an HttpOnly, SameSite=Strict cookie; only a hash is stored, in
`.kulidev/devices.json`, mode 0600). Paired devices can invite more, and revoke any device,
from the **Devices** view. KuliDev refuses to listen on a network address without TLS unless
you say `--insecure-http`, and terminals stay off there unless you add `--allow-remote-terminal`.

## Use it from your coding agent (MCP)

```sh
claude mcp add kulidev -- kuli mcp -C /path/to/repo     # Claude Code
```

`kuli mcp` scans the repo on first start, then serves:

| Tool | What the agent gets |
|---|---|
| `kuli_overview` | languages, symbol counts, infra files, hotspots |
| `kuli_search` | definitions/files/modules matching words, with `file:line` |
| `kuli_dependencies` | what a file/package/module imports, or **who imports it** (impact analysis) |
| `kuli_file_symbols` | outline of a file with line ranges — no need to read it all |
| `kuli_broken_imports` | imports pointing at files that don't exist |
| `kuli_rescan` | rebuild after changes |

Every answer warns the agent when `HEAD` has moved since the last scan.

## What it understands today

| | |
|---|---|
| **Parsed with Tree-sitter** | Go, Python, JavaScript, TypeScript/TSX — functions, methods, classes, structs, interfaces, types, enums, Go package vars/consts, imports, **doc comments & docstrings** |
| **Import resolution** | Go modules (multi-module), Python absolute/relative/namespace packages, JS/TS relative, `index.*`, `package.json` entry points, ESM `.js→.ts`, `@/` `~/` aliases |
| **Detected** | Dockerfile, Compose, Kubernetes YAML, kustomize, Helm, Terraform, GitHub Actions, Jenkins, GitLab CI, SQL migrations, dependency manifests |
| **Storage** | One SQLite file; FTS5 with stemming over names, docs and body words; git-ignored by default |

## Design principles

- **Repository-first** — works on the repos you already have; no project format.
- **Evidence-first** — every edge has provenance (file, lines, origin, confidence).
- **Agent-agnostic** — exposes knowledge to any coding agent over MCP.
- **Local-first** — a single binary; your code never leaves your machine unless you say so.

## Stack

Go (single binary, Tree-sitter, SQLite/FTS5) + React/TypeScript web UI embedded in the binary.

## License

[Apache-2.0](LICENSE)
