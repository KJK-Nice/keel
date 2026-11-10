package native

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// splitRig opens api-1 and splits it right: one tab, two panes, the second focused.
func splitRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.w.splitFocused(layout.Row)
	r.tt.Frame()
	if len(r.w.tab().panes) != 2 {
		t.Fatalf("%d panes after a split", len(r.w.tab().panes))
	}
	return r
}

// openTabs opens n tabs on api-1.
func openTabs(t *testing.T, n int) *rig {
	t.Helper()
	r := newRig(t, okBackend())
	r.tt.Frame()
	for i := 0; i < n; i++ {
		r.click(t, "host api-1")
	}
	return r
}

func visible(r *rig, id string) bool { _, ok := r.tt.Find("pane " + id); return ok }

func TestFocusViewByKeyAndByTool(t *testing.T) {
	r := splitRig(t)
	tb := r.w.tab()
	if !visible(r, "1") || !visible(r, "2") {
		t.Fatal("both panes should show before focus view")
	}
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyEnter)
	r.tt.Frame()
	if tb.zoomed != "2" || visible(r, "1") || !visible(r, "2") {
		t.Fatalf("zoomed %q, pane 1 visible %v, pane 2 visible %v", tb.zoomed, visible(r, "1"), visible(r, "2"))
	}
	if len(r.fb.killed) != 0 || r.released != 0 {
		t.Errorf("focus view closed something: killed %v, released %d", r.fb.killed, r.released)
	}
	if !r.tt.HasText("2") { // the badge still counts both panes
		t.Error("no pane count badge")
	}
	if _, ok := r.tt.Find("Exit focus view"); !ok {
		t.Error("the zoom tool does not say Exit focus view while zoomed")
	}
	r.click(t, "Exit focus view")
	if tb.zoomed != "" || !visible(r, "1") || !visible(r, "2") {
		t.Fatalf("the tool did not leave focus view: zoomed %q", tb.zoomed)
	}
	r.click(t, "Focus view")
	if tb.zoomed == "" {
		t.Error("the tool did not enter focus view")
	}
}

func TestFocusViewLeavesOnSplitAndOnClosingTheZoomedPane(t *testing.T) {
	r := splitRig(t)
	tb := r.w.tab()
	r.w.zoomFocused()
	r.w.splitFocused(layout.Col)
	if tb.zoomed != "" || len(tb.panes) != 3 {
		t.Errorf("a split must leave focus view: zoomed %q, panes %d", tb.zoomed, len(tb.panes))
	}
	r.w.zoomFocused()
	r.w.closeFocused()
	if tb.zoomed != "" || len(tb.panes) != 2 {
		t.Errorf("closing the zoomed pane must leave focus view: zoomed %q, panes %d", tb.zoomed, len(tb.panes))
	}
}

func TestFocusViewIgnoresAOnePaneTab(t *testing.T) {
	r := openTabs(t, 1)
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyEnter)
	r.tt.Frame()
	if r.w.tab().zoomed != "" {
		t.Error("a tab of one pane zoomed")
	}
}

func menuLabels(t *testing.T, r *rig, target string) []string {
	t.Helper()
	if err := r.tt.RightClick(target); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	m := r.tt.Menu()
	if m == nil {
		t.Fatalf("no menu after a right-click on %q", target)
	}
	return m
}

// choose picks an item of the open menu and settles the frames.
func choose(t *testing.T, r *rig, label string) {
	t.Helper()
	if err := r.tt.ChooseMenuItem(label); err != nil {
		t.Fatalf("choose %q: %v", label, err)
	}
	r.tt.Frame()
	r.tt.Frame()
}

func TestPaneMenuItems(t *testing.T) {
	// The labels, order and visibility of PaneMenu (internal/ui/rowmenus.templ) for a terminal pane opened from
	// its bar (lib/termmenu.js paneMenuItems): Focus is for a sidebar row, Focus view needs another pane or
	// a zoom, Reconnect a failed session, Share is left out until its phase.
	one := openTabs(t, 1)
	split := splitRig(t)
	zoomed := splitRig(t)
	zoomed.w.zoomFocused()
	zoomed.tt.Frame()
	failed := openTabs(t, 1)
	failed.fb.conns[0].st = &keelclient.State{Status: "error"}
	failed.tt.Frame()
	for _, tc := range []struct {
		name string
		r    *rig
		bar  string
		want []string
	}{
		{"one pane", one, "pane bar 1", []string{"Split right", "Split down", "-", "Close pane"}},
		{"split", split, "pane bar 2", []string{"Split right", "Split down", "Focus view", "-", "Close pane"}},
		{"zoomed", zoomed, "pane bar 2", []string{"Split right", "Split down", "Focus view", "-", "Close pane"}},
		{"failed", failed, "pane bar 1", []string{"Split right", "Split down", "Reconnect", "-", "Close pane"}},
	} {
		if got := menuLabels(t, tc.r, tc.bar); !slices.Equal(got, tc.want) {
			t.Errorf("%s: menu %q, want %q", tc.name, got, tc.want)
		}
		tc.r.tt.CloseMenu()
	}
}

func TestPaneMenuItemsAct(t *testing.T) {
	r := openTabs(t, 1)
	tb := r.w.tab()
	menuLabels(t, r, "pane bar 1")
	choose(t, r, "Split right")
	if len(tb.panes) != 2 || tb.root.Dir != layout.Row {
		t.Fatalf("Split right: %d panes, dir %v", len(tb.panes), tb.root.Dir)
	}
	menuLabels(t, r, "pane bar 2")
	choose(t, r, "Split down")
	if len(tb.panes) != 3 {
		t.Fatalf("Split down: %d panes", len(tb.panes))
	}

	menuLabels(t, r, "pane bar 3")
	choose(t, r, "Focus view")
	if tb.zoomed != "3" || visible(r, "1") {
		t.Fatalf("Focus view: zoomed %q", tb.zoomed)
	}
	if len(r.fb.killed) != 0 {
		t.Errorf("focus view killed %v", r.fb.killed)
	}
	menuLabels(t, r, "pane bar 3")
	choose(t, r, "Focus view")
	if tb.zoomed != "" || !visible(r, "1") {
		t.Fatalf("Focus view again: zoomed %q", tb.zoomed)
	}

	sid := tb.panes["2"].sessionID()
	menuLabels(t, r, "pane bar 2")
	choose(t, r, "Close pane")
	if tb.panes["2"] != nil || !slices.Equal(r.fb.killed, []string{sid}) {
		t.Errorf("Close pane: pane left %v, killed %v, want %s", tb.panes["2"] != nil, r.fb.killed, sid)
	}

	// Reconnect asks for the session of that pane, and the menu offers it for a failed one only.
	f := openTabs(t, 1)
	f.fb.conns[0].st = &keelclient.State{Status: "exited"}
	f.tt.Frame()
	menuLabels(t, f, "pane bar 1")
	choose(t, f, "Reconnect")
	if !slices.Equal(f.fb.reconnected, []string{"s1"}) {
		t.Errorf("reconnected %v", f.fb.reconnected)
	}
}

func TestMoreToolAndRightClickOpenTheSameMenu(t *testing.T) {
	r := splitRig(t)
	if err := r.tt.Click("More pane actions"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	want := []string{"Split right", "Split down", "Focus view", "-", "Close pane"}
	if got := r.tt.Menu(); !slices.Equal(got, want) {
		t.Errorf("More menu %q, want %q", got, want)
	}
	r.tt.CloseMenu()
	if got := menuLabels(t, r, "pane bar 1"); !slices.Equal(got, want) {
		t.Errorf("right-click menu %q, want %q", got, want)
	}
}

// named opens n tabs on api-1 and names them a, b, c ... so a test can point at one.
func named(t *testing.T, n int) *rig {
	t.Helper()
	r := openTabs(t, n)
	for i, tb := range r.w.tabs {
		tb.title = string(rune('a' + i))
	}
	r.tt.Frame()
	return r
}

func tabTitles(r *rig) (out []string) {
	for _, tb := range r.w.tabs {
		out = append(out, tb.name())
	}
	return
}

func TestTabMenuClosesExactlyTheSessionsNamed(t *testing.T) {
	// sessions s1..s4 are tabs a..d
	for _, tc := range []struct {
		name, tab, item string
		left            []string
		killed          []string
	}{
		{"close", "b", "Close", []string{"a", "c", "d"}, []string{"s2"}},
		{"others", "b", "Close others", []string{"b"}, []string{"s1", "s3", "s4"}},
		{"right", "b", "Close to the right", []string{"a", "b"}, []string{"s3", "s4"}},
		{"all", "c", "Close all", []string{"New Tab"}, []string{"s1", "s2", "s3", "s4"}}, // the window keeps a tab
	} {
		r := named(t, 4)
		if got := menuLabels(t, r, "tab "+tc.tab); !slices.Equal(got, []string{"Rename tab", "-", "Close", "Close others", "Close to the right", "Close all"}) {
			t.Fatalf("tab menu %q", got)
		}
		choose(t, r, tc.item)
		if got := tabTitles(r); !slices.Equal(got, tc.left) {
			t.Errorf("%s: tabs left %q, want %q", tc.name, got, tc.left)
		}
		killed := slices.Clone(r.fb.killed)
		slices.Sort(killed)
		if !slices.Equal(killed, tc.killed) {
			t.Errorf("%s: killed %v, want %v", tc.name, killed, tc.killed)
		}
	}
}

func TestTabMenuDisabledItems(t *testing.T) {
	// ui/tabmenu.js: Close others needs another tab, Close to the right a tab to the right.
	for _, tc := range []struct {
		name string
		n    int
		tab  string
		off  []string
		on   []string
	}{
		{"only tab", 1, "a", []string{"Close others", "Close to the right"}, []string{"Rename tab", "Close", "Close all"}},
		{"first", 3, "a", nil, []string{"Close", "Close others", "Close to the right", "Close all"}},
		{"middle", 3, "b", nil, []string{"Close", "Close others", "Close to the right", "Close all"}},
		{"last", 3, "c", []string{"Close to the right"}, []string{"Close", "Close others", "Close all"}},
	} {
		for _, item := range tc.off {
			r := named(t, tc.n)
			menuLabels(t, r, "tab "+tc.tab)
			if err := r.tt.ChooseMenuItem(item); err == nil {
				t.Errorf("%s: %q should be disabled", tc.name, item)
			}
			if len(r.w.tabs) != tc.n || len(r.fb.killed) != 0 {
				t.Errorf("%s: a disabled %q closed something", tc.name, item)
			}
		}
		for _, item := range tc.on {
			r := named(t, tc.n)
			menuLabels(t, r, "tab "+tc.tab)
			if err := r.tt.ChooseMenuItem(item); err != nil {
				t.Errorf("%s: %q should be enabled: %v", tc.name, item, err)
			}
		}
	}
}

func TestTabRename(t *testing.T) {
	r := openTabs(t, 2)
	rename := func() {
		menuLabels(t, r, "tab api-1")
		choose(t, r, "Rename tab")
		if r.w.renaming == nil || !r.tt.Focused("Rename tab") {
			t.Fatal("the rename field is not open and focused")
		}
	}
	// Commit with Enter: the old name is selected, so typing replaces it.
	rename()
	r.tt.Type("logs")
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.renaming != nil || r.w.tabs[0].title != "logs" || !r.tt.HasText("logs") {
		t.Fatalf("Enter: renaming %v, title %q", r.w.renaming != nil, r.w.tabs[0].title)
	}
	// Escape puts the old name back.
	menuLabels(t, r, "tab logs")
	choose(t, r, "Rename tab")
	r.tt.Type("zzz")
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if r.w.renaming != nil || r.w.tabs[0].title != "logs" {
		t.Fatalf("Escape: renaming %v, title %q", r.w.renaming != nil, r.w.tabs[0].title)
	}
	// An empty name clears the custom title: the host name is back.
	menuLabels(t, r, "tab logs")
	choose(t, r, "Rename tab")
	r.tt.Key(0, ui.KeyBackspace)
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.tabs[0].title != "" || r.w.tabs[0].name() != "api-1" {
		t.Fatalf("empty: title %q, name %q", r.w.tabs[0].title, r.w.tabs[0].name())
	}
	// A click elsewhere keeps what was typed.
	rename()
	r.tt.Type("kept")
	r.click(t, "New tab")
	if r.w.renaming != nil || r.w.tabs[0].title != "kept" {
		t.Fatalf("blur: renaming %v, title %q", r.w.renaming != nil, r.w.tabs[0].title)
	}
}

func TestTabKeysWrap(t *testing.T) {
	r := named(t, 3)
	press := func(mods ui.Modifiers, k ui.Key) {
		r.tt.Key(mods, k)
		r.tt.Frame()
	}
	if r.w.active != 2 {
		t.Fatalf("active %d", r.w.active)
	}
	press(ui.Cmd|ui.Shift, ui.KeyBracketRight)
	if r.w.active != 0 {
		t.Errorf("next from the last: %d, want 0", r.w.active)
	}
	press(ui.Cmd|ui.Shift, ui.KeyBracketLeft)
	if r.w.active != 2 {
		t.Errorf("previous from the first: %d, want 2", r.w.active)
	}
	press(ui.Cmd|ui.Shift, ui.KeyComma)
	press(ui.Cmd|ui.Shift, ui.KeyComma)
	if r.w.active != 0 {
		t.Errorf("⌘< twice: %d, want 0", r.w.active)
	}
	press(ui.Cmd|ui.Shift, ui.KeyPeriod)
	if r.w.active != 1 {
		t.Errorf("⌘>: %d, want 1", r.w.active)
	}
}

func TestDraggingATabReordersAndNumberKeysFollow(t *testing.T) {
	r := named(t, 4)
	d, _ := r.tt.Find("tab d")
	a, _ := r.tt.Find("tab a")
	r.tt.Press(d.X+d.W/2, d.Y+d.H/2)
	r.tt.Move(d.X+d.W/2-20, d.Y+d.H/2)
	r.tt.Move(a.X+4, a.Y+a.H/2) // the left half of the first tab
	r.tt.Release(a.X+4, a.Y+a.H/2)
	r.tt.Frame()
	r.tt.Frame()
	if got := tabTitles(r); !slices.Equal(got, []string{"d", "a", "b", "c"}) {
		t.Fatalf("order %q", got)
	}
	if r.w.tab().title != "d" {
		t.Errorf("the active tab changed to %q", r.w.tab().title)
	}
	r.tt.Key(ui.Cmd, ui.Key1)
	r.tt.Frame()
	if r.w.tab().title != "d" {
		t.Errorf("⌘1 shows %q, want d", r.w.tab().title)
	}
	r.tt.Key(ui.Cmd, ui.Key2)
	if r.w.tab().title != "a" {
		t.Errorf("⌘2 shows %q, want a", r.w.tab().title)
	}
	// The right half of a tab puts the dragged one after it.
	a, _ = r.tt.Find("tab a")
	dd, _ := r.tt.Find("tab d")
	r.tt.Press(dd.X+dd.W/2, dd.Y+dd.H/2)
	r.tt.Move(dd.X+dd.W/2+20, dd.Y+dd.H/2)
	r.tt.Move(a.X+a.W-4, a.Y+a.H/2)
	r.tt.Release(a.X+a.W-4, a.Y+a.H/2)
	r.tt.Frame()
	r.tt.Frame()
	if got := tabTitles(r); !slices.Equal(got, []string{"a", "d", "b", "c"}) {
		t.Errorf("order after the second drag %q", got)
	}
}

func TestMoveTabSlots(t *testing.T) {
	for _, tc := range []struct {
		from, slot int
		want       string
	}{
		{3, 0, "dabc"}, {0, 4, "bcda"}, {0, 1, "abcd"}, {0, 2, "bacd"}, {2, 9, "abdc"}, {1, -3, "bacd"},
	} {
		r := named(t, 4)
		r.w.moveTab(r.w.tabs[tc.from], tc.slot)
		if got := strings.Join(tabTitles(r), ""); got != tc.want {
			t.Errorf("move %d to slot %d: %s, want %s", tc.from, tc.slot, got, tc.want)
		}
	}
}

func TestReopenClosedIsANewSessionOnTheSameHost(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "host pg-primary")
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyT)
	r.tt.Frame()
	if len(r.w.tabs) != 2 || !r.tt.HasText("Nothing to reopen") {
		t.Fatalf("nothing is closed yet: %d tabs, err %q", len(r.w.tabs), r.w.err)
	}
	r.tt.Key(ui.Cmd, ui.KeyW) // closes pg-primary, s2
	r.tt.Frame()
	spawned := len(r.fb.spawned)
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyT)
	r.tt.Frame()
	if len(r.fb.spawned) != spawned+1 || r.fb.spawned[spawned] != "h-pg" || len(r.w.tabs) != 2 {
		t.Fatalf("spawned %v, tabs %d", r.fb.spawned, len(r.w.tabs))
	}
	if id := r.w.tab().panes[r.w.tab().focused].sessionID(); id == "s2" || id == "" {
		t.Errorf("reopened session id %q: it must be a new session", id)
	}
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyT) // the entry was used
	r.tt.Frame()
	if len(r.fb.spawned) != spawned+1 {
		t.Error("the same entry reopened twice")
	}
}

func TestClosedListRules(t *testing.T) {
	// lib/newtab.js: newest first, one entry per host and kind, at most 10; a pane is "pane" in a split tab.
	w := newWorkspace(okBackend())
	mk := func(id string) *pane { return &pane{host: keelclient.Host{ID: id, Name: id}} }
	for i := 0; i < 12; i++ {
		w.recordClosed(mk(fmt.Sprint("h", i)), "tab")
	}
	if len(w.recent) != 10 || w.recent[0].hostID != "h11" {
		t.Fatalf("%d entries, newest %q", len(w.recent), w.recent[0].hostID)
	}
	w.recordClosed(mk("h5"), "tab")
	w.recordClosed(mk("h5"), "pane")
	if w.recent[0].kind != "pane" || w.recent[1].kind != "tab" || w.recent[1].hostID != "h5" || len(w.recent) != 10 {
		t.Errorf("replacement: %+v", w.recent[:3])
	}
	w.recordClosed(mk(""), "tab")
	if w.recent[0].hostID == "" {
		t.Error("an entry without a host was kept")
	}

	r := splitRig(t)
	r.w.closeFocused()
	if r.w.recent[0].kind != "pane" {
		t.Errorf("a pane of a split tab is recorded as %q", r.w.recent[0].kind)
	}
	r.w.closeFocused()
	if r.w.recent[0].kind != "tab" {
		t.Errorf("the last pane is recorded as %q", r.w.recent[0].kind)
	}
}

func TestClosingAWindowRemembersNothing(t *testing.T) {
	r := openTabs(t, 1)
	r.w.closeFocused()
	r.w.closeWindow()
	r2 := newRig(t, okBackend())
	if len(r2.w.recent) != 0 {
		t.Errorf("a new window remembers %d closed tabs", len(r2.w.recent))
	}
}
