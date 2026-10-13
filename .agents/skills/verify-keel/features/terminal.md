# Local terminal

A shell on this machine in its own tab: xterm.js over a WebSocket to a PTY
that keel serve owns.

## Sub-features

- Open with `Local Shell` in the Vault sidebar's Hosts tree, `More host
  actions` › `Local terminal`, or the palette's `New Local Shell`.
- Type into the terminal; output streams back.
- Pane toolbar: `Sync input`, `Split right`, `Split down`, `Focus view`,
  `Close tab`. Closing the tab kills the PTY.
- `New tab` (`+`) opens the host picker (`Recent`, `All`, `Quick connect`,
  `Join session`). The window opens on one when there is no session to
  restore, and shows one again when the last tab closes.

## How to get to it (user POV)

Vault sidebar › Hosts › `Local Shell`. The session fills the New tab (or
opens a tab named `Local Shell`) and focuses the terminal. No account.

## Driving it with e2e

`tests/terminal.e2e.ts` pins open, run, and close. Locators:
`screen.getByRole('complementary', 'Vault').getByRole('button', /^Local
Shell\b/)` (tap `Hosts` first if another segment is selected),
`screen.getByRole('textbox', 'Terminal input')` (focused on open; use
`pressSequentially` then `press('Enter')`), `getByRole('button', 'Close
tab')`, the tab as `getByRole('button', 'Local Shell', { visible: true
}).first()`. The output is
WebGL-painted: assert it with `expect.poll` over `keelApi(app.baseUrl,
'/api/sessions/<id>/scrollback')`, and look at it with
`app.screenshot()`. Live walk: tap Local Shell in the sidebar, `type` into the Terminal input
node, `press Enter`, `screenshot`.

## Proof

On screen (screenshot): the command line and its output, e.g. `echo
keel-e2e-$((6*7))` then `keel-e2e-42`. Store: `/api/sessions` grows by one
on open and loses that id on Close tab; the scrollback `text` holds the
output.

## Charters

```
terminal-tabs|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), open three Local Shell tabs, split one right and down, close panes and tabs in different orders; check the tab strip, the focused pane, and that each closed pane is gone
terminal-input|web|fuzzer|Starting at the Vault sidebar (the app opens with it beside a New tab), open Local Shell and run echo with an empty argument, 300 characters, unicode, and a pasted multi-line string; report output that is lost, garbled, or doubled
```

## Gotchas

- The terminal text is not in the accessibility tree; `observe` shows only
  `textbox "Terminal input"`. Judge output from a screenshot or scrollback.
- `SHELL=/bin/sh` in the config: the prompt is `sh-3.2$`, not the user's.
- Chromium logs WebGL `GPU stall due to ReadPixels` warnings; harmless.
- ⌘W, ⌘T and other shortcuts belong to the native window's menu in the
  desktop app; in this browser tab use the buttons or the palette.
