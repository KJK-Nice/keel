# Runtime and configuration

`keel` opens the desktop window; PTYs live in a separate daemon, `keel serve`. Closing the window detaches and sessions keep running; closing a session tab kills that PTY.

On macOS Keel also lives in the menu bar (the Keel mark). Closing the window hides it and the Dock icon instead of quitting; the menu bar item brings it back. Its menu shows keel serve's status (`keel serve · 2 sessions`, or vault locked, or not running), **Show Keel** / **Hide Keel**, **New Tab**, **Quick Connect…**, the **Allow Claude Code (keel mcp)** switch (the same setting as Settings · Agent; see docs/agent.md) and **Quit Keel**. ⌘Q quits too. Quitting the app leaves `keel serve` and its sessions running, as closing the window always did. The menu re-reads keel serve every few seconds; it sends the UI token (for the Claude Code switch) only to a keel serve that proves it holds it.

```bash
keel                 # desktop app (starts keel serve if needed)
keel app             # same
keel serve           # runtime daemon (PTYs)
keel serve stop      # kill leftover sessions and stop the daemon
keel mcp             # MCP server on stdio for Claude Code (see docs/agent.md)
keel version         # the version this binary was released as (dev when built from source)
```

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `KEEL_ADDR` | `127.0.0.1:47241` | Local webview bind (loopback) |
| `KEEL_DATA` | OS config dir `/keel` | `keel.json`, `keel.vault`, pid, logs |
| `KEEL_NAME` | `$USER` | Display name |
| `KEEL_SIGNAL` | (unset, or baked via `make build KEEL_SIGNAL=…`) | Public URL of `keel-signal` for internet join ([Sharing](sharing.md)). Runtime env overrides the compile-time default. |
| `KEEL_VAULT_PASSPHRASE` | (unset) | Unlock/create the vault when the OS keychain is unavailable (a scripted setup: no first-run screens) |
| `KEEL_KEYRING` | (unset) | `off`: never read or write the OS keychain item (`keel`/`vault`), as if the keychain did not answer. For tests and machines without a keychain |
| `KEEL_SSH_DIR` | `~/.ssh` | Where the host import reads `config` and the keys it names; `~` in that config is the folder above it. Tests point it at a fixture |

## Loopback API

`keel serve` listens on loopback only and answers only requests whose `Host` is `127.0.0.1`, `::1` or `localhost`, so a web page that rebinds its own domain to 127.0.0.1 is refused. The UI, `/api/`, `/ws/` and `/ui/` also need the token in `keel-serve.token` (mode 0600 in `KEEL_DATA`): the desktop app trades it for an HttpOnly cookie on first load, and `keel serve stop` sends it as `X-Keel-Token`. Before handing over the token, the app asks the server to prove it holds it (HMAC of a random nonce), so another program that grabbed the port gets nothing.

`/api/mcp/` (keel mcp, [Agent](agent.md)) takes a different token, `keel-mcp.token` (mode 0600 in `KEEL_DATA`), in `X-Keel-MCP-Token`, and nothing else: not the UI's token or cookie, and not a request with an `Origin`. That token opens no other route. `/api/health?mcpNonce=` proves the server holds it, as `?nonce=` does for the UI's; keel mcp checks that proof before every request and follows no redirect. While Settings · Agent → "Allow Claude Code (keel mcp)" is off (the default), every `/api/mcp/` route answers 403.

## Upgrading

`keel serve`, not the app, serves the UI, and it keeps running across app launches. After you rebuild or upgrade, the old runtime would keep serving the old UI, so the window shows a bar when the runtime is from a different build: stop the runtime (this ends open sessions) and reopen Keel.

After upgrading from a build without the token, an older `keel serve` may still be running; the app then asks you to run `keel serve stop` once.
