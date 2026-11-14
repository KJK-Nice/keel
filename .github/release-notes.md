## What's new in v0.2.2

- **`keel serve stop` keeps its token safe.** It sends the token only to a server that proves it holds it, and never follows a redirect with it.
- **Settings keep the last value you set.** Moving a slider or the sidebar edge quickly could leave an older value in `keel-native.json`; the last one now always wins, and a change still waiting when the window closes is saved.
- **A notebook that cannot be saved when the window closes now says so.**
- **Logs has a Sharing filter** for share events, which showed only under All before.
- **Terminal sizes are capped at 1000 columns and rows** instead of wrapping to a wrong size.
- **Under the hood:** the daemon and the window now share one vocabulary package, and the code that decides when Keel may type for you, the approve dialog, the command table, preferences and pages each live in one place. The security tests were tightened so a moved name cannot quietly disable them. Nothing else should look different; tell us if something does.

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
