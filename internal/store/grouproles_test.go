package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func roleFixture(t *testing.T) (*Store, Group, Group, Host, Host, Host) {
	t.Helper()
	st := testStore(t)
	k8s, err := st.UpsertGroup(Group{Name: "k8s-prod"})
	if err != nil {
		t.Fatal(err)
	}
	workers, err := st.UpsertGroup(Group{Name: "workers", ParentID: k8s.ID})
	if err != nil {
		t.Fatal(err)
	}
	cp := noteHost(t, st, "cp-1")
	wk := noteHost(t, st, "wk-1")
	out := noteHost(t, st, "web-1")
	if err := st.AssignHostsGroup([]string{cp.ID}, k8s.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignHostsGroup([]string{wk.ID}, workers.ID); err != nil {
		t.Fatal(err)
	}
	return st, k8s, workers, cp, wk, out
}

// A role is one lower-case word for a host in the group (its folder or one
// below); it is kept in keel.json and survives a reload, a rename and a
// move of the group.
func TestGroupRolesSetAndKept(t *testing.T) {
	st, k8s, _, cp, wk, out := roleFixture(t)
	if _, err := st.SetGroupRole(k8s.ID, cp.ID, " Control-Plane "); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetGroupRole(k8s.ID, wk.ID, "worker"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"two words", "-x", "w;rm", "x\ny", "ROLE$", "a234567890123456789012345678901234"} {
		if _, err := st.SetGroupRole(k8s.ID, cp.ID, bad); !errors.Is(err, ErrRole) {
			t.Fatalf("role %q: %v", bad, err)
		}
	}
	if _, err := st.SetGroupRole(k8s.ID, out.ID, "worker"); !errors.Is(err, ErrRole) {
		t.Fatalf("a host outside the group: %v", err)
	}
	if _, err := st.SetGroupRole("nope", cp.ID, "worker"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
	g, members, err := st.GroupMembers(k8s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].Name != "cp-1" || members[0].Role != "control-plane" || members[1].Role != "worker" || members[1].Folder != "workers" {
		t.Fatalf("members %+v", members)
	}
	if len(g.Roles) != 2 {
		t.Fatalf("roles %+v", g.Roles)
	}
	// A rename from the folder form keeps the roles, whatever it sends.
	if _, err := st.UpsertGroup(Group{ID: k8s.ID, Name: "k8s", Roles: map[string]string{cp.ID: "evil"}}); err != nil {
		t.Fatal(err)
	}
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	_, members, _ = st2.GroupMembers(k8s.ID)
	if members[0].Role != "control-plane" {
		t.Fatalf("after rename and reload %+v", members)
	}
	js, _ := os.ReadFile(filepath.Join(filepath.Dir(st.path), "keel.json"))
	if !bytes.Contains(js, []byte(`"control-plane"`)) {
		t.Fatal("roles not in keel.json")
	}
	// A new group never takes roles from the client.
	ng, _ := st.UpsertGroup(Group{Name: "fresh", Roles: map[string]string{cp.ID: "x"}})
	if len(ng.Roles) != 0 {
		t.Fatalf("new group roles %+v", ng.Roles)
	}
	// Removing a role.
	if _, err := st.SetGroupRole(k8s.ID, wk.ID, ""); err != nil {
		t.Fatal(err)
	}
	_, members, _ = st.GroupMembers(k8s.ID)
	if members[1].Role != "" {
		t.Fatalf("role not removed %+v", members[1])
	}
}

// A host that moves out of the group loses its role there (not shown, not
// used); ListGroups shows only members' roles.
func TestGroupRoleOfAHostThatLeft(t *testing.T) {
	st, k8s, _, cp, _, _ := roleFixture(t)
	if _, err := st.SetGroupRole(k8s.ID, cp.ID, "etcd"); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignHostsGroup([]string{cp.ID}, ""); err != nil {
		t.Fatal(err)
	}
	_, members, _ := st.GroupMembers(k8s.ID)
	for _, m := range members {
		if m.HostID == cp.ID {
			t.Fatal("a host that left is still a member")
		}
	}
	groups, _ := st.ListGroups()
	for _, g := range groups {
		if g.ID == k8s.ID && len(g.Roles) != 0 {
			t.Fatalf("roles of a host that left: %+v", g.Roles)
		}
	}
}

// At most MaxGroupRoles distinct roles in a group.
func TestGroupRolesLimit(t *testing.T) {
	st := testStore(t)
	g, _ := st.UpsertGroup(Group{Name: "big"})
	var ids []string
	for i := 0; i <= MaxGroupRoles; i++ {
		h := noteHost(t, st, fmt.Sprintf("h-%d", i))
		ids = append(ids, h.ID)
	}
	if err := st.AssignHostsGroup(ids, g.ID); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		_, err := st.SetGroupRole(g.ID, id, fmt.Sprintf("r%d", i))
		if i < MaxGroupRoles && err != nil {
			t.Fatal(err)
		}
		if i == MaxGroupRoles && !errors.Is(err, ErrRole) {
			t.Fatalf("role %d: %v", i, err)
		}
	}
}

// Group notes live in keel.memory beside the others, count apart from the
// user's own notes, need the group to exist, and go with the group.
func TestGroupNotes(t *testing.T) {
	st, k8s, workers, cp, _, _ := roleFixture(t)
	if _, err := st.AddAgentNote(AgentNote{GroupID: "nope", Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
	if _, err := st.AddAgentNote(AgentNote{GroupID: k8s.ID, HostID: cp.ID, Text: "x"}); !errors.Is(err, ErrAgentNote) {
		t.Fatalf("both a host and a group: %v", err)
	}
	for i := 0; i < MaxGroupNotes; i++ {
		if _, err := st.AddAgentNote(AgentNote{GroupID: k8s.ID, Text: fmt.Sprintf("group note %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddAgentNote(AgentNote{GroupID: k8s.ID, Text: "one more"}); !errors.Is(err, ErrNotesFull) {
		t.Fatalf("over the limit: %v", err)
	}
	// The user's own notes and another group's are not affected.
	if _, err := st.AddAgentNote(AgentNote{Text: "mine"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddAgentNote(AgentNote{GroupID: workers.ID, Text: "workers note"}); err != nil {
		t.Fatal(err)
	}
	blob, _ := os.ReadFile(filepath.Join(filepath.Dir(st.path), "keel.memory"))
	if bytes.Contains(blob, []byte("group note")) {
		t.Fatal("keel.memory holds a group note in the clear")
	}
	notes, _ := st.ListAgentNotes()
	if notes[0].Text != "mine" {
		t.Fatalf("user notes not first: %+v", notes[0])
	}
	if err := st.DeleteGroup(k8s.ID); err != nil {
		t.Fatal(err)
	}
	notes, _ = st.ListAgentNotes()
	for _, n := range notes {
		if n.GroupID == k8s.ID {
			t.Fatal("a deleted group's notes stayed")
		}
	}
	if len(notes) != 2 {
		t.Fatalf("notes left %d", len(notes))
	}
}

// A runbook step's role is one lower-case word; OneHost is kept.
func TestRunbookStepRole(t *testing.T) {
	st := testStore(t)
	b, err := st.AddRunbook(Runbook{Name: "r", Steps: []RunbookStep{{Title: "a", Command: "echo", Role: " Worker ", OneHost: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Steps[0].Role != "worker" || !b.Steps[0].OneHost {
		t.Fatalf("%+v", b.Steps[0])
	}
	if _, err := st.AddRunbook(Runbook{Name: "r", Steps: []RunbookStep{{Title: "a", Role: "two words"}}}); !errors.Is(err, ErrRunbook) {
		t.Fatalf("bad role: %v", err)
	}
}
