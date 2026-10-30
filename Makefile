VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/kulidev/kulidev/internal/cli.Version=$(VERSION)

.PHONY: build test lint clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/kuli ./cmd/kuli

test:
	go test ./...

lint:
	gofmt -l . | (! grep .) && go vet ./...

clean:
	rm -rf bin dist .kulidev
