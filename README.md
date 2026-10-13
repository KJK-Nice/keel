# Keel

A local SSH workspace. `keel` opens a **native desktop window** (macOS WebKit, Windows WebView2, Linux WebKitGTK). Closing the window detaches; sessions keep running in `keel serve`. Closing a session tab kills that PTY.

The desktop UI is **templ** + [shadcn-templ](https://shadcn-templ.com) + [Datastar](https://data-star.dev), hosted in a [Wails v3](https://v3.wails.io) webview pointed at a loopback HTTP server (Wails cannot proxy WebSockets). Terminals still use xterm.js over WebSocket.

Host passwords and keys live in an encrypted **vault**, not in plaintext `keel.json`. Share a session over the internet with WebRTC — joiners only see that session's terminal, not your hosts, keys or LAN (unless you give them control of a shell that can reach them; see below).

## Install / build

```bash
make build && ./bin/keel
# bake a public signal URL into Keel.app (Finder launch has no shell env):
make build KEEL_SIGNAL=https://keel-signal-production.up.railway.app
```

`make build` runs `templ generate` and the Tailwind CLI (`assets/css/globals.css` → `cmd/keel/web/css/output.css`), then compiles the Go app. Both tools are pinned: templ comes from `go.mod` (`go tool templ`), and the Makefile downloads the Tailwind standalone binary at `TAILWIND_VERSION` into `bin/` on first use. Use your own with `make TAILWIND=tailwindcss …`, but a different version rewrites `output.css`.

Checks (the same ones CI runs on every PR):

```bash
make test              # go test, desktop tags
make lint              # gofmt, go vet, staticcheck, govulncheck
make check-generated   # committed *_templ.go and output.css match the pinned tools
```

The desktop UI script is `cmd/keel/web/js/term.js`, loaded as an ES module. Logic with no DOM or state lives in `cmd/keel/web/js/lib/*.js` and is tested with `node --test` (`make test-js`, also part of `make test`); move code there as it is untangled from `term.js`.

Ubuntu 24.04+ needs GTK4 + WebKitGTK 6 (Wails v3):

```bash
sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev
```

macOS needs Xcode Command Line Tools (`xcode-select --install`). Use **`make build`** (not a bare `go build`): it links **UniformTypeIdentifiers**, silences Wails’s `setShowsBaselineSeparator:` deprecation on macOS 15+, and writes **`bin/Keel.app`** (Dock icon is the Keel chevron). WKWebView on Apple Silicon **SIGTRAPs** a naked unsigned binary (JIT). `./bin/keel` copies itself into `~/Library/Caches/keel/Keel.app`, ad-hoc signs it with JIT entitlements, and re-execs **before** opening WebKit. The UI then starts `keel serve` if needed. Or `open bin/Keel.app`. Windows uses Edge WebView2. The About menu and Linux window icon use the same mark.

## Run

```bash
./keel                 # desktop app (starts keel serve if needed)
./keel app             # same
./keel serve           # runtime daemon (PTYs)
./keel serve stop      # kill leftover sessions and stop the daemon
```

| Variable | Default | Purpose |
|----------|---------|---------|
| `KEEL_ADDR` | `127.0.0.1:47241` | Local webview bind (loopback) |
| `KEEL_DATA` | OS config dir `/keel` | `keel.json`, `keel.vault`, pid, logs |
| `KEEL_NAME` | `$USER` | Display name |
| `KEEL_SIGNAL` | (unset, or baked via `make build KEEL_SIGNAL=…`) | Public URL of `keel-signal` for internet join. Runtime env overrides the compile-time default. |
| `KEEL_VAULT_PASSPHRASE` | (unset) | Unlock/create the vault when the OS keychain is unavailable |

⌘W closes the focused pane first (killing that PTY). When the tab is down to one pane, ⌘W closes the tab, the last session tab included; Vault stays. Close the window to detach: `keel serve` keeps remaining sessions, and the next launch restores them as tabs.

## Vault

Tab 1 is the **Vault** (pinned). It holds Hosts, Keychain, Port forwarding, Snippets, Logs, and Settings.

Secrets (SSH password, private key, key passphrase) are encrypted with [age](https://age-encryption.org). The wrapping key is stored in the OS keychain when possible (macOS Keychain, Windows Credential Manager, libsecret). If that fails, Keel asks for a passphrase on first launch.

**Keychain** in the Vault is a list of reusable SSH identities (password or private key) you attach to hosts. That is separate from the OS keychain that wraps `keel.vault`.

Existing plaintext `keel.json` files are migrated on unlock: secrets move into `keel.vault` and are stripped from the JSON.

The first time you connect to an SSH host, Keel asks you to trust its key. A different key later is rejected until you replace it.

## Loopback API

`keel serve` listens on loopback only and answers only requests whose `Host` is `127.0.0.1`, `::1` or `localhost`, so a web page that rebinds its own domain to 127.0.0.1 is refused. The UI, `/api/`, `/ws/` and `/ui/` also need the token in `keel-serve.token` (mode 0600 in `KEEL_DATA`): the desktop app trades it for an HttpOnly cookie on first load, and `keel serve stop` sends it as `X-Keel-Token`. Before handing over the token, the app asks the server to prove it holds it (HMAC of a random nonce), so another program that grabbed the port gets nothing.

After upgrading from a build without the token, an older `keel serve` may still be running; the app then asks you to run `keel serve stop` once.

`keel serve`, not the app, serves the UI, and it keeps running across app launches. After you rebuild or upgrade, the old runtime would keep serving the old UI, so the window shows a bar when the runtime is from a different build: stop the runtime (this ends open sessions) and reopen Keel.

## Tabs, split, shortcuts

On macOS the tab strip lives in the window titlebar, beside the traffic lights. Drag empty chrome to move the window. Windows and Linux keep a separate OS titlebar.

- **Hold ⌘** (Ctrl on Windows/Linux) to overlay keys on tabs and buttons.
- **⌘T** new tab (recent hosts to pick). **⌘W** closes the focused pane first; with one pane left it closes the session tab (Vault stays pinned). **⌘⇧T** reopens the last closed tab as a **new** session to the same host.
- **⌘/Ctrl+1…8** switch tabs (1 = Vault). **⌘/Ctrl+9** is the last tab.
- **⌘<** / **⌘>** previous or next tab. The strip wraps and includes Vault. **⌘⇧[** / **⌘⇧]** do the same.
- Drag session tabs to reorder. Vault stays first; numbered shortcuts follow the current order.
- **⌘K** / **⌘⇧P** opens the command palette (connect a host, focus a pane or tab, split, settings, and other chrome actions). Host search on Vault still works; the palette includes **Search hosts in Vault**. **⌘N** new host. **⌘⇧O** quick connect. **⌘J** join. **⌘,** settings.
- Palette actions that would type into a live session (**Type snippet**) always show **approve-before-type**: the bytes are previewed and nothing is written until you click **Type**. Vault snippet list clicks are unchanged.
- Open a host: new session tab. Close tab = kill that PTY. Close the window = detach (sessions stay in `keel serve`; reopen Keel to attach). SSH hosts keepalive every 30s (edit host to change or turn off) and auto-reconnect on a drop. `exit` on the remote shell stays dead; **Reconnect** on the pane brings it back.
- **Split →** / **⌘⇧D** and **Split ↓** / **⌘⇧E** spawn another PTY to the same host.
- **⌘+** / **⌘-** bigger or smaller terminal text (10–22px). **⌘0** resets to 12.
- **⌘⇧W** closes the window (UI quits; sessions stay in `keel serve`). File → **Stop Keel runtime** (or `keel serve stop`) kills leftover sessions and the daemon.
- **Sync** broadcasts keystrokes (and Vault snippet paste) to every driving pane in that tab.
- Settings: font, text size, copy-on-select, right-click paste (stored in `localStorage`).

## Port forwarding

Saved under Vault → Port forwarding. Local (`-L`), remote (`-R`), and dynamic SOCKS5 (`-D`). Each forward opens its own SSH connection (it does not share a shell session). Default bind is `127.0.0.1`. Binding `0.0.0.0` exposes the port on every interface.

## Logs

Vault → Logs is an append-only event log (`keel.log` under `KEEL_DATA`). Connect/spawn/kill, tunnel start/stop, and vault unlock are recorded. Secrets are never written.

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

3. Open a session, click **Share**, send the copied `/join?room=…#k=…` link.

4. On the other machine, open Keel → **Join session** → paste the link. A browser can open the same link if they don't have Keel.

5. On your side, Keel asks **Allow** or **Deny** for each person who opens the link (a request nobody answers is denied after two minutes). Allowed viewers see output from the moment you clicked Share, not what was on screen before; press Ctrl-L or redraw a full-screen program to fill their screen. They follow until you pass control, and never see hosts or keys.

6. To end it, run **Stop sharing this session** from the command palette (⌘K). Viewers are disconnected and the link stops working; closing the session tab does the same.

The part after `#k=` is a secret that never reaches the signaling server (browsers don't send the `#fragment`). Both ends use it to check each other's WebRTC certificate, so a signaling server that tried to sit in the middle of the connection is refused. Keel's own joiner does this check in its own code; a browser joiner runs `join.js` served by the signaling server, so for the browser the check is only as trustworthy as that server. Anyone with the full link can ask to watch, so share it like a password. The Allow prompt shows whatever name the joiner typed, and a hostile signaling server could swap one link holder for another, so Allow only when you're expecting someone; the prompt keeps strangers without the link out, not impostors who have it. Passing control hands them the keyboard of that shell — on a **Local Shell** that means your machine as you (your files, `keel.json`, and anything the shell can reach), so pass control only to people you'd let sit at your desk.

Update `keel-signal` before Keel itself: it has to pass the new `mac` field through, and Keel refuses joiners (and links) without it.

Rooms on `keel-signal` expire after two minutes if no host connects and live at most 24 hours (the server pings quiet connections so an idle share stays up). The server refuses more than 512 rooms, more than 16 joiners per room, and messages over 64 KB. Only the client that created a room (it gets a host key) can host it.

Per-address rate limiting of room creation (6 in a burst, then one per 10 s) is off until you tell `keel-signal` where the client address is, with `KEEL_SIGNAL_CLIENT_IP`:

| Value | Use when |
| --- | --- |
| *(unset)* | off |
| `remote` | clients connect to `keel-signal` directly (no proxy) |
| `xff:N` | behind N proxies that each append to `X-Forwarded-For`: the Nth entry from the right is the client |

Pick N from what your platform actually sends (log the header once): too far left and clients choose their own key; too far right and every client shares one, so one client can block sharing for everyone. The join page serves xterm from the signal binary (no CDN). Desktop and the join page may open the signaling WebSocket; other websites cannot.

Two Keel windows on one Mac (test): second instance needs its own port and data dir:

```bash
KEEL_ADDR=127.0.0.1:47243 KEEL_DATA=/tmp/keel-join ./bin/keel
```

`open bin/Keel.app` does not pass those env vars.

STUN uses `stun.l.google.com:19302`. Some home NATs still need TURN; that is not bundled yet.

## The herd

- **Spawn** a session from Vault → Hosts
- The first client to attach is the **owner** and **driver**. Later joiners (including WebRTC) **follow**
- **Pass control** from the tab widget
- **Close tab** kills that PTY. **Close window** detaches; reopen Keel to attach
- **Stop Keel runtime** kills leftover sessions and the daemon

## Why this shape

Go owns SSH and PTYs. The window is Wails v3 (native webview) pointed at localhost because Wails cannot proxy WebSockets. The chrome is templ + shadcn-templ; Datastar patches vault lists. Remote follow uses WebRTC so the host HTTP API never leaves loopback. Local Shell starts in `$HOME`.
