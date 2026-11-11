package keelclient

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A group run's Type: each ticked host is its own ApprovedInject on its own connection, with the run and its own proposal in
// the agent part of the inject frame (no conversation), its own one-time nonce for exactly its own bytes, and keel serve's
// answer for that proposal alone.

func TestAGroupRunsInjectNamesTheRunAndTheProposalAndNoConversation(t *testing.T) {
	var onInject = func(ws *websocket.Conn, m map[string]any) {
		a := m["agent"].(map[string]any)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"agent-type","ok":true,"proposalId":%q}`, a["proposalId"])))
	}
	bytes := "\x15echo port-8080\n" // the fill's bytes: what keel serve holds for this host, values in
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	s.onInject = onInject
	if err := c.ApprovedInject(context.Background(), bytes, &AgentRef{GroupRunID: "gr1", ProposalID: "p7", Shift: true}); err != nil {
		t.Fatal(err)
	}
	frames := s.all()
	if len(frames) != 2 || frames[0]["type"] != "inject-challenge" || frames[1]["type"] != "inject" {
		t.Fatalf("frames %v", s.types())
	}
	for _, m := range frames {
		if m["command"] != bytes {
			t.Fatalf("%v carries %q, not the approved bytes", m["type"], m["command"])
		}
	}
	a, _ := frames[1]["agent"].(map[string]any)
	if a["groupRunId"] != "gr1" || a["proposalId"] != "p7" || a["shift"] != true {
		t.Fatalf("agent part %v", a)
	}
	if _, ok := a["conversationId"]; ok {
		t.Fatalf("a group run's inject names a conversation: %v", a)
	}
	if frames[1]["nonce"] != "n-1" {
		t.Fatalf("nonce %v", frames[1]["nonce"])
	}
	// The challenge carries the bytes alone: no agent part, so keel serve's nonce is for exactly these bytes.
	if _, ok := frames[0]["agent"]; ok {
		t.Fatalf("the challenge names a proposal: %v", frames[0])
	}
}

func TestEachHostOfAGroupGetsItsOwnNonceOnItsOwnConnectionAndOneRefusalDoesNotStopTheOthers(t *testing.T) {
	type host struct {
		c *Conn
		s *injectServer
	}
	var hosts []host
	for i := 1; i <= 3; i++ {
		i := i
		c, s := dialInject(t, "ready", "driver", func(ws *websocket.Conn, _ map[string]any) {
			ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"inject-challenge","nonce":"n-%d"}`, i)))
		})
		s.onInject = func(ws *websocket.Conn, m map[string]any) {
			a := m["agent"].(map[string]any)
			if i == 2 { // keel serve refuses this host's proposal
				ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"agent-type","ok":false,"proposalId":%q,"message":"wk-2 left the group"}`, a["proposalId"])))
				return
			}
			ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"agent-type","ok":true,"proposalId":%q}`, a["proposalId"])))
		}
		hosts = append(hosts, host{c, s})
	}
	var errs []error
	for i, h := range hosts { // the dialog's loop: one call per ticked host, in order, none retried
		errs = append(errs, h.c.ApprovedInject(context.Background(), "\x15echo grp\n", &AgentRef{GroupRunID: "gr1", ProposalID: fmt.Sprintf("p%d", i+1)}))
	}
	if errs[0] != nil || errs[2] != nil || errs[1] == nil || errs[1].Error() != "wk-2 left the group" {
		t.Fatalf("answers %v", errs)
	}
	nonces := map[any]bool{}
	for i, h := range hosts {
		fr := h.s.all()
		if len(fr) != 2 {
			t.Fatalf("host %d got %v: each is asked once", i+1, h.s.types())
		}
		nonces[fr[1]["nonce"]] = true
		if fr[1]["agent"].(map[string]any)["proposalId"] != fmt.Sprintf("p%d", i+1) {
			t.Errorf("host %d typed another host's proposal: %v", i+1, fr[1]["agent"])
		}
	}
	if len(nonces) != 3 {
		t.Errorf("hosts shared a nonce: %v", nonces)
	}
}

func TestAFollowerOrAnUnreadyGroupSessionSendsNoFrameAtAll(t *testing.T) {
	for _, c := range []struct{ status, role string }{{"ready", "follower"}, {"connecting", "driver"}} {
		conn, s := dialInject(t, c.status, c.role, nonceAnswer)
		err := conn.ApprovedInject(context.Background(), "\x15echo x\n", &AgentRef{GroupRunID: "gr1", ProposalID: "p1"})
		if err == nil {
			t.Fatalf("%v typed", c)
		}
		time.Sleep(50 * time.Millisecond)
		if got := s.types(); len(got) != 0 {
			t.Fatalf("%v sent %v", c, got)
		}
	}
}

func TestGroupRunRequestsAreTheAppsAndNoneHoldsAValueInItsError(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"id":"gr1","name":"Print","status":"active","current":0,"steps":[{"id":"st1","title":"t","command":"echo {{port}}","vars":[{"name":"port"}],"cells":[{"hostId":"h1","host":"wk-1","state":"proposed","proposal":{"id":"p1","sessionId":"s1","bytes":"\u0015echo {{port}}\n"}}]}]}`
	run, err := r.c.StartGroupRun(ctx, "rb1", "g1", "rolling")
	if err != nil || run.ID != "gr1" || run.Steps[0].Cells[0].Proposal.Bytes != "\x15echo {{port}}\n" || run.Steps[0].Vars[0].Name != "port" {
		t.Fatalf("StartGroupRun: %v %+v", err, run)
	}
	if got, body := r.last(); got != "POST /api/agent/groupruns" || body != `{"groupId":"g1","mode":"rolling","runbookId":"rb1"}` {
		t.Fatalf("start: %s %s", got, body)
	}
	for _, c := range []struct {
		call       func() error
		method     string
		body, want string
	}{
		{func() error { _, e := r.c.GroupRun(ctx, "gr1"); return e }, "GET /api/agent/groupruns/gr1", "", ""},
		{func() error { _, e := r.c.StopGroupRun(ctx, "gr1"); return e }, "DELETE /api/agent/groupruns/gr1", "", ""},
		{func() error { _, e := r.c.SetGroupRunMode(ctx, "gr1", "parallel"); return e }, "PUT /api/agent/groupruns/gr1/mode", `{"mode":"parallel"}`, ""},
		{func() error { _, e := r.c.ProposeGroupStep(ctx, "gr1", []string{"s1", "s2"}); return e }, "POST /api/agent/groupruns/gr1/propose", `{"panes":["s1","s2"]}`, ""},
		{func() error { _, e := r.c.ProposeGroupStep(ctx, "gr1", nil); return e }, "POST /api/agent/groupruns/gr1/propose", `{"panes":[]}`, ""},
		{func() error { _, e := r.c.MarkGroupStep(ctx, "gr1", "st 1", "h1", "retry"); return e }, "POST /api/agent/groupruns/gr1/steps/st 1", `{"action":"retry","hostId":"h1"}`, ""},
	} {
		if err := c.call(); err != nil {
			t.Fatal(err)
		}
		if got, body := r.last(); got != c.method || body != c.body && c.body != "" {
			t.Errorf("%s %s, want %s %s", got, body, c.method, c.body)
		}
	}
	r.reply = `{"runs":[{"id":"gr1","name":"Print"}]}`
	runs, err := r.c.GroupRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].ID != "gr1" {
		t.Fatalf("GroupRuns: %v %+v", err, runs)
	}
}

func TestTheFillIsTheOnlyPlaceAValueArrivesAndItIsNeverPrinted(t *testing.T) {
	r := newRec(t)
	r.reply = `{"fills":[{"proposalId":"p1","hostId":"h1","host":"wk-1","bytes":"\u0015echo port-8080\n","used":[{"name":"port","from":"group variable"}],"risk":"readonly"},{"proposalId":"p2","host":"wk-2","error":"{{port}} has no value for wk-2"}]}`
	fills, err := r.c.FillGroupStep(context.Background(), "gr1", []string{"p1", "p2"})
	if err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/agent/groupruns/gr1/fill" || body != `{"proposalIds":["p1","p2"]}` {
		t.Fatalf("fill: %s %s", got, body)
	}
	if fills[0].ProposalRef != "p1" || fills[0].Bytes != "\x15echo port-8080\n" || fills[0].Used[0].From != "group variable" || fills[1].Error == "" {
		t.Fatalf("fills %+v", fills)
	}
	for _, s := range []string{fmt.Sprint(fills[0]), fmt.Sprintf("%+v", fills[0]), fmt.Sprintf("%#v", fills[0]), fmt.Sprintf("%v", fills)} {
		if strings.Contains(s, "8080") {
			t.Errorf("a fill printed its bytes: %s", s)
		}
	}
	// A refused fill's error names the method, path and status only.
	r.status, r.reply = 409, `{"error":"the run ended"}`
	_, err = r.c.FillGroupStep(context.Background(), "gr1", []string{"p1"})
	if err == nil || strings.Contains(err.Error(), "p1") {
		t.Fatalf("error %v", err)
	}
}
