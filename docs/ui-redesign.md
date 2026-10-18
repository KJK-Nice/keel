# Keel UI redesign: spec

Status: design approved for build · 2026-10-13

- Tokens and components (source of truth in the repo): [`docs/design/`](design/)
  - `tokens.json`: every token, both themes. `tokens.css`: the same as CSS custom properties.
  - `components.css`: reference `k-*` component styles. `README.md`: usage rules (the brand book).
- Design system (live, browsable): https://claude.ai/artifact/3RUKjVH2znuvieFDaDjkS1
- Screens (canvas, 17 boards): https://claude.ai/artifact/GJaVi8YQSrxoNiDVXoi2sW

## 1. Why

- The UI has three token layers fighting each other: `--keel-*` in `keel.css`, shadcn variables re-pointed at them, and the unused stock values in `globals.css`. The light palette is written out four times, `app.css` (1,196 lines) is dead, and about 20 classes are unused.
- The vault lives in a pinned tab, so reaching a host or snippet means leaving the session.
- The project is an agentic terminal multiplexer, but there is no agent surface yet.

## 2. Goals and non-goals

Goals
- One token set (the Keel design system), dark first, light fully usable.
- Vault reachable from every tab without switching tabs.
- An agent that reads only what you allow and **suggests, never types**.
- Plain-language → command suggestions inside a pane.
- Same flows as today for sessions, splits, sync, share and forwards. Only the presentation and placement change.

Non-goals (this round)
- An agent that runs commands by itself, including read-only ones.
- Changes to the runtime, SSH, PTY or WebRTC protocols.
- Settings sections other than Agent (Appearance, Terminal, Keyboard, Sharing, Runtime keep their current content and get restyled only).
- Notebook editor redesign (the editor stays ProseMirror + CommonMark; its reading styles were restyled later, §13).

## 3. Visual foundations (summary; `docs/design/` is the source of truth)

- Neutrals with a slate-blue bias. Grounds: `stage` (terminal, fields) < `chrome` (titlebar, rails, unfocused pane bars) < `surface` < `raised` (menus, dialogs) < `hover`.
- One accent, **brass**. At rest it marks only: the selected tab, the focused pane, the cursor. Otherwise only things you can press now (primary button, an on toggle) or a role that is yours (Driving).
- **Keel line**: a 2px brass rule under the selected tab and the focused pane bar. No coloured side bars anywhere.
- Status: `tide` live/success · `signal` attention · `flare` failure/destructive · `haze` info/follower. Always with a word or icon.
- Type: IBM Plex Sans (UI), IBM Plex Sans Condensed (titles), JetBrains Mono (terminal, addresses, keys). UI size 13px default = 1rem; ship fonts locally as woff2.
- Radii 3 / 5 / 8 / 12. Shadows only on things that float.
- Terminal theme: `term-bg`, `term-fg`, `term-cursor`, `term-selection`, `ansi-0…15`. Host colours are `ansi-9…14`.
- Components are plain `k-*` CSS classes so they work in templ and in `term.js` HTML strings.

## 4. Information architecture

| Before | After |
|---|---|
| Vault = pinned tab 1 (Hosts, Keychain, Forwards, Snippets, Logs, Settings) | **Vault sidebar** (left, 252px, collapsible, every tab): host tree by folder, segmented Hosts / Panes / Snippets / Forwards (Panes only while the active tab has 2+ panes, §5.1), footer Keychain · Logs · Settings |
| Keychain, Forwards, Snippets, Logs, Settings inside the vault tab | Each opens as **its own tab page** (like Settings) |
| ⌘1 = Vault | ⌘1 = first session tab |
| No agent | **Agent panel** (right, 380px, collapsible, one per window) |
| Start screen | **New tab page** (⌘T) |
| — | Status bar (24px) at the bottom of every window |

## 5. Screens

Board names refer to the canvas.

### 5.1 Session workspace (`Main`)
- Titlebar: traffic lights · sidebar toggle · tabs (state dot + host in mono + pane count badge when >1) · + · right: watchers badge, Share, Agent ⌘I.
- Panes: split grid, 1px `line` gutters. Pane bar 30px: state dot, host, `user@address` (truncates first), role chip when shared, "Agent reads" badge when the agent may read it, icon tools (sync, split →, split ↓, more).
- Focused pane: bar on `surface` + keel line. Unfocused: bar on `chrome`, muted text.
- Banners between bar and terminal for exited/failed (`flare`, Reconnect) and reconnecting/host-key changed (`signal`).
- Status bar: runtime state and session count, running forwards, focused pane size, encoding, Sync state.
- **Panes in the sidebar** (board `Panes`, post-plan polish). The sidebar segments are Hosts · Panes · Snippets · Forwards. Panes exists only while the active tab has 2+ panes and shows the pane count after its label; when the tab drops to one pane, or a one-pane tab (or a New tab or tab page) is selected, the segment hides and a selected Panes falls back to Hosts.
  - Clicking into a pane of a split tab (or picking a pane in the palette's Open now group) switches the sidebar to Panes. Only the section shown changes: keyboard focus stays in the terminal. Splitting or closing a pane does not switch it.
  - Lists **only the active tab's panes**. Top: a layout map that mirrors the tab's split tree (cells sized like the panes, numbered in pane order; the focused cell on `surface` with the 2px brass keel line). Then a "This tab" group header with the count and one 40px row per pane: host-colour tile (as in the host tree), name in mono, a sub line from what the pane already knows (`user@address` from the pane bar, "Notebook" for a notebook, else its connection state), and meta icons: Driving (steering) when the pane is shared and you drive, and the haze agent icon when the agent can read it (the host's "Agent can read" is on). The focused row is `aria-current` (hover background, name weight 500), no coloured side bar. No per-row shortcut.
  - A row or a map cell focuses that pane by the same path as a click into it, and gives its terminal the keyboard; a zoomed tab leaves focus view first.
  - The filter's placeholder is "Filter panes" and it narrows rows by name and sub line, never the map.
  - Live: split, close, rename, connection state and focus changes repaint it. Collapsed sidebar (⌘B): nothing changes. Joined tabs have no sidebar (§5.7).

### 5.2 Command palette ⌘K / ⌘⇧P (`Palette`)
- Groups in this order: **Open now** (tabs and panes, to avoid duplicate sessions) → Hosts → Actions → Snippets.
- Last row: "Ask the agent about '<query>'" ⌘↵.
- Snippet rows carry "Approve first"; they always go through Approve before type.
- ↵ open · ⌥↵ open in split · ⌘↵ ask agent · esc close.

### 5.3 New tab ⌘T (`NewTab`)
- One input: saved host, `user@host:port`, or a pasted `ssh …` command (parsed, then offered as a new saved host).
- Recent hosts (6 cards, ⌥1–⌥6), Start actions (Quick connect ⌘⇧O, Join ⌘J, New host ⌘N, Ask agent ⌘I), Recently closed (⌘⇧T reopens as a new session).

### 5.4 Agent panel (`Main`, right side)
- Header: scope chip "Reads N of M panes" (opens pane picker), new conversation, close.
- Thread: user message → agent trace (what it read, e.g. "Read prod-db-01 · last 120 lines") → answer → **command proposal card**.
- Proposal card: target host, Read-only / Changes state badge, full bytes with control glyphs, byte and line count, Edit · Copy · **Type into pane ↵**.
- Composer: @-chips for panes in scope, `@` adds a pane, ↵ send.
- Footer line: "Nothing reaches a pane until you press Type…"

### 5.5 Plain language → command in a pane (`Suggest`)
1. **Notice**: when the prompt line looks like plain language, it is highlighted (`haze-soft` + underline) and a hint shows "⇥ suggest a command". Detection is automatic (see §7).
2. **Suggest**: ⇥ opens a card under the line: command, Read-only badge, one-line reason, up to 2 alternatives (2, 3 or ↑↓), ⇥ insert · esc keep my words.
3. **Inserted**: the command replaces the line, highlighted `brass-soft`, **not run**. ↵ runs it; ⌘Z restores the original words.
4. **Destructive**: red edge + "Deletes files" (or similar) badge. Plain ⇥ never inserts; ⇧⇥ inserts anyway.
5. **History first (6c+)**: before the model writes a command, keel serve gathers commands already run in the pane (its scrollback, after shell prompt lines) and snippets marked "Let the agent suggest this snippet", and Jev picks one. A confident pick is the card, labelled "From this pane's scrollback" or "From your snippets", with no model call and no alternatives; otherwise the model answers as before. Same rating and ⇥ / ⇧⇥ gates. A scrollback line counts only at the pane's current prompt (§6, limit). Settings · Agent → "Suggest from this pane's scrollback" (`historySuggestions`, default on) turns the scrollback part off; snippets still apply. Never the remote `~/.bash_history`, never keystrokes. Design and thresholds: `internal/agent/service/history.go`, `suggestq.go`.

### 5.6 Share and pass control (`Share`)
- Popover from Share: join link + Copy, expiry (59 min or on tab close), mode **Watch only** / **Watch and ask for control**, people list with role and latency, Pass control to …, Stop sharing.
- Incoming request toast: "<name> asks for control" with Hand over / Keep driving.
- Copy states that joiners see only this tab's terminals.

### 5.7 Joined session, follower view (`Join`)
- Tab shows a link icon and "· joined". Banner (`haze`): "Following <owner>'s session on <host> · read-only · <ms>", Ask for control, Leave.
- Pane bars show who drives and "You follow". Typing shows a toast "Typing is off while you follow".
- No vault sidebar and no agent for joiners.

### 5.8 Edit host (`Host`)
- Dialog 760px, two columns. General (name, folder, colour from ANSI 9–14, tags) · Connection (host, port, user, jump host) · Authentication (identity from Keychain, host key fingerprint and trust) · Session (startup command, keepalive, reconnect) · **Agent** (Agent can read this host's panes; Treat as production).
- Footer: Delete host (left) · Test connection with inline result · Cancel · Save ⌘↵.
- **Jump host** is a saved host, by id (`jumpHost`), never a free `user@host:port`: the jump's password, key or Keychain identity stay in the vault and its host key is checked under its own `address:port`, like any connect. One hop only: a jump host cannot use one itself, which also rules out cycles and self-reference (`store/jump.go`). A session or forward dials the jump, opens a `direct-tcpip` channel to the target and runs the target's handshake over it; both hops go through the same trust-on-first-use check, and an error names the hop. A deleted jump host makes the connect fail ("jump host not found"), never fall back to a direct connect.
- **Startup command** (`startupCommand`, one line, no control characters, ≤1024 bytes) is typed into the first shell of a session, with Enter (CR), once. Not on reconnect (a new shell, but the command may change state, and the user who saw the drop is there), not for restored tabs (they attach to running sessions) and never for joiners (they only attach). It is a user setting, not agent output, so §6.1 is unaffected; the agent sees it only as scrollback. `keel.log` gets "startup command sent to <host>", never the command. It is stored in `keel.json` like the address: not a place for secrets.
- **Host key** shows the saved SHA256 fingerprint (the `ssh-keygen -l` format) and key type for the host's saved `address:port` (`GET /api/hosts/{id}/host-key`). Forget host key asks in the alertdialog, then `DELETE` removes it and logs it; the next connect asks again.
- **Test connection** (`POST /api/hosts/test`) connects and authenticates with the dialog's unsaved values, through the jump host if one is picked, then closes: no PTY, no command. 10 s, cancellable. Outcomes: ok with latency, auth failed, host key unknown or changed (fingerprint shown, never trusted on its own; Trust and save / Replace saved key is a separate click), timeout, DNS, refused. Typed secrets travel only in that request over the authed loopback API, are used for that connect and are never stored or logged. Vault secrets (a blank password or key, a Keychain identity) are used only while the dialog still targets the saved host: same address:port, username, authentication, identity and jump host. Any change needs the secret typed ("Address changed: type the password to test it"), so a test can never send a saved password to a new address. Any edit to the dialog drops the last result.
- **Trusting a key** (`POST /api/known-hosts`) takes only a key a connect was just refused for: each refusal records an offer (address:port, fingerprint, key type) under a random token for 2 minutes, and the request must name that token with the same address:port and fingerprint; a changed key also needs `replace`, and the saved key must not have changed since. Session prompts, forwards and Test connection all trust this way.

### 5.9 Settings · Agent (`Settings`)
- Provider (connect; key stored in the vault). Suggestions in panes (on/off; Notice automatically / Only lines starting with `#`). Default panes per conversation (focused only / pick each time / every pane in tab). Scrollback lines per pane (50–1000, default 200). Safety (two always-on rows, ⇧⇥ rule, redaction of secrets / IPs / hostnames). History retention and delete all.

### 5.10 Port forwarding (`Forwards`)
- Table: name, type (-L/-R/-D), route `local → remote`, through host, status, traffic, on/off switch, more.
- Bind on `0.0.0.0` shows a `signal` "Every interface" badge. Failures show cause and Retry / Edit.

### 5.11 Keychain (`Keychain`)
- List (SSH keys, Passwords) + detail: fingerprint, storage, passphrase, comment, public key with Copy, "Used by N hosts". Keys in use cannot be deleted. Old algorithms get a `signal` badge.

### 5.12 Snippets (`Snippets`)
- List + editor: commands (one per line), `{{var}}` fields with defaults, "Offer on hosts tagged", kind **Read-only / Changes state**, preview of exact bytes. Typing always goes through Approve before type; Changes state on a production host needs ⇧↵.
- "Let the agent suggest this snippet" (6c+, off by default): a one-line snippet with no `{{var}}` field may be offered as an in-pane suggestion (§5.5). Only then is its text sent, redacted, to Jev (§6.3).

### 5.13 Logs (`Logs`)
- Filters: All, Sessions, Forwards, Vault, Sharing, **Agent**. Rows: time, kind, target, event.
- Agent events are logged: what it read (panes, line counts, redactions) and every insert/type from a suggestion.

### 5.14 Unlock vault (`Unlock`)
- Shown when the keychain does not answer. Passphrase field, "Try macOS Keychain again next time", Unlock. States that running sessions keep working and that a lost passphrase cannot be recovered.

### 5.15 First run (`Onboard1–3`)
1. Create vault: Keychain (recommended) or passphrase; shows vault path and contents; "I already have a keel.vault…".
2. Bring your hosts: parse `~/.ssh/config` (read-only), table with checkboxes, wildcard patterns listed apart (not importable), ProxyCommand hosts flagged and importable without the proxy; a one-hop ProxyJump becomes the host's jump host when its target is imported too, or is the one saved host with the jump's user, address and port (no user in ProxyJump means the local user); otherwise it is flagged and imported as a direct host only when the user ticks "Without the jump", already-saved hosts unchecked; optional copy of referenced private keys into the vault, one switch per key, off by default (originals untouched).
3. Agent: three promises, provider picker, suggestions toggle, Set up later / Open Keel.

## 6. Agent invariants (must hold in code, not only in UI)

1. The agent never writes to a PTY. Only a user action (Type / ⇥ / ⇧⇥ / ↵) does.
2. The agent reads only panes in the conversation's scope, and only hosts with "Agent can read" on.
3. Vault contents (passwords, keys, passphrases, snippet bodies unless the user adds them) are never sent.
4. Redaction runs before anything leaves the machine. Secret-looking values are always redacted.
5. Destructive commands always need ⇧⇥ (or ⇧ + Type). State-changing commands need ⇧⇥ on production hosts; on other hosts plain ⇥ inserts them. A host counts as production if it was when the suggestion was made, is now, or can no longer be found. Read-only commands insert with plain ⇥ everywhere.
6. A follower cannot type agent output into a pane they follow; the proposal is not offered.
7. Every read and every insert is written to `keel.log`.

**Limit of these gates.** The ⇧ rule, the no-newline rule for inserts and the driver check are enforced in keel serve, so they hold against model output and against a UI bug that sends the wrong bytes or skips a key. They do not hold against hostile script running inside the app page itself: the pre-existing unlabelled inject path (palette snippets, the notebook's CLI input) accepts any bytes from the driving client with a fresh ticket, and script in the page can ask for a ticket like the page does. Keeping the page free of untrusted script (no remote scripts, escaped HTML) is what protects that path, not the agent gates.

**Limit of "From this pane's scrollback" (6c+).** Scrollback is untrusted output, and nothing proves a shell drew a line or that its command ran. So a candidate counts only when its prompt is exactly the pane's current prompt (the line being edited); with no readable current prompt there are none. Lines abandoned with ^C (or showing ^Z, ^D, ^[), lines touched by cursor moves, backspaces or erases, and commands with invisible format characters are dropped. Still, any program that writes that exact prompt string followed by text can pose as a command: a file printed with cat, a login banner, a REPL, a remote program, or a shell-like prompt that happens to match. The label says only where the line was seen, and the risk rating and the ⇥ / ⇧⇥ gates apply to it as to any suggestion. OSC 133 shell-integration marks are not used yet; they would let Keel tell prompts and commands from output.

## 7. Plain-language detection

- Default: automatic. A classifier decides "plain language vs command" for the current prompt line; it never blocks typing and never changes what ↵ sends.
- Fallback setting: only lines starting with `#` (safe if ↵ is pressed by mistake, because the shell treats it as a comment).
- Candidate for the classifier and the command/risk judgment: Jev (TypeSafe). To evaluate: latency budget (< 150 ms for the Notice hint), offline behaviour, cost per keystroke burst (debounce on pause, not per key).
- Risk label (Read-only / Changes state / Destructive) comes from the same judgment and is shown on every suggestion.
- **Speed after ⇥.** Suggestions use the provider's own suggestion model (Gemini: `gemini-3.5-flash-lite`, thinking level `minimal`); the agent panel keeps the settings model with its default thinking. The card is shown as soon as the model answers, rated by code (the denylist, made stricter by the model's own rating). Jev rates it afterwards (up to 2 s); when Jev is stricter, keel serve updates the kept suggestion and pushes `agent-suggest-risk`, and the card's badge, edge and ⇥ / ⇧⇥ hint follow. Jev can only make a rating stricter, never looser.
- **⇥ before Jev answers** is decided by keel serve on the code rating. This is accepted: the denylist is final for destructive commands, so those always need ⇧⇥ either way; what Jev can add in that window is a stricter rating for a command the code rule passed. Once Jev has answered, plain ⇥ is refused for an option it made need ⇧, with a reason that says a second check rated it riskier.
- `agent.suggest` in `keel.log` records how long the model call and the Jev risk call took (`modelMs`, `jevMs`), never the words; keel serve's own log prints a debug line with the model, thinking level and both times.

## 8. Keyboard

| Key | Action | Note |
|---|---|---|
| ⌘K / ⌘⇧P | Command palette | unchanged |
| ⌘T | New tab page | was host picker |
| ⌘1…8, ⌘9 | Switch tab / last tab | **⌘1 is now the first session** (Vault left the tab strip) |
| ⌘I | Toggle agent panel | new (6b); Ctrl+Shift+I on Windows/Linux, where Ctrl+I is Tab (§11.1) |
| Sidebar toggle | Toggle vault sidebar | new; shortcut to choose (⌘B proposed) |
| ⌥1–⌥6 | Open recent host on New tab page | new |
| ⇥ / ⇧⇥ | Suggest/insert · insert destructive | new, only while a suggestion is shown or a line is noticed |
| ⌘Z | Restore words after an insert | new, only right after insert; Ctrl+Shift+Z on Windows/Linux, where Ctrl+Z suspends the foreground job (6c) |
| ⌘↵ | Ask agent (palette): opens the agent panel with the query as an unsent message | new (6d); Ctrl+↵ off macOS. Save in dialogs is not bound yet (↵ in the form saves) |
| ⌘N, ⌘⇧O, ⌘J, ⌘,, ⌘W, ⌘⇧T, ⌘⇧D, ⌘⇧E, ⌘± / ⌘0 | as today | ⌘N is "new item" on Forwards/Keychain/Snippets pages |

**⌘↵ conflict check (6d).** Every `Enter` handler was read: `term.js` (the pane's quick-input confirm, bound on that input only and stopped there; the host-tree search ↵; the new-tab input), `ui/palette.js` (before 6d it swallowed ⌘↵ and Ctrl+↵ so they did not fall through to ↵; now they ask the agent), `ui/agentpanel.js` (↵ sends; ⌘↵ confirms a proposal being edited, scoped to that card), `ui/gate.js` (↵ in the import passphrase) and the native menu (`internal/desktop`: only ⌘⇧↵, Focus Pane, which `lib/shortcuts.js` matches with Shift). None of them listens for ⌘↵ at a level the palette's input could reach, so there is no conflict. ⌘↵ asks the agent only while the palette input has focus; the row is also reachable with ↑↓ and ↵. xterm never sees it because the palette is a modal over the terminal. The panel does not send: it opens with the text in the composer and the user presses ↵. Dialog Save is not bound to ⌘↵ yet (no handler exists); the Save ⌘↵ in the Edit host board is still to do.

## 9. Data model additions

- Host: `color` (ANSI index 9–14), `tags[]`, `agentReadable` (bool, default true), `production` (bool, default false), `jumpHost` (saved host id, one hop), `startupCommand` (one line).
- Known hosts: `knownHostTypes` (key type per `address:port`, beside the fingerprints in `knownHosts`).
- Snippet: `kind` (`readonly` | `changes-state`), `vars` with defaults, `offerOnTags[]`, `agentUsable` (bool, default false; 6c+).
- Settings: agent provider ref (secret in vault), detection mode, default scope, scrollback lines, redaction toggles, history retention, `historySuggestions` (on|off, default on; 6c+: suggest from the pane's scrollback).
- Log events: `agent.read`, `agent.suggest`, `agent.insert`, `agent.type`, `share.request`, `share.pass`.

## 10. Build plan

One phase per branch and PR.

| Phase | Scope | Notes |
|---|---|---|
| 0 · Tokens | Replace `globals.css` values and the `keel.css` bridge with `docs/design/tokens.css`; map shadcn vars (`--background→stage`, `--card→surface`, `--popover→raised`, `--primary→brass`, `--primary-foreground→on-brass`, `--muted-foreground→ink-muted`, `--border→line`, `--input→line-strong`, `--ring→focus`, `--destructive→flare`); delete `app.css` and unused classes; local woff2 fonts | No layout change. Visual diff only. |
| 1 · Chrome | Titlebar tabs, pane bar, banners, status bar, keel line, xterm theme from tokens | Mostly `term.js` + `keel.css` |
| 2 · Vault sidebar + tab pages | Sidebar, ⌘1 change, Keychain / Forwards / Snippets / Logs / Settings as tabs | Biggest IA change |
| 3 · Palette + New tab | Open-now group, pasted ssh parsing | |
| 4 · Share + Join | Popover, request toast, follower banner | Behaviour exists; UI only |
| 5 · Unlock + First run | ssh config import, key copy | New backend for import |
| 6 · Agent | Provider, scope, redaction, audit log, panel, proposals, then in-pane suggestions | Invariants in §6 are acceptance criteria |
| Polish · Sidebar Panes | Panes segment and section in the Vault sidebar (§5.1, board `Panes`) | Post-plan polish PR; UI only, no backend data |

## 11. Open questions

1. Shortcut for the sidebar toggle: ⌘B (phase 2; it was the right session rail's toggle, which the Vault sidebar replaced). **⌘I (decided, 6b):** free on macOS (no menu, term.js or xterm use; Ctrl-I stays Tab for readline because ⌘ is the app modifier). On Windows/Linux the app modifier is Ctrl and Ctrl+I is Tab in a terminal, so the panel takes **Ctrl+Shift+I** there (menu and keyboard agree, `internal/desktop/shortcuts_test.go`); in a plain Chromium browser tab Ctrl+Shift+I opens DevTools first, so the titlebar button and the palette are the way there.
2. Agent provider(s) to support first. **Decided (6a):** Google Gemini first, behind a provider interface (`internal/agent/provider`); the key is a vault secret.
3. Jev vs a local model for detection: latency and offline requirements. **Decided (6c):** Jev (`internal/agent/provider/jev`, model pinned to `jev-1.13.0`), one Noul per typing pause (350 ms), notice at P ≥ 0.70, a 900 ms timeout. With no Jev key, offline, an error or a timeout, only `#` lines are noticed, silently. Without a model key there is no notice at all. Jev also rates every suggestion (one Choice per command, one request), and may only make the code rating stricter. Questions and thresholds: `internal/agent/service/suggestq.go`. Measured from a home connection: 270–470 ms per call, so the < 150 ms budget holds only near the endpoint.
4. Should "Treat as production" default on for hosts in a folder named `production`? **Decided (6a):** yes, at the moment a host is created in, or moved into, a group whose own name is `production` (any case; the direct parent only, not an ancestor). Deleting a group whose hosts move up into a `production` parent counts as a move. Nothing else sets it (loading or renaming never does), and the user can turn it off. A value sent on create wins over the rule, so **6d's Edit host dialog must prefill "Treat as production" on when the chosen group is named `production`**, so an untouched dialog still gets it.
5. Light theme: ship together with dark in phase 0, or after?

## 12. Datastar for the vault surfaces

Status: D1–D4 done · 2026-10-17

Until now Datastar was loaded but unused: no `data-*` attribute read the page signals, and `term.js` fetched and swapped every vault list itself (`paintList`, `loadAll`). The vault surfaces move to Datastar; the session workspace does not.

- **Datastar owns:** the vault lists (host tree, Keychain, Forwards, Snippets, Notebooks, Logs), their filters, and then the vault forms. The server renders templ and is the only source of the lists.
- **term.js keeps:** terminals (xterm, the PTY WebSockets), tabs, panes and splits, the agent panel, the palette and the new-tab page, plus the behaviour it layers on the lists (selection, collapsed groups, live badges, drag and drop). It re-applies that after each morph (`watchStreamedLists`).
- **One stream:** `GET /ui/stream` (`internal/httpapi/uistream.go`) is opened by `#ui-stream` (`page.templ`). It patches the lists, morphed by `id`, whenever the store saves or a tunnel changes state. Filters are request/response (`@get('/ui/hosts')`); the server records them against the page's `streamId` signal so later pushes keep them.
- **Bridge:** term.js talks to Datastar through window events (`keel-stream`, `keel-form-saved`, `keel-vault-changed`) and `data-*` attributes (`#search[data-seg]`), never through Datastar internals. Signals the server reads are the fields of `uiSignals` (`internal/httpapi/ui.go`).

| Phase | Scope |
|---|---|
| D1 · Live lists | The stream; the sidebar host filter via `@get`; `loadAll` no longer repaints lists |
| D2 · Pages | Keychain / Forwards / Snippets / Notebooks filters and the Forwards / Logs chips are underscore signals (kept in the page) with `data-show` on each row; the Logs search is `@get('/ui/logs')`; new log entries are pushed on the stream (at most once a second) instead of the 3 s poll |
| D3 · Forms | Group, identity, tunnel and snippet dialogs post with `@post('/ui/forms/{kind}', {contentType: 'form'})` (`internal/httpapi/forms.go`, the same save functions as the JSON API). An error patches the dialog's `#{kind}-form-error`; a save clears it and dispatches `keel-form-saved`, which term.js uses to close the dialog and toast. The lists change through the stream. `data-indicator:_saving` disables Save while the request runs. term.js still calls `loadAll` after a save for its JSON state until D4. D3b moved the host dialog and the Snippets page editor (`/ui/forms/host`, `/ui/forms/snippet-editor`): a `k-switch` with `data-switch-field` mirrors into a hidden input (`setSwitch`) so it posts with the form, and the server parses tags, keepalive and secrets (`hostFromForm`). Test connection and Trust and save stay JSON (they are actions, not saves). Notebook rename stays in term.js: it renames open notebook tabs and panes, which term.js owns |
| D4 · Cleanup | Removed `paintList`, `datastarQS`, `paintVaultNotebooks`, the client list renderers in `vaultview.js` (it keeps the Keychain detail, the snippet preview and the glyphs) and `nestGroupTree`; removed the fragment routes only `paintList` used (`/ui/identities`, `/ui/tunnels`, `/ui/snippets`, `/ui/notebooks`) and the unused, token-exempt `POST /ui/vault`. Every list push also dispatches `keel-vault-changed`; term.js refreshes its JSON state on it (`refreshState`: one `loadAll` at a time, one queued), so saves no longer call `loadAll` themselves and changes from other windows or the agent reach the palette, the new-tab page and the dialogs |

## 13. Notebook reading spec

Status: built · 2026-10-18 · boards `Notebook` (the pane beside a session) and `NotebookTypes` (type specimen), copied to `docs/design/boards/`.

The notebook pane is a document you read beside a terminal, not a form. Code: `paintNotebook` and `syncNotebookMeta` in `term.js`, the fence and callout node views in `js/notebook-editor.mjs` (bundled to `vendor/notebook/editor.js` by `make notebook-editor`), the `notebook-*` rules in `keel.css`.

- **Ground and column.** The pane is on `surface`. Content sits in one column `size-reading` (68ch) wide, padded `space-6 + space-1` / `space-8` / `space-8 + space-4`; a wider pane leaves the rest empty.
- **One title.** The notebook's name is the only H1 (`display` 28/32, −0.01em) and stays editable in place (the field is `Rename notebook`). There is no separate name field above a second heading. A markdown `#` heading in the body renders like H2, and new notebooks start without one.
- **Meta line** under the title (`caption`, `ink-muted`, `·` separators in `ink-faint`): "Notebook", "Updated <date>", and a host chip (mono, `hover` ground, a dot in the host's colour) naming the pane shell fences type into, or "No terminal in this tab". A hairline (`line`) follows.
- **Type.** H2 `heading` 18/22 (display family), H3–H6 `title` 15/20, prose `reading` 14/22 (a new style in `tokens.json`: long-form text only, the interface stays on `body`). Block rhythm: `space-3` between blocks, `space-8` above H2, `space-6` above H3, `space-2` under a heading. Links `brass-ink`, underline `line-strong`.
- **Inline code:** `code` mono 12 on `hover`, `radius-xs`, never wraps.
- **Lists:** hanging markers in `ink-faint`, ordered numbers in mono 500, `space-1` between items.
- **Fence:** one box on `stage` (`line`, `radius-md`) with a 32px bar inside it (`size-control-lg`, hairline under it): the language as a quiet uppercase `meta` select, then an icon-only Copy, then "Type into <pane name>". Code is `code` 12/18, does not wrap and scrolls sideways. Only `sh`, `bash` and `shell` fences get Type; every fence gets Copy.
- **Type into <pane>** names the pane it will type into (the tab's first live terminal; "Type into pane" when there is none, which says to split a terminal first). It takes the same confirm path as agent proposals: the command fills the pane's CLI input, then the approve dialog (`approveBeforeType`) shows the exact bytes and the target, and nothing reaches the PTY until you approve (`sendApprovedInject` with a server nonce). The notebook never writes to a PTY itself.
- **Callout:** a blockquote is a box on `stage` (`line`, `radius-md`) with an info icon in `ink-muted`, `role="note"`. No side bar.
- **Not built (proposed on the boards):** the warning callout (`> [!WARNING]` on `signal-soft`), checklists and GFM tables. The editor parses CommonMark with prosemirror-markdown's default schema, which has none of them; each needs a schema node, a markdown-it plugin and a serializer rule, so they wait for their own change.
- Tests: `tests/notebook.e2e.ts` (one H1, the target pane in the meta line and on the button, Copy on every fence and Type only on shell fences, the callout, no wrapping, Type stops at the approve dialog, rename), and the `notebook pane` shots in `tests/visual.e2e.ts`.
