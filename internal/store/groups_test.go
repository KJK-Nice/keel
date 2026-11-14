package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroupNestPersistAndReload(t *testing.T) {
	st := testStore(t)
	root, err := st.UpsertGroup(Group{Name: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if root.ParentID != "" {
		t.Fatalf("root parent = %q", root.ParentID)
	}
	child, err := st.UpsertGroup(Group{Name: "api", ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID != root.ID {
		t.Fatalf("child parent = %q", child.ParentID)
	}
	host, err := st.UpsertHost(Host{
		Name: "edge", GroupID: child.ID, Address: "10.0.0.2", Username: "ubuntu", AuthType: "password", Password: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if host.Group != "api" || host.GroupID != child.ID {
		t.Fatalf("host group view = %+v", host)
	}

	st2, err := New(st.path, TestVault())
	if err != nil {
		t.Fatal(err)
	}
	groups, err := st2.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	var gotRoot, gotChild Group
	for _, g := range groups {
		switch g.ID {
		case root.ID:
			gotRoot = g
		case child.ID:
			gotChild = g
		}
	}
	if gotRoot.Name != "prod" || gotRoot.ParentID != "" {
		t.Fatalf("reloaded root = %+v", gotRoot)
	}
	if gotChild.Name != "api" || gotChild.ParentID != root.ID {
		t.Fatalf("reloaded child = %+v", gotChild)
	}
	listed, err := st2.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	var view Host
	for _, h := range listed {
		if h.ID == host.ID {
			view = h
		}
	}
	if view.Group != "api" || view.GroupID != child.ID {
		t.Fatalf("reloaded host group = %+v", view)
	}
}

func TestGroupCycleRejected(t *testing.T) {
	st := testStore(t)
	a, err := st.UpsertGroup(Group{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.UpsertGroup(Group{Name: "b", ParentID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertGroup(Group{ID: a.ID, Name: "a", ParentID: b.ID}); !errors.Is(err, ErrGroupCycle) {
		t.Fatalf("A under B: %v", err)
	}
	if _, err := st.UpsertGroup(Group{ID: a.ID, Name: "a", ParentID: a.ID}); !errors.Is(err, ErrGroupCycle) {
		t.Fatalf("self parent: %v", err)
	}
}

func TestGroupDepthLimit(t *testing.T) {
	st := testStore(t)
	parent := ""
	var last Group
	for i := 0; i < MaxGroupDepth; i++ {
		g, err := st.UpsertGroup(Group{Name: strings.Repeat("d", i+1), ParentID: parent})
		if err != nil {
			t.Fatalf("depth %d: %v", i+1, err)
		}
		parent = g.ID
		last = g
	}
	if _, err := st.UpsertGroup(Group{Name: "too-deep", ParentID: last.ID}); !errors.Is(err, ErrGroupDepth) {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestDeleteGroupReparents(t *testing.T) {
	st := testStore(t)
	root, err := st.UpsertGroup(Group{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	mid, err := st.UpsertGroup(Group{Name: "prod", ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := st.UpsertGroup(Group{Name: "api", ParentID: mid.ID})
	if err != nil {
		t.Fatal(err)
	}
	host, err := st.UpsertHost(Host{
		Name: "box", GroupID: mid.ID, Address: "10.0.0.3", Username: "ubuntu", AuthType: "password", Password: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteGroup(mid.ID); err != nil {
		t.Fatal(err)
	}
	groups, err := st.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.ID == mid.ID {
			t.Fatal("deleted group still listed")
		}
		if g.ID == leaf.ID && g.ParentID != root.ID {
			t.Fatalf("leaf parent = %q, want root", g.ParentID)
		}
	}
	got, err := st.GetHost(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupID != root.ID || got.Group != "work" {
		t.Fatalf("host after delete = %+v", got)
	}
}

// legacyGroupsFile is a keel.json from before groups had ids: a host's
// group is its folder's name.
const legacyGroupsFile = `{
  "hosts": [
    {"id":"h1","name":"a","group":"prod","address":"10.0.0.1","port":22,"username":"u","authType":"password"},
    {"id":"h2","name":"b","group":"prod","address":"10.0.0.2","port":22,"username":"u","authType":"password"}
  ],
  "knownHosts": {}
}`

func legacyGroupsStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keel.json")
	if err := os.WriteFile(path, []byte(legacyGroupsFile), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	st, err := New(path, TestVault())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// A read never writes: an old file is brought up to date when it is
// loaded, so listing hosts, groups or a group's members leaves keel.json
// as it is and tells no listener of a change.
func TestReadsOfAMigratedStoreDoNotWrite(t *testing.T) {
	st := legacyGroupsStore(t)
	before, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), `"groups"`) {
		t.Fatalf("the migration was not saved at load:\n%s", before)
	}
	changes := 0
	st.OnChange(func() { changes++ })
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	groups, err := st.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || hosts[0].GroupID != groups[0].ID {
		t.Fatalf("groups = %+v, host group id %q", groups, hosts[0].GroupID)
	}
	if _, _, err := st.GroupMembers(groups[0].ID); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || changes != 0 {
		t.Fatalf("a read wrote: file changed %v, %d change notices", string(after) != string(before), changes)
	}
}

// An old file loaded while the vault is locked is saved, migrated, by the
// unlock: its groups keep their ids over a restart.
func TestLegacyGroupsSavedAtUnlock(t *testing.T) {
	st := testStore(t)
	h, err := st.UpsertHost(Host{Name: "a", Address: "10.0.0.1", Username: "u", AuthType: "password"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	var old persistData
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	for i := range old.Hosts {
		if old.Hosts[i].ID == h.ID {
			old.Hosts[i].Group = "legacy"
		}
	}
	raw, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	locked, err := New(st.path, TestVault())
	if err != nil {
		t.Fatal(err)
	}
	if !locked.Locked() {
		t.Fatal("expected a locked store")
	}
	if err := locked.Unlock("test-vault-pass"); err != nil {
		t.Fatal(err)
	}
	got, err := locked.GetHost(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Group != "legacy" || got.GroupID == "" {
		t.Fatalf("host group = %q id %q", got.Group, got.GroupID)
	}
	raw, err = os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	var saved persistData
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	for _, sh := range saved.Hosts {
		if sh.ID == h.ID && sh.Group != got.GroupID {
			t.Fatalf("saved host group = %q, want the id %q", sh.Group, got.GroupID)
		}
	}
}

func TestEnsureGroupsDedupesLegacyNames(t *testing.T) {
	st := legacyGroupsStore(t)

	listed, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	var a, b Host
	for _, h := range listed {
		switch h.ID {
		case "h1":
			a = h
		case "h2":
			b = h
		}
	}
	if a.Group != "prod" || b.Group != "prod" {
		t.Fatalf("legacy names should still display as prod: %+v %+v", a, b)
	}
	if a.GroupID == "" || a.GroupID != b.GroupID {
		t.Fatalf("hosts in the same legacy folder should share a group id: %q %q", a.GroupID, b.GroupID)
	}
	groups, err := st.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range groups {
		if strings.EqualFold(g.Name, "prod") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected one prod group, got %d in %+v", n, groups)
	}

	raw, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	var dump persistData
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatal(err)
	}
	for _, h := range dump.Hosts {
		if h.ID == "h1" || h.ID == "h2" {
			if h.Group != a.GroupID {
				t.Fatalf("persisted host %s group = %q, want id %q", h.ID, h.Group, a.GroupID)
			}
		}
	}
}

func TestHostGroupNameStillCreatesRoot(t *testing.T) {
	st := testStore(t)
	saved, err := st.UpsertHost(Host{
		Name: "api", Group: "prod", Address: "10.0.0.8", Username: "ubuntu", AuthType: "password", Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Group != "prod" || saved.GroupID == "" {
		t.Fatalf("upsert view = %+v", saved)
	}
	groups, err := st.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range groups {
		if g.ID == saved.GroupID && g.Name == "prod" && g.ParentID == "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing root prod group in %+v", groups)
	}
}

func TestAssignHostsGroupBatch(t *testing.T) {
	st := testStore(t)
	g, err := st.UpsertGroup(Group{Name: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.UpsertHost(Host{Name: "a", Address: "10.0.0.1", Username: "u", AuthType: "password", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.UpsertHost(Host{Name: "b", Address: "10.0.0.2", Username: "u", AuthType: "password", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AssignHostsGroup([]string{a.ID, b.ID}, g.ID); err != nil {
		t.Fatal(err)
	}
	gotA, _ := st.GetHost(a.ID)
	gotB, _ := st.GetHost(b.ID)
	if gotA.GroupID != g.ID || gotB.GroupID != g.ID {
		t.Fatalf("batch move: %+v %+v", gotA, gotB)
	}
	if err := st.AssignHostsGroup([]string{a.ID}, ""); err != nil {
		t.Fatal(err)
	}
	gotA, _ = st.GetHost(a.ID)
	if gotA.GroupID != "" {
		t.Fatalf("ungroup = %+v", gotA)
	}
}

func TestGroupMissingParent(t *testing.T) {
	st := testStore(t)
	if _, err := st.UpsertGroup(Group{Name: "orphan", ParentID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing parent: %v", err)
	}
}

func TestMaxGroupDepthDocumented(t *testing.T) {
	if MaxGroupDepth != 8 {
		t.Fatalf("MaxGroupDepth = %d", MaxGroupDepth)
	}
	if !strings.Contains(ErrGroupDepth.Error(), "8") {
		t.Fatalf("depth error should mention the cap: %v", ErrGroupDepth)
	}
}
