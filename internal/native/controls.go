package native

import (
	"strings"

	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/ui"
)

// workingBlocks is the mark of the model at work (plan 030 step 4): MyGo's loading.Blocks, sized to the line it sits in, in
// the accent or, in muted text, that colour, and named for assistive technology. It means "the model is working" and nothing
// else: a wait on keel serve or the network that does not end at the provider uses ui.Spinner. The plugin stands still and
// asks for no frames where the desktop asks for less motion, and it takes no input, so Stop stays clickable.
func workingBlocks(c *ui.Context, col ui.Color, name string) ui.Element {
	return loading.Blocks(c, loading.Options{Size: iconSize, Color: col}).Label(name).Shrink(0)
}

// The shared control layer (plan 030 step 3): Keel's buttons are MyGo's. ctlButton is ui.Button or ui.PrimaryButton with the
// two looks MyGo has no variant for: quiet (no face until hovered) and destructive (the flare colours of the Destructive
// badge). Colours come from the theme MyGo sets from the tokens (theme.go), so a control follows the system accent.

// ctlButton is a text button. name is what a test or assistive technology finds it by, shown the words on it.
func ctlButton(c *ui.Context, name, shown string, kind btnKind) ui.Element {
	var b ui.Element
	if kind == btnPrimary {
		b = ui.PrimaryButton(c, shown)
	} else {
		b = ui.Button(c, shown)
	}
	b.Label(name).Shrink(0).KeepFocus() // a press leaves the keyboard where it was, as the hand-built buttons did
	switch kind {
	case btnGhost:
		b.Border(1, ui.Transparent).Background(ui.Transparent).TextColor(inkMuted)
		if b.Hovered() {
			b.Background(hoverGround).TextColor(ink)
		}
	case btnDanger:
		b.Border(1, ui.Transparent).Background(flareSoft).TextColor(flare)
		if b.Hovered() {
			b.Background(flare).TextColor(onFlare)
		}
	}
	return b
}

// capButton is MyGo's primary button with the key that presses it drawn as a cap after the words. name is what a test or
// assistive technology finds it by; off greys it and takes no click.
func capButton(c *ui.Context, name, cap string, off bool) ui.Element {
	b := ctlButton(c, name, "", btnPrimary).Gap(space1_5)
	b.Children(func() {
		ui.Text(c, name).SingleLine()
		text(c, cap, onAccent, kbdSize, weightMedium).Font(monoStack)
	})
	if off {
		b.Disabled(true)
	}
	return b
}

// chosenRow gives the chosen row of the sidebar the source-list look of ui.Sidebar's chosen item (MyGo sidebar.go, same
// theme values): the accent with AccentText on it while the sidebar's list has the keyboard in a window drawn as active,
// the neutral SurfacePressed grey otherwise (the list unfocused, or the window not the user's). It returns the colour for
// the row's text and whether the accent is on, so a mark that means something (the live dot) can take the text colour
// instead of vanishing on the fill. AccentText on Accent is the pair deriveAccent holds to 4.5:1.
func (w *workspace) chosenRow(c *ui.Context, r ui.Element) (fg ui.Color, accentOn bool) {
	t := c.Theme()
	if c.WindowActive() && w.sideListH.FocusWithin(c) {
		r.Background(t.Accent)
		return t.AccentText, true
	}
	r.Background(t.SurfacePressed)
	return ink, false
}

// rowTile is the host's tile in a sidebar row. On the accent fill (on is true) the glyph takes the on-accent colour and the
// tile a translucent tint of it, as ui.Sidebar draws an icon on its chosen item: the host's own colour is low contrast there.
func rowTile(c *ui.Context, glyph string, tint ui.Color, on bool, fg ui.Color) {
	if on {
		tint = fg
	}
	ui.Row(c).Size(sizeTileSm, sizeTileSm).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(tint.Alpha(tileTintAlpha)).Children(func() {
		iconSized(c, glyph, tint, iconSizeSm)
	})
}

// barButton is a title bar button: MyGo's quiet button with an icon, the words and, when cap is not empty, the key that
// presses it. active keeps the face of the pressed state while its popover or panel is open.
func barButton(c *ui.Context, name, glyph, cap string, active bool) ui.Element {
	b := ctlButton(c, name, "", btnGhost)
	fg := inkMuted
	if active || b.Hovered() {
		b.Background(hoverGround).TextColor(ink)
		fg = ink
	}
	b.Gap(space1_5).Children(func() {
		icon(c, glyph, fg)
		ui.Text(c, name).SingleLine()
		if cap != "" {
			kbd(c, cap)
		}
	})
	return b
}

// ctlIconButton is a square button holding an icon, named and given a tooltip by tip.
func ctlIconButton(c *ui.Context, tip string, size float32) ui.Element {
	return ui.Button(c, "").Label(tip).Tooltip(tip).Size(size, size).Padding(0).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).KeepFocus().
		Border(1, ui.Transparent).Background(ui.Transparent)
}

// waitHelp is a wait that does not end at the model (keel serve, the network, a load): MyGo's spinner, named by the words,
// beside the words in help text. Blocks stay for the model alone (workingBlocks).
func waitHelp(c *ui.Context, msg string) {
	ui.Row(c).Gap(space1_5).AlignItems(ui.Center).Children(func() {
		ui.Spinner(c).Label(strings.TrimSuffix(msg, "…")).Shrink(0)
		help(c, msg)
	})
}

// busyMark is MyGo's spinner beside the button of an action that waits on keel serve (a save, a restore) while on is true.
func busyMark(c *ui.Context, on bool, name string) {
	if on {
		ui.Spinner(c).Label(name).Shrink(0)
	}
}

// modalChrome gives a dialog MyGo's Modal look (the dimmed backdrop, the theme's panel with its radius, border and shadow, sized
// to fit the window) on a ui.DialogBase, which is the part of ui.Modal that lets Keel set the panel's width, label and keys.
func modalChrome(c *ui.Context, backdrop, panel ui.Element) {
	t := c.Theme()
	w, h := c.Size()
	backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
	panel.Radius(t.Space(2.5)).Background(t.Background).Border(1, t.Border).Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.3)).
		MaxWidth(w - t.Space(10)).MaxHeight(h - t.Space(10))
}

// searchField is MyGo's search field for a filter: the label is its name and its placeholder.
func searchField(c *ui.Context, value *string, name string, h *ui.Handle) ui.Element {
	return ui.SearchField(c, value).Label(name).Placeholder(name).MinWidth(0).Bind(h)
}

// inputBox is a single-line input in a bordered box (k-input).
func inputBox(c *ui.Context, name string, value *string, placeholder string, h *ui.Handle, mono, masked bool, maxLen int, onKey func(ui.TextKey) bool) {
	in := ui.TextInput(c, value).Label(name).Placeholder(placeholder).FillWidth().MinWidth(0).Bind(h)
	if mono {
		in.Font(monoStack)
	}
	if masked {
		in.Password()
	}
	if maxLen > 0 {
		in.MaxLength(maxLen)
	}
	if onKey != nil {
		in.OnTextKey(onKey)
	}
}

// labeled is a k-field: the label above, the control below. Clicking the label gives h (when there is one) the
// keyboard, as a <label for> does.
func labeled(c *ui.Context, name string, h *ui.Handle, control func()) {
	ui.Column(c).FillWidth().MinWidth(0).Gap(space1).Children(func() {
		r := ui.Row(c).Cursor(ui.CursorPointer)
		r.Children(func() { label(c, name, inkMuted) })
		if h != nil && r.Clicked() {
			h.Focus(c)
		}
		control()
	})
}

func help(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(inkMuted).FontSize(captionSize)
}

// menuLabel is a k-menu-label: a group heading.
func menuLabel(c *ui.Context, s string) { text(c, s, inkFaint, captionSize, weightSemibold) }

// picker is a select: the chosen label and a chevron; the menu lists the entries (their second line is muted).
func picker(c *ui.Context, name, current string, entries []pickEntry, onPick func(string)) {
	shown := ""
	for _, e := range entries {
		if e.value == current {
			shown = e.label
		}
	}
	b := ui.Button(c, "").FillWidth().Justify(ui.Start).
		Label(name + " picker") // not the label's own name: a click by name finds the label first
	b.Children(func() {
		ui.Text(c, shown).SingleLine().Grow(1).MinWidth(0)
		icon(c, "chevron-down", inkMuted)
	})
	b.PopoverMenuButton(func(m *ui.Menu) {
		for _, e := range entries {
			it := m.Item(e.label).Radio(e.value == current)
			if e.desc != "" {
				it.Subtitle(e.desc)
			}
			if it.Chosen() {
				v := e.value
				onPick(v)
			}
		}
	})
}

// segControl is k-seg: one option is on.
func segControl(c *ui.Context, name string, current string, opts [][2]string, onPick func(string)) {
	segControlSized(c, name, current, opts, onPick, c.Theme().FontSize, space1)
}

// segControlSized is segControl with the labels' size and each segment's side padding set: the sidebar's four sections
// (Hosts, Panes with its count, Snippets, Forwards) take the caption size and a narrow padding so they show whole.
func segControlSized(c *ui.Context, name string, current string, opts [][2]string, onPick func(string), size, padX float32) {
	labels := make([]string, len(opts))
	at := 0
	for i, o := range opts {
		labels[i] = o[1]
		if o[0] == current {
			at = i
		}
	}
	idx := at
	// ui.SegmentedBase: MyGo's segmented control with a tighter segment, so four labels fit the sidebar at 13 pt.
	seg := ui.SegmentedBase(c, &idx, len(opts))
	t := c.Theme()
	seg.Track.FillWidth().AlignItems(ui.Stretch).Padding(2).Gap(2).Radius(t.Radius).Border(1, t.Border).Label(name).Children(func() {
		for i, l := range labels {
			word, count, hasCount := strings.Cut(l, " ") // "Panes 3": the count is a badge beside the word, so the label stays short
			if hasCount && strings.Trim(count, "0123456789") != "" {
				word, count, hasCount = l, "", false
			}
			// Each segment starts from its text and shares the free room, so a long label never gives way to a short one.
			need, _ := c.MeasureText(0, ui.Span{Text: word, Size: size})
			if hasCount {
				cw, _ := c.MeasureText(0, ui.Span{Text: count, Size: kbdSize})
				need += space1 + cw + 2*space1_5 // the badge's own padding
			}
			s := seg.Segment(i).Grow(1).Shrink(1).Basis(need+2*padX+2).MinWidth(0).Padding(space1, padX).Radius(max(t.Radius-2, 0)).Justify(ui.Center).Label(l).KeepFocus()
			if i == idx {
				s.Background(hoverGround).Border(1, t.Border)
			}
			col := map[bool]ui.Color{true: ink, false: inkMuted}[i == idx]
			s.Children(func() {
				ui.Row(c).Gap(space1).AlignItems(ui.Center).Children(func() {
					ui.Text(c, word).SingleLine().TextColor(col).FontSize(size)
					if hasCount {
						ui.Badge(c, count).FontSize(kbdSize)
					}
				})
			})
		}
	})
	if idx != at {
		onPick(opts[idx][0])
	}
}

// switchRow is a k-field--row: the text on the left, the switch on the right.
func switchRow(c *ui.Context, name, note string, on *bool) {
	ui.Row(c).FillWidth().Gap(space3).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			body(c, name, ink).MaxLines(0)
			help(c, note)
		})
		ui.Switch(c, on).Label(name).Shrink(0)
	})
}
