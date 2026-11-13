# Contributing to Keel

## Build

```bash
make build && ./bin/keel
# bake a public signal URL into Keel.app (Finder launch has no shell env):
make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
```

Keel is pure Go: no cgo, no Node, no generated code. `make build` compiles `bin/keel` (`CGO_ENABLED=0`, the plain binary for development), `bin/keel-signal` and `bin/keel-sshtest`, then runs `make package`: `go tool mygo build` (MyGo's CLI, pinned by the `tool` line of `go.mod`; the app is described by `mygo.json`) writes the shipped app into `build/<os>-<arch>/` for this machine: `Keel.app` and its disk image on macOS; the binary, `.tar.gz` and `.deb` on Linux. `make package PLATFORM=darwin/universal,linux/amd64,linux/arm64` builds other targets (`mygo build` needs no cgo, so any machine builds any of them; the app is signed and put in a disk image on macOS only). The first package makes `THIRD_PARTY_NOTICES` (`scripts/notices.sh`, needs the network; delete the file to make it again). MyGo's CLI downloads libghostty-vt once (SHA-256 checked) and puts it in the package (`Contents/Resources` in the app, beside the binary on Linux), so the packaged app never downloads it; `bin/keel` downloads it into the user cache on first run. `make package` signs ad hoc; `KEEL_SIGN="Developer ID Application: …"` signs with that identity and notarizes the disk image with the keychain profile `notary`. `make app` runs the window from source.

macOS needs the Xcode Command Line Tools for `codesign` (`xcode-select --install`). `make macos-icon` regenerates the icon files in `packaging/macos/` from `Keel.icon`; `mygo.json` points at `packaging/macos/icon.png` and `Keel.icns`.

`keel app` opens the native window (MyGo, libghostty-vt terminals): see `docs/native.md`. `make native-e2e` drives the window under MyGo's tester; `make native-automation` drives the built app from outside through MyGo's automation socket (menu bar, close and reopen, real keys, both themes). It builds `bin/keel-e2e` with the build tags `keelautomation` and `keelfake`, which no release path may pass, opens real windows briefly and runs only on scratch data; see "Driving the built app from outside" in `docs/native.md`.

## Checks

The same ones CI runs on every PR:

```bash
make test              # go test ./... (both CI legs, Ubuntu and macOS, run it, the window's package included)
make lint              # gofmt, go vet, staticcheck, govulncheck
make native-e2e        # the window end to end (macOS); PNGs in .scratch-native/e2e/
```

Set `KEEL_KEYRING=off` (and `KEEL_AGENT_PROVIDER=fake` when the agent is involved) for any command run by a script or an agent. The fake provider is linked only with the build tag `keelfake`, which `make native-e2e` (`bin/keel`) and `make native-automation` (`bin/keel-e2e`) pass and no release path may: any other build refuses to start `keel serve` with `KEEL_AGENT_PROVIDER=fake`, so run `make build` again before using `bin/keel` for anything but tests. UI changes are verified with `make native-e2e` and, for the menu bar and window lifecycle, `make native-automation`; see `.agents/skills/verify-keel/SKILL.md`.

## Releasing

Releases are cut from `main` by pushing a tag; `.github/workflows/release.yml` runs `scripts/release.sh` for each target. The version lives in `mygo.json` and must equal the tag. What a release holds, how to cut one, a local dry build and the signing secrets are in [docs/release.md](docs/release.md); the "What's new" section of `.github/release-notes.md` is the release's own: rewrite it for each tag.

## UI work

Read [`docs/native.md`](docs/native.md) first and [`CLAUDE.md`](CLAUDE.md) for the rules on tokens, fonts and the agent's invariants. [`docs/ui-redesign.md`](docs/ui-redesign.md) holds the screens and design intent the window follows.

## Code layout

- `cmd/keel` is the one binary: `keel app` (the window), `keel serve` (the daemon), `keel mcp` (Claude Code's tools), `keel version`. `cmd/keel-native` is the window alone, attaching to a running `keel serve` (the e2e and automation use it); `cmd/keel-signal` is the signaling server for shared sessions; `cmd/keel-sshtest` is a test SSH server.
- `internal/native` is the window and its tests; `internal/native/keelclient` is its client of the daemon's API and the one place that builds the frames which can lead to bytes in a PTY (`guard_test.go` keeps that true); `internal/native/joinpeer` joins a shared session.
- `internal/httpapi` is the daemon's JSON/WebSocket API; `internal/sessionsock` is the one session-socket handler; `internal/runtime` and `internal/session` own the PTYs and SSH; `internal/store` is the encrypted vault; `internal/agent` is the agent; `internal/share` and `internal/signal` are sharing.
- Leaf packages that the daemon and the window both import, so a rule is written once: `internal/wire` (frames, roles, statuses, reasons), `internal/cmdrule` (command classification and the direct-type grammar), `internal/limits`, `internal/sharelink` (the share link, its MAC and the STUN server). `limits`, `cmdrule` and `sharelink` have a test (`leaf_test.go`) that keeps their imports to a few packages.
- `internal/app` is `keel serve` and `keel app`; the fake agent provider is linked only by the build tag `keelfake` (`agent_fake.go`; `agent_real.go` otherwise).

## Why this shape

Go owns SSH and PTYs. The window is native (`internal/native`, MyGo): a client of `keel serve` on loopback, token-protected, with no web page anywhere. Remote follow uses WebRTC, joined from Keel, so the host's HTTP API never leaves loopback. Local Shell starts in `$HOME`.
