package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// Notebooks and the agent: method, path and body of each call. The text is obviously fake.

func TestNotebookCardCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"proposal":{"id":"nbc_1","kind":"edit","status":"undone","notebookId":"nb1","title":"Ops","versionId":"nbv_1","rev":3},"notebook":{"id":"nb1","name":"Ops","rev":3,"cells":[{"id":"c","body":"old"}]}}`

	for _, c := range []struct {
		name string
		call func() (NotebookAction, error)
		want string
	}{
		{"undo", func() (NotebookAction, error) { return r.c.UndoNotebookChange(ctx, "c1", "nbc_1") }, "POST /api/agent/conversations/c1/notebooks/nbc_1/undo"},
		{"delete", func() (NotebookAction, error) { return r.c.ConfirmNotebookDelete(ctx, "c1", "nbc_1") }, "POST /api/agent/conversations/c1/notebooks/nbc_1/delete"},
		{"keep", func() (NotebookAction, error) { return r.c.DismissNotebookProposal(ctx, "c1", "nbc_1") }, "DELETE /api/agent/conversations/c1/notebooks/nbc_1"},
		{"restore", func() (NotebookAction, error) { return r.c.RestoreNotebookChange(ctx, "c1", "nbc_1") }, "POST /api/agent/conversations/c1/notebooks/nbc_1/restore"},
	} {
		out, err := c.call()
		if got, body := r.last(); got != c.want || body != "" || err != nil || out.Proposal.Status != "undone" || out.Proposal.VersionID != "nbv_1" || out.Notebook == nil || out.Notebook.Rev != 3 || out.Notebook.Markdown() != "old" {
			t.Fatal(c.name, got, body, out, err)
		}
	}

	r.reply = `{"proposal":{"id":"nbc_1","kind":"delete","status":"dismissed"}}`
	out, err := r.c.DismissNotebookProposal(ctx, "c1", "nbc_1")
	if err != nil || out.Notebook != nil || out.Proposal.Status != "dismissed" {
		t.Fatal(out, err)
	}
}

func TestACardRefusalKeepsKeelServesWords(t *testing.T) {
	r := newRec(t)
	r.status, r.reply = 410, `{"error":"This proposal has expired."}`
	_, err := r.c.UndoNotebookChange(context.Background(), "c1", "nbc_1")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 410 || se.Msg != "This proposal has expired." {
		t.Fatal(err)
	}
}

func TestAnAskSendsNotebookIdsNeverText(t *testing.T) {
	r := newRec(t)
	r.reply = `{"conversationId":"c1","trace":[{"status":"notebook","notebook":"Ops","notebookId":"nb1","bytes":42,"truncated":true,"redactions":{"secret":1}},{"status":"notebook","notebook":"Deploy","notebookId":"nb2","bytes":1,"asked":true},{"status":"notebook-index","count":3},{"status":"skipped","reason":"notebook-private"}],"notebooks":[{"id":"nbc_1","kind":"create","status":"created","notebookId":"nb3","title":"New","fromPane":true,"seenIn":"web-1","panesRead":["web-1"],"record":{"id":"nb3","name":"New","rev":1,"agentUnreviewed":true}}],"proposals":[],"notes":[],"runbooks":[]}`
	res, err := r.c.Ask(context.Background(), AskRequest{ConversationID: "c1", Question: "q", Notebooks: []string{"nb1"}, Editing: []string{"nb2"}})
	got, body := r.last()
	var sent map[string]any
	if json.Unmarshal([]byte(body), &sent) != nil || got != "POST /api/agent/ask" || err != nil {
		t.Fatal(got, body, err)
	}
	if nbs, _ := sent["notebooks"].([]any); len(nbs) != 1 || nbs[0] != "nb1" {
		t.Fatal(body)
	}
	if ed, _ := sent["editing"].([]any); len(ed) != 1 || ed[0] != "nb2" {
		t.Fatal(body)
	}
	for k := range sent {
		switch k {
		case "conversationId", "question", "panes", "notebooks", "editing":
		default:
			t.Fatal("unexpected key", k)
		}
	}
	s := res.Trace
	if len(s) != 4 || s[0].Notebook != "Ops" || s[0].NotebookID != "nb1" || s[0].Bytes != 42 || !s[0].Truncated || s[0].Redactions["secret"] != 1 || !s[1].Asked || s[2].Count != 3 || s[3].Reason != "notebook-private" {
		t.Fatal(s)
	}
	if len(res.Notebooks) != 1 || res.Notebooks[0].Record == nil || !res.Notebooks[0].Record.AgentUnreviewed || !res.Notebooks[0].FromPane || res.Notebooks[0].PanesRead[0] != "web-1" {
		t.Fatal(res.Notebooks)
	}

	// Nothing attached: neither key is sent.
	if _, err := r.c.Ask(context.Background(), AskRequest{ConversationID: "c1", Question: "q"}); err != nil {
		t.Fatal(err)
	}
	if _, body := r.last(); body != `{"conversationId":"c1","question":"q","panes":[]}` {
		t.Fatal(body)
	}
}

func TestASavedTurnHoldsItsNotebookCards(t *testing.T) {
	r := newRec(t)
	r.reply = `{"id":"c1","turns":[{"question":"q","notebooks":[{"id":"nbc_2","kind":"delete","status":"proposed","notebookId":"nb1","title":"Ops"}]}]}`
	tr, err := r.c.Conversation(context.Background(), "c1")
	if err != nil || len(tr.Turns[0].Notebooks) != 1 || tr.Turns[0].Notebooks[0].Kind != "delete" {
		t.Fatal(tr, err)
	}
}

func TestDraftRunbookFromANotebookSendsTheIdOnly(t *testing.T) {
	r := newRec(t)
	r.reply = `{"name":"Ops","steps":[{"title":"Look","command":"df -h"}],"source":"notebook","sourceRef":"Ops","draftId":"dr_1","fromPrivate":true}`
	d, err := r.c.DraftRunbookFromNotebook(context.Background(), "nb1")
	if got, body := r.last(); got != "POST /api/agent/runbooks/draft" || body != `{"notebookId":"nb1"}` || err != nil || d.Source != "notebook" || !d.FromPrivate || d.DraftID != "dr_1" {
		t.Fatal(got, body, d, err)
	}
	r.status, r.reply = 422, `{"error":"nothing to make steps from"}`
	_, err = r.c.DraftRunbookFromNotebook(context.Background(), "nb1")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 422 || se.Msg != "nothing to make steps from" {
		t.Fatal(err)
	}
}
