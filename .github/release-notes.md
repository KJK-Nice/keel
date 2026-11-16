## What's new in v0.2.3

Fixes from a full audit of Keel (issues #144–#164).

- **The vault is harder to lose.** Restoring an old `keel.json` next to an existing vault no longer lets Unlock replace the vault; a failed read of the OS keychain no longer makes a new key over the real one, and Unlock tries the keychain again; host ids can no longer collide with Keel's own vault entries; a save that fails leaves nothing half-changed; restoring a deleted notebook cannot lose it.
- **Sharing.** A viewer you passed control to goes back to watching when your last Keel window closes; requests for control are paced; a joiner's name is cleaned before it is shown; a short network blip no longer ends a join, and a host that never answers ends it after 30 s.
- **keel-signal** limits the rooms one client can hold (set `KEEL_SIGNAL_CLIENT_IP` on a public server, see docs/sharing.md) and a stalled connection can no longer hold up the server.
- **Notebooks.** Text is saved when you close a pane or quit after a failed save, quitting saves every notebook at once within a few seconds, and typing during a delete is not saved over it.
- **Panes.** A pane whose connection to keel serve drops shows Connection lost, and Reconnect opens it again; a window that opens before keel serve is ready starts by itself once it is; a flaky start no longer rewrites your saved tabs.
- **Smaller fixes:** model provider requests never follow a redirect (the API key stays with the provider); a long outage no longer fills the scrollback with retry notes; Start after Stop on a forward that is still connecting keeps it running; a damaged `keel-native.json` is kept as `.bad` and replaced; `keel mcp` survives an oversized request.

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
