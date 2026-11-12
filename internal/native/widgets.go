package native

import "github.com/egoist/mygo/ui"

// Small building blocks shared by the views. Colours and sizes come from tokens.go only.

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
	b := ctlIconButton(c, tip, size)
	if off {
		b.Disabled(true)
		b.Children(func() { icon(c, name, inkMuted) })
		return false
	}
	col := inkMuted
	switch {
	case pressed:
		b.Background(accentSoft)
		col = accentInk
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
