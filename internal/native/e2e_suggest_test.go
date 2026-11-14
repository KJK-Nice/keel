//go:build nativee2e

package native

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// In-pane suggestions against a real keel serve (KEEL_AGENT_PROVIDER=fake: the fake model and the fake Jev answer in
// process, nothing is sent anywhere) and a real shell (Local Shell, /bin/sh).
//
// What these prove: a noticed line, the card, an insert that leaves the command on the prompt line with its output absent
// from the scrollback until the test presses Enter, the put-back of the user's words, the ⇧ rule, esc, the # mode, the
// guards, and that typing is not held back. A destructive command the fake proposes is inserted only with ⇧⇥, never
// run: the test does not press Enter on it and clears the line with Ctrl-U.
//
// The window's clock is the tester's, which stands still: pumpClock moves it with the real wait, so the typing pause and
// the machine's reading of the line pass as they do for a person.

const fakeJevKey = "fake-jev-key-for-tests-only-0000"

// suggestSetup stores the made-up keys (the fake provider ignores them; keel serve only needs them to be there) and turns
// suggestions on with the given detection ("auto" or "hash-only").
func (e *win) suggestSetup(detection string) {
	e.t.Helper()
	e.putAgentKey()
	if code, body := e.raw("PUT", "/api/agent/keys/jev", map[string]string{"key": fakeJevKey}); code != 200 {
		e.t.Fatalf("PUT /api/agent/keys/jev: %d %s", code, body)
	}
	e.w.startPoll(200 * time.Millisecond) // the window learns of settings changed elsewhere through the vault rev
	e.suggestSettings("on", detection)
}

// suggestSettings saves the settings as another window would (straight to keel serve) and waits for this window to learn
// of it through the rev poll.
func (e *win) suggestSettings(suggestions, detection string) {
	e.t.Helper()
	if code, body := e.raw("PUT", "/api/agent/settings", map[string]string{"suggestions": suggestions, "detection": detection}); code != 200 {
		e.t.Fatalf("PUT /api/agent/settings: %d %s", code, body)
	}
	e.pump(func() bool { return e.w.sugSet == sugSettings{suggestions, detection} }, "the window to learn of the settings")
}

// pumpClock is pump with the window's clock moving.
func (e *win) pumpClock(cond func() bool, what string) {
	e.t.Helper()
	deadline := time.Now().Add(e2eWait)
	for {
		e.tt.Advance(25 * time.Millisecond)
		e.step()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// settleClock is settle with the window's clock moving, to show that something does NOT happen.
func (e *win) settleClock(d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		e.tt.Advance(25 * time.Millisecond)
		e.step()
	}
}

// typeKeys types s as key presses (a real keyboard sends keys, which is what the pane's key hook sees).
func (e *win) typeKeys(s string) {
	e.t.Helper()
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			e.tt.TypeKey(0, ui.KeyA+ui.Key(r-'a'), string(r))
		case r >= '0' && r <= '9':
			e.tt.TypeKey(0, ui.Key0+ui.Key(r-'0'), string(r))
		case r == ' ':
			e.tt.TypeKey(0, ui.KeySpace, " ")
		case r == '#':
			e.tt.TypeKey(ui.Shift, ui.Key3, "#")
		default:
			e.t.Fatalf("typeKeys has no key for %q", r)
		}
	}
	e.step()
}

func (e *win) needTexts(want ...string) {
	e.t.Helper()
	for _, s := range want {
		if !e.tt.HasText(s) {
			e.t.Errorf("no text %q in %v", s, e.tt.Texts())
		}
	}
}

func (e *win) notice() bool { return e.tt.HasText("Looks like a request") }

func (e *win) sugState() sugState {
	if p := e.w.focusedPane(); p != nil && p.sg.m != nil {
		return p.sg.m.State()
	}
	return sugIdle
}

// tail is the line the cursor is on in the window's own terminal: what the user sees on the prompt line (the server's
// scrollback is the raw stream, with the shell's redraw sequences in it).
func (e *win) tail(string) string {
	_, text, _ := e.w.focusedPane().sg.cursor()
	return text
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// bare is s without escape sequences, line ends and spaces.
func bare(s string) string {
	return strings.NewReplacer("\r", "", "\n", "", " ", "").Replace(ansiRe.ReplaceAllString(s, ""))
}

// ranOutput reports whether the scrollback has a line that is exactly out (the output of a command, not the echo of it).
func (e *win) ranOutput(sid, out string) bool {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(out) + `\r?$`).MatchString(e.scrollback(sid))
}

func (e *win) logCount(kind, sid string) (n int) {
	for _, l := range e.logEntries() {
		if l.Kind == kind && l.SessionID == sid {
			n++
		}
	}
	return n
}

// toCard types words, waits for the notice, presses the suggest key (⌘Y: a line noticed on its own leaves ⇥ to the shell)
// and waits for the card.
func (e *win) toCard(words string) {
	e.t.Helper()
	e.typeKeys(words)
	e.pumpClock(e.notice, "the notice for "+words)
	e.key(ui.Cmd, ui.KeyY)
	e.pumpClock(func() bool { return e.sugState() == sugCard }, "the card")
}

func TestE2ESuggestInsertNotRun(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.suggestSetup("auto")
	sid := e.openLocal()
	e.clickBody("1")

	e.typeKeys("show the files here")
	e.pumpClock(e.notice, "the notice")
	if !e.hasLabel("suggestion notice") {
		t.Error("the line is not highlighted")
	}
	e.needTexts("⌘", "Y", "suggest a command") // the notice names the key: ⇥ stays the shell's
	e.shot("TestE2ESuggestInsertNotRun-notice")

	e.key(ui.Cmd, ui.KeyY)
	e.pumpClock(func() bool { return e.sugState() == sugCard && e.tt.HasText("echo keel-suggest-ok") }, "the card")
	e.needTexts("Read-only", "Prints a marker to show the command reached the prompt line.", "echo keel-suggest-alt-2", "echo keel-suggest-alt-3", "Suggested from your words", "insert", "keep my words")
	e.shot("TestE2ESuggestInsertNotRun-card")
	if sb := e.scrollback(sid); strings.Contains(sb, "keel-suggest-ok") {
		t.Fatalf("the command is in the pane before ⇥: %q", sb)
	}

	e.key(0, ui.KeyTab) // insert
	e.pumpClock(func() bool { return strings.HasSuffix(e.tail(sid), "echo keel-suggest-ok") }, "the command on the prompt line")
	e.settleClock(time.Second) // pumping: if it had run, its output would be here by now
	if e.ranOutput(sid, "keel-suggest-ok") {
		t.Fatalf("the inserted command ran without Enter: %q", e.scrollback(sid))
	}
	if strings.Contains(e.tail(sid), "show the files here") {
		t.Errorf("the words are still on the line: %q", e.tail(sid))
	}
	if e.logCount("agent.suggest", sid) == 0 {
		t.Error("no agent.suggest log row")
	}
	var ins []int
	for _, l := range e.logEntries() {
		if l.Kind == "agent.insert" && l.SessionID == sid {
			ins = append(ins, l.Bytes)
			if l.Source != "generated" {
				t.Errorf("the insert's source is %q", l.Source)
			}
		}
	}
	if len(ins) != 1 || ins[0] != len("\x15echo keel-suggest-ok") {
		t.Errorf("agent.insert rows %v, want one of %d bytes", ins, len("\x15echo keel-suggest-ok"))
	}
	e.pumpClock(func() bool { return e.tt.HasText("Inserted, not run") }, "the inserted look")
	e.shot("TestE2ESuggestInsertNotRun-inserted")

	e.key(0, ui.KeyEnter) // the user runs it
	e.pump(func() bool { return e.ranOutput(sid, "keel-suggest-ok") }, "the output after Enter")
}

func TestE2ESuggestRestore(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.suggestSetup("auto")
	sid := e.openLocal()
	e.clickBody("1")
	e.toCard("show the files here")
	e.key(0, ui.KeyTab)
	e.pumpClock(func() bool {
		return strings.HasSuffix(e.tail(sid), "echo keel-suggest-ok") && e.tt.HasText("Inserted, not run")
	}, "the inserted command")

	e.key(ui.Cmd, ui.KeyZ)
	e.pumpClock(func() bool { return strings.HasSuffix(e.tail(sid), "show the files here") }, "the words back on the line")
	if strings.Contains(e.tail(sid), "keel-suggest-ok") {
		t.Errorf("the command is still on the line: %q", e.tail(sid))
	}
	if e.sugState() != sugIdle || e.tt.HasText("Inserted, not run") {
		t.Error("the inserted look stayed")
	}
	before := e.scrollback(sid)
	e.key(ui.Cmd, ui.KeyZ) // a second ⌘Z is not ours: it goes where it always goes
	e.settleClock(500 * time.Millisecond)
	if e.scrollback(sid) != before {
		t.Errorf("a second ⌘Z changed the line: %q", e.tail(sid))
	}
	if e.ranOutput(sid, "keel-suggest-ok") {
		t.Fatal("the command ran")
	}
	e.key(ui.Ctrl, ui.KeyU) // clear the line; nothing is run
}

func TestE2ESuggestDestructiveNeedsShift(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.suggestSetup("auto")
	sid := e.openLocal()
	e.clickBody("1")
	e.toCard("clean up old files")
	if !e.hasLabel("suggestion card destructive") {
		t.Fatalf("no red card: %v", e.tt.Texts())
	}
	e.needTexts("rm -rf /tmp/keel-suggest-e2e", "Deletes files", "insert anyway", "Plain ⇥ never inserts a destructive command")
	e.shot("TestE2ESuggestDestructiveNeedsShift-card")

	e.key(0, ui.KeyTab) // plain: refused, with the reason
	e.pump(func() bool { return e.tt.HasText("Press ⇧⇥ to insert this command.") }, "the reason")
	e.settleClock(500 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "keel-suggest-e2e") || e.logCount("agent.insert", sid) != 0 {
		t.Fatalf("plain ⇥ inserted a destructive command: %q", e.tail(sid))
	}
	e.shot("TestE2ESuggestDestructiveNeedsShift-refused")

	e.key(ui.Shift, ui.KeyTab)
	e.pumpClock(func() bool { return strings.HasSuffix(e.tail(sid), "rm -rf /tmp/keel-suggest-e2e") }, "the command on the line after ⇧⇥")
	if e.logCount("agent.insert", sid) != 1 {
		t.Errorf("agent.insert rows: %d", e.logCount("agent.insert", sid))
	}
	// Never Enter on it: clear the line.
	e.key(ui.Ctrl, ui.KeyU)
	e.pumpClock(func() bool { return !strings.Contains(e.tail(sid), "keel-suggest-e2e") }, "the line cleared")
}

func TestE2ESuggestEscKeepsWords(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.suggestSetup("auto")
	sid := e.openLocal()
	e.clickBody("1")
	e.toCard("show the files here")
	e.key(0, ui.KeyEscape)
	e.pump(func() bool { return e.sugState() == sugIdle }, "the card to close")
	if !strings.HasSuffix(e.tail(sid), "show the files here") || e.logCount("agent.insert", sid) != 0 {
		t.Fatalf("esc did not keep the words: %q", e.tail(sid))
	}
	// ⇥ is the shell's again: it completes a command name.
	e.key(ui.Ctrl, ui.KeyU)
	e.typeKeys("ech")
	e.key(0, ui.KeyTab)
	e.pumpClock(func() bool { return strings.HasSuffix(strings.TrimRight(e.tail(sid), " "), "echo") }, "the shell to complete ech")
	e.key(ui.Ctrl, ui.KeyU)
}

func TestE2ESuggestHashMode(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.suggestSetup("hash-only")
	sid := e.openLocal()
	e.clickBody("1")
	e.typeKeys("show the files here")
	e.settleClock(1500 * time.Millisecond)
	if e.notice() {
		t.Fatal("a plain line is noticed when only # lines are")
	}
	for _, l := range e.logEntries() {
		if l.SessionID == sid && strings.HasPrefix(l.Kind, "agent.") {
			t.Fatalf("a typed line without # reached keel serve's agent: %+v", l)
		}
	}
	// The suggest key asks for it, and keel serve logs it as it logs a ⇥ request.
	e.key(ui.Cmd, ui.KeyY)
	e.pumpClock(func() bool { return e.sugState() == sugCard && e.tt.HasText("echo keel-suggest-ok") }, "the card for the asked line")
	e.pumpClock(func() bool { return e.logCount("agent.suggest", sid) > 0 }, "the agent.suggest log row")
	e.key(0, ui.KeyEscape)
	e.pump(func() bool { return e.sugState() == sugIdle }, "the card to close")
	e.key(ui.Ctrl, ui.KeyU)
	e.typeKeys("# show the files here")
	e.pumpClock(e.notice, "the notice for a # line")
	e.key(ui.Ctrl, ui.KeyU)
}

func TestE2ESuggestOffAndGuards(t *testing.T) {
	addr, dir := startServe(t, true)
	a := newWin(t, addr, dir, nil)
	a.suggestSetup("auto")
	sid := a.openLocal()
	a.clickBody("1")

	// Suggestions in panes off: nothing.
	a.suggestSettings("off", "auto")
	a.typeKeys("show the files here")
	a.settleClock(1500 * time.Millisecond)
	if a.notice() {
		t.Fatal("a notice with suggestions off")
	}
	a.key(ui.Cmd, ui.KeyY) // the key says so rather than doing nothing (on the words still typed: an empty line is no prompt to read)
	a.pumpClock(func() bool { return a.tt.HasText("Suggestions in panes are off. Turn them on in Settings · Agent.") }, "the message")
	a.key(0, ui.KeyEscape)
	a.key(ui.Ctrl, ui.KeyU)
	a.suggestSettings("on", "auto")

	// "Agent can read" off for the host: nothing.
	if err := a.tt.RightClick("host Local Shell"); err != nil {
		t.Fatal(err)
	}
	a.step()
	if err := a.tt.ChooseMenuItem("Agent can read"); err != nil {
		t.Fatal(err)
	}
	a.pump(func() bool {
		h, ok := a.hostNamed("Local Shell")
		return ok && h.AgentReadable != nil && !*h.AgentReadable
	}, "reading off")
	a.clickBody("1")
	a.typeKeys("show the files here")
	a.settleClock(1500 * time.Millisecond)
	if a.notice() {
		t.Fatal("a notice on a host the agent cannot read")
	}
	a.key(ui.Ctrl, ui.KeyU)
	if err := a.tt.RightClick("host Local Shell"); err != nil {
		t.Fatal(err)
	}
	a.step()
	if err := a.tt.ChooseMenuItem("Agent can read"); err != nil {
		t.Fatal(err)
	}
	a.pump(func() bool {
		h, ok := a.hostNamed("Local Shell")
		return ok && (h.AgentReadable == nil || *h.AgentReadable)
	}, "reading back on")
	a.settleClock(16 * time.Second)

	// Inside less: nothing. The file is the test's own.
	file := filepath.Join(repoRoot(t), ".scratch-native", "e2e", t.Name(), "pager.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.clickBody("1")
	a.typeLine("less " + file)
	a.pumpClock(func() bool { p := a.w.focusedPane(); alt, _ := p.screen(); return alt }, "less on the alternate screen")
	a.typeKeys("xx yy")
	a.settleClock(1500 * time.Millisecond)
	if a.notice() || a.sugState() != sugIdle {
		t.Fatal("a notice inside a full-screen program")
	}
	a.key(0, ui.KeyQ)
	a.pumpClock(func() bool { p := a.w.focusedPane(); alt, _ := p.screen(); return !alt }, "less to exit")

	// A pane that follows gets nothing: window B follows A's session.
	b := newWin(t, addr, dir, nil)
	both := []*win{a, b}
	pumpAll(t, both, func() bool { return len(b.w.tabs) == 1 && b.snap().Role == "follower" && a.snap().Role == "driver" }, "B following")
	a.clickBody("1")
	a.typeKeys("show the files here")
	for end := time.Now().Add(1500 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		a.tt.Advance(25 * time.Millisecond)
		b.tt.Advance(25 * time.Millisecond)
		a.step()
		b.step()
		if b.notice() {
			t.Fatal("a notice in the window that follows")
		}
	}
	if !a.notice() {
		t.Error("the driver's window shows no notice")
	}
	if b.sugState() != sugIdle {
		t.Error("the follower's pane left idle")
	}
	a.key(ui.Ctrl, ui.KeyU)
	_ = sid
}

func TestE2ESuggestDoesNotDelayTyping(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.suggestSetup("auto")
	sid := e.openLocal()
	e.clickBody("1")
	var b strings.Builder
	for b.Len() < 200 {
		b.WriteString("abcdefghij ")
	}
	typed := b.String()[:200]
	e.typeKeys("echo " + typed)
	e.pump(func() bool { return strings.HasSuffix(bare(e.scrollback(sid)), bare("echo "+typed)) }, "the echo of all 200 characters, in order")
	// Enter sends exactly what was typed: the output is the words, no more.
	e.key(0, ui.KeyEnter)
	want := strings.Join(strings.Fields(typed), " ")
	e.pump(func() bool { return e.ranOutput(sid, want) }, "the output of the echo")
}
