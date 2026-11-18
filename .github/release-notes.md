## What's new in v0.2.5

**Settings → Terminal** grows from four rows to five groups:

- **Typography.** Pick any monospaced font installed on your machine, after Keel's own five. **Text size** and **Weight** are number fields; **Line height** goes from 100 to 200%. The preview draws in that font.
- **Advanced font.** **Ligatures** on or off, **Font features** (OpenType tags such as `ss01, cv02, -calt`), **Cell width** (80–150%) and, on macOS, **Thicken**.
- **Colors.** **Improve text contrast** (Off, Some, High) lightens or darkens text a program draws too faint. **Selection background** and **Selection text** take the system accent or a colour of the terminal palette.
- **Cursor.** **Cursor style** (Block, Bar, Underline) and **Blink**. On macOS, **Option key** in Input sends Alt (Meta) to programs, or types special characters as before. These three apply to panes opened afterwards.
- **Play animation on copy.** Copied text flashes in the selection's colour. Turn it off in Input; with reduced motion it never plays.

Everything except the Cursor rows and the Option key applies to every open pane at once.

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
