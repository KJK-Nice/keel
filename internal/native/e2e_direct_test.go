//go:build nativee2e

package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Type without the dialog against a real keel serve and a real shell (Local Shell): rule 13 of docs/ui-redesign.md §6.
// What these prove is what reaches the PTY and when. A direct type puts the command alone on the prompt line and nothing
// runs until the test presses Enter in the pane, so each scenario asserts the command text in the scrollback with none of
// its output ("Filesystem" is what df prints first), and only then presses Enter. The commands typed are df -h, echo and
// less on a file the test made under its own scratch directory.

// typeFromPalette picks "Type snippet: name" in the palette with ↵, as a person does.
func (e *win) typeFromPalette(name string) {
	e.t.Helper()
	e.pump(func() bool { return e.inWindow(name) }, "the snippet "+name+" in the window")
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil && e.tt.Focused("Command palette") }, "the palette with the keyboard in its input")
	e.tt.Type("Type snippet: " + name)
	e.step()
	e.key(0, ui.KeyEnter)
}

// noDialog fails when the Approve before type dialog is open or the window is about to open it.
func (e *win) noDialog(what string) {
	e.t.Helper()
	if e.w.approve != nil || e.hasLabel("dialog "+approveTitle) {
		e.t.Fatalf("%s: the Approve before type dialog is open", what)
	}
}

// pressEnterInPane waits until the pane has the keyboard back (nothing is held, the review focus is spent) and presses ↵.
func (e *win) pressEnterInPane() {
	e.t.Helper()
	e.pump(func() bool { return e.w.reviewing == "" && e.w.wantFocus == "" }, "the terminal to have the keyboard")
	e.key(0, ui.KeyEnter)
}

// directLogged reports a log entry of that kind whose source is "direct".
func (e *win) directLogged(kind string) bool {
	for _, l := range e.logEntries() {
		if l.Kind == kind && l.Source == "direct" {
			return true
		}
	}
	return false
}

func TestE2EDirectTypeSnippet(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()

	// The seeded snippet "disk" is df -h: in the grammar, so the palette row has no "Approve first" badge.
	e.pump(func() bool { return e.inWindow("disk") }, "the seeded snippet")
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil && e.tt.Focused("Command palette") }, "the palette")
	e.tt.Type("Type snippet: disk")
	e.step()
	if !e.tt.HasText("Type snippet: disk") || e.hasLabel("badge Approve first") {
		t.Fatalf("the palette row, or a badge it should not have: %v", e.tt.Texts())
	}
	e.key(0, ui.KeyEnter)

	e.pump(func() bool { return strings.Contains(e.scrollback(sid), "df -h") }, "df -h on the prompt line")
	e.settle(time.Second) // pumping: if it had run, the output would be here by now
	e.noDialog("a read-only one-liner")
	if sb := e.scrollback(sid); strings.Contains(sb, "Filesystem") {
		t.Fatalf("the command ran without Enter: %q", sb)
	}
	if !e.directLogged("snippet") {
		t.Errorf("no snippet log row with source direct: %v", e.logEntries())
	}
	if !e.tt.HasText("Press Enter to run.") {
		t.Errorf("the note says nothing about Enter: %v", e.tt.Texts())
	}
	e.shot("TestE2EDirectTypeSnippet-typed")

	e.pressEnterInPane()
	e.waitScrollback(sid, "Filesystem")
}

func TestE2EDirectTypeMeetsDialog(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.addSnippet("e2e outside", "echo not-in-grammar")
	e.addSnippet("e2e two lines", "echo two-line-$((1+1))\necho second-$((1+1))")

	// Outside the grammar: the badge says so, the dialog opens as before, and nothing is typed until Type.
	e.pump(func() bool { return e.inWindow("e2e outside") && e.inWindow("e2e two lines") }, "the snippets")
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil && e.tt.Focused("Command palette") }, "the palette")
	e.tt.Type("Type snippet: e2e outside")
	e.step()
	if !e.hasLabel("badge Approve first") {
		t.Errorf("no badge on a snippet outside the grammar: %v", e.tt.Texts())
	}
	e.key(0, ui.KeyEnter)
	e.waitApprove()
	e.settle(700 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "not-in-grammar") {
		t.Fatal("something was typed before Type")
	}
	e.click("Cancel")
	e.pump(func() bool { return e.w.approve == nil }, "Cancel")

	e.typeFromPalette("e2e two lines")
	e.waitApprove()
	e.settle(700 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "two-line-") {
		t.Fatal("a two-line snippet was typed before Type")
	}
	e.shot("TestE2EDirectTypeMeetsDialog-dialog")
	e.click("Type")
	e.pump(func() bool { return e.w.approve == nil }, "the dialog to close")
	e.waitScrollback(sid, "\nsecond-2")
	if e.directLogged("snippet") {
		t.Error("a snippet that went through the dialog is logged as direct")
	}
}

func TestE2EDirectTypeAppendsToThePromptLine(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.clickBody("1")
	e.tt.Type("# ") // typed at the prompt, no Enter
	e.step()
	e.waitScrollback(sid, "# ")
	e.typeFromPalette("disk")
	e.pump(func() bool { return strings.Contains(e.scrollback(sid), "df -h") || e.w.approve != nil }, "df -h on the line, or the dialog")
	e.settle(700 * time.Millisecond)
	sb := e.scrollback(sid)
	if e.w.approve != nil {
		t.Fatalf("keel serve chose the dialog for a prompt line that holds text: it should append (no Ctrl-U): %q", sb)
	}
	if !strings.Contains(sb, "# df -h") {
		t.Fatalf("the command was not appended to what was on the prompt line (no Ctrl-U): %q", sb)
	}
	if strings.Contains(sb, "Filesystem") {
		t.Fatal("it ran")
	}
	e.shot("TestE2EDirectTypeAppendsToThePromptLine")
}

func TestE2EDirectTypeInFullScreenProgram(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	file := filepath.Join(dir, "..", "small.txt")
	file, _ = filepath.Abs(file)
	e.clickBody("1")
	e.typeLine("printf 'one\\ntwo\\n' > " + file) // a file this test makes in its own scratch directory
	e.pump(func() bool { _, err := os.Stat(file); return err == nil }, "the scratch file")
	e.typeLine("LESS= less " + file)
	e.pump(func() bool { alt, _ := e.w.focusedPane().screen(); return alt }, "less on the alternate screen")

	e.typeFromPalette("disk")
	e.waitApprove() // the window reads the alternate screen and keeps keel serve out of it
	e.settle(700 * time.Millisecond)
	if sb := e.scrollback(sid); strings.Contains(sb, "df -h") || strings.Contains(sb, "Filesystem") {
		t.Fatalf("the pane received df -h: %q", sb)
	}
	e.shot("TestE2EDirectTypeInFullScreenProgram")
	e.click("Cancel")
	e.pump(func() bool { return e.w.approve == nil }, "Cancel")
	e.clickBody("1")
	e.tt.Type("q")
	e.step()
	e.pump(func() bool { alt, _ := e.w.focusedPane().screen(); return !alt }, "less to end")
}

func TestE2EDirectTypeAgentProposal(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()
	e.agentAsk("what should I run?")
	e.waitCard(1)

	// The fake proposes echo keel-agent-ok: outside the grammar, so the button says a dialog follows and it opens.
	if !e.hasLabel("Type into pane…") {
		t.Fatalf("the button does not say a dialog follows: %v", e.tt.Texts())
	}
	e.click("Type into pane…")
	e.waitApprove()
	e.settle(500 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "keel-agent-ok") {
		t.Fatal("opening the dialog typed")
	}
	e.click("Cancel")
	e.pump(func() bool { return e.w.approve == nil }, "Cancel")

	// The fake cannot propose a command in the grammar, so edit the proposal into one: keel serve rates the edit again.
	e.click("Edit command")
	e.pump(func() bool { return e.tt.Focused("Edit command") }, "the edit field to have the keyboard")
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type("df -h")
	e.step()
	e.click("Save command")
	e.pump(func() bool { return !e.w.agent.props[e.firstProposal()].editing && e.tt.HasText("1 line · 7 bytes") }, "the card to show what keel serve holds")
	e.pump(func() bool { return e.hasLabel("Type into pane") && !e.hasLabel("Type into pane…") }, "the button to name a one-click type")
	e.shot("TestE2EDirectTypeAgentProposal-card")
	e.click("Type into pane")

	e.pump(func() bool { return strings.Contains(e.scrollback(sid), "df -h") }, "df -h on the prompt line")
	e.settle(time.Second)
	e.noDialog("a read-only one-line proposal")
	if sb := e.scrollback(sid); strings.Contains(sb, "Filesystem") {
		t.Fatalf("the command ran without Enter: %q", sb)
	}
	if !e.directLogged("agent.type") {
		t.Errorf("no agent.type log row with source direct: %v", e.logEntries())
	}
	if !e.tt.HasText("Press Enter to run.") {
		t.Errorf("the card does not say Enter is the user's: %v", e.tt.Texts())
	}
	e.pressEnterInPane()
	e.waitScrollback(sid, "Filesystem")
}

func TestE2EDirectTypeFollower(t *testing.T) {
	addr, dir := startServe(t, true)
	a := newWin(t, addr, dir, nil)
	sid := a.openLocal()
	b := newWin(t, addr, dir, nil)
	both := []*win{a, b}
	pumpAll(t, both, func() bool {
		return len(b.w.tabs) == 1 && b.snap().Role == "follower" && len(a.snap().Attached) == 2 && b.inWindow("disk")
	}, "B following with the snippets loaded")
	a.settle(300 * time.Millisecond)
	before := a.scrollback(sid)

	b.key(ui.Cmd, ui.KeyK)
	pumpAll(t, both, func() bool { return b.w.pal != nil && b.tt.Focused("Command palette") }, "B's palette")
	b.tt.Type("Type snippet: disk")
	b.step()
	b.key(0, ui.KeyEnter)
	// B is refused: the dialog is the existing refusal and says why; nothing was asked of keel serve, nothing typed.
	pumpAll(t, both, func() bool { return b.w.approve != nil && b.tt.HasText(msgFollowing) }, "B to be refused")
	for end := time.Now().Add(700 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		a.step()
		b.step()
	}
	if got := a.scrollback(sid); got != before || strings.Contains(got, "df -h") {
		t.Fatalf("A's scrollback changed: %q", got)
	}
	if b.directLogged("snippet") {
		t.Error("a follower's snippet is logged as typed")
	}
	b.shot("TestE2EDirectTypeFollower")
}
