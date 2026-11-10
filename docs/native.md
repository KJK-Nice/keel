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

- Host tree grouped by group, with a filter on name, address and username. Click a host to open it in a new tab (or in the New tab that is showing).
- Tabs with a state dot and a pane count badge; `+` and ⌘T open a New tab (below).
- Split → and Split ↓ (another PTY to the same host), drag the gap to resize, close a pane (kills its PTY; the last pane closes the tab).
- Live terminals; the focused tile gets the soft ring and the others dim in a split tab.
- The workspace comes back. The tabs, how each is split (direction and ratio), custom tab titles, the focused and zoomed pane of each tab and the active tab are saved in `keel-native-workspace.json` in the data directory (session ids, titles and layout only: no host, user, address or secret; mode 0600, written atomically, debounced and on window close). A window reopens with the same tabs and splits. Sessions that are gone drop out of their tab and the tree closes up; running sessions that were in no saved tab become tabs after the saved ones; a missing, unreadable or other-version file gives one tab per session. A restarted `keel serve` gives new session ids, so nothing restores after a daemon restart. Closing the window detaches; sessions keep running.
- Status bar: session count, focused pane size, encoding.
- Focus view: ⌘⇧↵ or the pane bar's Focus view tool shows the focused pane alone and keeps the others' sessions; splitting or closing the zoomed pane leaves it.
- The pane menu, from the pane bar's **More pane actions** tool or a right-click on the pane bar: Split right, Split down, Focus view (with another pane beside it, or while zoomed), Reconnect (for a failed session), Close pane. (Share and the menu on ⇧F10 come later.)
- The tab menu, from a right-click on a tab: Rename tab, Close, Close others, Close to the right, Close all (disabled as in the webview). Rename edits in place: Enter or a click elsewhere keeps the name, Escape cancels, an empty name clears the custom title. Closing a tab kills its panes' sessions.
- Drag a tab onto another to reorder (it lands before or after it by which half the pointer is over); ⌘1…⌘9 follow the order. ⌘⇧[ and ⌘⇧] (also ⌘< and ⌘>) select the previous and next tab and wrap.
- ⌘⇧T reopens the last closed tab or pane as a new session on the same host, in a new tab or in the New tab that is showing (up to 10 are remembered for this window; none across a window close).
- The command palette, ⌘K or ⌘⇧P, a modal styled as the webview's (`k-palette`): the input, then **Open now** (every tab, and each pane of a split tab, with its state dot; choosing one focuses it and never starts a session), **Hosts** (↵ a new session in a new tab, ⌥↵ in a split right of the focused pane) and **Actions**, with the webview's titles, section hints and key caps. Every word of the query must appear in a row's title, hint, group or keys; the matched text is highlighted. ↑ ↓ wrap and keep the row in view, ↵ runs, esc (or ⌘K again) closes and returns the keyboard to the pane. While it is open no key reaches a terminal and no window shortcut acts. Actions it has: New tab, New Local Shell, Close pane or tab, Reopen last tab, Previous tab, Next tab, First tab, Last tab, Toggle vault sidebar, Quick connect, Split right, Split down, Focus view (zoom pane), Find in terminal, Larger text, Smaller text, Reset text size.
- The New tab page (⌘T, `+`, and what an empty window shows; closing the last tab leaves one, and ⌘W on the lone New tab does nothing). "Where to?", one input (a saved host, `user@host:port`, or a pasted `ssh …` command), the recent host cards (up to 6, `⌥1`-`⌥6` open them; the local shell is always first; a query filters every host by name, group, address and user, recent ones first), the **Start** action Quick connect (⌘⇧O), and **Recently closed** (up to 3, with their age; a row, or ⌘⇧T for the newest, starts a new session on that host). ↵ opens the highlighted card (↑ ↓ move it). Opening a host fills the New tab that is showing. A New tab is not written to the saved workspace. Recent hosts are kept as host ids only, newest first, up to 12, as `recentHosts` in `keel-native.json`.
- A pasted command is parsed, never run (`internal/native/sshparse`, a port of `lib/sshparse.js` with the same cases as its test): no shell, no `ssh` binary, no key file read, no lookup. The page shows "Read as `user@host:port`. Nothing has run.", what was **left out** and why (a command that runs something, host-key trust changes, forwards, a jump host, config files, a remote command) and a key-file note. **Connect once** (needs a user) opens the Quick connect dialog, as in the webview: the target is prefilled, the user types the password (never taken from the pasted text), and **Connect** saves a password host named `user@host` in the group "Quick connect" with `POST /api/hosts` and opens it; the host key dialog follows. Quick connect (⌘⇧O, the Start action, or the palette) is the same dialog with an empty target; its target is read with the same parser, which takes `user@host`, `user@host:port`, `ssh://…` and a pasted command.
- ⌘F finds in the focused terminal with the terminal's own search: a find bar above it with the match count, previous and next, and close. Enter and ⌘G go to the next match, ⇧Enter and ⌘⇧G to the previous, Escape closes. Nothing is sent to the session.
- ⌘+ (⌘=), ⌘- and ⌘0 set the terminal text size of every pane, 10 to 22, default 12, kept as `termSize` in `keel-native.json` (the other fields of that file stay as they are).
- Keys: ⌘1…⌘8, ⌘9, ⌘W, ⌘T, ⌘K, ⌘⇧P, ⌘⇧O, ⌘⇧D, ⌘⇧E, ⌘⇧↵, ⌘⇧T, ⌘⇧[ ⌘⇧], ⌘F, ⌘G, ⌘⇧G, ⌘+ ⌘- ⌘0, ⌘B, and ⌥1…⌥6 on the New tab page (Ctrl off macOS).
- Window translucency: the window is see-through at the saved opacity (default 66, range 30-100) with a macOS background blur (default 20, range 0-64; a private window-server call that falls back to no blur and logs once to stderr). All three are read from `keel-native.json` in the data directory, `{"opacity": 66, "blur": 20, "termSize": 12}` (the file also holds `recentHosts`, a list of host ids); the N5 Settings page will write opacity and blur (the text size keys already write `termSize`); `KEEL_NATIVE_OPACITY` and `KEEL_NATIVE_BLUR` override them for development, and an out-of-range value is an error.
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
| N4 | Done: the palette and the New tab page. Still to do, each waiting on the phase that has its feature: **palette actions** left out (the webview's list, `term.js` paletteCatalog): Toggle agent panel, Agent conversations, Runbooks (N7); Group <name> · roles and runbooks (N7); Search hosts (no shortcut to focus the sidebar filter yet); New host, Import hosts from ~/.ssh/config, Keychain, Port forwarding, Snippets, Notebooks, Logs, Settings (N5; notebooks N8); Join session, Share session (join link), Stop sharing this session (N6); Split notebook, New notebook, Open notebook <name> (N8); Toggle sync input, Previous command, Next command (shell integration marks); Stop Keel runtime, Log shortcut keys (debug); **the palette's** Snippets group ("Type snippet: <name>", Approve before type) and the "Ask the agent about …" row with ⌘↵ (N7); **Start actions** left out: Join session ⌘J (N6), New host ⌘N (N5), and the spec's Ask agent ⌘I (N7); **the pasted command's** Save as host (↵ on a read command, and the page's help text that mentions it) and its prefilled New host dialog (N5); a host's tags and folder path in the card search; "· identity" in a card's subtitle; the recently closed list across a window close; the rest of the shortcuts map (⌘N, ⌘J, ⌘,, ⌘I). Not in the webview's palette, so not added: Rename tab and Reconnect (they live in the tab and pane menus). |
| N5 | Vault: Unlock, First run, Edit host, Keychain, Forwards, Snippets, Logs, Settings pages; live updates; sidebar sections and bottom bar |
| N6 | Share popover, control requests, watchers badge |
| N7 | Agent panel and page, suggestions, Approve before type, runbooks, groups, notes |
| N8 | Notebooks |
| N9 | Cutover: `keel app` opens the native window; Wails, templ, Tailwind and the web assets are removed |

Until N9 every UI change to the webview has a native counterpart to build; this list is the parity ledger. No release ships `keel-native` before N2.
