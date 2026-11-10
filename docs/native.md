# keel-native

`keel-native` is a second Keel client: a native window drawn with [MyGo](https://github.com/egoist/mygo) and libghostty terminals, not a webview. It is the first step of leaving the webview. The goal is the same features and the same look as the app; the webview app (`keel app`) keeps shipping unchanged until the last phase.

It is a client only. It needs a running `keel serve`, which still owns SSH and the PTYs, and it never reads the keychain or the vault: it sees what `keel serve` returns on loopback, with the token from the data directory.

## Run it

```sh
keel serve &            # or let Keel.app start it
make native             # builds bin/keel-native (pure Go, no cgo)
./bin/keel-native
```

`KEEL_DATA` and `KEEL_ADDR` select the data directory and address, as for `keel serve`. From a checkout, run it in the repository root so it finds the JetBrains Mono fonts in `cmd/keel/web/fonts`; elsewhere it falls back to the system monospace. The first run downloads libghostty-vt into the user cache (SHA-256 checked by MyGo).

## Verifying

`make native-e2e` builds `bin/keel` and drives the real window under MyGo's `ui.Tester` against a scratch `keel serve` it starts per test (scratch data in `.scratch-native/e2e/`, one PNG per scenario). It needs macOS and does not touch your Keel.

## What works

- Host tree grouped by group, with a filter on name, address and username. Click a host to open it in a new tab.
- Tabs with a state dot and a pane count badge; `+` opens the first local host.
- Split → and Split ↓ (another PTY to the same host), drag the gap to resize, close a pane (kills its PTY; the last pane closes the tab).
- Live terminals; the focused tile gets the soft ring and the others dim in a split tab.
- Sessions already running in `keel serve` come back as one tab each. Closing the window detaches; sessions keep running.
- Status bar: session count, focused pane size, encoding.
- Keys: ⌘1…⌘8, ⌘9, ⌘W, ⌘⇧D, ⌘⇧E, ⌘B (Ctrl off macOS).
- A locked or unset vault shows a message: unlock it in Keel, then reopen the window.

## What is missing

| Phase | Scope |
|---|---|
| N2 | Packaging: libghostty-vt bundled and signed in `Keel.app`, fonts embedded, Linux build; translucency settings (Window opacity and blur) |
| N3 | Pane states and control: banners (exited, reconnecting, host key), follower role and Take control, pane menu, terminal menu, zoom, tab rename and drag, saved workspace layout |
| N4 | Command palette, New tab page (`+` opens the first local host until then), full shortcuts map |
| N5 | Vault: Unlock, First run, Edit host, Keychain, Forwards, Snippets, Logs, Settings pages; live updates; sidebar sections and bottom bar |
| N6 | Share popover, control requests, watchers badge |
| N7 | Agent panel and page, suggestions, Approve before type, runbooks, groups, notes |
| N8 | Notebooks |
| N9 | Cutover: `keel app` opens the native window; Wails, templ, Tailwind and the web assets are removed |

Until N9 every UI change to the webview has a native counterpart to build; this list is the parity ledger. No release ships `keel-native` before N2.
