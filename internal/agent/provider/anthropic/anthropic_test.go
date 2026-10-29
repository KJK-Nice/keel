package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
)

// All tests talk to an httptest server on loopback; nothing leaves the
// machine.

func TestGenerateSendsOnlyRequestAndKeyInHeader(t *testing.T) {
	const key = "test-key-not-real"
	var got map[string]any
	var path, header, version, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, header, version, query = r.URL.Path, r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"), r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"type":"message","role":"assistant","content":[{"type":"thinking","thinking":"","signature":"abc"},{"type":"text","text":"df -h"},{"type":"text","text":" /"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":3}}`)
	}))
	defer srv.Close()
	c, err := New(key, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	r := redact.New(redact.Options{})
	resp, err := c.Generate(context.Background(), provider.Request{
		Model:  "claude-sonnet-5-5",
		System: redact.Static("You suggest shell commands."),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: r.Text("disk usage? password=hunter22")},
			{Role: provider.RoleModel, Text: redact.Static("Which disk?")},
			{Role: provider.RoleUser, Text: redact.Static("root")},
		},
		MaxOutputTokens: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "df -h /" || resp.InputTokens != 12 || resp.OutputTokens != 3 || resp.Stop != "" {
		t.Fatalf("resp = %+v", resp)
	}
	if path != "/v1/messages" || header != key || version != apiVersion || query != "" {
		t.Fatalf("path=%q header=%q version=%q query=%q", path, header, version, query)
	}
	if got["model"] != "claude-sonnet-5-5" || got["system"] != "You suggest shell commands." {
		t.Fatalf("body = %v", got)
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", got["messages"])
	}
	roles := []string{}
	for _, m := range msgs {
		mm := m.(map[string]any)
		roles = append(roles, mm["role"].(string))
		if strings.Contains(mm["content"].(string), "hunter22") {
			t.Fatalf("secret sent: %v", mm)
		}
	}
	if strings.Join(roles, ",") != "user,assistant,user" {
		t.Fatalf("roles = %v", roles)
	}
	// 64 answer tokens plus the room for the default (high) effort.
	if got["max_tokens"] != float64(64+16384) {
		t.Fatalf("max_tokens = %v", got["max_tokens"])
	}
	// No level asked: the model's default effort, no output_config, and
	// nothing else in the body.
	for k := range got {
		switch k {
		case "model", "max_tokens", "system", "messages":
		default:
			t.Fatalf("unexpected field %q in %v", k, got)
		}
	}
}

// The thinking level goes in output_config.effort.
func TestGenerateSendsEffort(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = nil
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))
	defer srv.Close()
	c, _ := New("k", WithBaseURL(srv.URL))
	for _, tc := range []struct {
		thinking, want string
		max            float64
	}{
		{provider.ThinkingMinimal, "low", 32 + 4096},
		{provider.ThinkingLow, "low", 32 + 4096},
		{provider.ThinkingMedium, "medium", 32 + 8192},
		{provider.ThinkingHigh, "high", 32 + 16384},
	} {
		_, err := c.Generate(context.Background(), provider.Request{
			Model:           "claude-haiku-5-5",
			Thinking:        tc.thinking,
			MaxOutputTokens: 32,
			Messages:        []provider.Message{{Role: provider.RoleUser, Text: redact.Static("x")}},
		})
		if err != nil {
			t.Fatal(err)
		}
		oc, _ := raw["output_config"].(map[string]any)
		if oc["effort"] != tc.want || len(oc) != 1 || raw["max_tokens"] != tc.max {
			t.Fatalf("%s: body %v", tc.thinking, raw)
		}
	}
	if _, err := c.Generate(context.Background(), provider.Request{Model: "m", Thinking: "lots", Messages: []provider.Message{{Text: redact.Static("x")}}}); err == nil {
		t.Fatal("unknown level accepted")
	}
}

// The suggestion model in the descriptor is one of the listed models, and
// its thinking level is one Generate takes.
func TestDescriptorModels(t *testing.T) {
	d, ok := provider.Lookup("anthropic")
	if !ok || d.Kind != provider.KindModel {
		t.Fatalf("descriptor = %+v", d)
	}
	listed := map[string]bool{}
	for _, m := range d.Models {
		listed[m] = true
	}
	if !listed[d.DefaultModel] || !listed[d.SuggestModel] {
		t.Fatalf("default %q or suggest %q not in %v", d.DefaultModel, d.SuggestModel, d.Models)
	}
	if _, err := effort(d.SuggestThinking); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateErrorNeverCarriesKey(t *testing.T) {
	const key = "sk-ant-echoed-key-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key `+key+`"},"request_id":"req_1"}`)
	}))
	defer srv.Close()
	c, _ := New(key, WithBaseURL(srv.URL))
	_, err := c.Generate(context.Background(), provider.Request{Model: "m", Messages: []provider.Message{{Role: provider.RoleUser, Text: redact.Static("hi")}}})
	if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewNeedsKeyAndModelIsChecked(t *testing.T) {
	if _, err := New("  "); !errors.Is(err, provider.ErrNoKey) {
		t.Fatal("empty key accepted")
	}
	c, _ := New("k", WithBaseURL("http://127.0.0.1:1"))
	for _, m := range []string{"", "../x", "a?b"} {
		if _, err := c.Generate(context.Background(), provider.Request{Model: m, Messages: []provider.Message{{Text: redact.Static("x")}}}); err == nil {
			t.Fatalf("model %q accepted", m)
		}
	}
	if _, err := c.Generate(context.Background(), provider.Request{Model: "m"}); err == nil {
		t.Fatal("no messages accepted")
	}
	if _, err := c.Generate(context.Background(), provider.Request{Model: "m", Messages: []provider.Message{{Text: redact.Static("")}}}); err == nil {
		t.Fatal("only empty messages accepted")
	}
}

func TestUnreadableResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not json")
	}))
	defer srv.Close()
	c, _ := New("k", WithBaseURL(srv.URL))
	_, err := c.Generate(context.Background(), provider.Request{Model: "m", Messages: []provider.Message{{Text: redact.Static("x")}}})
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("err = %v", err)
	}
}

// Thinking counts toward max_tokens: the limit sent leaves room for it on
// top of the answer, and never goes over the hard cap.
func TestOutputLimitLeavesRoomForThinking(t *testing.T) {
	for _, tc := range []struct {
		answer int
		effort string
		want   int
	}{
		{8192, "", 8192 + 16384},
		{8192, "high", 8192 + 16384},
		{2048, "low", 2048 + 4096},
		{0, "", defaultAnswer + 16384},
		{120000, "", maxOutputCap},
	} {
		if got := outputLimit(tc.answer, tc.effort); got != tc.want {
			t.Errorf("outputLimit(%d, %q) = %d, want %d", tc.answer, tc.effort, got, tc.want)
		}
	}
}

// A reply that ends for any reason but end_turn comes back as cut, with
// its reason and token counts.
func TestGenerateReportsCutReply(t *testing.T) {
	for reason, want := range map[string]string{
		"end_turn":                      "",
		"":                              "",
		"stop_sequence":                 "",
		"max_tokens":                    provider.StopLength,
		"model_context_window_exceeded": provider.StopLength,
		"refusal":                       provider.StopSafety,
		"pause_turn":                    provider.StopOther,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"content":[{"type":"thinking","thinking":"","signature":"s"},{"type":"text","text":"Free space on /var is "}],"stop_reason":"`+reason+`","usage":{"input_tokens":900,"output_tokens":2048}}`)
		}))
		c, _ := New("k", WithBaseURL(srv.URL))
		resp, err := c.Generate(context.Background(), provider.Request{Model: "claude-sonnet-5-5", MaxOutputTokens: 2048, Messages: []provider.Message{{Text: redact.Static("x")}}})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.Stop != want || resp.Text != "Free space on /var is " || resp.OutputTokens != 2048 || resp.InputTokens != 900 {
			t.Fatalf("%q: resp = %+v", reason, resp)
		}
	}
}
