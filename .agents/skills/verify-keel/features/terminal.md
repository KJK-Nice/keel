# Local terminal

A shell on this machine in its own tab: xterm.js over a WebSocket to a PTY
that keel serve owns.

## Sub-features

- Open with `Terminal` on the Hosts page, the Local Shell card, or the
  palette's `New Local Shell`.
- Type into the terminal; output streams back.
- Pane toolbar: `Sync input`, `Split right`, `Split down`, `Focus view`,
  `Close tab`. Closing the tab kills the PTY.
- `New tab` (`+`) opens the host picker (`Recent`, `All`, `Quick connect`,
  `Join session`).

## How to get to it (user POV)

Vault › Hosts › `Terminal`. A tab named `Local Shell` opens and focuses the
terminal. No account.

## Driving it with e2e

`tests/terminal.e2e.ts` pins open, run, and close. Locators:
`screen.getByRole('button', 'Terminal')`,
`screen.getByRole('textbox', 'Terminal input')` (focused on open; use
`pressSequentially` then `press('Enter')`), `getByRole('button', 'Close
tab')`, the tab as `getByRole('button', /^Local Shell\b/)`. The output is
WebGL-painted: assert it with `expect.poll` over `keelApi(app.baseUrl,
'/api/sessions/<id>/scrollback')`, and look at it with
`app.screenshot()`. Live walk: tap Terminal, `type` into the Terminal input
node, `press Enter`, `screenshot`.

## Proof

On screen (screenshot): the command line and its output, e.g. `echo
keel-e2e-$((6*7))` then `keel-e2e-42`. Store: `/api/sessions` grows by one
on open and loses that id on Close tab; the scrollback `text` holds the
output.

## Charters

```
terminal-tabs|web|skeptic|Starting at the Vault (the app opens there), open three Local Shell tabs, split one right and down, close panes and tabs in different orders; check the tab strip, the focused pane, and that each closed pane is gone
terminal-input|web|fuzzer|Starting at the Vault (the app opens there), open Terminal and run echo with an empty argument, 300 characters, unicode, and a pasted multi-line string; report output that is lost, garbled, or doubled
```

## Gotchas

- The terminal text is not in the accessibility tree; `observe` shows only
  `textbox "Terminal input"`. Judge output from a screenshot or scrollback.
- `SHELL=/bin/sh` in the config: the prompt is `sh-3.2$`, not the user's.
- Chromium logs WebGL `GPU stall due to ReadPixels` warnings; harmless.
- ⌘W, ⌘T and other shortcuts belong to the native window's menu in the
  desktop app; in this browser tab use the buttons or the palette.
