## What's new in v0.2.0

- **The window is native.** Keel's window is drawn by [MyGo](https://github.com/egoist/mygo) with libghostty terminals; the webview is gone, and so is `keel app --webview` (a leftover `--webview` argument or `KEEL_UI=webview` prints one line saying so). The daemon serves no web page.
- **Building needs no GTK or WebKit** (nor cgo, Node or a C toolchain): `make build` is pure Go. On Linux the window needs GTK 3 at run time.
- **Packages.** macOS: one universal `.dmg` (Apple Silicon and Intel). Linux: a `.tar.gz` and a `.deb` for x86_64 (`amd64`) and arm64. Each carries libghostty-vt, so Keel never downloads it.
- **Sharing.** The browser join page is gone: a shared session is joined from Keel, with **Join session** and the link. A link opened in a browser answers one line saying so.
- **The local API** accepts only the `X-Keel-Token` header (no cookie, no query parameter).
- **Known gaps** compared with the old window: the find bar has no Match case, there is no Previous / Next command, and the terminal's right-click menu is the plugin's own (Copy, Paste, Select all, Clear), without Find, Split or Close pane. `docs/native.md` lists what is missing.
- **Linux** packages are built but the Linux window has not been run by the maintainer.

## Install

**macOS 13 or later** (Apple Silicon and Intel): download `keel-*.dmg`, open it and drag **Keel.app** to Applications. Keel is signed ad hoc and not notarized unless this release says it is; macOS then blocks the first launch: open **System Settings → Privacy & Security**, scroll to "Keel was blocked…" and click **Open Anyway** (skip this if macOS opens it without a warning), or run

```bash
xattr -dr com.apple.quarantine /Applications/Keel.app
```

The `keel` command (`keel serve`, `keel mcp`, `keel version`) is `/Applications/Keel.app/Contents/MacOS/Keel`.

**Linux** (x86_64 is `amd64`): the window is Keel's own (not a web view), so there is no WebKitGTK to install; it needs GTK 3, and the package carries `libghostty-vt.so`, the terminal engine, beside the binary. What the desktop itself must provide for a window is MyGo's concern (<https://github.com/egoist/mygo>); Keel's CI does not run on Linux windows, so the Linux build is not checked there.

```bash
# Debian, Ubuntu: /opt/keel, the `keel` command and a menu entry
sudo apt install ./keel_*_amd64.deb      # or _arm64.deb
# any distribution
mkdir keel && tar -xzf keel-*-linux-amd64.tar.gz -C keel && ./keel/keel   # or -arm64
```

**keel-signal**, the server for sharing sessions over the internet, is in the `keel-signal-*` archives; see [Sharing](https://github.com/KJK-Nice/keel/blob/main/docs/sharing.md). Release builds have no signaling server set: Share stays off until you enter one in **Settings → Sharing → Signal server**.

Check a download against `SHA256SUMS` with `shasum -a 256 -c SHA256SUMS --ignore-missing`.
