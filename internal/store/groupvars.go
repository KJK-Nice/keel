package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"keel/internal/runvars"
)

// Group variables (docs/ui-redesign.md §17, groups part 2, board
// GroupRoles): name = value pairs on a group, such as k8s_version = 1.31.2
// or api_vip = 10.20.0.10. A runbook step uses one as {{name}}; keel serve
// fills it in only when the user presses Type in a group run, after the
// approve dialog showed the filled bytes (service/groupvars.go). The
// Ansible export writes them as the group's vars.
//
// They are config, like roles: kept on the group in keel.json, never a
// secret. The agent service refuses, before this is called, a value that
// looks like a secret or holds a vault value (service.SetGroupVar); the
// store checks the shape only. Only SetGroupVar changes them; a folder
// form's save keeps them.

const (
	// MaxGroupVars bounds the variables of one group.
	MaxGroupVars = 32
)

// ErrGroupVar is a variable name or value that is not allowed.
var ErrGroupVar = errors.New("invalid group variable")

// GroupVar is one variable.
type GroupVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CleanGroupVar trims and checks a name and value. An empty value is
// valid: it removes the variable.
func CleanGroupVar(name, value string) (string, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !runvars.ValidName(name) {
		return "", "", fmt.Errorf("%w: a name is lower-case letters, digits and _, up to %d, not starting with a digit (such as api_vip)", ErrGroupVar, runvars.MaxNameBytes)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return name, "", nil
	}
	if err := runvars.CheckValue(value, runvars.MaxGroupValueBytes); err != nil {
		return "", "", fmt.Errorf("%w: %s's value %s", ErrGroupVar, name, strings.TrimPrefix(err.Error(), runvars.ErrUnsafe.Error()+": "))
	}
	return name, value, nil
}

// SetGroupVar sets a group's variable; an empty value removes it. A new
// name goes at the end, a known one keeps its place.
func (s *Store) SetGroupVar(groupID, name, value string) (Group, error) {
	name, value, err := CleanGroupVar(name, value)
	if err != nil {
		return Group{}, err
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
	g := s.data.Groups[idx]
	vars := make([]GroupVar, 0, len(g.Vars)+1)
	found := false
	for _, v := range g.Vars {
		if v.Name == name {
			found = true
			if value == "" {
				continue
			}
			v.Value = value
		}
		vars = append(vars, v)
	}
	if !found && value != "" {
		if len(vars) >= MaxGroupVars {
			return Group{}, fmt.Errorf("%w: at most %d variables in a group", ErrGroupVar, MaxGroupVars)
		}
		vars = append(vars, GroupVar{Name: name, Value: value})
	}
	if len(vars) == 0 {
		vars = nil
	}
	g.Vars, g.UpdatedAt = vars, time.Now().UTC()
	s.data.Groups[idx] = g
	if err := s.saveLocked(); err != nil {
		return Group{}, err
	}
	return copyGroup(g, s.groupSubtreeLocked(groupID), s.data.Hosts), nil
}

// AllGroupVars returns every group's variables, by group id. The agent
// service redacts their values out of anything sent to the model.
func (s *Store) AllGroupVars() (map[string][]GroupVar, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	out := map[string][]GroupVar{}
	for _, g := range s.data.Groups {
		if len(g.Vars) > 0 {
			out[g.ID] = append([]GroupVar(nil), g.Vars...)
		}
	}
	return out, nil
}
