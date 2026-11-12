.PHONY: app build native native-app native-e2e native-automation native-automation-build test signal macos-app macos-icon lint fmt vet staticcheck vulncheck

ADDR ?= 127.0.0.1:47241
UNAME_S := $(shell uname -s)
STATICCHECK_VERSION := v0.8.2
GOVULNCHECK_VERSION := v1.9.0
# Release builds stamp the version (keel version, Keel.app's Info.plist):
#   make build KEEL_VERSION=v0.1.0
KEEL_VERSION ?= dev
KEEL_X := -X main.version=$(KEEL_VERSION)
# Finder-launched Keel.app has no shell env. Bake the public signal URL:
#   make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
ifneq ($(strip $(KEEL_SIGNAL)),)
KEEL_X += -X keel/internal/share.DefaultSignal=$(KEEL_SIGNAL)
endif
KEEL_LDFLAGS := -ldflags "$(KEEL_X)"
# A THIRD_PARTY_NOTICES file to put in Keel.app (scripts/notices.sh makes it).
KEEL_NOTICES ?=

# Keel is pure Go (the native window is MyGo, purego): no cgo, so a build needs no C toolchain and carries no
# macOS deployment target of its own.
# make app opens the native window.
app:
	CGO_ENABLED=0 KEEL_ADDR=$(ADDR) go run $(KEEL_LDFLAGS) ./cmd/keel app

build:
	rm -f bin/keel
	CGO_ENABLED=0 go build $(KEEL_LDFLAGS) -o bin/keel ./cmd/keel
	go build -o bin/keel-signal ./cmd/keel-signal
	go build -o bin/keel-sshtest ./cmd/keel-sshtest
ifeq ($(UNAME_S),Darwin)
	$(MAKE) macos-app
endif
# Linux: libghostty-vt.so beside bin/keel (MyGo's loader finds it there; it would otherwise fetch it into the user cache).
ifeq ($(UNAME_S),Linux)
	$(MAKE) native
	cp "$$(./bin/keel-native --library-path)" bin/
endif

# Keel.app opens the native window; libghostty-vt sits beside the binary so the app never downloads it. The library
# comes from MyGo's loader (pinned SHA-256) via bin/keel-native --library-path; KEEL_LIBGHOSTTY=<file> names another
# (scripts/release.sh passes the universal one). The bundle id stays app.keel.desktop: the Keychain items and the
# user defaults of Keel are tied to it.
KEEL_LIBGHOSTTY ?=

macos-app: native
	rm -rf bin/Keel.app
	mkdir -p bin/Keel.app/Contents/MacOS bin/Keel.app/Contents/Resources
	cp bin/keel bin/Keel.app/Contents/MacOS/keel
	cp "$(if $(KEEL_LIBGHOSTTY),$(KEEL_LIBGHOSTTY),$$(./bin/keel-native --library-path))" bin/Keel.app/Contents/MacOS/libghostty-vt.dylib
	codesign --force --sign - bin/Keel.app/Contents/MacOS/libghostty-vt.dylib
	cp packaging/macos/Info.plist bin/Keel.app/Contents/Info.plist
ifneq ($(KEEL_VERSION),dev)
	plutil -replace CFBundleShortVersionString -string $(patsubst v%,%,$(KEEL_VERSION)) bin/Keel.app/Contents/Info.plist
endif
	if [ -f LICENSE ]; then cp LICENSE bin/Keel.app/Contents/Resources/LICENSE; fi
	if [ -n "$(KEEL_NOTICES)" ]; then cp "$(KEEL_NOTICES)" bin/Keel.app/Contents/Resources/THIRD_PARTY_NOTICES; fi
	cp packaging/macos/Keel.icns bin/Keel.app/Contents/Resources/Keel.icns
	rm -rf bin/Keel.app/Contents/Resources/Keel.icon
	cp -R packaging/macos/Keel.icon bin/Keel.app/Contents/Resources/Keel.icon
	if [ -f packaging/macos/Assets.car ]; then cp packaging/macos/Assets.car bin/Keel.app/Contents/Resources/Assets.car; fi
	codesign --force --sign - --identifier app.keel.desktop --entitlements packaging/macos/entitlements.plist bin/Keel.app/Contents/MacOS/keel
	codesign --force --sign - --identifier app.keel.desktop --entitlements packaging/macos/entitlements.plist bin/Keel.app

macos-icon:
	cd packaging/macos && go run genicon.go

signal:
	go run ./cmd/keel-signal

# The native client (MyGo, pure Go). It attaches to a running keel serve.
native:
	rm -f bin/keel-native
	CGO_ENABLED=0 go build -o bin/keel-native ./cmd/keel-native

# Keel Native.app: keel-native with libghostty-vt beside it, so the app
# never downloads it. The library comes from MyGo's loader (pinned SHA-256).
native-app: native
	rm -rf "bin/Keel Native.app"
	mkdir -p "bin/Keel Native.app/Contents/MacOS" "bin/Keel Native.app/Contents/Resources"
	cp bin/keel-native "bin/Keel Native.app/Contents/MacOS/keel-native"
	cp "$$(./bin/keel-native --library-path)" "bin/Keel Native.app/Contents/MacOS/"
	cp internal/native/macos/Info.plist "bin/Keel Native.app/Contents/Info.plist"
	cp packaging/macos/Keel.icns "bin/Keel Native.app/Contents/Resources/Keel.icns"
	if [ -f LICENSE ]; then cp LICENSE "bin/Keel Native.app/Contents/Resources/LICENSE"; fi
	codesign --force --sign - "bin/Keel Native.app/Contents/MacOS/libghostty-vt.dylib"
	codesign --force --sign - --identifier app.keel.native "bin/Keel Native.app"

# The native client end to end: the real view and terminal under MyGo's
# ui.Tester against a scratch keel serve it starts. PNGs: .scratch-native/e2e/.
native-e2e:
	rm -f bin/keel
	CGO_ENABLED=0 go build -o bin/keel ./cmd/keel
	go build -o bin/keel-sshtest ./cmd/keel-sshtest
	go build -o bin/keel-signal ./cmd/keel-signal
	go test -tags nativee2e -count=1 -run 'TestE2E' ./internal/native/

native-automation-build:
	rm -f bin/keel-e2e
	CGO_ENABLED=0 go build -tags keelautomation -o bin/keel-e2e ./cmd/keel

# Drives the built app from outside through MyGo's automation socket (plan 029). Opens real windows briefly.
# Scratch data only; not part of make test or make native-e2e.
native-automation: native-automation-build
	go test -tags nativeautomation -count=1 -timeout 10m ./tests/native-automation/

test:
	go test ./...

fmt:
	@out=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$out" ]; then echo "gofmt needed:" >&2; echo "$$out" >&2; exit 1; fi

vet:
	go vet ./...

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

lint: fmt vet staticcheck vulncheck
