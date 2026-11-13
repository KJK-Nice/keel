package keelclient

import (
	"context"
	"strings"
	"testing"
)

// Notes and runbooks: method, path and body of each call. The bodies hold only obviously fake text.

func TestNoteCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()

	r.reply = `{"notes":[{"id":"n1","hostId":"h1","text":"backups at 02:00","source":"agent","fromPane":true,"seenIn":"db-2","edited":true,"createdAt":"2026-10-12T09:00:00Z"},{"id":"n2","text":"mine","source":"user"}],"unreadable":false}`
	l, err := r.c.Notes(ctx)
	if got, _ := r.last(); got != "GET /api/agent/notes" || err != nil || len(l.Notes) != 2 || !l.Notes[0].FromPane || l.Notes[0].SeenIn != "db-2" || !l.Notes[0].Edited || l.Notes[1].HostID != "" {
		t.Fatal(got, l, err)
	}

	r.reply = `{"note":{"id":"n3","hostId":"h1","text":"[secret:1] kept","source":"user"},"redacted":2}`
	s, err := r.c.SaveNote(ctx, NoteInput{HostID: "h1", Text: "fake-note"})
	if got, body := r.last(); got != "POST /api/agent/notes" || body != `{"groupId":"","hostId":"h1","text":"fake-note"}` || err != nil || s.Redacted != 2 || s.Note.Text != "[secret:1] kept" {
		t.Fatal(got, body, s, err)
	}
	if _, err := r.c.SaveNote(ctx, NoteInput{Text: "mine"}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/agent/notes" || body != `{"groupId":"","hostId":"","text":"mine"}` {
		t.Fatal(got, body)
	}
	if _, err := r.c.SaveNote(ctx, NoteInput{ID: "n 3", HostID: "ignored", Text: "edited"}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/agent/notes/n 3" && got != "PUT /api/agent/notes/n%203" || body != `{"text":"edited"}` {
		t.Fatal(got, body)
	}

	r.reply = ``
	if err := r.c.DeleteNote(ctx, "n1"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "DELETE /api/agent/notes/n1" || body != "" {
		t.Fatal(got, body)
	}
	r.reply = `{"deleted":3}`
	n, err := r.c.DeleteAllNotes(ctx)
	if got, body := r.last(); got != "DELETE /api/agent/notes" || body != "" || err != nil || n != 3 {
		t.Fatal(got, body, n, err)
	}

	r.reply = `{"note":{"id":"n9","text":"x","source":"agent"},"redacted":1,"proposal":{"id":"np1","for":"host","host":"web-1","status":"saved","noteId":"n9"}}`
	sp, err := r.c.SaveProposedNote(ctx, "c1", "np1", "my edit")
	if got, body := r.last(); got != "POST /api/agent/conversations/c1/notes/np1" || body != `{"text":"my edit"}` || err != nil || sp.Proposal == nil || sp.Proposal.Status != "saved" || sp.Redacted != 1 {
		t.Fatal(got, body, sp, err)
	}
	r.reply = `{"id":"np1","for":"host","status":"open"}`
	u, err := r.c.UndoProposedNote(ctx, "c1", "np1")
	if got, body := r.last(); got != "POST /api/agent/conversations/c1/notes/np1/undo" || body != "" || err != nil || u.Status != "open" {
		t.Fatal(got, body, u, err)
	}
	r.reply = `{"id":"np1","for":"host","status":"dismissed"}`
	d, err := r.c.DismissProposedNote(ctx, "c1", "np1")
	if got, body := r.last(); got != "DELETE /api/agent/conversations/c1/notes/np1" || body != "" || err != nil || d.Status != "dismissed" {
		t.Fatal(got, body, d, err)
	}
}

func TestRunbookCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()

	r.reply = `{"runbooks":[{"id":"rb1","name":"Disk","when":"disk full","hosts":["ci"],"agentMayPick":false,"source":"agent","fromPane":true,"steps":[{"id":"st1","title":"See","command":"df -h","risk":"readonly","role":"web","captures":[{"name":"a","pattern":"b"}]}],"lastUsedOn":"web-1"}],"unreadable":true}`
	l, err := r.c.Runbooks(ctx)
	if got, _ := r.last(); got != "GET /api/agent/runbooks" || err != nil || !l.Unreadable || len(l.Runbooks) != 1 || l.Runbooks[0].Steps[0].Risk != "readonly" || l.Runbooks[0].Steps[0].Captures[0].Name != "a" || l.Runbooks[0].AgentMayPick {
		t.Fatal(got, l, err)
	}

	in := RunbookInput{Name: "N", When: "w", AgentMayPick: false, Steps: []RunbookStepInput{{ID: "st1", Title: "T", Command: "echo fake", Role: "web", Captures: []StepCapture{{Name: "a", Pattern: "b"}}}}, DraftID: "dr_1"}
	r.reply = `{"runbook":{"id":"rb2","name":"N","steps":[]},"redacted":1}`
	s, err := r.c.SaveRunbook(ctx, "", in)
	want := `{"name":"N","when":"w","hosts":[],"agentMayPick":false,"steps":[{"id":"st1","title":"T","description":"","command":"echo fake","expect":"","role":"web","oneHost":false,"captures":[{"name":"a","pattern":"b"}]}],"draftId":"dr_1"}`
	if got, body := r.last(); got != "POST /api/agent/runbooks" || body != want || err != nil || s.Redacted != 1 || s.Runbook.ID != "rb2" {
		t.Fatal(got, body, s, err)
	}
	if _, err := r.c.SaveRunbook(ctx, "rb2", RunbookInput{Name: "N"}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/agent/runbooks/rb2" || !strings.Contains(body, `"hosts":[]`) || !strings.Contains(body, `"steps":[]`) {
		t.Fatal(got, body)
	}

	r.reply = ``
	if err := r.c.DeleteRunbook(ctx, "rb2"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "DELETE /api/agent/runbooks/rb2" || body != "" {
		t.Fatal(got, body)
	}
	r.reply = `{"id":"rb2","name":"N","agentMayPick":true,"steps":[]}`
	rb, err := r.c.SetRunbookPick(ctx, "rb2", true)
	if got, body := r.last(); got != "PUT /api/agent/runbooks/rb2/pick" || body != `{"agentMayPick":true}` || err != nil || !rb.AgentMayPick {
		t.Fatal(got, body, rb, err)
	}
	r.reply = `{"name":"From chat","when":"","hosts":[],"steps":[{"title":"One","command":"echo fake"}],"source":"conversation","sourceRef":"Disk","draftId":"dr_9","fromPrivate":false}`
	dr, err := r.c.DraftRunbook(ctx, "c1")
	if got, body := r.last(); got != "POST /api/agent/runbooks/draft" || body != `{"conversationId":"c1"}` || err != nil || dr.DraftID != "dr_9" || dr.Source != "conversation" || dr.SourceRef != "Disk" {
		t.Fatal(got, body, dr, err)
	}
}

func TestRunbookInUseCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()

	r.reply = `{"runbook":{"id":"ru1","runbookId":"rb1","name":"Disk","pickedBy":"agent","why":"fits","status":"active","turn":1,"pinHost":"web-1","blocked":"","steps":[{"id":"st1","title":"See","command":"df -h","state":"typed","proposalId":"p1","exit":0,"result":"reported"},{"id":"st2","title":"Look","state":"next"}]},"noRunbook":false}`
	st, err := r.c.RunbookState(ctx, "c1")
	if got, body := r.last(); got != "GET /api/agent/conversations/c1/runbook" || body != "" || err != nil || st.Runbook == nil || st.Runbook.Steps[0].Exit == nil || *st.Runbook.Steps[0].Exit != 0 ||
		st.Runbook.Steps[0].ProposalRef != "p1" || st.Runbook.PinHost != "web-1" {
		t.Fatal(got, body, st, err)
	}
	for _, action := range []string{"done", "skip", "retry"} {
		if _, err := r.c.StepAction(ctx, "c1", "st1", action); err != nil {
			t.Fatal(err)
		}
		if got, body := r.last(); got != "POST /api/agent/conversations/c1/runbook/steps/st1" || body != `{"action":"`+action+`"}` {
			t.Fatal(got, body)
		}
	}
	if _, err := r.c.StopRunbook(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "DELETE /api/agent/conversations/c1/runbook" || body != "" {
		t.Fatal(got, body)
	}
	if _, err := r.c.MoveRunbook(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/agent/conversations/c1/runbook/move" || body != "" {
		t.Fatal(got, body)
	}

	// Starting one is an ask with its id.
	r.reply = `{"conversationId":"c1","answer":"ok","proposals":[{"id":"p2","command":"df -h","bytes":"\u0015df -h\n","runbook":{"runbookId":"rb1","name":"Disk","step":1,"of":2}}],"runbook":{"id":"ru1","status":"active","steps":[]},"notes":[{"id":"np1","for":"user","text":"x","status":"open","panesRead":["web-1"]}],"runbooks":[{"id":"bp1","name":"B","status":"open","steps":[{"title":"s","command":"echo fake"}]}]}`
	a, err := r.c.Ask(ctx, AskRequest{ConversationID: "c1", Question: "go", RunbookID: "rb1"})
	if got, body := r.last(); got != "POST /api/agent/ask" || !strings.Contains(body, `"runbookId":"rb1"`) || err != nil ||
		a.Proposals[0].Runbook == nil || a.Proposals[0].Runbook.Step != 1 || a.Runbook == nil || len(a.Notes) != 1 || a.Notes[0].PanesRead[0] != "web-1" || len(a.Runbooks) != 1 {
		t.Fatal(got, body, a, err)
	}
	if _, err := r.c.Ask(ctx, AskRequest{ConversationID: "c1", Question: "go"}); err != nil {
		t.Fatal(err)
	}
	if _, body := r.last(); strings.Contains(body, "runbookId") {
		t.Fatalf("an ask that starts no runbook sends no runbookId: %s", body)
	}
}

func TestRunbookProposalCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()

	r.reply = `{"runbook":{"id":"rb7","name":"B","agentMayPick":false,"source":"agent","steps":[]},"redacted":0,"proposal":{"id":"bp1","name":"B","status":"saved","runbookId":"rb7","steps":[]}}`
	s, err := r.c.SaveProposedRunbook(ctx, "c1", "bp1")
	if got, body := r.last(); got != "POST /api/agent/conversations/c1/runbooks/bp1" || body != "" || err != nil || s.Runbook.AgentMayPick || s.Proposal == nil || s.Proposal.RunbookID != "rb7" {
		t.Fatal(got, body, s, err)
	}
	r.reply = `{"id":"bp1","name":"B","status":"open","steps":[]}`
	u, err := r.c.UndoProposedRunbook(ctx, "c1", "bp1")
	if got, body := r.last(); got != "POST /api/agent/conversations/c1/runbooks/bp1/undo" || body != "" || err != nil || u.Status != "open" {
		t.Fatal(got, body, u, err)
	}
	r.reply = `{"id":"bp1","name":"B","status":"dismissed","steps":[]}`
	d, err := r.c.DismissProposedRunbook(ctx, "c1", "bp1")
	if got, body := r.last(); got != "DELETE /api/agent/conversations/c1/runbooks/bp1" || body != "" || err != nil || d.Status != "dismissed" {
		t.Fatal(got, body, d, err)
	}
}

func TestTranscriptCarriesNotesRunbooksAndTheRunInUse(t *testing.T) {
	r := newRec(t)
	r.reply = `{"id":"c1","title":"T","saved":true,"runbook":{"id":"ru1","name":"Disk","turn":2,"steps":[]},"noRunbook":true,"turns":[{"at":"2026-11-11T09:00:00Z","question":"q","trace":[],"proposals":[],"notes":[{"id":"np1","for":"host","host":"web-1","text":"t","status":"saved","noteId":"n1","proposedAt":"2026-11-11T09:00:00Z"}],"runbooks":[{"id":"bp1","name":"B","status":"dismissed","steps":[]}]}]}`
	tr, err := r.c.Conversation(context.Background(), "c1")
	if err != nil || tr.Runbook == nil || tr.Runbook.Turn != 2 || !tr.NoRunbook || len(tr.Turns[0].Notes) != 1 || tr.Turns[0].Notes[0].NoteID != "n1" || tr.Turns[0].Runbooks[0].Status != "dismissed" {
		t.Fatal(tr, err)
	}
}
