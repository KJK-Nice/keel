## Install

**macOS 13 or later** (Apple Silicon and Intel): download `Keel-*-macos-universal.zip`, unzip it and move **Keel.app** to Applications. Keel is not notarized by Apple yet, so the first launch is blocked: open **System Settings → Privacy & Security**, scroll to "Keel was blocked…" and click **Open Anyway**, or run

```bash
xattr -dr com.apple.quarantine /Applications/Keel.app
```

**Linux** (Ubuntu 24.04 or later, or another distro with GTK 4 and WebKitGTK 6): install the libraries, then run `keel` from the archive for your CPU.

```bash
sudo apt install libgtk-4-1 libwebkitgtk-6.0-4
tar -xzf keel-*-linux-x86_64.tar.gz   # or linux-arm64
./keel-*/keel
```

**keel-signal**, the server for sharing sessions over the internet, is in the `keel-signal-*` archives; see [Sharing](https://github.com/KJK-Nice/keel/blob/main/docs/sharing.md). Release builds have no signaling server set: Share stays off until you enter one in **Settings → Sharing → Signal server**.

Check a download against `SHA256SUMS` with `shasum -a 256 -c SHA256SUMS --ignore-missing`.
