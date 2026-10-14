package store

import "strings"

// ProductionGroupName is the group name that turns "Treat as production"
// on. Only the host's own (direct parent) group counts, compared without
// case and surrounding spaces: a host in production/db is not affected by
// the outer "production" group. The switch turns on at two moments only,
// when a host is created in that group or moved into it (AssignHostsGroup,
// or an update that changes its group). Nothing else changes it: loading
// keel.json, renaming a group, deleting a group (its hosts go to the parent)
// or saving an unchanged host leave it as it is, and the user can turn it
// off again while the host stays in the group.
const ProductionGroupName = "production"

func (s *Store) isProductionGroupLocked(groupID string) bool {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return false
	}
	g, ok := s.groupByIDLocked(groupID)
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(g.Name), ProductionGroupName)
}

// applyCreateDefaultsLocked fills the agent switches a new host did not
// send: readable by the agent, and production when it is created in a group
// named production. A value the caller sent wins.
func (s *Store) applyCreateDefaultsLocked(h *Host) {
	if h.AgentReadable == nil {
		h.AgentReadable = boolPtr(true)
	}
	if h.Production == nil {
		h.Production = boolPtr(s.isProductionGroupLocked(h.GroupID))
	}
}

// applyUpdateDefaultsLocked keeps the saved agent switches when an update
// does not send them (older clients), and turns production on when the
// update moves the host into a production group without sending it.
func (s *Store) applyUpdateDefaultsLocked(h *Host, existing persistHost) {
	if h.AgentReadable == nil {
		h.AgentReadable = boolPtr(!existing.AgentOff)
	}
	if h.Production == nil {
		prod := existing.Production
		if strings.TrimSpace(existing.Group) != h.GroupID && s.isProductionGroupLocked(h.GroupID) {
			prod = true
		}
		h.Production = boolPtr(prod)
	}
}
