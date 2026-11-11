package native

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"keel/internal/native/keelclient"
)

// The agent-and-notebooks half of fakeBackend: the card routes recorded, and answered as the test says. Nothing here reads
// a pane or types. What the agent "did" to a notebook is set up by the test in nbc.cards and in the notebooks' list; the
// fake only answers a card's route with the card in its next state and the notebook as the test left it.

type fakeNBCards struct {
	reqs     []string                                                  // "undo conv/card", "delete conv/card", "keep conv/card", "restore conv/card"
	answer   func(route, id string) (keelclient.NotebookAction, error) // when set: what a route answers
	draftReq []string                                                  // notebook ids a runbook draft was asked for
	draft    keelclient.RunbookDraft
	draftErr error
}

func (f *fakeBackend) nbCards() *fakeNBCards {
	if f.nbc == nil {
		f.nbc = &fakeNBCards{}
	}
	return f.nbc
}

func (f *fakeBackend) nbCardRoute(route, conv, id string) (keelclient.NotebookAction, error) {
	n := f.nbCards()
	n.reqs = append(n.reqs, route+" "+conv+"/"+id)
	f.rec("notebook card " + route)
	if n.answer != nil {
		return n.answer(route, id)
	}
	return keelclient.NotebookAction{}, &keelclient.StatusError{Code: 500, Msg: "no answer set"}
}

func (f *fakeBackend) UndoNotebookChange(_ context.Context, conv, id string) (keelclient.NotebookAction, error) {
	return f.nbCardRoute("undo", conv, id)
}

func (f *fakeBackend) ConfirmNotebookDelete(_ context.Context, conv, id string) (keelclient.NotebookAction, error) {
	return f.nbCardRoute("delete", conv, id)
}

func (f *fakeBackend) DismissNotebookProposal(_ context.Context, conv, id string) (keelclient.NotebookAction, error) {
	return f.nbCardRoute("keep", conv, id)
}

func (f *fakeBackend) RestoreNotebookChange(_ context.Context, conv, id string) (keelclient.NotebookAction, error) {
	return f.nbCardRoute("restore", conv, id)
}

func (f *fakeBackend) DraftRunbookOfNotebook(_ context.Context, id string) (keelclient.RunbookDraft, error) {
	n := f.nbCards()
	f.rec("draft runbook from notebook")
	n.draftReq = append(n.draftReq, id)
	f.mem.rbReqs = append(f.mem.rbReqs, "draft notebook "+id)
	return n.draft, n.draftErr
}

// routes lists the card routes asked of the fake, for a test to compare.
func (f *fakeBackend) cardRoutes() string { return strings.Join(f.nbCards().reqs, ",") }

// ---- what keel serve does for the agent, as a fake does it ----

// agentCreates is a notebook the agent made (not private): Source "agent", unreviewed.
func (f *fakeBackend) agentCreates(name, md string) keelclient.Notebook {
	nb := f.addNotebook(name, md)
	i := f.nbIndex(nb.ID)
	now := time.Now()
	f.nbf().list[i].Source, f.nbf().list[i].LastBy, f.nbf().list[i].AgentUnreviewed, f.nbf().list[i].AgentEditedAt = byAgent, byAgent, true, &now
	return f.nbf().list[i]
}

// agentEdits is an edit of the agent's: keel serve keeps the version before it ("before-agent-edit") and bumps the revision.
func (f *fakeBackend) agentEdits(id, md string) keelclient.Notebook {
	n := f.nbf()
	i := f.nbIndex(id)
	old := n.list[i]
	now := time.Now()
	n.history[id] = append([]keelclient.NotebookVersion{{ID: fmt.Sprintf("bv%d", len(n.history[id])+1), NotebookID: id, Name: old.Name, Cells: old.Cells, Rev: old.Rev,
		By: "user", Event: "before-agent-edit", Summary: "Adds a line", At: now, Bytes: len(old.Markdown())}}, n.history[id]...)
	n.list[i].Cells = []keelclient.NotebookCell{{ID: old.Cells[0].ID, Body: md}}
	n.list[i].Rev, n.list[i].LastBy, n.list[i].AgentUnreviewed, n.list[i].AgentEditedAt = old.Rev+1, byAgent, true, &now
	return n.list[i]
}

// agentUndo is a card's Undo of an edit: the version before it is back, and the notebook is the user's again.
func (f *fakeBackend) agentUndo(id string) keelclient.Notebook {
	n := f.nbf()
	i := f.nbIndex(id)
	for _, v := range n.history[id] {
		if v.Event == "before-agent-edit" {
			n.list[i].Cells, n.list[i].Rev, n.list[i].LastBy, n.list[i].AgentUnreviewed = v.Cells, n.list[i].Rev+1, "", false
			break
		}
	}
	return n.list[i]
}

// agentRemove takes a notebook to Recently deleted, as a delete or the Undo of a create does; it returns the version kept.
func (f *fakeBackend) agentRemove(id string) string {
	n := f.nbf()
	i := f.nbIndex(id)
	nb := n.list[i]
	n.list = append(n.list[:i], n.list[i+1:]...)
	vid := "t" + id
	n.trash = append([]keelclient.NotebookVersion{{ID: vid, NotebookID: id, Name: nb.Name, Cells: nb.Cells, Event: "deleted", KeptAt: time.Now()}}, n.trash...)
	return vid
}

// answersNbs makes the fake answer every ask with these notebook cards.
func (r *rig) answersNbs(cards ...keelclient.NotebookProposal) {
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{ConversationID: req.ConversationID, Answer: "ok", Notebooks: cards}, nil
	}
}

var errAgentDown = errors.New("keel serve is down")
