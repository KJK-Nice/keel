package native

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Attaching notebooks to a question, "Ask the agent about this notebook", and the trace lines (plan 024, step 3). These are
// the panel cases of cmd/keel/web/js/test/notebooks.test.mjs (an ask names no notebook until one is attached, a notebook
// deleted after it was attached is not named, a private notebook is listed but cannot be attached, an ask saves typing first
// and names notebooks still saving) and the picker and chips of the composer. A made-up text stands for a notebook's words:
// it must never be in a request.

const nbSecretText = "nb-text-that-must-not-leave-as-text-0451"

// nbAgentRig is an agent rig with three saved notebooks (Deploy nb1, Backups nb2, Scratch nb3) and a private one (Keys nb4).
func nbAgentRig(t *testing.T) *rig {
	t.Helper()
	r := agentRig(t)
	r.tt.SetSize(1400, 1000)
	for _, n := range []string{"Deploy", "Backups", "Scratch", "Keys"} {
		r.fb.addNotebook(n, nbSecretText+" "+n)
	}
	r.fb.nb.list[3].Private = true
	r.w.reloadNotebooks()
	r.frames(2)
	return r
}

func (r *rig) lastAsk(t *testing.T) keelclient.AskRequest {
	t.Helper()
	if len(r.fb.agent.asks) == 0 {
		t.Fatal("no ask was sent")
	}
	return r.fb.agent.asks[len(r.fb.agent.asks)-1]
}

// closePopover presses Escape, which closes the picker as a click outside it does.
func (r *rig) closePopover() {
	r.tt.Key(0, ui.KeyEscape)
	r.frames(2)
	for range 4 { // the toasts sit over the composer: close them, as a person does
		if r.tt.Click("Close notification") != nil {
			break
		}
		r.tt.Frame()
	}
}

func (r *rig) openNbPicker(t *testing.T) {
	t.Helper()
	r.click(t, "Attach notebook")
	if r.w.agent.nbPicker != "panel" || !r.tt.HasText("Notebooks to send with your question") {
		t.Fatalf("the picker is not open: %q", r.tt.Texts())
	}
}

// agentpanel.js nbPickerKeys: ↓ and ↑ move the keyboard between the rows, wrapping at both ends, and tick nothing.
func TestThePickersArrowKeysWalkBetweenRowsAndTickNothing(t *testing.T) {
	r := nbAgentRig(t)
	r.openNbPicker(t)
	// The picker lists them by name: Backups, Deploy, Keys (private), Scratch.
	rows := []string{"pick notebook Backups", "pick notebook Deploy", "pick notebook Keys", "pick notebook Scratch"}
	r.click(t, rows[2]) // the private one: a click gives it the keyboard and ticks nothing
	if !r.tt.Focused(rows[2]) {
		t.Fatal("the click did not give the row the keyboard")
	}
	for i, want := range []string{rows[3], rows[0], rows[1], rows[2]} { // ↓ four times: to the end, around it, then down the list
		r.tt.Key(0, ui.KeyDown)
		r.tt.Frame()
		r.tt.Frame()
		if !r.tt.Focused(want) {
			t.Fatalf("after %d ↓ the keyboard is not on %q", i+1, want)
		}
	}
	r.tt.Key(0, ui.KeyUp)
	r.tt.Frame()
	r.tt.Frame()
	if !r.tt.Focused(rows[1]) {
		t.Fatalf("↑ from Keys did not go to the row before it")
	}
	r.click(t, rows[0])
	r.click(t, rows[0]) // ticked and un-ticked again; the first row has the keyboard
	r.tt.Key(0, ui.KeyUp)
	r.tt.Frame()
	r.tt.Frame()
	if !r.tt.Focused(rows[3]) {
		t.Fatalf("↑ from the first row did not wrap to the last")
	}
	if len(r.w.agent.attached) != 0 {
		t.Errorf("walking ticked %v", r.w.agent.attached)
	}
}

func TestThePickerListsSavedNotebooksAndATickedOneIsSentByIdWithEachQuestion(t *testing.T) {
	r := nbAgentRig(t)
	r.ask(t, "first")
	if a := r.lastAsk(t); a.Notebooks != nil || a.Editing != nil {
		t.Fatalf("nothing attached, nothing named: %+v", a)
	}

	r.openNbPicker(t)
	if miss := r.lacks("pick notebook Deploy", "pick notebook Backups", "pick notebook Scratch", "pick notebook Keys", "Private",
		"At most 2, sent in full. The agent also sees the titles of your notebooks that are not private and may read them; it never sees a private one."); len(miss) > 0 {
		// Labels are found, not drawn: look them up as labels too.
		for _, m := range miss {
			if _, ok := r.tt.Find(m); !ok {
				t.Errorf("the picker lacks %q", m)
			}
		}
	}
	// A private notebook is listed and cannot be ticked: it says why and attaches nothing.
	r.click(t, "pick notebook Keys")
	if len(r.w.agent.attached) != 0 || !r.tt.HasText(privateRefused) {
		t.Fatalf("a private notebook was attached %v or no reason shown", r.w.agent.attached)
	}
	r.click(t, "pick notebook Deploy")
	r.click(t, "pick notebook Backups")
	if !slices.Equal(r.w.agent.attached, []string{"nb1", "nb2"}) {
		t.Fatalf("attached %v", r.w.agent.attached)
	}
	// A third is refused with the app's words, and the two stay.
	r.click(t, "pick notebook Scratch")
	if !slices.Equal(r.w.agent.attached, []string{"nb1", "nb2"}) || !r.tt.HasText("Attach at most 2 notebooks to a question") {
		t.Fatalf("a third notebook was attached %v or no reason shown", r.w.agent.attached)
	}
	r.closePopover()
	if r.w.agent.nbPicker != "" {
		t.Fatal("the picker stays open")
	}

	// Chips with Stop sending, and the hint under them.
	if _, ok := r.tt.Find("attached Deploy"); !ok {
		t.Fatalf("no chip: %q", r.tt.Texts())
	}
	if miss := r.lacks("Stop sending Deploy", "Stop sending Backups", "These notebooks are sent, redacted, with each question until you remove them."); len(miss) > 0 {
		for _, m := range miss {
			if _, ok := r.tt.Find(m); !ok {
				t.Errorf("no %q", m)
			}
		}
	}

	r.ask(t, "second")
	a := r.lastAsk(t)
	if !slices.Equal(a.Notebooks, []string{"nb1", "nb2"}) {
		t.Fatalf("the ask names %v", a.Notebooks)
	}
	// The request holds ids and never a notebook's text.
	body, _ := json.Marshal(a)
	if strings.Contains(string(body), nbSecretText) {
		t.Fatalf("the ask carries notebook text: %s", body)
	}
	// Still attached for the next question, until removed.
	r.ask(t, "third")
	if !slices.Equal(r.lastAsk(t).Notebooks, []string{"nb1", "nb2"}) {
		t.Fatalf("not kept for the next question: %v", r.lastAsk(t).Notebooks)
	}
	r.click(t, "Stop sending Deploy")
	r.ask(t, "fourth")
	if !slices.Equal(r.lastAsk(t).Notebooks, []string{"nb2"}) {
		t.Fatalf("after removing Deploy: %v", r.lastAsk(t).Notebooks)
	}
	// A new conversation starts with none.
	r.w.newAgentConversation()
	if len(r.w.agent.attached) != 0 {
		t.Fatal("a new conversation keeps the attached notebooks")
	}
}

func TestANotebookDeletedOrMadePrivateAfterItWasAttachedIsNotNamed(t *testing.T) {
	r := nbAgentRig(t)
	r.w.attachNotebook("nb1")
	r.w.attachNotebook("nb2")
	r.w.attachNotebook("nb-unknown") // not a notebook: not attached
	if !slices.Equal(r.w.agent.attached, []string{"nb1", "nb2"}) {
		t.Fatalf("attached %v", r.w.agent.attached)
	}
	r.fb.nb.list = r.fb.nb.list[1:] // nb1 deleted elsewhere
	r.fb.nb.list[0].Private = true  // nb2 made private elsewhere
	r.w.reloadNotebooks()
	r.frames(2)
	if len(r.w.agent.attached) != 0 {
		t.Fatalf("still attached: %v", r.w.agent.attached)
	}
	r.ask(t, "q")
	if a := r.lastAsk(t); a.Notebooks != nil {
		t.Fatalf("named %v", a.Notebooks)
	}
}

func TestTheAskSavesTypingFirstAndNamesNotebooksStillUnsaved(t *testing.T) {
	r := nbAgentRig(t)
	p := r.openNB(t, "nb2")
	p.nb.md += " typed since"
	r.frames(1)
	if !p.nb.dirty() {
		t.Fatal("the edit was not noticed")
	}
	r.ask(t, "q")
	if got := r.fb.nb.list[1].Markdown(); !strings.HasSuffix(got, "typed since") {
		t.Fatalf("typing was not saved before the ask: %q", got)
	}
	puts := r.fb.nb.reqsWith("put nb2")
	if puts != 1 || r.lastAsk(t).Editing != nil {
		t.Fatalf("saves %d, editing %v: the save is done, so nothing is still being typed", puts, r.lastAsk(t).Editing)
	}

	// A save that fails leaves typing that is only here: the ask goes and names the notebook, so the agent's edit is refused.
	p.nb.md += " and more"
	r.frames(1)
	r.fb.nb.saveErr = errors.New("down")
	r.ask(t, "q2")
	if !slices.Equal(r.lastAsk(t).Editing, []string{"nb2"}) {
		t.Fatalf("editing %v", r.lastAsk(t).Editing)
	}
	if !strings.Contains(p.nb.md, "and more") {
		t.Fatal("the typing was lost")
	}
}

func TestAskTheAgentAboutThisNotebookFromTheRowMenuAttachesOneAndSendsNothing(t *testing.T) {
	r := nbAgentRig(t)
	r.w.setAgentOpen(false)
	r.w.openPage("notebooks")
	r.frames(3)
	r.tt.RightClick("notebook Backups")
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Contains(got, "Ask the agent about this notebook") {
		t.Fatalf("menu %q", got)
	}
	if err := r.tt.ChooseMenuItem("Ask the agent about this notebook"); err != nil {
		t.Fatal(err)
	}
	r.frames(2)
	if !r.w.agent.open || !slices.Equal(r.w.agent.attached, []string{"nb2"}) || len(r.fb.agent.asks) != 0 {
		t.Fatalf("open %v attached %v asks %d: the panel opens with the notebook attached and nothing is asked", r.w.agent.open, r.w.agent.attached, len(r.fb.agent.asks))
	}
	// A private notebook is never offered to the agent.
	r.tt.RightClick("notebook Keys")
	r.tt.Frame()
	if got := r.tt.Menu(); slices.Contains(got, "Ask the agent about this notebook") {
		t.Fatalf("a private notebook offers the agent: %q", got)
	}
}

func TestTheNotebookPaneBarAsksTheAgentOnlyForASavedNotebookThatIsNotPrivate(t *testing.T) {
	r := nbAgentRig(t)
	r.w.setAgentOpen(false)
	r.openNB(t, "nb1")
	r.click(t, "Ask the agent about this notebook")
	if !r.w.agent.open || !slices.Equal(r.w.agent.attached, []string{"nb1"}) {
		t.Fatalf("open %v attached %v", r.w.agent.open, r.w.agent.attached)
	}

	// Not saved yet: the app's sentence, nothing attached.
	r2 := nbAgentRig(t)
	r2.w.agent.attached = nil
	np := r2.w.newNotebookPane(nil)
	r2.w.placeNotebook(np)
	r2.frames(2)
	r2.click(t, "Ask the agent about this notebook")
	if len(r2.w.agent.attached) != 0 || !r2.tt.HasText("Save the notebook first, then ask the agent about it") {
		t.Fatalf("attached %v: an unsaved notebook has no id to send", r2.w.agent.attached)
	}

	// Private: not offered at all.
	r3 := nbAgentRig(t)
	r3.openNB(t, "nb4")
	if _, ok := r3.tt.Find("Ask the agent about this notebook"); ok {
		t.Fatal("the pane bar offers the agent a private notebook")
	}
	r3.w.askAboutNotebook("nb4") // and if it is asked anyway, the client refuses before the server does
	r3.frames(1)
	if len(r3.w.agent.attached) != 0 || !r3.tt.HasText(privateRefused) {
		t.Fatalf("attached %v", r3.w.agent.attached)
	}
}

func TestTheTraceDrawsTheNotebookLinesOfKeelServe(t *testing.T) {
	r := nbAgentRig(t)
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{ConversationID: req.ConversationID, Answer: "ok", Trace: []keelclient.TraceStep{
			{Status: "notebook-index", Count: 3},
			{Status: "notebook", Notebook: "Deploy", NotebookID: "nb1", Bytes: 2048, Redactions: map[string]int{"secret": 2}},
			{Status: "notebook", Notebook: "Backups", Bytes: 10, Asked: true},
			{Status: "skipped", Reason: "notebook-private"},
			{Status: "skipped", Reason: "notebook-missing"},
			{Status: "skipped", Reason: "notebook-limit"},
		}}, nil
	}
	r.ask(t, "q")
	if miss := r.lacks("Sent the titles of 3 notebooks (not private)", "Sent notebook Deploy (2048 bytes, 2 redacted)", "Read notebook Backups (10 bytes, 0 redacted) · the agent asked",
		"Not sent · a private notebook (the agent cannot read it)", "Skipped a notebook · it was deleted", "Stopped reading notebooks · Keel's limit for one question"); len(miss) > 0 {
		t.Fatalf("missing %q in %q", miss, r.tt.Texts())
	}
}

func TestTheAgentPageComposerHasTheSamePickerAndChips(t *testing.T) {
	r := nbAgentRig(t)
	r.w.openAgentPage()
	r.openPageSettled()
	r.w.setAgentOpen(false)
	r.frames(2)
	r.click(t, "Attach notebook")
	if r.w.agent.nbPicker != "page" || !r.tt.HasText("Notebooks to send with your question") {
		t.Fatalf("picker %q", r.w.agent.nbPicker)
	}
	r.click(t, "pick notebook Scratch")
	r.closePopover()
	if _, ok := r.tt.Find("attached Scratch"); !ok || !r.tt.HasText("This notebook is sent, redacted, with each question until you remove it.") {
		t.Fatalf("no chip or hint on the page: %q", r.tt.Texts())
	}
	r.ask2(t, "from the page")
	if !slices.Equal(r.lastAsk(t).Notebooks, []string{"nb3"}) {
		t.Fatalf("the page's ask names %v", r.lastAsk(t).Notebooks)
	}
}

// ---- a runbook draft from a notebook (plan 024, step 5) ----

func TestADraftFromANotebookIsOneRequestWithTheIdAndIsNotKeptUntilSave(t *testing.T) {
	r := rbRig(t)
	r.fb.addNotebook("Disk check", nbSecretText+"\n\n## Look\n\n```sh\ndf -h\n```\n")
	r.fb.addNotebook("Keys", "private words")
	r.fb.nb.list[1].Private = true
	r.w.reloadNotebooks()
	r.frames(2)
	r.click(t, "From a notebook…")
	if miss := r.lacks("Make a draft from a notebook: its headings become steps, its shell blocks their commands."); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	for _, l := range []string{"draft from notebook Disk check", "draft from notebook Keys"} {
		if _, ok := r.tt.Find(l); !ok {
			t.Fatalf("no %q", l)
		}
	}
	r.fb.nbc = &fakeNBCards{draft: keelclient.RunbookDraft{Name: "Disk check", Steps: []keelclient.RunbookStep{{Title: "Look", Command: "df -h"}}, Source: "notebook", SourceRef: "Disk check", DraftID: "dr_nb1"}}
	r.click(t, "draft from notebook Disk check")
	if !slices.Equal(r.fb.nbc.draftReq, []string{"nb1"}) || len(r.fb.mem.runbooks) != 0 || len(r.fb.mem.rbSaved) != 0 {
		t.Fatalf("draft requests %v, saved %v", r.fb.nbc.draftReq, r.fb.mem.rbSaved)
	}
	if !r.tt.HasText("A draft from the notebook “Disk check”. Nothing is saved until you press Save, and the agent may not pick it until you turn that on.") || r.w.runbooksPage().ed.agentMayPick {
		t.Fatalf("the draft is not shown as a draft with pick off: %q", r.tt.Texts())
	}
	r.press(ui.Cmd, ui.KeyS)
	if in := r.fb.mem.rbSaved[0]; in.DraftID != "dr_nb1" || in.AgentMayPick || len(r.fb.mem.rbSaved) != 1 {
		t.Fatalf("sent %+v", in)
	}

	// A private notebook: the draft says so (a runbook is not private).
	r.fb.nbc.draft = keelclient.RunbookDraft{Name: "Keys", Steps: []keelclient.RunbookStep{{Title: "Look", Command: "df -h"}}, Source: "notebook", SourceRef: "Keys", DraftID: "dr_nb2", FromPrivate: true}
	r.click(t, "From a notebook…")
	r.click(t, "draft from notebook Keys")
	if !r.onScreen("The notebook is private, but a runbook is not: once saved, the agent can be sent this runbook when you use it.") {
		t.Fatalf("no private-notebook sentence: %q", r.tt.Texts())
	}

	// Nothing to make steps from: 422, and the app's toast with its full words.
	r.press(ui.Cmd, ui.KeyS) // the editor holds no unsaved draft now, so it does not ask
	r.fb.nbc.draftErr = &keelclient.StatusError{Code: 422, Status: "422 Unprocessable Entity", Msg: "nothing to make steps from"}
	r.click(t, "From a notebook…")
	r.click(t, "draft from notebook Disk check")
	if !r.tt.HasText("Nothing to make steps from: no headings or shell blocks, or no proposals.") {
		t.Error("no words for an empty draft")
	}
}

// The app escapes a notebook's name in a chip and in the picker (test "chips and the picker escape notebook names"). The
// native client draws text, never markup, so the hostile name is the characters it is, and a card's title and summary too.
func TestAHostileNotebookNameIsDrawnAsTheCharactersItIs(t *testing.T) {
	const hostile = `<img src=x onerror="alert(1)">`
	r := agentRig(t)
	r.tt.SetSize(1400, 1000)
	r.fb.addNotebook(hostile, "x")
	r.w.reloadNotebooks()
	r.frames(2)
	r.w.attachNotebook("nb1")
	r.frames(2)
	if _, ok := r.tt.Find("attached " + hostile); !ok || !r.tt.HasText(hostile) {
		t.Fatalf("the chip: %q", r.tt.Texts())
	}
	r.openNbPicker(t)
	if _, ok := r.tt.Find("pick notebook " + hostile); !ok {
		t.Fatalf("the picker: %q", r.tt.Texts())
	}
	r.closePopover()
	card := nbCard("edit", "edited")
	card.Title, card.Summary = "<b>title</b>", "<script>x</script>"
	r.answersNbs(card)
	r.ask(t, "q")
	if !r.tt.HasText("<b>title</b>") || !r.tt.HasText("<script>x</script>") {
		t.Fatalf("the card: %q", r.tt.Texts())
	}
}
