.PHONY: app build test signal

ADDR ?= 127.0.0.1:47241
GO := sh scripts/go.sh
GO_TAGS := $(shell ./scripts/go-tags.sh)

app:
	KEEL_ADDR=$(ADDR) $(GO) run $(GO_TAGS) ./cmd/keel app

build:
	$(GO) build $(GO_TAGS) -o bin/keel ./cmd/keel
	go build -o bin/keel-signal ./cmd/keel-signal

signal:
	go run ./cmd/keel-signal

test:
	$(GO) test $(GO_TAGS) ./...
