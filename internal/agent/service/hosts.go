package service

import "keel/internal/agent"

// hostSet is the saved hosts as the agent sees them (no credentials), read
// once for a request: the list, each by id, and what the agent may read.
type hostSet struct {
	list  []agent.Host
	byID  map[string]agent.Host
	scope agent.Scope
}

func (s *Service) hosts() (hostSet, error) {
	saved, err := s.st.ListHosts()
	if err != nil {
		return hostSet{}, err
	}
	list := agent.HostsFrom(saved)
	byID := make(map[string]agent.Host, len(list))
	for _, h := range list {
		byID[h.ID] = h
	}
	return hostSet{list: list, byID: byID, scope: agent.NewScope(list)}, nil
}
