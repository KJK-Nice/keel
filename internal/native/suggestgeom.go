package native

import (
	"math"
	"runtime"

	"github.com/egoist/mygo/ui"
)

// Where the prompt line is on the screen, for the notice and the card that sit over it (docs/ui-redesign.md §5.5).
//
// The terminal plugin has no call for its cell size or for the cursor's row (its view tells an input method where the
// caret is, but only a test can read that). So the row comes from the Snapshot (readSnapshot, typedirect.go), and a cell
// is measured the way the plugin's view does it (plugins/terminal paint.go layout): the widest advance of the printable
// ASCII in the font, rounded up to whole device pixels, and the font's line height the same way, inside the padding the
// terminal is given in run.go. suggestgate_test.go holds that to the plugin's own caret rect, after a resize, a text
// size change and at two display scales; a plugin that changes how it lays out a cell fails it.

// termPadX and termPadY are the room around the terminal's grid, in DIPs: the plugin's own default, written out so the
// window knows it (run.go passes them as terminal.Options.Padding).
const termPadX, termPadY = 6, 4

const asciiPrintable = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

// cellGrid is a terminal's grid in DIPs, from the top left of the terminal view: where the first cell is, and a cell's size.
type cellGrid struct{ ox, oy, cw, ch float32 }

// gridFor measures the grid of a terminal in family at size DIPs (weight 400, line height 1) at a display scale.
func gridFor(family string, size, scale float32) cellGrid {
	if scale <= 0 {
		scale = 1
	}
	if family == "" {
		family = "monospace"
	}
	f := ui.Font{Family: family, Size: size, Weight: 400}
	if runtime.GOOS == "darwin" {
		f.Features = "kern=0" // the plugin turns kerning off on macOS so that each character keeps its cell
	}
	adv := float32(0)
	for _, g := range ui.Shape(asciiPrintable, f) {
		adv = max(adv, g.Advance)
	}
	m := f.Metrics()
	px := func(v float32) float32 { return float32(max(int(math.Ceil(float64(v*scale-0.05))), 1)) }
	return cellGrid{
		ox: float32(math.Round(float64(termPadX*scale))) / scale,
		oy: float32(math.Round(float64(termPadY*scale))) / scale,
		cw: px(adv) / scale,
		ch: px(m.Ascent+m.Descent+m.LineGap) / scale,
	}
}

// at is the rect of cells [col, col+n) on screen row row (0-based) in DIPs from the top left of the terminal view.
func (g cellGrid) at(row, col, n int) ui.Rect {
	return ui.Rect{X: g.ox + float32(col)*g.cw, Y: g.oy + float32(row)*g.ch, W: float32(n) * g.cw, H: g.ch}
}

// cursorRow is the line the cursor is on, read from a terminal Snapshot: its screen row (0-based), its text with the
// trailing spaces cut, and whether the snapshot placed it. ok is false on the alternate screen (a full-screen program),
// for a snapshot that cannot be read, and when the cursor's row is not in it: never a guess, as screenOf guesses for a
// Type, because a notice is drawn on exactly that row.
func cursorRow(snap string, rows int) (row int, text string, ok bool) {
	alt, lines, top, r := readSnapshot(snap, rows)
	if alt || r <= 0 || top+r-1 >= len(lines) {
		return 0, "", false
	}
	return r - 1, lines[top+r-1], true
}
