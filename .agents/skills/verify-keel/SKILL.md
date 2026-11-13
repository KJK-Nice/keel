---
name: verify-keel
description: Launch, check, drive and prove Keel's native window (tabs, the Vault sidebar, the command palette, dialogs, the terminal) with `make native-e2e` and `make native-automation`, against a scratch `keel serve`. Use before claiming a Keel UI or httpapi change works, or when asked to verify or screenshot a Keel screen.
---

# Verify Keel

Keel has one UI: the native MyGo window in `internal/native` (the Wails webview and the web page the daemon served were removed in cutover stage 2). `keel serve` serves only its JSON/WebSocket API. What is true of the window and how each feature behaves is in [features/native.md](features/native.md) and `docs/native.md`; read the section for the feature you touched.

## Rules that never bend

- Scratch data only. Every check runs on its own data directory and loopback port, with `KEEL_KEYRING=off` (never read or write the OS keychain item) and `KEEL_AGENT_PROVIDER=fake` when the agent is involved. Never point anything at the user's Keel: not `127.0.0.1:47241`, not the default data directory, not `~/.ssh` (the host import reads `KEEL_SSH_DIR`, which the tests point at a copy of `tests/fixtures/ssh-home/.ssh`).
- Stop a daemon you started by its handle or with `KEEL_ADDR=<addr> KEEL_DATA=<dir> ./bin/keel serve stop`, never `pkill keel`: that kills the user's own Keel and its PTYs.
- Never print a token file's contents.

## The checks

```bash
go test -count=1 ./internal/native/...   # logic and view tests under MyGo's ui.Tester, no window
make native-e2e                          # the real view, terminal (libghostty) and client against a scratch keel serve it starts
make native-automation                   # the built app driven from outside through MyGo's automation socket (opens real windows briefly)
```

- `make native-e2e` builds `bin/keel` (the build tag `keelfake`: only such a build has the fake provider, and any other refuses to start `keel serve` with `KEEL_AGENT_PROVIDER=fake`), `bin/keel-sshtest` and `bin/keel-signal`, then runs `go test -tags nativee2e -run TestE2E ./internal/native/`. It writes one PNG per test to `.scratch-native/e2e/<Test>.png` and the daemon log to `.scratch-native/e2e/<Test>/serve.log`. Look at the PNGs, in both themes, before you claim a visual change works. Run it twice when a change touches timing.
- `make native-automation` builds `bin/keel-e2e` (the build tags `keelautomation` and `keelfake`, which no release path passes) and runs `tests/native-automation/`: the menu bar, close and reopen, real keys, both themes. See "Driving the built app from outside" in `docs/native.md`. It is not part of `make test`.
- A change to `keel serve` (the httpapi) is also covered by `go test ./internal/httpapi/`; the window and the daemon agree through `internal/native/keelclient`.

## Doctor

If something looks off: `go build ./... && go vet ./...`, then `make native-e2e` for one test (`go test -tags nativee2e -count=1 -run TestE2ESplitAndFocus ./internal/native/`, after `make native-e2e` has built `bin/keel` once). A held lock on a data directory means another daemon uses it. The first run fetches libghostty-vt into the user cache (SHA-256 checked).

## Drive

There is no browser and no web page to drive. A person drives the real window; an agent drives it with the tester (`ui.Tester`: `Click`, `Key`, `Type`, `Image`) in a `TestE2E…` test, or from outside with the automation socket in `tests/native-automation/`. Check effects on the server (`Sessions()`, `/api/sessions/<id>/scrollback`, `/api/hosts`, `/api/logs` with the `X-Keel-Token` header), not only in the model. To add a scenario see "Adding a scenario" in [features/native.md](features/native.md).

## Bundle

`make build` (or `make package`) writes the shipped app with `mygo build` into `build/darwin-<arch>/Keel.app` on macOS (`mygo.json`, `packaging/macos/`, libghostty-vt in `Contents/Resources`). To check that it launches, run it on scratch data: `env KEEL_ADDR=127.0.0.1:<free port> KEEL_DATA=<scratch dir> KEEL_KEYRING=off KEEL_VAULT_PASSPHRASE=<scratch> SHELL=/bin/sh build/darwin-arm64/Keel.app/Contents/MacOS/Keel app`, wait for the port, then stop it and its `keel serve` as above. Opening the app from Finder is a person-only check.

## What only a person can check

The blur and the real see-through window, the menu chords, Dock and menu bar behaviour, Finder launch and the look on a real display. Say so when a change lives there.

PR before/after screenshots: see `.agents/skills/pr-screenshots/SKILL.md`.
