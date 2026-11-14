package native

import (
	"slices"
	"testing"

	"keel/internal/native/layout"
)

// Dragging a pane to the tab strip and a tab onto a pane (panedrag.go: extractPane and dockTab). Nothing opens or closes a session.

// dockRig is a window with tab A of panes 1 and 2 (split right) and tab B of pane 3, B named "second". A is shown.
func dockRig(t *testing.T) (*rig, *tab, *tab) {
	t.Helper()
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.w.splitFocused(layout.Row)
	r.tt.Frame()
	r.click(t, "host api-1")
	a, b := r.w.tabAt(0), r.w.tabAt(1)
	b.title = "second"
	r.w.selectTab(0)
	r.tt.Frame()
	if len(a.panes) != 2 || len(b.panes) != 1 {
		t.Fatalf("tabs have %d and %d panes", len(a.panes), len(b.panes))
	}
	return r, a, b
}

func TestDockingATabMovesItsPanesBesideAPaneAndKeepsSyncIfEitherHadIt(t *testing.T) {
	for _, tc := range []struct{ a, b, want bool }{{false, false, false}, {true, false, true}, {false, true, true}, {true, true, true}} {
		r, a, b := dockRig(t)
		a.sync, b.sync = tc.a, tc.b
		r.w.dockTab(b, "1", "bottom")
		if r.w.tabCount() != 1 || r.w.tabAt(0) != a || len(a.panes) != 3 {
			t.Fatalf("tabs %d, panes %d", r.w.tabCount(), len(a.panes))
		}
		if got := a.root.Leaves(); !slices.Equal(got, []string{"1", "3", "2"}) || a.root.A.Dir != layout.Col {
			t.Errorf("leaves %v, want 3 under 1 and beside 2", got)
		}
		if a.sync != tc.want {
			t.Errorf("sync %v and %v docked, sync is %v, want %v", tc.a, tc.b, a.sync, tc.want)
		}
		if a.focused != "3" || r.w.active != 0 {
			t.Errorf("focused %q, active %d: the focus the docked tab had stays and the tab shows", a.focused, r.w.active)
		}
		if len(r.fb.killed) != 0 || r.released != 0 || r.fb.conns[2].closed {
			t.Errorf("a session was closed: killed %v released %d", r.fb.killed, r.released)
		}
		// A drop on the right edge or the centre docks to the right.
		r2, a2, b2 := dockRig(t)
		r2.w.dockTab(b2, "2", "center")
		if got := a2.root.Leaves(); !slices.Equal(got, []string{"1", "2", "3"}) {
			t.Errorf("centre dock: %v", got)
		}
	}
}

func TestATabThatCannotDockIsLeftAlone(t *testing.T) {
	r, a, b := dockRig(t)
	r.w.dockTab(a, "1", "right") // onto its own pane
	r.w.dockTab(b, "9", "right") // a pane that is nowhere
	b.remote = true
	r.w.dockTab(b, "1", "right") // a joined tab
	b.remote = false
	pg := r.w.addPageTab("logs")
	r.w.dockTab(pg, "1", "right") // a page
	if r.w.tabCount() != 3 || len(a.panes) != 2 || len(b.panes) != 1 {
		t.Fatalf("tabs %d, panes %d and %d", r.w.tabCount(), len(a.panes), len(b.panes))
	}
}

func TestExtractingAPaneMakesItsOwnTabAtTheDropSlot(t *testing.T) {
	r, a, b := dockRig(t)
	a.sync = true
	r.w.extractPane(a, "2", 2) // the end of the strip
	if r.w.tabCount() != 3 || r.w.tabAt(2) == a || r.w.tabAt(1) != b {
		t.Fatalf("tabs %d", r.w.tabCount())
	}
	n := r.w.tabAt(2)
	if len(n.panes) != 1 || n.panes["2"] == nil || n.focused != "2" || r.w.active != 2 || n.sync {
		t.Errorf("new tab: panes %d focused %q active %d sync %v", len(n.panes), n.focused, r.w.active, n.sync)
	}
	if got := a.root.Leaves(); !slices.Equal(got, []string{"1"}) || a.focused != "1" || len(a.panes) != 1 {
		t.Errorf("the tab it left: %v focused %q", got, a.focused)
	}
	if len(r.fb.killed) != 0 || r.released != 0 {
		t.Errorf("a session was closed: killed %v released %d", r.fb.killed, r.released)
	}
	// A tab of one pane has nothing to leave from.
	r.w.extractPane(a, "1", 0)
	r.w.extractPane(b, "3", 0)
	if r.w.tabCount() != 3 {
		t.Errorf("a one-pane tab was split: %d tabs", r.w.tabCount())
	}
	// At the first slot.
	r, a, _ = dockRig(t)
	r.w.extractPane(a, "1", 0)
	if r.w.tabCount() != 3 || r.w.tabAt(0).panes["1"] == nil || r.w.active != 0 {
		t.Errorf("first slot: %d tabs, active %d", r.w.tabCount(), r.w.active)
	}
}

// The gestures: a pane's bar let go at the end of the strip, and a tab let go on the right edge of a pane.
func TestDraggingAPaneBarToTheStripAndATabOntoAPane(t *testing.T) {
	r, a, _ := dockRig(t)
	bar, ok := r.tt.Find("pane bar 2")
	end, ok2 := r.tt.Find("tab strip end")
	if !ok || !ok2 {
		t.Fatalf("pane bar 2 %v, tab strip end %v", ok, ok2)
	}
	bx, by := bar.X+bar.W/2, bar.Y+bar.H/2
	ex, ey := end.X+end.W/2, end.Y+end.H/2
	r.tt.Press(bx, by)
	r.tt.Move(bx+30, by-20)
	r.tt.Move(ex, ey)
	r.tt.Release(ex, ey)
	r.tt.Frame()
	if r.w.tabCount() != 3 || len(a.panes) != 1 || r.w.tabAt(2).panes["2"] == nil {
		t.Fatalf("after the drop: %d tabs, tab A has %d panes", r.w.tabCount(), len(a.panes))
	}
	// Now the last tab onto the right of pane 1.
	last := r.w.tabAt(2)
	last.title = "moved"
	r.w.selectTab(0)
	r.tt.Frame()
	tb, ok := r.tt.Find("tab moved")
	to, ok2 := r.tt.Find("pane 1")
	if !ok || !ok2 {
		t.Fatalf("tab moved %v, pane 1 %v", ok, ok2)
	}
	sx, sy := tb.X+tb.W/2, tb.Y+tb.H/2
	tx, ty := to.X+to.W-4, to.Y+to.H/2
	r.tt.Press(sx, sy)
	r.tt.Move(sx-30, sy+30)
	r.tt.Move(tx, ty)
	r.tt.Release(tx, ty)
	r.tt.Frame()
	if r.w.tabCount() != 2 || len(a.panes) != 2 || a.panes["2"] == nil {
		t.Fatalf("after the dock: %d tabs, tab A has %d panes", r.w.tabCount(), len(a.panes))
	}
	if got := a.root.Leaves(); !slices.Equal(got, []string{"1", "2"}) {
		t.Errorf("leaves %v, want 2 to the right of 1", got)
	}
}
