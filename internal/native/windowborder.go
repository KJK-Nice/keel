package native

import (
	"github.com/egoist/mygo/ui"
)

// The window's border (plan 037 part C, addition): with no shadow the window's edge is lost against what is behind it, most of
// all a translucent window over a similar colour. A 1 pt line is drawn round the whole window in the root view, above the
// grounds and under the dialogs' own chrome, in the strong line token (the one token that shows against a light and a dark
// desktop in both themes) at full alpha: it is not made translucent with the grounds.
//
// MyGo and AppKit offer no border for a titled window with a hidden title bar (WindowOptions.CornerRadius rounds only a
// frameless one), so it is drawn here. Its corners follow the window's: macOS cuts a titled window to a system radius, 10 pt
// before macOS 26 and larger after (windowCornerRadius, darwin only). In full screen there is no line (Context.TitleBar is
// zero there, as in a window that draws none), and off macOS the system draws the frame.

const windowBorderWidth = 1

// windowBorder draws the line. It takes no pointer event and no focus (HitTest false, not focusable), and sits Absolute, so
// nothing in the layout moves by a pixel.
func (w *workspace) windowBorder(c *ui.Context) {
	bar := c.TitleBar()
	if w.cornerR <= 0 || (bar.Height <= 0 && bar.Left <= 0) {
		return
	}
	ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).Radius(w.cornerR).Border(windowBorderWidth, lineStrong).
		Label("window border").HitTest(func(float32, float32, ui.Rect) bool { return false })
}
