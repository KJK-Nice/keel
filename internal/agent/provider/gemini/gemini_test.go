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
