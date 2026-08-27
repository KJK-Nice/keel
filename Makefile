.PHONY: run tui app build test

ADDR ?= 127.0.0.1:47241

app:
	KEEL_ADDR=$(ADDR) go run ./cmd/keel app

run:
	KEEL_ADDR=$(ADDR) go run ./cmd/keel server

tui:
	KEEL_ADDR=$(ADDR) go run ./cmd/keel tui

build:
	go build -o bin/keel ./cmd/keel

test:
	go test ./...
