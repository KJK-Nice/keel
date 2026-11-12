## Install

**macOS 13 or later** (Apple Silicon and Intel): download `Keel-*-macos-universal.zip`, unzip it and move **Keel.app** to Applications. Keel is not notarized by Apple yet, so the first launch is blocked: open **System Settings → Privacy & Security**, scroll to "Keel was blocked…" and click **Open Anyway**, or run

```bash
xattr -dr com.apple.quarantine /Applications/Keel.app
```

**Linux**: unpack the archive for your CPU and run `keel`. The window is Keel's own (not a web view), so there is no GTK or WebKitGTK to install; the archive carries `libghostty-vt.so`, the terminal engine, beside the binary. What the desktop itself must provide for a window is MyGo's concern (<https://github.com/egoist/mygo>); Keel's CI does not run on Linux windows, so the Linux build is not checked there.

```bash
tar -xzf keel-*-linux-x86_64.tar.gz   # or linux-arm64
./keel-*/keel
```

**keel-signal**, the server for sharing sessions over the internet, is in the `keel-signal-*` archives; see [Sharing](https://github.com/KJK-Nice/keel/blob/main/docs/sharing.md). Release builds have no signaling server set: Share stays off until you enter one in **Settings → Sharing → Signal server**.

Check a download against `SHA256SUMS` with `shasum -a 256 -c SHA256SUMS --ignore-missing`.
