# Keychain

Reusable SSH identities (password or private key) that hosts attach to, on
their own tab page. Separate from the OS keychain that wraps `keel.vault`.

## Sub-features

- List (SSH keys, then Passwords) with a filter, and a detail for the
  selected identity: kind, username, whether a key, passphrase or password
  is stored (never the secret), and `Used by N hosts` with their names.
- Add with `New identity` (or ⌘N on this page): Name, Username, Kind
  (Password, Private key), Password or Private key + passphrase;
  `Save identity`.
- Detail `Edit` opens the same dialog; `Delete identity` asks to confirm and
  is disabled while hosts use the identity.
- Attach in a host's Authentication › Keychain identity › Identity.

## How to get to it (user POV)

Vault sidebar footer › `Keychain`, or the palette's `Keychain`. It opens as
a `Keychain` tab; opening it again selects that tab. Empty state: "No
identities yet".

## Driving it with e2e

`tests/sidebar.e2e.ts` opens it and pins ⌘N here. Locators:
`screen.getByRole('complementary', 'Vault').getByRole('button',
'Keychain')`, `screen.getByRole('region', 'Keychain')`,
`getByRole('button', 'New identity')`,
`getByRole('dialog').filter({ hasText: 'New identity' })` with
`getByLabel('Name' | 'Username' | 'Password')`, `getByRole('combobox',
'Kind')`, `getByRole('button', 'Save identity')`; a list item as
`getByRole('button', /^<name>\b/)` inside the region.

## Proof

On screen: the identity appears under its kind and is selected; the host
dialog's Identity select lists it; after a host uses it the detail says
`Used by 1 host` and Delete is disabled. Store: `/api/identities` holds it;
`keelJson()` never holds its password or key.

## Charters

```
keychain-attach|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), add a password identity in Keychain, attach it to a new host, then rename it, detach it and remove the identity; check what the detail and the host show after each step
```

## Gotchas

- Fingerprint, public key, algorithm and "old algorithm" badges are not
  shown: the backend does not expose them yet (phase 2 deferred them).
