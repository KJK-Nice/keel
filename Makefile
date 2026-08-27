.PHONY: app build test signal macos-app

ADDR ?= 127.0.0.1:47241
GO := sh scripts/go.sh
GO_TAGS := $(shell ./scripts/go-tags.sh)
UNAME_S := $(shell uname -s)

app:
	KEEL_ADDR=$(ADDR) $(GO) run $(GO_TAGS) ./cmd/keel app

build:
	$(GO) build $(GO_TAGS) -o bin/keel ./cmd/keel
	go build -o bin/keel-signal ./cmd/keel-signal
ifeq ($(UNAME_S),Darwin)
	$(MAKE) macos-app
endif

macos-app:
	rm -rf bin/Keel.app
	mkdir -p bin/Keel.app/Contents/MacOS
	cp bin/keel bin/Keel.app/Contents/MacOS/keel
	cp internal/desktop/macos/Info.plist bin/Keel.app/Contents/Info.plist
	codesign --force --sign - --identifier app.keel.desktop --entitlements internal/desktop/macos/entitlements.plist bin/Keel.app/Contents/MacOS/keel
	codesign --force --sign - --identifier app.keel.desktop --entitlements internal/desktop/macos/entitlements.plist bin/Keel.app

signal:
	go run ./cmd/keel-signal

test:
	$(GO) test $(GO_TAGS) ./...
