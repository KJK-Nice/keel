package native

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// ---- the model: cases of cmd/keel/web/js/test/palette.test.mjs for matching, ordering, cursor, highlight ----

var palSample = []palItem{
	{group: "Actions", title: "New tab", keys: "⌘T"},
	{group: "Actions", title: "Close pane or tab", keys: "⌘W"},
	{group: "Hosts", title: "New session on prod-db", hint: "root@10.0.0.5"},
}

func titles(items []palItem) (out []string) {
	for _, it := range items {
		out = append(out, it.title)
	}
	return
}

// "every query word must appear somewhere in the item"
func TestPaletteMatchesEveryWord(t *testing.T) {
	if !paletteMatches(palSample[2], "  SESSION 10.0 ") || !paletteMatches(palSample[0], "⌘t") || paletteMatches(palSample[0], "new pane") {
		t.Error("word matching")
	}
	if got := titles(filterPalette(palSample, "tab")); !slices.Equal(got, []string{"New tab", "Close pane or tab"}) {
		t.Errorf("tab: %q", got)
	}
	if n := len(filterPalette(palSample, "")); n != 3 {
		t.Errorf("empty query: %d", n)
	}
}

// "cursor wraps and clamps"
func TestPaletteCursorWrapsAndClamps(t *testing.T) {
	for _, c := range []struct{ got, want int }{
		{moveCursor(0, -1, 3), 2}, {moveCursor(2, 1, 3), 0}, {moveCursor(1, 0, 0), 0},
		{clampCursor(5, 2), 1}, {clampCursor(3, 0), 0},
	} {
		if c.got != c.want {
			t.Errorf("got %d, want %d", c.got, c.want)
		}
	}
}

// "groups print in the order Open now, Hosts, Actions, Snippets"
func TestPaletteGroupOrder(t *testing.T) {
	if !slices.Equal(groupOrder, []string{"Open now", "Hosts", "Actions", "Snippets"}) {
		t.Fatalf("%q", groupOrder)
	}
	mixed := []palItem{
		{group: "Snippets", title: "s1"}, {group: "Actions", title: "a1"}, {group: "Hosts", title: "h1"}, {group: "Open now", title: "o1"},
		{group: "Actions", title: "a2"}, {group: "Hosts", title: "h2"}, {group: "Open now", title: "o2"},
	}
	if got := titles(orderGroups(mixed)); !slices.Equal(got, []string{"o1", "o2", "h1", "h2", "a1", "a2", "s1"}) {
		t.Errorf("%q", got)
	}
	// Filtering keeps the order and drops empty groups.
	named := []palItem{{group: "Hosts", title: "New session on db"}, {group: "Open now", title: "db"}, {group: "Actions", title: "New tab"}}
	if got := titles(filterPalette(named, "db")); !slices.Equal(got, []string{"db", "New session on db"}) {
		t.Errorf("%q", got)
	}
}

// "unknown groups follow the known ones in order of appearance"
func TestPaletteUnknownGroupsFollow(t *testing.T) {
	got := titles(orderGroups([]palItem{{group: "Zed", title: "x"}, {group: "Snippets", title: "s"}, {group: "Alpha", title: "y"}, {group: "Open now", title: "o"}}))
	if !slices.Equal(got, []string{"o", "s", "x", "y"}) {
		t.Errorf("%q", got)
	}
}

// "highlightHTML marks every query word, merges overlaps": the runs a title is drawn in
func TestPaletteHighlight(t *testing.T) {
	str := func(sp []span) string {
		var b strings.Builder
		for _, s := range sp {
			if s.mark {
				b.WriteString("[" + s.text + "]")
			} else {
				b.WriteString(s.text)
			}
		}
		return b.String()
	}
	for _, c := range []struct{ text, q, want string }{
		{"prod-db-01", "prod", "[prod]-db-01"},
		{"prod-db-01", "PROD 01", "[prod]-db-[01]"},
		{"abcd", "ab bc", "[abc]d"},
		{"<i>x</i>", "i", "<[i]>x</[i]>"},
		{"plain", "", "plain"},
		{"plain", "zzz", "plain"},
	} {
		if got := str(highlight(c.text, c.q)); got != c.want {
			t.Errorf("highlight(%q, %q) = %q, want %q", c.text, c.q, got, c.want)
		}
	}
}

// ---- the palette in a window ----

// palRig is two tabs (the first split) and three hosts.
func palRig(t *testing.T) *rig {
	fb := &fakeBackend{hosts: []keelclient.Host{
		{ID: "h-api", Name: "api-1", Group: "production", Address: "10.0.0.1", Username: "deploy", AuthType: "key"},
		{ID: "h-web", Name: "staging-web", Group: "staging", Address: "10.1.0.1", Username: "ci", AuthType: "password", Port: 2222},
		{ID: "h-local", Name: "Local Shell", Address: "localhost", Username: "me", AuthType: "local"},
	}}
	r := newRig(t, fb)
	r.tt.Frame()
	r.click(t, "host api-1")
	r.click(t, "Split right")
	r.click(t, "host staging-web")
	if len(r.w.tabs) != 2 || len(r.w.tabs[0].panes) != 2 {
		t.Fatalf("tabs %d", len(r.w.tabs))
	}
	return r
}

func (r *rig) press(mods ui.Modifiers, k ui.Key) {
	r.tt.Key(mods, k)
	r.tt.Frame()
}

func (r *rig) openPal(t *testing.T) {
	t.Helper()
	r.press(ui.Cmd, ui.KeyK)
	if r.w.pal == nil || !r.tt.Focused("Command palette") {
		t.Fatal("⌘K did not open the palette with the keyboard in its input")
	}
}

func (r *rig) rows() []palItem { return filterPalette(r.w.pal.items, r.w.pal.query) }

func (r *rig) typePal(s string) {
	r.tt.Type(s)
	r.tt.Frame()
}

func TestPaletteGroupsAndOrder(t *testing.T) {
	r := palRig(t)
	r.openPal(t)
	var seen []string
	for _, it := range r.rows() {
		if len(seen) == 0 || seen[len(seen)-1] != it.group {
			seen = append(seen, it.group)
		}
	}
	if !slices.Equal(seen, []string{"Open now", "Hosts", "Actions"}) {
		t.Fatalf("groups %q", seen)
	}
	var open, hosts []string
	for _, it := range r.rows() {
		switch it.group {
		case "Open now":
			open = append(open, it.title+" | "+it.hint)
		case "Hosts":
			hosts = append(hosts, it.title+" | "+it.hint)
		}
	}
	wantOpen := []string{
		"api-1 | tab 1 · 2 panes · following", "api-1 | pane in tab 1", "api-1 | pane in tab 1", "staging-web | tab 2 · following",
	}
	if !slices.Equal(open, wantOpen) {
		t.Errorf("Open now %q, want %q", open, wantOpen)
	}
	wantHosts := []string{"New session on api-1 | deploy@10.0.0.1", "New session on staging-web | ci@10.1.0.1:2222", "New session on Local Shell | this machine"}
	if !slices.Equal(hosts, wantHosts) {
		t.Errorf("Hosts %q, want %q", hosts, wantHosts)
	}
	for _, s := range []string{"Open now", "Hosts", "Actions", "Split right", "No matching"} {
		if s == "No matching" {
			continue
		}
		if !r.tt.HasText(s) {
			t.Errorf("the palette does not show %q", s)
		}
	}
	if r.tt.HasText("Ask the agent") { // the Snippets group is absent too, with no snippets: seen has no such group
		t.Error("the agent row is shown before its phase")
	}
}

func TestPaletteFilters(t *testing.T) {
	r := palRig(t)
	r.openPal(t)
	r.typePal("staging")
	if got := titles(r.rows()); !slices.Equal(got, []string{"staging-web", "New session on staging-web"}) {
		t.Fatalf("rows %q", got)
	}
	r.typePal("zzz")
	if len(r.rows()) != 0 || !r.tt.HasText("No matching commands") {
		t.Error("no empty message")
	}
}

func TestPaletteOpenNowFocusesAndStartsNothing(t *testing.T) {
	r := palRig(t)
	spawned := len(r.fb.spawned)
	r.press(ui.Cmd, ui.Key1) // tab 1 is showing
	r.openPal(t)
	r.typePal("staging")
	r.press(0, ui.KeyEnter)
	if r.w.pal != nil || r.w.active != 1 || len(r.fb.spawned) != spawned {
		t.Fatalf("pal open %v, active %d, spawned %d (was %d)", r.w.pal != nil, r.w.active, len(r.fb.spawned), spawned)
	}
	// A pane of a split tab: its tab shows and the pane has the keyboard.
	r.openPal(t)
	r.typePal("pane in tab 1")
	if got := r.rows(); len(got) != 3 || got[0].hint != "tab 1 · 2 panes · following" {
		t.Fatalf("rows %v", titles(got))
	}
	r.press(0, ui.KeyDown) // the tab's row, then its two panes
	r.press(0, ui.KeyDown)
	r.press(0, ui.KeyEnter)
	t1 := r.w.tabs[0]
	leaves := t1.root.Leaves()
	if r.w.active != 0 || t1.focused != leaves[1] || len(r.fb.spawned) != spawned {
		t.Fatalf("active %d, focused %q (leaves %v), spawned %d", r.w.active, t1.focused, leaves, len(r.fb.spawned))
	}
	if !r.tt.Focused("body " + leaves[1]) {
		t.Error("the chosen pane does not have the keyboard")
	}
	for _, c := range r.fb.conns {
		if c.wrote != 0 {
			t.Error("choosing a row wrote to a session")
		}
	}
}

func TestPaletteHostOpensInANewTabAndAltEnterInASplit(t *testing.T) {
	r := palRig(t)
	r.press(ui.Cmd, ui.Key1)
	r.openPal(t)
	r.typePal("session on staging")
	r.press(0, ui.KeyEnter)
	if r.fb.spawned[len(r.fb.spawned)-1] != "h-web" || len(r.w.tabs) != 3 || r.w.active != 2 {
		t.Fatalf("spawned %v tabs %d active %d", r.fb.spawned, len(r.w.tabs), r.w.active)
	}
	// ⌥↵ puts the host in a split of the focused pane (here the staging tab), not in the host of that pane.
	spawned := len(r.fb.spawned)
	r.openPal(t)
	r.typePal("session on api")
	r.press(ui.Alt, ui.KeyEnter)
	if len(r.fb.spawned) != spawned+1 || r.fb.spawned[spawned] != "h-api" || len(r.w.tabs) != 3 || len(r.w.tabs[2].panes) != 2 {
		t.Fatalf("spawned %v tabs %d panes %d", r.fb.spawned, len(r.w.tabs), len(r.w.tabs[2].panes))
	}
	var hosts []string
	for _, p := range r.w.tabs[2].panes {
		hosts = append(hosts, p.host.ID)
	}
	slices.Sort(hosts)
	if !slices.Equal(hosts, []string{"h-api", "h-web"}) {
		t.Errorf("the tab's panes are on %v", hosts)
	}
}

func TestPaletteRunsAnAction(t *testing.T) {
	r := palRig(t)
	spawned := len(r.fb.spawned)
	r.openPal(t)
	r.typePal("Split down")
	if got := titles(r.rows()); !slices.Equal(got, []string{"Split down"}) {
		t.Fatalf("rows %q", got)
	}
	r.press(0, ui.KeyEnter)
	if len(r.fb.spawned) != spawned+1 || len(r.w.tab().panes) != 2 {
		t.Fatalf("spawned %d (was %d), panes %d", len(r.fb.spawned), spawned, len(r.w.tab().panes))
	}
	r.openPal(t)
	r.typePal("Toggle vault sidebar")
	r.press(0, ui.KeyEnter)
	if r.w.sidebar {
		t.Error("the sidebar is still shown")
	}
}

func TestPaletteEscapeReturnsTheKeyboardToThePane(t *testing.T) {
	r := palRig(t)
	focused := r.w.tab().focused
	r.openPal(t)
	r.press(0, ui.KeyEscape)
	if r.w.pal != nil || !r.tt.Focused("body "+focused) {
		t.Fatalf("pal open %v, focus on body %s: %v", r.w.pal != nil, focused, r.tt.Focused("body "+focused))
	}
	// ⌘K opens it and ⌘K closes it; ⌘⇧P opens it too.
	r.openPal(t)
	r.press(ui.Cmd, ui.KeyK)
	if r.w.pal != nil || !r.tt.Focused("body "+focused) {
		t.Fatalf("⌘K did not close the palette and return the keyboard: open %v, focused %v", r.w.pal != nil, r.tt.Focused("body "+focused))
	}
	r.press(ui.Cmd|ui.Shift, ui.KeyP)
	if r.w.pal == nil {
		t.Fatal("⌘⇧P did not open the palette")
	}
}

func TestPaletteNeverTypesIntoASession(t *testing.T) {
	r := palRig(t)
	r.openPal(t)
	r.typePal("echo hello; rm -rf /")
	for _, k := range []ui.Key{ui.KeyDown, ui.KeyUp, ui.KeyBackspace, ui.KeyTab} {
		r.press(0, k)
	}
	r.press(ui.Cmd, ui.KeyW) // a window key does not act while the palette is open
	r.press(ui.Cmd|ui.Shift, ui.KeyD)
	r.press(ui.Cmd, ui.KeyEnter) // the agent row is a later phase: no plain ↵ comes of it
	if r.w.pal == nil || len(r.w.tabs) != 2 || len(r.w.tabs[0].panes) != 2 {
		t.Fatalf("a key outside the palette acted: pal %v, tabs %d", r.w.pal != nil, len(r.w.tabs))
	}
	r.press(0, ui.KeyEscape)
	for i, c := range r.fb.conns {
		if c.wrote != 0 {
			t.Errorf("session %d was written %d bytes", i, c.wrote)
		}
	}
}

func TestPaletteCursorRowStaysInView(t *testing.T) {
	r := palRig(t)
	r.openPal(t)
	r.press(0, ui.KeyUp) // wraps to the last row, far below the first screenful
	rows := r.rows()
	last := rows[len(rows)-1]
	list, ok1 := r.tt.Find("palette list")
	row, ok2 := r.tt.Find("palette row " + last.title)
	if !ok1 || !ok2 || r.w.pal.cursor != len(rows)-1 {
		t.Fatalf("list %v row %v cursor %d of %d", ok1, ok2, r.w.pal.cursor, len(rows))
	}
	if row.Y < list.Y || row.Y+row.H > list.Y+list.H+1 {
		t.Errorf("the last row %v is outside the list %v", row, list)
	}
}
