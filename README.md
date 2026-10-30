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

## Quick start

```sh
make build            # → bin/kuli  (needs Go 1.23+ and a C compiler for Tree-sitter)

kuli scan ./my-repo   # build the knowledge base in my-repo/.kulidev/
kuli -C my-repo stats
kuli -C my-repo find payment service          # camelCase / snake_case aware
kuli -C my-repo deps app/core/database.py --reverse   # who depends on this?
```

Example (a 1.5M-line Python monolith, ~6 s on a laptop):

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

## Web UI — `kuli serve`

```sh
kuli serve            # http://127.0.0.1:7878
```

- **Overview**: language mix, the files everything depends on, third-party modules, infra files.
- **Map**: interactive component graph; click a component to see what it depends on, who
  uses it, and its files with their doc comments.
- **Search** and a **file viewer** where cited lines are highlighted — every citation in
  docs and answers opens here.
- **Docs** (DocsMana output with stale warnings), **Ask**, and **Issues** (broken imports).

It binds to localhost by default because it shows your source code; the API refuses
cross-origin writes, and only files recorded by the scan can be read. The UI (React +
TypeScript, `web/`) is embedded in the binary — no Node needed to run it.

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
