package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A type without the dialog (DirectType) against a fake /ws/session: the frames, their order, what each
// answer does, and everything that must send nothing. The page's twin is cmd/keel/web/js/pty.js typeDirect.

// answerWith replies to a frame with the given type, carrying its ref, and the fields given.
func answerWith(typ string, fields map[string]any) func(*websocket.Conn, map[string]any) {
	return func(ws *websocket.Conn, m map[string]any) {
		out := map[string]any{"type": typ, "ref": m["direct"].(map[string]any)["ref"]}
		for k, v := range fields {
			out[k] = v
		}
		b, _ := json.Marshal(out)
		ws.WriteMessage(websocket.TextMessage, b)
	}
}

func dialDirect(t *testing.T, onInject func(*websocket.Conn, map[string]any)) (*Conn, *injectServer) {
	t.Helper()
	c, s := dialInject(t, "ready", "driver", nonceAnswer)
	s.onInject = onInject
	c.wait = 400 * time.Millisecond
	return c, s
}

var snippetRef = DirectRef{Kind: "snippet", SnippetID: "sn-1", AltScreen: false, PromptLine: "user@h $"}

func TestDirectTypeSendsTheChallengeThenTheBareCommandWithTheNonceAndTheDirectPart(t *testing.T) {
	c, s := dialDirect(t, answerWith("type-direct", map[string]any{"ok": true}))
	res, err := c.DirectType(context.Background(), "df -h", snippetRef, nil)
	if err != nil || !res.Typed || res.Dialog || res.Message != "" {
		t.Fatalf("got %+v, %v", res, err)
	}
	waitFrames(s, 2)
	fr := s.all()
	if got := s.types(); len(got) != 2 || got[0] != "inject-challenge" || got[1] != "inject" {
		t.Fatalf("frames %v", got)
	}
	for _, m := range fr {
		if m["command"] != "df -h" {
			t.Fatalf("%v carries %q, not the bare command", m["type"], m["command"])
		}
	}
	in := fr[1]
	if in["nonce"] != "n-1" {
		t.Errorf("nonce %v: not the server's", in["nonce"])
	}
	d := in["direct"].(map[string]any)
	if d["kind"] != "snippet" || d["snippetId"] != "sn-1" || d["altScreen"] != false || d["promptLine"] != "user@h $" || d["ref"] == "" || d["ref"] == nil {
		t.Errorf("direct part %v", d)
	}
	if _, ok := in["agent"]; ok {
		t.Error("an agent part on a snippet")
	}
	if cmd := in["command"].(string); strings.ContainsAny(cmd, "\x00\x15\r\n\t\x1b\x7f") {
		t.Errorf("the command %q holds a control character", cmd)
	}
}

func TestDirectTypeFreshNonceEveryTime(t *testing.T) {
	nonces := 0
	c, s := dialInject(t, "ready", "driver", func(ws *websocket.Conn, _ map[string]any) {
		nonces++
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"inject-challenge","nonce":"n-`+string(rune('0'+nonces))+`"}`))
	})
	s.onInject = answerWith("type-direct", map[string]any{"ok": true})
	c.wait = 400 * time.Millisecond
	for i := 0; i < 2; i++ {
		if res, err := c.DirectType(context.Background(), "pwd", snippetRef, nil); err != nil || !res.Typed {
			t.Fatal(res, err)
		}
	}
	waitFrames(s, 4)
	var got []any
	for _, m := range s.all() {
		if m["type"] == "inject" {
			got = append(got, m["nonce"])
		}
	}
	if len(got) != 2 || got[0] != "n-1" || got[1] != "n-2" {
		t.Fatalf("nonces %v", got)
	}
}

func TestDirectTypeCarriesTheAgentReference(t *testing.T) {
	c, s := dialDirect(t, answerWith("type-direct", map[string]any{"ok": true}))
	if res, err := c.DirectType(context.Background(), "uptime", DirectRef{Kind: "agent"}, &AgentRef{ConversationID: "c1", ProposalID: "p1", Shift: true}); err != nil || !res.Typed {
		t.Fatal(res, err)
	}
	waitFrames(s, 2)
	in := s.all()[1]
	if a := in["agent"]; a == nil || a.(map[string]any)["conversationId"] != "c1" || a.(map[string]any)["proposalId"] != "p1" || a.(map[string]any)["shift"] != true {
		t.Fatalf("agent part %v", a)
	}
	if d := in["direct"].(map[string]any); d["kind"] != "agent" || d["snippetId"] != nil {
		t.Fatalf("direct part %v", d)
	}
}

func TestDirectTypeAnswers(t *testing.T) {
	for name, c := range map[string]struct {
		answer map[string]any
		want   DirectResult
	}{
		"dialog":              {map[string]any{"ok": false, "dialog": true, "reason": "the pane shows a full-screen program"}, DirectResult{Dialog: true, Reason: "the pane shows a full-screen program"}},
		"refusal":             {map[string]any{"ok": false, "message": "you follow this pane: take or request control to type"}, DirectResult{Message: "you follow this pane: take or request control to type"}},
		"refusal with no say": {map[string]any{"ok": false}, DirectResult{Message: "Not typed."}},
		"typed":               {map[string]any{"ok": true}, DirectResult{Typed: true}},
	} {
		conn, _ := dialDirect(t, answerWith("type-direct", c.answer))
		res, err := conn.DirectType(context.Background(), "df -h", snippetRef, nil)
		if err != nil || res != c.want {
			t.Errorf("%s: got %+v, %v; want %+v", name, res, err, c.want)
		}
	}
}

func TestDirectTypeNoAnswerIsNotADialogAndNotRetried(t *testing.T) {
	c, s := dialDirect(t, nil) // keel serve never answers the inject
	res, err := c.DirectType(context.Background(), "df -h", snippetRef, nil)
	if !errors.Is(err, ErrNoDirectAnswer) || res != (DirectResult{}) {
		t.Fatalf("got %+v, %v", res, err)
	}
	if err.Error() != "keel serve did not answer. Check the pane before you press Type again." {
		t.Fatalf("not the page's words: %q", err)
	}
	time.Sleep(60 * time.Millisecond)
	if got := s.types(); len(got) != 2 {
		t.Fatalf("frames %v: a second request went out", got)
	}
}

func TestDirectTypeAnswerForAnotherRefIsIgnored(t *testing.T) {
	c, _ := dialDirect(t, func(ws *websocket.Conn, _ map[string]any) {
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"type-direct","ref":"d999","ok":true}`))
	})
	if _, err := c.DirectType(context.Background(), "df -h", snippetRef, nil); !errors.Is(err, ErrNoDirectAnswer) {
		t.Fatalf("an answer for another request counted: %v", err)
	}
}

func TestDirectTypeWithNoNonceIsADialogAndNoInjectFrame(t *testing.T) {
	c, s := dialInject(t, "ready", "driver", nil) // keel serve says nothing to the challenge
	c.wait = 80 * time.Millisecond
	res, err := c.DirectType(context.Background(), "df -h", snippetRef, nil)
	if err != nil || !res.Dialog || res.Typed {
		t.Fatalf("got %+v, %v", res, err)
	}
	time.Sleep(60 * time.Millisecond)
	if got := s.types(); len(got) != 1 || got[0] != "inject-challenge" {
		t.Fatalf("frames %v", got)
	}
	// An empty nonce is the same.
	c2, s2 := dialInject(t, "ready", "driver", func(ws *websocket.Conn, _ map[string]any) {
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"inject-challenge","nonce":""}`))
	})
	if res, err := c2.DirectType(context.Background(), "df -h", snippetRef, nil); err != nil || !res.Dialog {
		t.Fatalf("empty nonce: %+v, %v", res, err)
	}
	time.Sleep(60 * time.Millisecond)
	if got := s2.types(); len(got) != 1 {
		t.Fatalf("frames %v", got)
	}
}

func TestDirectTypeFollowerNotReadyAndBadCommandSendNothing(t *testing.T) {
	anyErr := errors.New("any error")
	for _, c := range []struct {
		status, role, command string
		ref                   DirectRef
		want                  error
		dialog                bool
	}{
		{"ready", "follower", "df -h", snippetRef, ErrNotDriver, false},
		{"ready", "owner", "df -h", snippetRef, ErrNotDriver, false},
		{"connecting", "driver", "df -h", snippetRef, ErrNotReady, false},
		{"error", "driver", "df -h", snippetRef, ErrNotReady, false},
		{"ready", "driver", "df -h\n", snippetRef, nil, true},
		{"ready", "driver", "df -h\r", snippetRef, nil, true},
		{"ready", "driver", "\x15df -h", snippetRef, nil, true},
		{"ready", "driver", "df\x1b[A", snippetRef, nil, true},
		{"ready", "driver", "df\tx", snippetRef, nil, true},
		{"ready", "driver", "df\xe2\x80\x8b -h", snippetRef, nil, true},
		{"ready", "driver", "", snippetRef, nil, true},
		{"ready", "driver", "a\nb", snippetRef, nil, true},
		{"ready", "driver", "df -h", DirectRef{Kind: "group"}, anyErr, false}, // a kind not built here ("notebook" is now built)
	} {
		conn, s := dialInject(t, c.status, c.role, nonceAnswer)
		res, err := conn.DirectType(context.Background(), c.command, c.ref, nil)
		switch {
		case c.want == nil && err != nil, c.want != nil && err == nil, c.want != nil && c.want != anyErr && !errors.Is(err, c.want):
			t.Errorf("%s/%s %q: err %v, want %v", c.status, c.role, c.command, err, c.want)
		case c.dialog && !res.Dialog:
			t.Errorf("%s/%s %q: %+v, want a dialog", c.status, c.role, c.command, res)
		case !c.dialog && (res.Dialog || res.Typed):
			t.Errorf("%s/%s %q: %+v", c.status, c.role, c.command, res)
		}
		time.Sleep(40 * time.Millisecond)
		if got := s.types(); len(got) != 0 {
			t.Errorf("%s/%s %q sent %v", c.status, c.role, c.command, got)
		}
	}
	if ErrDirectBusy.Error() != "Still typing the last command into this pane." {
		t.Fatalf("not the page's words: %q", ErrDirectBusy)
	}
}

func TestDirectTypeOneAtATimeNeverQueues(t *testing.T) {
	release := make(chan struct{})
	c, s := dialInject(t, "ready", "driver", nil)
	c.wait = 2 * time.Second
	s.answer = func(ws *websocket.Conn, m map[string]any) {
		<-release
		nonceAnswer(ws, m)
	}
	s.onInject = answerWith("type-direct", map[string]any{"ok": true})
	first := make(chan error, 1)
	go func() {
		_, err := c.DirectType(context.Background(), "one", snippetRef, nil)
		first <- err
	}()
	waitFrames(s, 1)
	if _, err := c.DirectType(context.Background(), "two", snippetRef, nil); !errors.Is(err, ErrDirectBusy) {
		t.Fatalf("second direct type: %v", err)
	}
	if err := c.ApprovedInject(context.Background(), "three\n", nil); !errors.Is(err, ErrInjectBusy) {
		t.Fatalf("an approval while a direct type waits: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	for _, m := range s.all() {
		if m["command"] == "two" || m["command"] == "three\n" {
			t.Fatalf("a queued call reached keel serve: %v", m)
		}
	}
	if res, err := c.DirectType(context.Background(), "four", snippetRef, nil); err != nil || !res.Typed {
		t.Fatalf("the next one after the first is done: %+v, %v", res, err)
	}
}

func TestDirectTypeStopsWhenItsContextEnds(t *testing.T) {
	c, s := dialDirect(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.DirectType(ctx, "df -h", snippetRef, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, ty := range s.types() {
		if ty == "inject" {
			t.Fatal("an inject went out")
		}
	}
}
