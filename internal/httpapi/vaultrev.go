package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
)

// vaultRev names the vault's state as of now: GET /api/vault reports it as "rev", and the native client polls it to
// refresh hosts and tunnels. It changes with every store save and every tunnel change (bump, wired to
// store.OnChange and tunnel.Runtime.OnChange).
type vaultRev struct {
	mu sync.Mutex
	// epoch and changes make up the rev: changes counts bump calls, epoch tells this keel serve process from the
	// next, whose count restarts.
	epoch   string
	changes uint64
}

func newVaultRev() *vaultRev { return &vaultRev{epoch: randomHex(6)} }

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (v *vaultRev) rev() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.epoch + "." + strconv.FormatUint(v.changes, 10)
}

// bump never blocks: it runs from the store's and the tunnel runtime's change hooks, under their locks.
func (v *vaultRev) bump() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.changes++
}
