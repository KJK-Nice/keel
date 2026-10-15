# Agent panel

The right-side panel (docs/ui-redesign.md §5.4, phase 6b): ask about the
panes in scope; the agent reads their last lines through keel serve,
redacts them, and answers with a trace and command proposals. Nothing
reaches a pane until the user presses Type into pane and approves the bytes.
The §6 invariants are its acceptance criteria.

## Sub-features

- Toggle: ⌘I (macOS; Ctrl+Shift+I elsewhere, because Ctrl+I is Tab in a
  terminal), the titlebar `Agent` button, the palette's `Toggle agent
  panel`, the native View › Toggle Agent Panel. Open or closed is kept in
  `localStorage['keel-ui'].agentPanel`.
- Header: scope chip `Reads N of M panes` (opens the pane picker: hosts with
  "Agent can read" off show `Agent can't read` and cannot be picked; presets
  Focused pane / Every pane in tab / None), `New conversation`, `Close agent`.
- Default scope comes from the agent settings (`defaultScope`: focused, tab,
  pick).
- Thread: your message, the trace (`Read <host> · last <n> lines · <k>
  redacted`, skipped panes), the answer, proposal cards.
- Proposal card: `Run on <host>`, `Production` badge, `Read-only` /
  `Changes state` / `Destructive`, the exact bytes (`^U`, the command, `↵`),
  `1 line · 20 bytes`, Edit · Copy · `Type into pane ↵`. Destructive, or
  changes state on a production host: plain Type is refused in the card;
  ⇧ + Type opens the approve dialog. A pane you follow, a closed pane, or a
  proposal with no pane gets no Type button.
- Composer: @-chips for the panes in scope (× removes one), `@` opens the
  picker, ↵ sends, ⇧↵ is a newline, `Stop` cancels the ask.
- No key: an inline `Connect Gemini` card with the provider's free-tier
  warning and a `Gemini API key` field (PUT /api/agent/keys/gemini).
- Joined tabs: no panel and no Agent button (§5.7); ⌘I says why.

## How to get to it (user POV)

Open Local Shell, press ⌘I (or the `Agent` button in the titlebar).

## Driving it with e2e

`tests/agent.e2e.ts`. keel serve runs with `KEEL_AGENT_PROVIDER=fake`
(e2e.config.ts, test only): the agent answers from an in-process fake that
sends nothing anywhere. Its answer says `I was given N pane excerpt(s) and K
redacted value(s)`, and it proposes `echo keel-agent-ok` (read-only),
`touch /tmp/keel-agent-e2e-touched` for a question with "touch", `rm -rf
/tmp/keel-agent-e2e` for "delete", none for "no command". Store a fake key
first with `keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', {key})`;
never a real one.

Locators: `screen.getByRole('complementary', 'Agent')`; inside it
`getByRole('textbox', 'Message the agent')`, `getByRole('button', 'Choose
what the agent can read')`, `getByRole('dialog', 'Panes the agent reads')`
with `menuitemcheckbox` rows, `getByRole('group', 'Proposal')` cards with
`getByRole('button', /Type into pane/)` (⇧: `tap({ modifiers: ['Shift']
})`). The approve dialog: `screen.getByRole('dialog').filter({ hasText:
'Type into the terminal?' })`, buttons `Type` / `Cancel`.

Local Shell is the only host with a shell, and local hosts cannot be
deleted: to test production or "Agent can read" off, PUT those flags on it
and put them back in `finally`.

## Proof

On screen: the trace line, the answer, the card. Store: `/api/logs` has
`agent.read` (lines, redaction counts) and `agent.suggest` per proposal,
and `agent.type` (bytes) only after an approved Type; the scrollback holds
`keel-agent-ok` only after approval. `keel.log` holds counts, never pane
text or secrets.

## Charters

```
agent-scope|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), open two Local Shell tabs, open the agent panel with the Agent button, change the panes it reads with the scope chip and the @ key, ask a question each time, and check the trace names exactly the panes in scope
agent-type|web|default|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell, open the agent panel, ask what to run, then use Edit, Copy and Type into pane on the proposal; check nothing is typed before you approve the dialog and that the card says what happened
```

## Gotchas

- The fake provider is in-process; asking never needs the network. With no
  `KEEL_AGENT_PROVIDER=fake` (a hand-started keel serve) a stored key would
  be sent to Google: do not do that in tests.
- e2e screenshots mask password fields, so the key field looks black.
- Restored sessions from earlier tests count in `M`: assert counts relative
  to what the window shows.
