# Feature map

One file per user-facing feature of Keel's UI, from the user's point of
view. Each names the test that pins it, the locators that matter, the proof,
and its bug-bash charters. Keel is one page (`/`, after the token
redirect); "route" below is where in that page the feature lives.

Not mapped yet: Port forwarding, Logs, Settings (Appearance, Terminal),
groups and folders, split panes and Sync input, Quick connect, Share and
Join (need a signal server), host-key trust (needs an SSH server), the
vault unlock gate (needs a locked vault, which this setup avoids on
purpose), and the redesign screens in `docs/ui-redesign.md` §5 as they land.
Map one when a change touches it.

| Feature | Route | Test | Status |
| --- | --- | --- | --- |
| Theme screens (dark, light) | Vault pages, terminal, palette | `tests/visual.e2e.ts` | tested (screenshots, no pixel assertions) |
| [Hosts](hosts.md) | Vault › Hosts (default) | `tests/hosts.e2e.ts`, `tests/smoke.e2e.ts` | tested |
| [Local terminal](terminal.md) | Vault › Hosts › `Terminal`, then the session tab | `tests/terminal.e2e.ts` | tested |
| [Command palette](command-palette.md) | ⌘K anywhere | none | untested |
| [Keychain](keychain.md) | Vault › Keychain | none | untested |
| [Snippets](snippets.md) | Vault › Snippets, sidebar Snippets | none | untested |
| [Notebooks](notebooks.md) | Vault › Notebooks, sidebar Notebooks | none | untested |
