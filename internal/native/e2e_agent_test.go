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

// The agent panel against a real keel serve that answers from its in-process fake (KEEL_AGENT_PROVIDER=fake, set for
// every daemon by startServeAt): nothing here reaches a model provider, and the only key ever stored is made up. What
// the scenarios prove is what reaches the PTY and when: nothing before the approved Type, then exactly the proposal's
// bytes, and the log rows keel serve writes (agent.read, agent.suggest, and agent.type only after approval).

const fakeAgentKey = "fake-key-for-tests-only-0000"

// The fake's destructive and state-changing commands name only these two paths. A test creates the first empty, and
// refuses to start if either exists, so it never touches anything it did not make; the cleanup removes them.
const (
	agentScratchDir  = "/tmp/keel-agent-e2e"
	agentScratchFile = "/tmp/keel-agent-e2e-touched"
)

// scratchPath makes sure path does not exist yet (fails the test if it does), and removes what the test made at the end.
func scratchPath(t *testing.T, path string, dir bool) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Fatalf("%s exists: this test will not touch a path it did not create", path)
	}
	if dir {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { os.Remove(path) }) // empty dir or file: never recursive
}

// putAgentKey stores the made-up key through the API (the scenarios that are not about the Connect card).
func (e *win) putAgentKey() {
	e.t.Helper()
	if code, body := e.raw("PUT", "/api/agent/keys/gemini", map[string]string{"key": fakeAgentKey}); code != 200 {
		e.t.Fatalf("PUT /api/agent/keys/gemini: %d %s", code, body)
	}
}

// openAgent opens the panel and waits for its settings.
func (e *win) openAgent() {
	e.t.Helper()
	e.key(agentMods(), ui.KeyI)
	e.pump(func() bool {
		return e.w.prefs.AgentPanel && e.w.agentState().keyConfigured != nil && e.hasLabel("Message the agent")
	}, "the agent panel with its settings")
}

func (e *win) agentAsk(q string) {
	e.t.Helper()
	e.click("Message the agent")
	e.tt.Type(q)
	e.step()
	e.key(0, ui.KeyEnter)
}

// logKinds counts the log entries by kind.
func (e *win) logKinds() map[string]int {
	e.t.Helper()
	out := map[string]int{}
	for _, l := range e.logEntries() {
		out[l.Kind]++
	}
	return out
}

func (e *win) cards() (n int) {
	for _, s := range e.tt.Texts() {
		if s == "Proposal" {
			n++
		}
	}
	return n
}

// waitCard waits until the thread has n proposal cards.
func (e *win) waitCard(n int) {
	e.t.Helper()
	e.pump(func() bool { return e.cards() >= n }, "a proposal card")
}

func TestE2EAgentAskAndType(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()
	if !e.tt.HasText("Reads 1 of 1 pane") {
		t.Fatalf("the chip: %v", e.tt.Texts())
	}
	e.agentAsk("what should I run?")
	e.waitCard(1)
	for _, s := range []string{"what should I run?", "Read Local Shell · last ", " lines", "Test provider · nothing left this machine", "Run on", "Local Shell", "Read-only", "1 line · 20 bytes", "Edit command", "Copy command", "Type into pane…", "echo keel-agent-ok"} {
		if !e.tt.HasText(s) {
			t.Errorf("the thread lacks %q: %v", s, e.tt.Texts())
		}
	}
	if !e.tt.HasText("I was given 1 pane excerpt and 0 redacted values.") { // the fake's sentence (scripted.go)
		t.Errorf("the answer lacks the fake's sentence: %v", e.tt.Texts())
	}
	e.shot("TestE2EAgentAskAndType-card")

	e.settle(time.Second) // pumping: nothing may reach the shell by itself
	if strings.Contains(e.scrollback(sid), "keel-agent-ok") {
		t.Fatal("the proposal reached the PTY before any Type")
	}
	if k := e.logKinds(); k["agent.read"] == 0 || k["agent.suggest"] == 0 || k["agent.type"] != 0 {
		t.Fatalf("log rows before a Type: %v", k)
	}

	e.click("Type into pane…")
	e.waitApprove()
	if !e.tt.HasText("Local Shell (agent proposal)") || !e.tt.HasText("echo keel-agent-ok") || !e.tt.HasText("^U") {
		t.Errorf("the dialog does not show the bytes: %v", e.tt.Texts())
	}
	e.shot("TestE2EAgentAskAndType-dialog")
	e.settle(500 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "keel-agent-ok") {
		t.Fatal("opening the dialog typed")
	}
	e.click("Cancel")
	e.pump(func() bool { return !e.w.approveOpen() }, "Cancel to close the dialog")
	e.settle(500 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "keel-agent-ok") || e.logKinds()["agent.type"] != 0 {
		t.Fatal("Cancel typed, or logged a Type")
	}

	e.click("Type into pane…")
	e.waitApprove()
	e.click("Type")
	e.pump(func() bool { return !e.w.approveOpen() }, "the dialog to close after Type")
	e.waitScrollback(sid, "\nkeel-agent-ok") // the output line: the echoed command line has "echo " before it
	e.pump(func() bool { return e.tt.HasText("Typed into Local Shell.") }, "the card to say it typed")
	if k := e.logKinds(); k["agent.type"] != 1 {
		t.Fatalf("log rows after the Type: %v", k)
	}
	e.shot("TestE2EAgentAskAndType-typed")
}

func TestE2EAgentScope(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	e.openLocal()
	e.newSession()
	e.pump(func() bool { return e.w.tabCount() == 2 && e.sidOf(1) != "" }, "a second tab with a session")
	e.openAgent()
	e.pump(func() bool { return e.tt.HasText("Reads 1 of 2 panes") }, "the chip: the focused pane of two")

	reads := func() int { return e.logKinds()["agent.read"] }
	// Both panes: the trace names both.
	e.click("Choose what the agent can read")
	e.pump(func() bool { return e.hasLabel("dialog Panes the agent reads") }, "the picker")
	var pickLabels []string
	for _, l := range e.tt.Texts() {
		if strings.HasPrefix(l, "pick ") {
			pickLabels = append(pickLabels, l)
		}
	}
	if len(pickLabels) != 2 {
		t.Fatalf("the picker's rows: %v", pickLabels)
	}
	for _, l := range pickLabels { // the row of the pane that is not in scope yet (two rows read the same: the session id tells them apart)
		inScope := false
		for _, sid := range e.w.agentState().scope {
			inScope = inScope || strings.HasSuffix(l, sid)
		}
		if !inScope {
			e.click(l)
		}
	}
	e.pump(func() bool { return len(e.w.agentState().scope) == 2 }, "both panes in scope")
	e.click("Choose what the agent can read")
	e.agentAsk("what should I run?")
	e.waitCard(1)
	n := 0
	for _, s := range e.tt.Texts() {
		if strings.HasPrefix(s, "Read Local Shell · last ") {
			n++
		}
	}
	if n != 2 || !e.tt.HasText("I was given 2 pane excerpts and") {
		t.Fatalf("the trace names %d panes: %v", n, e.tt.Texts())
	}
	e.shot("TestE2EAgentScope-both")

	// None: nothing is read.
	before := reads()
	e.click("Choose what the agent can read")
	e.pump(func() bool { return e.hasLabel("dialog Panes the agent reads") }, "the picker")
	e.click("None")
	e.pump(func() bool { return e.tt.HasText("Reads 0 of 2 panes") }, "the chip after None")
	e.agentAsk("no command, just hello")
	e.pump(func() bool { return e.tt.HasText("I was given 0 pane excerpts and") }, "an answer that read nothing")
	// One entry: the request itself, which keel serve logs for every ask (it names no pane).
	if reads() != before+1 {
		t.Fatalf("an ask with no pane in scope wrote %d agent.read entries, want 1 for the request alone", reads()-before)
	}

	// "Agent can read" off for Local Shell: the picker marks it, it cannot be picked, and the ask reads nothing.
	if err := e.tt.RightClick("host Local Shell"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Agent can read"); err != nil {
		t.Fatal(err)
	}
	if h, ok := e.hostNamed("Local Shell"); !ok || h.AgentReadable == nil || *h.AgentReadable {
		e.pump(func() bool {
			h, ok := e.hostNamed("Local Shell")
			return ok && h.AgentReadable != nil && !*h.AgentReadable
		}, "keel serve to hold reading as off")
	}
	e.pump(func() bool {
		for _, h := range e.w.hosts {
			if h.AuthType == "local" {
				return h.AgentReadable != nil && !*h.AgentReadable
			}
		}
		return false
	}, "the window to know reading is off")
	e.click("Choose what the agent can read")
	e.pump(func() bool { return e.tt.HasText("Agent can't read") }, "Agent can't read in the picker")
	e.shot("TestE2EAgentScope-cannot-read")
	for _, l := range e.tt.Texts() {
		if strings.HasPrefix(l, "pick ") {
			e.click(l)
		}
	}
	if len(e.w.agentState().scope) != 0 {
		t.Fatalf("a host the agent cannot read was picked: %v", e.w.agentState().scope)
	}
	e.click("Choose what the agent can read")
	before = reads()
	answers := e.answerCount()
	e.agentAsk("no command, again")
	e.pump(func() bool { return e.answerCount() > answers }, "an answer that read nothing")
	e.settle(300 * time.Millisecond)
	// One entry, as above: the request itself, never the pane.
	if reads() != before+1 {
		t.Fatalf("keel serve read a pane whose host the agent cannot read (%d -> %d)", before, reads())
	}

	// Back on: readable again.
	if err := e.tt.RightClick("host Local Shell"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Agent can read"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool {
		for _, h := range e.w.hosts {
			if h.AuthType == "local" {
				return h.AgentReadable == nil || *h.AgentReadable
			}
		}
		return false
	}, "reading back on")
}

// answerCount is how many answers the thread draws.
func (e *win) answerCount() (n int) {
	for _, s := range e.tt.Texts() {
		if s == "agent answer" {
			n++
		}
	}
	return n
}

func TestE2EAgentShiftRule(t *testing.T) {
	scratchPath(t, agentScratchDir, true)
	scratchPath(t, agentScratchFile, false)
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()

	e.agentAsk("delete the scratch folder")
	e.waitCard(1)
	if !e.tt.HasText("Destructive") || !e.tt.HasText("Destructive. Hold "+shiftWord()+" when you press Type.") {
		t.Fatalf("the card does not say it needs ⇧: %v", e.tt.Texts())
	}
	e.shot("TestE2EAgentShiftRule-destructive")
	e.click("Type into pane…")
	e.settle(300 * time.Millisecond)
	if e.w.approveOpen() || !e.tt.HasText("Not typed. Destructive: hold "+shiftWord()+" and press Type.") {
		t.Fatalf("a plain Type on a destructive proposal was not refused in the card: %v", e.tt.Texts())
	}
	if _, err := os.Stat(agentScratchDir); err != nil || strings.Contains(e.scrollback(sid), "rm -rf") {
		t.Fatal("something was typed or deleted by a plain Type")
	}
	if err := e.tt.ClickWith(ui.Shift, "Type into pane…"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.waitApprove()
	e.settle(300 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "rm -rf") {
		t.Fatal("opening the dialog typed")
	}
	e.click("Type")
	e.pump(func() bool { return !e.w.approveOpen() }, "the dialog to close after Type")
	e.waitScrollback(sid, "rm -rf "+agentScratchDir)
	e.pump(func() bool { _, err := os.Stat(agentScratchDir); return os.IsNotExist(err) }, "the shell to remove its own scratch folder")
	if _, err := os.Stat(agentScratchFile); err == nil {
		t.Fatal("a path outside the destructive command's was created")
	}
	if e.logKinds()["agent.type"] != 1 {
		t.Fatalf("log rows: %v", e.logKinds())
	}

	// Make Local Shell a production host in the host dialog; a state change then needs ⇧ too.
	e.setLocalProduction(true)
	e.click("New conversation") // one card at a time: the destructive one is spent
	e.agentAsk("touch the marker file")
	e.waitCard(1)
	if !e.tt.HasText("Production") || !e.tt.HasText("Changes state on a production host. Hold "+shiftWord()+" when you press Type.") {
		t.Fatalf("the production card does not ask for ⇧: %v", e.tt.Texts())
	}
	e.click("Type into pane…")
	e.settle(300 * time.Millisecond)
	if e.w.approveOpen() {
		t.Fatal("a plain Type on a state change on a production host opened the dialog")
	}
	if _, err := os.Stat(agentScratchFile); err == nil {
		t.Fatal("the marker file was created without an approval")
	}
	e.shot("TestE2EAgentShiftRule-production")
	e.setLocalProduction(false) // restored: the scratch daemon is thrown away, but the flag goes back as it was
}

// setLocalProduction turns "Treat as production" on or off for Local Shell in its Edit host dialog.
func (e *win) setLocalProduction(on bool) {
	e.t.Helper()
	if err := e.tt.RightClick("host Local Shell"); err != nil {
		e.t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Edit"); err != nil {
		e.t.Fatal(err)
	}
	e.pump(func() bool { return e.hasLabel("Treat as production") }, "the Edit host dialog")
	if e.w.hostDlg == nil || e.w.hostDlg.f.production == on {
		e.t.Fatalf("the dialog's production flag is already %v", on)
	}
	// The row's text and its switch share the label, and Find gives the text: press the switch at the right end of the row.
	row, _ := e.tt.Find("Treat as production")
	dlg, _ := e.tt.Find("dialog Edit host")
	e.tt.ClickAt(dlg.X+dlg.W-space4-18, row.Y+row.H+space2) // the switch is centred on the name and its note: below the name's line
	e.step()
	if e.w.hostDlg == nil || e.w.hostDlg.f.production != on {
		e.t.Fatalf("the switch did not turn production %v", on)
	}
	e.click("Save host")
	e.pump(func() bool {
		for _, h := range e.w.hosts {
			if h.AuthType == "local" {
				return (h.Production != nil && *h.Production) == on
			}
		}
		return false
	}, "the window to know the production flag")
}

func TestE2EAgentEditAndCopy(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()
	e.agentAsk("what should I run?")
	e.waitCard(1)

	e.click("Edit command")
	e.pump(func() bool { return e.tt.Focused("Edit command") }, "the edit field to have the keyboard")
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type("echo edited-ok")
	e.step()
	e.click("Save command")
	e.pump(func() bool {
		return e.tt.HasText("1 line · 16 bytes") && !e.w.agentState().props[e.firstProposal()].editing
	}, "the card to show what keel serve holds")
	if e.tt.HasText("keel-agent-ok") {
		t.Fatal("the card still shows the original command")
	}
	e.shot("TestE2EAgentEditAndCopy-edited")

	e.click("Copy command")
	e.pump(func() bool { return e.tt.Clipboard() == "echo edited-ok" }, "the command on the clipboard")

	e.click("Type into pane…")
	e.waitApprove()
	if !e.tt.HasText("echo edited-ok") || e.tt.HasText("keel-agent-ok") {
		t.Fatalf("the dialog does not show the edited bytes: %v", e.tt.Texts())
	}
	e.click("Type")
	e.pump(func() bool { return !e.w.approveOpen() }, "the dialog to close")
	e.waitScrollback(sid, "\nedited-ok")
	if strings.Contains(e.scrollback(sid), "keel-agent-ok") {
		t.Fatal("the original command was typed")
	}
}

func (e *win) firstProposal() string {
	for id := range e.w.agentState().props {
		return id
	}
	return ""
}

func TestE2EAgentFollowerHasNoType(t *testing.T) {
	addr, dir := startServe(t, true)
	a := newWin(t, addr, dir, nil)
	a.putAgentKey()
	sid := a.openLocal()
	b := newWin(t, addr, dir, nil)
	both := []*win{a, b}
	pumpAll(t, both, func() bool { return b.w.tabCount() == 1 && b.snap().Role == "follower" && b.paneSID("1") == sid }, "B following A's session")
	b.key(agentMods(), ui.KeyI)
	pumpAll(t, both, func() bool {
		return b.w.prefs.AgentPanel && b.w.agentState().keyConfigured != nil && b.hasLabel("Message the agent")
	}, "B's agent panel")
	b.agentAsk("what should I run?")
	pumpAll(t, both, func() bool { return b.cards() >= 1 }, "a proposal card in B")
	if b.hasLabel("Type into pane…") {
		t.Fatal("B follows the pane and is offered Type")
	}
	if !b.tt.HasText("You follow this pane. The agent cannot type into it.") {
		t.Errorf("the card does not say why: %v", b.tt.Texts())
	}
	pumpAll(t, both, func() bool { return true }, "a frame")
	b.shot("TestE2EAgentFollowerHasNoType-follower")
	if strings.Contains(a.scrollback(sid), "keel-agent-ok") || a.logKinds()["agent.type"] != 0 {
		t.Fatal("something was typed")
	}
}

// The panel's sash: a drag widens the panel and the terminal pane gives way (keel serve sees the narrower terminal), the width
// is kept in keel-native.json and a window opened later on the same data starts at it.
func TestE2EAgentPanelSashWidthIsKept(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()
	panel := func() float32 { p, _ := e.tt.Find("agent panel"); return p.W }
	cols := func() int {
		for _, s := range e.sessions() {
			if s.ID == sid {
				return s.Cols
			}
		}
		return 0
	}
	e.settle(300 * time.Millisecond)
	before := cols()
	sash, ok := e.tt.Find("Resize agent panel")
	if !ok {
		t.Fatal("no sash")
	}
	x, y := sash.X+sash.W/2, sash.Y+sash.H/2
	e.tt.Press(x, y)
	e.tt.Move(x-60, y)
	e.tt.Move(x-120, y)
	e.step()
	e.tt.Release(x-120, y)
	e.pump(func() bool { return panel() > sizeAgent+100 && loadPrefs(dir).AgentWidth > sizeAgent+100 }, "the panel to be wider and its width kept")
	e.pump(func() bool { return cols() < before }, "the terminal to give way: fewer columns on keel serve")
	e.shot("TestE2EAgentPanelSash")
	kept := loadPrefs(dir).AgentWidth
	e.w.closeWindow()

	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { p, ok := e2.tt.Find("agent panel"); return ok && p.W > kept-1 && p.W < kept+1 }, "a new window to open the panel at the kept width")
}

func TestE2EAgentNoKey(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.key(agentMods(), ui.KeyI)
	e.pump(func() bool { return e.tt.HasText("Connect Gemini") }, "the Connect card")
	for _, s := range []string{"Gemini API key", "Connect", "On the free tier of the Gemini API"} {
		if !e.tt.HasText(s) {
			t.Errorf("the card lacks %q", s)
		}
	}
	e.shot("TestE2EAgentNoKey-connect")
	e.click("Gemini API key input")
	e.tt.Type(fakeAgentKey)
	e.step()
	if e.tt.HasText(fakeAgentKey) {
		t.Fatal("the key is drawn in the clear")
	}
	e.click("Connect")
	e.pump(func() bool { return e.tt.HasText("Ask about your sessions") && e.w.agentState().keyField == "" }, "the card to give way to the thread, the field emptied")
	e.agentAsk("what should I run?")
	e.waitCard(1)
	e.settle(300 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "keel-agent-ok") {
		t.Fatal("an answer typed")
	}
	// The key is in no log, no prefs file and no screen. (The vault holds it, by design, and is not read here.)
	root := filepath.Dir(dir) // the test's scratch directory: data/ and the daemon's own log
	for _, f := range []string{"keel.log", "keel-native.json", "keel-native-workspace.json"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && strings.Contains(string(b), fakeAgentKey) {
			t.Fatalf("the key is in %s", f)
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "serve.log")); err == nil && strings.Contains(string(b), fakeAgentKey) {
		t.Fatal("the key is in the daemon's log")
	}
	if strings.Contains(e.texts(), fakeAgentKey) {
		t.Fatal("the key is on screen")
	}
	for _, l := range e.logEntries() {
		if strings.Contains(l.Message, fakeAgentKey) {
			t.Fatal("the key is in GET /api/logs")
		}
	}
}
