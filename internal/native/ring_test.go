package native

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The focused tile of a split tab has a 1.5 px border in focus (lineStrong while the window is not
// the active one), the others 1 px in line, a single or zoomed pane none; focus never moves a tile.
func TestTileRingByState(t *testing.T) {
	for _, th := range []string{themeDark, themeLight} {
		t.Run(th, func(t *testing.T) {
			r := themeRig(t)
			r.w.setTheme(th)
			r.click(t, "Split right")
			if r.tt.Image() == nil {
				t.Skip("Tester.Image gave no pixels")
			}
			edge := func(id string) ([3]uint8, [3]uint8) { // the border pixel and the one 3 px in
				p, _ := r.tt.Find("pane " + id)
				return r.pixel(p.X+0.5, p.Y+p.H/2), r.pixel(p.X+3, p.Y+p.H/2)
			}
			border := func(id string, want ui.Color, what string) {
				t.Helper()
				if got, _ := edge(id); !is(got, want) {
					t.Errorf("%s: pane %s border %v, want %v", what, id, got, want)
				}
			}
			// pane 2 is focused after the split
			border("2", focus, "focused")
			border("1", line, "unfocused")
			a1, _ := r.tt.Find("pane 1")
			b2, _ := r.tt.Find("pane 2")
			if _, in := edge("2"); is(in, focus) {
				t.Errorf("the ring is wider than 1.5 px: %v 3 px in", in)
			}

			// focus moves, the tiles do not
			if err := r.tt.Click("pane bar 1"); err != nil {
				t.Fatal(err)
			}
			r.tt.Frame()
			if got, _ := r.tt.Find("pane 1"); got != a1 {
				t.Errorf("pane 1 moved with focus: %v -> %v", a1, got)
			}
			if got, _ := r.tt.Find("pane 2"); got != b2 {
				t.Errorf("pane 2 moved with focus: %v -> %v", b2, got)
			}
			border("1", focus, "focus moved")
			border("2", line, "focus moved")

			// the window is not the active one: lineStrong, no glow
			r.tt.SetWindowActive(false)
			r.tt.Frame()
			border("1", lineStrong, "window inactive")
			border("2", line, "window inactive")
			if glow := r.pixel(a1.X-3, a1.Y+a1.H/2); is(glow, focus.Alpha(glowAlphaDark).Over(chrome)) || is(glow, focus.Alpha(glowAlphaLight).Over(chrome)) {
				t.Errorf("a glow while the window is inactive: %v", glow)
			}
			r.tt.SetWindowActive(true)
			r.tt.Frame()

			// zoomed: one tile, no ring
			r.w.toggleZoom(r.w.tabs[0], "1")
			r.tt.Frame()
			z, _ := r.tt.Find("pane 1")
			if got := r.pixel(z.X+0.5, z.Y+z.H/2); is(got, focus) || is(got, line) {
				t.Errorf("a zoomed pane has a border: %v", got)
			}
		})
	}
}

// A single pane has no ring and no glow.
func TestSinglePaneHasNoRing(t *testing.T) {
	r := themeRig(t)
	if r.tt.Image() == nil {
		t.Skip("Tester.Image gave no pixels")
	}
	p, _ := r.tt.Find("pane 1")
	if got := r.pixel(p.X+0.5, p.Y+p.H/2); is(got, focus) || is(got, line) {
		t.Errorf("a single pane has a border: %v", got)
	}
}
