# Unlock vault

The window-wide screen when `keel.vault` exists and keel serve could not
open it at start (docs/ui-redesign.md §5.14, board `Unlock`).

## Sub-features

- When: `/api/health` and `/api/vault` say `locked` and not `needsSetup`.
  keel serve tries the OS keychain first at every start (unless
  `KEEL_KEYRING=off`), then `KEEL_VAULT_PASSPHRASE`; this screen is what is
  left.
- `Unlock your vault`, the count of sessions waiting in keel serve, and a
  banner by vault mode: a passphrase vault (`haze`) says the keychain does
  not hold its key (or that `KEEL_KEYRING=off`); a keychain vault whose
  keychain did not answer (`signal`) says a passphrase cannot open it and
  hides the field.
- `Vault passphrase` (with `Show passphrase`), `Unlock` (↵). A wrong one is
  an inline alert, `That passphrase doesn't open the vault.`; nothing else
  changes. Unlock posts `/api/vault/unlock`, which never creates, re-keys or
  overwrites a vault.
- `Try macOS Keychain again next time`: shown checked and not changeable,
  with `Keel always asks macOS Keychain first when keel serve starts.`
  (that is keel serve's behavior; there is no setting to skip it). Hidden
  under `KEEL_KEYRING=off`.
- The footer: a lost passphrase cannot be recovered; sessions already
  running in keel serve keep working while the vault is locked.

## How to get to it (user POV)

Create a vault with a passphrase (First run with the keychain off), quit
and stop keel serve, open Keel again.

## Driving it with e2e

`tests/onboarding.e2e.ts`, at its end: `keel.restart()` on the scratch keel
serve (`startScratchKeel`), `app.open(`${keel.baseUrl}/?name=e2e`)` (the
cookie is still valid), then `getByRole('region', 'Unlock your vault')`,
`getByLabel('Vault passphrase')`, `getByRole('button', 'Unlock')`,
`getByRole('alert')`.

The keychain paths (a keychain vault that will not open, the real Keychain
prompt) cannot run in e2e: it never touches the OS keychain. Check them by
hand on a Mac.

## Proof

After Unlock the Vault sidebar lists the hosts saved before the restart;
`/api/vault` is `locked: false`, `mode: passphrase`, `firstRun: false`.
Screenshots `dark|light-unlock`, `dark-unlock-error`.

## Charters

```
unlock-wrong|web|fuzzer|Starting at the Vault sidebar (the app opens with it beside a New tab), this window's vault is open; open the command palette and Settings and look for any way to re-key, reset or overwrite the vault from the UI; report any control that changes keel.vault without the current passphrase
```
