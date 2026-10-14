# First run and host import

The window-wide screens a new Keel shows before the Vault (docs/ui-redesign.md
§5.15, boards `Onboard1`, `Onboard2`; `Onboard3`, the Agent step, is phase 6),
and the same host import from the command palette.

## Sub-features

- When: keel serve seeded a fresh `keel.json` (its `firstRun` flag) and the
  vault was not opened by `KEEL_VAULT_PASSPHRASE` (a scripted setup has no
  first run), or the vault needs setup at all (no keychain, no vault yet).
  An existing `keel.json` from before the flag never starts a first run.
  `GET /api/vault` reports `firstRun`, `needsSetup`, `mode`
  (`keychain` | `passphrase`), `keyringOff`, `path`, `sessions`.
- Step 1, `Welcome to Keel`: two cards in a radiogroup `How to unlock the
  vault`. The keychain card is the choice only when the keychain already
  made the vault (keel serve does that at start when the keychain answers);
  otherwise it is disabled and says why (`didn't answer`, or
  `KEEL_KEYRING=off`). The passphrase card shows `Passphrase` and `Repeat
  passphrase`; `Create vault` posts `/api/vault/setup`, which only creates a
  vault that does not exist (409 otherwise). A box shows the vault path,
  the encryption and what the vault holds. `I already have a keel.vault…`
  explains where to put one; it copies nothing.
- Step 2, `Bring your hosts`: keel serve parses `KEEL_SSH_DIR/config`
  (default `~/.ssh/config`) read-only. A table (`Hosts in ~/.ssh/config`)
  has a checkbox per concrete Host name (`Add <alias>`), the address
  (`:port` when not 22), user, key file names and `Via`. Defaults: new
  hosts on; hosts already saved (same address, user and port) off with
  `Already saved`; ProxyCommand / ProxyJump hosts off with a `signal` badge
  (`ProxyJump bastion, not used`) and importable without the proxy.
  Wildcard and negated names (`*`, `?`, `!`) are a `Patterns` group, never
  rows. Match blocks, bad lines, loops and limits are in `N notes about this
  config`.
- Keys (`Private keys` group): one switch per IdentityFile the hosts name,
  `Copy <path> into the vault`, all off. A key with a passphrase shows
  `Passphrase for <name>` when on; a missing or wrong one is an inline error
  and nothing is saved. A copied key becomes a Keychain identity (kind key,
  passphrase in the vault) and the hosts that name it use it; other hosts
  are added with password sign-in. Originals are only read.
- `Add N hosts` posts `/api/ssh-config/import` with aliases and key paths
  only; keel serve parses again and refuses names or paths the config does
  not hold. `Skip, add hosts later` and the import both post
  `/api/first-run/done`. `Back` returns to step 1 (Continue).
- Command palette `Import hosts from ~/.ssh/config`: step 2 alone, region
  `Import hosts`, `Cancel` instead of Skip, no steps, Escape closes.

## How to get to it (user POV)

A new Mac whose keychain refuses Keel, or `KEEL_KEYRING=off` with no
`KEEL_VAULT_PASSPHRASE`, on an empty data dir. Any time later: ⌘K, `Import
hosts`.

## Driving it with e2e

`tests/onboarding.e2e.ts`. The runner's keel serve has a passphrase, so the
test starts a second one with `startScratchKeel(name)` from `tests/keel.ts`
(no passphrase, `KEEL_KEYRING=off`, `KEEL_SSH_DIR` = the fixture, data in
`.e2e/first-run-data/<name>`, stopped by PID) and opens its `openUrl`.
Regions: `getByRole('region', 'Welcome to Keel' | 'Bring your hosts' |
'Import hosts')`; `getByRole('list', 'Setup progress')`; radios by
`/^Unlock with/`, `/^Use a passphrase/`; checkboxes `Add <alias>`; switches
`Copy ~/.ssh/<file> into the vault`; groups `Patterns`, `Private keys`.
Password fields are blacked out in screenshots by the runner.

The fixture is `tests/fixtures/ssh-home/.ssh` (config with Include, a
two-name Host, a Port, a ProxyCommand, a ProxyJump in `config.d/`,
patterns, a Match block); `scripts/e2e-serve.sh` copies it to
`.e2e/ssh-home/.ssh` and adds `id_e2e_plain` and `id_e2e_locked`
(passphrase `e2e-key-pass`) on every run.

## Proof

`/api/hosts` (names, address, port, `authType` identity only where the key
was copied), `/api/identities` (one key identity, `__saved__`),
`/api/vault` (`firstRun` false), `keel.json` with no `PRIVATE KEY`, key
line or passphrase, and a snapshot of `.e2e/ssh-home/.ssh` (bytes, mode,
mtime) equal before and after. Screenshots `dark|light-first-run-vault`,
`-first-run-hosts`, `-import-saved`.

## Charters

```
first-run-import|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), open the command palette and run Import hosts from ~/.ssh/config; toggle hosts, the select-all box and the key switches, try importing a key with no or a wrong passphrase; report any host imported that was not checked, any wildcard pattern offered as a host, any key copied whose switch was off, and any count or label that disagrees with the table
```
