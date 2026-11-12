package native

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Plan 028 step 2: control fills under window opacity.

func TestControlAlphaFollowsTheWindowAlpha(t *testing.T) {
	t.Cleanup(func() { useControls(1, true) })
	for _, c := range []struct{ window, want float32 }{{1, 1}, {0.85, 0.91}, {0.7, 0.82}, {0.5, 0.72}, {0.3, 0.72}} {
		useControls(c.window, true)
		if math.Abs(float64(controlAlpha-c.want)) > 0.005 {
			t.Errorf("window alpha %v: control alpha %v, want %v", c.window, controlAlpha, c.want)
		}
	}
}

func TestControlsAreUntouchedAtOpacity1(t *testing.T) {
	t.Cleanup(func() { useControls(1, true) })
	for _, dark := range []bool{true, false} {
		p := paletteOf(dark)
		useControls(1, dark)
		for _, c := range []ui.Color{tok(p.stage), tok(p.surface), tok(p.raised)} {
			if got := controlFill(c); got != c {
				t.Errorf("dark=%v: controlFill(%v) = %v at opacity 1", dark, c, got)
			}
		}
		if hoverGround != tok(p.hover) {
			t.Errorf("dark=%v: hover %v at opacity 1, want %v", dark, hoverGround, tok(p.hover))
		}
	}
}

// Every fill a control uses, with its text, over black and over white at the lowest control alpha: 4.5:1 for ink and
// ink-muted, 3:1 for ink-faint, and no worse than the opaque pair where that is already lower.
func TestTranslucentControlFillsKeepTheirTextContrast(t *testing.T) {
	t.Cleanup(func() { useControls(1, true) })
	for _, dark := range []bool{true, false} {
		p := paletteOf(dark)
		useControls(0.3, dark) // window opacity at its floor: controls at controlFloor or above
		theme := map[bool]string{true: "dark", false: "light"}[dark]
		for _, f := range []struct {
			name string
			fill ui.Color
		}{{"stage (inputs)", tok(p.stage)}, {"surface (cards)", tok(p.surface)}, {"raised (segments, tabs, buttons)", tok(p.raised)}, {"hover (rows, chips)", tok(p.hover)}} {
			got := controlFill(f.fill)
			if got.A == 255 {
				t.Logf("%s %-34s opaque (only an opaque fill keeps its text)", theme, f.name)
				continue
			}
			for _, fg := range []struct {
				name string
				col  ui.Color
				want float64
			}{{"ink", tok(p.ink), 4.5}, {"ink-muted", tok(p.inkMuted), 4.5}, {"ink-faint", tok(p.inkFaint), 3}} {
				thr := math.Min(fg.want, contrast(fg.col, f.fill)-0.05)
				worst := math.Inf(1)
				for _, back := range []ui.Color{ui.RGB(0, 0, 0), ui.RGB(255, 255, 255)} {
					worst = math.Min(worst, contrast(fg.col, got.Over(back)))
				}
				t.Logf("%s %-34s alpha %.2f  %-9s worst %.2f (needs %.2f, opaque %.2f)", theme, f.name, float64(got.A)/255, fg.name, worst, thr, contrast(fg.col, f.fill))
				if worst < thr-0.05 { // 0.05: the alpha is a byte
					t.Errorf("%s: %s on %s is %.2f over black or white at alpha %.2f, needs %.2f", theme, fg.name, f.name, worst, float64(got.A)/255, thr)
				}
			}
		}
	}
}

// The sidebar's filter input is a control on the glass: at window alpha 0.7 its fill is the stage at the control alpha
// (0.82, or more where its text needs it), once, not the ground's 0.7 and not stacked; the sidebar's ground stays at 0.7.
// Dialogs, drawn after the stage, see opaque controls.
func TestControlFillOnTheGlass(t *testing.T) {
	t.Cleanup(func() { useControls(1, true) })
	for _, a := range []float32{0.7, 1} {
		r := newRig(t, okBackend())
		r.w.alpha = a
		r.tt = ui.NewTester(r.w.view, 1280, 720)
		r.tt.Frame()
		if controlAlpha != 1 {
			t.Errorf("controls are at %v after a frame: dialogs would be translucent", controlAlpha)
		}
		img := r.tt.Image()
		if img == nil {
			t.Skip("Tester.Image gave no pixels")
		}
		in, ok := r.tt.Find("Filter hosts")
		if !ok {
			t.Fatal("no filter input")
		}
		useControls(a, true)
		want := controlFill(stage)
		useControls(1, true)
		// The fill sits on the sidebar's ground, so what shows is the fill at ac over the ground at a: coverage
		// 1-(1-ac)(1-a), colour (stage*ac + chrome*a*(1-ac)), premultiplied.
		ac := float32(want.A) / 255
		got := img.RGBAAt(int(in.X+in.W-4), int(in.Y+in.H/2))
		near := func(g uint8, v float32) bool { d := float32(g) - v; return d >= -2 && d <= 2 }
		if !near(got.R, float32(stage.R)*ac+float32(chrome.R)*a*(1-ac)) || !near(got.G, float32(stage.G)*ac+float32(chrome.G)*a*(1-ac)) ||
			!near(got.B, float32(stage.B)*ac+float32(chrome.B)*a*(1-ac)) || !near(got.A, 255*(1-(1-ac)*(1-a))) {
			t.Errorf("filter input at window alpha %v: got %v, want stage at %.2f over the ground at %v", a, got, ac, a)
		}
		wantPixel(t, "sidebar ground", img, 100, 400, chrome, a)
		if a == 1 && want.A != 255 {
			t.Errorf("fill alpha %d at opacity 1", want.A)
		}
		if a < 1 && want.A <= uint8(a*255+1) {
			t.Errorf("control alpha %d is not above the ground's %v", want.A, a*255)
		}
	}
}
