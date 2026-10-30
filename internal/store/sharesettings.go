package store

import "strings"

// ShareSettings are the values Settings · Sharing saves in keel.json. No
// secret lives here: the signal server only relays a share's handshake.
type ShareSettings struct {
	// SignalURL is the keel-signal server for internet join links, used
	// when KEEL_SIGNAL is not set for the run (share.Hub). httpapi checks
	// it with share.NormalizeSignal before it is saved.
	SignalURL string `json:"signalURL,omitempty"`
}

// SignalURL returns the saved signal server, or "". keel.json is read
// before the vault is unlocked and the URL is no secret, so this works
// while the vault is locked: keel serve reads it at start.
func (s *Store) SignalURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Share == nil {
		return ""
	}
	return strings.TrimSpace(s.data.Share.SignalURL)
}

// SetSignalURL saves the signal server; "" removes it.
func (s *Store) SetSignalURL(u string) error {
	u = strings.TrimSpace(u)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return ErrLocked
	}
	if u == "" {
		s.data.Share = nil
	} else {
		s.data.Share = &ShareSettings{SignalURL: u}
	}
	return s.saveLocked()
}
