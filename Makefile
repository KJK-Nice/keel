.PHONY: run build test

ADDR ?= 0.0.0.0:47241

run:
	KEEL_ADDR=$(ADDR) go run ./cmd/keel

build:
	go build -o bin/keel ./cmd/keel

test:
	go test ./...
