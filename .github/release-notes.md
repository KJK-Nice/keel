## What's new in v0.3.0

**Keel is now Slime**, with a new logo: a blue slime on a pink-to-blue ground, in the Dock, the menu bar and the window. The repository moved to `KJK-Nice/slime` (the old links redirect), and the command is now `slime`; `keel` still works. If you linked the `keel` command, use Claude Code's `keel mcp`, or installed the Linux package, see "Keel is now Slime" under Install below. Your hosts, vault and settings are untouched.

- **⌘-click a file path to open it** (Control-click on Linux). Click a path printed in a terminal (absolute, `~/`, relative, or `file:line:col`) or a `file://` link. In a local pane the file opens where it is. In a remote pane Slime copies it over the session's own SSH connection into **Downloads › Slime › <host>**, opens the copy and offers **Show in Finder**. Files up to 64 MB; a relative path needs the shell to report its folder (OSC 7). Web links open in the browser, as before.
- **Toasts look new.** They are pills now: a green check on a result, a red alert and red title on a failure with the reason on a second line, and a title too long for one line scrolls sideways (with Reduce Motion it wraps instead).
- **Relaunch Now after an update.** When an update has installed in the background, a toast offers to relaunch. Only the window restarts: `slime serve` keeps every session running.

## Install

**macOS 13 or later** (Apple Silicon and Intel): download `slime-*.dmg`, open it and drag **Slime.app** to Applications. Slime is signed ad hoc and not notarized unless this release says it is; macOS then blocks the first launch: open **System Settings → Privacy & Security**, scroll to "Slime was blocked…" and click **Open Anyway** (skip this if macOS opens it without a warning), or run

```bash
xattr -dr com.apple.quarantine /Applications/Slime.app
```

The `slime` command (`slime serve`, `slime mcp`, `slime version`) is `/Applications/Slime.app/Contents/MacOS/Slime`.

**Keel is now Slime.** An app updated in place keeps its folder, `/Applications/Keel.app`, but its binary is now `Contents/MacOS/Slime`. The command is now `slime` (`slime serve`, `slime mcp`), and `keel` still works as a name for it: if you linked the `keel` command or added `keel mcp` to Claude Code with `…/MacOS/Keel`, point it at `/Applications/Keel.app/Contents/MacOS/Slime`. On Linux the package is now `slime`, in `/opt/slime`, with the `slime` command; remove the old one with `sudo apt remove keel`. Your hosts, vault and settings are untouched.

**Linux** (x86_64 is `amd64`): the window is Slime's own (not a web view), so there is no WebKitGTK to install; it needs GTK 3, and the package carries `libghostty-vt.so`, the terminal engine, beside the binary. What the desktop itself must provide for a window is MyGo's concern (<https://github.com/egoist/mygo>); Slime's CI runs the window's tests on Ubuntu, but nobody has run the window on a Linux desktop yet, so expect rough edges.

```bash
# Debian, Ubuntu: /opt/slime, the `slime` command and a menu entry
sudo apt install ./slime_*_amd64.deb      # or _arm64.deb
# any distribution
mkdir slime && tar -xzf slime-*-linux-amd64.tar.gz -C slime && ./slime/slime   # or -arm64
```

**slime-signal**, the server for sharing sessions over the internet, is in the `slime-signal-*` archives; see [Sharing](https://github.com/KJK-Nice/slime/blob/main/docs/sharing.md). Release builds have no signaling server set: Share stays off until you enter one in **Settings → Sharing → Signal server**.

Check a download against `SHA256SUMS` with `shasum -a 256 -c SHA256SUMS --ignore-missing`.
