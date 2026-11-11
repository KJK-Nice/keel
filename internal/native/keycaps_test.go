package native

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
)

// capLabels are the key caps the last frame drew, as "B", "⇧D", in order.
func capLabels(r *rig) []string {
	var out []string
	for _, s := range r.tt.Texts() {
		if k, ok := strings.CutPrefix(s, "key cap "); ok {
			out = append(out, k)
		}
	}
	return out
}

func holdCaps(r *rig) {
	r.tt.HoldModifiers(ui.Cmd)
	r.tt.Frame()
	r.tt.Advance(capDelay + capFade + 20*time.Millisecond)
}

func TestKeyCapsAppearAfterTheDelayAndGoOnRelease(t *testing.T) {
	r := splitRig(t)
	r.tt.HoldModifiers(ui.Cmd)
	r.tt.Frame()
	r.tt.Advance(capDelay - 40*time.Millisecond)
	if len(capLabels(r)) != 0 {
		t.Fatalf("caps before the delay: %v", capLabels(r))
	}
	r.tt.Advance(capFade + 60*time.Millisecond)
	if len(capLabels(r)) == 0 {
		t.Fatal("no caps after the delay")
	}
	r.tt.HoldModifiers(0)
	if len(capLabels(r)) != 0 { // at once: no Advance
		t.Errorf("caps after release: %v", capLabels(r))
	}
}

func TestKeyCapsGoOnAnotherKeyAClickAndBlur(t *testing.T) {
	for name, end := range map[string]func(r *rig){
		"key":   func(r *rig) { r.tt.KeyDown(ui.KeyA) },
		"click": func(r *rig) { r.tt.ClickAtWith(ui.Cmd, 640, 360) },
		"blur":  func(r *rig) { r.tt.SetFocused(false) },
	} {
		r := splitRig(t)
		holdCaps(r)
		if len(capLabels(r)) == 0 {
			t.Fatalf("%s: no caps to hide", name)
		}
		end(r)
		r.tt.Frame()
		if len(capLabels(r)) != 0 {
			t.Errorf("%s: caps still show: %v", name, capLabels(r))
		}
	}
}

// A key whose release was lost before ⌘ went down must not keep the caps away.
func TestKeyCapsIgnoreAKeyHeldBeforeTheHold(t *testing.T) {
	r := splitRig(t)
	r.tt.KeyDown(ui.KeyD)
	r.tt.Frame()
	holdCaps(r)
	if len(capLabels(r)) == 0 {
		t.Fatal("a stale key hid the caps")
	}
	r.tt.KeyDown(ui.KeyT)
	r.tt.Frame()
	if len(capLabels(r)) != 0 {
		t.Error("a new key kept the caps")
	}
}

func TestKeyCapsCoverTheElementsOfTheApp(t *testing.T) {
	r := splitRig(t)
	r.click(t, "host api-1") // a second tab
	r.w.splitFocused(layout.Row)
	r.tt.Frame()
	holdCaps(r)
	got := map[string]int{}
	for _, k := range capLabels(r) {
		got[k]++
	}
	// Tabs 1 and 2, the sidebar toggle, + tab, New host, Settings, and the focused pane's three tools (one pane each).
	for _, k := range []string{"1", "2", "B", "T", "N", ",", "⇧D", "⇧E", "⇧↵"} {
		if got[k] != 1 {
			t.Errorf("cap %q drawn %d times, want 1 (all: %v)", k, got[k], got)
		}
	}
	if len(got) != 9 {
		t.Errorf("caps %v", got)
	}
}

func TestTabKeys(t *testing.T) {
	for _, c := range []struct {
		i, n int
		want string
	}{{0, 12, "1"}, {7, 12, "8"}, {8, 12, ""}, {11, 12, "9"}, {8, 9, "9"}, {2, 3, "3"}} {
		if got := tabKey(c.i, c.n); got != c.want {
			t.Errorf("tabKey(%d, %d) = %q, want %q", c.i, c.n, got, c.want)
		}
	}
}

func TestKeyCapsMoveNothing(t *testing.T) {
	r := splitRig(t)
	names := []string{"tab api-1", "Toggle sidebar", "New tab", "New host", "Settings", "Split right", "Split down", "Focus view"}
	before := map[string]ui.Rect{}
	for _, n := range names {
		rc, ok := r.tt.Find(n)
		if !ok {
			t.Fatalf("no %q", n)
		}
		before[n] = rc
	}
	holdCaps(r)
	if len(capLabels(r)) == 0 {
		t.Fatal("no caps")
	}
	for _, n := range names {
		if rc, _ := r.tt.Find(n); rc != before[n] {
			t.Errorf("%s moved: %v -> %v", n, before[n], rc)
		}
	}
}

// The cap stands in for what is under it: nothing of a page tab's icon shows beside it.
func TestKeyCapReplacesThePageTabsIcon(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.SetDark(false)
	r.tt.Frame()
	r.w.openPage("settings")
	r.tt.Frame()
	tab, ok := r.tt.Find("tab Settings")
	if !ok {
		t.Fatal("no Settings tab")
	}
	holdCaps(r)
	cap, ok := r.tt.Find("key cap 2") // the New tab page is tab 1
	if !ok {
		t.Fatalf("no cap on the Settings tab: %v", capLabels(r))
	}
	img := r.tt.Image()
	s := float32(img.Bounds().Dx()) / 1280
	x0, x1 := int((cap.X+cap.W+1)*s), int((tab.X+space2+iconSize)*s)
	for x := x0; x <= x1; x++ {
		for y := int((tab.Y + 6) * s); y < int((tab.Y+tab.H-6)*s); y++ {
			gr, gg, gb, _ := img.At(x, y).RGBA()
			for _, d := range [3]int{int(gr>>8) - int(raised.R), int(gg>>8) - int(raised.G), int(gb>>8) - int(raised.B)} {
				if d > 40 || d < -40 {
					t.Fatalf("pixel (%d,%d) beside the cap is %v: the icon shows", x, y, img.At(x, y))
				}
			}
		}
	}
}

func TestKeyCapsInBothThemes(t *testing.T) {
	for _, dark := range []bool{true, false} {
		r := splitRig(t)
		r.tt.SetDark(dark)
		r.tt.Frame()
		holdCaps(r)
		rc, ok := r.tt.Find("key cap B")
		if !ok {
			t.Fatalf("dark=%v: no cap", dark)
		}
		px := r.tt.Image().At(int(rc.X)+3, int(rc.Y+rc.H/2)) // inside the border, left of the glyph
		gr, gg, gb, ga := px.RGBA()
		if ga != 0xffff || gr>>8 != uint32(raised.R) || gg>>8 != uint32(raised.G) || gb>>8 != uint32(raised.B) {
			t.Errorf("dark=%v: cap ground %v, want the raised token", dark, px)
		}
	}
}

func TestKeyCapsFadeUnlessMotionIsReduced(t *testing.T) {
	r := splitRig(t)
	r.tt.HoldModifiers(ui.Cmd)
	r.tt.Frame()
	r.tt.Advance(capDelay + capFade/4)
	if a := r.w.capAlpha; a <= 0 || a >= 1 {
		t.Errorf("alpha %v a quarter into the fade", a)
	}
	q := splitRig(t)
	q.tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	q.tt.HoldModifiers(ui.Cmd)
	q.tt.Frame()
	q.tt.Advance(capDelay + 10*time.Millisecond)
	if q.w.capAlpha != 1 {
		t.Errorf("alpha %v with reduced motion, want 1 at once", q.w.capAlpha)
	}
}
