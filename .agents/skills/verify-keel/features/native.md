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

## Translucency

Checked by pixel tests, not by eye: `TestGroundsFollowTheWindowAlphaWithoutStacking`
(`internal/native/workspace_test.go`) reads rendered pixels and asserts the
ground alpha equals the window alpha, so a tint painted twice fails. Try other
values with `KEEL_NATIVE_OPACITY` (30-100) and `KEEL_NATIVE_BLUR` (0-64) or the
`keel-native.json` prefs file in the data dir. The blur and the real
see-through window are person-only checks.

## The bundle

`make native-app`, then each as its own command: `"bin/Keel Native.app/Contents/MacOS/keel-native" --library-path`
prints a path inside the bundle; `codesign --verify --deep --strict "bin/Keel Native.app"`
exits 0; `otool -L "bin/Keel Native.app/Contents/MacOS/keel-native" | grep -c libghostty`
prints 0; with a scratch daemon, `env -u MYGO_GHOSTTY_VT KEEL_NATIVE_CAPTURE=.scratch-native/app.png`
and the bundled binary exits 0 and the PNG shows a prompt. Opening it from Finder is person-only.

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

## SSH, host keys and two windows

- `bin/keel-sshtest -out <file>` (built by `make native-e2e`) runs a throwaway SSH server on loopback.
  `startSSH(t, addr, dataDir)` runs it, stops it by its handle, and creates a password host for it with
  `POST /api/hosts` (`noAutoReconnect: true`, so a dropped server stays failed). The client never creates
  hosts. Never print the file or the request body: they hold the throwaway password.
- A first connection stops at "Unknown host key". The test clicks "Trust and connect" in the dialog, as a
  person does (`connectSSH`); it never calls `/api/known-hosts` itself. A `trustToken` is a one-time
  capability: never log it, never put it in a failure message.
- Two windows on one daemon: call `newWin` twice with the same `addr` and `dir`, and drive both with
  `pumpAll(t, []*win{a, b}, cond, what)` (each window's posted work runs on the test goroutine).
- keel serve lets only the session's owner take control, and the window that opened a session owns it. A
  second window is a follower with "Ask for control"; `TestE2EFollowerAndTakeControl` has the owner pass
  control with `GrantControl` (a control frame, never a PTY write), then take it back with "Take control".

## Menus, tab drags and the saved workspace

- Menus: `tt.RightClick("pane bar 1")` (a pane bar), `tt.RightClickAt(x, y)` (a tab pill: equal names are
  equally wide, so `tabRect(i)` offsets the first), then `tt.Menu()` lists the labels (`-` is a separator)
  and `tt.ChooseMenuItem("Close pane")` picks one; a disabled item returns an error, which is how the
  tab menu's disabled items are tested. The More tool is `tt.Click("More pane actions")`.
- Reorder: `Press`, two `Move`s, `Release` from one pill to another's left or right half.
- The saved workspace: close the window with `w.closeWindow()` (it writes the file), then `newWin` again on
  the same `addr` and `dir`: the second window restores from `<dir>/keel-native-workspace.json`. Compare
  trees by session id (`structure`), never by local pane id. `newWin` sets the data dir; a test that
  must not restore uses a fresh `startServe`.
- Find and text size: the find bar's count comes from the terminal (`p.search.status()`); the bar makes
  the terminal a row shorter, so `sh` redraws its prompt: compare scrollbacks through the `redraw`
  replacer. Text size shows on the server as the session's `cols`.

## Not covered

Say so when a change lives there: the OS window (traffic lights, full
screen, title bar drag, the macOS menu), the blur and the see-through window, input
methods, the system clipboard, real GPU text rendering, Linux and Windows.
`KEEL_NATIVE_CAPTURE=<png> ./bin/keel-native` on a real window is the only
check of the page inside a real window, and it does not draw the window
chrome.
