.PHONY: app build test signal macos-app macos-icon

ADDR ?= 127.0.0.1:47241
GO := sh scripts/go.sh
GO_TAGS := $(shell ./scripts/go-tags.sh)
UNAME_S := $(shell uname -s)
# Finder-launched Keel.app has no shell env. Bake the public signal URL:
#   make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
ifneq ($(strip $(KEEL_SIGNAL)),)
KEEL_LDFLAGS := -ldflags "-X keel/internal/share.DefaultSignal=$(KEEL_SIGNAL)"
endif

app:
	KEEL_ADDR=$(ADDR) $(GO) run $(GO_TAGS) $(KEEL_LDFLAGS) ./cmd/keel app

build:
	$(GO) build $(GO_TAGS) $(KEEL_LDFLAGS) -o bin/keel ./cmd/keel
	go build -o bin/keel-signal ./cmd/keel-signal
ifeq ($(UNAME_S),Darwin)
	$(MAKE) macos-app
endif

macos-app:
	rm -rf bin/Keel.app
	mkdir -p bin/Keel.app/Contents/MacOS bin/Keel.app/Contents/Resources
	cp bin/keel bin/Keel.app/Contents/MacOS/keel
	cp internal/desktop/macos/Info.plist bin/Keel.app/Contents/Info.plist
	cp internal/desktop/macos/Keel.icns bin/Keel.app/Contents/Resources/Keel.icns
	codesign --force --sign - --identifier app.keel.desktop --entitlements internal/desktop/macos/entitlements.plist bin/Keel.app/Contents/MacOS/keel
	codesign --force --sign - --identifier app.keel.desktop --entitlements internal/desktop/macos/entitlements.plist bin/Keel.app

macos-icon:
	cd internal/desktop/macos && go run genicon.go

signal:
	go run ./cmd/keel-signal

test:
	$(GO) test $(GO_TAGS) ./...
