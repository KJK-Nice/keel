# Share a session

Joiners cannot open TCP to your machine. Both sides dial **out** to a signaling server (`keel-signal`); PTY bytes go over a WebRTC datachannel. Joiners only see that session's terminal, not your hosts, keys or LAN (unless you give them control of a shell that can reach them; see [Security](#security)).

## Set up

1. Run signaling somewhere public (VPS, Railway, …):

```bash
./bin/keel-signal
# or: KEEL_SIGNAL_ADDR=:8080 ./bin/keel-signal
```

2. On the host desktop, open **Settings → Sharing** and enter the server's URL under **Signal server** (for example `https://your-signal.example`), then press Enter. keel serve saves it in `keel.json` and uses it for the next share; shares already running keep their link. Clear the field to turn Share off.

Two other ways set the server, for scripts and builds:

```bash
# for one run, from a shell (wins over the saved server):
KEEL_SIGNAL=https://your-signal.example ./bin/keel
# built into the app (used when nothing is saved and KEEL_SIGNAL is unset):
make build KEEL_SIGNAL=https://your-signal.example
```

Order: `KEEL_SIGNAL` for the run, then the server saved in Settings, then the one built in. Release builds have none built in. Settings · Sharing says which one is in use.

## Share and join

1. Open a session, click **Share** in the titlebar. The Share popover shows the `/join?room=…#k=…` link (copied for you; **Copy** copies it again), who is in the session with their role, and **Stop sharing**. The link shares the focused pane's session only. Send the link.

2. On the other machine, open Keel → **Join session** → paste the link. A browser can open the same link if they don't have Keel.

3. On your side, Keel asks **Allow** or **Deny** for each person who opens the link (a request nobody answers is denied after two minutes). Allowed viewers see output from the moment you clicked Share, not what was on screen before; press Ctrl-L or redraw a full-screen program to fill their screen. They follow until you pass control, and never see hosts or keys. A joined tab shows a link icon and `· joined`, a banner "Following <owner>'s session on <host> · read-only" with **Ask for control** and **Leave**, and no Vault sidebar; typing while following shows "Typing is off while you follow" and sends nothing.

4. When a joiner asks for control you get a toast, "<name> asks for control", with **Hand over** and **Keep driving**. To pass control unasked, use **Pass control** in the Share popover; the owner can **Take control** back from the pane banner or the popover.

5. To end it, click **Stop sharing** in the Share popover, or run **Stop sharing this session** from the command palette (⌘K). Viewers are disconnected and the link stops working; closing the session's pane does the same.

## The herd

- **Spawn** a session from the Vault sidebar's Hosts
- The first client to attach is the **owner** and **driver**. Later joiners (including WebRTC) **follow**
- **Pass control** from the Share popover (titlebar **Share**, or the **N watching** badge); a request from a follower arrives as a toast with **Hand over**
- **Close tab** kills that PTY. **Close window** detaches; reopen Keel to attach
- **Stop Keel runtime** kills leftover sessions and the daemon

## Security

The part after `#k=` is a secret that never reaches the signaling server (browsers don't send the `#fragment`). Both ends use it to check each other's WebRTC certificate, so a signaling server that tried to sit in the middle of the connection is refused. Keel's own joiner does this check in its own code; a browser joiner runs `join.js` served by the signaling server, so for the browser the check is only as trustworthy as that server.

Anyone with the full link can ask to watch, so share it like a password. The Allow prompt shows whatever name the joiner typed, and a hostile signaling server could swap one link holder for another, so Allow only when you're expecting someone; the prompt keeps strangers without the link out, not impostors who have it.

Passing control hands them the keyboard of that shell — on a **Local Shell** that means your machine as you (your files, `keel.json`, and anything the shell can reach), so pass control only to people you'd let sit at your desk.

## Running keel-signal

Update `keel-signal` before Keel itself: it has to pass the new `mac` field through, and Keel refuses joiners (and links) without it.

Rooms on `keel-signal` expire after two minutes if no host connects and live at most 24 hours (the server pings quiet connections so an idle share stays up). The server refuses more than 512 rooms, more than 16 joiners per room, and messages over 64 KB. Only the client that created a room (it gets a host key) can host it.

Per-address rate limiting of room creation (6 in a burst, then one per 10 s) is off until you tell `keel-signal` where the client address is, with `KEEL_SIGNAL_CLIENT_IP`:

| Value | Use when |
| --- | --- |
| *(unset)* | off |
| `remote` | clients connect to `keel-signal` directly (no proxy) |
| `xff:N` | behind N proxies that each append to `X-Forwarded-For`: the Nth entry from the right is the client |

Pick N from what your platform actually sends (log the header once): too far left and clients choose their own key; too far right and every client shares one, so one client can block sharing for everyone. The join page serves xterm from the signal binary (no CDN). Desktop and the join page may open the signaling WebSocket; other websites cannot.

STUN uses `stun.l.google.com:19302`. Some home NATs still need TURN; that is not bundled yet.

## Testing with two windows

Two Keel windows on one Mac: the second instance needs its own port and data dir:

```bash
KEEL_ADDR=127.0.0.1:47243 KEEL_DATA=/tmp/keel-join ./bin/keel
```

`open bin/Keel.app` does not pass those env vars.
