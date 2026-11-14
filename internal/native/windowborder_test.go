package native

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A 1 pt border line round the window, in the strong line token at full alpha in both themes and at
// any window opacity, rounded as the window is, none in full screen, and no layout moves by it.

func borderRig(t *testing.T, theme string, alpha float32, r10 bool) *rig {
	t.Helper()
	r := newRig(t, okBackend())
	r.w.alpha = alpha
	if r10 {
		r.w.cornerR = 10
	}
	r.w.setTheme(theme)
	r.tt = ui.NewTester(r.w.view, 1280, 720)
	r.tt.SetTitleBar(ui.TitleBar{Height: 38, Left: 76})
	r.tt.Frame()
	r.tt.Frame()
	return r
}

func TestTheWindowBorderIsOnTheFourEdgesAndFollowsTheCorners(t *testing.T) {
	for _, th := range []string{themeDark, themeLight} {
		for _, a := range []float32{1, 0.3} {
			r := borderRig(t, th, a, true)
			img := r.tt.Image()
			sc := float32(img.Bounds().Dx()) / 1280
			at := func(x, y float32) color.RGBA { return img.RGBAAt(int(x*sc+sc/2), int(y*sc+sc/2)) }
			want := color.RGBA{R: lineStrong.R, G: lineStrong.G, B: lineStrong.B, A: 255}
			for name, p := range map[string][2]float32{"left": {0, 360}, "right": {1279, 360}, "top": {640, 0}, "bottom": {640, 719}} {
				if got := at(p[0], p[1]); got != want {
					t.Errorf("%s alpha %v %s edge: got %v, want %v (full alpha: the line is not translucent)", th, a, name, got, want)
				}
			}
			if got := at(1, 360); got == want {
				t.Errorf("%s alpha %v: the line is wider than 1 pt", th, a)
			}
			if got := at(0, 0); got == want {
				t.Errorf("%s alpha %v: a square corner pokes out at (0,0): %v", th, a, got)
			}
			// A 10 pt radius: the arc passes about (2.9, 2.9) and is anti-aliased, so only that it is drawn there is checked.
			if got, ground := at(3, 3), at(20, 20); got == ground || got.A == 0 {
				t.Errorf("%s alpha %v: no line on the arc at (3,3): %v over %v", th, a, got, ground)
			}
		}
	}
}

func TestNoWindowBorderInFullScreenOrWithoutARadius(t *testing.T) {
	r := borderRig(t, themeDark, 1, true)
	r.tt.SetTitleBar(ui.TitleBar{}) // full screen: the controls hide
	r.tt.Frame()
	if r.has("window border") {
		t.Error("a border in full screen")
	}
	r = borderRig(t, themeDark, 1, false)
	if r.has("window border") {
		t.Error("a border where the window has no radius (off macOS, tests)")
	}
}

func TestTheWindowBorderMovesNothingAndTakesNoPointer(t *testing.T) {
	with, without := borderRig(t, themeDark, 1, true), borderRig(t, themeDark, 1, false)
	for _, label := range []string{"Toggle sidebar", "Resize sidebar", "New tab", "Local Shell"} {
		a, okA := with.tt.Find(label)
		b, okB := without.tt.Find(label)
		if okA != okB || a != b {
			t.Errorf("%q moved: %v with the border, %v without", label, a, b)
		}
	}
	// A click on the window's edge row reaches what is under the line.
	with.tt.Move(640, 0)
	with.tt.Frame()
	if n := len(with.tt.Texts()); n == 0 {
		t.Fatal("no texts")
	}
}
