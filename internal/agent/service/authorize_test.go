package service

import (
	"context"
	"errors"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/provider"
)

// The one gate (authorize.go) runs its checks in one order on every path.
// Each table starts from a request that breaks every check and mends it one
// check at a time: the refusal is always the earliest check still broken.

type orderStep struct {
	name string
	err  error  // errors.Is target; nil with dialog "" means allowed
	want string // a DialogError's reason
}

func checkOrder(t *testing.T, path string, i int, st orderStep, err error) {
	t.Helper()
	switch {
	case st.want != "":
		if reason, ok := NeedsDialog(err); !ok || reason != st.want {
			t.Errorf("%s step %d (%s): err = %v, want the dialog for %q", path, i+1, st.name, err, st.want)
		}
	case st.err == nil:
		if err != nil {
			t.Errorf("%s step %d (%s): refused: %v", path, i+1, st.name, err)
		}
	case !errors.Is(err, st.err):
		t.Errorf("%s step %d (%s): err = %v, want %v", path, i+1, st.name, err, st.err)
	}
}

func TestAuthorizeOrderApprovedConversation(t *testing.T) {
	f := newFixture(t, "")
	ch := askOne(t, f, "systemctl restart postgresql", "s-db") // changes state, production
	held := askOne(t, f, "echo [secret:1]", "s-web")
	named := askOne(t, f, "echo {{token}}", "s-web")
	bare := func() Proposal { // no pane: Copy only
		f.fake.Reply = provider.Response{Text: "```keel-proposal\n{\"command\": \"uptime\"}\n```"}
		resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q"})
		if err != nil || len(resp.Proposals) != 1 {
			t.Fatalf("ask: %v %+v", err, resp.Proposals)
		}
		return resp.Proposals[0]
	}()
	req := func(p Proposal, edit func(*TypeRequest)) TypeRequest {
		r := TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: p.SessionID, Role: agent.RoleDriver, Bytes: p.Bytes}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	steps := []struct {
		orderStep
		req TypeRequest
	}{
		{orderStep{name: "role first", err: agent.ErrFollower}, req(ch, func(r *TypeRequest) {
			r.Role, r.Bytes, r.ProposalID, r.SessionID = agent.RoleFollower, "echo [secret:1]", "p_nope", "s-web"
		})},
		{orderStep{name: "a placeholder in the bytes", err: ErrPlaceholder}, req(ch, func(r *TypeRequest) { r.Bytes, r.ProposalID, r.SessionID = "echo [secret:1]", "p_nope", "s-web" })},
		{orderStep{name: "the proposal exists", err: ErrNotFound}, req(ch, func(r *TypeRequest) { r.Bytes, r.ProposalID, r.SessionID = "x", "p_nope", "s-web" })},
		{orderStep{name: "it has a pane", err: ErrNoTarget}, req(bare, func(r *TypeRequest) { r.Bytes, r.SessionID = "x", "s-web" })},
		{orderStep{name: "it is for this pane", err: ErrWrongPane}, req(ch, func(r *TypeRequest) { r.Bytes, r.SessionID = "x", "s-web" })},
		{orderStep{name: "its command holds a placeholder", err: ErrPlaceholder}, req(held, func(r *TypeRequest) { r.Bytes = "x" })},
		{orderStep{name: "its command names a variable", err: ErrVarsOutsideRun}, req(named, func(r *TypeRequest) { r.Bytes = "x" })},
		{orderStep{name: "the bytes are the ones shown", err: ErrChanged}, req(ch, func(r *TypeRequest) { r.Bytes = "x" })},
		{orderStep{name: "the ⇧ rule", err: ErrNeedsShift}, req(ch, nil)},
	}
	for i, st := range steps {
		checkOrder(t, "approved", i, st.orderStep, f.svc.AuthorizeType(st.req))
	}
	if n := len(f.logKinds(agent.EventType)); n != 0 {
		t.Fatalf("a refused type was logged (%d)", n)
	}
	if err := f.svc.AuthorizeType(req(ch, func(r *TypeRequest) { r.Shift = true })); err != nil {
		t.Fatalf("allowed: %v", err)
	}
	if n := len(f.logKinds(agent.EventType)); n != 1 {
		t.Fatalf("agent.type entries = %d, want 1 written before the write", n)
	}
}

func TestAuthorizeOrderDirectConversation(t *testing.T) {
	f := newSuggestFixture(t, "")
	ask := func(cmd, paneID string) Proposal {
		f.model.Reply = provider.Response{Text: "```keel-proposal\n{\"pane\": 1, \"command\": " + jsonString(cmd) + "}\n```"}
		resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{paneID}})
		if err != nil || len(resp.Proposals) != 1 {
			t.Fatalf("ask: %v %+v", err, resp.Proposals)
		}
		return resp.Proposals[0]
	}
	ro := ask("uptime", "s-web")
	ch := ask("touch /tmp/x", "s-web")
	held := ask("echo [secret:1]", "s-web")
	req := func(p Proposal, edit func(*TypeRequest)) TypeRequest {
		r := TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: p.SessionID, Role: agent.RoleDriver, Bytes: p.Command}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	steps := []struct {
		orderStep
		req TypeRequest
	}{
		{orderStep{name: "role first", err: agent.ErrFollower}, req(ro, func(r *TypeRequest) {
			r.Role, r.Bytes, r.ProposalID = agent.RoleFollower, "echo [secret:1]\nls", "p_nope"
		})},
		{orderStep{name: "a placeholder in the bytes", err: ErrPlaceholder}, req(ro, func(r *TypeRequest) { r.Bytes, r.ProposalID = "echo [secret:1]\nls", "p_nope" })},
		{orderStep{name: "one line", want: ReasonLines}, req(ro, func(r *TypeRequest) { r.Bytes, r.ProposalID = "pwd\nls", "p_nope" })},
		{orderStep{name: "the proposal exists", err: ErrNotFound}, req(ro, func(r *TypeRequest) { r.Bytes, r.ProposalID, r.SessionID = "pwd", "p_nope", "s-db" })},
		{orderStep{name: "it is for this pane", err: ErrWrongPane}, req(ro, func(r *TypeRequest) { r.Bytes, r.SessionID = "pwd", "s-db" })},
		{orderStep{name: "its command holds a placeholder", err: ErrPlaceholder}, req(held, func(r *TypeRequest) { r.Bytes = "pwd" })},
		{orderStep{name: "the bytes are the command", want: ReasonBytes}, req(ch, func(r *TypeRequest) { r.Bytes = "pwd" })},
		{orderStep{name: "plainly read-only", want: ReasonRisk}, req(ch, nil)},
	}
	for i, st := range steps {
		_, err := f.svc.AuthorizeDirect(context.Background(), st.req)
		checkOrder(t, "direct", i, st.orderStep, err)
	}
	// Jev is asked last, and only when everything else holds: an answer
	// that is not read-only sends the Type to the dialog.
	if n := len(f.judge.Requests()); n != 0 {
		t.Fatalf("Jev was asked %d times about refused types", n)
	}
	f.judge.Choices["risk_0"] = provider.ChoiceAnswer{Choice: "changes", Probabilities: map[string]float64{"changes": 0.9}}
	_, err := f.svc.AuthorizeDirect(context.Background(), req(ro, nil))
	if reason, ok := NeedsDialog(err); !ok || reason != ReasonJev {
		t.Fatalf("Jev says changes: %v", err)
	}
	f.judge.Choices["risk_0"] = provider.ChoiceAnswer{Choice: "readonly", Probabilities: map[string]float64{"readonly": 0.9}}
	g, err := f.svc.AuthorizeDirect(context.Background(), req(ro, nil))
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	if n := len(f.entries(agent.EventType)); n != 0 {
		t.Fatalf("agent.type logged before the write (%d)", n)
	}
	if err := f.svc.DirectTyped(g); err != nil {
		t.Fatal(err)
	}
	if e := f.entries(agent.EventType); len(e) != 1 || e[0].Source != agent.TypeDirect || e[0].Bytes != len("uptime") {
		t.Fatalf("agent.type = %+v", e)
	}
}

func TestAuthorizeOrderInsert(t *testing.T) {
	f := newSuggestFixture(t, block("rm -rf /tmp/build", "Deletes it.", "destructive")+block("echo [secret:1] {{name}}", "Prints.", "readonly"))
	s := f.suggest(t, "s-web", "delete the build folder")
	req := func(edit func(*InsertRequest)) InsertRequest {
		r := InsertRequest{SuggestionID: s.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: s.Options[0].Bytes}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	steps := []struct {
		orderStep
		req InsertRequest
	}{
		{orderStep{name: "role first", err: agent.ErrFollower}, req(func(r *InsertRequest) {
			r.Role, r.SuggestionID, r.SessionID, r.Bytes = agent.RoleFollower, "s_nope", "s-db", "x"
		})},
		{orderStep{name: "the suggestion exists", err: ErrExpired}, req(func(r *InsertRequest) { r.SuggestionID, r.SessionID, r.Bytes = "s_nope", "s-db", "x" })},
		{orderStep{name: "the option exists", err: ErrNotFound}, req(func(r *InsertRequest) { r.Option, r.SessionID, r.Bytes = 9, "s-db", "x" })},
		{orderStep{name: "it is for this pane", err: ErrWrongPane}, req(func(r *InsertRequest) { r.SessionID, r.Bytes = "s-db", "x" })},
		{orderStep{name: "the bytes are the option's", err: ErrChanged}, req(func(r *InsertRequest) { r.Bytes = "x" })},
		{orderStep{name: "the ⇧ rule", err: ErrNeedsShiftInsert}, req(nil)},
	}
	for i, st := range steps {
		_, err := f.svc.AuthorizeInsert(st.req)
		checkOrder(t, "insert", i, st.orderStep, err)
	}
	// An insert lands on the prompt line to be edited (§5.5): a placeholder
	// or a variable name is flagged on the card, not refused.
	g, err := f.svc.AuthorizeInsert(req(func(r *InsertRequest) { r.Option, r.Bytes = 1, s.Options[1].Bytes }))
	if err != nil {
		t.Fatalf("an option holding a placeholder: %v", err)
	}
	if n := len(f.entries(agent.EventInsert)); n != 0 {
		t.Fatalf("agent.insert logged before the write (%d)", n)
	}
	if _, err := f.svc.AuthorizeInsert(req(func(r *InsertRequest) { r.Shift = true })); !errors.Is(err, ErrUsed) {
		t.Fatalf("a second insert while one is reserved: %v", err)
	}
	if err := f.svc.InsertDone(g, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(f.entries(agent.EventInsert)); n != 1 {
		t.Fatalf("agent.insert entries = %d", n)
	}
}

func TestAuthorizeOrderGroupRun(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeParallel})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	run = g.skipCP(t, run)
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	c := cellOf(t, run, 1, "wk1") // a changes-state step
	req := func(edit func(*TypeRequest)) TypeRequest {
		r := TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: c.Proposal.Bytes}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	logged := len(g.logKinds(agent.EventType))
	// From here the host is production and the agent may not read it.
	g.st.setProduction("wk1", true)
	g.st.setReadable("wk1", false)
	steps := []struct {
		orderStep
		req  TypeRequest
		then func()
	}{
		{orderStep{name: "role first", err: agent.ErrFollower}, req(func(r *TypeRequest) {
			r.Role, r.ProposalID, r.SessionID, r.Bytes = agent.RoleFollower, "p_nope", "s-wk2", "x"
		}), nil},
		{orderStep{name: "the proposal exists", err: ErrNotFound}, req(func(r *TypeRequest) { r.ProposalID, r.SessionID, r.Bytes = "p_nope", "s-wk2", "x" }), nil},
		{orderStep{name: "it is for this pane", err: ErrWrongPane}, req(func(r *TypeRequest) { r.SessionID, r.Bytes = "s-wk2", "x" }), nil},
		{orderStep{name: "the bytes are the ones shown", err: ErrChanged}, req(func(r *TypeRequest) { r.Bytes = "x" }), nil},
		{orderStep{name: "the host may still be typed into", err: ErrNotTarget}, req(nil), func() { g.st.setReadable("wk1", true) }},
		{orderStep{name: "the ⇧ rule, production now", err: ErrNeedsShift}, req(nil), nil},
	}
	for i, st := range steps {
		checkOrder(t, "group", i, st.orderStep, g.svc.AuthorizeType(st.req))
		if st.then != nil {
			st.then()
		}
	}
	// Without the dialog: the bytes are the command alone, and a step that
	// changes state goes to the dialog.
	direct := func(edit func(*TypeRequest)) error {
		r := req(func(r *TypeRequest) { r.Bytes = c.Proposal.Command })
		if edit != nil {
			edit(&r)
		}
		_, err := g.svc.AuthorizeDirect(context.Background(), r)
		return err
	}
	checkOrder(t, "group direct", 0, orderStep{name: "role first", err: agent.ErrFollower}, direct(func(r *TypeRequest) { r.Role, r.Bytes = agent.RoleFollower, "a\nb" }))
	checkOrder(t, "group direct", 1, orderStep{name: "one line", want: ReasonLines}, direct(func(r *TypeRequest) { r.Bytes, r.SessionID = "a\nb", "s-wk2" }))
	checkOrder(t, "group direct", 2, orderStep{name: "it is for this pane", err: ErrWrongPane}, direct(func(r *TypeRequest) { r.Bytes, r.SessionID = "pwd", "s-wk2" }))
	checkOrder(t, "group direct", 3, orderStep{name: "the bytes are the command", want: ReasonBytes}, direct(func(r *TypeRequest) { r.Bytes = "pwd" }))
	checkOrder(t, "group direct", 4, orderStep{name: "plainly read-only", want: ReasonRisk}, direct(nil))
	if n := len(g.logKinds(agent.EventType)); n != logged {
		t.Fatalf("a refused type was logged (%d, was %d)", n, logged)
	}
	// Claimed once: the approved type goes through, a second is refused.
	if err := g.svc.AuthorizeType(req(func(r *TypeRequest) { r.Shift = true })); err != nil {
		t.Fatalf("allowed: %v", err)
	}
	if err := g.svc.AuthorizeType(req(func(r *TypeRequest) { r.Shift = true })); !errors.Is(err, ErrNotProposed) {
		t.Fatalf("typed twice: %v", err)
	}
}
