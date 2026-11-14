package store

import (
	"errors"
	"fmt"
	"strings"

	"keel/internal/limits"
)

// Jump hosts (ProxyJump, docs/ui-redesign.md §5.8).
//
// A host's JumpHost is the ID of another saved host. Referencing a saved
// host, rather than a free "user@host:port", keeps the jump's credentials
// in the vault and its host key in KnownHosts under the jump's own
// address, checked like any other connect. Only one hop is supported: a
// jump host cannot itself use a jump host, which also rules out cycles.

// ErrHostField wraps every validation error of a host a caller saves (its
// name, how it signs in, its jump host and startup command), and
// ErrIdentityField every one of an identity, so the API can answer 400
// with the message.
var (
	ErrHostField     = errors.New("invalid host")
	ErrIdentityField = errors.New("invalid identity")
)

// MaxStartupCommand is the longest startup command, in bytes.
const MaxStartupCommand = limits.MaxStartupCommand

// fieldError is a validation error: msg for the user, of for errors.Is.
type fieldError struct {
	of  error
	msg string
}

func (e *fieldError) Error() string { return e.msg }
func (e *fieldError) Unwrap() error { return e.of }

func fieldErr(format string, args ...any) error {
	return &fieldError{of: ErrHostField, msg: fmt.Sprintf(format, args...)}
}

// checkHostFieldsLocked validates h for every save, whoever asks (the API,
// the ssh config import, a duplicate): its name and sign-in, then the
// startup command and jump host against the saved hosts. h.ID is empty for
// a new host.
func (s *Store) checkHostFieldsLocked(h Host) error {
	if err := CheckHostSignIn(h); err != nil {
		return err
	}
	if h.AuthType == AuthIdentity && strings.TrimSpace(h.Username) == "" {
		// A connect takes the identity's user then (withIdentityLocked).
		if ident, err := s.identityFullLocked(h.IdentityID); err != nil || strings.TrimSpace(ident.Username) == "" {
			return fieldErr("username is required")
		}
	}
	if err := ValidateStartupCommand(h.StartupCommand); err != nil {
		return err
	}
	if h.StartupCommand != "" && h.IsLocal() {
		return fieldErr("the local shell has no startup command")
	}
	return s.checkJumpLocked(h)
}

// CheckHostSignIn wants a name, a known AuthType and, for an SSH host, an
// address and a user. A host that signs in with an identity names it, and
// may leave the user to it (a save then checks the identity has one).
// Every save runs it; Test connection runs it on a host that is not saved.
func CheckHostSignIn(h Host) error {
	if strings.TrimSpace(h.Name) == "" {
		return fieldErr("name is required")
	}
	switch h.AuthType {
	case AuthLocal:
		return nil
	case AuthIdentity:
		if strings.TrimSpace(h.IdentityID) == "" {
			return fieldErr("identity is required")
		}
	case AuthPassword, AuthKey:
	default:
		return fieldErr("authType must be local, password, key, or identity")
	}
	if strings.TrimSpace(h.Address) == "" {
		return fieldErr("address is required")
	}
	if strings.TrimSpace(h.Username) == "" && h.AuthType != AuthIdentity {
		return fieldErr("username is required")
	}
	return nil
}

// ValidateStartupCommand accepts one line of printable text: no newline,
// carriage return, tab or other control character, at most
// MaxStartupCommand bytes. Keel adds the Enter itself.
func ValidateStartupCommand(cmd string) error {
	if p := limits.StartupCommandProblem(cmd); p != "" {
		return fieldErr("%s", p)
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
			if h.IsLocal() {
				return fieldErr("%s uses this host as its jump host, so it cannot be a local shell", o.Name)
			}
		}
	}
	if jid == "" {
		return nil
	}
	if h.IsLocal() {
		return fieldErr("the local shell cannot use a jump host")
	}
	if h.ID != "" && jid == h.ID {
		return fieldErr("a host cannot be its own jump host")
	}
	j, ok := s.hostLocked(jid)
	if !ok {
		return fieldErr("jump host not found")
	}
	if j.AuthType == AuthLocal {
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
	if j.AuthType == AuthLocal {
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
	if s.locked.Load() {
		return Host{}, ErrLocked
	}
	draft.Jump = nil
	draft.Password = cleanSecret(draft.Password)
	draft.PrivateKey = cleanSecret(draft.PrivateKey)
	draft.Passphrase = cleanSecret(draft.Passphrase)
	if draft.AuthType != AuthIdentity {
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
		case AuthIdentity:
			return Host{}, fieldErr("%s: save it first to test with a Keychain identity, or type a password or key", changed)
		case AuthKey:
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
	case d.AuthType == AuthIdentity && d.IdentityID != saved.IdentityID:
		return "Identity changed"
	}
	return ""
}
