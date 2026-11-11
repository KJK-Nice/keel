package native

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// In-pane suggestions on the fake backend: what a pane asks of keel serve, what it shows, which keys it takes, and what
// reaches the session. The fake records every question, every insert and every restore apart from the keystrokes.

// sugRig is a window with one session this client drives, a placeholder body whose prompt line the test scripts, and the
// keys the pane's terminal would hand OnKey.
type sugRig struct {
	*rig
	p    *pane
	c    *fakeConn
	line string // the text of the cursor's row; "" for a screen with no cursor row (a full-screen program)
	alt  bool
}

func newSugRig(t *testing.T) *sugRig {
	t.Helper()
	r := &sugRig{rig: snippetRig(t, snippetBackend())}
	r.p, r.c = r.w.focusedPane(), r.fb.conns[0]
	r.line = "sh-3.2$ show the files here"
	r.p.sg.cursor = func() (int, string, bool) { return 2, r.line, !r.alt }
	r.tt.Frame()
	return r
}

func (r *sugRig) key(mods ui.Modifiers, k ui.Key) bool {
	taken := r.w.suggestKey(r.p, mods, k)
	r.tt.Frame()
	return taken
}

// pause is the typing pause passing and a frame running.
func (r *sugRig) pause() { r.tt.Advance(detectPause + time.Millisecond); r.tt.Frame() }

func (r *sugRig) push(ev ...keelclient.SuggestEvent) {
	r.c.sugEvents = append(r.c.sugEvents, ev...)
	r.tt.Frame()
}

func (r *sugRig) state() sugState { return r.p.sg.m.State() }

// noticed takes the pane from typing to a noticed line.
func (r *sugRig) noticed(t *testing.T) {
	t.Helper()
	r.key(0, ui.KeyX)
	r.pause()
	if len(r.c.detects) == 0 {
		t.Fatal("no detection after the typing pause")
	}
	d := r.c.detects[len(r.c.detects)-1]
	r.push(keelclient.SuggestEvent{Kind: keelclient.SuggestDetected, Seq: d.seq, Plain: true, Source: "jev"})
	if r.state() != sugNotice {
		t.Fatalf("state %q, want a notice", r.state())
	}
}

func card(options ...keelclient.SuggestOption) *keelclient.Suggestion {
	return &keelclient.Suggestion{ID: "s_1", Options: options}
}

func ro(cmd string) keelclient.SuggestOption {
	return keelclient.SuggestOption{Command: cmd, Risk: "readonly", Why: "Lists the files.", Source: "generated"}
}

func (r *sugRig) carded(t *testing.T, sug *keelclient.Suggestion) {
	t.Helper()
	r.noticed(t)
	if !r.key(0, ui.KeyTab) {
		t.Fatal("⇥ not taken on a notice")
	}
	a := r.c.asks[len(r.c.asks)-1]
	r.push(keelclient.SuggestEvent{Kind: keelclient.SuggestAnswered, Seq: a.seq, OK: true, Suggestion: sug})
	if r.state() != sugCard {
		t.Fatalf("state %q, want a card", r.state())
	}
}

func TestNoticeThenTabAsksThenTabInsertsNotRunThenCmdZRestores(t *testing.T) {
	r := newSugRig(t)
	r.noticed(t)
	if got := r.c.detects; len(got) != 1 || got[0].line != "show the files here" {
		t.Fatalf("detects %v: only the words the user typed leave the client", got)
	}
	r.texts(t, "Looks like a request", "suggest a command")
	if !r.has("suggestion notice") {
		t.Error("the line is not highlighted")
	}
	r.tt.Frame()
	if !r.key(0, ui.KeyTab) {
		t.Fatal("⇥ not taken")
	}
	if got := r.c.asks; len(got) != 1 || got[0].line != "show the files here" {
		t.Fatalf("asks %v", got)
	}
	r.texts(t, "Suggesting a command…", "keep my words")
	a := r.c.asks[0]
	r.push(keelclient.SuggestEvent{Kind: keelclient.SuggestAnswered, Seq: a.seq, OK: true,
		Suggestion: card(ro("find . -type f -size +100M"), ro("du -ah . | sort -rh | head"))})
	r.texts(t, "find . -type f -size +100M", "Read-only", "Lists the files.", "Suggested from your words", "du -ah . | sort -rh | head", "insert", "other options", "keep my words")
	if r.has("suggestion card destructive") {
		t.Error("a read-only card is red")
	}
	if !r.key(0, ui.KeyTab) {
		t.Fatal("⇥ not taken on a card")
	}
	if got := r.c.sugInserts; len(got) != 1 || got[0] != (keelclient.SuggestionRef{ID: "s_1", Option: 0, Shift: false}) {
		t.Fatalf("inserts %v", got)
	}
	if r.c.wrote != 0 || len(r.c.restored) != 0 || len(r.c.injected) != 0 || len(r.c.directs) != 0 {
		t.Errorf("another writer was used: wrote %d restored %v injected %v directs %v", r.c.wrote, r.c.restored, r.c.injected, r.c.directs)
	}
	if r.state() != sugInserted {
		t.Fatalf("state %q", r.state())
	}
	r.line = "sh-3.2$ find . -type f -size +100M"
	r.tt.Advance(sugReadEvery + time.Millisecond)
	r.tt.Frame()
	r.texts(t, "Inserted, not run", "back to your words")
	if !r.key(ui.Cmd, ui.KeyZ) {
		t.Fatal("⌘Z not taken right after an insert")
	}
	if got := r.c.restored; len(got) != 1 || got[0] != "show the files here" {
		t.Fatalf("restored %v", got)
	}
	if r.key(ui.Cmd, ui.KeyZ) {
		t.Error("a second ⌘Z is taken")
	}
	if r.c.wrote != 0 {
		t.Error("the window wrote keystrokes itself")
	}
}

func TestPlainTabNeverInsertsAnOptionThatNeedsShiftAndShiftTabSendsShiftAsPressed(t *testing.T) {
	r := newSugRig(t)
	r.carded(t, card(keelclient.SuggestOption{Command: "rm -rf /tmp/x", Risk: "destructive", NeedsShift: true, Source: "generated"}))
	r.texts(t, "Deletes files", "insert anyway", "Plain ⇥ never inserts a destructive command")
	if !r.has("suggestion card destructive") {
		t.Error("no destructive card")
	}
	r.key(0, ui.KeyTab)
	r.tt.Frame()
	if len(r.c.sugInserts) != 0 || !r.tt.HasText("Press ⇧⇥ to insert this command.") {
		t.Fatalf("plain ⇥ on a destructive command: inserts %v texts %v", r.c.sugInserts, r.tt.Texts())
	}
	r.key(ui.Shift, ui.KeyTab)
	if got := r.c.sugInserts; len(got) != 1 || !got[0].Shift {
		t.Fatalf("inserts %v", got)
	}
}

func TestAStricterRatingTurnsTheCardRedAndAsksForShift(t *testing.T) {
	r := newSugRig(t)
	r.carded(t, card(ro("echo keel-jev-slow")))
	r.push(keelclient.SuggestEvent{Kind: keelclient.SuggestRated, ID: "s_1", Options: []keelclient.SuggestOption{{Risk: "destructive", NeedsShift: true, Judged: true}}})
	r.texts(t, "insert anyway", "Destructive")
	r.key(0, ui.KeyTab)
	if len(r.c.sugInserts) != 0 {
		t.Fatal("plain ⇥ inserted after a stricter rating")
	}
}

func TestEscKeepsTheWordsAndTabGoesToTheShell(t *testing.T) {
	r := newSugRig(t)
	r.carded(t, card(ro("ls")))
	if !r.key(0, ui.KeyEscape) {
		t.Fatal("esc not taken on a card")
	}
	if r.state() != sugIdle || len(r.c.sugInserts) != 0 || len(r.c.restored) != 0 {
		t.Fatalf("state %q inserts %v restored %v", r.state(), r.c.sugInserts, r.c.restored)
	}
	if r.key(0, ui.KeyTab) {
		t.Error("⇥ taken with nothing noticed")
	}
}

func TestNothingIsAskedOrTakenWhereAGuardApplies(t *testing.T) {
	for name, set := range map[string]func(r *sugRig){
		"a follower":            func(r *sugRig) { st := drivingState; st.Role = "follower"; r.c.st = &st },
		"not ready":             func(r *sugRig) { st := drivingState; st.Status = "connecting"; r.c.st = &st },
		"a joined pane":         func(r *sugRig) { r.p.remote = true },
		"a full-screen program": func(r *sugRig) { r.alt = true },
		"a line with no prompt": func(r *sugRig) { r.line = "no prompt on this line" },
		"a dialog":              func(r *sugRig) { r.w.openPalette() },
	} {
		r := newSugRig(t)
		set(r)
		r.key(0, ui.KeyX)
		r.pause()
		if len(r.c.detects) != 0 || len(r.c.asks) != 0 {
			t.Errorf("%s: asked keel serve: %v %v", name, r.c.detects, r.c.asks)
		}
		if r.key(0, ui.KeyTab) || r.key(ui.Cmd, ui.KeyZ) {
			t.Errorf("%s: a key was taken", name)
		}
	}
}

func TestAPaneInTheBackgroundOrALeftNoticeGetsNothing(t *testing.T) {
	r := newSugRig(t)
	r.noticed(t)
	st := drivingState
	st.Role = "follower" // control passed away while a notice showed
	r.c.st = &st
	if r.key(0, ui.KeyTab) {
		t.Fatal("⇥ taken by a follower's notice")
	}
	if r.state() != sugIdle || len(r.c.asks) != 0 {
		t.Fatalf("state %q asks %v", r.state(), r.c.asks)
	}
}

func TestInsertIsDroppedWhenTheWordsChangedUnderTheCard(t *testing.T) {
	r := newSugRig(t)
	r.carded(t, card(ro("ls")))
	r.line = "sh-3.2$ show the files here and more"
	r.key(0, ui.KeyTab)
	if len(r.c.sugInserts) != 0 || r.state() != sugIdle {
		t.Fatalf("inserts %v state %q: nothing may be written for words the suggestion was not for", r.c.sugInserts, r.state())
	}
}

func TestAnInsertKeelServeRefusesKeepsTheCardWithItsWords(t *testing.T) {
	r := newSugRig(t)
	r.c.sugInsertErr = keelclient.ErrSuggestionGone
	r.carded(t, card(ro("ls")))
	r.key(0, ui.KeyTab)
	r.tt.Frame()
	if r.state() != sugCard || !r.tt.HasText("the insert expired; press ⇥ again") {
		t.Fatalf("state %q texts %v", r.state(), r.tt.Texts())
	}
}

func TestTypingIsNeverHeldBackOrChanged(t *testing.T) {
	r := newSugRig(t)
	for _, k := range []ui.Key{ui.KeyA, ui.KeyB, ui.KeyTab, ui.KeyEscape, ui.KeyUp, ui.Key1, ui.KeyEnter} {
		if r.key(0, k) {
			t.Errorf("key %v taken while nothing is noticed", k)
		}
	}
	if len(r.c.detects) != 0 {
		t.Error("a detection per key")
	}
	// Enter sends exactly what the user typed: the window never adds to it, and a noticed line ends at Enter.
	r.noticed(t)
	if r.key(0, ui.KeyEnter) {
		t.Fatal("Enter taken")
	}
	if r.state() != sugIdle || r.c.wrote != 0 || len(r.c.sugInserts) != 0 || len(r.c.restored) != 0 {
		t.Fatalf("state %q wrote %d", r.state(), r.c.wrote)
	}
	// A card lets every other key through, and closes.
	r.carded(t, card(ro("ls")))
	if r.key(0, ui.KeyA) || r.state() != sugIdle {
		t.Error("a typed key was taken by the card, or the card stayed")
	}
}

func TestOptionKeysPickAndTheCardFollowsTheChoice(t *testing.T) {
	r := newSugRig(t)
	r.carded(t, card(ro("first-cmd"), ro("second-cmd"), ro("third-cmd")))
	if !r.key(0, ui.KeyDown) || !r.key(0, ui.Key3) {
		t.Fatal("option keys not taken")
	}
	r.tt.Frame()
	if !r.tt.HasText("third-cmd") || !r.has("suggestion option 1") {
		t.Fatalf("texts %v", r.tt.Texts())
	}
	r.key(0, ui.KeyTab)
	if got := r.c.sugInserts; len(got) != 1 || got[0].Option != 2 {
		t.Fatalf("inserts %v", got)
	}
}

func TestSourceLabelsAndThePlaceholderNoteShow(t *testing.T) {
	r := newSugRig(t)
	o := ro("echo [secret:1]")
	o.Source, o.Placeholders = "history", true
	r.carded(t, card(o))
	r.texts(t, "From this pane's scrollback", "Holds a redacted value such as [secret:1]. Replace it after inserting.")
}

func TestAnErrorAnswerShowsItsMessageAndAnyKeyClosesIt(t *testing.T) {
	r := newSugRig(t)
	r.noticed(t)
	r.key(0, ui.KeyTab)
	a := r.c.asks[0]
	r.push(keelclient.SuggestEvent{Kind: keelclient.SuggestAnswered, Seq: a.seq, Message: "No command came back for these words. Try other words.", Reason: "empty"})
	r.texts(t, "No command came back for these words. Try other words.", "keep my words")
	if !r.key(0, ui.KeyTab) || r.state() != sugIdle { // ⇥ is taken once so it does not complete into the words
		t.Fatalf("state %q", r.state())
	}
}

func TestTheNoticeMovesWithTheTerminalsCells(t *testing.T) {
	r := newSugRig(t)
	r.noticed(t)
	m, ok := r.tt.Find("suggestion notice")
	if !ok {
		t.Fatal("no notice")
	}
	g := gridFor(r.p.fontFamily, r.p.fontSize, 1)
	if w := float32(len([]rune("show the files here"))) * g.cw; m.W < w-0.5 || m.W > w+0.5 || m.H < g.ch-0.5 || m.H > g.ch+0.5 {
		t.Errorf("the mark is %v wide and %v high, want %v by %v (the words' cells)", m.W, m.H, w, g.ch)
	}
	if strings.Contains(strings.Join(r.tt.Texts(), "|"), "sh-3.2") {
		t.Error("the overlay repeats the terminal's text")
	}
}
