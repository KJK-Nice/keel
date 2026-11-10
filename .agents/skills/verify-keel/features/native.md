# Native client (keel-native)

The MyGo window in `internal/native`, a second client of `keel serve`
(`docs/native.md`). It is verified with Go tests, not with e2e or a browser.

## What is driven

`keel-native`'s real view, real terminal view (libghostty) and real client,
under MyGo's `ui.Tester`, which renders the view in memory and sends it
pointer and keyboard input (`Click`, `Key`, `Type`, `Press`/`Move`/`Release`,
`Image`). Each test starts its own scratch `keel serve` from `bin/keel`: a
free loopback port, data in `.scratch-native/e2e/<test>/data`,
`KEEL_KEYRING=off`, a scratch vault passphrase, `SHELL=/bin/sh`. It stops it
by the handle it started, never by name. Effects are checked on the server
(`Sessions()`, `/api/sessions/<id>/scrollback`), not only in the model.

## How to run

```bash
make native-e2e    # builds bin/keel, then go test -tags nativee2e ... TestE2E
go test -tags nativee2e -count=1 -run TestE2ESplitAndFocus ./internal/native/
```

The second form needs `bin/keel`: run `make native-e2e` once, and again after
any change to `keel serve`. The first run fetches libghostty-vt into the user
cache. A native change is working only when `make native-e2e` and
`go test -count=1 ./internal/native/...` both pass.

## Evidence

`.scratch-native/e2e/<Test>.png` (one per test) and the daemon log
`.scratch-native/e2e/<Test>/serve.log`. Look at the PNGs before claiming a
visual change works, next to `docs/images/hero-dark.png` or the matching
webview screenshot. The tree is git-ignored.

## Adding a scenario

In `internal/native/e2e_test.go` (build tag `nativee2e`, test files only):

- `startServe(t, passphrase)` starts the daemon; `newWin(t, addr, dir, wrap)`
  opens a window on it. Never share a daemon between tests.
- Everything touching the workspace or the Tester runs on the test goroutine.
  Background work reaches it through `pump(cond, what)`, which runs posted
  work and frames until `cond` holds. No sleeps outside `pump` and `settle`.
- Type only through `tt.Type` and `tt.Key`, as a person does; never add a
  function to non-test code that writes into a session.
- Assert on server state where the effect is on the server. End with
  `shot(name)`.

## Not covered

Say so when a change lives there: the OS window (traffic lights, full
screen, title bar drag, the macOS menu), window translucency and blur, input
methods, the system clipboard, real GPU text rendering, Linux and Windows.
`KEEL_NATIVE_CAPTURE=<png> ./bin/keel-native` on a real window is the only
check of the page inside a real window, and it does not draw the window
chrome.
