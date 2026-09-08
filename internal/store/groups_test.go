package store

import (
	"encoding/json"
	"errors"
	"os"
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

	st2, err := New(st.path)
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

func TestEnsureGroupsDedupesLegacyNames(t *testing.T) {
	st := testStore(t)
	st.mu.Lock()
	st.data.Hosts = append(st.data.Hosts,
		persistHost{ID: "h1", Name: "a", Group: "prod", Address: "10.0.0.1", Username: "u", AuthType: "password"},
		persistHost{ID: "h2", Name: "b", Group: "prod", Address: "10.0.0.2", Username: "u", AuthType: "password"},
	)
	st.mu.Unlock()

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
