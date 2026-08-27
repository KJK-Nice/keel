# Keel

A local SSH workspace. `keel` opens a desktop window on this machine. Close the window and the runtime plus every PTY die.

The TUI is still a client of the same runtime. Multiplayer (driver/follow) is available if you bind beyond localhost and set a token.

## Run

```bash
go run ./cmd/keel          # desktop app (default)
go run ./cmd/keel app      # same
go run ./cmd/keel tui      # terminal UI
go run ./cmd/keel server   # headless (no window)
```

```bash
go build -o keel ./cmd/keel
./keel
```

The window is Chromium app mode (Chrome, Chromium, Edge, or Brave). Isolated profile under the data dir, so it does not attach to your everyday browser. Set `KEEL_BROWSER` if yours is not in the usual place.

| Variable     | Default                         | Purpose                                      |
|--------------|---------------------------------|----------------------------------------------|
| `KEEL_ADDR`  | `127.0.0.1:47241`               | Runtime listen address                       |
| `KEEL_DATA`  | OS config dir `/keel`           | `keel.json`, pid, logs, window profile       |
| `KEEL_TOKEN` | (unset)                         | Optional share token for API and WebSockets  |
| `KEEL_NAME`  | `$USER`                         | Display name                                 |
| `KEEL_URL`   | (unset)                         | Remote runtime URL for `keel tui`            |
| `KEEL_BROWSER` | (auto)                        | Browser binary for the desktop window        |

Quit the window: every session is killed, then the process exits. Closing a tab only detaches. `keel tui` + `q` still leaves a `keel server` daemon running if one was spawned.

## The herd

- **Spawn** a session from a host (window Hosts, or TUI enter on a host)
- **Attach** from the Herd list
- The first client to attach is the **owner** and **driver**. Later joiners **follow**
- **Pass control** from the tab widget or `ctrl+b t` in the TUI
- **Detach** (close the tab, `ctrl+b d`, or `q` in the TUI)
- **Quit Keel** (close the window) kills everything

TUI keys: `enter` attach/spawn, `j`/`k` move, `x` kill, `n` local shell, `ctrl+b d` detach, `ctrl+b t` control, `q` quit.

## Share a session

Default bind is loopback. To share, listen on the LAN and set a token:

```bash
KEEL_ADDR=0.0.0.0:47241 KEEL_TOKEN=correct-horse-battery keel server
```

Web joiners open:

```
http://<host>:47241/?token=correct-horse-battery&name=bob
```

or paste a **Share** link. Joiners attach as followers.

Remote TUI:

```bash
KEEL_URL=http://<host>:47241 KEEL_TOKEN=correct-horse-battery KEEL_NAME=carol keel tui
```

This is still not a public internet service. Anyone with the token can spawn, kill, and see host inventory on that runtime.

## Why this shape

Go owns SSH and PTYs. The desktop window is a Chromium webview of the same UI. The TUI is another client. Local Shell is a PTY on this machine, started in `$HOME`.
