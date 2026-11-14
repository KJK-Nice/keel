package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// newRigIn is newRig with a data dir for the saved workspace.
func newRigIn(t *testing.T, fb *fakeBackend, dir string) *rig {
	t.Helper()
	r := &rig{fb: fb}
	r.w = newWorkspace(fb)
	r.w.dataDir, r.w.prefs = dir, loadPrefs(dir) // what RunWith does
	r.w.work = func(fn func()) { fn() }
	r.w.post = func(fn func()) { fn() }
	r.w.mkBody = placeholder(&r.released)
	t.Cleanup(r.w.saved.stop)
	r.w.start()
	r.tt = ui.NewTester(r.w.view, 1280, 720)
	r.tt.Frame()
	return r
}

// shape writes a tab's tree with the session id of each leaf and the ratio of each split.
func shape(t *tab) string {
	var f func(n *layout.Node) string
	f = func(n *layout.Node) string {
		if n.A == nil {
			return t.panes[n.Pane].sessionID()
		}
		d := "R"
		if n.Dir == layout.Col {
			d = "C"
		}
		return fmt.Sprintf("%s%.2f(%s %s)", d, n.Ratio, f(n.A), f(n.B))
	}
	return f(t.root)
}

func sessionsOf(fb *fakeBackend) (out []keelclient.Session) {
	for _, c := range fb.conns {
		out = append(out, keelclient.Session{ID: c.id, HostID: "h-api"})
	}
	return
}

func TestWorkspaceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := newRigIn(t, okBackend(), dir)
	r.click(t, "host api-1")
	r.w.splitFocused(layout.Row) // s2 beside s1
	r.w.tab().focused = "1"
	r.w.splitFocused(layout.Col) // s3 under s1
	r.tt.Frame()
	r.w.tab().root.Ratio = 0.30
	r.w.tab().root.A.Ratio = 0.65
	r.click(t, "host api-1") // tab 2: s4
	r.w.tabs[1].title = "logs"
	r.w.selectTab(0)
	r.w.tabs[0].focused, r.w.wantFocus = "1", "1"
	r.tt.Frame()
	want := []string{shape(r.w.tabs[0]), shape(r.w.tabs[1])}
	if want[0] != "R0.30(C0.65(s1 s3) s2)" {
		t.Fatalf("the layout under test is %s", want[0])
	}
	r.w.closeWindow()

	// A new window on the same daemon (the sessions are still there) and the same data dir.
	fb := &fakeBackend{hosts: testHosts, sessions: sessionsOf(r.fb)}
	slices.Reverse(fb.sessions) // the order the server lists them in does not matter
	r2 := newRigIn(t, fb, dir)
	if len(r2.w.tabs) != 2 {
		t.Fatalf("%d tabs came back", len(r2.w.tabs))
	}
	got := []string{shape(r2.w.tabs[0]), shape(r2.w.tabs[1])}
	if !slices.Equal(got, want) {
		t.Errorf("tabs %v, want %v", got, want)
	}
	if r2.w.tabs[1].title != "logs" || r2.w.tabs[0].title != "" || r2.w.active != 0 {
		t.Errorf("titles %q %q, active %d", r2.w.tabs[0].title, r2.w.tabs[1].title, r2.w.active)
	}
	if f := r2.w.tabs[0].panes[r2.w.tabs[0].focused].sessionID(); f != "s1" {
		t.Errorf("focused session %s, want s1", f)
	}
	if !r2.tt.HasText("logs") {
		t.Error("the tab is not titled logs")
	}
	if len(fb.attached) != 4 || len(fb.killed) != 0 {
		t.Errorf("attached %v, killed %v", fb.attached, fb.killed)
	}
}

func TestWorkspaceRestoresZoomAndTheActiveTab(t *testing.T) {
	dir := t.TempDir()
	r := newRigIn(t, okBackend(), dir)
	r.click(t, "host api-1")
	r.w.splitFocused(layout.Row)
	r.w.zoomFocused()
	r.tt.Frame() // the section switch is taller, so the sidebar settles before the row is clicked
	r.click(t, "host api-1")
	r.w.closeWindow()

	r2 := newRigIn(t, &fakeBackend{hosts: testHosts, sessions: sessionsOf(r.fb)}, dir)
	if r2.w.active != 1 {
		t.Errorf("active %d, want 1", r2.w.active)
	}
	r2.w.selectTab(0)
	r2.tt.Frame()
	z := r2.w.tabs[0]
	if z.zoomed == "" || z.panes[z.zoomed].sessionID() != "s2" || visible(r2, "1") {
		t.Errorf("zoomed %q", z.zoomed)
	}
}

func writeDoc(t *testing.T, dir string, body any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, savedFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func leaf(s string) *savedNode { return &savedNode{Session: s} }

func TestRestorePrunesAndAppendsUnknownSessions(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, savedDoc{V: 1, Active: 1, Tabs: []savedTab{
		{Layout: &savedNode{Dir: "row", Ratio: 0.4, A: leaf("s1"), B: &savedNode{Dir: "col", Ratio: 0.7, A: leaf("gone"), B: leaf("s3")}}, Focused: "gone"},
		{Layout: leaf("gone2"), Title: "dead"},
		{Title: "x", Layout: &savedNode{Dir: "row", A: leaf("s2"), B: leaf("s1")}}, // s1 is already placed
	}})
	fb := &fakeBackend{hosts: testHosts, sessions: []keelclient.Session{{ID: "s1", HostID: "h-api"}, {ID: "s2", HostID: "h-api"}, {ID: "s3", HostID: "h-api"}, {ID: "s9", HostID: "h-api"}}}
	r := newRigIn(t, fb, dir)
	var got []string
	for _, tb := range r.w.tabs {
		got = append(got, shape(tb))
	}
	// The tree closes up where "gone" was (the sibling takes the parent's place); the tab of only a dead
	// session is dropped; the third tab keeps s2 alone; s9 is in no tab, so it gets its own, last.
	want := []string{"R0.40(s1 s3)", "s2", "s9"}
	if !slices.Equal(got, want) {
		t.Fatalf("tabs %v, want %v", got, want)
	}
	if r.w.tabs[1].title != "x" || r.w.tabs[0].title != "" {
		t.Errorf("titles %q %q", r.w.tabs[0].title, r.w.tabs[1].title)
	}
	// The saved active tab (index 1) was the dead one: the last tab shows, as without a file.
	if r.w.active != 2 {
		t.Errorf("active %d", r.w.active)
	}
	if f := r.w.tabs[0].panes[r.w.tabs[0].focused].sessionID(); f != "s1" {
		t.Errorf("a focus on a session that is gone must fall back to the first pane: %s", f)
	}
}

func TestBadOrOtherVersionFilesAreIgnored(t *testing.T) {
	for name, body := range map[string]string{
		"bad json":      "{",
		"wrong version": `{"v":2,"active":0,"tabs":[{"layout":{"sessionId":"s2"}}]}`,
		"no version":    `{"tabs":[{"layout":{"sessionId":"s2"}}]}`,
		"empty":         ``,
		"deep":          `{"v":1,"tabs":[{"layout":` + strings.Repeat(`{"a":`, 5000) + `{"sessionId":"s1"}` + strings.Repeat(`}`, 5000) + `}]}`,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, savedFile), []byte(body), 0o600)
		fb := &fakeBackend{hosts: testHosts, sessions: []keelclient.Session{{ID: "s1", HostID: "h-api"}, {ID: "s2", HostID: "h-api"}}}
		r := newRigIn(t, fb, dir)
		var got []string
		for _, tb := range r.w.tabs {
			got = append(got, shape(tb))
		}
		if !slices.Equal(got, []string{"s1", "s2"}) || r.w.active != 1 {
			t.Errorf("%s: tabs %v, active %d: want today's one tab per session", name, got, r.w.active)
		}
	}
	// Without a data dir nothing is read.
	if loadSaved("") != nil {
		t.Error("loadSaved read something without a data dir")
	}
}

func TestSavedFileHoldsOnlyIdsTitlesAndLayout(t *testing.T) {
	dir := t.TempDir()
	hosts := []keelclient.Host{{ID: "h-secret", Name: "prod-db", Address: "10.9.8.7", Username: "root-user", AuthType: "password", Group: "secret-group"}}
	r := newRigIn(t, &fakeBackend{hosts: hosts}, dir)
	r.click(t, "host prod-db")
	r.w.splitFocused(layout.Row)
	r.w.tabs[0].title = "my tab"
	r.tt.Frame()
	r.w.closeWindow()

	path := filepath.Join(dir, savedFile)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v, %v", fi, err)
	}
	b, _ := os.ReadFile(path)
	for _, s := range []string{"prod-db", "10.9.8.7", "root-user", "h-secret", "secret-group", "password"} {
		if strings.Contains(string(b), s) {
			t.Errorf("the saved file contains %q", s)
		}
	}
	var keys []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	walk(doc)
	slices.Sort(keys)
	allowed := []string{"a", "active", "b", "dir", "focusedSessionId", "layout", "ratio", "sessionId", "tabs", "title", "v", "zoomedSessionId"}
	for _, k := range keys {
		if !slices.Contains(allowed, k) {
			t.Errorf("unexpected key %q in the saved workspace", k)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
}

func TestSavingFollowsChangesAndWaitsForTheStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, savedFile)
	r := newRigIn(t, okBackend(), dir)
	r.click(t, "host api-1")
	r.w.splitFocused(layout.Row)
	r.tt.Frame()
	if err := r.w.flush(); err != nil {
		t.Fatal(err)
	}
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	first := read()
	if !strings.Contains(first, `"ratio":0.5`) || !strings.Contains(first, `"s2"`) {
		t.Fatalf("saved %s", first)
	}
	// A ratio, a title, a focus and the order are changes too.
	r.w.tab().root.Ratio = 0.25
	r.tt.Frame()
	r.w.flush()
	if got := read(); !strings.Contains(got, `"ratio":0.25`) {
		t.Errorf("a dragged ratio was not saved: %s", got)
	}
	r.w.tab().title = "renamed"
	r.tt.Frame()
	r.w.flush()
	if got := read(); !strings.Contains(got, `"title":"renamed"`) {
		t.Errorf("a title was not saved: %s", got)
	}

	// A locked vault never restored anything: closing the window must not write an empty workspace over it.
	locked := t.TempDir()
	writeDoc(t, locked, savedDoc{V: 1, Tabs: []savedTab{{Layout: leaf("s1")}}})
	before, _ := os.ReadFile(filepath.Join(locked, savedFile))
	l := newRigIn(t, &fakeBackend{vault: keelclient.Vault{Locked: true}}, locked)
	l.tt.Frame()
	l.w.closeWindow()
	if after, _ := os.ReadFile(filepath.Join(locked, savedFile)); string(after) != string(before) {
		t.Errorf("a locked window rewrote the file: %s", after)
	}
}

// testdata/saved-workspace.json is a file users have on disk (one tab of every page kind): it comes back whole and is
// written again byte for byte. Do not regenerate it to make this pass.
func TestSavedWorkspaceFixtureComesBackAndSavesTheSame(t *testing.T) {
	want, err := os.ReadFile("testdata/saved-workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, savedFile), want, 0o600); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBackend{hosts: testHosts, sessions: []keelclient.Session{{ID: "s1", HostID: "h-api"}, {ID: "s2", HostID: "h-api"}}}
	r := newRigIn(t, fb, dir)
	var names []string
	for _, tb := range r.w.tabs {
		if tb.isPage() {
			names = append(names, tb.pg.name)
		} else {
			names = append(names, tb.title+" "+shape(tb))
		}
	}
	wantTabs := []string{"deploy R0.50(s1 s2)", "logs", "keychain", "settings", "forwards", "notebooks", "snippets", "agent", "runbooks", "group", "grouprun"}
	if !slices.Equal(names, wantTabs) {
		t.Fatalf("tabs %q, want %q", names, wantTabs)
	}
	if r.w.tab() != r.w.pageTab("snippets") {
		t.Errorf("active tab %d, want the Snippets page", r.w.active)
	}
	if g, run := r.w.groupPageState(), r.w.groupRunPageState(); g.id != "g-fixture" || g.notes.groupID != "g-fixture" || run.id != "run-fixture" {
		t.Errorf("group %q (notes %q), run %q", g.id, g.notes.groupID, run.id)
	}
	r.w.closeWindow()
	got, _ := os.ReadFile(filepath.Join(dir, savedFile))
	if string(got)+"\n" != string(want) {
		t.Errorf("saved again as\n%s\nwant\n%s", got, want)
	}
}
