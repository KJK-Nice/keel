package native

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Runbooks on the fake backend: the page's list and editor and the bodies they send; the card for a runbook the agent
// proposes (kept only by Save, with Agent may pick off); and the walk, one step at a time, where each step is an ordinary
// proposal typed through Approve before type and nothing else types. Every command is a harmless echo and every secret made up.

func rbRig(t *testing.T) *rig {
	t.Helper()
	r := agentRig(t)
	r.tt.SetSize(1280, 2400) // the editor and the thread are long: every control is on screen
	r.w.openPage("runbooks")
	r.tt.Frame()
	return r
}

func markerBook() keelclient.Runbook {
	return keelclient.Runbook{ID: "rb1", Name: "Marker check", When: "The marker needs checking", Hosts: []string{"production"}, AgentMayPick: true, Source: "user",
		Steps: []keelclient.RunbookStep{
			{ID: "st1", Title: "Print one", Command: "echo rb-1", Risk: "readonly"},
			{ID: "st2", Title: "Print two", Command: "echo rb-2", Risk: "readonly"},
			{ID: "st3", Title: "Print three", Command: "echo rb-3", Risk: "readonly"},
		}}
}

func TestTheRunbooksPageIsOpenedByThePaletteAndItsNewByCmdN(t *testing.T) {
	r := agentRig(t)
	r.tt.Key(ui.Cmd, ui.KeyK)
	r.tt.Frame()
	r.tt.Type("Runbooks")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if t2 := r.w.pageTab("runbooks"); t2 == nil || r.w.tab() != t2 {
		t.Fatal("the palette's Runbooks does not open the page")
	}
	if miss := r.lacks("Runbooks", "No runbooks yet", "Select a runbook to edit it, or add one with New.", "0 runbooks · stored in the vault"); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	r.press(ui.Cmd, ui.KeyN)
	if r.w.runbooksPage().ed == nil || r.w.hostDlg != nil {
		t.Fatal("⌘N on the page is not its New")
	}
	if miss := r.lacks("Use when", "Steps", "Add step", "Agent may pick this runbook", "Off: only you can start it.", "Hosts", "New, not saved", "Never used"); len(miss) > 0 {
		t.Errorf("the editor lacks %q", miss)
	}
	if !r.tt.Focused("Runbook name") {
		t.Error("the name does not have the keyboard")
	}
}

func TestCreatingEditingAndDeletingARunbookSendsTheEditorsStateAndDeleteAsksFirst(t *testing.T) {
	r := rbRig(t)
	r.w.newRunbook()
	r.tt.Frame()
	r.fill(t, "Runbook name", "  Disk\nfull ")
	r.fill(t, "Use when", "runner above 85%")
	r.fill(t, "Step 1 title", "See docker")
	r.fill(t, "Step 1 command", "docker system df")
	r.click(t, "Add step")
	r.fill(t, "Step 2 title", "Look at the output")
	r.click(t, "Folder or tag")
	r.tt.Type("ci")
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	r.click(t, "Save")
	if len(r.fb.mem.rbSaved) != 1 {
		t.Fatalf("%d saves", len(r.fb.mem.rbSaved))
	}
	got, _ := json.Marshal(r.fb.mem.rbSaved[0])
	want := `{"name":"Disk full","when":"runner above 85%","hosts":["ci"],"agentMayPick":true,"steps":[{"id":"","title":"See docker","description":"","command":"docker system df","expect":"","role":"","oneHost":false,"captures":[]},{"id":"","title":"Look at the output","description":"","command":"","expect":"","role":"","oneHost":false,"captures":[]}],"draftId":""}`
	if string(got) != want {
		t.Fatalf("sent\n%s\nwant\n%s", got, want)
	}
	if !slices.Equal(r.fb.mem.rbReqs, []string{"post Disk full"}) || !r.tt.HasText("Runbook saved") || !r.has("Open Disk full") || !r.tt.HasText("1 runbook · stored in the vault") {
		t.Fatalf("requests %v", r.fb.mem.rbReqs)
	}
	// Edit it and save with ⌘S: a PUT with the id.
	r.fill(t, "Runbook name", "Disk usage")
	r.press(ui.Cmd, ui.KeyS)
	if !slices.Equal(r.fb.mem.rbReqs, []string{"post Disk full", "put rb1 Disk usage"}) {
		t.Fatalf("requests %v", r.fb.mem.rbReqs)
	}
	// The list's switch turns Agent may pick off.
	r.click(t, "Agent may pick Disk usage switch")
	if r.fb.mem.rbReqs[len(r.fb.mem.rbReqs)-1] != "pick rb1 false" || r.fb.mem.runbooks[0].AgentMayPick {
		t.Fatalf("requests %v", r.fb.mem.rbReqs)
	}
	// Delete asks first.
	r.click(t, "Delete")
	r.settleFrames()
	cf := r.w.confirm
	if cf == nil || cf.title != "Delete runbook?" || cf.action != "Delete" || cf.message != "“Disk usage” is deleted from the vault. Conversations that used it keep their copy of its steps." {
		t.Fatalf("no confirm, or not the app's: %+v", cf)
	}
	r.click(t, "Cancel")
	r.settleFrames()
	if len(r.fb.mem.runbooks) != 1 {
		t.Fatal("Cancel deleted it")
	}
	r.click(t, "Delete")
	r.settleFrames()
	r.w.confirm.run()
	r.settleFrames()
	if len(r.fb.mem.runbooks) != 0 || !r.tt.HasText("No runbooks yet") || !r.tt.HasText("Runbook deleted") {
		t.Fatal("confirming did not delete it")
	}
}

func TestARunbookIsCheckedBeforeItIsSentAndAUnsavedEditorAsksBeforeItIsLeft(t *testing.T) {
	r := rbRig(t)
	r.w.newRunbook()
	r.tt.Frame()
	r.click(t, "Save")
	if len(r.fb.mem.rbSaved) != 0 || !r.tt.HasText("Name the runbook.") {
		t.Fatalf("a nameless runbook was sent: %d", len(r.fb.mem.rbSaved))
	}
	r.fill(t, "Runbook name", "x")
	r.click(t, "Save")
	if len(r.fb.mem.rbSaved) != 0 || !r.tt.HasText("Add at least one step.") {
		t.Fatal("a runbook with no step was sent")
	}
	r.fill(t, "Step 1 command", "echo fake")
	r.click(t, "Save")
	if len(r.fb.mem.rbSaved) != 0 || !r.tt.HasText("Step 1 needs a title.") {
		t.Fatal("a step with no title was sent")
	}
	// What was typed is not dropped without asking.
	r.fb.mem.runbooks = []keelclient.Runbook{markerBook()}
	r.w.loadRunbooks()
	r.tt.Frame()
	r.click(t, "Open Marker check")
	r.settleFrames()
	cf := r.w.confirm
	if cf == nil || cf.title != "Discard changes?" || cf.message != "Your changes to this runbook are not saved." || cf.action != "Discard" {
		t.Fatalf("no confirm: %+v", cf)
	}
	r.click(t, "Cancel")
	r.settleFrames()
	if r.w.runbooksPage().ed.name != "x" {
		t.Fatal("Cancel left the editor")
	}
}

func TestARunbookWithASecretInItsCommandShowsWhatWasKeptAndTheCount(t *testing.T) {
	r := rbRig(t)
	r.w.newRunbook()
	r.tt.Frame()
	r.fill(t, "Runbook name", "Login")
	r.fill(t, "Step 1 title", "Log in")
	r.fill(t, "Step 1 command", "mysql -p"+fakeSecret)
	r.click(t, "Save")
	if !r.tt.HasText("Saved. 1 secret-looking value was replaced first.") || r.onScreen(fakeSecret) {
		t.Fatal("the count is not told, or the value shows")
	}
	if got := r.w.runbooksPage().ed.steps[0].command; got != "mysql -p[secret:1]" {
		t.Fatalf("the editor holds %q, not what keel serve kept", got)
	}
	// keel serve refuses a command the redactor would change in a saved runbook: the editor shows its reason.
	r.fb.mem.rbErr = &keelclient.StatusError{Code: 400, Status: "400 Bad Request", Msg: "a step command holds a secret or a secret-looking value"}
	r.click(t, "Save")
	if !r.tt.HasText("a step command holds a secret or a secret-looking value") {
		t.Error("the refusal is not shown")
	}
}

func TestWhatTheEditorDoesNotEditGoesBackAsItWasLoaded(t *testing.T) {
	r := rbRig(t)
	rb := markerBook()
	rb.Steps[0].Role, rb.Steps[0].OneHost, rb.Steps[0].Captures = "web", true, []keelclient.StepCapture{{Name: "tok", Pattern: "x(\\d+)"}}
	r.fb.mem.runbooks = []keelclient.Runbook{rb}
	r.w.loadRunbooks()
	r.tt.Frame()
	r.click(t, "Open Marker check")
	r.fill(t, "Step 1 title", "Print one, renamed")
	r.press(ui.Cmd, ui.KeyS)
	if len(r.fb.mem.rbSaved) != 1 {
		t.Fatal("no save")
	}
	s := r.fb.mem.rbSaved[0].Steps[0]
	if s.ID != "st1" || s.Role != "web" || !s.OneHost || len(s.Captures) != 1 || s.Captures[0].Pattern != "x(\\d+)" {
		t.Errorf("a save dropped what the group runs use: %+v", s)
	}
}

func TestARunbookTheAgentProposedWaitsForAReviewAndEnableTurnsPickOn(t *testing.T) {
	r := rbRig(t)
	rb := markerBook()
	rb.Source, rb.AgentMayPick, rb.FromPane = "agent", false, true
	rb.Steps[1].Risk = "changes"
	r.fb.mem.runbooks = []keelclient.Runbook{rb}
	r.w.loadRunbooks()
	r.tt.Frame()
	r.click(t, "Open Marker check")
	if miss := r.lacks("Not enabled yet. Review before enabling.", "Proposed", "Enable", "Proposed by the agent after reading panes"); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	if !r.onScreen("The agent proposed it after reading terminal output. The agent reads a runbook as a checklist, so read every step. Step 2 changes state.") {
		t.Error("the review words")
	}
	if len(r.fb.mem.rbReqs) != 0 {
		t.Fatal("showing it turned something on")
	}
	r.click(t, "Enable")
	if r.fb.mem.rbReqs[len(r.fb.mem.rbReqs)-1] != "pick rb1 true" || r.tt.HasText("Not enabled yet. Review before enabling.") {
		t.Fatalf("requests %v", r.fb.mem.rbReqs)
	}
}

func TestADraftFromAConversationIsNotKeptUntilSaveAndStartsWithPickOff(t *testing.T) {
	r := rbRig(t)
	r.fb.convs.list.HistoryDays = 30
	r.fb.convs.list.Sessions = []keelclient.ConversationSummary{{ID: "c-old-0001", Title: "nginx 502", UpdatedAt: time.Now(), Proposals: 2}}
	r.click(t, "From a conversation…")
	if miss := r.lacks("Make a draft from a conversation: its proposals become steps.", "nginx 502"); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	r.fb.mem.draft = keelclient.RunbookDraft{Name: "nginx 502", Steps: []keelclient.RunbookStep{{Title: "Tail the log", Command: "echo tail"}}, Source: "conversation", SourceRef: "nginx 502", DraftID: "dr_1"}
	r.click(t, "draft from nginx 502")
	if !slices.Equal(r.fb.mem.rbReqs, []string{"draft c-old-0001"}) || len(r.fb.mem.runbooks) != 0 {
		t.Fatalf("requests %v", r.fb.mem.rbReqs)
	}
	if !r.tt.HasText("A draft from the conversation “nginx 502”. Nothing is saved until you press Save, and the agent may not pick it until you turn that on.") || r.w.runbooksPage().ed.agentMayPick {
		t.Fatal("the draft is not shown as a draft with pick off")
	}
	r.press(ui.Cmd, ui.KeyS)
	if in := r.fb.mem.rbSaved[0]; in.DraftID != "dr_1" || in.AgentMayPick {
		t.Fatalf("sent %+v", in)
	}
	// Nothing to make steps from: 422, and a toast says why.
	r.fb.mem.draftErr = &keelclient.StatusError{Code: 422, Status: "422 Unprocessable Entity", Msg: "nothing to make steps from"}
	r.click(t, "From a conversation…")
	r.click(t, "draft from nginx 502")
	if !r.tt.HasText("Nothing to make steps from: no headings or shell blocks, or no proposals.") {
		t.Error("no words for an empty draft")
	}
}

// ---- the card for a runbook the agent proposes ----

func bookProposal() keelclient.RunbookProposal {
	return keelclient.RunbookProposal{ID: "bp1", Name: "keel-runbook-e2e proposed", When: "The marker needs checking", Status: "open", PanesRead: []string{"api-1"},
		Steps: []keelclient.RunbookStep{{Title: "Print the marker", Command: "echo keel-runbook-proposed"}, {Title: "Look at it"}}}
}

func (r *rig) answersBooks(bs ...keelclient.RunbookProposal) {
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{ConversationID: req.ConversationID, Answer: "ok", Runbooks: bs}, nil
	}
}

func TestARunbookProposalIsKeptOnlyBySaveWithPickOffAndDismissKeepsNothing(t *testing.T) {
	r := agentRig(t)
	r.answersBooks(bookProposal())
	r.tt.SetSize(1280, 900) // the body text is 13 pt, so the card's footer needs a taller window to be in view
	r.ask(t, "save this as a runbook")
	if miss := r.lacks("Save as a runbook?", "keel-runbook-e2e proposed", "Use when: The marker needs checking", "After reading panes", "2 steps", "echo keel-runbook-proposed",
		"This runbook was written after reading terminal output. Check every step before saving: a saved runbook can be offered to the agent, and text in a pane can be written to mislead it.",
		"Nothing is saved until you press Save. A saved runbook starts with Agent may pick off; review it on the Runbooks page."); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	r.tt.Frame()
	if !r.has("Proposed runbook") || !r.has("Save anyway") || r.has("Save") || len(r.fb.mem.propReqs) != 0 || len(r.fb.mem.runbooks) != 0 {
		t.Fatalf("buttons, or the proposal was acted on without a press: %v", r.fb.mem.propReqs)
	}
	conv := r.fb.agent.asks[0].ConversationID
	r.click(t, "Save anyway")
	if !slices.Equal(r.fb.mem.propReqs, []string{"save-book " + conv + "/bp1"}) || len(r.fb.mem.runbooks) != 1 || r.fb.mem.runbooks[0].AgentMayPick || r.fb.mem.runbooks[0].Source != "agent" {
		t.Fatalf("requests %v, runbooks %+v", r.fb.mem.propReqs, r.fb.mem.runbooks)
	}
	if miss := r.lacks("Saved to Runbooks. The agent may not pick it until you review it and turn it on.", "Open Runbooks", "Undo"); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	r.click(t, "Undo")
	if r.fb.mem.propReqs[1] != "undo-book "+conv+"/bp1" || len(r.fb.mem.runbooks) != 0 || !r.has("Save anyway") {
		t.Fatalf("requests %v", r.fb.mem.propReqs)
	}
	r.click(t, "Dismiss")
	if r.fb.mem.propReqs[2] != "dismiss-book "+conv+"/bp1" || len(r.fb.mem.runbooks) != 0 || !r.tt.HasText("Dismissed. Nothing was saved.") || r.has("Save anyway") {
		t.Fatalf("requests %v", r.fb.mem.propReqs)
	}
}

func TestOpenRunbooksOnASavedProposalOpensThePage(t *testing.T) {
	r := agentRig(t)
	p := bookProposal()
	p.PanesRead = nil
	r.answersBooks(p)
	r.ask(t, "save this as a runbook")
	r.click(t, "Save")
	r.click(t, "Open Runbooks")
	if t2 := r.w.pageTab("runbooks"); t2 == nil || r.w.tab() != t2 {
		t.Fatal("Open Runbooks does not open the page")
	}
	if !r.has("Open proposed") {
		t.Error("the saved runbook is not listed")
	}
}

// ---- the walk ----

func stepOf(id, title, cmd, state string) keelclient.RunStep {
	return keelclient.RunStep{ID: id, Title: title, Command: cmd, State: state}
}

func newRun() *keelclient.ChatRunbook {
	return &keelclient.ChatRunbook{ID: "ru1", RunbookID: "rb1", Name: "Marker check", PickedBy: "user", Status: "active", Turn: 1, PinHost: "api-1", PinSessionID: "s1",
		Steps: []keelclient.RunStep{stepOf("st1", "Print one", "echo rb-1", "next"), stepOf("st2", "Print two", "echo rb-2", "next"), stepOf("st3", "Print three", "echo rb-3", "next")}}
}

func stepProposal(n int, placeholders bool) keelclient.Proposal {
	cmd := "echo rb-" + string(rune('0'+n))
	return keelclient.Proposal{ID: "p" + string(rune('0'+n)), SessionID: "s1", HostID: "h-api", Host: "api-1", Command: cmd, Bytes: "\x15" + cmd + "\n", Risk: "readonly", Placeholders: placeholders,
		Runbook: &keelclient.ProposalStep{RunbookID: "rb1", Name: "Marker check", Step: n, Of: 3, StepID: "st" + string(rune('0'+n))}}
}

// walkAnswers makes the fake propose the current step of the run, as keel serve does: only for the step it is on.
func (r *rig) walkAnswers() {
	r.fb.mem.run = newRun()
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		run := r.fb.mem.run
		cur := currentStep(run)
		var props []keelclient.Proposal
		if cur >= 0 && run.Status == "active" {
			run.Steps[cur].State, run.Steps[cur].ProposalRef = "proposed", "p"+string(rune('1'+cur))
			props = append(props, stepProposal(cur+1, false))
		}
		return keelclient.AskResponse{ConversationID: req.ConversationID, Answer: "ok", Proposals: props, Runbook: run,
			Trace: []keelclient.TraceStep{{Status: "runbook", Runbook: "Marker check"}}}, nil
	}
}

func TestAWalkMovesOneStepAtATimeAndEveryStepIsTypedOnlyThroughApproveBeforeType(t *testing.T) {
	r := rbRig(t)
	r.fb.mem.runbooks = []keelclient.Runbook{markerBook()}
	r.w.loadRunbooks()
	r.tt.Frame()
	r.walkAnswers()
	// Use in agent: an ask with the runbook's id, never a type.
	r.click(t, "Open Marker check")
	r.click(t, "Use in agent")
	if len(r.fb.agent.asks) != 1 || r.fb.agent.asks[0].RunbookID != "rb1" || r.fb.agent.asks[0].Question != "Walk me through the runbook “Marker check”." {
		t.Fatalf("asks %+v", r.fb.agent.asks)
	}
	if len(r.typed()) != 0 {
		t.Fatal("starting a runbook typed")
	}
	if miss := r.lacks("Runbook", "Run on api-1", "You picked this runbook.", "Step 1 · Run on", "Used runbook · Marker check",
		"Step 1 is proposed below. Nothing is typed until you press Type.", "Runbook steps are proposals. Each one waits for you. The agent cannot skip ahead or type for you."); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	// Step 1's card is an ordinary proposal: Type opens Approve before type with the proposal's bytes.
	r.click(t, "Type into pane…")
	if r.w.approve == nil || len(r.typed()) != 0 {
		t.Fatal("Type did not open the dialog, or typed")
	}
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15echo rb-1\n"}) {
		t.Fatalf("typed %q", got)
	}
	if ref := r.fb.conns[0].agentRefs[0]; ref == nil || ref.ProposalID != "p1" {
		t.Fatalf("reference %+v", ref)
	}
	if !r.w.agent.runs.pollForce {
		t.Fatal("a typed step does not make the window read its result")
	}
	// The pane's shell reports exit 0: the result is read, and a production-style confirm waits for the user.
	exit := 0
	r.fb.mem.run.Steps[0].State, r.fb.mem.run.Steps[0].Result, r.fb.mem.run.Steps[0].Exit = "typed", "reported", &exit
	r.w.runTick()
	r.tt.Frame()
	if r.fb.mem.stateGets != 1 || !r.tt.HasText("exit 0 · reported — confirm") {
		t.Fatalf("state reads %d", r.fb.mem.stateGets)
	}
	r.click(t, "Confirm done")
	conv := r.fb.agent.asks[0].ConversationID
	if !slices.Equal(r.fb.mem.actions, []string{conv + "/st1:done"}) {
		t.Fatalf("actions %v", r.fb.mem.actions)
	}
	// Step 2 is asked for, by name; nothing else is.
	if !r.has("Propose step 2") || r.has("Propose step 3") {
		t.Fatal("the card does not offer exactly step 2")
	}
	r.click(t, "Propose step 2")
	if q := r.fb.agent.asks[1]; q.Question != "Continue with step 2 of 3: Print two" || q.RunbookID != "" {
		t.Fatalf("asked %+v", q)
	}
	if !r.tt.HasText("Step 2 · Run on") {
		t.Fatal("no card for step 2")
	}
	// Skip it: step 2 is never typed.
	r.click(t, "Skip")
	if r.fb.mem.actions[1] != conv+"/st2:skip" || len(r.typed()) != 1 {
		t.Fatalf("actions %v, typed %q", r.fb.mem.actions, r.typed())
	}
	r.click(t, "Propose step 3")
	r.w.typeAgentProposal("p3", false)
	r.tt.Frame()
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15echo rb-1\n", "\x15echo rb-3\n"}) {
		t.Fatalf("typed %q", got)
	}
	// The user marks the last step; the walk ends.
	r.fb.mem.run.Steps[2].State = "typed"
	r.w.runTick()
	r.tt.Frame()
	r.click(t, "Mark done")
	if !r.tt.HasText("All steps are done or skipped.") || r.fb.mem.actions[len(r.fb.mem.actions)-1] != conv+"/st3:done" {
		t.Fatalf("actions %v", r.fb.mem.actions)
	}
	if r.fb.conns[0].wrote != 0 {
		t.Error("the keystroke path was used")
	}
}

func TestRetryResetsTheStepAndAsksForItAgain(t *testing.T) {
	r := agentRig(t)
	r.walkAnswers()
	r.fb.mem.run.Steps[0].State, r.fb.mem.run.Steps[0].Result, r.fb.mem.run.Steps[0].Exit = "failed", "reported", func() *int { n := 3; return &n }()
	r.w.setRun(r.fb.mem.run, nil)
	r.w.agent.turns = []*agentTurn{{question: "q", done: true, runID: "ru1"}}
	r.tt.Frame()
	if miss := r.lacks("Step 1 failed · exit 3, reported by the shell. The runbook waits for you.", "✗", "exit 3 · reported"); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	r.click(t, "Retry step 1")
	if len(r.fb.mem.actions) != 1 || !strings.HasSuffix(r.fb.mem.actions[0], "/st1:retry") || len(r.fb.agent.asks) != 1 || r.fb.agent.asks[0].Question != "Continue with step 1 of 3: Print one" {
		t.Fatalf("actions %v, asks %+v", r.fb.mem.actions, r.fb.agent.asks)
	}
	if len(r.typed()) != 0 {
		t.Fatal("Retry typed")
	}
}

func TestAStepWithAPlaceholderHasNoTypeButtonAndIsNeverTyped(t *testing.T) {
	r := agentRig(t)
	r.fb.mem.run = newRun()
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		r.fb.mem.run.Steps[0].State, r.fb.mem.run.Steps[0].ProposalRef = "proposed", "p1"
		p := stepProposal(1, true)
		p.Command, p.Bytes = "echo [secret:1]", "\x15echo [secret:1]\n"
		return keelclient.AskResponse{ConversationID: req.ConversationID, Answer: "ok", Proposals: []keelclient.Proposal{p}, Runbook: r.fb.mem.run}, nil
	}
	r.ask(t, "walk me through it")
	if !r.tt.HasText("It holds a redacted placeholder such as [secret:1], which a shell would read as a pattern. Keel will not type it: edit it to the real value or an environment variable first.") {
		t.Fatal("the app's sentence is not shown")
	}
	if r.has("Type into pane…") {
		t.Fatal("a step with a placeholder has a Type button")
	}
	for _, shift := range []bool{false, true} {
		r.w.typeAgentProposal("p1", shift)
		r.tt.Frame()
	}
	if r.w.approve != nil || len(r.typed()) != 0 || len(r.fb.conns[0].directs) != 0 {
		t.Fatal("a step with a placeholder was typed")
	}
}

func TestDontUseStopsTheRunAndUseAgainStartsItOver(t *testing.T) {
	r := agentRig(t)
	r.walkAnswers()
	r.ask(t, "walk me through it")
	r.click(t, "Don't use")
	if r.fb.mem.stops != 1 || !r.w.agent.runs.none {
		t.Fatal("Don't use did not stop the run")
	}
	if miss := r.lacks("No runbook for this conversation. The agent answers from the panes in scope and your notes only.", "Use Marker check"); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	r.click(t, "Use Marker check")
	if n := len(r.fb.agent.asks); n != 2 || r.fb.agent.asks[1].RunbookID != "rb1" {
		t.Fatalf("asks %+v", r.fb.agent.asks)
	}
}

func TestProposeStepNeedsAPaneInScopeAndUseADifferentRunbookSaysWhyOneIsNotOffered(t *testing.T) {
	r := agentRig(t)
	r.walkAnswers()
	r.ask(t, "walk me through it")
	other := markerBook()
	other.ID, other.Name, other.Hosts = "rb2", "Staging only", []string{"staging"}
	fit := markerBook()
	fit.ID, fit.Name, fit.Hosts = "rb3", "Production check", []string{"production"}
	r.fb.mem.runbooks = []keelclient.Runbook{markerBook(), other, fit}
	r.fb.mem.run.Steps[0].State = "next"
	r.w.agent.runs.run = r.fb.mem.run
	r.click(t, "Use a different runbook")
	r.tt.Frame()
	if miss := r.lacks("Not offered: no staging host in scope", "Production check"); len(miss) > 0 || r.tt.HasText("Marker check ·") {
		t.Fatalf("missing %q", miss)
	}
	r.click(t, "Use Production check")
	if n := len(r.fb.agent.asks); n != 2 || r.fb.agent.asks[1].RunbookID != "rb3" {
		t.Fatalf("asks %+v", r.fb.agent.asks)
	}
	// With no pane in scope Propose step N sends nothing and says why.
	r.w.setAgentScope(nil)
	r.tt.Frame()
	r.fb.mem.run.Steps[0].State = "next"
	r.w.agent.runs.run = r.fb.mem.run
	r.w.proposeRunStep()
	r.tt.Frame()
	if len(r.fb.agent.asks) != 2 || !r.tt.HasText("Put a pane in scope first: a step is proposed only for a pane the agent reads.") {
		t.Fatalf("asks %d", len(r.fb.agent.asks))
	}
}

func TestOnlyTheCurrentStepIsEverAnsweredOrChangedFromTheCard(t *testing.T) {
	// Each button on a fresh run: it acts on the current step (st1) and on no later one.
	for _, b := range []string{"Skip", "Mark done"} {
		r := agentRig(t)
		r.walkAnswers()
		r.ask(t, "walk me through it")
		r.click(t, b)
		if got := r.fb.mem.actions[len(r.fb.mem.actions)-1]; !strings.HasSuffix(got, "/st1:skip") && !strings.HasSuffix(got, "/st1:done") {
			t.Fatalf("%s acted on another step: %v", b, r.fb.mem.actions)
		}
		for _, a := range r.fb.mem.actions {
			if strings.Contains(a, "st2") || strings.Contains(a, "st3") {
				t.Fatalf("%s changed a later step: %v", b, r.fb.mem.actions)
			}
		}
	}
}

func TestTheRunIsReadAgainOnlyWhileATypedStepWaitsForItsResult(t *testing.T) {
	r := agentRig(t)
	run := newRun()
	run.Steps[0].State = "done"
	run.Steps[1].State = "proposed"
	r.fb.mem.run = run
	r.w.setRun(run, nil)
	r.w.runTick()
	if r.fb.mem.stateGets != 0 {
		t.Fatal("the run was read with no typed step waiting")
	}
	run.Steps[1].State = "typed"
	r.w.setRun(run, nil)
	for i := 0; i < 3; i++ {
		r.w.runTick()
	}
	if r.fb.mem.stateGets != 3 {
		t.Fatalf("%d reads", r.fb.mem.stateGets)
	}
	run.Steps[1].Result = "abandoned"
	r.w.setRun(run, nil)
	r.w.runTick()
	if r.fb.mem.stateGets != 3 {
		t.Fatal("the run was read after the step had its result")
	}
}

func TestAResumedConversationShowsTheRunUnderTheTurnThatStartedIt(t *testing.T) {
	r := agentRig(t)
	run := newRun()
	run.Turn, run.Steps[0].State, run.Steps[0].Result = 2, "done", "user"
	r.w.showTranscript(keelclient.Transcript{ID: "c-saved-2", Title: "Walk", Runbook: run, Turns: []keelclient.SavedTurn{{Question: "one", Answer: "a"}, {Question: "two", Answer: "b"}}})
	r.tt.Frame()
	if r.w.agent.turns[0].runID != "" || r.w.agent.turns[1].runID != "ru1" {
		t.Fatalf("placed on %q / %q", r.w.agent.turns[0].runID, r.w.agent.turns[1].runID)
	}
	if miss := r.lacks("Marker check", "marked done", "Next: step 2 · Print two", "Propose step 2"); len(miss) > 0 {
		t.Errorf("missing %q", miss)
	}
	if len(r.fb.agent.asks) != 0 || len(r.fb.mem.actions) != 0 {
		t.Fatal("showing a saved run asked or changed something")
	}
}
