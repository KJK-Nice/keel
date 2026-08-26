# Keel

A Termius-style SSH workspace with a **Herdr-style runtime**: the server owns the PTYs, and every UI is just a client.

`keel tui` is a detachable Bubble Tea client. The web UI is another client of the same herd. Close either one and the session keeps running.

Multiplayer is **driver / follow**, not everyone-types: one person drives, others watch in real time, and control can be passed.

## Run

```bash
keel server          # runtime in the foreground (API + web UI)
keel                 # attach the TUI (starts the runtime if needed)
keel tui             # same
keel tui --name alice
```

```bash
go run ./cmd/keel server
# web: http://127.0.0.1:47241
```

In another terminal:

```bash
go run ./cmd/keel tui --name alice
```

| Variable    | Default         | Purpose                                      |
|-------------|-----------------|----------------------------------------------|
| `KEEL_ADDR` | `0.0.0.0:47241` | Runtime listen address                       |
| `KEEL_DATA` | `data`          | `keel.json`, pid, logs                       |
| `KEEL_TOKEN`| (unset)         | Optional share token for API and WebSockets  |
| `KEEL_NAME` | `$USER`         | Display name for this TUI client             |
| `KEEL_URL`  | (unset)         | Remote runtime URL; never starts a local daemon |

## The herd

- **Spawn** a session from a host (web Hosts, or TUI enter on a host)
- **Attach** from the Herd list — web tab or TUI pane, including at the same time
- The first client to attach is the **owner** and **driver**. Later joiners **follow** (view-only)
- **Pass control** from the web widget (click a name) or `ctrl+b t` in the TUI (take if you own it, otherwise request)
- **Detach** (close the tab, `ctrl+b d`, or `q`) — PTY stays alive
- **Kill** from the herd when you actually want it gone

TUI keys: `enter` attach/spawn, `j`/`k` move, `x` kill, `n` local shell, `ctrl+b d` detach, `ctrl+b t` control, `q` quit.

## Share a session

Set a token so the port is joinable on a LAN (or over an SSH tunnel). Without a token, the API stays open like before.

```bash
KEEL_TOKEN=correct-horse-battery keel server
```

Web joiners open:

```
http://<host>:47241/?token=correct-horse-battery&name=bob
```

or paste a **Share** link from an open tab (`?session=<id>&name=&token=`). Joiners attach as followers.

Remote TUI (does not start a local runtime):

```bash
KEEL_URL=http://<host>:47241 KEEL_TOKEN=correct-horse-battery KEEL_NAME=carol keel tui
```

If `KEEL_ADDR` is a non-local host and nothing is listening, the TUI will not spawn a daemon — it expects that runtime to already be up.

This is still not a public internet service. Use a token plus LAN or an SSH tunnel. Anyone with the token can spawn, kill, and see host inventory on that runtime.

## Why this shape

Go is a good SSH engine. A browser still needs xterm.js. A TUI still needs a VT parser (`vt10x`) to draw a live pane beside the sidebar. Neither is a from-scratch xterm rewrite. The part that makes it feel like Herdr is **server-owned PTYs**, not the renderer.

Termius Multiplayer fans a client-owned SSH session out over WebRTC. Keel keeps the herd as the hub: same driver/follow UX, bytes stay on the runtime.

Hosts and snippets live in `data/keel.json` (mode `0600`). Sessions are in-memory: they survive TUI/web detach, not a runtime restart.
