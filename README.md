# Keel

A local SSH workspace. `keel` opens a **native desktop window** (macOS WebKit, Windows WebView2, Linux WebKitGTK). Close the window and the runtime plus every PTY die.

Host passwords and keys live in an encrypted **vault**, not in plaintext `keel.json`. Share a session over the internet with WebRTC — joiners never reach your LAN or your vault.

## Install / build

```bash
make build && ./bin/keel
# bake a public signal URL into Keel.app (Finder launch has no shell env):
make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
```

Ubuntu 24.04+ needs GTK + WebKit and the `webkit2_41` build tag (`make build` sets it):

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
```

macOS needs Xcode Command Line Tools (`xcode-select --install`). Use **`make build`** (not a bare `go build`): it links **UniformTypeIdentifiers**, silences Wails’s `setShowsBaselineSeparator:` deprecation on macOS 15+, and writes **`bin/Keel.app`**. WKWebView on Apple Silicon **SIGTRAPs** a naked unsigned binary (JIT). `./bin/keel` copies itself into `~/Library/Caches/keel/Keel.app`, ad-hoc signs it with JIT entitlements, and re-execs **before** binding the port. Or `open bin/Keel.app`. Windows uses Edge WebView2.

## Run

```bash
./keel                 # desktop app (default)
./keel app             # same
```

| Variable | Default | Purpose |
|----------|---------|---------|
| `KEEL_ADDR` | `127.0.0.1:47241` | Local webview bind (loopback) |
| `KEEL_DATA` | OS config dir `/keel` | `keel.json`, `keel.vault`, pid, logs |
| `KEEL_NAME` | `$USER` | Display name |
| `KEEL_SIGNAL` | (unset, or baked via `make build KEEL_SIGNAL=…`) | Public URL of `keel-signal` for internet join. Runtime env overrides the compile-time default. |
| `KEEL_VAULT_PASSPHRASE` | (unset) | Unlock/create the vault when the OS keychain is unavailable |

Quit the window: every session is killed, then the process exits. Closing a tab only detaches.

## Vault

Secrets (SSH password, private key, key passphrase) are encrypted with [age](https://age-encryption.org). The wrapping key is stored in the OS keychain when possible (macOS Keychain, Windows Credential Manager, libsecret). If that fails, Keel asks for a passphrase on first launch.

Existing plaintext `keel.json` files are migrated on unlock: secrets move into `keel.vault` and are stripped from the JSON.

## Share a session (no LAN)

Joiners cannot open TCP to your machine. Both sides dial **out** to a signaling server; PTY bytes go over a WebRTC datachannel.

1. Run signaling somewhere public (VPS, Railway, …):

```bash
./bin/keel-signal
# or: KEEL_SIGNAL_ADDR=:8080 ./bin/keel-signal
```

2. On the host desktop, either bake the URL into the app or set it at runtime:

```bash
make build KEEL_SIGNAL=https://your-signal.example
open bin/Keel.app
# or, from a shell only:
KEEL_SIGNAL=https://your-signal.example ./bin/keel
```

3. Open a session, click **Share**, send the copied `/join?room=…` link. The joiner follows until you pass control. They never see hosts or keys.

STUN uses `stun.l.google.com:19302`. Some home NATs still need TURN; that is not bundled yet.

## The herd

- **Spawn** a session from Hosts
- **Attach** from the Herd list
- The first client to attach is the **owner** and **driver**. Later joiners (including WebRTC) **follow**
- **Pass control** from the tab widget
- **Detach** (close the tab)
- **Quit Keel** (close the window) kills everything

## Why this shape

Go owns SSH and PTYs. The window is Wails (native webview) pointed at localhost because Wails cannot proxy WebSockets. Remote follow uses WebRTC so the host HTTP API never leaves loopback. Local Shell starts in `$HOME`.
