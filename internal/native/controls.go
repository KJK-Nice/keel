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
