# Snippets

Saved shell commands. A fresh vault seeds four: list files (`ls -la`),
disk (`df -h`), memory (`free -h`), top cpu.

## Sub-features

- Add with `New snippet`: Name, Command.
- Card menu `Actions`: Edit, Remove (confirm).
- Run: clicking a snippet asks to confirm, then fills it into a terminal's
  input; nothing is typed until confirmed.
- Also in the session sidebar (`Snippets` tab).

## How to get to it (user POV)

Vault rail › `Snippets`, or the sidebar's `Snippets` tab in a session.

## Driving it with e2e

No test yet. Locators: `screen.getByRole('button', 'Snippets', { visible:
true }).first()` (the sidebar has a second one), `getByRole('button', 'New
snippet')`, a card as `getByRole('button', 'disk df -h')`,
`getByRole('dialog').filter({ hasText: 'New snippet' })` with
`getByLabel('Name' | 'Command')`.

## Proof

On screen: the card shows name and command. Store: `/api/snippets`. A run
proof: the confirm dialog shows the command, and after confirming the
terminal's scrollback holds its output.

## Charters

```
snippets-run|web|skeptic|Starting at the Vault (the app opens there), open a Local Shell tab, then run the disk snippet from the sidebar; check that nothing reaches the terminal before you confirm and that exactly the snippet's command runs after
snippets-input|web|fuzzer|Starting at the Vault (the app opens there), create snippets with an empty name, a 300-character command, unicode, and a multi-line command; report what is accepted, trimmed, or lost
```

## Gotchas

- `free -h` and `ps aux --sort` are Linux flags; on macOS they print an
  error. That is the seed data, not a bug.
