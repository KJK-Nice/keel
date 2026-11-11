package keelclient

import (
	"context"
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
