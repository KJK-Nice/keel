package store

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

func (s *Store) ListTunnels() ([]Tunnel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return nil, ErrLocked
	}
	out := make([]Tunnel, len(s.data.Tunnels))
	copy(out, s.data.Tunnels)
	return out, nil
}

func (s *Store) GetTunnel(id string) (Tunnel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Tunnel{}, ErrLocked
	}
	for _, t := range s.data.Tunnels {
		if t.ID == id {
			return t, nil
		}
	}
	return Tunnel{}, ErrNotFound
}

func (s *Store) UpsertTunnel(tn Tunnel) (Tunnel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return Tunnel{}, ErrLocked
	}
	// Brackets would end up doubled by net.JoinHostPort ("[[::1]]:port").
	tn.BindAddr = strings.Trim(strings.TrimSpace(tn.BindAddr), "[]")
	if tn.BindAddr == "" {
		tn.BindAddr = "127.0.0.1"
	}
	if err := validateTunnel(tn); err != nil {
		return Tunnel{}, err
	}
	now := time.Now().UTC()
	if tn.ID == "" {
		tn.ID = newID()
		tn.CreatedAt = now
		tn.UpdatedAt = now
		s.data.Tunnels = append(s.data.Tunnels, tn)
		return tn, s.saveLocked()
	}
	for i, existing := range s.data.Tunnels {
		if existing.ID != tn.ID {
			continue
		}
		tn.CreatedAt = existing.CreatedAt
		tn.UpdatedAt = now
		s.data.Tunnels[i] = tn
		return tn, s.saveLocked()
	}
	tn.CreatedAt = now
	tn.UpdatedAt = now
	s.data.Tunnels = append(s.data.Tunnels, tn)
	return tn, s.saveLocked()
}

func (s *Store) DeleteTunnel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked.Load() {
		return ErrLocked
	}
	out := s.data.Tunnels[:0]
	found := false
	for _, t := range s.data.Tunnels {
		if t.ID == id {
			found = true
			continue
		}
		out = append(out, t)
	}
	if !found {
		return ErrNotFound
	}
	s.data.Tunnels = out
	return s.saveLocked()
}

func validateTunnel(tn Tunnel) error {
	if strings.TrimSpace(tn.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(tn.HostID) == "" {
		return fmt.Errorf("hostId is required")
	}
	switch tn.Kind {
	case "local", "remote":
		if strings.TrimSpace(tn.DestHost) == "" || tn.DestPort < 1 {
			return fmt.Errorf("destHost and destPort are required")
		}
	case "dynamic":
		// The SOCKS5 proxy has no authentication: bound beyond loopback
		// it would be an open proxy into the remote network.
		if !IsLoopbackAddr(tn.BindAddr) {
			return ErrOpenSOCKS
		}
	default:
		return fmt.Errorf("kind must be local, remote, or dynamic")
	}
	if tn.BindPort < 0 || tn.BindPort > 65535 {
		return fmt.Errorf("bindPort out of range")
	}
	return nil
}

// ErrOpenSOCKS rejects a SOCKS tunnel bound beyond loopback: the proxy has
// no authentication, so it would be open to the network.
var ErrOpenSOCKS = errors.New("a dynamic (SOCKS) tunnel must bind to 127.0.0.1, ::1 or localhost: it has no authentication")

// IsLoopbackAddr reports whether a bind address is loopback-only.
func IsLoopbackAddr(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
