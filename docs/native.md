# keel-native

`keel-native` is a second Keel client: a native window drawn with [MyGo](https://github.com/egoist/mygo) and libghostty terminals, not a webview. It is the first step of leaving the webview. The goal is the same features and the same look as the app; the webview app (`keel app`) keeps shipping unchanged until the last phase.

It is a client only. It needs a running `keel serve`, which still owns SSH and the PTYs, and it never reads the keychain or the vault: it sees what `keel serve` returns on loopback, with the token from the data directory.

## Run it

```sh
keel serve &            # or let Keel.app start it
make native             # builds bin/keel-native (pure Go, no cgo)
./bin/keel-native
```

`make native-app` builds `bin/Keel Native.app`: `keel-native` with libghostty-vt beside it in `Contents/MacOS`, so the app never downloads the library. The library comes from MyGo's loader (SHA-256 checked); the bundle is ad-hoc signed, not notarized, and is a developer bundle until the cutover (N9).

`KEEL_DATA` and `KEEL_ADDR` select the data directory and address, as for `keel serve`. From a checkout, run it in the repository root so it finds the JetBrains Mono fonts in `cmd/keel/web/fonts`; elsewhere it falls back to the system monospace. The first run downloads libghostty-vt into the user cache (SHA-256 checked by MyGo).

## Verifying

`make native-e2e` builds `bin/keel` and `bin/keel-sshtest` and drives the real window under MyGo's `ui.Tester` against a scratch `keel serve` it starts per test (scratch data in `.scratch-native/e2e/`, one PNG per scenario). It needs macOS and does not touch your Keel.

## What works

- Host tree grouped by group, with a filter on name, address and username. Click a host to open it in a new tab.
- Tabs with a state dot and a pane count badge; `+` opens the first local host.
- Split → and Split ↓ (another PTY to the same host), drag the gap to resize, close a pane (kills its PTY; the last pane closes the tab).
- Live terminals; the focused tile gets the soft ring and the others dim in a split tab.
- Sessions already running in `keel serve` come back as one tab each. Closing the window detaches; sessions keep running.
- Status bar: session count, focused pane size, encoding.
- Keys: ⌘1…⌘8, ⌘9, ⌘W, ⌘⇧D, ⌘⇧E, ⌘B (Ctrl off macOS).
- Window translucency: the window is see-through at the saved opacity (default 66, range 30-100) with a macOS background blur (default 20, range 0-64; a private window-server call that falls back to no blur and logs once to stderr). Both are read from `keel-native.json` in the data directory, `{"opacity": 66, "blur": 20}`, which the N5 Settings page will write; `KEEL_NATIVE_OPACITY` and `KEEL_NATIVE_BLUR` override them for development, and an out-of-range value is an error.
- JetBrains Mono is embedded, so the bundle needs no font files beside it.
- A locked or unset vault shows a message: unlock it in Keel, then reopen the window.
- Pane states, with the webview's words. A session that is down (exited, or failed) shows a flare banner, "Session ended", "Connection lost" or "Connection failed", with its endpoint and the server's last message, and **Close** and **Reconnect**. Sessions that were already down when the window opened come back as tabs with that banner. A non-local session that is connecting shows a signal "Connecting…" banner.
- A first connection to an unknown host key, or a changed key, opens a dialog for the focused pane: host, key type, fingerprint (and the saved one for a changed key). **Trust and connect** sends the server's own offer back and reconnects; **Abort** or Escape leaves the pane with its banner. Nothing else trusts a key.
- A session another window also attached shows a role chip (Driving, Owner, Following) in the pane bar. A follower sees a haze banner ("Following · <name> is driving · read-only") with **Take control**; keel serve drops a follower's keystrokes and only the session's owner may take control.
- The area outside a tile's rounded corners is chrome under window translucency, once.

## What is missing

| Phase | Scope |
|---|---|
| N2 | Linux build and packaging; release packaging (universal binary, notices, `scripts/release.sh`) is the cutover |
| N3 | Still to do: the banner's Copy logs and Edit host and its connection log, the follower's Ask for control and Leave (N6), the role chip's icon, pane menu, terminal menu, zoom, tab rename and drag, saved workspace layout |
| N4 | Command palette, New tab page (`+` opens the first local host until then), full shortcuts map |
| N5 | Vault: Unlock, First run, Edit host, Keychain, Forwards, Snippets, Logs, Settings pages; live updates; sidebar sections and bottom bar |
| N6 | Share popover, control requests, watchers badge |
| N7 | Agent panel and page, suggestions, Approve before type, runbooks, groups, notes |
| N8 | Notebooks |
| N9 | Cutover: `keel app` opens the native window; Wails, templ, Tailwind and the web assets are removed |

Until N9 every UI change to the webview has a native counterpart to build; this list is the parity ledger. No release ships `keel-native` before N2.
