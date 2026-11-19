.PHONY: app build slime-binary helpers package native-e2e native-automation native-automation-build test bench signal macos-icon lint fmt vet staticcheck vulncheck

ADDR ?= 127.0.0.1:47241
STATICCHECK_VERSION := v0.8.2
GOVULNCHECK_VERSION := v1.9.0
# Release builds stamp the version into `slime version`:
#   make package SLIME_VERSION=vX.Y.Z
# (the packages carry the version of mygo.json; scripts/release.sh checks the two agree)
# SLIME_VERSION, SLIME_SIGNAL and SLIME_SIGN were KEEL_* before the rename; the old names still work.
SLIME_VERSION ?= $(or $(KEEL_VERSION),dev)
SLIME_SIGNAL ?= $(KEEL_SIGNAL)
SLIME_X := -X main.version=$(SLIME_VERSION)
# Finder-launched Slime.app has no shell env. Bake the public signal URL:
#   make build SLIME_SIGNAL=https://keel-signal-production.up.railway.app
ifneq ($(strip $(SLIME_SIGNAL)),)
SLIME_X += -X keel/internal/share.DefaultSignal=$(SLIME_SIGNAL)
endif
SLIME_LDFLAGS := -ldflags "$(SLIME_X)"
# Slime is pure Go (the native window is MyGo, purego): no cgo, so a build needs no C toolchain and carries no
# macOS deployment target of its own.
# make app opens the native window.
app:
	CGO_ENABLED=0 SLIME_ADDR=$(ADDR) go run $(SLIME_LDFLAGS) ./cmd/slime app

# The one recipe that builds ./cmd/slime into SLIME_OUT, with the build tags in SLIME_TAGS (none for a normal build; the
# test builds below set them for their own target only). bin/slime is the plain binary for development (no bundle:
# libghostty-vt downloads into the user cache on first run).
SLIME_TAGS ?=
SLIME_OUT ?= bin/slime
slime-binary:
	rm -f $(SLIME_OUT)
	CGO_ENABLED=0 go build $(SLIME_LDFLAGS) $(if $(SLIME_TAGS),-tags $(SLIME_TAGS)) -o $(SLIME_OUT) ./cmd/slime
	$(if $(filter bin/slime,$(SLIME_OUT)),ln -sf slime bin/keel) # keel, the command's name before the rename, still works

# The helper binaries next to bin/slime: the signaling server and the test SSH server.
helpers:
	go build -o bin/keel-signal ./cmd/keel-signal
	go build -o bin/keel-sshtest ./cmd/keel-sshtest

# make package builds the shipped app next to bin/slime.
build: slime-binary helpers
	$(MAKE) package

# THIRD_PARTY_NOTICES goes into every package (mygo.json resources). It needs the network, so it is made once;
# delete it to make it again.
THIRD_PARTY_NOTICES:
	sh scripts/notices.sh $@

# The shipped app, made by mygo build (docs/native.md "Packaging"): build/<os>-<arch>/ holds Slime.app and its disk
# image on macOS, the binary, tar.gz and .deb on Linux. PLATFORM is mygo's -platform list (default: this machine's;
# darwin/universal, linux/amd64, linux/arm64 are the release ones, scripts/release.sh). Signed ad hoc and not
# notarized unless SLIME_SIGN names a Developer ID identity. The bundle id stays app.keel.desktop (mygo.json): the
# Keychain items and the user defaults of Slime are tied to it.
PLATFORM ?=
SLIME_SIGN ?= $(KEEL_SIGN)
package: THIRD_PARTY_NOTICES
	MYGO_LDFLAGS="$(SLIME_X)" go tool mygo build $(if $(PLATFORM),-platform $(PLATFORM)) $(if $(SLIME_SIGN),-sign "$(SLIME_SIGN)",-skip-notarize)

macos-icon:
	cd packaging/macos && go run genicon.go

signal:
	go run ./cmd/keel-signal

# The native client end to end: the real view and terminal under MyGo's
# ui.Tester against a scratch slime serve it starts. PNGs: .scratch-native/e2e/.
# The tag keelfake links the agent's test provider (SLIME_AGENT_PROVIDER=fake), which no other build has: this
# bin/slime is a test binary, and make build replaces it.
native-e2e: SLIME_TAGS = keelfake
native-e2e: slime-binary helpers
	go test -tags nativee2e -count=1 -run 'TestE2E' ./internal/native/

# bin/keel-e2e is the test build the automation suite drives: its first tag opens MyGo's automation socket
# (internal/native.CheckAutomationEnv guards it) and the second links the fake agent provider. No release path passes
# either tag.
native-automation-build: SLIME_TAGS = keelautomation,keelfake
native-automation-build: SLIME_OUT = bin/keel-e2e
native-automation-build: slime-binary

# Drives the built app from outside through MyGo's automation socket. Opens real windows briefly.
# Scratch data only; not part of make test or make native-e2e.
native-automation: native-automation-build
	go test -tags nativeautomation -count=1 -timeout 10m ./tests/native-automation/

test:
	go test ./...

# The store's save and lock contention and the scrollback ring (not part of make test).
bench:
	go test ./internal/store -run '^$$' -bench 'NotebookAutosave|ReadIdle|ReadDuringSaves' -benchmem -count 3
	go test ./internal/runtime -run '^$$' -bench RingWrite -benchmem -count 3

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
