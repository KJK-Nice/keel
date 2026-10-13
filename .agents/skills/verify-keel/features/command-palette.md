# Command palette

⌘K (or ⌘⇧P) opens a filterable list in four groups, in this order: **Open
now**, **Hosts**, **Actions**, **Snippets** (docs/ui-redesign.md §5.2).

## Sub-features

- Open with ⌘K (Meta+k) from any focused control, including the terminal.
- Filter by typing in the combobox; every word must appear in the row's
  title, hint, group or shortcut. Matches are marked.
- **Open now**: every tab (`tab N`, state dot, `driving` or `following`,
  `⌘N`), plus one row per pane (`pane in tab N`) for tabs with more than one.
  Picking one focuses it and never starts a session.
- **Hosts**: `New session on <host>` with `user@address` and the host's first
  tag. `↵` opens a session; `⌥↵` opens it in a split of the focused pane (a
  new session when no session tab is showing).
- **Actions**: tabs, panes, vault pages, notebooks, terminal text size,
  runtime; each row's hint is the section it used to sit in (`Tabs`, `Vault`,
  `Panes`, `Terminal`, `Notebooks`).
- **Snippets**: `Type snippet: <name>` with its first line and an `Approve
  first` badge. Picking one opens the approve-before-type dialog (the exact
  bytes, the target session); nothing is typed until `Type`, and `⌥↵` does
  not skip it.
- Footer: `↑↓ move · ↵ open · ⌥↵ open in split`; `esc` closes.
- Not here yet: "Ask the agent about …" (⌘↵), phase 6.

## How to get to it (user POV)

Press ⌘K anywhere in the app. No account.

## Driving it with e2e

`tests/palette.e2e.ts` pins the group order, picking an open tab, ⌥↵ split
and the snippet approval. Locators:
`screen.getByRole('dialog', 'Command palette')`,
`getByRole('combobox', 'Run a command or connect a host…')`, rows as
`getByRole('option').filter({ hasText: 'New session on Local Shell' })` (the
option's name is title, hint, badge and keys joined, so a `getByRole` name
only matches a whole name; filter by text instead; shortcut keys read
`⌘ 1`). The approval is `getByRole('dialog').filter({ hasText: 'Type into
the terminal?' })` with `Cancel` and `Type`. `press('Alt+Enter')` is ⌥↵.
Live walk: `press Meta+k`, `observe`, `type` a filter, `press Enter`.

## Proof

On screen: the dialog opens with the combobox focused, the rows come in group
order, Enter does the row's action and the palette closes. For a snippet:
the approval dialog shows the bytes, and `/api/sessions/<id>/scrollback`
holds nothing from it after Cancel and the command's output after Type.

## Charters

```
palette-run|web|default|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell, press Meta+K and run each Actions option once; report options that do nothing, do something else, or leave the palette open
palette-open-now|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell twice, split one, then use the palette's Open now rows to move between tabs and panes; check the session count never grows and the focused pane is the one you picked
palette-snippet|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell, run a snippet from the palette and press Cancel, Escape and Type in turn; check nothing reaches the terminal before Type
```

## Gotchas

- The `.press('Meta+k')` must target a focused control; a press on the body
  may not reach the listener.
- The same chrome shortcut within 300 ms is dropped as a key repeat: wait
  before a second `Meta+w`.
