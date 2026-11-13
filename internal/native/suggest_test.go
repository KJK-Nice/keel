package native

import (
	"strings"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

// Every case of cmd/keel/web/js/test/suggest.test.mjs (16 tests), as Go. The card tests that asserted HTML are in
// suggestview_test.go (the words and look of the card in the window).

func TestPromptInputSplitsThePromptFromWhatWasTyped(t *testing.T) { // JS 1
	cases := []struct {
		in, prompt, input string
		start             int
		nilWant           bool
	}{
		{"sh-3.2$ show the files here   ", "sh-3.2$ ", "show the files here", 8, false},
		{"deploy@prod-db-01:~$ which process is listening on 5432", "", "which process is listening on 5432", 0, false},
		{"root@box:~# # restart it", "", "# restart it", 0, false},
		{"[me@h dir]$ ", "", "", 0, false},
		{"❯ list containers", "", "list containers", 0, false},
		{"no prompt on this line", "", "", 0, true},
	}
	for _, c := range cases {
		p := promptInput(c.in)
		if c.nilWant {
			if p != nil {
				t.Errorf("%q: got %v, want none", c.in, p)
			}
			continue
		}
		if p == nil || p.Input != c.input || (c.prompt != "" && (p.Prompt != c.prompt || p.Start != c.start)) {
			t.Errorf("%q: got %+v, want input %q", c.in, p, c.input)
		}
	}
}

func TestWorthDetecting(t *testing.T) { // JS 2
	for in, want := range map[string]bool{
		"show the files": true, "# restart": true, "#": false, "uptime": false,
		strings.Repeat("x", suggestMaxLen+1) + " y": false, "a\x03 b": false,
	} {
		if got := worthDetecting(in); got != want {
			t.Errorf("worthDetecting(%q) = %v", in, got)
		}
	}
}

func TestInsertGate(t *testing.T) { // JS 3
	if ok, why := insertGate(&keelclient.SuggestOption{NeedsShift: true}, false); ok || why != needsShiftReason {
		t.Errorf("plain on a ⇧ option: %v %q", ok, why)
	}
	if ok, _ := insertGate(&keelclient.SuggestOption{NeedsShift: true}, true); !ok {
		t.Error("⇧ on a ⇧ option")
	}
	if ok, _ := insertGate(&keelclient.SuggestOption{}, false); !ok {
		t.Error("plain on a plain option")
	}
	if ok, _ := insertGate(nil, true); ok {
		t.Error("no option")
	}
}

func TestUndoKey(t *testing.T) { // JS 4
	for _, c := range []struct {
		k    sugKey
		mac  bool
		want bool
	}{
		{sugKey{Name: "z", Meta: true}, true, true},
		{sugKey{Name: "z", Ctrl: true}, true, false},
		{sugKey{Name: "z", Ctrl: true}, false, false}, // Ctrl+Z is SIGTSTP
		{sugKey{Name: "Z", Ctrl: true, Shift: true}, false, true},
		{sugKey{Name: "z", Meta: true, Shift: true}, true, false},
	} {
		if got := undoKey(c.k, c.mac); got != c.want {
			t.Errorf("undoKey(%+v, mac=%v) = %v", c.k, c.mac, got)
		}
	}
}

func TestCardWordsAndRiskRules(t *testing.T) { // the logic of JS 6 (the card's words are checked in suggestview_test.go)
	if got := riskWords(keelclient.SuggestOption{Risk: "destructive", Command: "rm -rf /tmp/x"}); got != "Deletes files" {
		t.Errorf("rm: %q", got)
	}
	if got := riskWords(keelclient.SuggestOption{Risk: "destructive", Command: "dd if=/dev/zero of=/dev/sda"}); got != "Destructive" {
		t.Errorf("dd: %q", got)
	}
	if got := riskWords(keelclient.SuggestOption{Risk: "readonly"}); got != "Read-only" {
		t.Errorf("readonly: %q", got)
	}
	for src, want := range map[string]string{"history": "From this pane's scrollback", "snippet": "From your snippets", "generated": "Suggested from your words", "": "Suggested from your words"} { // JS 16
		if got := sourceLabel(src); got != want {
			t.Errorf("sourceLabel(%q) = %q", src, got)
		}
	}
	k := keyLabels(false)
	if k.Tab != "Tab" || k.Shift != "Shift" || len(k.Undo) != 3 {
		t.Errorf("labels off macOS: %+v", k)
	}
}

func TestOptionKeys(t *testing.T) {
	if optionKey(sugKey{Name: "ArrowDown"}, 0, 3) != 1 || optionKey(sugKey{Name: "ArrowUp"}, 0, 3) != 2 || optionKey(sugKey{Name: "2"}, 0, 3) != 1 {
		t.Error("option keys move wrongly")
	}
	if optionKey(sugKey{Name: "4"}, 0, 3) != -1 || optionKey(sugKey{Name: "2", Shift: true}, 0, 3) != -1 || optionKey(sugKey{Name: "2", Meta: true}, 0, 3) != -1 {
		t.Error("a key that is not an option key is taken")
	}
}

// harness is JS machine(): a machine with a fake clock, fake I/O and a pending insert the test completes.
type harness struct {
	t        *testing.T
	m        *sugMachine
	sent     []sentFrame
	inserts  []insertCall
	restores []string
	now      time.Time
	line     *promptLine
	driving  bool
	finish   func(insertResult) // the insert in flight
	auto     *insertResult      // when set, an insert completes at once with it
}

type sentFrame struct {
	kind string
	seq  int
	line string
}

type insertCall struct {
	sug   *keelclient.Suggestion
	i     int
	shift bool
	words string
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, now: time.Unix(1000, 0), driving: true, line: &promptLine{Input: "show the files here", Start: 8}}
	ok := insertResult{OK: true}
	h.auto = &ok
	h.m = newSugMachine(sugDeps{
		readLine:    func() *promptLine { return h.line },
		canSuggest:  func() bool { return h.driving },
		sendDetect:  func(seq int, l string) { h.sent = append(h.sent, sentFrame{"detect", seq, l}) },
		sendSuggest: func(seq int, l string) { h.sent = append(h.sent, sentFrame{"suggest", seq, l}) },
		insert: func(s *keelclient.Suggestion, i int, shift bool, words string, done func(insertResult)) {
			h.inserts = append(h.inserts, insertCall{s, i, shift, words})
			if h.auto != nil {
				done(*h.auto)
				return
			}
			h.finish = done
		},
		restore: func(w string) { h.restores = append(h.restores, w) },
		now:     func() time.Time { return h.now },
		mac:     true,
	})
	return h
}

// fire is the typing pause ending.
func (h *harness) fire() { h.now = h.now.Add(detectPause); h.m.Tick() }
func (h *harness) setLine(input string) {
	h.line = &promptLine{Input: input, Start: 8}
}
func (h *harness) tick(d time.Duration) { h.now = h.now.Add(d) }

func (h *harness) state(want sugState) {
	h.t.Helper()
	if got := h.m.State(); got != want {
		h.t.Fatalf("state %q, want %q", got, want)
	}
}

func kd(name string) sugKey { return sugKey{Name: name} }

func opts(cmds ...string) *keelclient.Suggestion {
	s := &keelclient.Suggestion{ID: "s_1"}
	for _, c := range cmds {
		s.Options = append(s.Options, keelclient.SuggestOption{Command: c, Risk: "readonly"})
	}
	return s
}

func TestNoticeSuggestInsertUndo(t *testing.T) { // JS 7
	h := newHarness(t)
	if h.m.OnKey(kd("Tab")) { // ⇥ is the shell's until a line is noticed
		t.Fatal("⇥ taken while idle")
	}
	h.m.OnInput("e")
	if len(h.sent) != 0 {
		t.Fatal("a frame per key")
	}
	h.fire()
	if len(h.sent) != 1 || h.sent[0] != (sentFrame{"detect", 1, "show the files here"}) {
		t.Fatalf("sent %v", h.sent)
	}
	h.m.OnDetect(1, true, "jev")
	h.state(sugNotice)
	if !h.m.OnKey(kd("Tab")) {
		t.Fatal("⇥ not taken on a notice")
	}
	h.state(sugLoading)
	if h.sent[1].kind != "suggest" {
		t.Fatalf("sent %v", h.sent)
	}
	sug := opts("echo a", "echo b")
	h.m.OnSuggest(h.sent[1].seq, true, sug, "", "")
	h.state(sugCard)
	if !h.m.OnKey(kd("ArrowDown")) || h.m.View().Selected != 1 {
		t.Fatal("ArrowDown")
	}
	if !h.m.OnKey(kd("1")) || h.m.View().Selected != 0 {
		t.Fatal("1")
	}
	if !h.m.OnKey(kd("Tab")) {
		t.Fatal("⇥ on a card")
	}
	if len(h.inserts) != 1 || h.inserts[0].sug != sug || h.inserts[0].i != 0 || h.inserts[0].shift {
		t.Fatalf("inserts %+v", h.inserts)
	}
	h.state(sugInserted)
	h.setLine("echo a")
	h.m.OnRender()
	if !h.m.View().Seen {
		t.Fatal("the inserted command is not seen")
	}
	if !h.m.OnKey(sugKey{Name: "z", Meta: true}) { // ⌘Z restores the words
		t.Fatal("⌘Z not taken")
	}
	if len(h.restores) != 1 || h.restores[0] != "show the files here" {
		t.Fatalf("restores %v", h.restores)
	}
	h.state(sugIdle)
}

func TestDestructiveNeedsShift(t *testing.T) { // JS 8
	h := newHarness(t)
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "hash")
	h.m.OnKey(kd("Tab"))
	h.m.OnSuggest(h.sent[1].seq, true, &keelclient.Suggestion{ID: "s", Options: []keelclient.SuggestOption{{Command: "rm -rf /tmp/x", Risk: "destructive", NeedsShift: true}}}, "", "")
	h.m.OnKey(kd("Tab"))
	if len(h.inserts) != 0 {
		t.Fatal("plain ⇥ inserted a destructive command")
	}
	if !strings.Contains(h.m.View().Note, "Press ⇧⇥") {
		t.Fatalf("note %q", h.m.View().Note)
	}
	h.m.OnKey(sugKey{Name: "Tab", Shift: true})
	if len(h.inserts) != 1 || !h.inserts[0].shift {
		t.Fatalf("inserts %+v", h.inserts)
	}
}

func TestEscKeepsTheWordsAndTheNoticeDoesNotComeBack(t *testing.T) { // JS 9
	h := newHarness(t)
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.m.OnKey(kd("Tab"))
	h.m.OnSuggest(h.sent[1].seq, true, opts("ls"), "", "")
	if !h.m.OnKey(kd("Escape")) {
		t.Fatal("Esc not taken on a card")
	}
	h.state(sugIdle)
	h.m.OnInput("x")
	h.fire()
	n := 0
	for _, s := range h.sent {
		if s.kind == "detect" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dismissed words were sent again: %v", h.sent)
	}
}

// Esc on the notice is Keel's (the webview also passes it to the shell, which then eats the next key as Meta+key); the
// key typed right after goes to the session, and Esc with nothing showing stays the shell's.
func TestEscOnTheNoticeIsNotTheShellsAndTheNextKeyIs(t *testing.T) {
	h := newHarness(t)
	if h.m.OnKey(kd("Escape")) {
		t.Fatal("Esc taken with nothing showing")
	}
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.state(sugNotice)
	if !h.m.OnKey(kd("Escape")) {
		t.Fatal("Esc on the notice was passed to the shell")
	}
	h.state(sugIdle)
	if h.m.OnKey(kd("a")) {
		t.Fatal("the key after Esc did not reach the session")
	}
}

func TestAFollowerGetsNothing(t *testing.T) { // JS 10
	h := newHarness(t)
	h.driving = false
	h.m.OnInput("x")
	h.fire()
	if len(h.sent) != 0 || h.m.OnKey(kd("Tab")) {
		t.Fatal("a follower asked or lost ⇥")
	}
	h.driving = true
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.state(sugNotice)
	h.driving = false
	if h.m.OnKey(kd("Tab")) {
		t.Fatal("⇥ taken after control passed away")
	}
	h.state(sugIdle)
}

func TestAltScreenStaleAnswerEnterAndAnOffPane(t *testing.T) { // JS 11
	h := newHarness(t)
	h.line = nil // full-screen app or no prompt
	h.m.OnInput("x")
	h.fire()
	if len(h.sent) != 0 {
		t.Fatal("asked without a prompt")
	}
	h.setLine("show the files here")
	h.m.OnInput("x")
	h.fire()
	h.setLine("show the files here and more")
	h.m.OnDetect(1, true, "jev")
	h.state(sugIdle) // words changed since the question
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(2, false, "off")
	h.m.OnInput("y")
	h.setLine("other words here")
	h.fire()
	if len(h.sent) != 2 {
		t.Fatalf("no back off after off: %v", h.sent)
	}
	h.tick(offBackoff + time.Millisecond)
	h.m.OnInput("y")
	h.fire()
	if len(h.sent) != 3 {
		t.Fatalf("sent %v", h.sent)
	}
	h.m.OnDetect(3, true, "jev")
	h.state(sugNotice)
	h.m.OnInput("\r")
	h.state(sugIdle) // Enter runs the words; the notice goes
}

func TestCachedAnswersAreNotAskedAgainFallbackIsNot(t *testing.T) { // JS 12
	h := newHarness(t)
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, false, "fallback")
	h.m.OnInput("x")
	h.fire()
	if len(h.sent) != 2 {
		t.Fatalf("fallback was cached: %v", h.sent)
	}
	h.m.OnDetect(2, true, "jev")
	h.m.OnInput("\r")
	h.m.OnInput("x")
	h.fire()
	if len(h.sent) != 2 {
		t.Fatalf("a jev answer was not cached: %v", h.sent)
	}
	h.state(sugNotice)
}

func TestAnInsertRefusedKeepsTheCardWithTheReason(t *testing.T) { // JS 13
	h := newHarness(t)
	h.auto = &insertResult{Message: "the insert expired; press ⇥ again"}
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.m.OnKey(kd("Tab"))
	h.m.OnSuggest(h.sent[1].seq, true, opts("ls"), "", "")
	h.m.OnKey(kd("Tab"))
	h.state(sugCard)
	if !strings.Contains(h.m.View().Note, "expired") {
		t.Fatalf("note %q", h.m.View().Note)
	}
}

func TestAnInsertDroppedWritesNothingAndClosesTheCard(t *testing.T) { // JS 14
	h := newHarness(t)
	h.auto = &insertResult{Dropped: true}
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.m.OnKey(kd("Tab"))
	h.m.OnSuggest(h.sent[1].seq, true, opts("ls"), "", "")
	h.m.OnKey(kd("Tab"))
	if len(h.inserts) != 1 || h.inserts[0].words != "show the files here" {
		t.Fatalf("insert gets the words to re-check: %+v", h.inserts)
	}
	h.state(sugIdle)
}

func TestALaterStricterRatingChangesTheBadgeNeverALooserOne(t *testing.T) { // JS 15
	h := newHarness(t)
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.m.OnKey(kd("Tab"))
	sug := &keelclient.Suggestion{ID: "s_9", Options: []keelclient.SuggestOption{{Command: "echo keel-jev-slow", Risk: "readonly"}}}
	h.m.OnSuggest(h.sent[1].seq, true, sug, "", "")
	opt := func() keelclient.SuggestOption { return h.m.View().Sug.Options[0] }
	if riskWords(opt()) != "Read-only" {
		t.Fatal("not read-only at first")
	}
	h.m.OnRisk("s_other", []keelclient.SuggestOption{{Risk: "destructive", NeedsShift: true, Judged: true}}) // another suggestion
	if opt().Risk != "readonly" {
		t.Fatal("an update for another suggestion was applied")
	}
	h.m.OnRisk("s_9", []keelclient.SuggestOption{{Risk: "destructive", NeedsShift: true, Judged: true}})
	if h.m.State() != sugCard || opt().Risk != "destructive" || !opt().NeedsShift {
		t.Fatalf("not stricter: %+v", opt())
	}
	h.m.OnKey(kd("Tab"))
	if len(h.inserts) != 0 || !strings.Contains(h.m.View().Note, "Press ⇧⇥") {
		t.Fatalf("plain ⇥ after a stricter rating: inserts %v note %q", h.inserts, h.m.View().Note)
	}
	h.m.OnRisk("s_9", []keelclient.SuggestOption{{Risk: "readonly", NeedsShift: false, Judged: true}}) // never looser
	if opt().Risk != "destructive" || !opt().NeedsShift {
		t.Fatalf("loosened: %+v", opt())
	}
	h.m.OnKey(sugKey{Name: "Tab", Shift: true})
	if len(h.inserts) != 1 {
		t.Fatal("⇧⇥ did not insert")
	}
}

// A request whose answer is lost does not stay loading: past suggestWait the card shows the error, ⇥ dismisses it, and a
// late answer for it shows nothing (plan 036; before, nothing but a key ended the wait).
func TestALostAnswerEndsInTheErrorCard(t *testing.T) {
	h := newHarness(t)
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.m.OnKey(kd("Tab"))
	h.state(sugLoading)
	if due, ok := h.m.Due(); !ok || !due.Equal(h.now.Add(suggestWait)) {
		t.Fatalf("due %v %v", due, ok)
	}
	h.tick(suggestWait - time.Second)
	h.m.Tick()
	h.state(sugLoading)
	h.tick(time.Second)
	h.m.Tick()
	h.state(sugError)
	if got := h.m.View().Message; got != "No suggestion came back." {
		t.Fatalf("message %q", got)
	}
	if _, ok := h.m.Due(); ok {
		t.Fatal("a timer is still waiting after the error")
	}
	h.m.OnSuggest(h.sent[1].seq, true, opts("ls"), "", "") // the answer that was lost arrives
	h.state(sugError)
	if !h.m.OnKey(kd("Tab")) {
		t.Fatal("⇥ on the error was not taken")
	}
	h.state(sugIdle)
}

// Beyond the JS cases: the pieces the page has in other files.
func TestStaleSuggestAnswerAndTypingWhileLoading(t *testing.T) {
	h := newHarness(t)
	h.m.OnInput("x")
	h.fire()
	h.m.OnDetect(1, true, "jev")
	h.m.OnKey(kd("Tab"))
	h.m.OnSuggest(99, true, opts("ls"), "", "") // an answer for another question
	h.state(sugLoading)
	if !h.m.OnKey(kd("Tab")) { // ⇥ while loading is swallowed, not sent to the shell
		t.Fatal("⇥ while loading reached the shell")
	}
	if h.m.OnKey(kd("a")) { // another key goes on to the shell and closes the card
		t.Fatal("a typed key was taken")
	}
	h.state(sugIdle)
	h.m.OnSuggest(h.sent[1].seq, true, opts("ls"), "", "") // the late answer shows nothing
	h.state(sugIdle)
}
