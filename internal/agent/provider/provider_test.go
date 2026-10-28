package provider_test

import (
	"context"
	"strings"
	"testing"

	"keel/internal/agent/provider"
	"keel/internal/agent/provider/providertest"
	"keel/internal/agent/redact"
)

func TestDescriptors(t *testing.T) {
	d, ok := provider.Lookup("gemini")
	if !ok || d.Kind != provider.KindModel || d.DefaultModel == "" || d.TermsURL == "" {
		t.Fatalf("gemini = %+v", d)
	}
	// The warning says "may", names the free tier, and points to the terms.
	for _, want := range []string{"free tier", "may use", "Gemini API terms"} {
		if !strings.Contains(d.Warning, want) {
			t.Fatalf("warning lacks %q: %s", want, d.Warning)
		}
	}
	a, ok := provider.Lookup("anthropic")
	if !ok || a.Kind != provider.KindModel || a.ShortName != "Claude" || a.DefaultModel == "" || a.SuggestModel == "" || a.TermsURL == "" {
		t.Fatalf("anthropic = %+v", a)
	}
	if !provider.ListedElsewhere("anthropic", d.DefaultModel) || !provider.ListedElsewhere("gemini", a.DefaultModel) ||
		provider.ListedElsewhere("gemini", d.DefaultModel) || provider.ListedElsewhere("gemini", "custom-model") {
		t.Fatal("ListedElsewhere is wrong")
	}
	if j, ok := provider.Lookup("jev"); !ok || j.Kind != provider.KindDetector {
		t.Fatalf("jev = %+v", j)
	}
	if _, ok := provider.Lookup("openai"); ok {
		t.Fatal("unknown provider found")
	}
	list := provider.Descriptors()
	list[0].Models[0] = "changed"
	if again, _ := provider.Lookup("gemini"); again.Models[0] == "changed" {
		t.Fatal("Descriptors must return a copy")
	}
}

func TestFakeRecordsRedactedRequests(t *testing.T) {
	f := &providertest.Fake{Reply: provider.Response{Text: "uptime"}}
	r := redact.New(redact.Options{})
	resp, err := f.Generate(context.Background(), provider.Request{
		Model:    "m",
		System:   redact.Static("sys"),
		Messages: []provider.Message{{Role: provider.RoleUser, Text: r.Text("load? token=abc123xyz")}},
	})
	if err != nil || resp.Text != "uptime" {
		t.Fatal(resp, err)
	}
	reqs := f.Requests()
	if len(reqs) != 1 || reqs[0].Messages[0].Text.String() != "load? token=[secret:1]" {
		t.Fatalf("requests = %+v", reqs)
	}
	var _ provider.Provider = f
}
