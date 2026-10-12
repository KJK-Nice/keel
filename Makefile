.PHONY: app build test signal macos-app macos-icon ui lint fmt vet staticcheck vulncheck check-generated

ADDR ?= 127.0.0.1:47241
GO := sh scripts/go.sh
GO_TAGS := $(shell ./scripts/go-tags.sh)
UNAME_S := $(shell uname -s)
TEMPL := go tool templ
UNAME_M := $(shell uname -m)
# Pinned so every machine and CI builds the same output.css. The standalone
# binary is downloaded into bin/ on first use; override TAILWIND to use your own.
TAILWIND_VERSION := v4.1.17
ifeq ($(UNAME_S),Darwin)
TAILWIND_OS := macos
else
TAILWIND_OS := linux
endif
ifneq ($(filter arm64 aarch64,$(UNAME_M)),)
TAILWIND_ARCH := arm64
else
TAILWIND_ARCH := x64
endif
TAILWIND_BIN := bin/tailwindcss-$(TAILWIND_VERSION)
TAILWIND ?= $(TAILWIND_BIN)
STATICCHECK_VERSION := v0.8.2
GOVULNCHECK_VERSION := v1.9.0
# Finder-launched Keel.app has no shell env. Bake the public signal URL:
#   make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
ifneq ($(strip $(KEEL_SIGNAL)),)
KEEL_LDFLAGS := -ldflags "-X keel/internal/share.DefaultSignal=$(KEEL_SIGNAL)"
endif

$(TAILWIND_BIN):
	mkdir -p bin
	curl -fsSL -o $@.tmp https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH)
	chmod +x $@.tmp
	mv $@.tmp $@

ui: $(if $(filter $(TAILWIND_BIN),$(TAILWIND)),$(TAILWIND_BIN))
	$(TEMPL) generate
	$(TAILWIND) -i ./assets/css/globals.css -o ./cmd/keel/web/css/output.css --minify

app: ui
	KEEL_ADDR=$(ADDR) $(GO) run $(GO_TAGS) $(KEEL_LDFLAGS) ./cmd/keel app

build: ui
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
	rm -rf bin/Keel.app/Contents/Resources/Keel.icon
	cp -R internal/desktop/macos/Keel.icon bin/Keel.app/Contents/Resources/Keel.icon
	if [ -f internal/desktop/macos/Assets.car ]; then cp internal/desktop/macos/Assets.car bin/Keel.app/Contents/Resources/Assets.car; fi
	codesign --force --sign - --identifier app.keel.desktop --entitlements internal/desktop/macos/entitlements.plist bin/Keel.app/Contents/MacOS/keel
	codesign --force --sign - --identifier app.keel.desktop --entitlements internal/desktop/macos/entitlements.plist bin/Keel.app

macos-icon:
	cd internal/desktop/macos && go run genicon.go

signal:
	go run ./cmd/keel-signal

test: ui
	$(GO) test $(GO_TAGS) ./...

# Fails when the committed templ output or output.css differs from what the
# pinned tools generate.
check-generated: ui
	@git diff --exit-code -- '*_templ.go' cmd/keel/web/css/output.css || \
		(echo "generated files are stale: run make ui and commit the result" >&2; exit 1)

fmt:
	@out=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$out" ]; then echo "gofmt needed:" >&2; echo "$$out" >&2; exit 1; fi

vet:
	$(GO) vet $(GO_TAGS) ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) $(GO_TAGS) ./...

vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) $(GO_TAGS) ./...

lint: fmt vet staticcheck vulncheck
