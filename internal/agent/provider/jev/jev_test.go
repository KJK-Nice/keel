package jev

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
)

// All tests talk to an httptest server on loopback; nothing leaves the
// machine.

const testKey = "jev-test-key-not-real-0123"

func detectRequest(r *redact.Redactor, line string) provider.JudgeRequest {
	return provider.JudgeRequest{
		Fields: map[string]redact.Safe{"prompt_line": r.Text(line)},
		Lists:  map[string][]redact.Safe{"commands": {r.Text("df -h"), r.Text("rm -rf /tmp/x")}},
		Questions: map[string]provider.Question{
			"plain": {Type: provider.QuestionNoul, Instructions: "Is `prompt_line` plain language?", True: "a request", False: "a command"},
			"risk_0": {Type: provider.QuestionChoice, Instructions: "What does `commands[0]` do?", Options: []provider.Option{
				{Key: "readonly", Description: "reads"}, {Key: "changes", Description: "changes"}, {Key: "destructive"},
			}},
		},
	}
}

func TestJudgeSendsTheDocumentedBodyAndKeyInHeader(t *testing.T) {
	var got map[string]any
	var path, auth, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, query = r.URL.Path, r.Header.Get("Authorization"), r.URL.RawQuery
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s %s", r.Method, r.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"model":"jev-1.13.0","answers":{
			"plain":{"type":"noul","noul":0.93},
			"risk_0":{"type":"choice","choice":"readonly","confidence":0.9,"probabilities":{"readonly":0.95,"changes":0.04,"destructive":0.01}}
		},"usage":{"input_tokens":120,"output_tokens":9}}`)
	}))
	defer srv.Close()
	c, err := New(testKey, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	r := redact.New(redact.Options{})
	resp, err := c.Judge(context.Background(), detectRequest(r, "show disk usage password=hunter22"))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/systemone" || auth != "Bearer "+testKey || query != "" {
		t.Fatalf("path %q auth %q query %q", path, auth, query)
	}
	if got["model"] != Model {
		t.Fatalf("model = %v", got["model"])
	}
	state := got["state"].(map[string]any)
	line := state["prompt_line"].(string)
	if strings.Contains(line, "hunter22") || !strings.Contains(line, "show disk usage") {
		t.Fatalf("state.prompt_line = %q (not redacted)", line)
	}
	if cmds := state["commands"].([]any); len(cmds) != 2 || cmds[1] != "rm -rf /tmp/x" {
		t.Fatalf("state.commands = %v", cmds)
	}
	qs := got["questions"].(map[string]any)
	plain := qs["plain"].(map[string]any)
	if plain["type"] != "noul" || plain["criteria"].(map[string]any)["true"] != "a request" {
		t.Fatalf("plain question = %v", plain)
	}
	choice := qs["risk_0"].(map[string]any)
	crit := choice["criteria"].(map[string]any)
	if choice["type"] != "choice" || crit["readonly"] != "reads" || crit["destructive"] != nil {
		t.Fatalf("choice question = %v", choice)
	}
	if _, ok := crit["destructive"]; !ok {
		t.Fatal("an option without a description must still be sent (as null)")
	}
	if resp.Nouls["plain"] != 0.93 || resp.Choices["risk_0"].Choice != "readonly" || resp.Choices["risk_0"].Probabilities["changes"] != 0.04 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Model != "jev-1.13.0" || resp.InputTokens != 120 {
		t.Fatalf("meta = %+v", resp)
	}
	// The key is only in the header, never in the body.
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), testKey) {
		t.Fatal("key in the body")
	}
}

func TestJudgeErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `{"error":"bad key ` + testKey + `"}`},
		{"rate limited", 429, `{}`},
		{"overloaded", 529, `{}`},
		{"not json", 200, `<html>`},
		{"missing answer", 200, `{"model":"jev-1.13.0","answers":{"plain":{"type":"noul","noul":0.5}}}`},
		{"wrong type", 200, `{"answers":{"plain":{"type":"choice","choice":"x"},"risk_0":{"type":"choice","choice":"readonly"}}}`},
		{"out of range", 200, `{"answers":{"plain":{"type":"noul","noul":1.7},"risk_0":{"type":"choice","choice":"readonly"}}}`},
		{"unknown option", 200, `{"answers":{"plain":{"type":"noul","noul":0.2},"risk_0":{"type":"choice","choice":"nuke"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c, _ := New(testKey, WithBaseURL(srv.URL))
			_, err := c.Judge(context.Background(), detectRequest(redact.New(redact.Options{}), "hello there"))
			if err == nil {
				t.Fatal("no error")
			}
			if strings.Contains(err.Error(), testKey) {
				t.Fatalf("error holds the key: %v", err)
			}
		})
	}
}

func TestJudgeHonoursTheContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	c, _ := New(testKey, WithBaseURL(srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.Judge(ctx, detectRequest(redact.New(redact.Options{}), "hello there")); err == nil {
		t.Fatal("no error after the deadline")
	}
	if time.Since(start) > time.Second {
		t.Fatal("the deadline was not honoured")
	}
}

func TestNewNeedsAKeyAndBadQuestionsAreRefused(t *testing.T) {
	if _, err := New("  "); err != provider.ErrNoKey {
		t.Fatalf("empty key → %v", err)
	}
	c, _ := New(testKey, WithBaseURL("http://127.0.0.1:1"))
	if _, err := c.Judge(context.Background(), provider.JudgeRequest{}); err == nil {
		t.Fatal("no questions accepted")
	}
	bad := provider.JudgeRequest{Questions: map[string]provider.Question{"x": {Type: provider.QuestionChoice, Instructions: "?", Options: []provider.Option{{Key: "a"}}}}}
	if _, err := c.Judge(context.Background(), bad); err == nil {
		t.Fatal("one-option choice accepted")
	}
}
