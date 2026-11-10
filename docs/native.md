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
- The workspace comes back. The tabs, how each is split (direction and ratio), custom tab titles, the focused and zoomed pane of each tab and the active tab are saved in `keel-native-workspace.json` in the data directory (session ids, titles and layout only: no host, user, address or secret; mode 0600, written atomically, debounced and on window close). A window reopens with the same tabs and splits. Sessions that are gone drop out of their tab and the tree closes up; running sessions that were in no saved tab become tabs after the saved ones; a missing, unreadable or other-version file gives one tab per session. A restarted `keel serve` gives new session ids, so nothing restores after a daemon restart. Closing the window detaches; sessions keep running.
- Status bar: session count, focused pane size, encoding.
- Focus view: ⌘⇧↵ or the pane bar's Focus view tool shows the focused pane alone and keeps the others' sessions; splitting or closing the zoomed pane leaves it.
- The pane menu, from the pane bar's **More pane actions** tool or a right-click on the pane bar: Split right, Split down, Focus view (with another pane beside it, or while zoomed), Reconnect (for a failed session), Close pane. (Share and the menu on ⇧F10 come later.)
- The tab menu, from a right-click on a tab: Rename tab, Close, Close others, Close to the right, Close all (disabled as in the webview). Rename edits in place: Enter or a click elsewhere keeps the name, Escape cancels, an empty name clears the custom title. Closing a tab kills its panes' sessions.
- Drag a tab onto another to reorder (it lands before or after it by which half the pointer is over); ⌘1…⌘9 follow the order. ⌘⇧[ and ⌘⇧] (also ⌘< and ⌘>) select the previous and next tab and wrap.
- ⌘⇧T reopens the last closed tab or pane as a new session on the same host, in a new tab (up to 10 are remembered for this window; none across a window close).
- ⌘F finds in the focused terminal with the terminal's own search: a find bar above it with the match count, previous and next, and close. Enter and ⌘G go to the next match, ⇧Enter and ⌘⇧G to the previous, Escape closes. Nothing is sent to the session.
- ⌘+ (⌘=), ⌘- and ⌘0 set the terminal text size of every pane, 10 to 22, default 12, kept as `termSize` in `keel-native.json` (the other fields of that file stay as they are).
- Keys: ⌘1…⌘8, ⌘9, ⌘W, ⌘⇧D, ⌘⇧E, ⌘⇧↵, ⌘⇧T, ⌘⇧[ ⌘⇧], ⌘F, ⌘G, ⌘⇧G, ⌘+ ⌘- ⌘0, ⌘B (Ctrl off macOS).
- Window translucency: the window is see-through at the saved opacity (default 66, range 30-100) with a macOS background blur (default 20, range 0-64; a private window-server call that falls back to no blur and logs once to stderr). All three are read from `keel-native.json` in the data directory, `{"opacity": 66, "blur": 20, "termSize": 12}`; the N5 Settings page will write opacity and blur (the text size keys already write `termSize`); `KEEL_NATIVE_OPACITY` and `KEEL_NATIVE_BLUR` override them for development, and an out-of-range value is an error.
- JetBrains Mono is embedded, so the bundle needs no font files beside it.
- A locked or unset vault shows a message: unlock it in Keel, then reopen the window.
- Pane states, with the webview's words. A session that is down (exited, or failed) shows a flare banner, "Session ended", "Connection lost" or "Connection failed", with its endpoint and the server's last message, and **Close** and **Reconnect**. Sessions that were already down when the window opened come back as tabs with that banner. A non-local session that is connecting shows a signal "Connecting…" banner.
- A first connection to an unknown host key, or a changed key, opens a dialog for the focused pane: host, key type, fingerprint (and the saved one for a changed key). **Trust and connect** sends the server's own offer back and reconnects; **Abort** or Escape leaves the pane with its banner. Nothing else trusts a key.
- A session another window also attached shows a role chip (Driving, Owner, Following) in the pane bar. Anyone who is not driving sees a haze banner ("Following · <name> is driving · read-only", or "Nobody is driving") with **Take control** for the session's owner and **Ask for control** for everyone else; keel serve drops a follower's keystrokes. Passing control (Grant) has no UI yet: it is part of the share phase.
- The area outside a tile's rounded corners is chrome under window translucency, once.

## What is missing

| Phase | Scope |
|---|---|
| N2 | Linux build and packaging; release packaging (universal binary, notices, `scripts/release.sh`) is the cutover |
| N3 | Still to do: the banner's Copy logs and Edit host and its connection log, the follower's Leave and passing control (N6), the role chip's icon, the terminal's own right-click menu, the pane menu's Share and ⇧F10, the find bar's Match case (the terminal plugin's search has no case option: it ignores the case of ASCII letters), dropping a tab past the last one or on the empty strip, tab pages, sync input across panes, the sidebar's Panes section, the pane bar's drag to rearrange |
| N4 | Command palette, New tab page (`+` opens the first local host until then), full shortcuts map |
| N5 | Vault: Unlock, First run, Edit host, Keychain, Forwards, Snippets, Logs, Settings pages; live updates; sidebar sections and bottom bar |
| N6 | Share popover, control requests, watchers badge |
| N7 | Agent panel and page, suggestions, Approve before type, runbooks, groups, notes |
| N8 | Notebooks |
| N9 | Cutover: `keel app` opens the native window; Wails, templ, Tailwind and the web assets are removed |

Until N9 every UI change to the webview has a native counterpart to build; this list is the parity ledger. No release ships `keel-native` before N2.
