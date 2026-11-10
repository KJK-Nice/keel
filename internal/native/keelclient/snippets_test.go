package keelclient

import (
	"context"
	"strings"
	"testing"
)

func TestSnippetCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `[{"id":"s1","name":"disk","command":"df -h\n","agentUsable":true,"createdAt":"2026-03-04T05:06:07Z"}]`
	list, err := r.c.Snippets(ctx)
	if got, _ := r.last(); got != "GET /api/snippets" || err != nil || len(list) != 1 || list[0].Command != "df -h\n" || !list[0].AgentUsable || list[0].CreatedAt.IsZero() {
		t.Fatal(got, list, err)
	}
	r.reply = `{"id":"s2","name":"up","command":"uptime\n"}`
	out, err := r.c.SaveSnippet(ctx, SnippetInput{Name: "up", Command: "uptime\n"})
	got, body := r.last()
	if got != "POST /api/snippets" || err != nil || out.ID != "s2" || body != `{"name":"up","command":"uptime\n"}` {
		t.Fatal(got, body, out, err)
	}
	// An update names the id in the path, not the body, and never sends agentUsable: the server keeps the flag.
	if _, err := r.c.SaveSnippet(ctx, SnippetInput{ID: "s 1", Name: "disk", Command: "df\n"}); err != nil {
		t.Fatal(err)
	}
	got, body = r.last()
	if got != "PUT /api/snippets/s 1" || strings.Contains(body, "agentUsable") || strings.Contains(body, `"id"`) {
		t.Fatal(got, body)
	}
	r.c.DeleteSnippet(ctx, "s1")
	if got, _ := r.last(); got != "DELETE /api/snippets/s1" {
		t.Fatal(got)
	}
}
