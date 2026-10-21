Keel is a local SSH workspace: a native window of tabs and split panes over PTYs that keep running in `keel serve`, an encrypted vault of hosts and keys, and sessions you can share over WebRTC. The interface is a tool you sit inside for hours. It should feel like a well-kept instrument panel: dark, quiet and exact, with one warm light kept for what you can act on.

## Principles

1. **The terminal is the work; chrome steps back.** Chrome, rails and pane bars use the darkest neutrals and muted text. Nothing in chrome is brighter than the text in the terminal.
2. **One warm light.** Brass is the only accent. At rest it marks only the terminal cursor. Anything else brass is something you can press right now (primary button, a toggle that is on) or a role that is yours (Driving). Where you are is shown by shape and brightness, not by color: see Depth, borders, motion.
3. **State is always spoken.** Live, reconnecting, exited, driving, following: each has a color *and* a word or icon. Never color alone.
4. **Show the bytes.** Anything that writes into a session from outside the keyboard shows the exact bytes and target first (ApproveType).
5. **Only code is mono.** `user@host:port`, IPs, paths, flags, commands, terminal output and key caps are set in mono. Everything a person reads as UI (tab and host names, labels, counts, status text) is the system font.

## Content fundamentals

- **Voice:** plain, technical, second person only when instructing ("Add a host to open SSH sessions"). No exclamation marks, no emoji, no apologies.
- **Casing:** sentence case for everything, section labels included (Hosts, Recent). No uppercase-tracked labels.
- **Verbs name the result:** "Connect", "Type 58 bytes", "Stop 4 sessions", "Delete host". Never "OK", "Yes", "Submit".
- **Numbers, not hedges:** "4 sessions are still running", not "some sessions".
- **Errors** say what happened, then the fix: "bastion rejected key deploy-ed25519." with an "Edit host" action.
- **Product words:** Vault (the left sidebar), host, session, pane, split, sync, snippet, port forward, runtime (`keel serve`). Herd roles: owner, driver, follower; in UI: "Driving", "Following", "Pass control".
- **Shortcuts** use macOS glyphs on macOS (⌘ ⇧ ⌥ ↵) and words elsewhere (Ctrl, Shift, Enter).

## Color

Two themes from one token set; **Dark is primary** and is what every component is designed in first. All neutrals carry a slight blue-slate bias (hue 255), so the UI reads cool and the brass reads warm.

Grounds, darkest to lightest in Dark:

- `stage`: terminal background and recessed fields (inputs, code). Equal to `term-bg`.
- `chrome`: tab strip, vault rail, unfocused pane bars, and the ground tiles sit on in a split tab.
- `surface`: vault lists, settings, the focused pane bar, pane bars inside split tiles.
- `raised`: menus, dialogs, palette, secondary buttons, the selected tab.
- `hover`: hover and highlighted rows on any ground.

Text: `ink` (primary), `ink-muted` (secondary, addresses), `ink-faint` (placeholders, counts, shortcut hints; not on chrome or hover in Light). Lines: `line` for hairlines, `line-strong` for control borders (3:1).

Accent and status:

- `brass` fill + `on-brass` text; `brass-ink` for brass as text, icon or 2px mark on neutrals; `brass-soft` behind brass-ink; `brass-strong` for hover.
- `tide` live/success, `signal` attention, `flare` failure/destructive, `haze` info/follower. Each has a `-soft` ground. Tide and flare are close in lightness, so status always carries a word or icon.
- `focus` (= `brass-ink`) is the focus ring: 2px solid, 1px offset, on every interactive element. It holds 3:1 or more on every ground in both themes.

Terminal: xterm's theme maps to `term-bg`, `term-fg`, `term-cursor`, `term-selection` and `ansi-0` … `ansi-15`. Host colors are chosen from `ansi-9` … `ansi-14` and appear only on the host's tile (16% tint, icon at full strength).

Every text token holds 4.5:1 on the grounds its usage note names, in both themes.

## Type

- `sans` is the **system UI font** (SF Pro Text on macOS, Segoe UI Variable on Windows, the desktop's sans on Linux) so Keel reads like a native app. `display` is the same system family at display optical size (SF Pro Display) for dialog titles, page titles and empty states. `mono` (JetBrains Mono) for terminals, addresses, paths, flags and commands only.
- Default UI size is 13px (`body` = 1rem). The user scales the UI from 11–16px, so in the app every size here is written in rem (px ÷ 13). Terminal text scales separately (10–22px, ⌘0 resets to 12).
- Styles: `display` 28/32, `heading` 18/22, `title` 15/20, `body` 13/18, `body-strong`, `label` 12/16, `caption` 12/16, `reading` 14/22, `term` 12/16, `code` 12/18, `address` 12/16, `meta` 12/16 semibold sans in sentence case, `kbd` 11/14.
- `reading` is for long-form prose only (the notebook's reading column, `size-reading` 68ch wide). The interface stays on `body`.
- `tokens.css` carries each style as a font shorthand, `--type-<name>` (weight, size/line height in rem, family). Use `font: var(--type-reading)`; add letter spacing and case in the rule.
- The UI font needs nothing shipped: it is the OS font. Ship only the terminal fonts as local woff2 (JetBrains Mono, and IBM Plex Mono because Settings offers it as a terminal font). Keel is a desktop app and must look right offline; do not load fonts from the network at runtime.

## Space, size and shape

- Spacing steps: `space-0.5` 2 · `space-1` 4 · `space-1.5` 6 · `space-2` 8 · `space-3` 12 · `space-4` 16 · `space-6` 24 · `space-8` 32. Lay out with `gap`, not margins.
- Fixed chrome: `size-titlebar` 38, `size-tab` 28, `size-panebar` 30, `size-control` 28 (`-sm` 24, `-lg` 32), `size-row` 44, `size-rail` 208 (settings sections), `size-sidebar` 252 (Vault sidebar), `size-list` 360 (list column of a list and detail page), `size-agent` 380 (agent panel), `size-agentpage-list` 280 / `size-agentpage-context` 300 / `size-agentpage-thread` 820 (Agent tab page columns and thread measure), `size-reading` 68ch (notebook reading column).
- Radii are small and tight: `radius-xs` 3 (kbd, badge, menu item), `radius-sm` 5 (buttons, inputs, tabs), `radius-md` 8 (menus, toasts), `radius-lg` 12 (dialogs, palette), `radius-full` for dots, chips and the switch.

## Depth, borders, motion

- Separation comes from ground steps and hairlines first. Shadows only on things that float: `shadow-pop` (menus, tooltips, toasts) and `shadow-modal` (dialogs, palette, over `scrim`).
- **Selected tab.** A `raised` pill with `ink` text: no border, no underline.
- **Split tabs.** Only when a tab holds two or more panes, the panes become `radius-md` tiles on the `chrome` ground with a `space-1.5` (6px) gap and no outlines (`.k-tiles`, or any container with two `.k-pane` children). The focused tile carries a soft ring (3px, `focus` at 30%, `shadow-tile-focus`) and the other terminals dim to 70%. A single-pane tab stays full-bleed with no ring.
- Never use colored rules, side bars or left-border accents to mark the selected tab or the focused pane.
- Motion is functional: `duration-fast` 90ms for hover and focus, `duration-base` 160ms for menus, tooltips and the switch, `duration-slow` 240ms for dialogs and toasts. Ease-out in, ease-in out. The reconnecting dot pulses; under `prefers-reduced-motion` it is static and nothing slides.

## Iconography

- Hugeicons core-free 4.3.0, stroke style, 1.5 stroke width, `currentColor`, the same set Keel ships in `components/icon`. Sizes 14 (pane bar, small buttons), 15–16 (menus, rail, rows), 22 (empty states).
- Icons default to `ink-muted`; they take `brass-ink` only when their item is active or highlighted.
- Product-specific choices: steering wheel = driving, eye = following, crown = owner, panel-left = Vault sidebar toggle, two-column / two-row layout = split right / down, exchange = sync input.
- No emoji anywhere in the UI.

## Logo

`assets/Logos/keel-mark.svg` is the in-app mark (splash, favicon): a dark hexagonal hull with a slit, on a white rounded square. `keel-app-icon.png` is the macOS Dock icon. Use the mark as supplied; do not recolor it brass or redraw it.

## Choosing a value

- **A list, or a list that changes** (identity, folder, jump host, forward host, role, runbook, terminal font, model, retention): a **combobox**, `.k-combobox`. A `.k-input` with `role="combobox"` shows the chosen label; typing filters the options (case-insensitive substring of the label and the second line). The popup is a `.k-menu` (`raised`, `radius-md`, `shadow-pop`) of `.k-menu-item` rows: optional leading icon, label, optional `ink-muted` second line, a check on the chosen row. ↑ ↓ move, Home / End jump, ↵ picks, Esc closes the list (not the dialog), a click outside closes it, Tab leaves it. Disabled rows are skipped. Nothing matching says "No matches"; no options at all says what to do ("Add an SSH host first").
- **Two to four fixed choices** (theme, smoothing, translucency, right-click, authentication, identity kind, forward kind): a **segmented control**, `.k-seg`, every choice visible. Do not put a fixed set this small in a combobox.
- A "None" choice is the first option, with an empty value. Label it with what it means ("No group", "No role", "None").
- The text is the system font. Only an option that is code is mono: an address line (`user@host:port`) uses `.k-combobox-desc--mono`, and a picker whose values are code (roles) is `.k-combobox--mono`.
- Never a native `<select>`, and never a menu used as a value picker: menus are for actions.
- The value posts with its form through a hidden input; the text input has no name. The same markup comes from templ (`Combobox`, `internal/ui/combobox.templ`) and from term.js strings (`comboboxHTML`, `js/lib/combobox.js`), which wires every picker by delegation, so pickers a page repaints or a stream morphs in keep working. The popup is a top-layer popover, so it shows over a modal dialog and is not clipped by its scroll.

## Building with it

- Components are plain CSS classes (`k-` prefix) in `components/bundle.css`, so they work in templ (shadcn-templ components restyled through tokens) and in HTML strings rendered by `term.js`. `components/bundle.js` only provides the preview icon helper.
- Map shadcn's variables onto these tokens once (`--background` → `stage`, `--card` → `surface`, `--popover` → `raised`, `--primary` → `brass`, `--primary-foreground` → `on-brass`, `--muted-foreground` → `ink-muted`, `--border` → `line`, `--input` → `line-strong`, `--ring` → `focus`, `--destructive` → `flare`). Keep no second token layer.
- Theme switching sets `data-theme="dark|light"` on `<html>`; "system" resolves it from `prefers-color-scheme` in script so CSS only ever sees the two themes.
