.PHONY: app build package native-e2e native-automation native-automation-build test signal macos-icon lint fmt vet staticcheck vulncheck

ADDR ?= 127.0.0.1:47241
STATICCHECK_VERSION := v0.8.2
GOVULNCHECK_VERSION := v1.9.0
# Release builds stamp the version into `keel version`:
#   make package KEEL_VERSION=v0.1.0
# (the packages carry the version of mygo.json; scripts/release.sh checks the two agree)
KEEL_VERSION ?= dev
KEEL_X := -X main.version=$(KEEL_VERSION)
# Finder-launched Keel.app has no shell env. Bake the public signal URL:
#   make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
ifneq ($(strip $(KEEL_SIGNAL)),)
KEEL_X += -X keel/internal/share.DefaultSignal=$(KEEL_SIGNAL)
endif
KEEL_LDFLAGS := -ldflags "$(KEEL_X)"
# Keel is pure Go (the native window is MyGo, purego): no cgo, so a build needs no C toolchain and carries no
# macOS deployment target of its own.
# make app opens the native window.
app:
	CGO_ENABLED=0 KEEL_ADDR=$(ADDR) go run $(KEEL_LDFLAGS) ./cmd/keel app

# bin/keel is the plain binary for development (no bundle: libghostty-vt downloads into the user cache on first run).
# make package builds the shipped app next to it.
build:
	rm -f bin/keel
	CGO_ENABLED=0 go build $(KEEL_LDFLAGS) -o bin/keel ./cmd/keel
	go build -o bin/keel-signal ./cmd/keel-signal
	go build -o bin/keel-sshtest ./cmd/keel-sshtest
	$(MAKE) package

# THIRD_PARTY_NOTICES goes into every package (mygo.json resources). It needs the network, so it is made once;
# delete it to make it again.
THIRD_PARTY_NOTICES:
	sh scripts/notices.sh $@

# The shipped app, made by mygo build (docs/native.md "Packaging"): build/<os>-<arch>/ holds Keel.app and its disk
# image on macOS, the binary, tar.gz and .deb on Linux. PLATFORM is mygo's -platform list (default: this machine's;
# darwin/universal, linux/amd64, linux/arm64 are the release ones, scripts/release.sh). Signed ad hoc and not
# notarized unless KEEL_SIGN names a Developer ID identity. The bundle id stays app.keel.desktop (mygo.json): the
# Keychain items and the user defaults of Keel are tied to it.
PLATFORM ?=
KEEL_SIGN ?=
package: THIRD_PARTY_NOTICES
	MYGO_LDFLAGS="$(KEEL_X)" go tool mygo build $(if $(PLATFORM),-platform $(PLATFORM)) $(if $(KEEL_SIGN),-sign "$(KEEL_SIGN)",-skip-notarize)

macos-icon:
	cd packaging/macos && go run genicon.go

signal:
	go run ./cmd/keel-signal

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
