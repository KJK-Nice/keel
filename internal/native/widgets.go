package native

import "github.com/egoist/mygo/ui"

// Small building blocks shared by the views. Colours and sizes come from tokens.go only.

// windowTheme maps the Keel tokens onto the MyGo theme so stock widgets inherit them.
func windowTheme(c *ui.Context, dark bool) {
	t := *c.Theme()
	if t.Dark != dark { // the choice differs from the system's appearance: the stock widgets take the chosen base
		if dark {
			t = *ui.DarkTheme()
		} else {
			t = *ui.LightTheme()
		}
	}
	t.Background, t.Surface, t.SurfaceHover, t.Border = chrome, surface, hoverGround, line
	t.Text, t.TextMuted, t.Accent, t.FontSize = ink, inkMuted, brass, uiFontSize
	c.SetTheme(&t)
}

const monoStack = monoFamily + "," + monoFallback

func text(c *ui.Context, s string, col ui.Color, size float32, weight int) ui.Element {
	return ui.Text(c, s).TextColor(col).FontSize(size).FontWeight(weight).MaxLines(1)
}

func body(c *ui.Context, s string, col ui.Color) ui.Element {
	return text(c, s, col, uiFontSize, weightRegular)
}
func bodyStrong(c *ui.Context, s string, col ui.Color) ui.Element {
	return text(c, s, col, uiFontSize, weightMedium)
}
func label(c *ui.Context, s string, col ui.Color) ui.Element {
	return text(c, s, col, captionSize, weightMedium)
}
func caption(c *ui.Context, s string, col ui.Color) ui.Element {
	return text(c, s, col, captionSize, weightRegular)
}
func mono(c *ui.Context, s string, col ui.Color) ui.Element {
	return caption(c, s, col).Font(monoStack)
}

func dot(c *ui.Context, col ui.Color) ui.Element {
	return ui.Box(c).Size(space2, space2).Radius(radiusFull).Background(col).Shrink(0)
}

// iconBtn is a square icon button named for the tester and assistive tech; it reports a click.
func iconBtn(c *ui.Context, name, tip string, size float32, pressed bool) bool {
	return iconBtnOff(c, name, tip, size, pressed, false)
}

// iconBtnOff is iconBtn that can be disabled: .pane-bar .icon-btn:disabled is 32% opaque and takes no pointer.
func iconBtnOff(c *ui.Context, name, tip string, size float32, pressed, off bool) bool {
	return iconBtnCap(c, name, tip, size, pressed, off, nil)
}

// iconBtnCap is iconBtnOff with a key cap: cap, when set, is drawn over the corner of an enabled button (keycaps.go).
func iconBtnCap(c *ui.Context, name, tip string, size float32, pressed, off bool, cap func()) bool {
	b := ui.Row(c).Size(size, size).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label(tip).Cursor(ui.CursorPointer)
	if off {
		b.Opacity(disabledAlpha).Disabled(true)
		b.Children(func() { icon(c, name, inkMuted) })
		return false
	}
	col := inkMuted
	switch {
	case pressed:
		b.Background(brassSoft)
		col = brassInk
	case b.Hovered():
		b.Background(hoverGround)
	}
	b.Children(func() {
		icon(c, name, col)
		if cap != nil {
			cap()
		}
	})
	return b.Clicked()
}

func hairline(e ui.Element, top, right, bottom, left float32) ui.Element {
	return e.BorderWidth(top, right, bottom, left).BorderColor(line)
}

// statusColor is the state dot of a session: tide live, signal attention, flare failure.
func statusColor(status string) ui.Color {
	switch status {
	case "ready":
		return tide
	case "error", "exited":
		return flare
	}
	return signal // connecting, or not heard yet
}

// glyphBtn is iconBtn with a character for its icon (the find bar's ↑ ↓ ×, as the webview draws them).
func glyphBtn(c *ui.Context, glyph, tip string) bool {
	b := ui.Row(c).Size(sizeControlSm, sizeControlSm).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label(tip).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(hoverGround)
	}
	b.Children(func() { body(c, glyph, inkMuted) })
	return b.Clicked()
}
