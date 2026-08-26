# Keel

A Termius-style SSH workspace written in Go: host inventory, command snippets, and real PTY sessions in the browser.

Go is a strong fit for this kind of client. `golang.org/x/crypto/ssh` is production-grade, goroutines map cleanly onto many concurrent sessions, and the whole app ships as one static binary. The part that is *not* worth rewriting in Go is the terminal emulator itself — that is a years-long project (WezTerm, Alacritty, Windows Terminal). Keel uses xterm.js for rendering and Go for everything behind it: SSH, local PTY, host storage, and the HTTP/WebSocket server.

This first slice is a local workspace, not a cloud-synced team product. There is no account system, no encrypted vault sync, and no mobile app. Bind it to localhost when you run it on your own machine.

## Features

- Host library with groups, search, colors, password or private-key auth
- Quick connect from `user@host:port`
- Multi-tab terminals (local shell and SSH)
- Saved snippets that type into the focused session
- Trust-on-first-use SSH host keys
- Local shell so the app is usable with no remote servers

## Run

```bash
go run ./cmd/keel
```

Then open [http://127.0.0.1:47241](http://127.0.0.1:47241).

```bash
make build        # writes bin/keel
KEEL_ADDR=127.0.0.1:47241 KEEL_DATA=./data ./bin/keel
go test ./...
```

| Variable    | Default         | Purpose                          |
|-------------|-----------------|----------------------------------|
| `KEEL_ADDR` | `0.0.0.0:47241` | Listen address                   |
| `KEEL_DATA` | `data`          | Directory for `keel.json`        |

Hosts, snippets, and known-host fingerprints are stored in `data/keel.json` with mode `0600`. Passwords are kept there in plaintext so this is a **local tool**. Do not expose the port to the internet.

## How it fits together

```
browser (xterm.js)  --WebSocket-->  Keel (Go)
                                      ├─ local PTY  (this machine)
                                      └─ golang.org/x/crypto/ssh
```

A later native desktop shell could wrap the same Go engine with Wails or Fyne. The SSH session code would not need to change.

## Why not a full Termius clone in Go?

Termius is mostly product around SSH: encrypted multi-device sync, teams, mobile, SFTP, port forwarding, and a custom terminal UI on every OS. Go handles the protocol and the process model well. Shipping that entire surface is a different project. Keel is the engine + a usable workspace, which is the right first slice.
