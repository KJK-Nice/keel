package keelclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConversationCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()

	r.reply = `{"historyDays":30,"sessions":[{"id":"c1","title":"Disk","createdAt":"2026-11-11T09:00:00Z","updatedAt":"2026-11-11T09:05:00Z","hosts":["api-1","db"],"turns":2,"proposals":3}],"saveError":"","unreadable":false}`
	l, err := r.c.Conversations(ctx)
	if got, _ := r.last(); got != "GET /api/agent/conversations" || err != nil || l.HistoryDays != 30 || len(l.Sessions) != 1 ||
		l.Sessions[0].Title != "Disk" || l.Sessions[0].Proposals != 3 || len(l.Sessions[0].Hosts) != 2 || l.Sessions[0].Turns != 2 {
		t.Fatal(got, l, err)
	}

	r.reply = `{"id":"c1","title":"Disk","createdAt":"2026-11-11T09:00:00Z","updatedAt":"2026-11-11T09:05:00Z","saved":true,"turns":[
	{"at":"2026-11-11T09:00:00Z","question":"why?","answer":"because","trace":[{"host":"api-1","status":"read","lines":9}],
	 "proposals":[{"id":"p1","command":"df -h","bytes":"\u0015df -h\n","risk":"readonly","expired":true}]},{"at":"2026-11-11T09:01:00Z","again":true,"trace":[],"proposals":[]}]}`
	tr, err := r.c.Conversation(ctx, "c 1")
	if got, _ := r.last(); (got != "GET /api/agent/conversations/c 1" && got != "GET /api/agent/conversations/c%201") || err != nil || !tr.Saved || len(tr.Turns) != 2 ||
		tr.Turns[0].Answer != "because" || !tr.Turns[0].Proposals[0].Expired || !tr.Turns[1].Again || tr.Turns[0].Trace[0].Lines != 9 {
		t.Fatal(got, tr, err)
	}

	r.reply = `{"id":"c1","title":"New name"}`
	title, err := r.c.RenameConversation(ctx, "c1", "New name")
	if got, body := r.last(); got != "PATCH /api/agent/conversations/c1" || body != `{"title":"New name"}` || err != nil || title != "New name" {
		t.Fatal(got, body, title, err)
	}

	r.reply = ``
	if err := r.c.DeleteConversation(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "DELETE /api/agent/conversations/c1" || body != "" {
		t.Fatal(got, body)
	}
	if err := r.c.DeleteAllConversations(ctx); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "DELETE /api/agent/conversations" || body != "" {
		t.Fatal(got, body)
	}

	r.reply = `{"id":"p9","sessionId":"s1","command":"df -h","bytes":"\u0015df -h\n","risk":"readonly"}`
	p, err := r.c.ProposeAgain(ctx, "c1", "p1")
	if got, body := r.last(); got != "POST /api/agent/conversations/c1/proposals/p1/again" || body != "" || err != nil || p.ID != "p9" || p.Expired {
		t.Fatal(got, body, p, err)
	}

	r.reply = `{"provider":"gemini","configured":false}`
	if err := r.c.DeleteAgentKey(ctx, "gemini"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "DELETE /api/agent/keys/gemini" || body != "" {
		t.Fatal(got, body)
	}
}

func TestSaveAgentSettingsSendsOnlyTheFieldsOfThePatch(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"settings":{"provider":"gemini","model":"m","detection":"auto","suggestions":"on","historySuggestions":"on","defaultScope":"tab","scrollbackLines":300,"redact":{"secrets":true,"ips":true,"hostnames":false},"historyDays":0,"claudeCode":false},"keys":{"gemini":{"configured":true}}}`
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }
	no := false
	for _, c := range []struct {
		name  string
		patch AgentSettingsPatch
		body  string
	}{
		{"scope", AgentSettingsPatch{DefaultScope: str("tab")}, `{"defaultScope":"tab"}`},
		{"lines", AgentSettingsPatch{ScrollbackLines: num(300)}, `{"scrollbackLines":300}`},
		{"history zero is sent", AgentSettingsPatch{HistoryDays: num(0)}, `{"historyDays":0}`},
		{"claude code off is sent, and nothing else", AgentSettingsPatch{ClaudeCode: &no}, `{"claudeCode":false}`},
		{"redact is sent whole", AgentSettingsPatch{Redact: &AgentRedact{Secrets: true, IPs: true}}, `{"redact":{"secrets":true,"ips":true,"hostnames":false}}`},
		{"suggestions", AgentSettingsPatch{Suggestions: str("off"), HistorySuggestions: str("off"), Detection: str("hash-only")}, `{"detection":"hash-only","suggestions":"off","historySuggestions":"off"}`},
		{"model and provider", AgentSettingsPatch{Provider: str("anthropic"), Model: str("c1")}, `{"provider":"anthropic","model":"c1"}`},
	} {
		s, err := r.c.SaveAgentSettings(ctx, c.patch)
		got, body := r.last()
		if got != "PUT /api/agent/settings" || body != c.body || err != nil {
			t.Errorf("%s: %s %s %v", c.name, got, body, err)
		}
		if s.Settings.ScrollbackLines != 300 || !s.Settings.Redact.IPs || s.Settings.ClaudeCode || !s.KeyConfigured() {
			t.Errorf("%s: the saved settings are not read back: %+v", c.name, s)
		}
	}
}

func TestAConversationCallThatFailsNamesNoKeyAndNoBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		http.Error(w, `{"error":"unknown agent provider"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewAt(strings.TrimPrefix(srv.URL, "http://"), tok)
	in := AgentKeyInput{Provider: "gemini", Key: fakeKey}
	err := c.SetAgentKey(context.Background(), in)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 404 || strings.Contains(err.Error(), fakeKey) || strings.Contains(se.Msg, fakeKey) {
		t.Fatalf("%v", err)
	}
	for _, shown := range []string{fmt.Sprintf("%v", in), fmt.Sprintf("%+v", in), fmt.Sprintf("%#v", in), fmt.Sprint(err)} {
		if strings.Contains(shown, fakeKey) {
			t.Errorf("a printed key input or error shows the key: %s", shown)
		}
	}
	if err := c.DeleteAgentKey(context.Background(), "gemini"); err == nil {
		t.Fatal("a refused removal is an error")
	}
}
