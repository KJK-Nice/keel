package keelclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The suggestion frames (inject.go): each one's shape is the app's (pty.js / term.js, internal/httpapi/agentsuggest.go).

const suggestionJSON = `{"type":"agent-suggest","seq":2,"ok":true,"suggestion":{"id":"s_1","options":[` +
	`{"command":"echo a","bytes":"\u0015echo a","risk":"readonly","needsShift":false,"source":"generated","why":"w"},` +
	`{"command":"rm -rf /tmp/x","bytes":"\u0015rm -rf /tmp/x","risk":"destructive","needsShift":true}]}}`

// suggestServer answers like keel serve: the suggestion above to an agent-suggest, agent-insert to an inject.
func suggestServer(t *testing.T, role string, insert func(ws *websocket.Conn, m map[string]any)) (*Conn, *injectServer) {
	c, s := dialInject(t, "ready", role, nonceAnswer)
	s.onInject = insert
	s.onOther = func(ws *websocket.Conn, m map[string]any) {
		switch m["type"] {
		case "agent-detect":
			ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"agent-detect","seq":1,"plain":true,"source":"jev"}`))
		case "agent-suggest":
			ws.WriteMessage(websocket.TextMessage, []byte(suggestionJSON))
			ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"agent-suggest-risk","suggestionId":"s_1","options":[{"risk":"changes","needsShift":true,"judged":true}]}`))
		}
	}
	return c, s
}

func waitEvents(t *testing.T, c *Conn, n int) (out []SuggestEvent) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end) && len(out) < n; time.Sleep(5 * time.Millisecond) {
		out = append(out, c.SuggestEvents()...)
	}
	if len(out) < n {
		t.Fatalf("got %d events, want %d", len(out), n)
	}
	return out
}

func TestSuggestFramesHaveTheAppsShapeAndAnswersComeBackAsEvents(t *testing.T) {
	c, s := suggestServer(t, "driver", nil)
	if err := c.SuggestDetect(1, "show the files"); err != nil {
		t.Fatal(err)
	}
	ev := waitEvents(t, c, 1)
	if ev[0].Kind != SuggestDetected || ev[0].Seq != 1 || !ev[0].Plain || ev[0].Source != "jev" {
		t.Fatalf("detect answer %+v", ev[0])
	}
	if err := c.SuggestAsk(2, "show the files"); err != nil {
		t.Fatal(err)
	}
	ev = waitEvents(t, c, 2)
	a := ev[0]
	if a.Kind != SuggestAnswered || a.Seq != 2 || !a.OK || a.Suggestion == nil || a.Suggestion.ID != "s_1" || len(a.Suggestion.Options) != 2 {
		t.Fatalf("suggest answer %+v", a)
	}
	if o := a.Suggestion.Options[1]; o.Command != "rm -rf /tmp/x" || o.Risk != "destructive" || !o.NeedsShift {
		t.Fatalf("option %+v", o)
	}
	if r := ev[1]; r.Kind != SuggestRated || r.ID != "s_1" || len(r.Options) != 1 || r.Options[0].Risk != "changes" || !r.Options[0].NeedsShift || !r.Options[0].Judged {
		t.Fatalf("risk update %+v", r)
	}
	f := s.all()
	if len(f) != 2 || f[0]["type"] != "agent-detect" || f[0]["seq"] != float64(1) || f[0]["line"] != "show the files" || len(f[0]) != 3 ||
		f[1]["type"] != "agent-suggest" || f[1]["seq"] != float64(2) || f[1]["line"] != "show the files" || len(f[1]) != 3 {
		t.Fatalf("frames %v", f)
	}
}

func TestSuggestQuestionsSendNothingForAFollowerOrASessionNotReady(t *testing.T) {
	for _, tc := range []struct{ status, role string }{{"ready", "follower"}, {"ready", "owner"}, {"connecting", "driver"}} {
		c, s := dialInject(t, tc.status, tc.role, nonceAnswer)
		if err := c.SuggestDetect(1, "show the files"); err == nil {
			t.Errorf("%v: detect sent", tc)
		}
		if err := c.SuggestAsk(2, "show the files"); err == nil {
			t.Errorf("%v: ask sent", tc)
		}
		if err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_1"}); err == nil {
			t.Errorf("%v: insert sent", tc)
		}
		if err := c.RestoreWords("x"); err == nil {
			t.Errorf("%v: restore sent", tc)
		}
		time.Sleep(50 * time.Millisecond)
		if got := s.types(); len(got) != 0 {
			t.Errorf("%v: frames %v", tc, got)
		}
	}
}

func TestSuggestInsertSendsChallengeThenInjectWithTheServersBytesAndTheSuggestionReference(t *testing.T) {
	var shift any
	c, s := suggestServer(t, "driver", func(ws *websocket.Conn, m map[string]any) {
		shift = m["suggestion"].(map[string]any)["shift"]
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"agent-insert","ok":true,"suggestionId":"s_1"}`))
	})
	c.SuggestAsk(2, "x y")
	waitEvents(t, c, 2)
	if err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_1", Option: 1, Shift: true}); err != nil {
		t.Fatal(err)
	}
	f := s.all()
	if got := s.types(); len(got) != 3 || got[1] != "inject-challenge" || got[2] != "inject" {
		t.Fatalf("frames %v", got)
	}
	for _, m := range f[1:] {
		if m["command"] != "\x15rm -rf /tmp/x" {
			t.Fatalf("%v carries %q, not the bytes keel serve gave for option 1", m["type"], m["command"])
		}
		if strings.ContainsAny(m["command"].(string), "\r\n") {
			t.Fatal("an Enter in an insert")
		}
	}
	sug := f[2]["suggestion"].(map[string]any)
	if f[2]["nonce"] != "n-1" || sug["suggestionId"] != "s_1" || sug["option"] != float64(1) || shift != true {
		t.Fatalf("inject %v", f[2])
	}
	if _, ok := f[2]["agent"]; ok {
		t.Fatal("an agent part on a suggestion's insert")
	}
}

func TestSuggestInsertSendsShiftExactlyAsPressed(t *testing.T) {
	var got []any
	c, _ := suggestServer(t, "driver", func(ws *websocket.Conn, m map[string]any) {
		got = append(got, m["suggestion"].(map[string]any)["shift"])
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"agent-insert","ok":false,"suggestionId":"s_1","message":"Plain ⇥ never inserts a destructive command"}`))
	})
	c.SuggestAsk(2, "x y")
	waitEvents(t, c, 2)
	err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_1", Option: 1}) // plain ⇥ on the destructive option
	if err == nil || err.Error() != "Plain ⇥ never inserts a destructive command" {
		t.Fatalf("a refusal comes back in keel serve's words: %v", err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("the client upgraded shift: %v", got)
	}
}

func TestSuggestInsertNeedsASuggestionKeelServeSentAndOneLine(t *testing.T) {
	c, s := suggestServer(t, "driver", nil)
	if err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_unknown"}); !errors.Is(err, ErrSuggestionGone) {
		t.Fatalf("unknown suggestion: %v", err)
	}
	c.SuggestAsk(2, "x y")
	waitEvents(t, c, 2)
	if err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_1", Option: 5}); !errors.Is(err, ErrSuggestionGone) {
		t.Fatalf("unknown option: %v", err)
	}
	c.mu.Lock()
	c.sugKept = append(c.sugKept, keptSuggestion{id: "s_ml", bytes: []string{"\x15a" + string(rune(10)) + "b"}})
	c.mu.Unlock()
	if err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_ml"}); !errors.Is(err, ErrMultiLine) {
		t.Fatalf("a command over lines: %v", err)
	}
	for _, ty := range s.types() {
		if ty == "inject" || ty == "inject-challenge" {
			t.Fatalf("a frame %s went out", ty)
		}
	}
}

func TestSuggestInsertNoAnswerIsAnErrorAndOneAtATime(t *testing.T) {
	c, _ := suggestServer(t, "driver", func(*websocket.Conn, map[string]any) {}) // keel serve never answers
	c.wait = 200 * time.Millisecond
	c.SuggestAsk(2, "x y")
	waitEvents(t, c, 2)
	done := make(chan error, 1)
	go func() { done <- c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_1"}) }()
	time.Sleep(60 * time.Millisecond)
	if err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_1"}); !errors.Is(err, ErrInjectBusy) {
		t.Fatalf("a second insert: %v", err)
	}
	if err := <-done; !errors.Is(err, ErrNoAgentAnswer) {
		t.Fatalf("no answer: %v", err)
	}
}

func TestAnAnswerAboutAnotherSuggestionIsNotOurs(t *testing.T) {
	c, _ := suggestServer(t, "driver", func(ws *websocket.Conn, m map[string]any) {
		ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"agent-insert","ok":true,"suggestionId":"s_other"}`))
	})
	c.wait = 200 * time.Millisecond
	c.SuggestAsk(2, "x y")
	waitEvents(t, c, 2)
	if err := c.SuggestInsert(context.Background(), SuggestionRef{ID: "s_1"}); !errors.Is(err, ErrNoAgentAnswer) {
		t.Fatalf("%v", err)
	}
}

func TestRestoreBytesClearTheLineAndCannotPressEnter(t *testing.T) { // lib/suggest.js restoreBytes
	if got := restoreBytes("show files\r\nrm -rf /"); got != "\x15show filesrm -rf /" {
		t.Fatalf("%q", got)
	}
	if got := restoreBytes("a\x03b\x7fc\x1b[A"); strings.ContainsAny(got[1:], "\x03\x7f\x1b\r\n") {
		t.Fatalf("control characters kept: %q", got)
	}
}
