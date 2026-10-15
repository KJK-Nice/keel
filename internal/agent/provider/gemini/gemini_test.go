package gemini

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
	var got genRequest
	var path, header, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, header, query = r.URL.Path, r.Header.Get("x-goog-api-key"), r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"df -h"},{"text":" /"}]}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3}}`)
	}))
	defer srv.Close()
	c, err := New(key, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	r := redact.New(redact.Options{})
	resp, err := c.Generate(context.Background(), provider.Request{
		Model:  "gemini-3.8-flash",
		System: redact.Static("You suggest shell commands."),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: r.Text("disk usage? password=hunter22")},
		},
		MaxOutputTokens: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "df -h /" || resp.InputTokens != 12 || resp.OutputTokens != 3 {
		t.Fatalf("resp = %+v", resp)
	}
	if path != "/models/gemini-3.8-flash:generateContent" || header != key || query != "" {
		t.Fatalf("path=%q header=%q query=%q", path, header, query)
	}
	if got.SystemInstruction == nil || got.SystemInstruction.Parts[0].Text != "You suggest shell commands." {
		t.Fatalf("system = %+v", got.SystemInstruction)
	}
	if len(got.Contents) != 1 || got.Contents[0].Role != "user" || strings.Contains(got.Contents[0].Parts[0].Text, "hunter22") {
		t.Fatalf("contents = %+v", got.Contents)
	}
	if got.GenerationConfig.MaxOutputTokens != 64 {
		t.Fatal("max tokens not sent")
	}
	// No level asked: the model's default thinking, no thinkingConfig.
	if got.GenerationConfig.ThinkingConfig != nil {
		t.Fatalf("thinkingConfig sent without a level: %+v", got.GenerationConfig.ThinkingConfig)
	}
}

// The thinking level goes in generationConfig.thinkingConfig.thinkingLevel
// as the API's enum, and a model without MINIMAL gets LOW instead.
func TestGenerateSendsThinkingLevel(t *testing.T) {
	var raw map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		raw = nil
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer srv.Close()
	c, _ := New("k", WithBaseURL(srv.URL))
	level := func() any {
		gc, _ := raw["generationConfig"].(map[string]any)
		tc, _ := gc["thinkingConfig"].(map[string]any)
		return tc["thinkingLevel"]
	}
	for _, tc := range []struct{ model, thinking, want string }{
		{"gemini-3.5-flash-lite", provider.ThinkingMinimal, "MINIMAL"},
		{"gemini-3.8-flash", provider.ThinkingMinimal, "LOW"},
		{"gemini-3.8-flash", provider.ThinkingMedium, "MEDIUM"},
	} {
		_, err := c.Generate(context.Background(), provider.Request{
			Model:           tc.model,
			Thinking:        tc.thinking,
			MaxOutputTokens: 32,
			Messages:        []provider.Message{{Role: provider.RoleUser, Text: redact.Static("x")}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if path != "/models/"+tc.model+":generateContent" || level() != tc.want {
			t.Fatalf("%s/%s: path %q, body %v", tc.model, tc.thinking, path, raw)
		}
		gc := raw["generationConfig"].(map[string]any)
		if gc["maxOutputTokens"] != float64(32) || len(gc) != 2 {
			t.Fatalf("generationConfig = %v", gc)
		}
	}
	if _, err := c.Generate(context.Background(), provider.Request{Model: "m", Thinking: "lots", Messages: []provider.Message{{Text: redact.Static("x")}}}); err == nil {
		t.Fatal("unknown level accepted")
	}
}

// The suggestion model in the descriptor takes its thinking level as is:
// never "minimal" sent to a model that would lower it.
func TestSuggestModelTakesItsThinkingLevel(t *testing.T) {
	d, _ := provider.Lookup("gemini")
	if d.SuggestModel != "gemini-3.5-flash-lite" || d.SuggestThinking != provider.ThinkingMinimal {
		t.Fatalf("descriptor = %+v", d)
	}
	if !SupportsMinimal(d.SuggestModel) || SupportsMinimal(d.DefaultModel) {
		t.Fatal("thinking table out of step with the descriptor")
	}
	listed := false
	for _, m := range d.Models {
		listed = listed || m == d.SuggestModel
	}
	if !listed {
		t.Fatalf("%s not in the model list", d.SuggestModel)
	}
}

func TestGenerateErrorNeverCarriesKey(t *testing.T) {
	const key = "echoed-key-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":400,"message":"API key `+key+` not valid","status":"INVALID_ARGUMENT"}}`)
	}))
	defer srv.Close()
	c, _ := New(key, WithBaseURL(srv.URL))
	_, err := c.Generate(context.Background(), provider.Request{Model: "m", Messages: []provider.Message{{Role: provider.RoleUser, Text: redact.Static("hi")}}})
	if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "HTTP 400") {
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
}

func TestBlockedPromptAndNoCandidates(t *testing.T) {
	for body, want := range map[string]string{
		`{"promptFeedback":{"blockReason":"SAFETY"}}`: "blocked",
		`{"candidates":[]}`:                           "no candidates",
		`not json`:                                    "unreadable",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		c, _ := New("k", WithBaseURL(srv.URL))
		_, err := c.Generate(context.Background(), provider.Request{Model: "m", Messages: []provider.Message{{Text: redact.Static("x")}}})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v", body, err)
		}
	}
}
