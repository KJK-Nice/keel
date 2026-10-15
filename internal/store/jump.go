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
// Test connection. Nothing is saved.
//
// Vault-held credentials go only to the server they were saved for. A
// secret left blank (or "__saved__") is filled from the saved host with
// draft.ID, and a Keychain identity is used, only when the draft still
// targets that host: same address:port, username, authentication and jump
// host. Otherwise the secret must be typed, so editing a saved host's
// address and pressing Test cannot send its password to the new address.
// The jump host's own secrets always go to the jump's saved address.
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
	if draft.AuthType != "identity" {
		draft.IdentityID = ""
	}
	changed := "This host is not saved"
	if draft.ID != "" {
		if saved, ok := s.hostLocked(draft.ID); ok {
			changed = draftChange(draft, saved)
			if changed == "" {
				sec := s.secrets[draft.ID]
				if draft.Password == "" && draft.PrivateKey == "" {
					draft.Password = sec.Password
					draft.PrivateKey = sec.PrivateKey
				}
				if draft.Passphrase == "" && draft.PrivateKey == sec.PrivateKey {
					draft.Passphrase = sec.Passphrase
				}
			}
		} else {
			draft.ID = ""
		}
	}
	if changed != "" {
		switch draft.AuthType {
		case "identity":
			return Host{}, fieldErr("%s: save it first to test with a Keychain identity, or type a password or key", changed)
		case "key":
			if draft.PrivateKey == "" {
				return Host{}, fieldErr("%s: paste the private key to test it", changed)
			}
		default:
			if draft.Password == "" {
				return Host{}, fieldErr("%s: type the password to test it", changed)
			}
		}
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

// draftChange names the first connection field the draft changed from the
// saved host ("Address changed"), or "" when it targets the same server
// the same way.
func draftChange(d Host, saved persistHost) string {
	sp := saved.Port
	if sp == 0 {
		sp = 22
	}
	dp := d.Port
	if dp == 0 {
		dp = 22
	}
	switch {
	case !strings.EqualFold(strings.TrimSpace(d.Address), strings.TrimSpace(saved.Address)):
		return "Address changed"
	case dp != sp:
		return "Port changed"
	case strings.TrimSpace(d.Username) != strings.TrimSpace(saved.Username):
		return "Username changed"
	case d.AuthType != saved.AuthType:
		return "Authentication changed"
	case strings.TrimSpace(d.JumpHost) != saved.JumpHost:
		return "Jump host changed"
	case d.AuthType == "identity" && d.IdentityID != saved.IdentityID:
		return "Identity changed"
	}
	return ""
}
