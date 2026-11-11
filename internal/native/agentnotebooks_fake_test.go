package native

import (
	"context"
	"strings"

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
