## What's new in v0.2.1

- **Suggestions in panes start only when you ask.** A line you start with `#` (press ⇥ on its notice) or **Suggest a command for this line**, ⌘Y (Ctrl+⇧Y off macOS), also in the palette and on the Keyboard page. Nothing else you type is sent anywhere, and ⇥ stays the shell's. **Notice automatically** is now opt-in in Settings · Agent and, when on, its notice names ⌘Y instead of taking ⇥. Installs that stored the old default are moved once to "Only lines starting with #".
- **A suggestion request can no longer hang.** It ends with the card or an error ("No suggestion came back." after 37 s at the latest).
- **Turning suggestions off takes effect at once** in every open pane, also when it is changed in another window; ⌘Y then says they are off.
- **⌥1–⌥6 on the New tab page** open the card with the caret in the input too, without typing the Option character.
- **A disconnected pane** no longer shows the terminal and a blinking caret behind its overlay; the terminal comes back when the session does.
- **Linux fixes** found by running the window's tests on Ubuntu: the hint after a plain Tab on a destructive suggestion reads "Shift+Tab" (it read "ShiftTab"); the Agent page no longer pushes the Agent panel out of the window beside it; the narrowest sidebar is a little wider so its four labels fit; and the bundled JetBrains Mono and IBM Plex Mono are not used on Linux for now (see docs/native.md, Linux), the system monospace is. The window's own tests now run on Linux in CI too.
- **No window shadow.** The Window shadow setting is gone; the window has a thin border instead (macOS).

## Install

**macOS 13 or later** (Apple Silicon and Intel): download `keel-*.dmg`, open it and drag **Keel.app** to Applications. Keel is signed ad hoc and not notarized unless this release says it is; macOS then blocks the first launch: open **System Settings → Privacy & Security**, scroll to "Keel was blocked…" and click **Open Anyway** (skip this if macOS opens it without a warning), or run

```bash
xattr -dr com.apple.quarantine /Applications/Keel.app
```

The `keel` command (`keel serve`, `keel mcp`, `keel version`) is `/Applications/Keel.app/Contents/MacOS/Keel`.

**Linux** (x86_64 is `amd64`): the window is Keel's own (not a web view), so there is no WebKitGTK to install; it needs GTK 3, and the package carries `libghostty-vt.so`, the terminal engine, beside the binary. What the desktop itself must provide for a window is MyGo's concern (<https://github.com/egoist/mygo>); Keel's CI runs the window's tests on Ubuntu, but nobody has run the window on a Linux desktop yet, so expect rough edges.

```bash
# Debian, Ubuntu: /opt/keel, the `keel` command and a menu entry
sudo apt install ./keel_*_amd64.deb      # or _arm64.deb
# any distribution
mkdir keel && tar -xzf keel-*-linux-amd64.tar.gz -C keel && ./keel/keel   # or -arm64
```

**keel-signal**, the server for sharing sessions over the internet, is in the `keel-signal-*` archives; see [Sharing](https://github.com/KJK-Nice/keel/blob/main/docs/sharing.md). Release builds have no signaling server set: Share stays off until you enter one in **Settings → Sharing → Signal server**.

Check a download against `SHA256SUMS` with `shasum -a 256 -c SHA256SUMS --ignore-missing`.
