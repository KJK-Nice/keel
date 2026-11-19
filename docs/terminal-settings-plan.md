# Terminal settings: plan and progress

Goal: Settings → Terminal as complete as Rex's (Typography, Colors, Cursor, Keyboard, Scrolling, Selection), within
Slime's rules (tokens only, fonts never from the network, nothing writes to a PTY).

Slime's terminal is libghostty-vt (the emulator: cells, colours, cursor) drawn by MyGo's terminal plugin
(`plugins/terminal`, `paint.go`, Go on MyGo's GPU UI). Rex uses the same emulator with its own Swift and Metal
renderer, so what Rex draws differently has to come from MyGo's plugin, or be drawn by Slime over the terminal
(`ui/shader` and the painter, which apps may use).

Status: `[ ]` to do, `[~]` in progress, `[x]` done (with the PR).

## Today (v0.2.4)

Settings → Terminal has Font (five bundled or common faces), Text size, Copy on select and Right-click.

## 1. Typography — Slime only (#179)

- [x] Terminal font: any installed monospaced family (`ui.FontFamilies()`, `Monospaced`), after Slime's five, in
      the picker menu. A missing family falls back to the default stack. (A search box in the menu: later, if the
      list proves long.)
- [x] Weight (`Font.Weight`, 100–900) and Line height (`Font.LineHeight`, 100–200%).
- [x] A preview of three lines: plain, an ANSI colour and bold, in the chosen font, weight and line height.
- [x] Kept in `keel-native.json` (`termFont`, `termWeight`, `termLineHeight`), applied to every open pane (`SetFont`).

## 2. Advanced font settings — Slime only (#180)

- [x] An "Advanced font" group (not a sub page: four rows): Ligatures (off is `-calt -liga -dlig`), Font features
      (`ss01, cv02, -calt`, cleaned, at most 16), Cell width (`Font.CellWidth`, 80–150%), Thicken (`Font.Thicken`,
      macOS). Text size and Weight became number fields.

## 3. Colors and Selection — Slime only (#181)

- [x] Improve text contrast: Off, Some (3:1), High (4.5:1) as `MinimumContrast` (live, `SetMinimumContrast`).
- [x] Selection background and text: Theme, or one of the six hues of the ANSI palette (`Theme.Selection`,
      `SelectionText`; live through `SetTheme`). No free colour: colours come from tokens.

## 4. Cursor and Keyboard — Slime only, new panes (#182)

- [x] Cursor style (Block, Bar, Underline) with a preview, and Blink on or off (`Options.Cursor`, `NoBlink`).
- [x] Option key (macOS): Characters, or Alt (Meta) (`Options.OptionAsAlt`).
- [x] MyGo sets these only when a terminal is made: the rows say "applies to panes opened from now on" until the
      setters of step 7 land.

## 5. Copy animation — Slime only (#183)

- [x] Play animation on copy: `OnCopy` flashes the copied cells over the terminal, `duration-slow`, none with
      reduced motion. The plugin gives only the text: it is found in the screen's snapshot; a copy not on the screen
      once (scrollback, a full-screen program, a repeated word) flashes the whole terminal faintly.

## 6. Cursor trails and shaders — Slime, drawn over the terminal (skipped for now)

Blocked on MyGo: the plugin has no public cursor position (v0.9 and v0.10; `internal/vt` has `Cursor()`, not
exported). `Snapshot()` holds it but costs about 7 ms and 600 KB with 10k rows of scrollback, too much for every
keystroke, and keel serve sends the PTY's raw output, so Slime cannot read the cursor without its own emulator. Needs
`Terminal.Cursor()` upstream (step 7) first.

- [ ] Cursor trails: the cursor's cell from `term.Cursor()` and the grid geometry (`suggestgeom.go`), a trail
      drawn with `ui/shader` over the pane.
- [ ] Cursor shaders: a small set of Slime's own shaders (no user code).

## 7. MyGo terminal plugin — upstream PRs to egoist/mygo

The plugin imports MyGo's `internal/*` packages, so it cannot be forked on its own; these go upstream.

- [ ] `Terminal.Cursor()`: the cursor's cell and whether it shows (for step 6).
- [ ] Setters: cursor style, blink, Option as Alt on a live terminal.
- [ ] Force cursor style (ignore programs' DECSCUSR) and blink speed.
- [ ] Rounded selection corners.
- [ ] Force theme palette for text (map program colours to the nearest theme colour).
- [ ] Display P3 for theme and program colours (`ui.Color` already carries wide colours).
- [ ] Smooth (pixel) scrolling of the scrollback.

## Not planned

- Smooth terminal resizing: the PTY's size is `keel serve`'s, so the window cannot reflow ahead of it without
  a change to how sessions are sized.
- Option key "Automatic (by keyboard layout)": MyGo does not report the layout.
- Key sequence hints: Slime has no multi-key shortcuts.

## Checks for each step

`make test`, `make lint`, `make native-e2e` (with `SLIME_KEYRING=off`), and `make native-automation` when a step
touches key handling (step 4). Every new preference has a test that it is saved and applied to an open pane.
