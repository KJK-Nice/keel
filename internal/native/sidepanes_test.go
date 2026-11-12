package native

import (
	"slices"
	"strings"
	"testing"

	"keel/internal/native/keelclient"
)

// The sidebar's Panes section: shown only while the active tab has two or more panes.

// paneSplitRig is a window with api-1, split right into pg-primary's host twice: three panes, ids 1, 2, 3.
func paneSplitRig(t *testing.T) *rig {
	t.Helper()
	fb := okBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "Split right")
	r.click(t, "Split down")
	return r
}

func TestPanesSegmentExistsOnlyWhileTheTabHasTwoPanesAndFallsBack(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	if r.has("Panes 2") || r.has("Panes 1") {
		t.Fatal("Panes shows for a one-pane tab")
	}
	r.click(t, "Split right")
	r.texts(t, "Hosts", "Panes 2", "Snippets", "Forwards")
	// The segments are in the app's order.
	segs := r.w.panesTab() != nil
	if !segs {
		t.Fatal("no panes tab")
	}
	r.click(t, "Panes 2")
	if !r.w.sidePanes() || !r.has("Filter panes") || r.has("Filter hosts") {
		t.Fatalf("Panes is not showing: %v", r.tt.Texts())
	}
	r.click(t, "Split down")
	r.texts(t, "Panes 3")
	// Another tab without splits: the segment is gone and the sidebar is on Hosts, and stays there.
	r.click(t, "New tab")
	r.w.openHost(testHosts[2])
	r.tt.Frame()
	if r.has("Panes 3") || r.w.sideSeg != "hosts" || !r.has("Filter hosts") {
		t.Fatalf("seg %q texts %v", r.w.sideSeg, r.tt.Texts())
	}
	r.w.selectTab(0)
	r.tt.Frame()
	if r.w.sideSeg != "hosts" || !r.has("Panes 3") {
		t.Errorf("seg %q: coming back to the split tab does not bring Panes back on its own", r.w.sideSeg)
	}
	// Closing down to one pane hides it and falls back.
	r.click(t, "Panes 3")
	r.w.closePane(r.w.tab(), "3")
	r.w.closePane(r.w.tab(), "2")
	r.tt.Frame()
	if r.has("Panes 1") || r.w.sideSeg != "hosts" {
		t.Errorf("seg %q after closing to one pane", r.w.sideSeg)
	}
}

func TestPanesMapMirrorsTheSplitTreeAndMarksTheFocusedCell(t *testing.T) {
	r := paneSplitRig(t)
	r.click(t, "Panes 3")
	t0 := r.w.tab()
	for i, name := range []string{"api-1", "api-1", "api-1"} {
		if _, ok := r.tt.Find("Focus pane " + string(rune('1'+i)) + ", " + name); !ok {
			t.Errorf("no map cell %d", i+1)
		}
	}
	a, _ := r.tt.Find("Focus pane 1, api-1")
	b, _ := r.tt.Find("Focus pane 2, api-1")
	c3, _ := r.tt.Find("Focus pane 3, api-1")
	// api-1 split right, then Split down on the first bar: 1 over 2 on the left, 3 beside them.
	if !(a.X == b.X && a.Y < b.Y && c3.X > a.X && c3.H > a.H) {
		t.Errorf("the map does not follow the split: %v %v %v", a, b, c3)
	}
	// The cell of the focused pane carries the brass line.
	if !r.has("focused pane " + t0.focused) {
		t.Errorf("the focused cell is not marked (focused %q)", t0.focused)
	}
	r.click(t, "Focus pane 1, api-1")
	if t0.focused != "1" || !r.has("focused pane 1") || r.has("focused pane 3") {
		t.Errorf("focused %q", t0.focused)
	}
}

func TestPanesRowsFocusTheirPaneAndLeaveFocusView(t *testing.T) {
	r := paneSplitRig(t)
	r.click(t, "Panes 3")
	r.texts(t, "This tab", "3", "Connected")
	t0 := r.w.tab()
	r.w.toggleZoom(t0, "3")
	r.tt.Frame()
	if t0.zoomed != "3" {
		t.Fatal("not in focus view")
	}
	r.click(t, "pane row api-1") // the first row is pane 1
	if t0.focused != "1" || t0.zoomed != "" {
		t.Fatalf("focused %q zoomed %q: a row must focus its pane and leave focus view", t0.focused, t0.zoomed)
	}
	if !r.tt.Focused("body 1") {
		t.Error("the picked pane's terminal does not have the keyboard")
	}
}

func TestPanesFilterNarrowsRowsAndNeverTheMap(t *testing.T) {
	fb := okBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	r.click(t, "host api-1")
	r.w.openHostInSplit(testHosts[2]) // staging-web beside it
	r.tt.Frame()
	r.click(t, "Panes 2")
	r.fill(t, "Filter panes", "staging")
	if r.has("pane row api-1") || !r.has("pane row staging-web") {
		t.Errorf("the filter keeps the wrong rows: %v", r.tt.Texts())
	}
	if !r.has("Focus pane 1, api-1") || !r.has("Focus pane 2, staging-web") {
		t.Error("the filter narrowed the map")
	}
	r.fill(t, "Filter panes", "zzz")
	r.texts(t, "Nothing matches “zzz”")
	// The sub line is read too, and each section keeps its own filter.
	r.fill(t, "Filter panes", "Connected")
	if !r.has("pane row api-1") || !r.has("pane row staging-web") {
		t.Error("the sub line is not searched")
	}
	r.click(t, "Hosts")
	if r.w.filter != "" {
		t.Errorf("the Hosts filter took the Panes query: %q", r.w.filter)
	}
}

func TestClickingIntoAPaneOfASplitTabShowsPanesWithoutTakingTheKeyboard(t *testing.T) {
	r := paneSplitRig(t)
	if r.w.sideSeg == "panes" {
		t.Fatalf("a split does not show Panes on its own: %q", r.w.sideSeg)
	}
	r.click(t, "body 1")
	if r.w.sideSeg != "panes" {
		t.Fatalf("a click into a pane left the sidebar on %q", r.w.sideSeg)
	}
	if !r.tt.Focused("body 1") {
		t.Error("the keyboard left the terminal")
	}
	// The bar is a click into the pane too.
	r.click(t, "Hosts")
	r.click(t, "pane bar 2")
	if r.w.sideSeg != "panes" || r.w.tab().focused != "2" {
		t.Errorf("seg %q focused %q", r.w.sideSeg, r.w.tab().focused)
	}
	// A one-pane tab never switches.
	r2 := newRig(t, okBackend())
	r2.tt.Frame()
	r2.click(t, "host api-1")
	r2.click(t, "body 1")
	if r2.w.sideSeg == "panes" {
		t.Error("Panes on a one-pane tab")
	}
}

func TestPaletteOpenNowPaneRevealsPanes(t *testing.T) {
	r := paneSplitRig(t)
	r.openPal(t)
	r.typePal("pane in tab 1")
	i := slices.IndexFunc(r.rows(), func(it palItem) bool { return strings.HasPrefix(it.hint, "pane in tab 1") })
	if i < 0 {
		t.Fatal("no pane rows in Open now")
	}
	r.rows()[i].run(false)
	r.w.closePalette()
	r.tt.Frame()
	if r.w.sideSeg != "panes" {
		t.Errorf("a palette pick of a pane left the sidebar on %q", r.w.sideSeg)
	}
}

func TestPaneRowMenuAndMetaIcons(t *testing.T) {
	hidden := false
	fb := okBackend()
	fb.hosts = slices.Clone(testHosts)
	fb.hosts[2].AgentReadable = &hidden // staging-web: the agent may not read it
	r := newRig(t, fb)
	r.tt.Frame()
	r.click(t, "host api-1")
	r.w.openHostInSplit(fb.hosts[2])
	r.tt.Frame()
	r.fb.conns[0].st = &keelclient.State{Status: "ready", Role: "driver", Subtitle: "deploy@10.0.0.1", Attached: []keelclient.Peer{{ID: 1}, {ID: 2}}, YouID: 2, DriverID: 2}
	r.click(t, "Panes 2")
	r.texts(t, "deploy@10.0.0.1")
	if !r.has("You are driving") || !r.has("The agent can read this pane") {
		t.Errorf("api-1 has neither meta icon: %v", r.tt.Texts())
	}
	n := 0
	for _, s := range r.tt.Texts() {
		if s == "The agent can read this pane" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d agent marks, want 1 (staging-web is not readable)", n)
	}
	if err := r.tt.RightClick("pane row api-1"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Equal(got, []string{"Focus", "Split right", "Split down", "Share", "-", "Close pane"}) {
		t.Errorf("row menu %v", got)
	}
	if err := r.tt.ChooseMenuItem("Focus"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.tab().focused != "1" {
		t.Errorf("Focus left %q focused", r.w.tab().focused)
	}
	// The bar's menu keeps Focus view and has no Focus.
	if err := r.tt.RightClick("pane bar 1"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if got := r.tt.Menu(); slices.Contains(got, "Focus") || !slices.Contains(got, "Focus view") {
		t.Errorf("bar menu %v", got)
	}
	r.tt.CloseMenu()
}
