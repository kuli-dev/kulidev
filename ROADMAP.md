# Roadmap

What is planned, roughly in order. Done items move to the CHANGELOG.

## Next

- **Release v0.3.0**: the desktop app (KuliDev.exe / macOS KuliDev) in the release
  archives, once tested on Windows.
- **Windows terminal and agents**: ConPTY support, so the terminal, Pulse and agents work
  on Windows without WSL.
- **Code signing** for the Windows and macOS apps (no SmartScreen / Gatekeeper warning).
- **Systems**: gRPC/proto and message queues (topics, queue names) as links; GraphQL;
  routes mounted with prefixes set in another file (include_router, app.use).

## Understanding code

- PHP class index (PHP imports resolve 69% today).
- More languages: C#, Rust, Kotlin, Ruby.
- Go-to-definition through language servers (LSP) where one is installed.
- Infrastructure graph: services, images, ports and env from compose/Kubernetes/Terraform.

## Workbench

- Settings for startup options (read-only, terminals, remote access) from the UI.
- Multi-root workspaces: several unrelated folders open side by side.
- Search and replace across files.

## Product

- Make the repository public (needs the owner's go-ahead).
- Auto-update for the desktop app.
