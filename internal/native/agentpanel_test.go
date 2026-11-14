package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The agent panel on the fake backend: what an ask sends, what the thread shows, and that a proposal reaches a session
// only through Approve before type, with the proposal's bytes, and keel serve's refusal final.

// agentRig is a window with one session this client drives, a saved key, and the panel open.
func agentRig(t *testing.T) *rig {
	t.Helper()
	r := approveRig(t)
	r.w.useDataDir(t, t.TempDir())
	r.fb.agent.settings.Keys = map[string]struct {
		Configured bool `json:"configured"`
	}{"gemini": {true}}
	r.tt.Key(agentMods(), ui.KeyI)
	r.tt.Frame()
	if !r.w.prefs.AgentPanel {
		t.Fatal("the shortcut did not open the panel")
	}
	return r
}

// ui/agentpanel.js width: the sash on the panel's left edge widens it by dragging left, keeps the width in keel-native.json,
// steps with the arrow keys, goes back to the default on a double click, and the panel is never narrower than size-agent
// nor wider than half the window.
func TestTheAgentPanelsSashResizesItAndTheWidthIsKept(t *testing.T) {
	for _, c := range []struct{ width, win, want float32 }{
		{0, 1280, sizeAgent}, {200, 1280, sizeAgent}, {440.4, 1280, 440}, {900, 1280, 640}, {900, 600, sizeAgent}, {500, 1000, 500},
	} {
		if got := clampAgentWidth(c.width, c.win); got != c.want {
			t.Errorf("clampAgentWidth(%v, %v) = %v, want %v", c.width, c.win, got, c.want)
		}
	}
	r := agentRig(t)
	width := func() float32 { p, _ := r.tt.Find("agent panel"); return p.W }
	near := func(got, want float32) bool { return got > want-1 && got < want+1 }
	if got := width(); !near(got, sizeAgent) {
		t.Fatalf("the panel is %v wide, want %v", got, float32(sizeAgent))
	}
	sash, ok := r.tt.Find("Resize agent panel")
	if !ok {
		t.Fatal("no sash")
	}
	x, y := sash.X+sash.W/2, sash.Y+sash.H/2
	r.tt.Press(x, y)
	r.tt.Move(x-30, y)
	r.tt.Move(x-60, y)
	r.tt.Frame()
	r.tt.Release(x-60, y)
	r.tt.Frame()
	r.tt.Frame()
	if got := width(); !near(got, sizeAgent+60) {
		t.Fatalf("after a drag 60 px left the panel is %v wide, want %v", got, float32(sizeAgent+60))
	}
	if got := loadPrefs(r.w.dataDir).AgentWidth; !near(got, sizeAgent+60) {
		t.Errorf("keel-native.json holds %v", got)
	}
	// Dragged far left it stops at half the window; dragged far right, at size-agent.
	sash, _ = r.tt.Find("Resize agent panel")
	x, y = sash.X+sash.W/2, sash.Y+sash.H/2
	r.tt.Press(x, y)
	r.tt.Move(x-700, y)
	r.tt.Move(x-900, y)
	r.tt.Frame()
	r.tt.Release(x-900, y)
	r.tt.Frame()
	r.tt.Frame()
	if got := width(); !near(got, 640) {
		t.Errorf("dragged far left the panel is %v wide, want half the window (640)", got)
	}
	// ← widens by size-agent-step when the sash has the keyboard, ⇧← by four, Home goes back to the narrowest.
	r.w.setAgentWidth(sizeAgent)
	r.tt.Frame()
	r.tt.Click("Resize agent panel")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyLeft)
	r.tt.Frame()
	if got := width(); !near(got, sizeAgent+sizeAgentStep) {
		t.Errorf("← gives %v, want %v", got, float32(sizeAgent+sizeAgentStep))
	}
	r.tt.Key(ui.Shift, ui.KeyLeft)
	r.tt.Frame()
	if got := width(); !near(got, sizeAgent+5*sizeAgentStep) {
		t.Errorf("⇧← gives %v, want %v", got, float32(sizeAgent+5*sizeAgentStep))
	}
	r.tt.Key(0, ui.KeyRight)
	r.tt.Frame()
	if got := width(); !near(got, sizeAgent+4*sizeAgentStep) {
		t.Errorf("→ gives %v", got)
	}
	r.tt.Key(0, ui.KeyHome)
	r.tt.Frame()
	if got := width(); !near(got, sizeAgent) || loadPrefs(r.w.dataDir).AgentWidth != 0 {
		t.Errorf("Home gives %v, kept %v", got, loadPrefs(r.w.dataDir).AgentWidth)
	}
	// A double click puts the default back.
	r.w.setAgentWidth(500)
	r.tt.Frame()
	sash, _ = r.tt.Find("Resize agent panel")
	r.tt.ClickAt(sash.X+sash.W/2, sash.Y+sash.H/2)
	r.tt.ClickAt(sash.X+sash.W/2, sash.Y+sash.H/2)
	r.tt.Frame()
	if got := width(); !near(got, sizeAgent) {
		t.Errorf("a double click gives %v", got)
	}
	// The width comes back with the window (start reads it), and nothing else in the file changed.
	r.w.setAgentWidth(520)
	w2 := newWorkspace(r.fb)
	w2.dataDir = r.w.dataDir
	w2.prefs = loadPrefs(w2.dataDir) // what RunWith does
	w2.work, w2.post, w2.mkBody = func(fn func()) { fn() }, func(fn func()) { fn() }, placeholder(&r.released)
	w2.start()
	if !near(w2.prefs.AgentWidth, 520) || !w2.prefs.AgentPanel {
		t.Errorf("a new window has width %v, open %v", w2.prefs.AgentWidth, w2.prefs.AgentPanel)
	}
}

func agentMods() ui.Modifiers {
	mods, _ := shortcutOf("agent.toggle")
	return mods
}

func (r *rig) ask(t *testing.T, q string) {
	t.Helper()
	if err := r.tt.Click("Message the agent"); err != nil {
		t.Fatal(err)
	}
	r.tt.Type(q)
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
}

func okProposal() keelclient.Proposal {
	return keelclient.Proposal{ID: "p1", SessionID: "s1", HostID: "h-api", Host: "api-1", Command: "echo keel-agent-ok", Bytes: "\x15echo keel-agent-ok\n", Why: "Prints a marker.", Risk: "readonly"}
}

// answers makes the fake answer every ask with one answer and the given proposals.
func (r *rig) answers(ps ...keelclient.Proposal) {
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{ConversationID: req.ConversationID, Title: "What to run", Answer: "I was given 1 pane excerpt(s) and 0 redacted value(s).",
			Trace:     []keelclient.TraceStep{{SessionID: "s1", Host: "api-1", Status: "read", Lines: 14}},
			Proposals: ps, TestProvider: true}, nil
	}
}

func TestTheShortcutTheButtonAndThePaletteToggleThePanelAndItIsRemembered(t *testing.T) {
	r := approveRig(t)
	dir := t.TempDir()
	r.w.useDataDir(t, dir)
	if r.w.prefs.AgentPanel || r.tt.HasText("Reads ") {
		t.Fatal("the panel starts closed")
	}
	for _, e := range []string{"Agent"} {
		if !r.has(e) {
			t.Errorf("no %s button in the title bar", e)
		}
	}
	saved := func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "keel-native.json"))
		var f struct{ AgentPanel bool }
		json.Unmarshal(b, &f)
		return f.AgentPanel
	}
	r.tt.Key(agentMods(), ui.KeyI)
	r.tt.Frame()
	if !r.w.prefs.AgentPanel || !r.tt.HasText("Reads 1 of 1 pane") || !saved() || !r.tt.Focused("Message the agent") {
		t.Fatalf("open %v saved %v: the panel is not open with the keyboard in the composer", r.w.prefs.AgentPanel, saved())
	}
	r.tt.Key(agentMods(), ui.KeyI)
	r.tt.Frame()
	if r.w.prefs.AgentPanel || r.tt.HasText("Reads ") || saved() {
		t.Fatal("the shortcut did not close it and save that")
	}
	r.click(t, "Agent") // the button
	if !r.w.prefs.AgentPanel {
		t.Fatal("the Agent button does not open it")
	}
	r.click(t, "Close agent")
	if r.w.prefs.AgentPanel || saved() {
		t.Fatal("Close agent does not close and save")
	}
	r.tt.Key(ui.Cmd, ui.KeyK)
	r.tt.Frame()
	r.tt.Type("toggle agent")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if !r.w.prefs.AgentPanel {
		t.Fatal("the palette's Toggle agent panel does not open it")
	}
	// A new window reads it back.
	w2 := newWorkspace(r.fb)
	w2.dataDir = dir
	if !loadPrefs(dir).AgentPanel {
		t.Fatal("keel-native.json does not say the panel is open")
	}
	_ = w2
}

func TestAWindowOpensWithThePanelAsItWasLeft(t *testing.T) {
	dir := t.TempDir()
	for _, open := range []bool{true, false} {
		if err := savePrefs(dir, map[string]any{"agentPanel": open, "termSize": 14}); err != nil {
			t.Fatal(err)
		}
		fb := okBackend()
		w := newWorkspace(fb)
		w.dataDir, w.prefs = dir, loadPrefs(dir) // what RunWith does
		w.work, w.post = func(fn func()) { fn() }, func(fn func()) { fn() }
		w.mkBody = placeholder(new(int))
		w.start()
		if w.prefs.AgentPanel != open {
			t.Errorf("saved %v: the window opened with the panel %v", open, w.prefs.AgentPanel)
		}
	}
	// A file that is not there, or says nothing, is closed.
	if loadPrefs(t.TempDir()).AgentPanel {
		t.Error("no file: the panel is open")
	}
}

func TestAJoinedTabHasNoPanelNoButtonAndTheShortcutSaysWhy(t *testing.T) {
	jr := newJoinRig(t)
	jr.w.useDataDir(t, t.TempDir())
	jr.tt.Key(agentMods(), ui.KeyI)
	jr.tt.Frame()
	if !jr.w.prefs.AgentPanel {
		t.Fatal("setup: the panel is not open")
	}
	jr.join(t, joinTestLink)
	if !jr.w.joinedActive() {
		t.Fatal("setup: no joined tab")
	}
	if jr.has("Agent") || jr.tt.HasText("Reads ") {
		t.Error("a joined tab shows the Agent button or the panel")
	}
	jr.tt.Key(agentMods(), ui.KeyI)
	jr.tt.Frame()
	if !jr.tt.HasText(msgAgentJoined) || msgAgentJoined != "The agent is off in joined sessions" {
		t.Errorf("the shortcut did not say why: %v", jr.tt.Texts())
	}
	if !jr.w.prefs.AgentPanel {
		t.Error("the shortcut changed the panel instead of saying why")
	}
}

func twoSessionRig(t *testing.T) *rig {
	t.Helper()
	r := agentRig(t)
	r.click(t, "host staging-web")
	st := drivingState
	r.fb.conns[1].st = &st
	r.tt.Frame()
	return r
}

func TestTheScopeChipCountsAndThePickerRefusesAHostTheAgentCannotRead(t *testing.T) {
	r := twoSessionRig(t)
	if !r.tt.HasText("Reads 1 of 2 panes") || !slices.Equal(r.w.agent.scope, []string{"s2"}) {
		t.Fatalf("chip %v, scope %v: the default is the focused pane", r.tt.Texts(), r.w.agent.scope)
	}
	r.click(t, "Choose what the agent can read")
	if !r.has("dialog Panes the agent reads") {
		t.Fatal("no picker")
	}
	for _, s := range []string{"Panes the agent reads", "Focused pane", "Every pane in tab", "None"} {
		if !r.tt.HasText(s) {
			t.Errorf("the picker lacks %q", s)
		}
	}
	r.click(t, "pick api-1 · api-1")
	if !slices.Equal(r.w.agent.scope, []string{"s2", "s1"}) && !slices.Equal(r.w.agent.scope, []string{"s1", "s2"}) {
		t.Errorf("scope %v after picking the first", r.w.agent.scope)
	}
	if !r.tt.HasText("Reads 2 of 2 panes") {
		t.Error("the chip did not follow")
	}
	r.click(t, "None")
	if len(r.w.agent.scope) != 0 || !r.tt.HasText("Reads 0 of 2 panes") {
		t.Errorf("None: %v", r.w.agent.scope)
	}
	// Reading turned off for staging-web: it is marked, it cannot be picked, and the scope drops it.
	off := false
	for i := range r.w.hosts {
		if r.w.hosts[i].ID == "h-web" {
			r.w.hosts[i].AgentReadable = &off
		}
	}
	r.w.setHosts(r.w.hosts)
	r.tt.Frame()
	r.click(t, "Choose what the agent can read")
	if !r.tt.HasText("Agent can't read") {
		t.Fatal("the picker does not mark the host that cannot be read")
	}
	r.click(t, "pick staging-web · staging-web")
	r.click(t, "Choose what the agent can read")
	if slices.Contains(r.w.agent.scope, "s2") {
		t.Fatalf("a host the agent cannot read was picked: %v", r.w.agent.scope)
	}
}

func TestEveryPaneInTabAndFocusedPanePresets(t *testing.T) {
	r := agentRig(t)
	r.w.splitFocused(0)
	r.tt.Frame()
	if len(r.fb.conns) != 2 {
		t.Fatalf("%d sessions", len(r.fb.conns))
	}
	st := drivingState
	r.fb.conns[1].st = &st
	r.tt.Frame()
	r.click(t, "Choose what the agent can read")
	r.click(t, "Every pane in tab")
	if len(r.w.agent.scope) != 2 || !r.tt.HasText("Reads 2 of 2 panes") {
		t.Errorf("tab preset: %v", r.w.agent.scope)
	}
	r.click(t, "Choose what the agent can read")
	r.click(t, "Focused pane")
	if len(r.w.agent.scope) != 1 {
		t.Errorf("focused preset: %v", r.w.agent.scope)
	}
}

func TestAnAskSendsTheQuestionAndExactlyTheChosenPaneIdsAndNoPaneText(t *testing.T) {
	r := twoSessionRig(t)
	r.answers(okProposal())
	r.click(t, "Choose what the agent can read")
	r.click(t, "None") // a preset closes the picker
	r.click(t, "Choose what the agent can read")
	r.click(t, "pick api-1 · api-1") // a pick keeps it open
	r.click(t, "Choose what the agent can read")
	r.ask(t, "what should I run?")
	if len(r.fb.agent.asks) != 1 {
		t.Fatalf("%d asks", len(r.fb.agent.asks))
	}
	a := r.fb.agent.asks[0]
	if a.Question != "what should I run?" || !slices.Equal(a.Panes, []string{"s1"}) || a.ConversationID == "" {
		t.Fatalf("ask %+v", a)
	}
	b, _ := json.Marshal(a)
	var keys map[string]any
	json.Unmarshal(b, &keys)
	if len(keys) != 3 {
		t.Fatalf("the ask carries %v: only the conversation, the question and the session ids may go", keys)
	}
	if r.fb.conns[0].wrote != 0 || r.fb.conns[1].wrote != 0 {
		t.Error("an ask wrote to a session")
	}
	// A second ask continues the conversation.
	r.ask(t, "and then?")
	if len(r.fb.agent.asks) != 2 || r.fb.agent.asks[1].ConversationID != a.ConversationID {
		t.Errorf("asks %+v", r.fb.agent.asks)
	}
}

func TestTheThreadShowsTheQuestionTheTraceTheAnswerAndTheCard(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.ask(t, "what should I run?")
	for _, s := range []string{"what should I run?", "Read api-1 · last 14 lines", "Test provider · nothing left this machine", "Run on", "api-1", "Read-only", "1 line · 20 bytes", "Prints a marker.", "What to run"} {
		if !r.tt.HasText(s) {
			t.Errorf("no %q in %v", s, r.tt.Texts())
		}
	}
	if !r.tt.HasText("I was given 1 pane excerpt(s)") || !r.has("Proposal") || !r.has("Type into pane…") || !r.has("Edit command") || !r.has("Copy command") {
		t.Errorf("the answer or the card is not drawn: %v", r.tt.Texts())
	}
	if !r.tt.HasText("^U") || !r.tt.HasText("echo keel-agent-ok") || !r.tt.HasText("↵") {
		t.Errorf("the exact bytes are not drawn with their glyphs: %v", r.tt.Texts())
	}
	if len(r.typed()) != 0 {
		t.Fatal("an answer typed")
	}
}

func TestAnErrorTheCutNoticeAndNoPaneReadAreShownAsTheAppShowsThem(t *testing.T) {
	r := agentRig(t)
	r.fb.agent.askReply = func(keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{}, &keelclient.StatusError{Code: 502, Status: "502 Bad Gateway", Msg: "quota exceeded"}
	}
	r.ask(t, "hi")
	if !r.tt.HasText("The agent could not answer.") || !r.tt.HasText("quota exceeded") {
		t.Errorf("%v", r.tt.Texts())
	}
	r.fb.agent.askReply = func(keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{Answer: "Partial", Cut: "length"}, nil
	}
	r.ask(t, "again")
	if !r.tt.HasText("Answer cut off at the length limit. Ask the agent to continue.") || !r.tt.HasText("Read no pane") {
		t.Errorf("%v", r.tt.Texts())
	}
}

func TestStopCancelsTheAskInFlight(t *testing.T) {
	r := agentRig(t)
	posts := make(chan func(), 8)
	r.w.work = func(fn func()) { go fn() }
	r.w.post = func(fn func()) { posts <- fn }
	r.fb.agent.askWait = make(chan struct{})
	r.ask(t, "slow one")
	select {
	case <-r.fb.agent.askWait:
	case <-time.After(2 * time.Second):
		t.Fatal("the ask never started")
	}
	r.tt.Frame()
	if r.w.agent.busy == nil || !r.tt.HasText("Asking without reading a pane") && !r.tt.HasText("Reading 1 pane and asking") || !r.has("Stop") {
		t.Fatalf("no Stop while asking: %v", r.tt.Texts())
	}
	r.click(t, "Stop")
	select {
	case fn := <-posts:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel the ask")
	}
	r.tt.Frame()
	if r.w.agent.busy != nil || !r.tt.HasText("Stopped") || r.has("Stop") {
		t.Errorf("after Stop: %v", r.tt.Texts())
	}
}

func TestNewConversationStartsOverWithANewId(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.ask(t, "one")
	first := r.fb.agent.asks[0].ConversationID
	r.click(t, "New conversation")
	if len(r.w.agent.turns) != 0 || r.w.agent.title != "" || r.tt.HasText("one") {
		t.Error("the conversation was not cleared")
	}
	r.ask(t, "two")
	if r.fb.agent.asks[1].ConversationID == first {
		t.Error("the new conversation reuses the id")
	}
}

func TestCopyPutsTheCommandOnTheClipboardNotTheBytes(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.ask(t, "q")
	r.click(t, "Copy command")
	r.tt.Frame()
	if got := r.tt.Clipboard(); got != "echo keel-agent-ok" {
		t.Fatalf("clipboard %q", got)
	}
	if len(r.typed()) != 0 {
		t.Error("Copy typed")
	}
}

func TestTypeOpensApproveBeforeTypeAndDoesNotTypeItself(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.ask(t, "q")
	r.click(t, "Type into pane…")
	if r.w.approve == nil || !r.has("dialog "+approveTitle) {
		t.Fatal("no Approve before type dialog")
	}
	if len(r.typed()) != 0 || r.fb.conns[0].wrote != 0 {
		t.Fatalf("opening the dialog typed %v", r.typed())
	}
	for _, s := range []string{approveTitle, "Session:", "api-1 (agent proposal)", "^U", "echo keel-agent-ok", "Cancel", "Type"} {
		if !r.tt.HasText(s) {
			t.Errorf("the dialog lacks %q in %v", s, r.tt.Texts())
		}
	}
	if !r.tt.Focused("Cancel") {
		t.Error("the keyboard does not start on Cancel")
	}
	// Cancel: nothing sent, the card says nothing.
	r.click(t, "Cancel")
	if r.w.approve != nil || len(r.typed()) != 0 || r.w.agent.props["p1"].status != nil {
		t.Fatalf("cancel: approve %v typed %v", r.w.approve, r.typed())
	}
	// Type: exactly the proposal's bytes, with the reference to it and no ⇧.
	r.click(t, "Type into pane…")
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15echo keel-agent-ok\n"}) {
		t.Fatalf("typed %q", got)
	}
	ref := r.fb.conns[0].agentRefs[0]
	if ref == nil || ref.ConversationID != r.fb.agent.asks[0].ConversationID || ref.ProposalID != "p1" || ref.Shift {
		t.Fatalf("reference %+v", ref)
	}
	if r.w.approve != nil || !r.tt.HasText("Typed into api-1.") {
		t.Errorf("the card does not say it typed: %v", r.tt.Texts())
	}
	if r.fb.conns[0].wrote != 0 {
		t.Error("the keystroke path was used")
	}
}

func TestAServerRefusalIsShownOnTheCardAndNeverRetried(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.ask(t, "q")
	r.fb.conns[0].injectErr = fmt.Errorf("the bytes are not the ones shown")
	r.click(t, "Type into pane…")
	r.click(t, "Type")
	if len(r.typed()) != 1 {
		t.Fatalf("typed %v", r.typed())
	}
	r.tt.Frame()
	if !r.tt.HasText("the bytes are not the ones shown") || r.tt.HasText("Typed into api-1.") || r.w.approve != nil {
		t.Errorf("%v", r.tt.Texts())
	}
	r.settle()
	if len(r.typed()) != 1 {
		t.Fatalf("the refused Type was tried again: %v", r.typed())
	}
}

// settle runs a few frames: nothing may happen by itself.
func (r *rig) settle() {
	for range 5 {
		r.tt.Frame()
	}
}

func TestEditSavesThroughTheServerAndWhatIsTypedIsWhatTheServerHolds(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.fb.agent.editReply = func(command string) (keelclient.Proposal, error) {
		// keel serve holds its own version of the edit: it is what the card shows and what is typed.
		p := okProposal()
		p.Command, p.Bytes, p.Edited = command, "\x15"+command+"\n", true
		return p, nil
	}
	r.ask(t, "q")
	r.click(t, "Edit command")
	if !r.tt.Focused("Edit command") {
		t.Error("the edit field does not have the keyboard")
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("echo edited-ok")
	r.tt.Frame()
	if len(r.fb.agent.edits) != 0 {
		t.Fatal("typing in the field called keel serve")
	}
	r.click(t, "Save command")
	if len(r.fb.agent.edits) != 1 || !strings.HasSuffix(r.fb.agent.edits[0], ": echo edited-ok") {
		t.Fatalf("edits %v", r.fb.agent.edits)
	}
	if !r.tt.HasText("echo edited-ok") || r.tt.HasText("keel-agent-ok") || !r.tt.HasText("1 line · 16 bytes") {
		t.Errorf("the card does not show the server's version: %v", r.tt.Texts())
	}
	r.click(t, "Type into pane…")
	if !r.tt.HasText("echo edited-ok") {
		t.Error("the dialog does not show the edited bytes")
	}
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15echo edited-ok\n"}) {
		t.Fatalf("typed %q", got)
	}
}

func TestAnEditThatKeelServeRefusesLeavesTheCardAndTheBytesAlone(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.fb.agent.editReply = func(string) (keelclient.Proposal, error) {
		return keelclient.Proposal{}, &keelclient.StatusError{Code: 400, Msg: "a command is 1 to 4096 bytes"}
	}
	r.ask(t, "q")
	r.click(t, "Edit command")
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("x")
	r.click(t, "Save command")
	if !r.tt.HasText("a command is 1 to 4096 bytes") || !r.w.agent.props["p1"].editing || r.w.agent.props["p1"].p.Bytes != "\x15echo keel-agent-ok\n" {
		t.Errorf("%v", r.tt.Texts())
	}
	r.click(t, "Cancel")
	r.click(t, "Type into pane…")
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15echo keel-agent-ok\n"}) {
		t.Fatalf("typed %q: not the bytes keel serve holds", got)
	}
}

func destructive() keelclient.Proposal {
	p := okProposal()
	p.Command, p.Bytes, p.Risk, p.NeedsShift, p.Why = "rm -rf /tmp/keel-agent-e2e", "\x15rm -rf /tmp/keel-agent-e2e\n", "destructive", true, "Deletes the scratch folder."
	return p
}

func TestPlainTypeIsRefusedInTheCardForADestructiveProposalAndShiftTypeOpensTheDialog(t *testing.T) {
	r := agentRig(t)
	r.answers(destructive())
	r.ask(t, "delete")
	if !r.tt.HasText("Destructive") || !r.tt.HasText("Destructive. Hold "+shiftWord()+" when you press Type.") {
		t.Errorf("%v", r.tt.Texts())
	}
	r.click(t, "Type into pane…")
	if r.w.approve != nil || len(r.typed()) != 0 {
		t.Fatal("a plain Type on a destructive proposal opened the dialog")
	}
	if !r.tt.HasText("Not typed. Destructive: hold " + shiftWord() + " and press Type.") {
		t.Errorf("the card does not refuse: %v", r.tt.Texts())
	}
	if err := r.tt.ClickWith(ui.Shift, "Type into pane…"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.approve == nil || len(r.typed()) != 0 {
		t.Fatal("⇧ + Type did not open the dialog, or typed")
	}
	r.click(t, "Type")
	ref := r.fb.conns[0].agentRefs[0]
	if len(r.typed()) != 1 || ref == nil || !ref.Shift {
		t.Fatalf("typed %q ref %+v", r.typed(), ref)
	}
}

func TestAKeyboardPressOfTypeNeverCarriesTheShift(t *testing.T) {
	r := agentRig(t)
	r.answers(destructive())
	r.ask(t, "delete")
	r.click(t, "Type into pane…") // refused; the button has the keyboard now
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.approve != nil {
		t.Fatal("a plain ↵ opened the dialog for a destructive proposal")
	}
	r.tt.Key(ui.Shift, ui.KeyEnter)
	r.tt.Frame()
	if r.w.approve != nil || len(r.typed()) != 0 {
		t.Fatal("a keyboard press opened the dialog or typed: only ⇧ held on a click may")
	}
	if !r.has("Type into pane…") || !r.tt.HasText(shiftWord()) {
		t.Error("the button does not say it wants ⇧")
	}
}

func TestAProductionHostMakesAStateChangeNeedShift(t *testing.T) {
	r := agentRig(t)
	p := okProposal()
	p.Command, p.Bytes, p.Risk, p.NeedsShift, p.Production = "touch /tmp/keel-agent-e2e-touched", "\x15touch /tmp/keel-agent-e2e-touched\n", "changes", true, true
	r.answers(p)
	r.ask(t, "touch")
	if !r.tt.HasText("Production") || !r.tt.HasText("Changes state on a production host. Hold "+shiftWord()+" when you press Type.") {
		t.Errorf("%v", r.tt.Texts())
	}
	r.click(t, "Type into pane…")
	if r.w.approve != nil || !r.tt.HasText("Not typed. Changes state on a production host: hold "+shiftWord()+" and press Type.") {
		t.Errorf("%v", r.tt.Texts())
	}
}

func TestNoTypeButtonForAFollowedPaneAClosedPaneOrAProposalWithNoPane(t *testing.T) {
	r := agentRig(t)
	noPane := okProposal()
	noPane.ID, noPane.SessionID = "p2", ""
	gone := okProposal()
	gone.ID, gone.SessionID = "p3", "s-gone"
	secret := okProposal()
	secret.ID, secret.Placeholders = "p4", true
	r.answers(okProposal(), noPane, gone, secret)
	r.ask(t, "q")
	for _, s := range []string{"No pane named for this command. Copy it instead.", "That pane is not open in this window. Copy the command instead.",
		"It holds a redacted placeholder such as [secret:1], which a shell would read as a pattern. Keel will not type it: edit it to the real value or an environment variable first."} {
		if !r.tt.HasText(s) {
			t.Errorf("no note %q", s)
		}
	}
	// Only the first card, whose pane is open and driven, has the button.
	n := 0
	for _, s := range r.tt.Texts() {
		if s == "Type into pane…" {
			n++
		}
	}
	if n == 0 || n > 2 { // the row and its text are two labels of one button
		t.Errorf("%d labels of Type into pane…", n)
	}
	// A pane you follow.
	st := keelclient.State{Status: "ready", Role: "follower", Subtitle: "deploy@10.0.0.1", YouID: 2, DriverID: 1}
	r.fb.conns[0].st = &st
	r.tt.Frame()
	if r.has("Type into pane…") || !r.tt.HasText("You follow this pane. The agent cannot type into it.") {
		t.Errorf("a followed pane offers Type: %v", r.tt.Texts())
	}
	if r.w.approve != nil || len(r.typed()) != 0 {
		t.Error("something opened or typed")
	}
}

func TestNoKeyShowsTheConnectCardAndTheKeyIsSentOnceAndTheFieldCleared(t *testing.T) {
	r := approveRig(t)
	r.w.useDataDir(t, t.TempDir())
	r.tt.Key(agentMods(), ui.KeyI)
	r.tt.Frame()
	for _, s := range []string{"Connect Gemini", "Gemini API key", "Connect", "Get a key", "On the free tier of the Gemini API, Google may use the content you submit."} {
		if !r.tt.HasText(s) {
			t.Errorf("the connect card lacks %q in %v", s, r.tt.Texts())
		}
	}
	// An ask with no key does not call keel serve: the card is what answers.
	r.ask(t, "hello")
	if len(r.fb.agent.asks) != 0 {
		t.Fatal("an ask went out with no key")
	}
	const fake = "fake-key-for-tests-only-0000"
	if err := r.tt.Click("Gemini API key input"); err != nil {
		t.Fatal(err)
	}
	r.tt.Type(fake)
	r.tt.Frame()
	if r.tt.HasText(fake) {
		t.Fatal("the key is shown in the clear")
	}
	r.click(t, "Connect")
	if len(r.fb.agent.keys) != 1 || r.fb.agent.keys[0].Key != fake || r.fb.agent.keys[0].Provider != "gemini" {
		t.Fatalf("keys %v", r.fb.agent.keys)
	}
	if r.w.agent.keyField != "" || r.tt.HasText("Connect Gemini") || !r.tt.HasText("Ask about your sessions") {
		t.Errorf("the field was not cleared or the card stays: %q %v", r.w.agent.keyField, r.tt.Texts())
	}
	if !r.tt.HasText("Gemini connected. The key is in the vault.") {
		t.Error("no confirmation")
	}
	for _, shown := range []string{fmt.Sprintf("%v", r.fb.agent.keys[0]), fmt.Sprintf("%+v", r.fb.agent.keys), fmt.Sprintf("%#v", r.fb.agent.keys[0])} {
		if strings.Contains(shown, fake) {
			t.Fatalf("a formatted key shows it: %q", shown)
		}
	}
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, fake) {
			t.Fatalf("the key is on screen: %q", s)
		}
	}
	b, _ := os.ReadFile(filepath.Join(r.w.dataDir, "keel-native.json"))
	if strings.Contains(string(b), fake) {
		t.Fatal("the key is in the prefs file")
	}
	r.ask(t, "hello")
	if len(r.fb.agent.asks) != 1 {
		t.Error("an ask does not work after connecting")
	}
	// An empty field says so.
	r2 := approveRig(t)
	r2.tt.Key(agentMods(), ui.KeyI)
	r2.tt.Frame()
	r2.click(t, "Connect")
	if !r2.tt.HasText("Paste a key first.") || len(r2.fb.agent.keys) != 0 {
		t.Errorf("%v", r2.tt.Texts())
	}
}

func TestAKeyKeelServeRefusesIsReportedAndTheFieldIsAlreadyEmpty(t *testing.T) {
	r := approveRig(t)
	r.fb.agent.keyErr = &keelclient.StatusError{Code: 400, Msg: "the key is empty, too long, or has spaces"}
	r.tt.Key(agentMods(), ui.KeyI)
	r.tt.Frame()
	r.tt.Click("Gemini API key input")
	r.tt.Type("fake-key-for-tests-only-0000")
	r.tt.Frame()
	r.click(t, "Connect")
	if !r.tt.HasText("the key is empty, too long, or has spaces") || r.w.agent.keyField != "" || !r.tt.HasText("Connect Gemini") {
		t.Errorf("%q %v", r.w.agent.keyField, r.tt.Texts())
	}
}

func TestTheComposerSendsOnEnterAndBreaksTheLineOnShiftEnter(t *testing.T) {
	r := agentRig(t)
	r.answers()
	r.tt.Click("Message the agent")
	r.tt.Type("line one")
	r.tt.Key(ui.Shift, ui.KeyEnter)
	r.tt.Type("line two")
	r.tt.Frame()
	if len(r.fb.agent.asks) != 0 || !strings.Contains(r.w.agent.input, "\n") {
		t.Fatalf("⇧↵ sent or did not break the line: %q", r.w.agent.input)
	}
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if len(r.fb.agent.asks) != 1 || r.fb.agent.asks[0].Question != "line one\nline two" || r.w.agent.input != "" {
		t.Fatalf("asks %+v input %q", r.fb.agent.asks, r.w.agent.input)
	}
}

func TestAtOpensThePickerAndIsNotTypedAndAChipRemovesAPane(t *testing.T) {
	r := twoSessionRig(t)
	r.click(t, "Choose what the agent can read")
	r.click(t, "Every pane in tab")
	r.click(t, "Choose what the agent can read")
	r.click(t, "pick api-1 · api-1")
	r.click(t, "Choose what the agent can read")
	if len(r.w.agent.scope) == 0 {
		t.Fatal("setup: no scope")
	}
	r.tt.Click("Message the agent")
	r.tt.Type("look at @")
	r.tt.Frame()
	r.tt.Frame()
	if !r.w.agent.picker || r.w.agent.input != "look at " {
		t.Fatalf("picker %v input %q", r.w.agent.picker, r.w.agent.input)
	}
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	r.w.agent.picker = false
	r.tt.Frame()
	before := len(r.w.agent.scope)
	r.click(t, "Stop reading api-1")
	if len(r.w.agent.scope) != before-1 {
		t.Errorf("a chip's × did not remove the pane: %v", r.w.agent.scope)
	}
	// user@host is not a trigger.
	r.tt.Click("Message the agent")
	r.tt.Type("deploy@")
	r.tt.Frame()
	if r.w.agent.picker {
		t.Error("an @ inside a word opened the picker")
	}
}
