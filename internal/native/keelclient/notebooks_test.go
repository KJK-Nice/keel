package keelclient

import (
	"context"
	"strings"
	"testing"
)

func TestNotebookCallsUseTheRightMethodPathAndBody(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `[{"id":"n1","name":"Deploy","cells":[{"id":"c1","body":"a"},{"id":"c2","body":"b"}],"pinned":true,"source":"agent","private":true,"rev":4,"lastBy":"agent","agentEditedAt":"2026-03-04T05:06:07Z","agentUnreviewed":true,"createdAt":"2026-03-04T05:06:07Z","updatedAt":"2026-03-05T05:06:07Z"}]`
	list, err := r.c.Notebooks(ctx)
	if got, _ := r.last(); got != "GET /api/notebooks" || err != nil || len(list) != 1 {
		t.Fatal(got, list, err)
	}
	n := list[0]
	if !n.Pinned || n.Source != "agent" || !n.Private || n.Rev != 4 || n.LastBy != "agent" || n.AgentEditedAt == nil || !n.AgentUnreviewed || n.UpdatedAt.IsZero() {
		t.Fatalf("fields lost: %+v", n)
	}
	if n.Markdown() != "a\n\nb" {
		t.Fatalf("Markdown() = %q", n.Markdown())
	}
	if w := n.WithMarkdown("x"); len(w.Cells) != 1 || w.Cells[0].ID != "c1" || w.Cells[0].Body != "x" {
		t.Fatalf("WithMarkdown: %+v", w.Cells)
	}

	r.reply = `{"id":"n2","name":"New","cells":[],"rev":1}`
	if _, err := r.c.CreateNotebook(ctx, NotebookSave{Name: "New"}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/notebooks" || body != `{"name":"New","cells":[],"pinned":false,"rev":0}` {
		t.Fatal(got, body)
	}

	r.reply = `{"id":"n2","name":"New","cells":[{"id":"c1","body":"x"}],"rev":2,"replacedAgentEdit":true}`
	out, err := r.c.SaveNotebook(ctx, "n 2", NotebookSave{Name: "New", Cells: []NotebookCell{{ID: "c1", Body: "x"}}, Rev: 1})
	got, body := r.last()
	if got != "PUT /api/notebooks/n 2" || err != nil || !out.ReplacedAgentEdit || out.Rev != 2 || out.Markdown() != "x" ||
		body != `{"name":"New","cells":[{"id":"c1","body":"x"}],"pinned":false,"rev":1}` {
		t.Fatal(got, body, out, err)
	}

	name, pin := "Renamed", false
	if _, err := r.c.PatchNotebook(ctx, "n1", &name, nil); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PATCH /api/notebooks/n1" || body != `{"name":"Renamed"}` {
		t.Fatal(got, body)
	}
	if _, err := r.c.PatchNotebook(ctx, "n1", nil, &pin); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PATCH /api/notebooks/n1" || body != `{"pinned":false}` {
		t.Fatal(got, body)
	}

	r.status, r.reply = 204, ""
	if err := r.c.DeleteNotebook(ctx, "n1", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.last(); got != "DELETE /api/notebooks/n1" {
		t.Fatal(got)
	}
	if err := r.c.DeleteNotebook(ctx, "n1", true); err != nil {
		t.Fatal(err)
	}
	r.status, r.reply = 200, `{"id":"n1"}`
	if _, err := r.c.PutNotebookPrivacy(ctx, "n1", true); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/notebooks/n1/private" || body != `{"private":true}` {
		t.Fatal(got, body)
	}
	if _, err := r.c.MarkNotebookReviewed(ctx, "n1"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/notebooks/n1/reviewed" || body != "" {
		t.Fatal(got, body)
	}
}

func TestNotebookPermanentDeleteSendsTheQuery(t *testing.T) {
	r := newRec(t)
	r.status = 204
	_ = r.c.DeleteNotebook(context.Background(), "n1", true)
	_ = r.c.DeleteNotebook(context.Background(), "n1", false)
	if len(r.queries) != 2 || r.queries[0] != "permanent=1" || r.queries[1] != "" {
		t.Fatalf("queries %q", r.queries)
	}
}

func TestNotebookHistoryRefusalIsTyped(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.status, r.reply = 409, `{"error":"Not saved: this notebook holds an agent edit.","reason":"history"}`
	_, err := r.c.SaveNotebook(ctx, "n1", NotebookSave{Name: "x"})
	if !IsHistoryRefusal(err) {
		t.Fatalf("a 409 history is not typed: %v", err)
	}
	if err := r.c.DeleteNotebook(ctx, "n1", false); !IsHistoryRefusal(err) {
		t.Fatalf("delete: %v", err)
	}
	r.reply = `{"error":"The notebook changed meanwhile.","reason":"conflict"}`
	if _, err := r.c.RestoreNotebookVersion(ctx, "n1", "v1", 3); IsHistoryRefusal(err) {
		t.Fatalf("a conflict is not a history refusal: %v", err)
	}
}

func TestNotebookHistoryAndTrashCalls(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"versions":[{"id":"v1","notebookId":"n1","name":"Deploy","rev":2,"by":"agent","event":"before-agent-edit","summary":"s","at":"2026-03-04T05:06:07Z","bytes":12}]}`
	vs, err := r.c.NotebookHistory(ctx, "n1")
	if got, _ := r.last(); got != "GET /api/notebooks/n1/history" || err != nil || len(vs) != 1 || vs[0].By != "agent" || vs[0].Event != "before-agent-edit" || vs[0].Bytes != 12 {
		t.Fatal(got, vs, err)
	}
	r.reply = `{"id":"v1","cells":[{"id":"c","body":"hello"}]}`
	v, err := r.c.NotebookVersion(ctx, "n1", "v1")
	if got, _ := r.last(); got != "GET /api/notebooks/n1/history/v1" || err != nil || v.Markdown() != "hello" {
		t.Fatal(got, v, err)
	}
	r.reply = `{"id":"n1","rev":5}`
	if _, err := r.c.RestoreNotebookVersion(ctx, "n1", "v1", 4); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/notebooks/n1/history/v1/restore" || body != `{"rev":4}` {
		t.Fatal(got, body)
	}
	r.status, r.reply = 204, ""
	if err := r.c.PurgeNotebookVersion(ctx, "n1", "v1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.last(); got != "DELETE /api/notebooks/n1/history/v1" {
		t.Fatal(got)
	}
	r.status, r.reply = 200, `{"versions":[{"id":"d1","name":"Gone","event":"deleted"}]}`
	d, err := r.c.DeletedNotebooks(ctx)
	if got, _ := r.last(); got != "GET /api/notebook-trash" || err != nil || len(d) != 1 || d[0].Name != "Gone" {
		t.Fatal(got, d, err)
	}
	r.reply = `{"id":"d1","name":"Gone","cells":[{"id":"c","body":"x"}]}`
	if dv, err := r.c.DeletedNotebook(ctx, "d1"); err != nil || dv.Markdown() != "x" {
		t.Fatal(dv, err)
	}
	if got, _ := r.last(); got != "GET /api/notebook-trash/d1" {
		t.Fatal(got)
	}
	r.reply = `{"id":"n9"}`
	if _, err := r.c.RestoreDeletedNotebook(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.last(); got != "POST /api/notebook-trash/d1/restore" {
		t.Fatal(got)
	}
	r.status, r.reply = 204, ""
	if err := r.c.PurgeDeletedNotebook(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.last(); got != "DELETE /api/notebook-trash/d1" {
		t.Fatal(got)
	}
}

// A notebook's fence goes out as a direct type with Kind "notebook" and the notebook's id (pty.js typeDirect), and as an
// approved inject with notebook:true and nothing added to the bytes.
func TestANotebookFenceFramesAreTheAppsFrames(t *testing.T) {
	c, s := dialDirect(t, answerWith("type-direct", map[string]any{"ok": true}))
	res, err := c.DirectType(context.Background(), "df -h", DirectRef{Kind: "notebook", NotebookID: "n1", PromptLine: "$"}, nil)
	if err != nil || !res.Typed {
		t.Fatalf("got %+v, %v", res, err)
	}
	waitFrames(s, 2)
	d := s.all()[1]["direct"].(map[string]any)
	if d["kind"] != "notebook" || d["notebookId"] != "n1" || d["snippetId"] != nil {
		t.Fatalf("direct part %v", d)
	}
	if s.all()[1]["command"] != "df -h" {
		t.Fatalf("bytes %q", s.all()[1]["command"])
	}

	c2, s2 := dialInject(t, "ready", "driver", nonceAnswer)
	if err := c2.ApprovedInject(context.Background(), "echo a\necho b\n", nil, FromNotebook); err != nil {
		t.Fatal(err)
	}
	waitFrames(s2, 2)
	in := s2.all()[1]
	if in["notebook"] != true || in["command"] != "echo a\necho b\n" || in["agent"] != nil || in["direct"] != nil {
		t.Fatalf("inject frame %v", in)
	}
	for _, m := range s2.all() {
		if strings.Contains(m["type"].(string), "check") {
			t.Fatalf("a check frame: %v", m)
		}
	}
	// Without the option, no notebook field: a snippet's frame is unchanged.
	c3, s3 := dialInject(t, "ready", "driver", nonceAnswer)
	if err := c3.ApprovedInject(context.Background(), "ls\n", nil); err != nil {
		t.Fatal(err)
	}
	waitFrames(s3, 2)
	if _, ok := s3.all()[1]["notebook"]; ok {
		t.Fatalf("a notebook field on a plain inject: %v", s3.all()[1])
	}
}

func TestADirectTypeOfAnUnknownKindSendsNothing(t *testing.T) {
	c, s := dialDirect(t, answerWith("type-direct", map[string]any{"ok": true}))
	if _, err := c.DirectType(context.Background(), "df -h", DirectRef{Kind: "other"}, nil); err == nil {
		t.Fatal("no error for an unknown kind")
	}
	if len(s.all()) != 0 {
		t.Fatalf("frames sent: %v", s.types())
	}
}
