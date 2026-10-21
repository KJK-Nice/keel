# Share and Join

Sharing one session over WebRTC and following someone else's
(docs/ui-redesign.md §5.6, §5.7). The behaviour (admission, roles, hand-over,
what a joiner receives) lives in `internal/share`, `internal/runtime` and
`lib/joinpeer.js`; the UI is `lib/share.js` (markup), `ui/joinprompt.js`,
`ui/actiontoast.js`, the Share popover in `ui/sharepop.js` and the rest of the share code in `term.js`.

## Sub-features

- **Share popover**: titlebar `Share` (or the `N watching` badge) on a
  session tab. Opening it shares the focused pane's session (`POST
  /api/share`, the same call the old button made; it returns the live link
  when there is one) and copies the link. Shows `Live link`, the `Join
  link` field with `Copy` (`Copied` once copied), the scope ("this pane's
  terminal only"), expiry ("until you stop sharing or close this pane, 24
  hours at most"; a join request is turned away after 2 minutes), the
  people (`In this session · N`: you first, the driver, then by arrival,
  each with `Driving` / `Owner` / `Following` and how they came: this
  window, Keel window, joined by link, with the time), `Pass control to
  <name>` (or per row when several), `Take control` / `Ask for control`
  when you do not drive, and `Stop sharing`. Escape or a click outside
  closes it. Without a signal server the popover shows the server's error.
- **Admission prompt**: `<name> wants to watch <session>.` with `Allow` /
  `Deny`, top right (region `Share requests`). Same answers as before
  (`POST /api/share/{id}/admit`).
- **Control request**: `<name> asks for control` with `Hand over` (sends
  `grant-control` to that person) / `Keep driving` (dismisses), in the same
  region, for whoever drives or owns the session.
- **Joined tab**: link icon and `<host> · joined`; a haze banner (role
  `status`) `Following <owner>'s session on <host> · read-only` with `Ask
  for control` and `Leave` (closes the tab); `Driving … · you have control`
  after a hand-over. The pane bar shows `<driver> is driving` and `You
  follow`. No Vault sidebar, no sidebar toggle, no Share button. Typing
  shows `Typing is off while you follow` (region `Following`) with `Ask for
  control`; nothing reaches the PTY.
- **Local follower** (another window on the session, or the owner after
  passing control): a haze pane banner `Following · <driver> is driving ·
  read-only` with `Take control` (owner) or `Ask for control`.
- Not here (no backend for it): Watch only / Watch and ask mode, a chosen
  expiry, latency per person, an agent to hide.

## How to get to it (user POV)

Open Local Shell, click `Share`. To be the joiner, open a New tab, `Join
session`, paste the link; the same window can join its own share.

## Driving it with e2e

`tests/share.e2e.ts`. The e2e config runs `scripts/e2e-serve.sh`, which
starts `bin/keel-signal` on a free loopback port and gives keel serve
`KEEL_SIGNAL`; `make build` builds both binaries. The popover is
`screen.getByRole('dialog', 'Share session')` with `getByRole('textbox',
'Join link')`, buttons `Copy`/`Copied`, `Stop sharing`, `Pass control to …`.
The prompts are `getByRole('region', 'Share requests').getByRole(
'alertdialog')`. The joined banner is `getByRole('status').filter({
hasText: "Following e2e's session on" })`. Both sides are named `e2e`.
Headless browsers refuse clipboard writes: the test stubs
`navigator.clipboard.writeText` in an init script and reads what was
copied. xterm's helper textarea does not count as a field for
`pressSequentially` while following: type with `browser.keyboard.type`.
`Meta+1` / `Meta+9` switch between the owner's tab and the joined tab.

## Proof

On screen: the popover text, the prompt, the banner and the toasts. Side
effects: the owner session's scrollback (`/api/sessions/<id>/scrollback`)
lacks what the follower typed and has what the joiner typed after `Hand
over`; after `Stop sharing` the next Share gets a different link (a new
room); closing the owner's tab removes the session.

Not proven here: two machines (the joiner is the same page over loopback
WebRTC, so NAT, STUN and a remote signal server are untested), the browser
join page (`internal/signal/web`, unchanged), the native WKWebView.

## Charters

```
share-popover|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell and click Share; check every claim in the popover (link, who is in the session, roles, expiry) against what you can observe, copy the link, stop sharing and share again, and report any count, role or link that does not match
share-follow|web|default|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell, Share it, then open a New tab, Join session with the link, Allow yourself, and as the follower try to type, ask for control, get handed control, type, and leave; report anything typed while following that reaches the shell, and any banner or role that is wrong
```
