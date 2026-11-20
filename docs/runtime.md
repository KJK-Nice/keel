# Runtime and configuration

`slime` opens the desktop window; PTYs live in a separate daemon, `slime serve`. Closing the window detaches and sessions keep running; closing a session tab kills that PTY.

On macOS Slime also lives in the menu bar (the Slime mark). Closing the window hides it and the Dock icon instead of quitting; the menu bar item brings it back. Its menu shows slime serve's status (`slime serve · 2 sessions`, or vault locked, or not running), **Show Slime** / **Hide Slime**, **New Tab**, **Quick Connect…**, the **Allow Claude Code (slime mcp)** switch (the same setting as Settings · Agent; see docs/agent.md) and **Quit Slime**. ⌘Q quits too. Quitting the app leaves `slime serve` and its sessions running, as closing the window always did. The menu re-reads slime serve every few seconds; it sends the UI token (for the Claude Code switch) only to a slime serve that proves it holds it.

```bash
slime                 # desktop app (starts slime serve if needed)
slime app             # same
slime serve           # runtime daemon (PTYs)
slime serve stop      # kill leftover sessions and stop the daemon
slime mcp             # MCP server on stdio for Claude Code (see docs/agent.md)
slime version         # the version this binary was released as (dev when built from source)
```

## Environment variables

Each variable also works under its name from before the rename, `KEEL_` in place of `SLIME_`; the `SLIME_` one wins when both are set.

| Variable | Default | Purpose |
|----------|---------|---------|
| `SLIME_ADDR` | `127.0.0.1:47241` | Where `slime serve` binds (loopback) |
| `SLIME_DATA` | OS config dir `/keel` | `keel.json`, `keel.vault`, pid, logs |
| `SLIME_NAME` | `$USER` | Display name |
| `SLIME_SIGNAL` | (unset) | Public URL of `slime-signal` for internet join ([Sharing](sharing.md)) for this run. It wins over the server saved in Settings · Sharing, which wins over one baked in with `make build SLIME_SIGNAL=…`. |
| `SLIME_VAULT_PASSPHRASE` | (unset) | Unlock/create the vault when the OS keychain is unavailable (a scripted setup: no first-run screens) |
| `SLIME_KEYRING` | (unset) | `off`: never read or write the OS keychain item (`keel`/`vault`), as if the keychain did not answer. For tests and machines without a keychain |
| `SLIME_SSH_DIR` | `~/.ssh` | Where the host import reads `config` and the keys it names; `~` in that config is the folder above it. Tests point it at a fixture |

## Loopback API

`slime serve` listens on loopback only and answers only requests whose `Host` is `127.0.0.1`, `::1` or `localhost`, so a web page that rebinds its own domain to 127.0.0.1 is refused. `slime serve` serves a JSON/WebSocket API and no web page: any other path answers 404. `/api/` and `/ws/` (except `/api/health`) need the token in `keel-serve.token` (mode 0600 in `SLIME_DATA`), sent as the `X-Slime-Token` header by the window and by `slime serve stop`; it is the only credential (no cookie, no query parameter), and a request with an `Origin` that is not the server's own loopback address is refused. Before handing over the token, the app asks the server to prove it holds it (HMAC of a random nonce), so another program that grabbed the port gets nothing.

`/api/mcp/` (slime mcp, [Agent](agent.md)) takes a different token, `keel-mcp.token` (mode 0600 in `SLIME_DATA`), in `X-Slime-MCP-Token`, and nothing else: not the UI's token or cookie, and not a request with an `Origin`. That token opens no other route. `/api/health?mcpNonce=` proves the server holds it, as `?nonce=` does for the UI's; slime mcp checks that proof before every request and follows no redirect. While Settings · Agent → "Allow Claude Code (slime mcp)" is off (the default), every `/api/mcp/` route answers 403.

Both headers also work under their names from before the rename, `X-Keel-Token` and `X-Keel-MCP-Token`, and Slime's clients send both, so an app and a `slime serve` from either side of the rename work together.

## Upgrading

`slime serve` keeps running across app launches. After you rebuild or upgrade, the old runtime would keep running the old code, so the window shows a toast when the runtime is from a different build (the build id is in `GET /api/health`): stop the runtime (this ends open sessions) and reopen Slime.

After upgrading from a build without the token, an older `slime serve` may still be running; the app then asks you to run `slime serve stop` once.
