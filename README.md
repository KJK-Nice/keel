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

`make build` writes `tokens.css` from `docs/design/tokens.json` (the only source of colors, type, radii and sizes), runs `templ generate` and the Tailwind CLI (`assets/css/globals.css` → `cmd/keel/web/css/output.css`), then compiles the Go app. Fonts ship as local woff2 in `cmd/keel/web/fonts/`. Both tools are pinned: templ comes from `go.mod` (`go tool templ`), and the Makefile downloads the Tailwind standalone binary at `TAILWIND_VERSION` into `bin/` on first use. Use your own with `make TAILWIND=tailwindcss …`, but a different version rewrites `output.css`.

Checks (the same ones CI runs on every PR):

```bash
make test              # go test, desktop tags
make lint              # gofmt, go vet, staticcheck, govulncheck
make check-generated   # committed *_templ.go and output.css match the pinned tools
```

The desktop UI script is `cmd/keel/web/js/term.js`, loaded as an ES module. It holds app state and wiring; the rest lives beside it and is tested with `node --test` (`make test-js`, also part of `make test`):

- `lib/` has no DOM or state: split layouts and their geometry (`layout.js`), vault groups and list markup (`groups.js`, `vaultview.js`), the command palette's filtering (`palette.js`), the session socket protocol (`session.js`), shortcuts, join links, the share joiner (`joinpeer.js`, `sharemac.js`), and the Share popover, people list and follower banner markup (`share.js`).
- `ui/` holds small DOM widgets that take their elements and callbacks as arguments (`palette.js`, `joinprompt.js`, `actiontoast.js`, `sharepop.js` (the Share popover), and `gate.js`: Unlock, First run and the host import, with their markup in `lib/onboard.js`).

`internal/signal/web/` serves copies of `joinpeer.js` and `sharemac.js` to the browser join page; a test fails if they drift from `lib/`.

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
| `KEEL_VAULT_PASSPHRASE` | (unset) | Unlock/create the vault when the OS keychain is unavailable (a scripted setup: no first-run screens) |
| `KEEL_KEYRING` | (unset) | `off`: never read or write the OS keychain item (`keel`/`vault`), as if the keychain did not answer. For tests and machines without a keychain |
| `KEEL_SSH_DIR` | `~/.ssh` | Where the host import reads `config` and the keys it names; `~` in that config is the folder above it. Tests point it at a fixture |

⌘W closes the focused pane first (killing that PTY). When the tab is down to one pane, ⌘W closes the tab, the last session tab included; with no tab left the window shows a New tab. Close the window to detach: `keel serve` keeps remaining sessions, and the next launch restores them as tabs (with the tab pages you had open).

## Vault

The **Vault** is the sidebar on the left of every tab (**⌘B** or the titlebar button folds it). Its filter and segments show **Hosts** (the host tree by group: click a host to connect, the row's menu edits, duplicates or removes it), **Snippets** (click one to type it after you approve; notebooks below) and **Forwards** (on/off per forward). Its footer opens **Keychain**, **Logs** and **Settings**. Keychain, Port forwarding, Snippets, Notebooks, Logs and Settings each open as their own tab page, one of each; opening one again selects its tab.

Secrets (SSH password, private key, key passphrase) are encrypted with [age](https://age-encryption.org). The wrapping key is stored in the OS keychain when possible (macOS Keychain, Windows Credential Manager, libsecret). If that fails, Keel asks for a passphrase on first launch.

**First run.** A new Keel opens on **Welcome to Keel**: step 1 shows the vault (made in the keychain, or created here with a passphrase when the keychain does not answer; its path and what it holds), step 2, **Bring your hosts**, reads `~/.ssh/config` and lists its hosts with checkboxes. The config is only read, never written; Include (relative to `~/.ssh`, globs, at most 8 deep, loops cut), Host (several names give several rows), HostName, User, Port and IdentityFile are understood. Wildcard names (`*`, `?`, `!`) are listed as patterns, not hosts; Match blocks are skipped with a note; hosts with ProxyCommand or ProxyJump are flagged and can be added without the proxy (jump hosts are not supported yet); hosts already saved (same address, user and port) start unchecked. Each private key the hosts name has its own switch, off by default: a key you turn on is copied into the vault as a Keychain identity (asking for its passphrase if it has one, which is stored in the vault too) and the files in `~/.ssh` are never changed, moved or chmod'ed. The same import is in the command palette as **Import hosts from ~/.ssh/config**.

**Unlock.** When the vault exists but keel serve could not open it at start, the window shows **Unlock your vault**: the passphrase, how many sessions are waiting in `keel serve` (they keep running while the vault is locked), and a reminder that a lost passphrase cannot be recovered. Keel never creates, re-keys or overwrites a vault from that screen.

**Keychain** is a list of reusable SSH identities (password or private key) you attach to hosts. That is separate from the OS keychain that wraps `keel.vault`.

Existing plaintext `keel.json` files are migrated on unlock: secrets move into `keel.vault` and are stripped from the JSON.

The first time you connect to an SSH host, Keel asks you to trust its key. A different key later is rejected until you replace it.

## Loopback API

`keel serve` listens on loopback only and answers only requests whose `Host` is `127.0.0.1`, `::1` or `localhost`, so a web page that rebinds its own domain to 127.0.0.1 is refused. The UI, `/api/`, `/ws/` and `/ui/` also need the token in `keel-serve.token` (mode 0600 in `KEEL_DATA`): the desktop app trades it for an HttpOnly cookie on first load, and `keel serve stop` sends it as `X-Keel-Token`. Before handing over the token, the app asks the server to prove it holds it (HMAC of a random nonce), so another program that grabbed the port gets nothing.

After upgrading from a build without the token, an older `keel serve` may still be running; the app then asks you to run `keel serve stop` once.

`keel serve`, not the app, serves the UI, and it keeps running across app launches. After you rebuild or upgrade, the old runtime would keep serving the old UI, so the window shows a bar when the runtime is from a different build: stop the runtime (this ends open sessions) and reopen Keel.

## Tabs, split, shortcuts

On macOS the tab strip lives in the window titlebar, beside the traffic lights. Drag empty chrome to move the window. Windows and Linux keep a separate OS titlebar.

- **Hold ⌘** (Ctrl on Windows/Linux) to overlay keys on tabs and buttons.
- **⌘T** new tab: the **New tab page** (also what an empty window shows). Its one input takes a saved host (filters the cards), `user@host:port`, an `ssh://` URL or a pasted `ssh …` command; below it are the six most recent hosts (**⌥1**–**⌥6** open them), Quick connect, Join session, New host, and Recently closed. A pasted command is parsed, never run: Keel shows what it kept and which options it left out (`-o ProxyCommand`, `LocalCommand`, `StrictHostKeyChecking=no`, `-L/-R/-D`, `-W`, a trailing remote command, `-J` jump hosts, which are not supported yet; `-i` becomes a note and the key file is not read), then offers a prefilled **New host** or **Connect once**. **⌘W** closes the focused pane first; with one pane left it closes the tab (a tab page closes too). The window always keeps a tab: with none left it shows a New tab. **⌘⇧T** reopens the last closed tab or pane as a **new** session to the same host (the list is kept in the browser's storage as host id, title and time).
- **⌘/Ctrl+1…8** switch tabs by position (**⌘1** is the first tab: the Vault is a sidebar, not a tab). **⌘/Ctrl+9** is the last tab.
- **⌘B** (Ctrl+B on Windows/Linux) shows or hides the Vault sidebar.
- **⌘I** (Ctrl+Shift+I on Windows/Linux, where Ctrl+I is Tab in the terminal) shows or hides the agent panel.
- **⌘<** / **⌘>** previous or next tab. The strip wraps. **⌘⇧[** / **⌘⇧]** do the same.
- Drag tabs to reorder; numbered shortcuts follow the current order.
- **⌘K** / **⌘⇧P** opens the command palette in four groups: **Open now** (every tab, and the panes of split tabs; picking one focuses it, it never starts a second session), **Hosts** (new session on a host), **Actions** (tabs, panes, vault pages, settings) and **Snippets**. **↵** opens, **⌥↵** opens a host in a split of the focused pane, **esc** closes. The sidebar filter still searches hosts (Enter connects the first match, or opens Quick connect for `user@host`); the palette includes **Search hosts**. **⌘N** new host, or the page's own New on Port forwarding, Keychain, Snippets and Notebooks. **⌘⇧O** quick connect. **⌘J** join. **⌘,** settings.
- Palette actions that would type into a live session (**Type snippet**) always show **approve-before-type**: the bytes are previewed and nothing is written until you click **Type**. Snippet clicks in the sidebar and **Type into pane…** on the Snippets page ask the same way.
- Open a host: new session tab. Close tab = kill that PTY. Close the window = detach (sessions stay in `keel serve`; reopen Keel to attach). SSH hosts keepalive every 30s (edit host to change or turn off) and auto-reconnect on a drop. `exit` on the remote shell stays dead; **Reconnect** on the pane brings it back. Keys typed while a pane is still connecting or reconnecting are dropped, not saved for later: the first thing that comes up may be a password or host key prompt. An approved Type (snippet, agent proposal, group run) at that moment fails and says nothing was typed; press it again once the pane is connected.
- **Split →** / **⌘⇧D** and **Split ↓** / **⌘⇧E** spawn another PTY to the same host.
- **⌘+** / **⌘-** bigger or smaller terminal text (10–22px). **⌘0** resets to 12.
- **⌘⇧W** closes the window (UI quits; sessions stay in `keel serve`). File → **Stop Keel runtime** (or `keel serve stop`) kills leftover sessions and the daemon.
- **Sync** broadcasts keystrokes (and approved snippet typing) to every driving pane in that tab.
- Settings: font, text size, copy-on-select, right-click paste (stored in `localStorage`).
- Settings · Appearance · **Window translucency** (desktop window only): Off, Low, Medium (default) or High, how much of the desktop shows through the window. Off is opaque. On Windows and Linux the window stays opaque while the theme differs from the system's. Stored in `localStorage` with the other window preferences and applied before the first paint.

## Shell integration (command marks)

When a shell reports its prompts and commands with OSC 133 escape sequences, Keel marks each finished command in the terminal's left inset (a green dot for exit 0, a red one otherwise, with an `exit N` chip at the end of a failed command's prompt line), the palette's **Previous command** / **Next command** scroll between prompts, and the agent reads the last command, its output and its exit status instead of the pane's last lines (`docs/ui-redesign.md` §14).

Keel never edits a remote host's shell files, and does not change how the Local Shell starts. fish 4.0 and later already send the marks; for bash and zsh, and fish 3, add one of these snippets yourself, on each host where you want it (the Local Shell reads your local files). It only prints escape sequences around your prompt; it does not change what commands do.

bash 4.4 or later, at the end of `~/.bashrc` (macOS's `/bin/bash` 3.2 has no `PS0`, so it gets prompts but no commands):

```bash
# Keel shell integration (OSC 133)
if [[ $- == *i* && -z ${__keel_osc133-} ]]; then
  __keel_osc133=1
  __keel_prompt() {
    local s=$?
    printf '\033]133;D;%s\007' "$s"
    return "$s"
  }
  PROMPT_COMMAND="__keel_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  PS1='\[\033]133;A\007\]'"$PS1"'\[\033]133;B\007\]'
  PS0='\033]133;C\007'
fi
```

zsh, at the end of `~/.zshrc`:

```zsh
# Keel shell integration (OSC 133)
if [[ -o interactive && -z ${__keel_osc133-} ]]; then
  __keel_osc133=1
  __keel_precmd() {
    local s=$?
    (( ${+__keel_ran} )) && printf '\033]133;D;%s\007' "$s"
    unset __keel_ran
    printf '\033]133;A\007'
  }
  __keel_preexec() { __keel_ran=1; printf '\033]133;C\007'; }
  precmd_functions=(__keel_precmd $precmd_functions)
  preexec_functions+=(__keel_preexec)
  PS1="$PS1%{$(printf '\033]133;B\007')%}"
fi
```

fish 3 (fish 4 needs nothing), in `~/.config/fish/config.fish`:

```fish
# Keel shell integration (OSC 133)
if status is-interactive; and test (string split -f1 . $version) -lt 4; and not set -q __keel_osc133
    set -g __keel_osc133 1
    function __keel_prompt --on-event fish_prompt
        printf '\e]133;A\a'
    end
    function __keel_preexec --on-event fish_preexec
        printf '\e]133;C\a'
    end
    function __keel_postexec --on-event fish_postexec
        printf '\e]133;D;%s\a' $status
    end
end
```

Prompt themes that rebuild `PS1` on every prompt (powerlevel10k, starship, bash-git-prompt) drop the `B` mark the snippet adds; marks still work, but the agent then sees the command's output without the command line. powerlevel10k can send the marks itself (`POWERLEVEL9K_TERM_SHELL_INTEGRATION=true`). The marks are output like any other, so a program can print fake ones: they only draw marks and choose what the agent reads, they never run or allow anything.

## Port forwarding

Saved on the Port forwarding page (Vault sidebar → Forwards → Manage forwards). Local (`-L`), remote (`-R`), and dynamic SOCKS5 (`-D`). Each forward opens its own SSH connection (it does not share a shell session). Default bind is `127.0.0.1`. Binding `0.0.0.0` exposes the port on every interface.

## Logs

Logs (Vault sidebar footer) is an append-only event log (`keel.log` under `KEEL_DATA`). Connect/spawn/kill, tunnel start/stop, and vault unlock are recorded. Secrets are never written.

## Agent (in progress)

The agent is being built in phases (`docs/ui-redesign.md` §6, §10). Phase 6b adds the **agent panel** (right side, **⌘I** on macOS, **Ctrl+Shift+I** on Windows/Linux, the titlebar **Agent** button, or View › Toggle Agent Panel). You ask about the panes in its scope (the chip "Reads N of M panes"); keel serve reads their last lines (or, when the shell reports its commands with OSC 133, the last command, its output and its exit status; see Shell integration), redacts them, and sends that and your question to Google Gemini, only when you press send. The answer can carry command proposals: Keel shows the exact bytes and a Read-only / Changes state / Destructive badge, and nothing is typed until you press **Type into pane** and approve the bytes (destructive commands, and state changes on a production host, need **⇧** as well). The key goes in the panel's Connect Gemini field and is kept in the vault. Conversations are kept in memory only, until New conversation or `keel serve stop`.

`KEEL_AGENT_PROVIDER=fake` is for end-to-end tests only: the agent then answers from an in-process fake and nothing is sent anywhere.

The backend core:

- Each host has **Agent can read** (`agentReadable`, on by default) and **Treat as production** (`production`, off by default; turned on when a host is created in or moved into a group named `production`). Older clients that do not send the fields keep the saved values.
- Agent settings (provider, model, detection, default scope, scrollback lines 50–1000, IP/hostname redaction, history) are in `keel.json` and at `GET`/`PUT /api/agent/settings`. Secret redaction is always on.
- Provider keys (Gemini, Jev) are vault secrets, set with `PUT /api/agent/keys/{provider}` and removed with `DELETE`. No response ever returns a key, only `configured: true/false`.
- Agent reads and inserts are written to `keel.log` as `agent.read`, `agent.suggest`, `agent.insert` and `agent.type`, with line counts and redaction counts, never the text.

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

3. Open a session, click **Share** in the titlebar. The Share popover shows the `/join?room=…#k=…` link (copied for you; **Copy** copies it again), who is in the session with their role, and **Stop sharing**. The link shares the focused pane's session only. Send the link.

4. On the other machine, open Keel → **Join session** → paste the link. A browser can open the same link if they don't have Keel.

5. On your side, Keel asks **Allow** or **Deny** for each person who opens the link (a request nobody answers is denied after two minutes). Allowed viewers see output from the moment you clicked Share, not what was on screen before; press Ctrl-L or redraw a full-screen program to fill their screen. They follow until you pass control, and never see hosts or keys. A joined tab shows a link icon and `· joined`, a banner "Following <owner>'s session on <host> · read-only" with **Ask for control** and **Leave**, and no Vault sidebar; typing while following shows "Typing is off while you follow" and sends nothing.

6. When a joiner asks for control you get a toast, "<name> asks for control", with **Hand over** and **Keep driving**. To pass control unasked, use **Pass control** in the Share popover; the owner can **Take control** back from the pane banner or the popover.

7. To end it, click **Stop sharing** in the Share popover, or run **Stop sharing this session** from the command palette (⌘K). Viewers are disconnected and the link stops working; closing the session's pane does the same.

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

- **Spawn** a session from the Vault sidebar's Hosts
- The first client to attach is the **owner** and **driver**. Later joiners (including WebRTC) **follow**
- **Pass control** from the Share popover (titlebar **Share**, or the **N watching** badge); a request from a follower arrives as a toast with **Hand over**
- **Close tab** kills that PTY. **Close window** detaches; reopen Keel to attach
- **Stop Keel runtime** kills leftover sessions and the daemon

## Why this shape

Go owns SSH and PTYs. The window is Wails v3 (native webview) pointed at localhost because Wails cannot proxy WebSockets. The chrome is templ + shadcn-templ; the vault lists reach the page over one Datastar stream (`GET /ui/stream`) that patches them whenever the vault changes (docs/ui-redesign.md §12). Remote follow uses WebRTC so the host HTTP API never leaves loopback. Local Shell starts in `$HOME`.
