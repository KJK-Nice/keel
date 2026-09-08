package store

import (
	"strings"
	"time"
)

func (s *Store) ListGroups() ([]Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return nil, ErrLocked
	}
	if err := s.ensureGroupsLocked(); err != nil {
		return nil, err
	}
	out := make([]Group, len(s.data.Groups))
	copy(out, s.data.Groups)
	return out, nil
}

func (s *Store) UpsertGroup(g Group) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return Group{}, ErrLocked
	}
	now := time.Now().UTC()
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		g.Name = "Folder"
	}
	g.ParentID = strings.TrimSpace(g.ParentID)
	if err := s.ensureGroupsLocked(); err != nil {
		return Group{}, err
	}
	if err := s.validateNestLocked(g.ID, g.ParentID); err != nil {
		return Group{}, err
	}
	if g.ID == "" {
		g.ID = newID()
		g.CreatedAt = now
		g.UpdatedAt = now
		s.data.Groups = append(s.data.Groups, g)
		return g, s.saveLocked()
	}
	for i, existing := range s.data.Groups {
		if existing.ID == g.ID {
			g.CreatedAt = existing.CreatedAt
			g.UpdatedAt = now
			s.data.Groups[i] = g
			return g, s.saveLocked()
		}
	}
	g.CreatedAt = now
	g.UpdatedAt = now
	s.data.Groups = append(s.data.Groups, g)
	return g, s.saveLocked()
}

func (s *Store) DeleteGroup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	id = strings.TrimSpace(id)
	var parent string
	found := false
	kept := s.data.Groups[:0]
	for _, g := range s.data.Groups {
		if g.ID == id {
			found = true
			parent = g.ParentID
			continue
		}
		kept = append(kept, g)
	}
	if !found {
		return ErrNotFound
	}
	s.data.Groups = kept
	for i, g := range s.data.Groups {
		if g.ParentID == id {
			s.data.Groups[i].ParentID = parent
		}
	}
	for i, h := range s.data.Hosts {
		if h.Group == id {
			s.data.Hosts[i].Group = parent
		}
	}
	return s.saveLocked()
}

func (s *Store) hydrateGroupLocked(h Host, persistGroup string) Host {
	persistGroup = strings.TrimSpace(persistGroup)
	if persistGroup == "" {
		h.Group = ""
		h.GroupID = ""
		return h
	}
	if g, ok := s.groupByIDLocked(persistGroup); ok {
		h.GroupID = g.ID
		h.Group = g.Name
		return h
	}
	h.Group = persistGroup
	h.GroupID = ""
	return h
}

func (s *Store) resolveGroupRefLocked(groupID, groupName string) (string, error) {
	groupID = strings.TrimSpace(groupID)
	groupName = strings.TrimSpace(groupName)
	if groupID != "" {
		if _, ok := s.groupByIDLocked(groupID); ok {
			return groupID, nil
		}
	}
	if groupName == "" {
		return "", nil
	}
	if _, ok := s.groupByIDLocked(groupName); ok {
		return groupName, nil
	}
	for _, g := range s.data.Groups {
		if strings.EqualFold(g.Name, groupName) {
			return g.ID, nil
		}
	}
	now := time.Now().UTC()
	g := Group{ID: newID(), Name: groupName, CreatedAt: now, UpdatedAt: now}
	s.data.Groups = append(s.data.Groups, g)
	return g.ID, nil
}

func (s *Store) ensureGroupsLocked() error {
	if s.data.Groups == nil {
		s.data.Groups = []Group{}
	}
	byID := map[string]struct{}{}
	byName := map[string]string{}
	for _, g := range s.data.Groups {
		byID[g.ID] = struct{}{}
		key := strings.ToLower(strings.TrimSpace(g.Name))
		if key != "" {
			if _, ok := byName[key]; !ok {
				byName[key] = g.ID
			}
		}
	}
	dirty := false
	now := time.Now().UTC()
	for i, h := range s.data.Hosts {
		name := strings.TrimSpace(h.Group)
		if name == "" {
			continue
		}
		if _, ok := byID[name]; ok {
			continue
		}
		key := strings.ToLower(name)
		if id, ok := byName[key]; ok {
			s.data.Hosts[i].Group = id
			dirty = true
			continue
		}
		id := newID()
		s.data.Groups = append(s.data.Groups, Group{
			ID: id, Name: name, CreatedAt: now, UpdatedAt: now,
		})
		byID[id] = struct{}{}
		byName[key] = id
		s.data.Hosts[i].Group = id
		dirty = true
	}
	if !dirty {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) groupByIDLocked(id string) (Group, bool) {
	for _, g := range s.data.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

func (s *Store) validateNestLocked(id, parentID string) error {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil
	}
	if id != "" && parentID == id {
		return ErrGroupCycle
	}
	if _, ok := s.groupByIDLocked(parentID); !ok {
		return ErrNotFound
	}
	seen := map[string]struct{}{}
	if id != "" {
		seen[id] = struct{}{}
	}
	cur := parentID
	depth := 1
	for cur != "" {
		if _, ok := seen[cur]; ok {
			return ErrGroupCycle
		}
		seen[cur] = struct{}{}
		g, ok := s.groupByIDLocked(cur)
		if !ok {
			break
		}
		depth++
		if depth > MaxGroupDepth {
			return ErrGroupDepth
		}
		cur = g.ParentID
	}
	if id != "" {
		depth += s.descendantDepthLocked(id)
		if depth > MaxGroupDepth {
			return ErrGroupDepth
		}
	}
	return nil
}

// AssignHostsGroup sets Parent Group for one or more hosts in a single save.
// Empty groupID ungroups. Does not touch PTY, inject, or host secrets.
func (s *Store) AssignHostsGroup(ids []string, groupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	if err := s.ensureGroupsLocked(); err != nil {
		return err
	}
	groupID = strings.TrimSpace(groupID)
	if groupID != "" {
		if _, ok := s.groupByIDLocked(groupID); !ok {
			return ErrNotFound
		}
	}
	want := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	if len(want) == 0 {
		return nil
	}
	now := time.Now().UTC()
	found := 0
	for i, h := range s.data.Hosts {
		if _, ok := want[h.ID]; !ok {
			continue
		}
		s.data.Hosts[i].Group = groupID
		s.data.Hosts[i].UpdatedAt = now
		found++
	}
	if found == 0 {
		return ErrNotFound
	}
	return s.saveLocked()
}

func (s *Store) descendantDepthLocked(id string) int {
	max := 0
	for _, g := range s.data.Groups {
		if g.ParentID != id {
			continue
		}
		d := 1 + s.descendantDepthLocked(g.ID)
		if d > max {
			max = d
		}
	}
	return max
}
