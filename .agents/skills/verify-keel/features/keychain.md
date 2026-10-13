# Keychain

Reusable SSH identities (password or private key) that hosts attach to.
Separate from the OS keychain that wraps `keel.vault`.

## Sub-features

- Add with `New identity`: Name, Username, Kind (Password, Private key),
  Password or Private key + passphrase; `Save identity`.
- Card menu `Actions`: Edit, Remove (confirm).
- Attach in a host's Authentication › Keychain identity › Identity.

## How to get to it (user POV)

Vault rail › `Keychain`. Empty state: "No identities yet".

## Driving it with e2e

No test yet. Locators: `screen.getByRole('button', 'Keychain')`,
`getByRole('button', 'New identity')`,
`getByRole('dialog').filter({ hasText: 'New identity' })` with
`getByLabel('Name' | 'Username' | 'Password')`, `getByRole('combobox',
'Kind')`, `getByRole('button', 'Save identity')`.

## Proof

On screen: the identity card appears; the host dialog's Identity select
lists it. Store: `/api/identities` holds it; `keelJson()` never holds its
password or key.

## Charters

```
keychain-attach|web|skeptic|Starting at the Vault (the app opens there), add a password identity in Keychain, attach it to a new host, then rename and remove the identity; check what the host shows after each step
```

## Gotchas

- Removing an identity a host uses: check what the host falls back to
  before calling it a bug; read `internal/store` first.
