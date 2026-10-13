# Snippets

Saved shell commands. A fresh vault seeds four: list files (`ls -la`),
disk (`df -h`), memory (`free -h`), top cpu.

## Sub-features

- Vault sidebar › `Snippets` segment: one row per snippet; clicking one asks
  Approve before type, then types it into the focused pane (or the session
  tab shown last). Notebooks sit below it.
- Snippets tab page (`Manage snippets`, or the palette's `Snippets`): list
  with a filter, and an editor for the selected snippet: name, commands
  (one per line), a preview of the exact bytes with each Enter shown as `↵`
  and the line and byte count, `Save` (⌘S), `Type into pane…`, `Delete
  snippet` (confirm).
- Add with `New snippet` (or ⌘N on the page): Name, Command.

## How to get to it (user POV)

Vault sidebar › `Snippets`, then `Manage snippets` for the page.

## Driving it with e2e

`tests/sidebar.e2e.ts` opens the segment and the page. Locators:
`const vault = screen.getByRole('complementary', 'Vault')`,
`vault.getByRole('button', 'Snippets')`, a sidebar row as
`vault.getByRole('button', /^disk\b/)`, `vault.getByRole('button', 'Manage
snippets')`, `screen.getByRole('region', 'Snippets')`, inside it
`getByRole('button', 'New snippet')`, `getByRole('textbox', 'Snippet
name')`, `getByLabel('Commands')`, `getByRole('button', 'Save')`;
`getByRole('dialog').filter({ hasText: 'New snippet' })` with
`getByLabel('Name' | 'Command')`.

## Proof

On screen: the row shows name and first command; the preview matches the
commands. Store: `/api/snippets`. A run proof: the confirm dialog shows the
command, and after confirming the terminal's scrollback holds its output.

## Charters

```
snippets-run|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell from Hosts, then run the disk snippet from the sidebar's Snippets segment and from the Snippets page's Type into pane; check that nothing reaches the terminal before you confirm and that exactly the snippet's command runs after
snippets-input|web|fuzzer|Starting at the Vault sidebar (the app opens with it beside a New tab), create and edit snippets with an empty name, a 300-character command, unicode, and a multi-line command; report what is accepted, trimmed, or lost, and whether the preview matches what is typed
```

## Gotchas

- `free -h` and `ps aux --sort` are Linux flags; on macOS they print an
  error. That is the seed data, not a bug.
- `{{var}}` fields, "Offer on hosts tagged" and Read-only / Changes state
  kinds from the board are not built (phase 2 deferred them; no backend).
