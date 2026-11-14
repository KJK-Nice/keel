//go:build nativee2e

package native

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Notes for the agent and runbooks against a real keel serve that answers from its in-process fake (KEEL_AGENT_PROVIDER=fake,
// set for every daemon by startServeAt): nothing here reaches a model provider or calls keel mcp's routes. "remember" in a
// question makes the fake propose a note, "save this as a runbook" a runbook, and a runbook in use makes it say which step is
// current while keel serve itself proposes the step (internal/agent/provider/providertest/scripted.go). Every command is a
// harmless echo and the only secret is made up.

const e2eNoteSecret = "native-e2e-fake-note-secret-7391" // a made-up password; no test prints it

func (e *win) serverNotes() []keelclient.Note {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	l, err := e.cl.Notes(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return l.Notes
}

func (e *win) serverRunbooks() []keelclient.Runbook {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	l, err := e.cl.Runbooks(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return l.Runbooks
}

// setLocalAgent sets the Local Shell's Agent can read and Let the agent propose notes, through the API.
func (e *win) setLocalAgent(readable bool) {
	e.t.Helper()
	code, body := e.raw("GET", "/api/hosts", nil)
	if code != 200 {
		e.t.Fatalf("GET /api/hosts: %d", code)
	}
	var hosts []map[string]any
	if err := json.Unmarshal([]byte(body), &hosts); err != nil {
		e.t.Fatal(err)
	}
	for _, h := range hosts {
		if h["authType"] == "local" {
			h["agentReadable"], h["agentNotes"] = readable, true
			if code, _ := e.raw("PUT", "/api/hosts/"+h["id"].(string), h); code != 200 {
				e.t.Fatalf("PUT the Local Shell: %d", code)
			}
			return
		}
	}
	e.t.Fatal("no Local Shell")
}

// editLocalShell opens Edit host for the Local Shell from its menu.
func (e *win) editLocalShell() {
	e.t.Helper()
	if err := e.tt.RightClick("host Local Shell"); err != nil {
		e.t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Edit"); err != nil {
		e.t.Fatal(err)
	}
	e.pump(func() bool { return e.hasLabel("dialog Edit host") && e.tt.HasText("Notes for the agent") }, "the Edit host dialog")
}

// addNote types a note into the open "Add a note" field and presses ↵.
func (e *win) addNote(text string) {
	e.t.Helper()
	e.click("Add a note")
	e.tt.Type(text)
	e.step()
	e.key(0, ui.KeyEnter)
}

func (e *win) logMessages(kind string) (out []string) {
	for _, l := range e.logEntries() {
		if l.Kind == kind {
			out = append(out, l.Message)
		}
	}
	return out
}

func countText(e *win, s string) (n int) {
	for _, x := range e.tt.Texts() {
		if x == s {
			n++
		}
	}
	return n
}

func TestE2EHostNotes(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	e.setLocalAgent(true)
	e.openLocal()
	h, _ := e.hostNamed("Local Shell")

	// Edit host: add a note; it is on the server, saved at once, and the dialog stayed open.
	e.editLocalShell()
	e.addNote("keel-note-e2e: deploys run from /srv/app")
	e.pump(func() bool { return len(e.serverNotes()) == 1 }, "the note on the server")
	if n := e.serverNotes()[0]; n.HostID != h.ID || n.Source != "user" || n.Text != "keel-note-e2e: deploys run from /srv/app" {
		t.Fatalf("the note on the server: %+v", n)
	}
	e.pump(func() bool { return e.tt.HasText("1 note · 40 chars") }, "the count")
	if e.w.hostDlg == nil {
		t.Fatal("↵ in the notes field closed or submitted the dialog")
	}
	e.shot("TestE2EHostNotes-dialog")
	e.click("Cancel")
	e.pump(func() bool { return e.w.hostDlg == nil }, "the dialog to close")

	// The next question that reads the pane sends the note: the trace says so and so does the log, without the text.
	e.openAgent()
	e.settledAsk("what now?")
	e.pump(func() bool { return e.tt.HasText("Used host notes · Local Shell (1)") }, "the trace's notes row")
	if !e.tt.HasText("I was also given 1 host note and 0 of your notes.") {
		t.Errorf("the answer does not say the note was given: %v", e.tt.Texts())
	}
	sent := func() (n int) {
		for _, m := range e.logMessages("agent.note") {
			if strings.Contains(m, "agent sent 1 host note for Local Shell") {
				n++
			}
		}
		return n
	}
	if sent() != 1 {
		t.Fatalf("agent.note rows: %v", e.logMessages("agent.note"))
	}
	if strings.Contains(strings.Join(e.logMessages("agent.note"), "\n"), "srv/app") {
		t.Fatal("the log holds the note's text")
	}

	// Agent can read off: the pane leaves the scope and the note is not sent.
	e.editLocalShell()
	// The row's text and its switch share the label, and Find gives the text: press the switch at the right end of the row.
	row, _ := e.tt.Find("Agent can read this host's panes")
	dlg, _ := e.tt.Find("dialog Edit host")
	e.tt.ClickAt(dlg.X+dlg.W-space4-18, row.Y+row.H+space2)
	e.step()
	e.pump(func() bool {
		return e.tt.HasText("Agent can read is off, so these notes are not sent. They stay in the vault.")
	}, "the notes-not-sent notice")
	e.shot("TestE2EHostNotes-off")
	e.click("Save host")
	e.pump(func() bool { return e.w.hostDlg == nil }, "the dialog to close")
	e.pump(func() bool { h, _ := e.hostNamed("Local Shell"); return h.AgentReadable != nil && !*h.AgentReadable }, "reading off on the server")
	e.settledAsk("anything to know?")
	e.pump(func() bool { return e.tt.HasText("Read no pane") }, "no pane read")
	if countText(e, "Used host notes · Local Shell (1)") != 1 || sent() != 1 {
		t.Fatalf("a note was sent with reading off: %d rows, %d log rows", countText(e, "Used host notes · Local Shell (1)"), sent())
	}

	// On again: edit, then delete, from the dialog.
	e.setLocalAgent(true)
	e.editLocalShell()
	e.click("Edit note")
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type("keel-note-e2e: deploys run from /srv/app with make deploy")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool {
		n := e.serverNotes()
		return len(n) == 1 && n[0].Text == "keel-note-e2e: deploys run from /srv/app with make deploy"
	}, "the edited note on the server")
	e.pump(func() bool {
		return e.hasLabel("Delete note") && e.tt.HasText("keel-note-e2e: deploys run from /srv/app with make deploy")
	}, "the edit in the list")
	e.click("Delete note")
	e.pump(func() bool { return len(e.serverNotes()) == 0 }, "the note gone from the server")
	if e.w.hostDlg == nil {
		t.Fatal("the dialog closed")
	}
}

func TestE2ENoteWithSecretIsRedactedOnSave(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	c, cancel := ctx()
	if _, err := e.cl.SaveIdentity(c, keelclient.IdentityInput{Name: "e2e made-up password", Username: "nobody", Kind: "password", Password: e2eNoteSecret}); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	e.openLocal()
	e.editLocalShell()
	e.addNote("the password is " + e2eNoteSecret)
	e.pump(func() bool { return len(e.serverNotes()) == 1 }, "the note on the server")
	n := e.serverNotes()[0]
	if strings.Contains(n.Text, e2eNoteSecret) || !strings.Contains(n.Text, "[secret:") {
		t.Fatal("the note on the server is not the redacted form")
	}
	e.pump(func() bool { return e.tt.HasText("Note added. 1 secret-looking value was replaced before saving.") }, "the count")
	if !e.onWindow("the password is [secret:") {
		t.Error("the list does not show the redacted form")
	}
	e.shot("TestE2ENoteWithSecretIsRedactedOnSave")
	e.noSecretDrawn(e2eNoteSecret)
	for _, p := range []string{"/api/agent/notes", "/api/logs"} {
		if _, body := e.raw("GET", p, nil); strings.Contains(body, e2eNoteSecret) {
			t.Fatalf("the value is in GET %s", p)
		}
	}
	if len(e.logMessages("agent.note")) == 0 {
		t.Error("the save is not in the log")
	}
}

// onWindow says whether a text on screen holds s.
func (e *win) onWindow(s string) bool {
	for _, x := range e.tt.Texts() {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

func TestE2ENoteProposal(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	e.setLocalAgent(true)
	e.openLocal()
	e.openAgent()
	e.settledAsk("please remember this")
	e.pump(func() bool { return e.hasLabel("Proposed note") }, "the note card")
	for _, s := range []string{"Remember this?", "keel-note-e2e: backups run at 02:00 from pgbackrest", "Host notes · Local Shell", "Sent when Local Shell is in scope", "After reading panes", "Save anyway", "Dismiss"} {
		if !e.tt.HasText(s) && !e.hasLabel(s) {
			t.Errorf("the card lacks %q", s)
		}
	}
	e.shot("TestE2ENoteProposal-card")
	e.settle(time.Second)
	if len(e.serverNotes()) != 0 {
		t.Fatal("a proposal was kept without a Save")
	}
	// Dismiss keeps nothing.
	e.click("Dismiss")
	e.pump(func() bool { return e.tt.HasText("Dismissed. Nothing was saved.") }, "Dismissed")
	if len(e.serverNotes()) != 0 {
		t.Fatal("Dismiss kept the note")
	}
	// Ask again in a new conversation (a closed card has an Undo of its own); this time Save.
	e.click("New conversation")
	e.settledAsk("please remember this")
	e.pump(func() bool { return e.hasLabel("Save anyway") }, "the second card")
	e.click("Save anyway")
	e.pump(func() bool { return len(e.serverNotes()) == 1 }, "the saved note on the server")
	if n := e.serverNotes()[0]; n.Source != "agent" || n.Edited || n.Text != "keel-note-e2e: backups run at 02:00 from pgbackrest" {
		t.Fatalf("the saved note: %+v", n)
	}
	e.pump(func() bool { return e.tt.HasText("Saved to host notes · Local Shell") }, "the card to say it saved")
	// Undo takes it away again.
	e.click("Undo")
	e.pump(func() bool { return len(e.serverNotes()) == 0 }, "Undo to delete the note")
	e.pump(func() bool { return e.hasLabel("Save anyway") }, "the card to reopen")
	if k := e.logMessages("agent.note"); !strings.Contains(strings.Join(k, "\n"), "note proposed by the agent saved by you") {
		t.Errorf("agent.note rows: %v", k)
	}
}

func TestE2EYourNotesAndDeleteAll(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWinH(t, addr, dir, nil, 2600) // Settings · Agent is long: its notes are on screen
	e.putAgentKey()
	h, _ := e.hostNamed("Local Shell")
	c, cancel := ctx()
	if _, err := e.cl.SaveNote(c, keelclient.NoteInput{HostID: h.ID, Text: "keel-note-e2e: a host note"}); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	e.openLocal()
	e.w.openPage("settings")
	e.pump(func() bool { return e.hasLabel("settings nav Agent") }, "Settings")
	e.click("settings nav Agent")
	e.pump(func() bool { return e.tt.HasText("Notes for the agent") }, "Settings · Agent")
	e.addNote("keel-note-e2e: prefer journalctl")
	e.pump(func() bool { return len(e.serverNotes()) == 2 }, "the first note")
	e.addNote("keel-note-e2e: short answers")
	e.pump(func() bool { return len(e.serverNotes()) == 3 }, "the second note")
	e.pump(func() bool { return e.tt.HasText("2 notes · 60 chars") }, "the count of your notes")
	e.shot("TestE2EYourNotesAndDeleteAll")

	e.click("Delete all notes…")
	e.pump(func() bool { return e.w.confirm != nil }, "the confirm")
	if !e.tt.HasText("Delete all notes?") || !e.tt.HasText("Every host note and every note about you is deleted from the vault. Conversations and the log stay.") {
		t.Error("the confirm is not the app's")
	}
	e.click("Cancel")
	e.settle(300 * time.Millisecond)
	if len(e.serverNotes()) != 3 {
		t.Fatal("Cancel deleted notes")
	}
	e.click("Delete all notes…")
	e.pump(func() bool { return e.w.confirm != nil }, "the confirm again")
	// The row's title and the dialog's action share their words, and Find gives the title under the scrim: press the action at
	// the dialog's bottom right, where its danger button is.
	dlg, ok := e.tt.Find("alertdialog Delete all notes?")
	if !ok {
		t.Fatal("no confirm dialog")
	}
	e.tt.ClickAt(dlg.X+dlg.W-space4-40, dlg.Y+dlg.H-space4-sizeControlSm/2)
	e.step()
	e.pump(func() bool { return len(e.serverNotes()) == 0 }, "no note on the server, host notes included")
	e.pump(func() bool {
		return e.tt.HasText("Notes deleted") && e.tt.HasText("No notes. Add how you like to work, e.g. Prefer journalctl over tail -f.")
	}, "the empty list")
}

// typeStepCard presses Type on the card of the step in the thread (a card made by keel serve for the current step) and
// answers whatever follows: the dialog's Type, or nothing when keel serve let it be typed without one.
func (e *win) typeStepCard(sid, want string) {
	e.t.Helper()
	e.click("Type into pane…")
	e.pump(func() bool { return e.w.approve != nil || strings.Contains(e.scrollback(sid), want) }, "the dialog, or the step typed")
	if e.w.approve != nil {
		e.click("Type")
		e.pump(func() bool { return e.w.approve == nil }, "the dialog to close")
	}
	e.waitScrollback(sid, want)
}

func TestE2ERunbookPageAndWalk(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWinH(t, addr, dir, nil, 2600) // the editor and the thread are long: every control is on screen
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()

	// The page: three steps.
	e.key(ui.Cmd, ui.KeyK)
	e.tt.Type("Runbooks")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.w.pageTab("runbooks") != nil && e.tt.HasText("No runbooks yet") }, "the Runbooks page")
	e.key(ui.Cmd, ui.KeyN)
	e.pump(func() bool { return e.hasLabel("Runbook name") }, "the editor")
	e.fillField("Runbook name", "keel-rb-e2e marker check")
	e.fillField("Use when", "The e2e marker needs checking")
	for i, cmd := range []string{"echo rb-1", "echo rb-2", "echo rb-3"} {
		n := string(rune('1' + i))
		if i > 0 {
			e.click("Add step")
			e.pump(func() bool { return e.hasLabel("Step " + n + " title") }, "a new step")
		}
		e.fillField("Step "+n+" title", "Print "+n)
		e.fillField("Step "+n+" command", cmd)
	}
	e.click("Save")
	e.pump(func() bool { return len(e.serverRunbooks()) == 1 }, "the runbook on the server")
	rb := e.serverRunbooks()[0]
	if rb.Name != "keel-rb-e2e marker check" || rb.Source != "user" || !rb.AgentMayPick || len(rb.Steps) != 3 || rb.Steps[2].Command != "echo rb-3" {
		t.Fatalf("the runbook on the server: %+v", rb)
	}
	e.pump(func() bool { return e.tt.HasText("Runbook saved") && e.hasLabel("Open keel-rb-e2e marker check") }, "the saved runbook in the list")
	e.shot("TestE2ERunbookPageAndWalk-page")

	// Use in agent: an ask with the runbook; keel serve proposes step 1 and nothing is typed.
	e.click("Use in agent")
	e.pump(func() bool { return e.hasLabel("Runbook keel-rb-e2e marker check") && e.tt.HasText("Step 1 · Run on") }, "the run card and step 1's card")
	for _, s := range []string{"You picked this runbook.", "Used runbook · keel-rb-e2e marker check", "Step 1 is proposed below. Nothing is typed until you press Type."} {
		if !e.tt.HasText(s) {
			t.Errorf("lacks %q", s)
		}
	}
	e.settle(time.Second)
	if strings.Contains(e.scrollback(sid), "rb-") {
		t.Fatal("a step reached the shell before any Type")
	}
	e.typeStepCard(sid, "echo rb-1")
	if s := e.scrollback(sid); strings.Contains(s, "rb-2") || strings.Contains(s, "rb-3") {
		t.Fatal("more than the current step was typed")
	}
	// The shell has no integration marks: the user marks the step.
	e.pump(func() bool { return e.tt.HasText("Mark done") }, "Mark done")
	e.click("Mark done")
	e.pump(func() bool { return e.tt.HasText("Propose step 2") }, "the card to offer step 2")
	if e.tt.HasText("Propose step 3") {
		t.Fatal("the card offers a later step")
	}
	e.click("Propose step 2")
	e.pump(func() bool { return e.tt.HasText("Step 2 · Run on") }, "step 2's card")
	e.click("Skip")
	e.pump(func() bool { return e.tt.HasText("Propose step 3") }, "the card to offer step 3")
	e.click("Propose step 3")
	e.pump(func() bool { return e.tt.HasText("Step 3 · Run on") }, "step 3's card")
	e.shot("TestE2ERunbookPageAndWalk-walk")
	// Step 2 has two cards' worth of history in the thread but only 1 and 3 can be typed: the card of step 2 is old now.
	e.typeStepCardLast(sid, "echo rb-3")
	if strings.Contains(e.scrollback(sid), "rb-2") {
		t.Fatal("the skipped step was typed")
	}
	e.pump(func() bool { return e.tt.HasText("Mark done") }, "Mark done for step 3")
	e.click("Mark done")
	e.pump(func() bool { return e.tt.HasText("All steps are done or skipped.") }, "the walk to end")

	rows := e.logMessages("agent.runbook")
	joined := strings.Join(rows, "\n")
	if len(rows) == 0 || strings.Contains(joined, "marker check") || strings.Contains(joined, "echo rb-") {
		t.Fatalf("agent.runbook rows hold names or commands: %v", rows)
	}
	for _, want := range []string{"runbook step 1 of 3 marked done by you", "runbook step 2 of 3 skipped by you", "runbook step 3 of 3 marked done by you"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no log row %q in %v", want, rows)
		}
	}
}

// typeStepCardLast is typeStepCard for the newest card: the thread keeps the cards of the steps before it.
func (e *win) typeStepCardLast(sid, want string) {
	e.t.Helper()
	e.w.typeAgentProposal(e.lastProposalID(), false) // the Type button of that card: the same function a press calls
	e.pump(func() bool { return e.w.approve != nil || strings.Contains(e.scrollback(sid), want) }, "the dialog, or the step typed")
	if e.w.approve != nil {
		e.click("Type")
		e.pump(func() bool { return e.w.approve == nil }, "the dialog to close")
	}
	e.waitScrollback(sid, want)
}

// lastProposalID is the newest proposal of the conversation.
func (e *win) lastProposalID() string {
	e.t.Helper()
	var id string
	for _, t := range e.w.agent.turns {
		for _, p := range t.props {
			id = p
		}
	}
	if id == "" {
		e.t.Fatal("no proposal")
	}
	return id
}

func TestE2ERunbookProposalAndPlaceholder(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()

	// The fake proposes a runbook: nothing is kept before Save, and a saved one starts with Agent may pick off.
	e.settledAsk("save this as a runbook")
	e.pump(func() bool { return e.hasLabel("Proposed runbook") }, "the runbook card")
	if !e.tt.HasText("keel-runbook-e2e proposed") || !e.tt.HasText("After reading panes") {
		t.Errorf("the card: %v", e.tt.Texts())
	}
	e.shot("TestE2ERunbookProposalAndPlaceholder-card")
	e.settle(time.Second)
	if len(e.serverRunbooks()) != 0 {
		t.Fatal("a proposal was kept without a Save")
	}
	e.click("Save anyway")
	e.pump(func() bool { return len(e.serverRunbooks()) == 1 }, "the saved runbook")
	if rb := e.serverRunbooks()[0]; rb.Source != "agent" || rb.AgentMayPick || rb.Name != "keel-runbook-e2e proposed" {
		t.Fatalf("the saved runbook: %+v", rb)
	}
	e.pump(func() bool {
		return e.tt.HasText("Saved to Runbooks. The agent may not pick it until you review it and turn it on.")
	}, "the saved card")
	e.click("Undo")
	e.pump(func() bool { return len(e.serverRunbooks()) == 0 }, "Undo to delete it")

	// A step whose command holds [secret:1] cannot exist in keel serve: it refuses to keep one, from the API and from the editor,
	// so a step proposal never carries a placeholder from a saved runbook. The card for one that does (an ordinary proposal
	// that holds a placeholder) is runbooks_test.go's: no Type button, the explanatory sentence, nothing typed.
	if code, body := e.raw("POST", "/api/agent/runbooks", map[string]any{"name": "keel-rb-e2e placeholder", "agentMayPick": true,
		"steps": []map[string]string{{"title": "Uses a placeholder", "command": "echo [secret:1]"}}}); code != 400 || !strings.Contains(body, "a step command holds a secret") {
		t.Fatalf("keel serve kept, or did not explain refusing, a step with a placeholder: %d", code)
	}
	e.w.openPage("runbooks")
	e.pump(func() bool { return e.hasLabel("Runbook name") == false && e.tt.HasText("No runbooks yet") }, "the Runbooks page")
	e.key(ui.Cmd, ui.KeyN)
	e.pump(func() bool { return e.hasLabel("Runbook name") }, "the editor")
	e.fillField("Runbook name", "keel-rb-e2e placeholder")
	e.fillField("Step 1 title", "Uses a placeholder")
	e.fillField("Step 1 command", "echo [secret:1]")
	e.click("Save")
	e.pump(func() bool { return e.onWindow("a step command holds a secret") }, "the refusal in the editor")
	e.shot("TestE2ERunbookProposalAndPlaceholder-refused")
	if len(e.serverRunbooks()) != 0 {
		t.Fatal("a runbook with a placeholder was kept")
	}
	if strings.Contains(e.scrollback(sid), "secret:1") || e.logKinds()["agent.type"] != 0 {
		t.Fatal("something was typed")
	}
}

func TestE2ERunbooksPageComesBack(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	if code, body := e.raw("POST", "/api/agent/runbooks", map[string]any{"name": "keel-rb-e2e kept", "agentMayPick": true, "steps": []map[string]string{{"title": "One", "command": "echo one"}}}); code != 200 {
		t.Fatalf("POST /api/agent/runbooks: %d %s", code, body)
	}
	e.key(ui.Cmd, ui.KeyK)
	e.tt.Type("Runbooks")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.w.pageTab("runbooks") != nil && e.hasLabel("Open keel-rb-e2e kept") }, "the Runbooks page")
	e.shot("TestE2ERunbooksPageComesBack")
	e.settle(600 * time.Millisecond)
	e.w.closeWindow()

	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return e2.w.pageTab("runbooks") != nil && e2.hasLabel("Open keel-rb-e2e kept") }, "the Runbooks tab to come back")
	if e2.w.tab().name() != "Runbooks" {
		t.Errorf("the page that was shown is not shown again: %s", e2.w.tab().name())
	}
}
