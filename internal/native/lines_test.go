package native

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// At the seams of the workspace there is one line, of one colour, one DIP wide (so whole device pixels at
// every scale), never two touching. MyGo snaps a Border to device pixels; this keeps the views from stacking a second one.

// runs lists the runs of `want`-coloured pixels along a line of the tester image, as {start, length} in device pixels,
// scanning n device pixels from (x, y) by (dx, dy).
func runsOf(r *rig, x0, y0 float32, dx, dy, n int, want ui.Color, scale float32) [][2]int {
	img := r.tt.Image()
	var out [][2]int
	in := false
	px, py := int(x0*scale), int(y0*scale)
	for i := 0; i < n; i++ {
		o := img.PixOffset(px+i*dx, py+i*dy)
		hit := is([3]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2]}, want)
		switch {
		case hit && !in:
			out = append(out, [2]int{i, 1})
		case hit:
			out[len(out)-1][1]++
		}
		in = hit
	}
	return out
}

// Only the terminal's text is dimmed in an unfocused tile, so both bodies are the same ground
// and the light theme does not show a white tile beside a grey one.
func TestUnfocusedTileBodyKeepsItsGround(t *testing.T) {
	for _, th := range []string{themeLight, themeDark} {
		r := themeRig(t)
		r.w.setTheme(th)
		r.click(t, "Split right")
		if r.tt.Image() == nil {
			t.Skip("Tester.Image gave no pixels")
		}
		a, _ := r.tt.Find("pane 1")
		b, _ := r.tt.Find("pane 2")
		pa, pb := r.pixel(a.X+a.W/2, a.Y+a.H*0.8), r.pixel(b.X+b.W/2, b.Y+b.H*0.8)
		if pa != pb || !is(pa, stage) {
			t.Errorf("%s: unfocused body %v, focused body %v, want both the stage %v", th, pa, pb, stage)
		}
	}
}

// A key cap is flat, inline and held, in both themes: the same colour on all four edges, one device pixel
// wide at scale 1, nothing under the bottom edge (no shadow, no thicker edge), the same box as kbdBox.
func TestKeyCapsAreFlat(t *testing.T) {
	for _, dark := range []bool{true, false} {
		for _, held := range []bool{false, true} {
			useTheme(dark)
			useControls(1, dark)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Background(stage).Padding(20).AlignItems(ui.Start).Children(func() {
					keyCapFace(c, "K", held, func(e ui.Element) { e.Label("cap") })
				})
			}, 120, 80)
			tt.Frame()
			img := tt.Image()
			if img == nil {
				t.Skip("Tester.Image gave no pixels")
			}
			r, ok := tt.Find("cap")
			if !ok {
				t.Fatal("no cap")
			}
			if r.H < kbdBox-0.01 || r.H > kbdBox+0.01 || r.W < kbdBox-0.01 {
				t.Errorf("held=%v: cap box %vx%v, want at least %vx%v", held, r.W, r.H, kbdBox, kbdBox)
			}
			x0, y0, x1, y1 := int(math.Round(float64(r.X))), int(math.Round(float64(r.Y))), int(math.Round(float64(r.X+r.W)))-1, int(math.Round(float64(r.Y+r.H)))-1
			edge, fill := line, surface
			if held {
				edge, fill = lineStrong, raised
			}
			at := func(x, y int) [3]uint8 {
				c := img.RGBAAt(x, y)
				return [3]uint8{c.R, c.G, c.B}
			}
			midX, midY := x0+5, y0+9 // clear of the rounded corners (radius 3) and the glyph
			for name, p := range map[string][3]uint8{
				"top": at(midX, y0), "bottom": at(midX, y1), "left": at(x0, midY), "right": at(x1, midY),
			} {
				if !is(p, edge) {
					t.Errorf("dark=%v held=%v: %s edge %v, want %v", dark, held, name, p, edge)
				}
			}
			if !is(at(midX, y0+1), fill) || !is(at(midX, y1-1), fill) {
				t.Errorf("dark=%v held=%v: the edge is wider than one pixel", dark, held)
			}
			for dy := 1; dy <= 3; dy++ { // under the cap: the ground, no shadow
				if p := at(midX, y1+dy); !is(p, stage) {
					t.Errorf("dark=%v held=%v: %d px under the cap is %v, want the stage (a shadow?)", dark, held, dy, p)
				}
			}
		}
	}
	useTheme(true)
}

// The token values themselves, measured for the maintainer (docs/design/tokens.json is not changed by them): how far each
// line stands from the ground it sits on. Read with -v.
func TestLineTokenContrastIsReported(t *testing.T) {
	for _, dark := range []bool{true, false} {
		p := paletteOf(dark)
		t.Logf("dark=%v: line on chrome %.2f, line on stage %.2f, line-strong on stage %.2f, line-strong on chrome %.2f", dark,
			contrast(tok(p.line), tok(p.chrome)), contrast(tok(p.line), tok(p.stage)), contrast(tok(p.lineStrong), tok(p.stage)), contrast(tok(p.lineStrong), tok(p.chrome)))
	}
}

func TestSeamsAreOneLineOfOneColour(t *testing.T) {
	for _, th := range []string{themeLight, themeDark} {
		for _, scale := range []float32{1, 2} {
			r := themeRig(t)
			r.w.setTheme(th)
			r.click(t, "Split right")
			r.tt.SetScale(scale)
			r.tt.Frame()
			if r.tt.Image() == nil {
				t.Skip("Tester.Image gave no pixels")
			}
			want := int(scale) // a 1 DIP line is `scale` device pixels
			name := func(s string) string { return th + " " + s }
			one := func(what string, got [][2]int, at int) {
				t.Helper()
				if len(got) != 1 || got[0][1] != want || got[0][0] != at {
					t.Errorf("%s at scale %v: runs %v, want one of %d px at %d", name(what), scale, got, want, at)
				}
			}
			// The sidebar's right edge: the seam, then the chrome gap, then the tile's own border; 4 DIPs either side.
			side := runsOf(r, sizeSidebar-4, 400, 1, 0, int(4*scale), line, scale)
			one("sidebar edge", side, int(3*scale))
			// The title bar's bottom edge and the status bar's top edge, on a column over the tiles.
			one("title bar edge", runsOf(r, 600, sizeTitlebar-4, 0, 1, int(4*scale), line, scale), int(3*scale))
			top := float32(720 - statusbarSize)
			one("status bar edge", runsOf(r, 600, top-4, 0, 1, int(5*scale), line, scale), int(4*scale))
			// Across the stage: the sidebar's edge, a gap, the tile's border: two lines, never adjacent.
			stageRuns := runsOf(r, sizeSidebar-1, 400, 1, 0, int(10*scale), line, scale)
			if len(stageRuns) != 2 || stageRuns[0][1] != want || stageRuns[1][1] != want || stageRuns[1][0]-stageRuns[0][0]-want < int(space1*scale) {
				t.Errorf("%s at scale %v: runs across the sidebar edge and the first tile %v, want two of %d px apart", name("stage seam"), scale, stageRuns, want)
			}
			// Between the tiles: the unfocused tile's border, the gap, the focused tile's ring (focus, not line).
			a, _ := r.tt.Find("pane 1")
			gap := runsOf(r, a.X+a.W-3, 400, 1, 0, int(8*scale), line, scale)
			one("pane gap", gap, int(2*scale))
		}
	}
}
