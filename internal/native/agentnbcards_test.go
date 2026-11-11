package native

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

// The cards of what the agent did to notebooks (plan 024, step 4): every kind and status draws the app's head, status and
// buttons; each button is one request to keel serve and the card shows what it answers; Delete asks first; an agent edit of
// an open notebook shows in its pane, or is refused while the user is typing. Cards are also in saved conversations.

func nbCard(kind, status string) keelclient.NotebookProposal {
	return keelclient.NotebookProposal{ID: "nbc_1", Kind: kind, Status: status, NotebookID: "nb1", Title: "Deploy", Bytes: 40, Rev: 2}
}

func TestEveryKindAndStatusIsDrawnWithTheAppsHeadStatusAndButtons(t *testing.T) {
	type row struct {
		kind, status, reason string
		versionID            string
		head                 string
		buttons              []string
	}
	rows := []row{
		{"create", "created", "", "", "Created notebook", []string{"Open", "Undo"}},
		{"create", "undone", "", "nbv_1", "Created notebook", []string{"Restore"}},
		{"create", "refused", "no-match", "", "Notebook not created", nil},
		{"edit", "edited", "", "nbv_1", "Edited notebook", []string{"Open", "Undo"}},
		{"edit", "undone", "", "", "Edited notebook", []string{"Open"}},
		{"edit", "refused", "conflict", "", "Notebook not changed", []string{"Open"}},
		{"edit", "refused", "editing", "", "Notebook not changed", []string{"Open"}},
		{"edit", "refused", "private", "", "Notebook not changed", nil},
		{"delete", "proposed", "", "", "Delete notebook?", []string{"Open", "Keep", "Delete…"}},
		{"delete", "deleted", "", "nbv_9", "Delete notebook", []string{"Restore"}},
		{"delete", "dismissed", "", "", "Delete notebook", []string{"Open"}},
		{"delete", "restored", "", "", "Delete notebook", []string{"Open"}},
		{"", "saved", "", "", "Notebook draft", []string{"Open"}},
		{"", "dismissed", "", "", "Notebook draft", nil},
		{"", "open", "", "", "Notebook draft", nil},
	}
	all := []string{"Open", "Undo", "Restore", "Keep", "Delete…"}
	for _, c := range rows {
		r := nbAgentRig(t)
		p := nbCard(c.kind, c.status)
		p.Reason, p.VersionID = c.reason, c.versionID
		r.answersNbs(p)
		r.ask(t, "q")
		name := c.kind + "/" + c.status + "/" + c.reason
		if _, ok := r.tt.Find(c.head); !ok {
			t.Errorf("%s: no head %q in %q", name, c.head, r.tt.Texts())
		}
		if want := cardStatus(p); want != "" && !r.tt.HasText(want) {
			t.Errorf("%s: no status %q", name, want)
		}
		if !r.tt.HasText("Deploy") {
			t.Errorf("%s: no title", name)
		}
		for _, b := range all {
			_, has := r.tt.Find(b + " Deploy")
			if want := slices.Contains(c.buttons, b); has != want {
				t.Errorf("%s: button %q is %v, want %v", name, b, has, want)
			}
		}
	}
}

func TestACardShowsItsBadgesTheSizeTheSummaryAndTheWarningAfterAPaneWasRead(t *testing.T) {
	r := nbAgentRig(t)
	p := nbCard("edit", "edited")
	p.Summary, p.PanesRead, p.FromPane, p.SeenIn = "Adds a restart step", []string{"api-1"}, true, "api-1"
	p.Placeholders, p.Withheld = true, true
	r.answersNbs(p)
	r.ask(t, "q")
	if miss := r.lacks("Edited by agent", "From pane output", "Placeholders", "Adds a restart step", "40 bytes · rev 2", paneWarning(p),
		"It names values Keel keeps from the agent as {{name}}. Replace them before you use a command.",
		"It holds placeholders such as [secret:1]. Fill them in before you use a command: Keel will not type a block that holds one."); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	// Read panes, text not from them: the other badge.
	r2 := nbAgentRig(t)
	q := nbCard("create", "created")
	q.PanesRead = []string{"api-1"}
	r2.answersNbs(q)
	r2.ask(t, "q")
	if miss := r2.lacks("From agent", "After reading panes"); len(miss) > 0 {
		t.Fatalf("missing %q", miss)
	}
	// No warning when no pane was read.
	r3 := nbAgentRig(t)
	r3.answersNbs(nbCard("create", "created"))
	r3.ask(t, "q")
	if r3.onScreen("The agent wrote this after reading terminal output") || r3.onScreen("After reading panes") {
		t.Fatal("a card shows the warning without a pane read")
	}
	// A delete card gives its why with "Why: ".
	r4 := nbAgentRig(t)
	d := nbCard("delete", "proposed")
	d.Summary = "It is out of date"
	r4.answersNbs(d)
	r4.ask(t, "q")
	if !r4.tt.HasText("Why: It is out of date") {
		t.Fatalf("no why: %q", r4.tt.Texts())
	}
}

func TestACreatedNotebookIsInTheListMarkedFromAgentAndItsUndoMovesItToRecentlyDeleted(t *testing.T) {
	r := nbAgentRig(t)
	rec := r.fb.agentCreates("Runbook for disks", "# Disks\n")
	card := nbCard("create", "created")
	card.NotebookID, card.Title, card.Record = rec.ID, rec.Name, &rec
	r.answersNbs(card)
	r.ask(t, "make a notebook")
	if nb, ok := r.w.notebook(rec.ID); !ok || nb.Source != byAgent || !nb.AgentUnreviewed {
		t.Fatalf("the notebook the agent made is not in the list: %+v", nb)
	}
	if card := r.w.agent.nbProps["nbc_1"]; card == nil || card.p.Record != nil {
		t.Fatal("the card keeps the notebook it carried")
	}
	r.w.sideSeg = "snippets"
	r.frames(2)
	if _, ok := r.tt.Find("sidebar notebook Runbook for disks"); !ok {
		t.Fatalf("not in the sidebar: %q", r.tt.Texts())
	}
	r.openNB(t, rec.ID)
	if !r.tt.HasText("Written by the agent") {
		t.Fatalf("no banner on the notebook the agent wrote: %q", r.tt.Texts())
	}

	r.fb.nbCards().answer = func(route, id string) (keelclient.NotebookAction, error) {
		vid := r.fb.agentRemove(rec.ID)
		p := card
		p.Record, p.Status, p.VersionID = nil, "undone", vid
		return keelclient.NotebookAction{Proposal: p}, nil
	}
	r.click(t, "Undo Runbook for disks")
	if n := len(r.fb.nbCards().reqs); n != 1 || r.fb.nbCards().reqs[0] != "undo "+r.w.agent.conv+"/nbc_1" {
		t.Fatalf("Undo must be one request: %v", r.fb.nbCards().reqs)
	}
	r.frames(2)
	if _, ok := r.w.notebook(rec.ID); ok || len(r.fb.nb.trash) != 1 {
		t.Fatal("the notebook is still listed, or not in Recently deleted on the server")
	}
	if !r.tt.HasText("Undone. The notebook is in Notebooks · Recently deleted.") {
		t.Fatalf("the card does not say so: %q", r.tt.Texts())
	}
	if _, ok := r.tt.Find("Restore Runbook for disks"); !ok {
		t.Fatal("no Restore after Undo of a create")
	}
	if len(r.notebookPanes()) != 0 {
		t.Fatal("the pane of a notebook that is gone stays open")
	}
	// Restore brings it back: one more request, and the answer's notebook is in the list.
	r.fb.nbCards().answer = func(route, id string) (keelclient.NotebookAction, error) {
		nb, err := r.fb.RestoreDeletedNotebook(context.Background(), "tnb"+strings.TrimPrefix(rec.ID, "nb"))
		_ = err
		p := card
		p.Record, p.Status = nil, "created"
		return keelclient.NotebookAction{Proposal: p, Notebook: &nb}, nil
	}
	r.click(t, "Restore Runbook for disks")
	r.frames(2)
	if _, ok := r.w.notebook(rec.ID); !ok || len(r.fb.nbCards().reqs) != 2 {
		t.Fatalf("restored? routes %v", r.fb.nbCards().reqs)
	}
}

func (r *rig) notebookPanes() []*pane { return r.w.notebookPanes() }

func TestAnAgentEditShowsInTheOpenPaneWithItsBannerAndUndoBringsTheOldTextBack(t *testing.T) {
	r := nbAgentRig(t)
	p := r.openNB(t, "nb1")
	old := p.nb.md
	rec := r.fb.agentEdits("nb1", old+"\nRestart the service.")
	card := nbCard("edit", "edited")
	card.Record, card.VersionID = &rec, "bv1"
	r.answersNbs(card)
	r.ask(t, "add a step")
	r.frames(3)
	if p.nb.md != rec.Markdown() || p.nb.rev != rec.Rev {
		t.Fatalf("the open pane does not show the agent's edit: %q", p.nb.md)
	}
	if !r.tt.HasText("Edited by the agent") || !r.tt.HasText(agentEditReview) {
		t.Fatalf("no review banner: %q", r.tt.Texts())
	}
	// Undo is the user's route; the pane then shows the old text.
	r.fb.nbCards().answer = func(route, id string) (keelclient.NotebookAction, error) {
		nb := r.fb.agentUndo("nb1")
		c := card
		c.Record, c.Status = nil, "undone"
		return keelclient.NotebookAction{Proposal: c, Notebook: &nb}, nil
	}
	r.click(t, "Undo Deploy")
	r.frames(3)
	if p.nb.md != old {
		t.Fatalf("Undo did not bring the old text back to the pane: %q", p.nb.md)
	}
	if !r.tt.HasText("Undone. The notebook is back to the version before the agent's edit.") || len(r.fb.nbCards().reqs) != 1 {
		t.Fatalf("routes %v", r.fb.nbCards().reqs)
	}
	if _, ok := r.tt.Find("Undo Deploy"); ok {
		t.Fatal("a card undone offers Undo again")
	}
}

func TestAnEditRefusedWhileTheUserIsTypingKeepsTheTypingAndSaysWhy(t *testing.T) {
	r := nbAgentRig(t)
	p := r.openNB(t, "nb1")
	p.nb.md += " my typing"
	r.frames(1)
	r.fb.nb.saveErr = errAgentDown // the save cannot land, so the typing is only in the window
	refused := nbCard("edit", "refused")
	refused.Reason = "editing"
	r.answersNbs(refused)
	r.ask(t, "edit it")
	if !slices.Equal(r.lastAsk(t).Editing, []string{"nb1"}) {
		t.Fatalf("the ask did not name the notebook being typed in: %v", r.lastAsk(t).Editing)
	}
	if !r.tt.HasText("You were typing in it, so Keel did not apply the agent's edit.") {
		t.Fatalf("the card does not say so: %q", r.tt.Texts())
	}
	if !strings.HasSuffix(p.nb.md, " my typing") {
		t.Fatal("the typing was lost")
	}
	// The failed save left the typing only here; the next edit saves it with the rest.
	r.fb.nb.saveErr = nil
	p.nb.md += "!"
	r.frames(1)
	r.w.nbFlush(p, nil)
	if got := r.fb.nb.list[0].Markdown(); !strings.HasSuffix(got, " my typing!") {
		t.Fatalf("the typing did not save: %q", got)
	}
}

func TestADeleteTheAgentProposesDeletesNothingUntilTheUserConfirms(t *testing.T) {
	r := nbAgentRig(t)
	r.openNB(t, "nb1")
	card := nbCard("delete", "proposed")
	r.answersNbs(card)
	r.ask(t, "delete it")
	if !r.tt.HasText("The agent asks to delete this notebook. Nothing is deleted unless you confirm.") || len(r.fb.nbCards().reqs) != 0 {
		t.Fatal("the proposal deleted something, or is not shown")
	}
	r.settle()
	r.click(t, "Delete… Deploy")
	if !r.tt.HasText(deleteConfirmText(card)) || len(r.fb.nbCards().reqs) != 0 || len(r.fb.nb.list) != 4 {
		t.Fatalf("Delete… must ask first, with the app's question: %q", r.tt.Texts())
	}
	// Cancel posts nothing.
	r.click(t, "Cancel")
	if len(r.fb.nbCards().reqs) != 0 || r.w.confirm != nil {
		t.Fatalf("Cancel posted %v", r.fb.nbCards().reqs)
	}
	// Keep: one request, nothing deleted.
	r.fb.nbCards().answer = func(route, id string) (keelclient.NotebookAction, error) {
		c := card
		c.Status = "dismissed"
		return keelclient.NotebookAction{Proposal: c}, nil
	}
	r.click(t, "Keep Deploy")
	if n := len(r.fb.nbCards().reqs); n != 1 || !strings.HasPrefix(r.fb.nbCards().reqs[0], "keep ") || len(r.fb.nb.list) != 4 || !r.tt.HasText("Kept. Nothing was deleted.") {
		t.Fatalf("Keep: routes %v", r.fb.nbCards().reqs)
	}
	if _, ok := r.w.notebook("nb1"); !ok || len(r.notebookPanes()) != 1 {
		t.Fatal("Keep deleted something")
	}
}

func TestConfirmingADeleteRequestsItOnceThenRestoreBringsTheNotebookBack(t *testing.T) {
	r := nbAgentRig(t)
	r.openNB(t, "nb1")
	card := nbCard("delete", "proposed")
	r.answersNbs(card)
	r.ask(t, "delete it")
	r.fb.nbCards().answer = func(route, id string) (keelclient.NotebookAction, error) {
		switch route {
		case "delete":
			vid := r.fb.agentRemove("nb1")
			c := card
			c.Status, c.VersionID = "deleted", vid
			return keelclient.NotebookAction{Proposal: c}, nil
		case "restore":
			nb, _ := r.fb.RestoreDeletedNotebook(context.Background(), "tnb1")
			c := card
			c.Status, c.VersionID = "restored", ""
			return keelclient.NotebookAction{Proposal: c, Notebook: &nb}, nil
		}
		return keelclient.NotebookAction{}, errAgentDown
	}
	r.click(t, "Delete… Deploy")
	if len(r.fb.nbCards().reqs) != 0 {
		t.Fatal("asked, not posted")
	}
	r.click(t, "Delete") // the confirm's action
	r.frames(2)
	if n := len(r.fb.nbCards().reqs); n != 1 || !strings.HasPrefix(r.fb.nbCards().reqs[0], "delete ") {
		t.Fatalf("routes %v", r.fb.nbCards().reqs)
	}
	if _, ok := r.w.notebook("nb1"); ok || len(r.notebookPanes()) != 0 {
		t.Fatal("the deleted notebook is still listed or its pane open")
	}
	if !r.tt.HasText("Deleted. It is in Notebooks · Recently deleted.") {
		t.Fatalf("no status: %q", r.tt.Texts())
	}
	r.click(t, "Restore Deploy")
	r.frames(2)
	if _, ok := r.w.notebook("nb1"); !ok || !r.tt.HasText("Restored to Notebooks.") || len(r.fb.nbCards().reqs) != 2 {
		t.Fatalf("restore: routes %v", r.fb.nbCards().reqs)
	}
}

func TestACardOpensItsNotebookAndAnExpiredCardOnlyOpens(t *testing.T) {
	r := nbAgentRig(t)
	r.answersNbs(nbCard("edit", "edited"))
	r.ask(t, "q")
	r.click(t, "Open Deploy")
	if len(r.notebookPanes()) != 1 || r.nbPane(t).nb.id != "nb1" || len(r.fb.nbCards().reqs) != 0 {
		t.Fatal("Open did not show the notebook")
	}
	r2 := nbAgentRig(t)
	e := nbCard("edit", "edited")
	e.Expired = true
	r2.answersNbs(e)
	r2.ask(t, "q")
	if _, ok := r2.tt.Find("Undo Deploy"); ok {
		t.Fatal("an expired card offers Undo")
	}
	if _, ok := r2.tt.Find("Open Deploy"); !ok || !r2.tt.HasText("Keel no longer holds this conversation's changes. Use History on the notebook to see or restore a version.") {
		t.Fatalf("an expired card: %q", r2.tt.Texts())
	}
}

func TestACardRefusedByKeelServeShowsItsWordsAndTheCardStays(t *testing.T) {
	r := nbAgentRig(t)
	r.answersNbs(nbCard("edit", "edited"))
	r.ask(t, "q")
	r.fb.nbCards().answer = func(string, string) (keelclient.NotebookAction, error) {
		return keelclient.NotebookAction{}, &keelclient.StatusError{Code: 410, Msg: "This proposal has expired."}
	}
	r.click(t, "Undo Deploy")
	if !r.tt.HasText("Keel no longer holds this conversation's changes. Use History on the notebook, or Notebooks · Recently deleted.") {
		t.Fatalf("%q", r.tt.Texts())
	}
	r.fb.nbCards().answer = func(string, string) (keelclient.NotebookAction, error) {
		return keelclient.NotebookAction{}, &keelclient.StatusError{Code: 409, Msg: "The notebook changed since then. Open History on the notebook to restore an earlier version."}
	}
	r.click(t, "Undo Deploy")
	if !r.tt.HasText("The notebook changed since then. Open History on the notebook to restore an earlier version.") {
		t.Fatalf("%q", r.tt.Texts())
	}
	if _, ok := r.tt.Find("Undo Deploy"); !ok {
		t.Fatal("a refused Undo took the button away")
	}
}

func TestACardsRightClickMenuIsItsOwnButtonsInOrder(t *testing.T) {
	r := nbAgentRig(t)
	r.answersNbs(nbCard("delete", "proposed"))
	r.ask(t, "q")
	r.tt.RightClick("Delete notebook?")
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Equal(got, []string{"Open", "Keep", "Delete…"}) {
		t.Fatalf("menu %q", got)
	}
	// Delete… from the menu still asks first.
	if err := r.tt.ChooseMenuItem("Delete…"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.confirm == nil || len(r.fb.nbCards().reqs) != 0 {
		t.Fatal("the menu's Delete… did not ask")
	}
}

func TestSavedConversationsShowTheirNotebookCardsAndALegacyDraftHasNoSave(t *testing.T) {
	r := nbAgentRig(t)
	legacy := keelclient.NotebookProposal{ID: "nbc_9", Kind: "", Status: "open", Title: "Old draft", Body: "# Old\n\nbody words of the draft"}
	r.w.showTranscript(keelclient.Transcript{ID: "c-saved-0001", Title: "Earlier", Saved: true, Turns: []keelclient.SavedTurn{
		{Question: "make one", Answer: "done", Notebooks: []keelclient.NotebookProposal{nbCard("create", "created"), legacy}},
	}})
	r.frames(3)
	if miss := r.lacks("Created notebook", "Notebook draft", "Old draft", "A draft from an earlier version of Keel. It was not saved; ask the agent again.", "# Old"); len(miss) > 0 {
		for _, m := range miss {
			if _, ok := r.tt.Find(m); !ok && !r.onScreen(m) {
				t.Errorf("missing %q in %q", m, r.tt.Texts())
			}
		}
	}
	for _, b := range []string{"Save Old draft", "Save", "Save anyway", "Undo Old draft"} {
		if _, ok := r.tt.Find(b); ok {
			t.Errorf("a legacy draft offers %q", b)
		}
	}
	if len(r.fb.nbCards().reqs) != 0 {
		t.Fatal("showing a saved conversation posted a card route")
	}
	// The Agent page shows the same cards.
	r.openPageSettled()
	if _, ok := r.tt.Find("Created notebook"); !ok {
		t.Fatalf("the Agent page lacks the notebook card: %q", r.tt.Texts())
	}
}

// ---- the review banner's actions (plan 024, step 5) ----

// editedRig is a window with notebook nb1 open and an agent edit on it that the user has not reviewed.
func editedRig(t *testing.T) (*rig, *pane, string) {
	t.Helper()
	r := nbAgentRig(t)
	p := r.openNB(t, "nb1")
	old := p.nb.md
	rec := r.fb.agentEdits("nb1", old+"\nRestart the service.")
	r.w.reloadNotebooks()
	r.frames(3)
	if p.nb.md != rec.Markdown() || !r.tt.HasText("Edited by the agent") {
		t.Fatalf("no banner: %q", r.tt.Texts())
	}
	return r, p, old
}

func TestViewChangesOpensHistoryOnTheVersionBeforeTheAgentsEdit(t *testing.T) {
	r, _, _ := editedRig(t)
	r.fb.nb.history["nb1"] = append([]keelclient.NotebookVersion{{ID: "newer", NotebookID: "nb1", Event: "session", By: "user", At: lt(time.March, 4, 9, 0)}}, r.fb.nb.history["nb1"]...)
	r.click(t, "View changes")
	r.frames(2)
	d := r.w.nbDlg
	if d == nil || d.kind != "history" || d.selected != "bv1" {
		t.Fatalf("History did not select the version before the agent's edit: %+v", d)
	}
	if !r.tt.HasText("History · Deploy") || !r.tt.HasText("Before the agent's edit") {
		t.Fatalf("%q", r.tt.Texts())
	}
	if slices.ContainsFunc(r.fb.nb.reqs, func(s string) bool { return strings.HasPrefix(s, "reviewed") }) {
		t.Fatal("looking at the changes marked the notebook reviewed")
	}
}

func TestUndoOnTheBannerRestoresTheVersionKeptBeforeTheAgentsEditAndMarksNothingReviewed(t *testing.T) {
	r, p, old := editedRig(t)
	r.click(t, "Undo agent edit")
	r.frames(3)
	if n := r.fb.nb.reqsWith("restore nb1 bv1 rev=2"); n != 1 {
		t.Fatalf("requests %v", r.fb.nb.reqs)
	}
	if p.nb.md != old || !r.tt.HasText("Undone. The agent's version is in History.") {
		t.Fatalf("the pane shows %q", p.nb.md)
	}
	if slices.ContainsFunc(r.fb.nb.reqs, func(s string) bool { return strings.HasPrefix(s, "reviewed") }) {
		t.Fatal("Undo marked the notebook reviewed")
	}
}

func TestUndoAsksFirstWhenTheUserChangedTheNotebookAfterTheAgentAndSaysSoWhenThereIsNothingToUndo(t *testing.T) {
	r, _, _ := editedRig(t)
	r.fb.nb.list[0].LastBy = "" // the user saved after the agent's edit
	r.w.reloadNotebooks()
	r.frames(2)
	r.click(t, "Undo agent edit")
	if !r.tt.HasText("You changed this notebook after the agent's edit. Undo puts back the version from before the agent's edit; your later changes stay in History.") || r.fb.nb.reqsWith("restore") != 0 {
		t.Fatalf("it did not ask: %q", r.tt.Texts())
	}
	r.click(t, "Cancel")
	if r.fb.nb.reqsWith("restore") != 0 {
		t.Fatal("Cancel restored")
	}
	r.click(t, "Undo agent edit")
	dlg, ok := r.tt.Find("alertdialog Undo the agent's edit")
	if !ok {
		t.Fatalf("no confirm: %q", r.tt.Texts())
	}
	r.tt.ClickAt(dlg.X+dlg.W-space4-20, dlg.Y+dlg.H-space4-sizeControlSm/2) // the confirm's Undo: the banner's button has the same word under the scrim
	r.frames(2)
	if r.fb.nb.reqsWith("restore nb1 bv1") != 1 {
		t.Fatalf("requests %v", r.fb.nb.reqs)
	}

	r2, _, _ := editedRig(t)
	r2.fb.nb.history["nb1"] = nil
	r2.click(t, "Undo agent edit")
	if !r2.tt.HasText("Keel has no version from before the agent's edit. Open History to choose one.") {
		t.Fatalf("%q", r2.tt.Texts())
	}
}
