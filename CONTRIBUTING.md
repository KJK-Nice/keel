# Contributing to Keel

## Build

```bash
make build && ./bin/keel
# bake a public signal URL into Keel.app (Finder launch has no shell env):
make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
```

`make build` writes `tokens.css` from `docs/design/tokens.json` (the only source of colors, type, radii and sizes), runs `templ generate` and the Tailwind CLI (`assets/css/globals.css` → `cmd/keel/web/css/output.css`), then compiles the Go app. Fonts ship as local woff2 in `cmd/keel/web/fonts/`. Both tools are pinned: templ comes from `go.mod` (`go tool templ`), and the Makefile downloads the Tailwind standalone binary at `TAILWIND_VERSION` into `bin/` on first use. Use your own with `make TAILWIND=tailwindcss …`, but a different version rewrites `output.css`.

### Platform requirements

Ubuntu 24.04+ needs GTK4 + WebKitGTK 6 (Wails v3):

```bash
sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev
```

macOS needs Xcode Command Line Tools (`xcode-select --install`). Use **`make build`** (not a bare `go build`): it links **UniformTypeIdentifiers**, silences Wails's `setShowsBaselineSeparator:` deprecation on macOS 15+, and writes **`bin/Keel.app`** (Dock icon is the Keel chevron). WKWebView on Apple Silicon **SIGTRAPs** a naked unsigned binary (JIT). `./bin/keel` copies itself into `~/Library/Caches/keel/Keel.app`, ad-hoc signs it with JIT entitlements, and re-execs **before** opening WebKit. The UI then starts `keel serve` if needed. Or `open bin/Keel.app`.

Windows uses Edge WebView2. The About menu and Linux window icon use the same mark.

## Checks

The same ones CI runs on every PR:

```bash
make test              # go test, desktop tags
make lint              # gofmt, go vet, staticcheck, govulncheck
make check-generated   # committed *_templ.go and output.css match the pinned tools
```

UI changes are verified end to end with e2e (`npm install`, then `npx e2e run`) against an isolated `keel serve`; see `.claude/skills/verify-keel/SKILL.md`.

## UI work

The UI is being redesigned. Read [`docs/ui-redesign.md`](docs/ui-redesign.md) first, and [`CLAUDE.md`](CLAUDE.md) for the rules on tokens, components and the Datastar stream.

## Code layout

The desktop UI script is `cmd/keel/web/js/term.js`, loaded as an ES module. It holds app state and wiring; the rest lives beside it and is tested with `node --test` (`make test-js`, also part of `make test`):

- `lib/` has no DOM or state: split layouts and their geometry (`layout.js`), vault groups and list markup (`groups.js`, `vaultview.js`), the command palette's filtering (`palette.js`), the session socket protocol (`session.js`), shortcuts, join links, the share joiner (`joinpeer.js`, `sharemac.js`), and the Share popover, people list and follower banner markup (`share.js`).
- `ui/` holds small DOM widgets that take their elements and callbacks as arguments (`palette.js`, `joinprompt.js`, `actiontoast.js`, `sharepop.js` (the Share popover), and `gate.js`: Unlock, First run and the host import, with their markup in `lib/onboard.js`).

README and docs screenshots come from `tests/readme.e2e.ts` (the file says how to refresh them).

`internal/signal/web/` serves copies of `joinpeer.js` and `sharemac.js` to the browser join page; a test fails if they drift from `lib/`.

## Why this shape

Go owns SSH and PTYs. The window is Wails v3 (native webview) pointed at localhost because Wails cannot proxy WebSockets. The chrome is templ + [shadcn-templ](https://shadcn-templ.com); the vault lists reach the page over one [Datastar](https://data-star.dev) stream (`GET /ui/stream`) that patches them whenever the vault changes (`docs/ui-redesign.md` §12). Terminals use xterm.js over WebSocket. Remote follow uses WebRTC so the host HTTP API never leaves loopback. Local Shell starts in `$HOME`.
