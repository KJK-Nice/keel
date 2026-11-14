package service

// A card is something the agent proposed in a conversation that the user
// acts on: a note, a runbook, a notebook change. The three kinds share one
// lifecycle: an action claims the card under the lock (so two presses, or
// two windows, make one change), does its work without the lock, then
// either releases the card or finishes it with a new status.

// cardKind is what differs between the kinds. P is the card held in memory,
// V the copy an action works on and returns.
type cardKind[P, V any] struct {
	find    func(c *conversation, id string) *P   // the card held in memory
	saved   func(c *conversation, id string) bool // the card is in the saved record
	busy    func(p *P) *bool
	view    func(p *P) V
	expired error // the card is only in the saved record: keel serve no longer holds it
	closed  error // the card is busy, or not in a state the action takes
	// finishGone: finish records the status and saves the conversation even
	// when the conversation or the card was dropped while the action ran.
	// Without it finish does nothing then and says so.
	finishGone bool
}

// claim marks a card busy if ok(it) holds and returns it with a copy.
func (k cardKind[P, V]) claim(s *Service, convID, id string, ok func(*P) bool) (*conversation, *P, V, error) {
	var none V
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convs[convID]
	if c == nil {
		return nil, nil, none, ErrNotFound
	}
	p := k.find(c, id)
	if p == nil {
		if k.saved(c, id) {
			return nil, nil, none, k.expired
		}
		return nil, nil, none, ErrNotFound
	}
	busy := k.busy(p)
	if *busy || !ok(p) {
		return nil, nil, none, k.closed
	}
	*busy = true
	return c, p, k.view(p), nil
}

func (k cardKind[P, V]) release(s *Service, p *P) {
	s.mu.Lock()
	*k.busy(p) = false
	s.mu.Unlock()
}

// finish runs set under the lock (the card's new status, on the card and on
// its saved record), frees the card and saves the conversation. ok is false
// when the card is gone and the kind does not finish one that is.
func (k cardKind[P, V]) finish(s *Service, c *conversation, id string, p *P, set func()) (v V, ok bool) {
	s.mu.Lock()
	if !k.finishGone && (s.convs[c.id] != c || k.find(c, id) != p) {
		s.mu.Unlock()
		return v, false
	}
	set()
	*k.busy(p) = false
	v = k.view(p)
	s.mu.Unlock()
	s.persist(c)
	return v, true
}
