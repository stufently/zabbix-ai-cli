# Builds and tests run inside a container, so a host with an older Go toolchain
# still produces a correct binary. The MCP SDK requires Go 1.25.
GO_IMAGE   ?= golang:1.26
BINARY     := zabbix-ai-cli
PKG        := ./cmd/zabbix-ai-cli
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X github.com/stufently/zabbix-ai-cli/internal/cli.Version=$(VERSION)
UID        := $(shell id -u)
GID        := $(shell id -g)
CACHE      ?= $(CURDIR)/.gocache

# Containers write into bind mounts as the invoking user, so build artefacts and
# caches stay owned by the person who ran make rather than by root.
GO = docker run --rm -u $(UID):$(GID) \
	-v $(CURDIR):/src -v $(CACHE):/cache \
	-e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOFLAGS=-buildvcs=false \
	-w /src $(GO_IMAGE)

.PHONY: all build test race vet fmt fmt-check lint tidy docker clean install help

all: fmt-check vet test build

## build: compile the binary into ./bin
build:
	@mkdir -p bin $(CACHE)
	$(GO) go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)
	@echo "built bin/$(BINARY) $(VERSION)"

## test: run the test suite
test:
	@mkdir -p $(CACHE)
	$(GO) go test ./...

## race: run the test suite under the race detector
race:
	@mkdir -p $(CACHE)
	$(GO) go test -race ./...

## vet: run go vet
vet:
	@mkdir -p $(CACHE)
	$(GO) go vet ./...

## fmt: rewrite sources with gofmt
fmt:
	$(GO) gofmt -w .

## fmt-check: fail if anything is unformatted
fmt-check:
	@out=$$($(GO) gofmt -l .); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## lint: run golangci-lint
lint:
	@mkdir -p $(CACHE)
	docker run --rm -u $(UID):$(GID) -v $(CURDIR):/src -v $(CACHE):/cache \
		-e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e GOLANGCI_LINT_CACHE=/cache/lint \
		-w /src golangci/golangci-lint:latest-alpine golangci-lint run

## tidy: tidy go.mod and go.sum
tidy:
	$(GO) go mod tidy

## docker: build the container image
docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) -t $(BINARY):latest .

## install: copy the binary into ~/bin
install: build
	@mkdir -p $(HOME)/bin
	install -m 0755 bin/$(BINARY) $(HOME)/bin/$(BINARY)
	@echo "installed $(HOME)/bin/$(BINARY)"

## clean: remove build artefacts
clean:
	rm -rf bin dist

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
