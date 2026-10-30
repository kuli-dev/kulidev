# Security

KuliDev shows, edits and runs code. Please report vulnerabilities privately through
GitHub's **Report a vulnerability** (Security tab) rather than in public issues.

## Model

- `kuli serve` binds to `127.0.0.1` by default and needs no login there. Requests must carry
  a localhost `Host` header, which defeats DNS rebinding.
- On a network address (or behind `--public-url`) every device must be **paired**: one-time
  codes (10 minutes, single use, rate-limited) become revocable per-device sessions
  (HttpOnly, SameSite=Strict, Secure under TLS; only a hash is stored). TLS is required
  unless `--insecure-http` is given explicitly.
- Writes require same-origin requests carrying `X-Kuli-Request`; WebSockets reject other
  origins. Only files recorded by the scan can be read or written; symlinks are never
  written through; saves detect conflicting changes instead of overwriting them.
- Terminals and agents are off with `--read-only` and off remotely unless
  `--allow-remote-terminal` is given.
- `kuli docs` / `kuli ask` redact credentials (key=value pairs and URL-embedded passwords)
  before anything is sent to a model; the endpoint is yours to choose.
- Secret files (`devices.json`, the TLS key) are mode 0600 on Linux and macOS; on Windows
  they rely on the user profile's access controls.
- `.kulidev/.gitignore` keeps the database, paired devices, TLS keys and agent configs out of
  version control.
