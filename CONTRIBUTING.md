# Contributing to Keel

## Build

```bash
make build && ./bin/keel
# bake a public signal URL into Keel.app (Finder launch has no shell env):
make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
```

Keel is pure Go: no cgo, no Node, no generated code. `make build` compiles `bin/keel` (`CGO_ENABLED=0`, the plain binary for development), `bin/keel-signal` and `bin/keel-sshtest`, then runs `make package`: `go tool mygo build` (MyGo's CLI, pinned by the `tool` line of `go.mod`; the app is described by `mygo.json`) writes the shipped app into `build/<os>-<arch>/` for this machine: `Keel.app` and its disk image on macOS; the binary, `.tar.gz` and `.deb` on Linux. `make package PLATFORM=darwin/universal,linux/amd64,linux/arm64` builds other targets (`mygo build` needs no cgo, so any machine builds any of them; the app is signed and put in a disk image on macOS only). The first package makes `THIRD_PARTY_NOTICES` (`scripts/notices.sh`, needs the network; delete the file to make it again). MyGo's CLI downloads libghostty-vt once (SHA-256 checked) and puts it in the package (`Contents/Resources` in the app, beside the binary on Linux), so the packaged app never downloads it; `bin/keel` downloads it into the user cache on first run. `make package` signs ad hoc; `KEEL_SIGN="Developer ID Application: …"` signs with that identity and notarizes the disk image with the keychain profile `notary`. `make app` runs the window from source.

macOS needs the Xcode Command Line Tools for `codesign` (`xcode-select --install`). `make macos-icon` regenerates the icon files in `packaging/macos/` from `Keel.icon`; `mygo.json` points at `packaging/macos/icon.png` and `Keel.icns`.

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

Releases are cut from `main` by pushing a tag; `.github/workflows/release.yml` does the rest. First set `"version"` in `mygo.json` to the release (`0.1.0`), commit it, then tag that commit:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The workflow runs `scripts/release.sh` for each target, which calls `go tool mygo build`. The script fails, before anything is published, when the tag and `mygo.json` disagree. It makes `keel-<version>.dmg` (macOS, universal: arm64 + x86_64, `Keel.app` inside), `keel-<version>-linux-{amd64,arm64}.tar.gz` and `keel_<version>_{amd64,arm64}.deb` (one Ubuntu runner builds both) and `keel-signal-<version>-<os>-<cpu>` for macOS, Linux and Windows (plain `go build`). `LICENSE` and `THIRD_PARTY_NOTICES` (`scripts/notices.sh`) are in each; `SHA256SUMS` lists them all. The release is published with the install notes in `.github/release-notes.md` and notes generated from the merged PRs. A tag with a hyphen (`v0.2.0-rc.1`) is a pre-release. Release builds bake in no `KEEL_SIGNAL`. The macOS app is signed ad hoc and not notarized unless the repository has the signing secrets of [docs/release.md](docs/release.md).

A PR that changes the release tooling (`mygo.json`, `packaging/`, the Makefile, the scripts, the workflow) runs the same builds as a dry run and keeps the files as workflow artifacts. Locally, `sh scripts/release.sh v0.1.0 macos|linux|signal` builds into `dist/` (the version of `mygo.json` is 0.1.0); it never uploads. The packages keep MyGo's file names.

## UI work

Read [`docs/native.md`](docs/native.md) first and [`CLAUDE.md`](CLAUDE.md) for the rules on tokens, fonts and the agent's invariants. [`docs/ui-redesign.md`](docs/ui-redesign.md) holds the screens and design intent the window follows.

## Code layout

- `cmd/keel` is the one binary: `keel app` (the window), `keel serve` (the daemon), `keel mcp` (Claude Code's tools), `keel version`. `cmd/keel-native` is the window alone, attaching to a running `keel serve` (the e2e and automation use it); `cmd/keel-signal` is the signaling server for shared sessions; `cmd/keel-sshtest` is a test SSH server.
- `internal/native` is the window and its tests; `internal/native/keelclient` is its client of the daemon's API and the one place that builds the frames which can lead to bytes in a PTY (`guard_test.go` keeps that true); `internal/native/joinpeer` joins a shared session.
- `internal/httpapi` is the daemon's JSON/WebSocket API; `internal/runtime` and `internal/session` own the PTYs and SSH; `internal/store` is the encrypted vault; `internal/agent` is the agent; `internal/share` and `internal/signal` are sharing.

## Why this shape

Go owns SSH and PTYs. The window is native (`internal/native`, MyGo): a client of `keel serve` on loopback, token-protected, with no web page anywhere. Remote follow uses WebRTC, joined from Keel, so the host's HTTP API never leaves loopback. Local Shell starts in `$HOME`.
