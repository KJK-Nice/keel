package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeKey is an obviously made-up key: no test uses, prints or needs a real one.
const fakeKey = "fake-key-for-tests-only-0000"

func TestAgentCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"settings":{"provider":"gemini","model":"m","defaultScope":"tab","scrollbackLines":150,"historyDays":7},"keys":{"gemini":{"configured":true}}}`
	s, err := r.c.AgentSettings(ctx)
	if got, _ := r.last(); got != "GET /api/agent/settings" || err != nil || s.Settings.DefaultScope != "tab" || s.Settings.ScrollbackLines != 150 || !s.KeyConfigured() {
		t.Fatal(got, s, err)
	}
	r.reply = `[{"id":"gemini","name":"Google Gemini","shortName":"Gemini","kind":"model","keyLabel":"Gemini API key","warning":"w","termsUrl":"u","keyHelpUrl":"k","configured":false}]`
	ps, err := r.c.AgentProviders(ctx)
	if got, _ := r.last(); got != "GET /api/agent/providers" || err != nil || len(ps) != 1 || ps[0].ShortName != "Gemini" || ps[0].Warning != "w" || ps[0].Configured {
		t.Fatal(got, ps, err)
	}
	r.reply = `{"provider":"gemini","configured":true}`
	if err := r.c.SetAgentKey(ctx, AgentKeyInput{Provider: "gemini", Key: fakeKey}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/agent/keys/gemini" || body != `{"key":"`+fakeKey+`"}` {
		t.Fatal(got, body)
	}
	r.reply = `{"id":"p1","sessionId":"s1","command":"echo edited-ok","bytes":"\u0015echo edited-ok\n","risk":"readonly","edited":true}`
	p, err := r.c.EditProposal(ctx, "c 1", "p/1", "echo edited-ok")
	if got, body := r.last(); got != "PUT /api/agent/conversations/c 1/proposals/p/1" && got != "PUT /api/agent/conversations/c 1/proposals/p1" || body != `{"command":"echo edited-ok"}` || err != nil || p.Bytes != "\x15echo edited-ok\n" || !p.Edited {
		t.Fatal(got, body, p, err)
	}
}

func TestAskSendsTheQuestionAndTheSessionIdsAndNothingElse(t *testing.T) {
	r := newRec(t)
	r.reply = `{"conversationId":"c1","title":"t","testProvider":true,"trace":[{"sessionId":"s1","host":"Local Shell","status":"read","lines":12,"redactions":{"secret":2}},{"host":"x","status":"skipped","reason":"not-readable"}],
	"answer":"I was given 1 pane excerpt(s)","proposals":[{"id":"p1","sessionId":"s1","host":"Local Shell","command":"echo keel-agent-ok","bytes":"\u0015echo keel-agent-ok\n","risk":"readonly","needsShift":false,"production":true,"placeholders":false}],"cut":"length","usage":{"inputTokens":1}}`
	res, err := r.c.Ask(context.Background(), AskRequest{ConversationID: "c1", Question: "what should I run?", Panes: []string{"s1", "s2"}})
	if err != nil {
		t.Fatal(err)
	}
	got, body := r.last()
	if got != "POST /api/agent/ask" || body != `{"conversationId":"c1","question":"what should I run?","panes":["s1","s2"]}` {
		t.Fatalf("%s %s", got, body)
	}
	var sent map[string]any
	json.Unmarshal([]byte(body), &sent)
	if len(sent) != 3 {
		t.Fatalf("the ask sends %v: only the id, the question and the sessions", sent)
	}
	if len(res.Trace) != 2 || res.Trace[0].Lines != 12 || res.Trace[0].Redactions["secret"] != 2 || res.Trace[1].Reason != "not-readable" ||
		res.Answer != "I was given 1 pane excerpt(s)" || !res.TestProvider || res.Cut != "length" || len(res.Proposals) != 1 {
		t.Fatalf("%+v", res)
	}
	if p := res.Proposals[0]; p.Bytes != "\x15echo keel-agent-ok\n" || p.Risk != "readonly" || !p.Production {
		t.Fatalf("%+v", p)
	}
	// No scope still sends a list, as the app does.
	r.c.Ask(context.Background(), AskRequest{ConversationID: "c1", Question: "q"})
	if _, body := r.last(); !strings.Contains(body, `"panes":[]`) {
		t.Fatal(body)
	}
}

func TestAskStopsWhenItsContextIsCancelled(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		close(started)
		select {
		case <-release:
		case <-q.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	c := NewAt(strings.TrimPrefix(srv.URL, "http://"), tok)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Ask(ctx, AskRequest{ConversationID: "c1", Question: "q"}); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ask did not return after its context was cancelled")
	}
}

func TestAskRefusalsKeepTheServersStatusAndWords(t *testing.T) {
	r := newRec(t)
	for _, c := range []struct {
		status int
		body   string
		msg    string
	}{{412, `{"error":"no model key","reason":"no-key"}`, "no model key"}, {423, `{"error":"vault locked"}`, "vault locked"}, {502, `{"error":"quota exceeded","reason":"provider"}`, "quota exceeded"}} {
		r.status, r.reply = c.status, c.body
		_, err := r.c.Ask(context.Background(), AskRequest{ConversationID: "c1", Question: "q"})
		var se *StatusError
		if !errors.As(err, &se) || se.Code != c.status || se.Msg != c.msg {
			t.Fatalf("%d: %v", c.status, err)
		}
		if strings.Contains(err.Error(), "q") && strings.Contains(err.Error(), "question") {
			t.Fatalf("the error echoes the request: %v", err)
		}
	}
}

func TestTheKeyIsInNoErrorAndNoFormattedValue(t *testing.T) {
	r := newRec(t)
	r.status, r.reply = 400, `{"error":"the key is empty, too long, or has spaces"}`
	in := AgentKeyInput{Provider: "gemini", Key: fakeKey}
	err := r.c.SetAgentKey(context.Background(), in)
	if err == nil || strings.Contains(err.Error(), fakeKey) {
		t.Fatalf("error %v", err)
	}
	var se *StatusError
	if !errors.As(err, &se) || strings.Contains(se.Msg, fakeKey) {
		t.Fatalf("status error %+v", se)
	}
	for _, shown := range []string{fmt.Sprintf("%v", in), fmt.Sprintf("%+v", in), fmt.Sprintf("%#v", in), in.String(), fmt.Sprintf("%v", &in), fmt.Sprintf("%+v", []AgentKeyInput{in})} {
		if strings.Contains(shown, fakeKey) {
			t.Fatalf("a formatted AgentKeyInput shows the key: %q", shown)
		}
	}
}

// ---- Type of a proposal through ApprovedInject ----

var ref = &AgentRef{ConversationID: "c1", ProposalID: "p1", Shift: true}

func typedAnswer(ok bool, msg string) func(*websocket.Conn, map[string]any) {
	return func(ws *websocket.Conn, _ map[string]any) {
		b, _ := json.Marshal(map[string]any{"type": "agent-type", "ok": ok, "proposalId": "p1", "message": msg})
		ws.WriteMessage(websocket.TextMessage, b)
	}
}

func waitFrames(s *injectServer, n int) {
	deadline := time.Now().Add(2 * time.Second)
	for len(s.types()) < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAgentTypeAddsTheReferenceToTheInjectFrameOnly(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	s.onInject = typedAnswer(true, "")
	bytes := "\x15echo keel-agent-ok\n"
	if err := c.ApprovedInject(context.Background(), bytes, ref); err != nil {
		t.Fatal(err)
	}
	waitFrames(s, 2)
	fr := s.all()
	if len(fr) != 2 || fr[0]["type"] != "inject-challenge" || fr[1]["type"] != "inject" {
		t.Fatalf("frames %v", fr)
	}
	if _, ok := fr[0]["agent"]; ok {
		t.Fatalf("the challenge carries the agent reference: %v", fr[0])
	}
	if fr[0]["command"] != bytes || fr[1]["command"] != bytes || fr[1]["nonce"] != "n-1" {
		t.Fatalf("the bytes or the nonce differ: %v", fr)
	}
	ag, _ := fr[1]["agent"].(map[string]any)
	if ag["conversationId"] != "c1" || ag["proposalId"] != "p1" || ag["shift"] != true || len(ag) != 3 {
		t.Fatalf("agent reference %v", fr[1]["agent"])
	}
}

func TestAnInjectWithoutAProposalHasNoAgentKey(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	if err := c.ApprovedInject(context.Background(), "\x15ls\n", nil); err != nil {
		t.Fatal(err)
	}
	waitFrames(s, 2)
	for _, m := range s.all() {
		if _, ok := m["agent"]; ok {
			t.Fatalf("an agent key without a proposal: %v", m)
		}
	}
}

func TestAnAgentTypeRefusalReturnsItsMessageAndNothingIsRetried(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	s.onInject = typedAnswer(false, "Destructive: hold ⇧")
	err := c.ApprovedInject(context.Background(), "\x15rm x\n", ref)
	if err == nil || err.Error() != "Destructive: hold ⇧" {
		t.Fatalf("got %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := s.types(); len(got) != 2 {
		t.Fatalf("frames %v: something was sent again", got)
	}
	// An empty message still refuses.
	s.onInject = typedAnswer(false, "")
	if err := c.ApprovedInject(context.Background(), "\x15rm x\n", ref); err == nil || err.Error() != "Not typed." {
		t.Fatalf("got %v", err)
	}
}

func TestAnAgentTypeThatKeelServeNeverAnswersIsNotTyped(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	s.onInject = func(*websocket.Conn, map[string]any) {} // keel serve says nothing
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.ApprovedInject(ctx, "\x15ls\n", ref); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if ErrNoAgentAnswer.Error() != "keel serve did not answer." {
		t.Fatal("not the webview's words")
	}
}

func TestAnAnswerAboutAnotherProposalIsNotOurs(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	s.onInject = func(ws *websocket.Conn, _ map[string]any) {
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"agent-type","ok":true,"proposalId":"other"}`))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := c.ApprovedInject(ctx, "\x15ls\n", ref); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an answer for another proposal settled this one: %v", err)
	}
}

func TestAnAgentTypeFrameIsConsumedAndNotShownAsOutput(t *testing.T) {
	// The frame is control: Read never returns it as PTY bytes, and a stray one with nobody waiting is dropped.
	c, _ := dialInject(t, "ready", "driver", nonceAnswer)
	if !c.injectFrame([]byte(`{"type":"agent-type","ok":true,"proposalId":"p1"}`)) {
		t.Fatal("agent-type is not routed")
	}
	if c.injectFrame([]byte(`{"type":"status"}`)) {
		t.Fatal("a status frame was taken")
	}
}
