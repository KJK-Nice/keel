package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Jump hosts (ProxyJump, docs/ui-redesign.md §5.8).
//
// A host's JumpHost is the ID of another saved host. Referencing a saved
// host, rather than a free "user@host:port", keeps the jump's credentials
// in the vault and its host key in KnownHosts under the jump's own
// address, checked like any other connect. Only one hop is supported: a
// jump host cannot itself use a jump host, which also rules out cycles.

// ErrHostField wraps every jump host and startup command validation error,
// so the API can answer 400 with the message.
var ErrHostField = errors.New("invalid host")

// MaxStartupCommand is the longest startup command, in bytes.
const MaxStartupCommand = 1024

type hostFieldError struct{ msg string }

func (e *hostFieldError) Error() string { return e.msg }
func (e *hostFieldError) Unwrap() error { return ErrHostField }

func fieldErr(format string, args ...any) error {
	return &hostFieldError{msg: fmt.Sprintf(format, args...)}
}

// checkHostFieldsLocked validates the startup command and jump host of h
// against the saved hosts. h.ID is empty for a new host.
func (s *Store) checkHostFieldsLocked(h Host) error {
	if err := ValidateStartupCommand(h.StartupCommand); err != nil {
		return err
	}
	if h.StartupCommand != "" && h.AuthType == "local" {
		return fieldErr("the local shell has no startup command")
	}
	return s.checkJumpLocked(h)
}

// ValidateStartupCommand accepts one line of printable text: no newline,
// carriage return, tab or other control character, at most
// MaxStartupCommand bytes. Keel adds the Enter itself.
func ValidateStartupCommand(cmd string) error {
	if cmd == "" {
		return nil
	}
	if len(cmd) > MaxStartupCommand {
		return fieldErr("startup command is longer than %d bytes", MaxStartupCommand)
	}
	if !utf8.ValidString(cmd) {
		return fieldErr("startup command is not valid text")
	}
	for _, r := range cmd {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return fieldErr("startup command must be one line with no control characters")
		}
	}
	return nil
}

func (s *Store) hostLocked(id string) (persistHost, bool) {
	for _, h := range s.data.Hosts {
		if h.ID == id {
			return h, true
		}
	}
	return persistHost{}, false
}

func (s *Store) checkJumpLocked(h Host) error {
	jid := strings.TrimSpace(h.JumpHost)
	if h.ID != "" {
		for _, o := range s.data.Hosts {
			if o.ID == h.ID || o.JumpHost != h.ID {
				continue
			}
			if jid != "" {
				return fieldErr("%s uses this host as its jump host, so it cannot use one itself (one hop only)", o.Name)
			}
			if h.AuthType == "local" {
				return fieldErr("%s uses this host as its jump host, so it cannot be a local shell", o.Name)
			}
		}
	}
	if jid == "" {
		return nil
	}
	if h.AuthType == "local" {
		return fieldErr("the local shell cannot use a jump host")
	}
	if h.ID != "" && jid == h.ID {
		return fieldErr("a host cannot be its own jump host")
	}
	j, ok := s.hostLocked(jid)
	if !ok {
		return fieldErr("jump host not found")
	}
	if j.AuthType == "local" {
		return fieldErr("the local shell cannot be a jump host")
	}
	if j.JumpHost != "" {
		return fieldErr("%s uses a jump host itself; only one hop is supported", j.Name)
	}
	return nil
}

// attachJumpLocked resolves host.JumpHost into host.Jump, with the jump's
// secrets (or Keychain identity) from the vault. It checks the rules again
// at connect time, since a saved file may predate them.
func (s *Store) attachJumpLocked(host *Host) error {
	host.Jump = nil
	jid := strings.TrimSpace(host.JumpHost)
	if jid == "" {
		return nil
	}
	if jid == host.ID {
		return fieldErr("a host cannot be its own jump host")
	}
	j, ok := s.hostLocked(jid)
	if !ok {
		return fieldErr("jump host not found; edit %s and pick another", host.Name)
	}
	if j.AuthType == "local" {
		return fieldErr("the local shell cannot be a jump host")
	}
	if j.JumpHost != "" {
		return fieldErr("%s uses a jump host itself; only one hop is supported", j.Name)
	}
	jump, err := s.withIdentityLocked(s.fullLocked(j))
	if err != nil {
		return fmt.Errorf("jump host %s: %w", j.Name, err)
	}
	jump.JumpHost = ""
	host.Jump = &jump
	return nil
}

// ResolveDraft turns unsaved dialog values into a host ready to dial, for
// Test connection. Secrets left empty (or "__saved__") come from the saved
// host with draft.ID, when there is one; a Keychain identity and the jump
// host come from the vault. Nothing is saved.
func (s *Store) ResolveDraft(draft Host) (Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return Host{}, ErrLocked
	}
	draft.Jump = nil
	draft.Password = cleanSecret(draft.Password)
	draft.PrivateKey = cleanSecret(draft.PrivateKey)
	draft.Passphrase = cleanSecret(draft.Passphrase)
	if draft.ID != "" {
		if _, ok := s.hostLocked(draft.ID); ok {
			sec := s.secrets[draft.ID]
			if draft.Password == "" {
				draft.Password = sec.Password
			}
			if draft.PrivateKey == "" {
				draft.PrivateKey = sec.PrivateKey
			}
			if draft.Passphrase == "" {
				draft.Passphrase = sec.Passphrase
			}
		} else {
			draft.ID = ""
		}
	}
	if draft.AuthType != "identity" {
		draft.IdentityID = ""
	}
	if err := s.checkJumpLocked(draft); err != nil {
		return Host{}, err
	}
	host, err := s.withIdentityLocked(draft)
	if err != nil {
		return Host{}, err
	}
	if err := s.attachJumpLocked(&host); err != nil {
		return Host{}, err
	}
	return host, nil
}
