package native

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Control fills under window opacity (plan 028 step 2). The grounds take the window's alpha (workspace.ground); a control
// drawn on them (an input, a segment, a tab pill, a button, a card, a hover fill) takes a milder one, so with opacity
// below 1 it reads as part of the glass and not as a sticker on it: controlAlpha = 1 - (1 - alpha) * controlShare, never
// below controlFloor. Plan 030: a control over the ground covers 1 - (1-alpha)(1-controlAlpha) of the backdrop, and the
// share is set so that is 0.8 at window opacity 0.7 (plan 028's 0.6 gave 0.95, too opaque). At alpha 1 it is 1 and nothing
// changes. Text, icons, borders, the primary and destructive buttons, dialogs, menus, popovers, toasts, the palette, key
// caps and the terminal never go through it.
const (
	controlShare = 2.2
	controlFloor = 0.2
)

// controlAlpha is the fill alpha of controls this frame; useControls sets it, as useTheme sets the colours.
var controlAlpha float32 = 1

// controlCache holds minControlAlpha by fill and theme; the answer depends on nothing else.
var controlCache = map[ctlKey]float32{}

type ctlKey struct {
	fill ui.Color
	dark bool
}

// useControls runs after the window's alpha is known: it sets controlAlpha and the hover fill, which every hover and
// selected row, button and chip uses as a plain colour.
func useControls(alpha float32, dark bool) {
	controlAlpha = 1
	if alpha < 1 {
		controlAlpha = max(1-(1-alpha)*controlShare, controlFloor)
	}
	ctlDark = dark
	hoverGround = controlFill(tok(paletteOf(dark).hover))
}

// ctlDark is the theme controlFill reads the text colours of.
var ctlDark = true

// controlFill is a control's fill colour at the control alpha, raised where the text on it would fall under its contrast
// floor over a black or a white backdrop (minControlAlpha). Opaque at alpha 1.
func controlFill(fill ui.Color) ui.Color {
	if controlAlpha >= 1 {
		return fill
	}
	return fill.Alpha(max(controlAlpha, minControlAlpha(fill, ctlDark)))
}

// minControlAlpha is the least alpha at which the text that sits on fill keeps its contrast over both a black and a white
// backdrop: 4.5:1 for ink and ink-muted, 3:1 for ink-faint (captions and placeholders), and where the opaque pair is
// already below that, no worse than it is. 1 when only an opaque fill will do.
func minControlAlpha(fill ui.Color, dark bool) float32 {
	k := ctlKey{fill, dark}
	if a, ok := controlCache[k]; ok {
		return a
	}
	p := paletteOf(dark)
	fills := [3]ui.Color{tok(p.ink), tok(p.inkMuted), tok(p.inkFaint)}
	wants := [3]float64{4.5, 4.5, 3}
	at := func(a float32) bool {
		for i, fg := range fills {
			thr := math.Min(wants[i], contrast(fg, fill)-0.05)
			for _, back := range []ui.Color{ui.RGB(0, 0, 0), ui.RGB(255, 255, 255)} {
				if contrast(fg, fill.Alpha(a).Over(back)) < thr {
					return false
				}
			}
		}
		return true
	}
	res := float32(1)
	for a := float32(controlFloor); a < 1; a += 0.01 {
		if at(a) {
			res = a
			break
		}
	}
	controlCache[k] = res
	return res
}

// contrast is the WCAG contrast ratio of two opaque colours.
func contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c ui.Color) float64 {
	f := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

// Native-only values (plan 028). They are deliberate departures from the webview's look that the maintainer asked
// for, so they are not in docs/design/tokens.json (TestTokensMatchJSON keeps that file and tokens.go in step). Each
// is derived from a token where it can be; there is no colour literal here.
const (
	ringWidth      = 1.5  // the focused pane tile's border (the webview draws a 3 px band)
	tileLineWidth  = 1    // every other split tile's border, in `line`
	glowBlur       = 8    // the focused tile's soft outer glow
	glowAlphaDark  = 0.18 // focus at this alpha on a dark stage
	glowAlphaLight = 0.12 // and on a light one, so it does not smear
	emptyAbove     = 38   // the page-size empty state sits this share of the free height down (docs/native.md)
	emptyDescPx    = 300  // its words' widest, about 44 characters at the caption size
)

// tileRing draws the border and the glow of a tile of a split tab. Focus changes the colour and the weight, never the
// tile's size: the border is painted over the tile's edge (DrawOver), not laid out as a Border, because a layout
// border of another width changes how the flex siblings share the room. The ring follows keyboard focus and the
// window's: while the window is not the active one it falls back to lineStrong with no glow. It never animates.
func (w *workspace) tileRing(c *ui.Context, tl ui.Element, focused bool) {
	col, width := line, float32(tileLineWidth)
	switch {
	case focused && c.WindowActive():
		col, width = focus, ringWidth
		a := float32(glowAlphaDark)
		if !w.dark {
			a = glowAlphaLight
		}
		tl.Shadow(0, 0, glowBlur, 0, focus.Alpha(a))
	case focused:
		col, width = lineStrong, ringWidth
	}
	tl.DrawOver(func(pt *ui.Painter, r ui.Rect) { pt.Stroke(r, col, radiusMd, width) })
}
