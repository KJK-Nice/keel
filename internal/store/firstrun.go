package store

// VaultState describes the vault for the unlock and first-run screens. It
// holds no secret and changes nothing.
type VaultState struct {
	Locked     bool `json:"locked"`
	NeedsSetup bool `json:"needsSetup"`
	// Mode is how keel.vault is unlocked: "keychain" (the wrapping key is
	// in the OS keychain), "passphrase" (scrypt), or "" before it exists.
	Mode string `json:"mode"`
	// FirstRun is true until the first-run screens finish, on a store Keel
	// seeded itself, unless KEEL_VAULT_PASSPHRASE opened the vault (a
	// scripted setup has no first run).
	FirstRun bool `json:"firstRun"`
	// KeyringOff is KEEL_KEYRING=off: the OS keychain is never asked.
	KeyringOff bool   `json:"keyringOff"`
	Path       string `json:"path"`
}

func (s *Store) VaultState() VaultState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := VaultState{
		Locked:     s.locked,
		NeedsSetup: s.locked && s.needSetup,
		FirstRun:   s.data.FirstRun && !s.fromEnv,
		KeyringOff: KeyringOff(),
		Path:       s.vaultPath,
	}
	if !st.NeedsSetup {
		switch s.data.Vault {
		case vaultX25519:
			st.Mode = "keychain"
		case vaultScrypt:
			st.Mode = "passphrase"
		}
	}
	return st
}

// FinishFirstRun clears the first-run flag so the screens do not come back.
func (s *Store) FinishFirstRun() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return ErrLocked
	}
	if !s.data.FirstRun {
		return nil
	}
	s.data.FirstRun = false
	return s.saveLocked()
}
