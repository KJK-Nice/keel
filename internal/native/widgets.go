package native

import "github.com/egoist/mygo/ui"

// Small building blocks shared by the views. Colours and sizes come from tokens.go only.

// windowTheme maps the Keel tokens onto the MyGo theme so stock widgets inherit them.
func windowTheme(c *ui.Context) {
	t := *c.Theme()
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
	b := ui.Row(c).Size(size, size).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label(tip).Cursor(ui.CursorPointer)
	col := inkMuted
	switch {
	case pressed:
		b.Background(brassSoft)
		col = brassInk
	case b.Hovered():
		b.Background(hoverGround)
	}
	b.Children(func() { icon(c, name, col) })
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
