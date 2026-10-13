# Hosts

The Vault sidebar's Hosts segment (left of every tab): the saved SSH hosts
as a tree by group, with a filter and a menu per host. Secrets typed here go
to the encrypted vault.

## Sub-features

- Add with `New host` (the `+` beside the filter, or ⌘N outside the
  Keychain, Port forwarding, Snippets and Notebooks pages): Label, Parent
  Group, Tags, Hostname / IP, Port, Username, Authentication (Password,
  Private key, Keychain identity, Local shell), Color, Keepalive, Auto
  reconnect; `Save host`.
- Filter with the `Search hosts` box: non-matching hosts and empty groups
  hide; it matches label, address, user, tags and group names. Enter
  connects the first match, or opens Quick connect for `user@host`.
- Click a host to connect. With a New tab selected the session fills it,
  otherwise it opens a new tab. A running host shows its session count.
- Row menu `Actions`: Edit, Duplicate, Remove (Remove asks to confirm).
  Local Shell has Edit only and cannot be removed.
- Groups: collapse with the group row; `Folder actions` (Edit, New subgroup,
  Rename, Move into…, Remove); drag a host or a group onto a group.
- `More host actions`: Quick connect, Join session, New group, Local
  terminal.
- `Toggle vault sidebar` in the titlebar (⌘B) folds the whole sidebar.

## How to get to it (user POV)

`openKeel(app)` lands with the sidebar open on Hosts, beside a New tab. If
another segment is selected (the choice is remembered per browser), tap
`Hosts`. No account.

## Driving it with e2e

`tests/hosts.e2e.ts` pins add, filter, and remove; `tests/smoke.e2e.ts`
pins the landmarks; `tests/sidebar.e2e.ts` pins ⌘B, ⌘1/⌘9, the tab pages
and restore on reload. Scope every locator to the sidebar,
`const vault = screen.getByRole('complementary', 'Vault')`, because the New
tab page and the tab strip repeat names (`Local Shell`, `Keychain`).
Then: `vault.getByRole('button', 'New host')`,
`screen.getByRole('dialog').filter({ hasText: 'New host' })` (no accessible
name), inside it `getByLabel('Label' | 'Hostname / IP' | 'Username' |
'Password')` and `getByRole('button', 'Save host')`,
`vault.getByRole('searchbox', 'Search hosts')`, a host as
`vault.getByRole('button', /^<label>\b/)` (its name is the label, then the
session count when one runs), `vault.getByRole('button', 'Actions', {
visible: true })` after filtering, `getByRole('menuitem', 'Remove')`,
`getByRole('alertdialog')` then `getByRole('button', 'Remove')`. Live walk:
`open_session {config: "e2e.bugbash.config.ts"}`, `tap` New host, `type`
into the Label node, `observe`.

## Proof

On screen: the dialog closes, the host shows in the tree, filtering leaves
only it, Remove makes it go. Store: `keelApi(app.baseUrl, '/api/hosts')`
holds the host with `address`, `username`, `authType`; `keelJson()`
contains the label and never the password.

## Charters

```
hosts-crud|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), add a host, edit its label and port, duplicate it, then remove the copy; after each step check every row, count, and group against what you did
hosts-input|web|fuzzer|Starting at the Vault sidebar (the app opens with it beside a New tab), open New host and save it with Label empty, a 300-character label, unicode, leading spaces, port 0, port 70000, and port "abc"; report what is accepted, trimmed, or lost
```

## Gotchas

- Hosts left by an earlier failed run stay in `.e2e/keel-data`; `rm -rf` it
  to reset. Tests use unique labels.
- Every row's menu button is named `Actions`, with no host in the name:
  filter first or the locator is ambiguous. The button shows on hover but is
  in the tree (and visible) all the time.
- The fresh vault puts Local Shell in a `This machine` group.
- The sidebar segment and open/closed state live in `localStorage`
  (`keel-ui`): a test that changes them changes them for the next test in
  the same browser.
- Tapping a host connects. Any host but Local Shell fails here (no SSH
  server); that is not a bug in this setup.
