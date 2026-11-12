.PHONY: build linux test clean

# Version injection via -ldflags. Override with: make build VERSION=v2.0.0
VERSION  ?= v1.0.0
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.buildDate=$(BUILD_DATE) \
	-X main.gitCommit=$(GIT_COMMIT)

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o check_sql .

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o check_sql .

test:
	CGO_ENABLED=0 go test -race ./...

clean:
	rm -f check_sql
