# New tab page

What the window shows on ⌘T, the `+` button, at startup with no session to
restore, and when the last tab closes (docs/ui-redesign.md §5.3).

## Sub-features

- One input, `Host to connect to`, focused on open. Typing filters the saved
  hosts shown as cards (name, `user@address`, tags). Enter opens the
  highlighted card (↑↓ move).
- `user@host:port`, an `ssh://user@host:port` URL, or a pasted `ssh …`
  command (a leading `$ ` is fine) is parsed by `lib/sshparse.js` and shown
  in a `Pasted command` panel: `Read as deploy@10.0.4.21:2222. Nothing has
  run.`, a `Left out (N)` list, and `Save as host` (↵, a New host dialog
  prefilled with host, port, user, label = host) or `Connect once` (Quick
  connect prefilled; needs a user). Nothing is run, saved or connected until
  the dialog is confirmed.
- Left out, each with its reason: `-o ProxyCommand/LocalCommand/
  PermitLocalCommand/RemoteCommand` (runs a command), `-o
  StrictHostKeyChecking/UserKnownHostsFile` (changes host key trust),
  `-L/-R/-D/-W` (forwards), `-J` and `-o ProxyJump` (jump hosts are not
  supported yet), `-A/-X`, `-F`, and a trailing remote command. `-i key` is a
  note ("was not read"); the file is never opened. The same list is repeated
  in the dialog.
- Input with `;`, `|`, `&`, `<`, `>`, backticks, `$(`, a newline, bad host
  characters or a port outside 1 to 65535 shows an error and never a host.
- `Recent` (up to six cards, the local shell always one; `⌥1`–`⌥6` open them;
  `N live` when sessions are open on the host), `Start` (Quick connect ⌘⇧O,
  Join session ⌘J, New host ⌘N), `Recently closed` (up to three rows with the
  age, ⌘⇧T on the newest; click a row or ⌘⇧T to reopen it as a new session).
- Not here yet: "Ask agent" (⌘I), phase 6.

## How to get to it (user POV)

Open Keel (a fresh window), press ⌘T, or close the last tab. No account.

## Driving it with e2e

`tests/newtab.e2e.ts`. The page is `screen.getByRole('region', 'New tab')`;
inside it `getByRole('heading', 'Where to?')`, `getByRole('textbox', 'Host
to connect to')`, the panel `getByRole('region', 'Pasted command')`,
`getByRole('region', 'Recently closed')`, cards and actions as `getByRole(
'button', /Local Shell/)`, `/Quick connect/`. `input.press('Alt+1')` opens
card 1; `press('Meta+Shift+t')` reopens. The New host dialog after Enter:
`getByRole('dialog').filter({ hasText: 'New host' })` with `getByLabel(
'Hostname / IP' | 'Port' | 'Username' | 'Label')`. The `Left out` heading is
uppercase on screen: match case-insensitively. Quick connect's Password is a
secure field; the runner will not read its value.

## Proof

On screen: the panel text and the dialog fields after pasting. Side effects:
`/api/hosts` and `/api/sessions` have the same length before and after
(nothing saved or started), and `/api/sessions` grows by one after ⌥1 or
⌘⇧T with a different session id.

## Charters

```
newtab-paste|web|fuzzer|Starting at the Vault sidebar (the app opens with it beside a New tab), paste ssh commands into the New tab input: with -p, -l, -i, -J, -o options, ssh:// URLs, IPv6 addresses, quotes, shell operators and substitutions; report any input that yields a host with unsafe characters, any option that is silently lost without being listed, and any that runs something
newtab-recent|web|default|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell, close it, and use the New tab page's Recent card, Alt+1 and Recently closed row to reopen it; check each opens one new session and that closed rows disappear once reopened
```
