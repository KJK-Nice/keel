## What's new in v0.2.4

- **In-app updates.** Keel checks for and installs new releases itself: **Check for Updates…** is in the menu bar item and the Keel app menu. This is the first release that has it, so install it by hand once; later releases come to you.
- **The menu bar item** shows keel serve's status and sessions, Keel's and keel serve's versions (choose one to copy both), New Tab, Quick Connect, Check for Updates, Settings and Stop Keel Runtime…. **Settings → About** shows the release you run.
- **A quieter title bar.** Its right holds only the Agent icon (⌘I): the pane already says whose session it is and who drives, and **Share** is in the pane's menu (or the palette).
- **Who else is in a pane.** A pane people watch by link gets a ring and a notch with their names; a click opens the Share panel. In a pane someone else drives, the ring, the notch and the cursor take that person's colour, and the notch has **Ask for control** (or **Take control**). The ring and notch replace the "N watching" badge and the "Following" banner.
- **Long tab names** are cut with an ellipsis and stay inside their tab, with the close button.

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
