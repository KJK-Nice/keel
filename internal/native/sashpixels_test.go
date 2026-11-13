package native

import (
	"image/color"
	"slices"
	"testing"
)

// Both width sashes sit on their panel's existing 1 px border line: at rest nothing but that line, hovered or focused a
// 2 px line that covers it (the border's pixel and the one inside), in both themes.
func TestTheSashesLieOnThePanelBorderAndAreTwoPixelsThick(t *testing.T) {
	for _, th := range []string{themeDark, themeLight} {
		r := agentRig(t)
		r.w.setTheme(th)
		r.tt.Frame()
		r.tt.Frame()
		for _, s := range []struct {
			name    string
			border  func() int // the x of the panel's border pixel
			inward  int        // +1: the panel lies right of the border (its left edge), -1: left of it
			sashTip string
		}{
			{"sidebar", func() int { return int(r.w.sidebarNow()) - 1 }, -1, "Resize sidebar"},
			{"agent", func() int { p, _ := r.tt.Find("agent panel"); return int(p.X) }, +1, "Resize agent panel"},
		} {
			r.tt.Move(1, 1)
			r.tt.Frame()
			bx := s.border()
			plainY := -1
			cols := func() (marked []int) {
				img := r.tt.Image()
				sc := float32(img.Bounds().Dx()) / 1280
				if sc <= 0 {
					t.Fatal("empty image")
				}
				at := func(x, y int) color.RGBA {
					return img.RGBAAt(int(float32(x)*sc+sc/2), int(float32(y)*sc+sc/2))
				}
				for y := 120; y < 640; y += 4 { // a row where the panel is plain ground, the same at rest and hovered
					if plainY >= 0 && y != plainY {
						continue
					}
					plain := true
					ref := at(bx+s.inward*20, y)
					for d := 3; d <= 20; d++ {
						if at(bx+s.inward*d, y) != ref {
							plain = false
						}
					}
					if !plain && plainY < 0 {
						continue
					}
					ref = at(bx+s.inward*20, y)
					plainY = y
					for d := 0; d <= 2; d++ { // the border and the two pixels inside it
						x := bx + s.inward*d
						if at(x, y) != ref {
							marked = append(marked, x)
						}
					}
					return marked
				}
				t.Fatalf("%s %s: no plain row", th, s.name)
				return nil
			}
			if got := cols(); len(got) != 1 || got[0] != bx {
				t.Errorf("%s %s at rest: marked columns %v, want only the border %d", th, s.name, got, bx)
			}
			sh, ok := r.tt.Find(s.sashTip)
			if !ok {
				t.Fatalf("%s: no %s", th, s.sashTip)
			}
			r.tt.Move(sh.X+sh.W/2, sh.Y+sh.H/2)
			r.tt.Frame()
			r.tt.Frame()
			got := cols()
			slices.Sort(got)
			lo, hi := bx, bx+s.inward
			if lo > hi {
				lo, hi = hi, lo
			}
			if len(got) != 2 || got[0] != lo || got[1] != hi {
				t.Errorf("%s %s hovered: marked columns %v, want %d and %d", th, s.name, got, lo, hi)
			}
		}
	}
}
