package store

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Host roles in a group (docs/ui-redesign.md §17, board GroupRoles): what
// each host in a folder is ("control-plane", "worker", "osd", "primary").
// A runbook step can target a role, and a group run proposes it for every
// host in the group with that role (service/grouprun.go).
//
// Roles are kept on the group in keel.json (Group.Roles, host id → role).
// They are config, like a host's tags: no secret, and the Ansible export of
// the next phase builds its inventory groups from them. A host belongs to a
// group when its folder is the group or a folder below it; a role kept for
// a host that has since moved out is not shown or used (GroupMembers), and
// comes back if the host moves back in.

const (
	// MaxGroupRoles bounds the distinct roles of one group.
	MaxGroupRoles = 16
	// MaxRoleBytes is the longest role name.
	MaxRoleBytes = 32
)

var (
	// ErrRole is a role name that is not one lower-case word (letters,
	// digits, "-", "_" or "."), or a host that is not in the group.
	ErrRole = errors.New("invalid role")
	roleRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)
)

// CleanRole lower-cases and trims a role name and reports whether it is
// valid. Empty is valid: no role.
func CleanRole(r string) (string, bool) {
	r = strings.ToLower(strings.TrimSpace(r))
	if r == "" {
		return "", true
	}
	return r, roleRe.MatchString(r)
}

// groupSubtreeLocked is id and every group below it.
func (s *Store) groupSubtreeLocked(id string) map[string]bool {
	in := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, g := range s.data.Groups {
			if !in[g.ID] && in[g.ParentID] {
				in[g.ID] = true
				changed = true
			}
		}
	}
	return in
}

// GroupMember is a host in a group, with its role there.
type GroupMember struct {
	HostID string `json:"hostId"`
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	// Folder is the host's own folder (the group itself or one below it).
	Folder string `json:"folder,omitempty"`
}

// GroupMembers returns the hosts in a group (its folder and every folder
// below it), by name, with their roles there.
func (s *Store) GroupMembers(groupID string) (Group, []GroupMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Group{}, nil, ErrLocked
	}
	s.migrateGroupsLocked()
	g, ok := s.groupByIDLocked(groupID)
	if !ok {
		return Group{}, nil, ErrNotFound
	}
	tree := s.groupSubtreeLocked(g.ID)
	var out []GroupMember
	for _, h := range s.data.Hosts {
		if !tree[h.Group] {
			continue
		}
		folder := ""
		if f, ok := s.groupByIDLocked(h.Group); ok {
			folder = f.Name
		}
		out = append(out, GroupMember{HostID: h.ID, Name: h.Name, Role: g.Roles[h.ID], Folder: folder})
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return copyGroup(g, tree, s.data.Hosts), out, nil
}

// copyGroup copies g with only the roles of hosts in its subtree.
func copyGroup(g Group, tree map[string]bool, hosts []persistHost) Group {
	g.Vars = append([]GroupVar(nil), g.Vars...)
	if len(g.Vars) == 0 {
		g.Vars = nil
	}
	if len(g.Roles) == 0 {
		g.Roles = nil
		return g
	}
	roles := map[string]string{}
	for _, h := range hosts {
		if r, ok := g.Roles[h.ID]; ok && tree[h.Group] {
			roles[h.ID] = r
		}
	}
	g.Roles = roles
	if len(roles) == 0 {
		g.Roles = nil
	}
	return g
}

// SetGroupRole gives a host in the group a role there; an empty role
// removes it. The host must be in the group (its folder or one below).
func (s *Store) SetGroupRole(groupID, hostID, role string) (Group, error) {
	role, ok := CleanRole(role)
	if !ok {
		return Group{}, fmt.Errorf("%w: a role is one lower-case word of up to %d letters, digits, \"-\", \"_\" or \".\" (such as worker)", ErrRole, MaxRoleBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Group{}, ErrLocked
	}
	s.migrateGroupsLocked()
	idx := -1
	for i, g := range s.data.Groups {
		if g.ID == groupID {
			idx = i
		}
	}
	if idx < 0 {
		return Group{}, ErrNotFound
	}
	tree := s.groupSubtreeLocked(groupID)
	member := false
	for _, h := range s.data.Hosts {
		if h.ID == hostID && tree[h.Group] {
			member = true
		}
	}
	if !member {
		return Group{}, fmt.Errorf("%w: that host is not in this group", ErrRole)
	}
	g := s.data.Groups[idx]
	roles := map[string]string{}
	for _, h := range s.data.Hosts {
		if r, ok := g.Roles[h.ID]; ok && tree[h.Group] {
			roles[h.ID] = r
		}
	}
	if role == "" {
		delete(roles, hostID)
	} else {
		roles[hostID] = role
	}
	distinct := map[string]bool{}
	for _, r := range roles {
		distinct[r] = true
	}
	if len(distinct) > MaxGroupRoles {
		return Group{}, fmt.Errorf("%w: at most %d roles in a group", ErrRole, MaxGroupRoles)
	}
	if len(roles) == 0 {
		roles = nil
	}
	g.Roles, g.UpdatedAt = roles, time.Now().UTC()
	s.data.Groups[idx] = g
	if err := s.saveLocked(); err != nil {
		return Group{}, err
	}
	return copyGroup(g, tree, s.data.Hosts), nil
}

// GroupChain returns a host's folder and the folders above it, nearest
// first. It stops at a cycle.
func GroupChain(groups []Group, groupID string) []Group {
	var out []Group
	seen := map[string]bool{}
	for cur := groupID; cur != "" && !seen[cur]; {
		seen[cur] = true
		found := false
		for _, g := range groups {
			if g.ID == cur {
				out = append(out, g)
				cur, found = g.ParentID, true
				break
			}
		}
		if !found {
			break
		}
	}
	return out
}
