package native

import (
	"context"
	"fmt"
	"strings"

	"keel/internal/native/keelclient"
)

// The folders half of fakeBackend: what the window asks of keel serve about folders, recorded, and answered as the test says.

type fakeFolders struct {
	reqs []string // "post name parent", "put id name parent", "delete id", "move ids -> dest"
	err  error    // what a write answers
	seq  int
}

func (f *fakeBackend) SaveGroup(_ context.Context, g keelclient.Group) (keelclient.Group, error) {
	if f.fold.err != nil {
		return keelclient.Group{}, f.fold.err
	}
	if g.ID == "" {
		f.fold.seq++
		g.ID = fmt.Sprintf("g-new%d", f.fold.seq)
		f.fold.reqs = append(f.fold.reqs, "post "+g.Name+" "+g.ParentID)
		f.groups = append(f.groups, g)
		return g, nil
	}
	f.fold.reqs = append(f.fold.reqs, "put "+g.ID+" "+g.Name+" "+g.ParentID)
	for i := range f.groups {
		if f.groups[i].ID == g.ID {
			g.Roles, g.Vars = f.groups[i].Roles, f.groups[i].Vars
			f.groups[i] = g
		}
	}
	return g, nil
}

// DeleteGroup moves the folder's hosts and subfolders up one level, as store.DeleteGroup does.
func (f *fakeBackend) DeleteGroup(_ context.Context, id string) error {
	if f.fold.err != nil {
		return f.fold.err
	}
	f.fold.reqs = append(f.fold.reqs, "delete "+id)
	parent := ""
	var keep []keelclient.Group
	for _, g := range f.groups {
		if g.ID == id {
			parent = g.ParentID
		}
	}
	for _, g := range f.groups {
		if g.ID == id {
			continue
		}
		if g.ParentID == id {
			g.ParentID = parent
		}
		keep = append(keep, g)
	}
	f.groups = keep
	for i := range f.hosts {
		if f.hosts[i].GroupID == id {
			f.hosts[i].GroupID = parent
			f.hosts[i].Group = f.groupName(parent)
		}
	}
	return nil
}

func (f *fakeBackend) groupName(id string) string {
	for _, g := range f.groups {
		if g.ID == id {
			return g.Name
		}
	}
	return ""
}

func (f *fakeBackend) MoveHosts(_ context.Context, ids []string, groupID string) error {
	if f.fold.err != nil {
		return f.fold.err
	}
	f.fold.reqs = append(f.fold.reqs, "move "+strings.Join(ids, ",")+" -> "+groupID)
	for i := range f.hosts {
		for _, id := range ids {
			if f.hosts[i].ID == id {
				f.hosts[i].GroupID, f.hosts[i].Group = groupID, f.groupName(groupID)
			}
		}
	}
	return nil
}

// GroupMembers lists the hosts in the folder and below it, by name, with the roles the folder keeps.
func (f *fakeBackend) GroupMembers(_ context.Context, id string) (keelclient.GroupInfo, error) {
	f.rec("members " + id)
	g, ok := groupByID(f.groups, id)
	if !ok {
		return keelclient.GroupInfo{}, &keelclient.StatusError{Code: 404, Msg: "not found"}
	}
	var members []keelclient.GroupMember
	for _, h := range byHostName(hostsInSubtree(f.groups, f.hosts, id, "")) {
		members = append(members, keelclient.GroupMember{HostID: h.ID, Name: h.Name, Role: g.Roles[h.ID], Folder: f.groupName(h.GroupID)})
	}
	return keelclient.GroupInfo{Group: g, Members: members}, nil
}
