package native

import "github.com/egoist/mygo/ui"

// Native-only values (plan 028). They are deliberate departures from the webview's look that the maintainer asked
// for, so they are not in docs/design/tokens.json (TestTokensMatchJSON keeps that file and tokens.go in step). Each
// is derived from a token where it can be; there is no colour literal here.
const (
	ringWidth      = 1.5  // the focused pane tile's border (the webview draws a 3 px band)
	tileLineWidth  = 1    // every other split tile's border, in `line`
	glowBlur       = 8    // the focused tile's soft outer glow
	glowAlphaDark  = 0.18 // focus at this alpha on a dark stage
	glowAlphaLight = 0.12 // and on a light one, so it does not smear
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
