# Keel

A Termius-style SSH workspace with a **Herdr-style runtime**: the server owns the PTYs, and every UI is just a client.

`keel tui` is a detachable Bubble Tea client. The web UI is another client of the same herd. Close either one and the session keeps running.

## Run

```bash
keel server          # runtime in the foreground (API + web UI)
keel                 # attach the TUI (starts the runtime if needed)
keel tui             # same
```

```bash
go run ./cmd/keel server
# web: http://127.0.0.1:47241
```

In another terminal:

```bash
go run ./cmd/keel tui
```

| Variable    | Default         | Purpose                   |
|-------------|-----------------|---------------------------|
| `KEEL_ADDR` | `0.0.0.0:47241` | Runtime listen address    |
| `KEEL_DATA` | `data`          | `keel.json`, pid, logs    |

## The herd

- **Spawn** a session from a host (web Hosts, or TUI enter on a host)
- **Attach** from the Herd list — web tab or TUI pane, including at the same time
- **Detach** (close the tab, `ctrl+b d`, or `q`) — PTY stays alive
- **Kill** from the herd when you actually want it gone

TUI keys: `enter` attach/spawn, `j`/`k` move, `x` kill, `n` local shell, `ctrl+b d` detach, `q` quit.

## Why this shape

Go is a good SSH engine. A browser still needs xterm.js. A TUI still needs a VT parser (`vt10x`) to draw a live pane beside the sidebar. Neither is a from-scratch xterm rewrite. The part that makes it feel like Herdr is **server-owned PTYs**, not the renderer.

Hosts and snippets live in `data/keel.json` (mode `0600`). Sessions are in-memory: they survive TUI/web detach, not a runtime restart. This is a local tool — do not expose the port.
