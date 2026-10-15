package runtime

import (
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

// A reconnect loads the host as saved now; when that fails (here the jump
// host was deleted), it fails instead of dialing the stale copy with its
// old jump host and secrets.
func TestReconnectFailsWhenHostCannotBeLoaded(t *testing.T) {
	st := testStore(t)
	bastion, err := st.UpsertHost(store.Host{Name: "bastion", Address: "10.0.0.9", Username: "u", AuthType: "password", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	host := sshHost(t, st)
	host.JumpHost = bastion.ID
	host.NoAutoReconnect = true
	if _, err := st.UpsertHost(host); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bridges []*fakeBridge
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		bridges = append(bridges, fb)
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	if err := st.DeleteHost(bastion.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Reconnect(info.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return sess.Info().Status == StatusError }, func() string { return string(sess.Info().Status) })
	if !strings.Contains(sess.Info().Error, "jump host not found") {
		t.Fatalf("error = %q", sess.Info().Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bridges) != 1 {
		t.Fatalf("dialed %d times; the stale host must not be dialed", len(bridges))
	}
}
