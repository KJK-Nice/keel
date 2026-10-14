# Feature map

One file per user-facing feature of Keel's UI, from the user's point of
view. Each names the test that pins it, the locators that matter, the proof,
and its bug-bash charters. Keel is one page (`/`, after the token
redirect); "route" below is where in that page the feature lives.

Not mapped yet: Port forwarding, Logs, Settings (Appearance, Terminal),
groups and folders, Sync input, Quick connect, host-key trust (needs an SSH server), and
the redesign screens in `docs/ui-redesign.md` §5 as they land.
Map one when a change touches it.

| Feature | Route | Test | Status |
| --- | --- | --- | --- |
| Theme screens (dark, light) | Vault sidebar, tab pages, terminal, palette | `tests/visual.e2e.ts` | tested (screenshots, no pixel assertions) |
| Vault sidebar and tab pages | Sidebar (⌘B), footer, segments; ⌘1/⌘9; restore on reload | `tests/sidebar.e2e.ts` | tested |
| [Hosts](hosts.md) | Vault sidebar › Hosts (default) | `tests/hosts.e2e.ts`, `tests/smoke.e2e.ts` | tested |
| [Local terminal](terminal.md) | Vault sidebar › Hosts › `Local Shell`, then the session tab | `tests/terminal.e2e.ts` | tested |
| [Command palette](command-palette.md) | ⌘K anywhere | `tests/palette.e2e.ts` | tested |
| [New tab page](newtab.md) | ⌘T, `+`, or the empty window | `tests/newtab.e2e.ts` | tested |
| [Share and Join](share.md) | Titlebar `Share`; New tab › Join session | `tests/share.e2e.ts` | tested (one window joins its own share) |
| [First run and host import](onboarding.md) | Window-wide on a new vault; ⌘K › Import hosts | `tests/onboarding.e2e.ts` (second keel serve, keychain off) | tested |
| [Unlock vault](unlock.md) | Window-wide when the vault is locked | `tests/onboarding.e2e.ts` (restart) | tested (passphrase vault; keychain paths by hand) |
| [Keychain](keychain.md) | Vault sidebar footer › Keychain (tab page) | `tests/sidebar.e2e.ts` (opens, ⌘N) | partly tested |
| [Snippets](snippets.md) | Vault sidebar › Snippets, Snippets tab page | `tests/sidebar.e2e.ts` (opens) | partly tested |
| [Notebooks](notebooks.md) | Vault sidebar › Snippets › Notebooks, Notebooks tab page | `tests/sidebar.e2e.ts` (opens) | partly tested |
