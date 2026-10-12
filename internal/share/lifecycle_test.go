package share

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/signal"
	"keel/internal/store"
)

func shareFixture(t *testing.T) (*runtime.Runtime, string) {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	info, err := rt.Spawn(hosts[0].ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	return rt, info.ID
}

// countingSignal wraps a signal server and counts room creations.
func countingSignal(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var rooms atomic.Int32
	inner := signal.New().Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/rooms" {
			rooms.Add(1)
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &rooms
}

func TestConcurrentShareMakesOneRoom(t *testing.T) {
	rt, id := shareFixture(t)
	sig, rooms := countingSignal(t)
	hub := New(sig.URL, rt)
	t.Cleanup(func() { hub.Stop(id) })

	const callers = 8
	urls := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			urls[i], errs[i] = hub.Share(id)
		}(i)
	}
	wg.Wait()
	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if urls[i] != urls[0] || urls[i] == "" {
			t.Fatalf("callers got different links: %q vs %q", urls[i], urls[0])
		}
	}
	if n := rooms.Load(); n != 1 {
		t.Fatalf("%d rooms created for one session, want 1", n)
	}
}

func TestStopShareDropsLink(t *testing.T) {
	rt, id := shareFixture(t)
	sig, rooms := countingSignal(t)
	hub := New(sig.URL, rt)
	first, err := hub.Share(id)
	if err != nil {
		t.Fatal(err)
	}
	hub.Stop(id)
	if got := hub.JoinURL(id); got != "" {
		t.Fatalf("JoinURL after Stop = %q", got)
	}
	second, err := hub.Share(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.Stop(id) })
	if second == first {
		t.Fatal("sharing again reused the stopped room")
	}
	if n := rooms.Load(); n != 2 {
		t.Fatalf("rooms = %d, want 2", n)
	}
}

// When the signaling connection drops, the share must be forgotten so the
// next Share creates a working room instead of returning a dead link.
func TestSignalDropForgetsShare(t *testing.T) {
	rt, id := shareFixture(t)
	sig, _ := countingSignal(t)
	hub := New(sig.URL, rt)
	if _, err := hub.Share(id); err != nil {
		t.Fatal(err)
	}
	sig.CloseClientConnections()
	deadline := time.Now().Add(3 * time.Second)
	for hub.JoinURL(id) != "" {
		if time.Now().After(deadline) {
			t.Fatal("share still listed after its signaling connection dropped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
