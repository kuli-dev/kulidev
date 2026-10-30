# Contributing to KuliDev

## Setup

- Go (see `go.mod`) and a C compiler (Tree-sitter is C): `build-essential` on Linux,
  Xcode command line tools on macOS, MinGW on Windows.
- Node 22 only if you change the web UI.

```sh
make build        # bin/kuli
make test         # Go tests + web type-check
make web          # rebuild the UI into internal/web/dist (commit the result)
cd web && npm run dev   # UI dev server on :5173, proxying the API of `kuli serve` on :7878
```

The built UI is committed so `go build` / `go install` work without Node; CI fails if
`internal/web/dist` is out of date.

## Pull requests

- Keep changes focused, with tests for behaviour changes (`go test -race ./...`).
- Anything that writes files, runs processes or listens on the network needs a test for
  the refusal paths too (see `internal/server/server_test.go`).
- Sign off your commits (`git commit -s`) to certify the
  [Developer Certificate of Origin](https://developercertificate.org/).
