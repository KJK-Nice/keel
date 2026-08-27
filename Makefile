.PHONY: run tui app build test

ADDR ?= 127.0.0.1:47241
GO_TAGS := $(shell ./scripts/go-tags.sh)

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
