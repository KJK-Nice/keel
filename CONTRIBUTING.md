# Contributing to Keel

## Build

```bash
make build && ./bin/keel
# bake a public signal URL into Keel.app (Finder launch has no shell env):
make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
```

Keel is pure Go: no cgo, no Node, no generated code. `make build` compiles `bin/keel` (`CGO_ENABLED=0`), `bin/keel-signal` and `bin/keel-sshtest`; on macOS it also writes `bin/Keel.app` from `packaging/macos/` (Info.plist, icon, entitlements) with libghostty-vt beside the binary; on Linux it puts `libghostty-vt.so` in `bin/`. MyGo's loader downloads libghostty-vt once (SHA-256 checked); the app and the release archives carry it, so they never download. `make app` runs the window from source.

macOS needs the Xcode Command Line Tools for `codesign` (`xcode-select --install`). `make macos-icon` regenerates the icon files in `packaging/macos/` from `Keel.icon`.

`keel app` opens the native window (MyGo, libghostty-vt terminals): see `docs/native.md`. `make native-e2e` drives the window under MyGo's tester; `make native-automation` drives the built app from outside through MyGo's automation socket (menu bar, close and reopen, real keys, both themes). It builds `bin/keel-e2e` with the build tag `keelautomation`, which no release path may pass, opens real windows briefly and runs only on scratch data; see "Driving the built app from outside" in `docs/native.md`.

## Checks

The same ones CI runs on every PR:

```bash
make test              # go test ./...
make lint              # gofmt, go vet, staticcheck, govulncheck
make native-e2e        # the window end to end (macOS); PNGs in .scratch-native/e2e/
```

Set `KEEL_KEYRING=off` (and `KEEL_AGENT_PROVIDER=fake` when the agent is involved) for any command run by a script or an agent. UI changes are verified with `make native-e2e` and, for the menu bar and window lifecycle, `make native-automation`; see `.claude/skills/verify-keel/SKILL.md`.

## Releasing

Releases are cut from `main` by pushing a tag; `.github/workflows/release.yml` does the rest:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

It builds `Keel-<v>-macos-universal.zip` (arm64 + x86_64, ad-hoc signed, not notarized), `keel-<v>-linux-{x86_64,arm64}.tar.gz` and `keel-signal` for macOS, Linux and Windows with `scripts/release.sh`, puts `LICENSE` and `THIRD_PARTY_NOTICES` (`scripts/notices.sh`) in each archive, and publishes a GitHub Release with `SHA256SUMS`, the install notes in `.github/release-notes.md` and notes generated from the merged PRs. A tag with a hyphen (`v0.2.0-rc.1`) is a pre-release. Release builds bake in no `KEEL_SIGNAL`.

A PR that changes the release tooling runs the same builds as a dry run and keeps the archives as workflow artifacts. Locally, `sh scripts/release.sh v0.0.0-test app` builds this machine's archive into `dist/`.

## UI work

Read [`docs/native.md`](docs/native.md) first and [`CLAUDE.md`](CLAUDE.md) for the rules on tokens, fonts and the agent's invariants. [`docs/ui-redesign.md`](docs/ui-redesign.md) holds the screens and design intent the window follows.

## Code layout

- `cmd/keel` is the one binary: `keel app` (the window), `keel serve` (the daemon), `keel mcp` (Claude Code's tools), `keel version`. `cmd/keel-native` is the window alone, attaching to a running `keel serve` (the e2e and automation use it); `cmd/keel-signal` is the signaling server for shared sessions; `cmd/keel-sshtest` is a test SSH server.
- `internal/native` is the window and its tests; `internal/native/keelclient` is its client of the daemon's API and the one place that builds the frames which can lead to bytes in a PTY (`guard_test.go` keeps that true); `internal/native/joinpeer` joins a shared session.
- `internal/httpapi` is the daemon's JSON/WebSocket API; `internal/runtime` and `internal/session` own the PTYs and SSH; `internal/store` is the encrypted vault; `internal/agent` is the agent; `internal/share` and `internal/signal` are sharing.

## Why this shape

Go owns SSH and PTYs. The window is native (`internal/native`, MyGo): a client of `keel serve` on loopback, token-protected, with no web page anywhere. Remote follow uses WebRTC, joined from Keel, so the host's HTTP API never leaves loopback. Local Shell starts in `$HOME`.
