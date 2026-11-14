---
name: verify-keel
description: Launch, check, drive and prove Keel's native window (tabs, the Vault sidebar, the command palette, dialogs, the terminal) with `make native-e2e` and `make native-automation`, against a scratch `keel serve`. Use before claiming a Keel UI or httpapi change works, or when asked to verify or screenshot a Keel screen.
---

# Verify Keel

Keel has one UI: the native MyGo window in `internal/native`. `keel serve` serves only its JSON/WebSocket API. What the window does, and how to drive each part, is in `docs/native.md`; read the section for the feature you touched. This skill is how to prove a change.

## Rules that never bend

- Scratch data only. Every check runs on its own data directory and loopback port, with `KEEL_KEYRING=off` (never read or write the OS keychain item) and `KEEL_AGENT_PROVIDER=fake` when the agent is involved. Never point anything at the user's Keel: not `127.0.0.1:47241`, not the default data directory, not `~/.ssh` (the host import reads `KEEL_SSH_DIR`, which the tests point at a copy of `tests/fixtures/ssh-home/.ssh`).
- Stop a daemon you started by its handle or with `KEEL_ADDR=<addr> KEEL_DATA=<dir> ./bin/keel serve stop`, never `pkill keel`: that kills the user's own Keel and its PTYs.
- Never print a token file's contents or a vault passphrase.

## The checks

```bash
make test                # go test ./...: logic and view tests under MyGo's ui.Tester, no window
make native-e2e          # the real view, terminal (libghostty) and client against a scratch keel serve it starts
make native-automation   # the built app driven from outside through MyGo's automation socket (opens real windows briefly)
make lint
```

A UI change is working when `make test` and `make native-e2e` pass and the PNGs look right; add `make native-automation` when the change touches the menu bar, the window's lifecycle or key handling.

## What each layer proves

- **Unit and view tests** (`internal/native/*_test.go`, run by `make test`): the model and the views under `ui.Tester`, which renders in memory and takes pointer and keyboard input (`Click`, `Key`, `Type`, `Press`/`Move`/`Release`, `Image`). They read rendered pixels where the look is the claim: both themes (`theme_test.go` sets the choice and compares against `lightHex` and `darkHex`; `TestTokensMatchJSON` and `TestNoHexColourOutsideTokens` keep colours in the tokens), translucency (`TestGroundsFollowTheWindowAlphaWithoutStacking` asserts the ground alpha equals the window alpha, so a tint painted twice fails), UI size (`uisize_test.go`: it scales rem sizes, not px ones), and that splits fit (`TestSplitsTileTheStage`). `guard_test.go` and the other source-scanning guards hold what can write into a PTY.
- **`make native-e2e`** (`TestE2E…` in `internal/native`, build tag `nativee2e`): the real view, a real terminal and the real client against a daemon each test starts itself. It builds `bin/keel` with the build tag `keelfake` (only such a build has the fake provider; any other refuses to start `keel serve` with `KEEL_AGENT_PROVIDER=fake`), `bin/keel-sshtest` and `bin/keel-signal`, then runs `go test -tags nativee2e -run TestE2E ./internal/native/`. Each test has its own free loopback port and data in `.scratch-native/e2e/<Test>/data`, `KEEL_KEYRING=off`, a scratch vault passphrase and `SHELL=/bin/sh`; it stops the daemon by the handle it started. Effects are checked on the server (`Sessions()`, `/api/sessions/<id>/scrollback`, `/api/hosts`, `/api/logs` with the `X-Keel-Token` header), not only in the model. It writes one PNG per test to `.scratch-native/e2e/<Test>.png` (dark; `TestE2EThemeSwitch` also writes `-light` shots) and the daemon log to `.scratch-native/e2e/<Test>/serve.log`. The directory is git-ignored. Run it twice when a change touches timing. It needs macOS.
- **`make native-automation`** (`tests/native-automation`, build tag `nativeautomation`): `bin/keel-e2e`, built with the tags `keelautomation` and `keelfake` (no release path passes either), driven through MyGo's automation socket in a real window: the menu bar, close and reopen, real keys into a real terminal, both themes, the run-time guard. Its screen-pixel scenarios skip on CI runners. See "Driving the built app from outside" in `docs/native.md`.
- A change to `keel serve` (the httpapi) is also covered by `go test ./internal/httpapi/`; the window and the daemon agree through `internal/native/keelclient`.

Look at the PNGs, in both themes, before you claim a visual change works. The look is native first: judge a screen as a MyGo app against `docs/native.md` "Look" and `docs/design/`, not against any older screenshot. The shots come from MyGo's in-memory renderer, not a real display.

## Adding a scenario

In `internal/native/e2e_test.go` and its siblings (build tag `nativee2e`, test files only):

- `startServe(t, passphrase)` starts the daemon; `newWin(t, addr, dir, wrap)` opens a window on it. Never share a daemon between tests.
- Everything touching the workspace or the Tester runs on the test goroutine. Background work reaches it through `pump(cond, what)`, which runs posted work and frames until `cond` holds. No sleeps outside `pump` and `settle`.
- Type only through `tt.Type` and `tt.Key`, as a person does; never add a function to non-test code that writes into a session.
- Assert on server state where the effect is on the server. Prove "typed nothing" by comparing every session's `scrollback` before and after, and `Sessions()` before and after for "started nothing". End with `shot(name)`.
- Passwords come from the fixture (`f.password`) and go through `tt.Type` only; never print them. For a host key dialog use the fixture host of `cmd/keel-sshtest`.
- To run one scenario: `go test -tags nativee2e -count=1 -run TestE2ESplitAndFocus ./internal/native/`, after `make native-e2e` has built `bin/keel` once (and again after any change to `keel serve`). The first run fetches libghostty-vt into the user cache (SHA-256 checked).

## Doctor

If something looks off: `go build ./... && go vet ./...`, then one e2e test as above. A held lock on a data directory means another daemon uses it.

## Bundle

`make build` (or `make package`) writes the shipped app with `mygo build` into `build/<os>-<arch>/` (`Keel.app` on macOS; `mygo.json`, `packaging/macos/`, libghostty-vt in `Contents/Resources`). Check it as separate commands: the dylib exists in `Contents/Resources`; `lipo -archs` of it and of `Contents/MacOS/Keel` name the architectures built (arm64 and x86_64 for `PLATFORM=darwin/universal`); `codesign --verify --deep --strict` exits 0; `sh scripts/release.sh vX.Y.Z macos` does all of that and checks `keel version`. To check that it launches, run it on scratch data: `env KEEL_ADDR=127.0.0.1:<free port> KEEL_DATA=<scratch dir> KEEL_KEYRING=off KEEL_VAULT_PASSPHRASE=<scratch> SHELL=/bin/sh build/darwin-arm64/Keel.app/Contents/MacOS/Keel app`, wait for the port, then stop it and its `keel serve` as above. Opening the app from Finder is a person-only check.

## What only a person can check

The blur and the real see-through window, the menu chords, Dock and menu bar behaviour, Finder launch, input methods, the system clipboard, the look on a real display, and the window on Linux and Windows. `docs/native.md` ("Only a person can check") has the full list. Say so when a change lives there.

PR before/after screenshots: see `.agents/skills/pr-screenshots/SKILL.md`.
