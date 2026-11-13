package native

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// Sync input (syncinput.go) on the fake backend. The terminal view's own write is (*syncConn).Write, so the tests call it with
// the bytes the terminal would write for the user's keys; the fakes record every Write.

// syncRig is a window with n panes in one tab, every one driven by this client; pane 1 has the keyboard. Sync is off.
func syncRig(t *testing.T, n int) *sugRig {
	t.Helper()
	r := newSugRig(t)
	for i := 1; i < n; i++ {
		r.w.splitFocused(layout.Row)
		r.tt.Frame()
	}
	for _, c := range r.fb.conns {
		st := drivingState
		c.st = &st
	}
	r.w.tab().focused = r.p.id
	r.w.wantFocus = r.p.id
	r.tt.Frame()
	return r
}

// typeKeys is what the terminal does for the user's keys or paste in pane p.
func typeKeys(p *pane, s string) {
	if n, err := newSyncConn(p).Write([]byte(s)); err != nil || n != len(s) {
		panic("own write failed")
	}
}

func (r *sugRig) written() (out []string) {
	for _, c := range r.fb.conns {
		out = append(out, c.written())
	}
	return out
}

func (r *sugRig) syncOn(t *testing.T) {
	t.Helper()
	r.w.toggleSync(r.w.tab())
	r.tt.Frame()
	if !r.w.tab().sync {
		t.Fatal("sync did not turn on")
	}
}

func TestSyncOffKeysGoToTheFocusedPaneOnly(t *testing.T) {
	r := syncRig(t, 3)
	typeKeys(r.p, "ls\r")
	if got := r.written(); !slices.Equal(got, []string{"ls\r", "", ""}) {
		t.Fatalf("writes %q", got)
	}
}

func TestSyncOnThreePanesGetTheSameBytesInOrderOnce(t *testing.T) {
	r := syncRig(t, 3)
	r.syncOn(t)
	for _, k := range []string{"e", "c", "ho hi", "\r"} {
		typeKeys(r.p, k)
	}
	for i, c := range r.fb.conns {
		if len(c.writes) != 4 || c.written() != "echo hi\r" {
			t.Errorf("pane %d got %q in %d writes, want echo hi\\r in 4 writes, once each", i+1, c.writes, len(c.writes))
		}
	}
	// Typing in another pane of the tab fans out from there.
	other := r.w.tab().panes["2"]
	typeKeys(other, "x")
	if got := r.written(); !slices.Equal(got, []string{"echo hi\rx", "echo hi\rx", "echo hi\rx"}) {
		t.Fatalf("writes %q", got)
	}
	// Sync off again: back to the pane alone.
	r.w.toggleSync(r.w.tab())
	r.tt.Frame()
	typeKeys(r.p, "y")
	if got := r.fb.conns[1].written(); got != "echo hi\rx" {
		t.Errorf("a pane got %q after sync went off", got)
	}
}

func TestSyncSendsToDrivenReadyTerminalPanesOfTheTabAndNothingElse(t *testing.T) {
	r := syncRig(t, 4)
	// pane 2 is followed, pane 3 is not ready; pane 4 stays good.
	fol, conn := drivingState, drivingState
	fol.Role = "follower"
	conn.Status = "connecting"
	*r.fb.conns[1].st, *r.fb.conns[2].st = fol, conn
	// A notebook pane of the tab.
	tb := r.w.tab()
	nb := &pane{id: "nb", nb: &notebookPane{}, conn: &fakeConn{}}
	tb.panes["nb"] = nb
	tb.root = tb.root.Split("1", "nb", layout.Row)
	// A pane of another tab.
	other := &fakeConn{}
	st := drivingState
	other.st = &st
	op := &pane{id: "o", conn: other}
	r.w.tabs = append(r.w.tabs, &tab{root: layout.Leaf("o"), panes: map[string]*pane{"o": op}, focused: "o"})
	tb.sync = true
	r.w.publishSync()
	typeKeys(r.p, "q")
	if got := r.written(); !slices.Equal(got, []string{"q", "", "", "q"}) {
		t.Fatalf("writes %q: only the driven, ready terminal panes get the keys", got)
	}
	if nb.conn.(*fakeConn).wrote != 0 || other.wrote != 0 {
		t.Error("a notebook pane or a pane of another tab was written to")
	}
}

func TestAJoinedTabAndATabWithNoTerminalCannotTurnSyncOn(t *testing.T) {
	r := syncRig(t, 2)
	tb := r.w.tab()
	tb.remote = true
	r.w.toggleSync(tb)
	if tb.sync {
		t.Error("a joined tab turned sync on")
	}
	tb.remote = false
	nbOnly := &tab{root: layout.Leaf("nb"), panes: map[string]*pane{"nb": {id: "nb", nb: &notebookPane{}, conn: &fakeConn{}}}, focused: "nb"}
	r.w.toggleSync(nbOnly)
	if nbOnly.sync {
		t.Error("a tab of notebooks turned sync on")
	}
	if nbOnly.canSync() || (&tab{page: &startPage{}}).canSync() {
		t.Error("canSync is true where there is nothing to sync")
	}
	// Not offered in the palette either; offered in a session tab.
	has := func() bool {
		for _, it := range r.w.paletteItems() {
			if it.title == "Toggle sync input" {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Error("the palette does not offer Toggle sync input in a session tab")
	}
	tb.remote = true
	if has() {
		t.Error("the palette offers Toggle sync input in a joined tab")
	}
	// A tab that was syncing when it became joined sends nothing to others.
	tb.sync = true
	r.w.publishSync()
	typeKeys(r.p, "z")
	if got := r.written(); !slices.Equal(got, []string{"z", ""}) {
		t.Fatalf("writes %q", got)
	}
}

func TestPasteFansOutAsKeysDo(t *testing.T) {
	r := syncRig(t, 2)
	r.syncOn(t)
	paste := "line one\nline two\x1b[201~ é"
	typeKeys(r.p, paste)
	if got := r.written(); !slices.Equal(got, []string{paste, paste}) {
		t.Fatalf("writes %q", got)
	}
}

func TestOnePanesFailingWriteStopsNeitherTheOthersNorTheUsersOwn(t *testing.T) {
	r := syncRig(t, 3)
	r.syncOn(t)
	r.fb.conns[1].writeErr = errors.New("broken pipe")
	typeKeys(r.p, "a") // typeKeys panics if the user's own write fails
	if got := r.written(); !slices.Equal(got, []string{"a", "a", "a"}) {
		t.Fatalf("writes %q: a failed write is not retried and does not stop the third", got)
	}
	// The user's own failure is reported as before, and the others still get the bytes.
	r.fb.conns[1].writeErr = nil
	r.fb.conns[0].writeErr = errors.New("closed")
	if _, err := newSyncConn(r.p).Write([]byte("b")); err == nil {
		t.Error("the own write's error was swallowed")
	}
	if r.fb.conns[2].written() != "ab" {
		t.Errorf("the third pane got %q", r.fb.conns[2].written())
	}
}

// What Keel types for the user goes to one pane only, with sync on: nothing but the terminal's own write fans out.
func TestWhatKeelTypesGoesToOnePaneEvenWithSyncOn(t *testing.T) {
	r := syncRig(t, 3)
	r.syncOn(t)
	r.p.screen = func() (bool, string) { return false, "user@h $" }
	r.fb.conns[0].directRes = keelclient.DirectResult{Typed: true}
	r.fb.snippets = []keelclient.Snippet{{ID: "sn-1", Name: "disk usage", Command: "df -h\n"}}
	others := func(what string) {
		t.Helper()
		for i, c := range r.fb.conns[1:] {
			if c.wrote != 0 || len(c.injected) != 0 || len(c.directs) != 0 || len(c.sugInserts) != 0 || len(c.restored) != 0 || len(c.detects) != 0 {
				t.Errorf("%s reached pane %d: wrote %d injected %v directs %v inserts %v restored %v", what, i+2, c.wrote, c.injected, c.directs, c.sugInserts, c.restored)
			}
		}
		if r.fb.conns[0].wrote != 0 {
			t.Errorf("%s wrote keystrokes to the pane", what)
		}
	}
	// A snippet's direct type.
	r.w.typeSnippet("sn-1")
	r.tt.Frame()
	if got := r.fb.conns[0].directs; len(got) != 1 || got[0].command != "df -h" {
		t.Fatalf("directs %v", got)
	}
	others("a direct type")
	// An approved inject from the dialog.
	r.w.askType(fixed("echo hi"))
	r.tt.Frame()
	r.click(t, "Type")
	if got := r.fb.conns[0].injected; len(got) != 1 || got[0] != "\x15echo hi\n" {
		t.Fatalf("injected %q", got)
	}
	others("the dialog's Type")
	// A suggestion insert and the ⌘Z put-back.
	r.line = "sh-3.2$ # show the files here"
	r.carded(t, card(ro("find . -type f")))
	if !r.key(0, ui.KeyTab) {
		t.Fatal("⇥ not taken on the card")
	}
	if got := r.fb.conns[0].sugInserts; len(got) != 1 {
		t.Fatalf("inserts %v", got)
	}
	others("a suggestion insert")
	r.line = "sh-3.2$ find . -type f"
	r.tt.Advance(sugReadEvery + time.Millisecond)
	r.tt.Frame()
	if !r.key(ui.Cmd, ui.KeyZ) {
		t.Fatal("⌘Z not taken")
	}
	if got := r.fb.conns[0].restored; len(got) != 1 {
		t.Fatalf("restored %v", got)
	}
	others("the put-back")
}

func readSaved(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, savedFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheFlagIsSavedWithTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	r := newRigIn(t, okBackend(), dir)
	r.click(t, "host api-1")
	r.w.splitFocused(layout.Row)
	r.tt.Frame()
	r.w.toggleSync(r.w.tab())
	r.click(t, "host api-1") // a second tab, sync off
	r.w.closeWindow()
	if b := readSaved(t, dir); !strings.Contains(b, `"sync":true`) || strings.Count(b, `"sync"`) != 1 {
		t.Fatalf("saved %s: sync is kept for the tab that has it and left out of the other", b)
	}

	r2 := newRigIn(t, &fakeBackend{hosts: testHosts, sessions: sessionsOf(r.fb)}, dir)
	if len(r2.w.tabs) != 2 || !r2.w.tabs[0].sync || r2.w.tabs[1].sync {
		t.Fatalf("tabs %d, sync %v %v", len(r2.w.tabs), r2.w.tabs[0].sync, r2.w.tabs[1].sync)
	}
	// And it works after the restart: a frame publishes the peers.
	r2.w.selectTab(0)
	r2.tt.Frame()
	a, b := r2.w.tabs[0].panes["1"], r2.w.tabs[0].panes["2"]
	for _, c := range r2.fb.conns {
		st := drivingState
		c.st = &st
	}
	typeKeys(a, "k")
	if a.conn.(*fakeConn).written() != "k" || b.conn.(*fakeConn).written() != "k" {
		t.Errorf("after a restart the keys reached %q and %q", a.conn.(*fakeConn).written(), b.conn.(*fakeConn).written())
	}
}

// The terminal's input goroutine writes while the UI thread publishes: run it with -race.
func TestSyncPublishAndWriteDoNotRace(t *testing.T) {
	r := syncRig(t, 3)
	r.syncOn(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			typeKeys(r.p, "x")
		}
	}()
	for i := 0; i < 200; i++ {
		r.w.publishSync()
		if i%50 == 0 {
			r.w.toggleSync(r.w.tab())
			r.w.toggleSync(r.w.tab())
		}
	}
	wg.Wait()
}

func TestTheSyncButtonAndTheStatusBarSayWhatSyncIs(t *testing.T) {
	r := syncRig(t, 2)
	if !r.tt.HasText("Sync off") || !r.has("Sync input") || r.has("Stop syncing input") {
		t.Fatalf("sync off: texts %v", r.tt.Texts())
	}
	if err := r.tt.Click("Sync input"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.w.tab().sync || !r.tt.HasText("Sync on") || !r.has("Stop syncing input") || r.has("Sync input") {
		t.Fatalf("sync on: texts %v", r.tt.Texts())
	}
}
