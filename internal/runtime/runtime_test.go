package runtime

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

type fakeBridge struct {
	inR     *io.PipeReader
	inW     *io.PipeWriter
	outR    *io.PipeReader
	outW    *io.PipeWriter
	once    sync.Once
	mu      sync.Mutex
	cols    int
	rows    int
	resizes int
}

func newFakeBridge() *fakeBridge {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	return &fakeBridge{inR: inR, inW: inW, outR: outR, outW: outW}
}

func (f *fakeBridge) Read(p []byte) (int, error)  { return f.outR.Read(p) }
func (f *fakeBridge) Write(p []byte) (int, error) { return f.inW.Write(p) }
func (f *fakeBridge) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cols, f.rows = cols, rows
	f.resizes++
	return nil
}
func (f *fakeBridge) Close() error {
	var err error
	f.once.Do(func() {
		_ = f.inW.Close()
		_ = f.outW.Close()
		_ = f.inR.Close()
		_ = f.outR.Close()
	})
	return err
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func spawnReady(t *testing.T) (*Runtime, *Session, *fakeBridge) {
	t.Helper()
	st := testStore(t)
	host := st.ListHosts()[0]
	fb := newFakeBridge()
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		return fb, nil
	})
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	return rt, sess, fb
}

func TestDetachKeepsSession(t *testing.T) {
	rt, sess, fb := spawnReady(t)

	cl, ch, _ := sess.Subscribe(Join{Name: "alice", Kind: "web"})
	if _, err := fb.outW.Write([]byte("hello-herd")); err != nil {
		t.Fatal(err)
	}
	got := waitBytes(t, ch, "hello-herd")
	if got == "" {
		t.Fatal("expected output")
	}
	sess.Unsubscribe(cl.ID)

	if _, err := fb.outW.Write([]byte("still-here")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)

	cl2, ch2, snap := sess.Subscribe(Join{Name: "bob", Kind: "tui"})
	if string(snap) != "hello-herdstill-here" {
		t.Fatalf("snapshot = %q", snap)
	}
	sess.Unsubscribe(cl2.ID)
	_ = ch2

	if err := rt.Kill(sess.id); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Get(sess.id); err != ErrNotFound {
		t.Fatal("killed session should disappear")
	}
}

func TestKillAllClosesEverySession(t *testing.T) {
	rt, sess, _ := spawnReady(t)
	host := rt.store.ListHosts()[0]
	info2, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Count() != 2 {
		t.Fatalf("count = %d", rt.Count())
	}
	rt.KillAll()
	if rt.Count() != 0 {
		t.Fatalf("after KillAll count = %d", rt.Count())
	}
	if _, err := rt.Get(sess.id); err != ErrNotFound {
		t.Fatal("first session still live")
	}
	if _, err := rt.Get(info2.ID); err != ErrNotFound {
		t.Fatal("second session still live")
	}
}

func TestTwoSubscribersSeeSameBytes(t *testing.T) {
	_, sess, fb := spawnReady(t)
	a, chA, _ := sess.Subscribe(Join{Name: "alice"})
	b, chB, _ := sess.Subscribe(Join{Name: "bob"})
	if _, err := fb.outW.Write([]byte("shared")); err != nil {
		t.Fatal(err)
	}
	if got := waitBytes(t, chA, "shared"); got != "shared" {
		t.Fatalf("alice = %q", got)
	}
	if got := waitBytes(t, chB, "shared"); got != "shared" {
		t.Fatalf("bob = %q", got)
	}
	sess.Unsubscribe(a.ID)
	sess.Unsubscribe(b.ID)
}

func TestFirstSubscriberIsOwnerDriver(t *testing.T) {
	_, sess, _ := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	bob, _, _ := sess.Subscribe(Join{Name: "bob"})
	info := sess.Info()
	if alice.Role != RoleDriver {
		t.Fatalf("alice role = %s", alice.Role)
	}
	if info.OwnerID != alice.ID || info.DriverID != alice.ID {
		t.Fatalf("owner/driver = %d/%d want %d", info.OwnerID, info.DriverID, alice.ID)
	}
	var bobClient Client
	for _, c := range info.Attached {
		if c.ID == bob.ID {
			bobClient = c
		}
	}
	if bobClient.Role != RoleFollower {
		t.Fatalf("bob role = %s", bobClient.Role)
	}
}

func TestFollowerWriteDroppedThenGrant(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	bob, _, _ := sess.Subscribe(Join{Name: "bob"})

	if err := sess.Write(bob.ID, []byte("nope")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("follower write err = %v", err)
	}
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := fb.inR.Read(buf)
		done <- string(buf[:n])
	}()
	if err := sess.GrantControl(alice.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if sess.Info().DriverID != bob.ID {
		t.Fatal("bob should be driving")
	}
	if err := sess.Write(bob.ID, []byte("yes")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got != "yes" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for write")
	}

	if err := sess.Write(alice.ID, []byte("owner-now-follower")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("alice write after grant err = %v", err)
	}
	if err := sess.TakeControl(alice.ID); err != nil {
		t.Fatal(err)
	}
	if sess.Info().DriverID != alice.ID {
		t.Fatal("owner should have taken control")
	}
}

func TestFollowerCannotTakeControl(t *testing.T) {
	_, sess, _ := spawnReady(t)
	_, _, _ = sess.Subscribe(Join{Name: "alice"})
	bob, _, _ := sess.Subscribe(Join{Name: "bob"})
	if err := sess.TakeControl(bob.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("take = %v", err)
	}
}

func TestOwnerLeavePromotes(t *testing.T) {
	_, sess, _ := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	bob, _, _ := sess.Subscribe(Join{Name: "bob"})
	sess.Unsubscribe(alice.ID)
	info := sess.Info()
	if info.OwnerID != bob.ID || info.DriverID != bob.ID {
		t.Fatalf("promoted owner/driver = %d/%d", info.OwnerID, info.DriverID)
	}
}

func TestOnlyDriverResizes(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	bob, _, _ := sess.Subscribe(Join{Name: "bob"})
	if err := sess.Resize(bob.ID, 100, 40); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("follower resize = %v", err)
	}
	if err := sess.Resize(alice.ID, 120, 36); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fb.mu.Lock()
		ok := fb.cols == 120 && fb.rows == 36
		fb.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("resize not applied: %+v", fb)
}

func TestHerdEvents(t *testing.T) {
	st := testStore(t)
	host := st.ListHosts()[0]
	fb := newFakeBridge()
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		return fb, nil
	})
	hid, ch, snap := rt.SubscribeHerd()
	if len(snap) != 0 {
		t.Fatalf("empty herd, got %d", len(snap))
	}
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	ev := waitHerd(t, ch, "spawn")
	if ev.ID != info.ID {
		t.Fatalf("spawn id %s", ev.ID)
	}
	sess, _ := rt.Get(info.ID)
	waitReady(t, sess)
	cl, _, _ := sess.Subscribe(Join{Name: "alice"})
	waitHerd(t, ch, "update")
	sess.Unsubscribe(cl.ID)
	if err := rt.Kill(info.ID); err != nil {
		t.Fatal(err)
	}
	kill := waitHerd(t, ch, "kill")
	if kill.ID != info.ID {
		t.Fatalf("kill id %s", kill.ID)
	}
	rt.UnsubscribeHerd(hid)
}

func waitHerd(t *testing.T, ch <-chan HerdEvent, kind string) HerdEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("herd closed")
			}
			if ev.Type == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("timeout waiting for herd %s", kind)
		}
	}
}

func waitReady(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.Info().Status == StatusReady {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session never became ready: %+v", s.Info())
}

func waitBytes(t *testing.T, ch <-chan Event, want string) string {
	t.Helper()
	var out string
	deadline := time.After(time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("channel closed")
			}
			if ev.Kind == "bytes" {
				out += string(ev.Bytes)
				if out == want || len(out) >= len(want) {
					return out
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %q, got %q", want, out)
			return out
		}
	}
}

func TestRingTrims(t *testing.T) {
	r := newRing(8)
	r.Write([]byte("abcdefghij"))
	if got := string(r.Bytes()); got != "cdefghij" {
		t.Fatalf("got %q", got)
	}
}
