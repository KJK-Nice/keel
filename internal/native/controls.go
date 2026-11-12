package native

import "github.com/egoist/mygo/ui"

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

// ctlIconButton is a square button holding an icon, named and given a tooltip by tip.
func ctlIconButton(c *ui.Context, tip string, size float32) ui.Element {
	return ui.Button(c, "").Label(tip).Tooltip(tip).Size(size, size).Padding(0).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).KeepFocus().
		Border(1, ui.Transparent).Background(ui.Transparent)
}

// searchField is MyGo's search field for a filter: the label is its name and its placeholder.
func searchField(c *ui.Context, value *string, name string, h *ui.Handle) ui.Element {
	return ui.SearchField(c, value).Label(name).Placeholder(name).MinWidth(0).Bind(h)
}
