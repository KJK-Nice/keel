.PHONY: run tui app build test

ADDR ?= 127.0.0.1:47241
GO_TAGS := $(shell ./scripts/go-tags.sh)
export CGO_ENABLED := 1
# Wails WKWebView needs UniformTypeIdentifiers on macOS (UTType / drag-and-drop).
# The wails CLI injects this; plain `go build` / `make` does not.
ifeq ($(shell go env GOOS 2>/dev/null),darwin)
	CGO_LDFLAGS += -framework UniformTypeIdentifiers
	export CGO_LDFLAGS
endif

app:
	KEEL_ADDR=$(ADDR) go run $(GO_TAGS) ./cmd/keel app

run:
	KEEL_ADDR=$(ADDR) go run $(GO_TAGS) ./cmd/keel server

tui:
	KEEL_ADDR=$(ADDR) go run $(GO_TAGS) ./cmd/keel tui

build:
	go build $(GO_TAGS) -o bin/keel ./cmd/keel

test:
	go test $(GO_TAGS) ./...
