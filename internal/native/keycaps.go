package native

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
)

// Key caps while ⌘ (Ctrl elsewhere) is held. The app's overlay is term.js showKbdHints: the modifier alone, held 280 ms
// (term.js:6792), puts a cap on the tabs, the + tab, the sidebar toggle, New host, Settings and the focused pane's tools;
// letting go or the window losing focus hides them. Here a cap also goes at once on any other key or a click, and appears
// with a short fade; it stands in for the element's icon or status dot, so it covers no label (the app hides the icon too).
const (
	capDelay = 280 * time.Millisecond // term.js:6792
	capFade  = 120 * time.Millisecond
	capKey   = "key caps"
	capLead  = 3 // a tab's cap starts this far in from its left edge, over the status dot
)

// updateCaps runs once a frame, before the view is built, and sets capAlpha: 0 while no cap shows.
func (w *workspace) updateCaps(c *ui.Context) {
	root := c.Root()
	if c.Modifiers()&ui.Cmd == 0 { // let go, or the window lost the keyboard (the tester and the platform both clear mods)
		w.capsSince, w.capsOff, w.capsBase = time.Time{}, false, nil
	} else {
		keys := c.KeysDown()
		if w.capsSince.IsZero() {
			// A key whose release a menu item's shortcut took stays "held" (⌘⇧D, as WebKit loses keyups under ⌘,
			// term.js heldKeys): keys down when the hold begins do not count.
			w.capsSince, w.capsBase = c.Now(), keys
		}
		for _, k := range keys {
			if !slices.Contains(w.capsBase, k) { // a shortcut used the hold
				w.capsOff = true
			}
		}
		if root.PressedInside() { // a click did
			w.capsOff = true
		}
	}
	show := !w.capsSince.IsZero() && !w.capsOff
	if wait := capDelay - c.Now().Sub(w.capsSince); show && wait > 0 {
		c.After(wait)
		show = false
	}
	if !show {
		w.capAlpha = 0
		root.Jump(capKey, 0)
		return
	}
	w.capAlpha = root.AnimateTo(capKey, 1, ui.Animation{Duration: capFade, Ease: ui.EaseOut}) // reduced motion: at once
}

// capBtn is an icon button that shows the cap of keys while ⌘ is held.
func (w *workspace) capBtn(c *ui.Context, keys, name, tip string, size float32, pressed, off bool) bool {
	return iconBtnCap(c, name, tip, size, pressed, off, func() { w.keyCap(c, keys) })
}

// paneCapBtn is a pane tool: only the focused pane's tools show caps, and a disabled one none (keel.css:1032-1035).
func (w *workspace) paneCapBtn(c *ui.Context, focused bool, keys, name, tip string, pressed, off bool) bool {
	if !focused {
		return iconBtnOff(c, name, tip, sizeControlSm, pressed, off)
	}
	return w.capBtn(c, keys, name, tip, sizeControlSm, pressed, off)
}

// tabKey is the digit that selects tab i of n: 1 to 8 the first eight, 9 the last (lib/tabstrip.js tabNumberKey).
func tabKey(i, n int) string {
	switch {
	case i < 8:
		return string(rune('1' + i))
	case i == n-1:
		return "9"
	}
	return ""
}

// keyCap puts the cap of a shortcut over the middle of the element being built, where its icon is: the cap stands in for
// the icon while ⌘ is held, as the app's does, and covers no label. keyCapLead is the same over a tab's status dot or icon
// at its left. A cap is the kbd type on a raised ground with a line border and a thick bottom edge, as a k-kbd, and a pop
// shadow so it reads over any ground, translucent ones included. It is absolute: the element never moves or resizes.
// It draws nothing while caps are hidden.
func (w *workspace) keyCap(c *ui.Context, keys string) {
	if w.capAlpha > 0 {
		ui.Row(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).Center().Children(func() { w.capFace(c, keys) })
	}
}

func (w *workspace) keyCapLead(c *ui.Context, keys string) {
	if w.capAlpha > 0 {
		ui.Row(c).Absolute().Top(0).Bottom(0).Left(capLead).AlignItems(ui.Center).Children(func() { w.capFace(c, keys) })
	}
}

func (w *workspace) capFace(c *ui.Context, keys string) {
	hairline(ui.Row(c).MinWidth(kbdBox).Height(kbdBox).Padding(0, space1).Radius(radiusXs).Justify(ui.Center).
		Background(raised).Opacity(w.capAlpha).Label("key cap "+keys).Shadow(0, 1, 4, 0, shadowPop), 1, 1, 2, 1).BorderColor(lineStrong).
		Children(func() { text(c, keys, ink, kbdSize, weightMedium).Font(monoStack) })
}
