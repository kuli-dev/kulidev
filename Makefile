VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/kuli-dev/kulidev/internal/cli.Version=$(VERSION)

.PHONY: build web test lint clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/kuli ./cmd/kuli

# Rebuild the embedded UI (needs Node). The built files are committed so a plain
# `go build` works without Node.
web:
	cd web && npm ci && npm run build

test:
	go test ./...
	cd web && npx tsc -b --noEmit

lint:
	gofmt -l . | (! grep .) && go vet ./...

clean:
	rm -rf bin dist .kulidev
