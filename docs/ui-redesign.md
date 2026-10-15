# Keel UI redesign: spec

Status: design approved for build · 2026-10-13

- Tokens and components (source of truth in the repo): [`docs/design/`](design/)
  - `tokens.json`: every token, both themes. `tokens.css`: the same as CSS custom properties.
  - `components.css`: reference `k-*` component styles. `README.md`: usage rules (the brand book).
- Design system (live, browsable): https://claude.ai/artifact/3RUKjVH2znuvieFDaDjkS1
- Screens (canvas, 16 boards): https://claude.ai/artifact/GJaVi8YQSrxoNiDVXoi2sW

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
- Notebook editor redesign.

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
| Vault = pinned tab 1 (Hosts, Keychain, Forwards, Snippets, Logs, Settings) | **Vault sidebar** (left, 252px, collapsible, every tab): host tree by folder, segmented Hosts / Snippets / Forwards, footer Keychain · Logs · Settings |
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

### 5.9 Settings · Agent (`Settings`)
- Provider (connect; key stored in the vault). Suggestions in panes (on/off; Notice automatically / Only lines starting with `#`). Default panes per conversation (focused only / pick each time / every pane in tab). Scrollback lines per pane (50–1000, default 200). Safety (two always-on rows, ⇧⇥ rule, redaction of secrets / IPs / hostnames). History retention and delete all.

### 5.10 Port forwarding (`Forwards`)
- Table: name, type (-L/-R/-D), route `local → remote`, through host, status, traffic, on/off switch, more.
- Bind on `0.0.0.0` shows a `signal` "Every interface" badge. Failures show cause and Retry / Edit.

### 5.11 Keychain (`Keychain`)
- List (SSH keys, Passwords) + detail: fingerprint, storage, passphrase, comment, public key with Copy, "Used by N hosts". Keys in use cannot be deleted. Old algorithms get a `signal` badge.

### 5.12 Snippets (`Snippets`)
- List + editor: commands (one per line), `{{var}}` fields with defaults, "Offer on hosts tagged", kind **Read-only / Changes state**, preview of exact bytes. Typing always goes through Approve before type; Changes state on a production host needs ⇧↵.

### 5.13 Logs (`Logs`)
- Filters: All, Sessions, Forwards, Vault, Sharing, **Agent**. Rows: time, kind, target, event.
- Agent events are logged: what it read (panes, line counts, redactions) and every insert/type from a suggestion.

### 5.14 Unlock vault (`Unlock`)
- Shown when the keychain does not answer. Passphrase field, "Try macOS Keychain again next time", Unlock. States that running sessions keep working and that a lost passphrase cannot be recovered.

### 5.15 First run (`Onboard1–3`)
1. Create vault: Keychain (recommended) or passphrase; shows vault path and contents; "I already have a keel.vault…".
2. Bring your hosts: parse `~/.ssh/config` (read-only), table with checkboxes, wildcard patterns listed apart (not importable), ProxyCommand / ProxyJump hosts flagged and importable without the proxy, already-saved hosts unchecked; optional copy of referenced private keys into the vault, one switch per key, off by default (originals untouched).
3. Agent: three promises, provider picker, suggestions toggle, Set up later / Open Keel.

## 6. Agent invariants (must hold in code, not only in UI)

1. The agent never writes to a PTY. Only a user action (Type / ⇥ / ⇧⇥ / ↵) does.
2. The agent reads only panes in the conversation's scope, and only hosts with "Agent can read" on.
3. Vault contents (passwords, keys, passphrases, snippet bodies unless the user adds them) are never sent.
4. Redaction runs before anything leaves the machine. Secret-looking values are always redacted.
5. Destructive or state-changing commands cannot be inserted with plain ⇥; on production hosts they need ⇧⇥.
6. A follower cannot type agent output into a pane they follow; the proposal is not offered.
7. Every read and every insert is written to `keel.log`.

## 7. Plain-language detection

- Default: automatic. A classifier decides "plain language vs command" for the current prompt line; it never blocks typing and never changes what ↵ sends.
- Fallback setting: only lines starting with `#` (safe if ↵ is pressed by mistake, because the shell treats it as a comment).
- Candidate for the classifier and the command/risk judgment: Jev (TypeSafe). To evaluate: latency budget (< 150 ms for the Notice hint), offline behaviour, cost per keystroke burst (debounce on pause, not per key).
- Risk label (Read-only / Changes state / Destructive) comes from the same judgment and is shown on every suggestion.

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
| ⌘↵ | Ask agent (palette) · Save (dialogs) | new |
| ⌘N, ⌘⇧O, ⌘J, ⌘,, ⌘W, ⌘⇧T, ⌘⇧D, ⌘⇧E, ⌘± / ⌘0 | as today | ⌘N is "new item" on Forwards/Keychain/Snippets pages |

## 9. Data model additions

- Host: `color` (ANSI index 9–14), `tags[]`, `agentReadable` (bool, default true), `production` (bool, default false).
- Snippet: `kind` (`readonly` | `changes-state`), `vars` with defaults, `offerOnTags[]`.
- Settings: agent provider ref (secret in vault), detection mode, default scope, scrollback lines, redaction toggles, history retention.
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

## 11. Open questions

1. Shortcut for the sidebar toggle: ⌘B (phase 2; it was the right session rail's toggle, which the Vault sidebar replaced). **⌘I (decided, 6b):** free on macOS (no menu, term.js or xterm use; Ctrl-I stays Tab for readline because ⌘ is the app modifier). On Windows/Linux the app modifier is Ctrl and Ctrl+I is Tab in a terminal, so the panel takes **Ctrl+Shift+I** there (menu and keyboard agree, `internal/desktop/shortcuts_test.go`); in a plain Chromium browser tab Ctrl+Shift+I opens DevTools first, so the titlebar button and the palette are the way there.
2. Agent provider(s) to support first. **Decided (6a):** Google Gemini first, behind a provider interface (`internal/agent/provider`); the key is a vault secret.
3. Jev vs a local model for detection: latency and offline requirements. **Decided (6c):** Jev (`internal/agent/provider/jev`, model pinned to `jev-1.13.0`), one Noul per typing pause (350 ms), notice at P ≥ 0.70, a 900 ms timeout. With no Jev key, offline, an error or a timeout, only `#` lines are noticed, silently. Without a model key there is no notice at all. Jev also rates every suggestion (one Choice per command, one request), and may only make the code rating stricter. Questions and thresholds: `internal/agent/service/suggestq.go`. Measured from a home connection: 270–470 ms per call, so the < 150 ms budget holds only near the endpoint.
4. Should "Treat as production" default on for hosts in a folder named `production`? **Decided (6a):** yes, at the moment a host is created in, or moved into, a group whose own name is `production` (any case; the direct parent only, not an ancestor). Deleting a group whose hosts move up into a `production` parent counts as a move. Nothing else sets it (loading or renaming never does), and the user can turn it off. A value sent on create wins over the rule, so **6d's Edit host dialog must prefill "Treat as production" on when the chosen group is named `production`**, so an untouched dialog still gets it.
5. Light theme: ship together with dark in phase 0, or after?
