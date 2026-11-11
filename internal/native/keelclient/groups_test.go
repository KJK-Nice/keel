package keelclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFolderRequestsSendNameAndParentOnly(t *testing.T) {
	r := newRec(t)
	r.reply = `{"id":"g1","name":"Prod","parentId":"p"}`
	ctx := context.Background()
	// A save sends the name and the parent and nothing else: roles and variables are keel serve's.
	g, err := r.c.SaveGroup(ctx, Group{Name: "Prod", ParentID: "p", Roles: map[string]string{"h": "worker"}, Vars: []GroupVar{{Name: "k", Value: "v"}}})
	if err != nil || g.ID != "g1" {
		t.Fatalf("SaveGroup: %v %+v", err, g)
	}
	if got, body := r.last(); got != "POST /api/groups" || body != `{"name":"Prod","parentId":"p"}` {
		t.Fatalf("new folder: %s %s", got, body)
	}
	if _, err := r.c.SaveGroup(ctx, Group{ID: "g 1", Name: "Prod2"}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/groups/g 1" || body != `{"name":"Prod2","parentId":""}` {
		t.Fatalf("edit folder: %s %s", got, body)
	}
	r.reply = ``
	r.status = 204
	if err := r.c.DeleteGroup(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.last(); got != "DELETE /api/groups/g1" {
		t.Fatalf("delete: %s", got)
	}
	if err := r.c.MoveHosts(ctx, []string{"a", "b"}, ""); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/hosts/move" || body != `{"groupId":"","ids":["a","b"]}` {
		t.Fatalf("move hosts: %s %s", got, body)
	}
}

func TestRolesVariablesNotesAndTheExportAreTheAppsRequests(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"id":"g1","name":"Prod","vars":[{"name":"ver","value":"1.2"}]}`
	g, err := r.c.SetGroupRole(ctx, "g1", "h1", "worker")
	if err != nil || g.ID != "g1" {
		t.Fatalf("SetGroupRole: %v", err)
	}
	if got, body := r.last(); got != "PUT /api/groups/g1/roles" || body != `{"hostId":"h1","role":"worker"}` {
		t.Fatalf("role: %s %s", got, body)
	}
	if g, err = r.c.SetGroupVar(ctx, "g1", "ver", "1.2"); err != nil || len(g.Vars) != 1 {
		t.Fatalf("SetGroupVar: %v %+v", err, g)
	}
	if got, body := r.last(); got != "PUT /api/groups/g1/vars" || body != `{"name":"ver","value":"1.2"}` {
		t.Fatalf("var: %s %s", got, body)
	}
	// A refusal names the method, path and status, and keel serve's own reason: never the value.
	r.status, r.reply = 400, `{"error":"Keep secrets in the vault"}`
	_, err = r.c.SetGroupVar(ctx, "g1", "tok", "fake-secret-looking-value")
	var se *StatusError
	if !errors.As(err, &se) || se.Msg != "Keep secrets in the vault" || strings.Contains(err.Error(), "fake-secret-looking-value") {
		t.Fatalf("refusal: %v", err)
	}
	// A note about a folder carries groupId and no hostId.
	r.status, r.reply = 200, `{"note":{"id":"n1","groupId":"g1","text":"t"}}`
	if _, err := r.c.SaveNote(ctx, NoteInput{GroupID: "g1", Text: "t"}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/agent/notes" || body != `{"groupId":"g1","hostId":"","text":"t"}` {
		t.Fatalf("group note: %s %s", got, body)
	}
	// The export: the query is the app's, in its order.
	r.reply = `{"files":[{"name":"inventory.ini","kind":"inventory","content":"[g]\n","lines":1}]}`
	files, err := r.c.GroupAnsible(ctx, "g1", AnsibleQuery{Format: "yaml", RunbookID: "rb 1", Mode: "parallel", Jump: true})
	if err != nil || len(files) != 1 || files[0].Name != "inventory.ini" {
		t.Fatalf("GroupAnsible: %v %+v", err, files)
	}
	r.mu.Lock()
	got := r.got[len(r.got)-1]
	r.mu.Unlock()
	if got != "GET /api/groups/g1/ansible" {
		t.Fatalf("export: %s", got)
	}
	if q := (AnsibleQuery{Format: "yaml", RunbookID: "rb 1", Mode: "parallel", Jump: true}).Values(); q != "format=yaml&runbookId=rb+1&mode=parallel&user=0&jump=1" {
		t.Fatalf("query %q", q)
	}
}

func TestGroupMembersReadsTheFolderAndItsHosts(t *testing.T) {
	r := newRec(t)
	r.reply = `{"group":{"id":"g1","name":"Prod","roles":{"h1":"worker"},"vars":[{"name":"ver","value":"1.2"}]},"members":[{"hostId":"h1","name":"wk-1","role":"worker","folder":"Prod"}]}`
	info, err := r.c.GroupMembers(context.Background(), "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := r.last(); got != "GET /api/groups/g1/members" {
		t.Fatalf("request %s", got)
	}
	if info.Group.Roles["h1"] != "worker" || len(info.Group.Vars) != 1 || info.Members[0].Name != "wk-1" || !strings.Contains(info.Members[0].Folder, "Prod") {
		t.Fatalf("members %+v", info)
	}
}
