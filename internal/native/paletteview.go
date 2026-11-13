package native

import (
	"github.com/egoist/mygo/ui"
)

// The palette's modal, styled as the webview's k-palette (docs/design/components.css, .cmd-palette in
// keel.css): the input with esc, group headings, rows with icon, title, hint and meta, and a footer of keys.
// The input has the keyboard while it is open, so no key reaches a terminal.

// kbd is one key cap (k-kbd), flat: see keyCapFace.
func kbd(c *ui.Context, s string) ui.Element { return keyCapFace(c, s, false, nil) }

// keyCapFace is the face of every key cap, the inline k-kbd and the one shown while ⌘ is held, in one place (plan 028 step 5,
// which supersedes the raised look of plan 016). Flat: a 1 px border on all four sides, radiusXs, no shadow and no thicker
// bottom edge, the same box as before (kbdBox, border inside). Inline it is surface with a line border and ink-muted mono
// text; held it must read on any ground, translucent ones and icons too, so it is raised, fully opaque, with a line-strong
// border and ink text. extra sets what the caller adds (an opacity, a label) before the children are drawn.
func keyCapFace(c *ui.Context, s string, held bool, extra func(ui.Element)) ui.Element {
	fill, edge, fg := surface, line, inkMuted
	if held {
		fill, edge, fg = raised, lineStrong, ink
	}
	box := ui.Row(c).MinWidth(kbdBox).Height(kbdBox).Padding(0, space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(fill).Border(1, edge)
	if extra != nil {
		extra(box)
	}
	return box.Children(func() { text(c, s, fg, kbdSize, weightMedium).Font(monoStack) })
}

// keyCaps draws a shortcut as one cap per key: "⌘⇧O" is ⌘, ⇧ and O (k-keys).
func keyCaps(c *ui.Context, keys string) {
	ui.Row(c).Gap(space0_5).Shrink(0).Children(func() {
		for _, k := range splitKeyCaps(keys) { // "Ctrl+↵" is Ctrl and ↵
			kbd(c, k)
		}
	})
}

// stateDot is a k-dot: live has a soft ring, dead is a hollow ring.
func stateDot(c *ui.Context, state string) {
	b := ui.Box(c).Size(dotSize, dotSize).Radius(radiusFull).Shrink(0)
	switch state {
	case "live":
		b.Background(tide).Shadow(0, 0, 0, dotRing, tideSoft)
	case "connecting":
		b.Background(signal)
	case "dead":
		b.Border(1.5, flare)
	default:
		b.Background(inkFaint)
	}
}

// paletteView draws the open palette.
func (w *workspace) paletteView(c *ui.Context) {
	p := w.pal
	if p == nil {
		return
	}
	open := true
	rows := p.visible()
	_, winH := c.Size()
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		for _, k := range commandByID("palette").chords() { // the keys that open it close it
			c.OnWindowShortcut(k.mods, k.key, func() { w.act(w.closePalette) })
		}
		backdrop.Background(scrim).Justify(ui.Start).Padding(winH*paletteTop, 0, 0, 0)
		panel.Width(paletteWidth).MaxHeight(sizeSidebar*2).Background(raised).Border(1, line).Radius(radiusLg).Clip().
			Shadow(0, 24, 64, 0, shadowModal).Label("dialog Command palette")
		hairline(ui.Row(c).FillWidth().Height(paletteSearch).Shrink(0).Padding(0, space4).Gap(space2), 0, 0, 1, 0).Children(func() {
			icon(c, "search-01", inkFaint)
			in := ui.TextInputBase(c, &p.query).Label("Command palette").Placeholder("Run a command or connect a host…").
				PlaceholderColor(inkFaint).TextColor(ink).FontSize(titleSize).Grow(1).MinWidth(0).Bind(&w.palH)
			in.OnTextKey(func(k ui.TextKey) bool { return w.paletteKey(p, k) })
			kbd(c, "esc")
		})
		ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Label("palette list").Padding(space1, space1, space2).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				// With only the ask row left the filter found nothing. The app's list holds just that row and no message;
				// the line stays here so that "nothing matched" still reads (a recorded difference).
				if len(rows) == 0 || (len(rows) == 1 && rows[0].bare) {
					emptySide(c, emptySpec{icon: "search-01", title: "No matching commands"})
				}
				group := ""
				for i, it := range rows {
					if it.group != group && !it.bare {
						group = it.group
						ui.Row(c).FillWidth().Padding(space1_5, space2, space1).Children(func() { text(c, group, inkFaint, captionSize, weightSemibold) })
					}
					w.paletteRow(c, p, i, it)
				}
			})
		})
		hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space2, space4).Gap(space4).Background(surface), 1, 0, 0, 0).Children(func() {
			foot := func(label string, caps ...string) {
				ui.Row(c).Gap(space1).Children(func() {
					for _, k := range caps {
						kbd(c, k)
					}
					caption(c, label, inkFaint)
				})
			}
			foot("move", "↑", "↓")
			foot("open", "↵")
			foot("open in split", "⌥↵")
			ui.Spacer(c)
			foot("ask agent", splitKeyCaps(askKeys())...) // k-palette-foot-end
		})
	})
	if !p.focus {
		w.palH.Focus(c)
		p.focus = true
	}
	if !open {
		w.act(w.closePalette)
	}
}

func (w *workspace) paletteRow(c *ui.Context, p *palette, i int, it palItem) {
	sel := i == p.cursor
	r := ui.Row(c).FillWidth().Height(sizeControlLg).Shrink(0).Padding(0, space3).Gap(space2).Radius(radiusSm).
		Label("palette row " + it.title).Cursor(ui.CursorPointer)
	if sel || r.Hovered() {
		r.Background(hoverGround)
	}
	if sel && p.reveal {
		r.ScrollIntoView()
		p.reveal = false
	}
	r.Children(func() {
		ic := it.icon
		if ic == "" {
			ic = "action"
		}
		col := inkMuted
		if sel {
			col = accentInk
		}
		icon(c, ic, col)
		ui.RichText(c).FontSize(uiFontSize).TextColor(ink).MaxLines(1).Shrink(0).MaxWidthPercent(70).Children(func() {
			spans := highlight(it.title, p.query)
			if it.bare { // the ask row is not highlighted (lib/palette.js paletteHTML)
				spans = []span{{text: it.title}}
			}
			for _, s := range spans {
				t := ui.Text(c, s.text)
				if s.mark {
					t.TextColor(accentInk).FontWeight(weightSemibold)
				}
			}
		})
		if it.hint != "" {
			mono(c, it.hint, inkFaint).Grow(1).Shrink(1).MinWidth(0)
		} else {
			ui.Spacer(c)
		}
		if it.state != "" {
			stateDot(c, it.state)
		}
		if it.badge != "" { // k-badge--brass
			accentBadge(c, it.badge)
		}
		if it.keys != "" {
			keyCaps(c, it.keys)
		}
	})
	if r.Clicked() {
		split := r.ClickModifiers()&ui.Alt != 0
		w.act(func() { p.cursor = i; w.choose(split) })
	}
}

// paletteKey is the input's keys: ↑ ↓ move, ↵ runs, ⌥↵ runs in a split, ⌘↵ asks the agent (the last row), esc closes.
// Anything else is typing.
func (w *workspace) paletteKey(p *palette, k ui.TextKey) bool {
	p.query = k.Text() // what is typed so far, ahead of the bound value
	switch {
	case k.Key == ui.KeyEscape && k.Mods == 0:
		w.act(w.closePalette)
	case k.Key == ui.KeyEnter && k.Mods == 0:
		w.act(func() { w.choose(false) })
	case k.Key == ui.KeyEnter && k.Mods == ui.Alt:
		w.act(func() { w.choose(true) })
	case k.Key == ui.KeyEnter && k.Mods == ui.Cmd: // ⌘↵ asks the agent with the query, whatever row is selected
		w.act(func() { p.cursor = len(p.visible()) - 1; w.choose(false) })
	case k.Key == ui.KeyEnter:
		// Another modifier: no plain ↵ may come of it.
	case (k.Key == ui.KeyDown || k.Key == ui.KeyUp) && k.Mods == 0:
		n := len(p.visible())
		d := 1
		if k.Key == ui.KeyUp {
			d = -1
		}
		p.cursor, p.reveal = moveCursor(p.cursor, d, n), n > 0
	default:
		return false
	}
	return true
}
