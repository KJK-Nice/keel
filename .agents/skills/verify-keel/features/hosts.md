# Hosts

The Vault's first page: the saved SSH hosts as cards, with search, groups,
and a menu per card. Secrets typed here go to the encrypted vault.

## Sub-features

- Add with `New host`: Label, Parent Group, Tags, Hostname / IP, Port,
  Username, Authentication (Password, Private key, Keychain identity, Local
  shell), Color, Keepalive, Auto reconnect; `Save host`.
- Search with the `Search hosts` box (⌘K hint beside it); non-matching cards
  hide.
- Card menu `Actions`: Edit, Duplicate, Remove (Remove asks to confirm).
  Local Shell has Edit only and cannot be removed.
- Card view and List view toggle; groups in the `Groups` tree.

## How to get to it (user POV)

`openKeel(app)` lands here. Otherwise the `Hosts` button in the Vault rail,
or the `Vault` tab. No account.

## Driving it with e2e

`tests/hosts.e2e.ts` pins add, search, and remove; `tests/smoke.e2e.ts`
pins the landmarks. Locators, from the trace screens:
`screen.getByRole('button', 'New host')`,
`screen.getByRole('dialog').filter({ hasText: 'New host' })` (no accessible
name), inside it `getByLabel('Label' | 'Hostname / IP' | 'Username' |
'Password')` and `getByRole('button', 'Save host')`,
`screen.getByRole('searchbox', 'Search hosts')`, a card as
`getByRole('button', /^<name>\b/)` (its name is label, `ssh`,
`user@address`), `getByRole('button', 'Actions', { visible: true })` after
searching, `getByRole('menuitem', 'Remove')`, `getByRole('alertdialog')`
then `getByRole('button', 'Remove')`. Live walk: `open_session {config:
"e2e.bugbash.config.ts"}`, `tap` New host, `type` into the Label node, `observe`.

## Proof

On screen: the dialog closes, the card shows `<label> ssh user@address`,
search leaves only it, Remove makes it go. Store: `keelApi(app.baseUrl,
'/api/hosts')` holds the host with `address`, `username`, `authType`;
`keelJson()` contains the label and never the password.

## Charters

```
hosts-crud|web|skeptic|Starting at the Vault (the app opens there), add a host, edit its label and port, duplicate it, then remove the copy; after each step check every card, count, and group against what you did
hosts-input|web|fuzzer|Starting at the Vault (the app opens there), open New host and save it with Label empty, a 300-character label, unicode, leading spaces, port 0, port 70000, and port "abc"; report what is accepted, trimmed, or lost
```

## Gotchas

- Hosts left by an earlier failed run stay in `.e2e/keel-data`; `rm -rf` it
  to reset. Tests use unique labels.
- Every card's menu button is named `Actions`, with no host in the name:
  search first or the locator is ambiguous.
- Tapping a card connects. Any host but Local Shell fails here (no SSH
  server); that is not a bug in this setup.
