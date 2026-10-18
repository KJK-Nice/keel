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

### 5.4.1 Agent sessions (`AgentSessions`; `AgentPage` is a follow-up)
- **Header**: the conversation's title (its first question, redacted, until renamed) is a button that opens **Recent conversations**; then New conversation and Close. The scope chip moves to a sub row with "Saved in the vault" (or "Not saved · kept while open" when history is off) on the right.
- **Recent conversations** (popover under the title): Search conversations (title or host), groups Today / Yesterday / Older with counts, one row per conversation (title, host badges it read or proposed for, time, "N proposals"), Rename (inline field, ↵ saves, esc cancels) and Delete per row; footer: the retention ("Kept 30 days") and Delete all (asks first). ↑↓ moves between rows, esc closes. "Open all in a tab" (the `AgentPage` board: three-column Agent tab page) is not built yet.
- **New conversation** starts a fresh thread and keeps the old one; nothing is deleted.
- **Resume**: picking a row shows its saved thread (You · time, the trace lines, answers, proposals), then a "Resumed · 2 days ago" divider and the note "Earlier reads stay in this conversation as text. New reads use only the panes in scope now, on hosts that still allow the agent." The next ask continues the conversation: keel serve sends the earlier questions and replies (never pane text, none is kept) and reads the panes in scope again through the Auditor and the scope rules as they are now (§6.2, §6.7).
- **Expired proposals**: a proposal keel serve no longer holds (loaded from the vault after a restart, or after the conversation left memory) shows an Expired badge, "Panes may have changed since this was proposed.", "Proposed <time> · not typed" (or "typed <time>"), and only **Copy** and **Propose again**. There is no Type and no Edit: keel serve refuses both for it (`AuthorizeType` looks proposals up in memory only). Propose again asks keel serve for a new proposal from the saved command: rated again (code rating, never looser than the saved one), the host's production flag now (a deleted host counts as production), aimed at the same pane only while it is still open on the same host and that host still has "Agent can read" on (else Copy only), logged as `agent.suggest`; the new card then goes through the normal approve-before-type path. A proposal from this run that is still in memory stays live when its conversation is reopened.
- **Storage**: `keel.chats`, next to `keel.vault`, age-encrypted to an X25519 key that is itself a vault secret (`agentchats:key`, made on the first save). So transcripts open only with the vault, and a save re-encrypts `keel.chats` only, not `keel.vault` (no scrypt per save in passphrase mode). Nothing goes to `keel.json`. Saved per conversation: id, title, created and updated times, and per turn the question as sent (redacted), the shown answer (redacted), the model's reply kept for context (redacted, truncated), the trace (host, line count, redaction counts; never pane text) and the proposals (command passed through the Redactor again, so a vault secret typed into an edit is not kept; risk, host, pane id, proposed and typed times). At most 200 conversations, 100 turns each and 8 MiB of saved text in all; the least recently used go first. Saved answers are model text: they can quote pane output the model saw, in the redacted form it was sent (a placeholder such as `[secret:1]`, never the value). Saves run on one background worker in order (never on the session socket's read loop); a failed save is logged once per reason in `keel.log` (no content) and the sub row says "Not saved" with the reason. A `keel.chats` that cannot be decrypted or parsed is never overwritten: the list says it cannot be read, new conversations go on in memory only, and Delete all replaces it. The decrypted conversations and their key are cached in keel serve while the vault is open; anything that locks the vault again must drop them (`store.forgetChats`). Known limit: resuming a conversation can push the least recently used one out of memory (16 are held), and that one's proposals then come back Expired.
- **Retention**: Settings · Agent → Keep conversations (`historyDays`: only while open, 7, 30, 90 days, 1 year). keel serve deletes conversations last used before that on start, every hour, on each list and at once when the setting changes; `keel.log` gets "N agent conversations past the retention deleted from the vault", never their content. "Only while open" saves nothing and deletes what was saved, `keel.chats` and its key with it; the open conversation lives in memory until keel serve stops or it idles out. Delete all (here or in Settings) deletes both the in-memory and the saved conversations, removes `keel.chats` and its key from the vault, and so also replaces an unreadable file; the next save starts under a new key.
- API: `GET /api/agent/conversations` (list, after pruning), `GET /api/agent/conversations/{id}` (transcript; reads no pane, sends nothing), `PATCH /api/agent/conversations/{id}` `{title}`, `POST /api/agent/conversations/{id}/proposals/{pid}/again`, `DELETE /api/agent/conversations/{id}`, `DELETE /api/agent/conversations`.

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
- Provider (connect; key stored in the vault). Suggestions in panes (on/off; Notice automatically / Only lines starting with `#`). Default panes per conversation (focused only / pick each time / every pane in tab). Scrollback lines per pane (50–1000, default 200). Safety (two always-on rows, ⇧⇥ rule, redaction of secrets / IPs / hostnames). History retention and delete all (§5.4.1: saved in the vault, pruned by `historyDays`).

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

**Limit of "From this pane's scrollback" (6c+).** Scrollback is untrusted output, and nothing proves a shell drew a line or that its command ran. So a candidate counts only when its prompt is exactly the pane's current prompt (the line being edited); with no readable current prompt there are none. Lines abandoned with ^C (or showing ^Z, ^D, ^[), lines touched by cursor moves, backspaces or erases, and commands with invisible format characters are dropped. Still, any program that writes that exact prompt string followed by text can pose as a command: a file printed with cat, a login banner, a REPL, a remote program, or a shell-like prompt that happens to match. The label says only where the line was seen, and the risk rating and the ⇥ / ⇧⇥ gates apply to it as to any suggestion. These suggestions do not use OSC 133 shell-integration marks yet; agent panel excerpts do (§14).

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
- Command records per pane, in keel serve memory only (§14): stream offsets of the OSC 133 marks, exit status, cwd, times. Agent trace steps and saved `AgentChatStep`s gain `command` ("exit 2", "still running"), never the command text.
- Saved agent conversations: `keel.chats` (age, X25519) with the key in the vault as `agentchats:key`; schema `store.AgentChat` (`internal/store/agentchats.go`, §5.4.1).

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
| Shell integration · OSC 133 | Parse OSC 133 A/B/C/D and OSC 7 from PTY output in keel serve (command records per pane, bounded), agent excerpts of the last command with its exit status through the Auditor, gutter marks and an exit chip in xterm, Previous / Next command in the palette, opt-in shell snippets in the README (§14) | Foundation for the error chip, runbook results and per-host results; no rc file is ever edited |
| Agent sessions | Conversations saved encrypted in the vault, `historyDays` retention, the Recent conversations list in the panel (search, resume, rename, delete), Expired proposals with Copy / Propose again (§5.4.1, board `AgentSessions`) | The Agent tab page (board `AgentPage`) is the next PR |

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

## 14. Shell integration marks (OSC 133)

Status: foundation built · 2026-10-18 · no board (the UI is two small marks inside the terminal). Groundwork for later features: an error chip with actions, runbook results per step and per host by exit code, and agent excerpts that follow commands.

**What is read.** A shell with integration wraps its prompt and each command in OSC sequences (FinalTerm semantic prompts): `ESC ] 133;A` the prompt starts, `133;B` the prompt is drawn and the typed command follows, `133;C` the command was accepted and its output follows, `133;D[;<exit>]` the command ended. Each ends with BEL or ST (`ESC \`). Keel also reads `ESC ] 7;file://host/path` (the working directory). Every other OSC (titles, hyperlinks, clipboard) is ignored by this code.

**Where.** Two readers of the same byte stream, neither of which changes it:

- **keel serve** (`internal/shellmark`): a streaming parser fed from `Session.output`, under the session lock, with the scrollback ring's write counter as the stream offset (`ring.Mark`). A tracker turns marks into **command records** per pane: `Seq`, `PromptStart`, `CommandStart` / `CommandTextEnd` (the typed command, when the shell sent B), `OutputStart`, `OutputEnd`, `Done`, `Exit` / `HasExit`, `Cwd` (the last OSC 7), `Started`, `Finished`. Records hold offsets, not text: the text is read from the ring (`ring.Range`) while it still holds it. At most 64 records per pane (the oldest go); not saved anywhere, gone with the session. A reconnect resets the parser and the open prompt (a half sequence from the old shell must not join the new one); records stay.
- **The terminal** (`js/lib/cmdmarks.js`): an xterm OSC 133 handler draws the marks (below). It parses with the same rules, so both sides agree on what a mark is. A pane re-attached later replays the ring, so its marks come back with its text.

**Rules.** A command exists once its output starts (C). A prompt with no C (Enter on an empty line) leaves no record. D closes the latest command only if it is still open, so a stray or repeated D changes nothing; D with no C draws nothing. Marks are flat: with nested shells (ssh from a shell with integration into one that has it too) the inner commands are recorded and the outer one stays open with no exit status.

**Security.** OSC data is untrusted remote output.

- Strict parsing: payloads are printable ASCII; a control byte inside an OSC skips it; CAN/SUB cancel it; `ESC ]` inside an OSC abandons it and starts the new one. An exit status is 1–10 digits within int32 and nothing else (no sign, no spaces); anything else is "not reported". A cwd must be `file://host/path`, decode to valid UTF-8 with no control characters, and be at most 1024 bytes.
- Caps: an OSC longer than 2048 bytes is consumed and dropped, so the parser holds at most that much whatever it is fed; 64 records per pane in keel serve, 1000 prompt markers per terminal in the UI; the command text read for the agent is cut at 4 KiB, its output to the scrollback-lines setting.
- Marks authorize nothing and trigger nothing: no write, no prompt, no network. They only draw a mark and choose which part of a pane's scrollback the agent reads. A program can print fake marks (or hide its output behind a fake D); the excerpt says it comes from shell-integration marks and the risk rating and ⇥ / ⇧⇥ gates apply to every suggestion as before.
- Fuzzed (`FuzzParser`) and tested split at every byte.

**Agent (§6.2, §6.4, §6.7).** When the agent reads a pane in scope (`Auditor.ReadPane`) and keel serve knows a last command for it (`agent.CommandSource`, satisfied by the runtime), the excerpt is that command instead of the tail: `(in <cwd>)`, `$ <command>`, the last N lines of its output (N = Scrollback lines per pane; "(earlier output not included)" when cut), and `[exit 2]`, `[exit status not reported]` or `[still running]`. Scope is checked before anything is asked. The text goes through `scrollback.Read`, `Normalize` and the request's Redactor like any pane read, and the read is written to `keel.log` as `agent.read` ("agent read web-1 · last command, exit 2 · 12 lines of output · 1 redaction"). The request header names it "last command (from shell integration marks), exit 2". The trace shows "Read web-1 · last command, exit 2 · 12 lines of output"; saved conversations keep that label (`command`), never the command. Without marks, or once the command's output has left the ring, the agent reads the last lines as before. History-first suggestions (§5.5) do not use the marks yet.

**UI.**

- A **gutter mark** per finished command: a `space-1` dot in the terminal's left inset (`space-2`), on the prompt line, never over text. `tide` when the command exited 0 (or did not report), `flare` otherwise. Its tooltip says "Command finished · exit N". No coloured side bar (§3).
- A failed command also gets an **exit chip**, `k-badge k-badge--flare k-badge--mono` reading "exit N", at the right end of its prompt line: the word that keeps state from being colour alone.
- **Previous command / Next command** in the palette (Terminal group) scroll the focused terminal to the prompt before or after the top of the view. No shortcut (none is planned in §8). With no marks in the pane a toast says to turn on shell integration.
- Classes `k-cmdmark`, `k-cmdmark-dot`, `k-cmdmark--ok`, `k-cmdmark--failed`, `k-cmdchip` (reference in `docs/design/components.css`, app in `keel.css`). xterm's decorations are a proposed API in xterm 6, so terminals are created with `allowProposedApi`.

**Turning it on.** Keel never edits a remote host's rc files and does not inject anything into the Local Shell: doing that safely means a temporary `ZDOTDIR` or `--rcfile` that changes the order startup files load in, which is not trivial across bash, zsh and fish and would differ from how the user's own terminal starts. fish 4.0 and later send the marks by themselves. For bash (4.4+, which has `PS0`), zsh and fish 3, the README (Shell integration) has a snippet to paste into the shell's own rc file, per host, by the user. Checked in a PTY: bash 5 and zsh 5.9 produce A B C D;n per command and nothing for an empty line; fish 4 sends `A;click_events=1` and B natively. A copyable snippet in the Edit host dialog is a follow-up.

**Not built yet.** Actions on the chip (copy the output, ask the agent about this command), runbook and per-host results by exit code, history suggestions from marked commands, the cwd anywhere in the UI, more than the last command in an agent excerpt, an API for the records.

Tests: `internal/shellmark` (each mark, every split, malformed, huge, nested, strict exit codes, bounded tracker, reset, fuzz), `internal/runtime/marks_test.go` (ring ranges, `LastCommand` from split chunks, the scrollback mask, a running command, output out of the ring, the command cap), `internal/agent/service/lastcommand_test.go` (excerpt, redaction, trace and `agent.read`; scope first; fallback to the tail; running and long output), `js/test/cmdmarks.test.mjs`, and `tests/cmdmarks.e2e.ts` (a Local Shell printing marks gets one ok mark, one failed mark with "exit 3" in the inset and at the line's end; the agent's trace and `keel.log` show the last command).
