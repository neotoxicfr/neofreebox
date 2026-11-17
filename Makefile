.PHONY: dev build test test-race lint vuln check tidy docker

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Needs a paired app token: neofreebox pair -o freebox.token
dev:
	NEOFREEBOX_APP_TOKEN_FILE=./freebox.token NEOFREEBOX_DATA_DIR=./data go run ./cmd/neofreebox serve

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/neofreebox ./cmd/neofreebox

test:
	go test -timeout 120s ./...

test-race:
	go test -race -timeout 120s ./...

lint:
	golangci-lint run

vuln:
	govulncheck ./...

# What CI runs, before pushing.
check: build test-race lint vuln

tidy:
	go mod tidy

docker:
	docker build --build-arg VERSION=$(VERSION) -t neofreebox:$(VERSION) .
