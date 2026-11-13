package native

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// sidebarresize.js and lib/sidebarwidth.js: the sash on the sidebar's right edge widens it by dragging right, steps with the
// arrows (space-4, ⇧ four times), goes to the bounds on Home and End, back to the default on a double click or Enter, and
// the width is kept in keel-native.json and comes back with the window.
func TestTheSidebarsSashResizesItAndTheWidthIsKept(t *testing.T) {
	def, lo, hi := sidebarWidth(), sidebarMin(), sidebarMax()
	if hi != 2*def || lo > def || lo < sizeRail {
		t.Fatalf("bounds %v..%v for the default %v", lo, hi, def)
	}
	for _, c := range []struct{ in, want float32 }{{0, def}, {-5, def}, {lo - 50, lo}, {300.4, 300}, {300.5, 301}, {hi + 100, hi}} {
		if got := clampSidebarWidth(c.in); got != c.want {
			t.Errorf("clampSidebarWidth(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	r := agentRig(t)
	width := func() float32 { p, ok := r.tt.Find("Resize sidebar"); _ = ok; return p.X + p.W } // the sash is on the right edge, over the 1 px seam
	near := func(got, want float32) bool { return got > want-1 && got < want+1 }
	sash := func() (x, y float32) {
		s, ok := r.tt.Find("Resize sidebar")
		if !ok {
			t.Fatal("no sidebar sash")
		}
		return s.X + s.W/2, s.Y + s.H/2
	}
	if got := width(); !near(got, def) {
		t.Fatalf("the sidebar is %v wide, want %v", got, def)
	}
	x, y := sash()
	r.tt.Press(x, y)
	r.tt.Move(x+30, y)
	r.tt.Move(x+60, y)
	r.tt.Frame()
	r.tt.Release(x+60, y)
	r.tt.Frame()
	r.tt.Frame()
	if got := width(); !near(got, def+60) {
		t.Fatalf("after a drag 60 px right the sidebar is %v wide, want %v", got, def+60)
	}
	if got := loadPrefs(r.w.dataDir).SidebarWidth; !near(got, def+60) {
		t.Errorf("keel-native.json holds %v", got)
	}
	// Far right stops at twice the default; far left at the narrowest.
	x, y = sash()
	r.tt.Press(x, y)
	r.tt.Move(x+400, y)
	r.tt.Move(x+900, y)
	r.tt.Frame()
	r.tt.Release(x+900, y)
	r.tt.Frame()
	r.tt.Frame()
	if got := width(); !near(got, hi) {
		t.Errorf("dragged far right the sidebar is %v wide, want %v", got, hi)
	}
	x, y = sash()
	r.tt.Press(x, y)
	r.tt.Move(x-300, y)
	r.tt.Move(x-450, y)
	r.tt.Frame()
	r.tt.Release(x-450, y)
	r.tt.Frame()
	r.tt.Frame()
	if got := width(); !near(got, lo) {
		t.Errorf("dragged far left the sidebar is %v wide, want %v", got, lo)
	}
	// → widens by space-4 when the sash has the keyboard, ⇧→ by four, ← narrows, End and Home go to the bounds, Enter back.
	r.w.setSidebarWidth(lo)
	r.tt.Frame()
	r.tt.Click("Resize sidebar")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyRight)
	r.tt.Frame()
	if got := width(); !near(got, lo+space4) {
		t.Errorf("→ gives %v, want %v", got, lo+space4)
	}
	r.tt.Key(ui.Shift, ui.KeyRight)
	r.tt.Frame()
	if got := width(); !near(got, lo+5*space4) {
		t.Errorf("⇧→ gives %v, want %v", got, lo+5*space4)
	}
	r.tt.Key(0, ui.KeyLeft)
	r.tt.Frame()
	if got := width(); !near(got, lo+4*space4) {
		t.Errorf("← gives %v, want %v", got, lo+4*space4)
	}
	r.tt.Key(0, ui.KeyEnd)
	r.tt.Frame()
	if got := width(); !near(got, hi) {
		t.Errorf("End gives %v, want %v", got, hi)
	}
	r.tt.Key(0, ui.KeyHome)
	r.tt.Frame()
	if got := width(); !near(got, lo) {
		t.Errorf("Home gives %v, want %v", got, lo)
	}
	r.tt.Key(0, ui.KeyRight)
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if got := width(); !near(got, def) || loadPrefs(r.w.dataDir).SidebarWidth != 0 {
		t.Errorf("Enter gives %v, kept %v", got, loadPrefs(r.w.dataDir).SidebarWidth)
	}
	// A double click puts the default back.
	r.w.setSidebarWidth(400)
	r.tt.Frame()
	x, y = sash()
	r.tt.ClickAt(x, y)
	r.tt.ClickAt(x, y)
	r.tt.Frame()
	if got := width(); !near(got, def) {
		t.Errorf("a double click gives %v", got)
	}
	// The width comes back with the window, and a width out of bounds is held to them.
	r.w.setSidebarWidth(420)
	w2 := newWorkspace(r.fb)
	w2.dataDir = r.w.dataDir
	w2.prefs = loadPrefs(w2.dataDir) // what RunWith does
	w2.work, w2.post, w2.mkBody = func(fn func()) { fn() }, func(fn func()) { fn() }, placeholder(&r.released)
	w2.start()
	if !near(w2.prefs.SidebarWidth, 420) || !near(w2.sidebarNow(), 420) {
		t.Errorf("a new window has width %v", w2.prefs.SidebarWidth)
	}
	w2.prefs.SidebarWidth = 10000
	if w2.sidebarNow() != hi {
		t.Errorf("an out of range width draws at %v, want %v", w2.sidebarNow(), hi)
	}
}
