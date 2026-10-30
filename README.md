# KuliDev

> **Docs-nya mana?** — every developer who ever inherited a repo.

KuliDev is your tireless dev *kuli*: it reads the codebase nobody documented, builds an
**evidence-backed knowledge graph**, and does the grunt work — so you can get on with the
actual engineering.

Every fact KuliDev records points back to the file and line that proves it. No vibes.

> *kuli* (Indonesian): labourer. As in "kuli coding" — what we call ourselves at 2 a.m.

## Status

Early. **v0.1 is the knowledge engine**: scan, search and dependency queries from the CLI.
Docs generation (`kuli docs`), Q&A, an MCP server for coding agents, and the web UI come next —
see [docs/ROADMAP.md](docs/ROADMAP.md).

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

## What it understands today

| | |
|---|---|
| **Parsed with Tree-sitter** | Go, Python, JavaScript, TypeScript/TSX — functions, methods, classes, structs, interfaces, types, enums, imports |
| **Import resolution** | Go modules (multi-module), Python absolute/relative, JS/TS relative, `index.*`, ESM `.js→.ts`, `@/` `~/` aliases |
| **Detected** | Dockerfile, Compose, Kubernetes YAML, kustomize, Helm, Terraform, GitHub Actions, Jenkins, GitLab CI, SQL migrations, dependency manifests |
| **Storage** | One SQLite file with full-text search; git-ignored by default |

## Design principles

- **Repository-first** — works on the repos you already have; no project format.
- **Evidence-first** — every edge has provenance (file, lines, origin, confidence).
- **Agent-agnostic** — exposes knowledge to any coding agent (MCP coming).
- **Local-first** — a single binary; your code never leaves your machine unless you say so.

## Stack

Go (single binary, Tree-sitter, SQLite/FTS5) + React/TypeScript web UI (embedded, coming).

## License

[Apache-2.0](LICENSE)
