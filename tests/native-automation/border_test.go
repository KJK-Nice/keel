//go:build nativeautomation

package nativeautomation

import (
	"runtime"
	"testing"
)

// Plan 037 part C, addition: the real window has a 1 pt line round it, in the strong line token, in both themes and at window
// opacity 100 and 30. The socket's capture is the app's own render (no compositor), so it holds the line and nothing behind the
// window: the four edge pixels carry the token colour at full alpha, and the corner pixel does not (the corners are rounded).
// The shots (.scratch-native/automation/border-*.png) are opened by eye for the continuity round the corners.
func TestWindowBorderInARealWindow(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the border is drawn on macOS only")
	}
	for _, c := range []struct {
		theme   string
		opacity int
		want    [3]int
	}{{"dark", 100, [3]int{0x7e, 0x7e, 0x87}}, {"light", 100, [3]int{0x96, 0x96, 0x9a}}, {"dark", 30, [3]int{0x7e, 0x7e, 0x87}}, {"light", 30, [3]int{0x96, 0x96, 0x9a}}} {
		r := newRig(t)
		r.ghostCfg(c.opacity, 0, c.theme)
		r.startServe()
		r.startApp()
		openShell(r)
		r.settle()
		img := r.capture("border-" + c.theme + "-" + itoa(c.opacity))
		b := img.Bounds()
		px := func(x, y int) [3]int {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a>>8 != 255 {
				return [3]int{-1, -1, -1}
			}
			return [3]int{int(r >> 8), int(g >> 8), int(bl >> 8)}
		}
		w, h := b.Dx(), b.Dy()
		for name, p := range map[string][2]int{"left": {0, h / 2}, "right": {w - 1, h / 2}, "top": {w / 2, 0}, "bottom": {w / 2, h - 1}} {
			got := px(p[0], p[1])
			for i := range got {
				if d := got[i] - c.want[i]; d > 6 || d < -6 {
					t.Errorf("%s %d %s edge: %v, want about %v", c.theme, c.opacity, name, got, c.want)
					break
				}
			}
		}
		if got := px(0, 0); got == c.want {
			t.Errorf("%s %d: a square corner at (0,0): %v", c.theme, c.opacity, got)
		}
		r.quit()
	}
}

func itoa(n int) string {
	if n == 100 {
		return "100"
	}
	return "30"
}
