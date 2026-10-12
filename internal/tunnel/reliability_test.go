package tunnel

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"keel/internal/store"
)

// fakeDialer is a Dialer whose connection can be failed on demand.
type fakeDialer struct {
	listenErr error
	closed    atomic.Int32
	dead      chan struct{}
	once      sync.Once
	pingErr   error
}

func newFakeDialer() *fakeDialer { return &fakeDialer{dead: make(chan struct{})} }

func (d *fakeDialer) Dial(n, addr string) (net.Conn, error) {
	return nil, errors.New("no backend")
}

func (d *fakeDialer) Listen(n, addr string) (net.Listener, error) {
	if d.listenErr != nil {
		return nil, d.listenErr
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

func (d *fakeDialer) Close() error {
	d.closed.Add(1)
	d.kill()
	return nil
}

func (d *fakeDialer) kill() { d.once.Do(func() { close(d.dead) }) }

func (d *fakeDialer) Wait() error {
	<-d.dead
	return errors.New("connection closed")
}

func (d *fakeDialer) Ping() error { return d.pingErr }

func tunnelFixture(t *testing.T, kind, bind string) (*store.Store, store.Tunnel) {
	t.Helper()
	st := testStore(t)
	host, err := st.UpsertHost(store.Host{
		Name: "box", Address: "10.0.0.9", Port: 22, Username: "u",
		AuthType: "password", Password: "x", Color: "#6ea8ff",
	})
	if err != nil {
		t.Fatal(err)
	}
	tn, err := st.UpsertTunnel(store.Tunnel{
		Name: "t", HostID: host.ID, Kind: kind,
		BindAddr: bind, BindPort: 0,
		DestHost: "127.0.0.1", DestPort: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, tn
}

func waitStatus(t *testing.T, rt *Runtime, id string, want Status) Info {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var info Info
	for time.Now().Before(deadline) {
		info, _ = rt.Get(id)
		if info.Status == want {
			return info
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("status %s (err %q), want %s", info.Status, info.Error, want)
	return info
}

func waitClosed(t *testing.T, d *fakeDialer) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if d.closed.Load() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("dialer (SSH connection) was never closed")
}

// A failed listen used to park the run on ctx.Done() while holding the SSH
// connection, and a restart replaced the entry without cancelling it.
func TestFailedListenReleasesConnectionAndRestartWorks(t *testing.T) {
	st, tn := tunnelFixture(t, "remote", "127.0.0.1")
	first := newFakeDialer()
	first.listenErr = errors.New("remote port forwarding disabled")
	second := newFakeDialer()
	var calls atomic.Int32
	rt := New(st, func(store.Host) (Dialer, error) {
		if calls.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})

	if _, err := rt.Start(tn.ID); err != nil {
		t.Fatal(err)
	}
	info := waitStatus(t, rt, tn.ID, StatusError)
	if info.Error != "remote port forwarding disabled" {
		t.Fatalf("error %q, want the listen error", info.Error)
	}
	waitClosed(t, first)

	if _, err := rt.Start(tn.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, rt, tn.ID, StatusReady)
	if second.closed.Load() != 0 {
		t.Fatal("the new run's connection was closed")
	}
	_ = rt.Stop(tn.ID)
	waitStatus(t, rt, tn.ID, StatusStopped)
	waitClosed(t, second)
}

// A previous run finishing late must not touch the entry of the run that
// replaced it.
func TestStaleRunDoesNotClobberNewEntry(t *testing.T) {
	st, tn := tunnelFixture(t, "local", "127.0.0.1")
	rt := New(st, func(store.Host) (Dialer, error) { return newFakeDialer(), nil })
	stale := &live{cancel: func() {}, status: StatusError, err: "old"}

	if _, err := rt.Start(tn.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, rt, tn.ID, StatusReady)
	rt.finish(tn.ID, stale)
	rt.fail(tn.ID, stale, "old failure")
	if info, _ := rt.Get(tn.ID); info.Status != StatusReady {
		t.Fatalf("stale run changed the entry: %s %q", info.Status, info.Error)
	}
	_ = rt.Stop(tn.ID)
}

// For local and dynamic forwards the listener is local, so it kept
// accepting after the SSH connection died and the tunnel showed "ready".
func TestLostConnectionEndsTunnel(t *testing.T) {
	for _, kind := range []string{"local", "dynamic"} {
		t.Run(kind, func(t *testing.T) {
			st, tn := tunnelFixture(t, kind, "127.0.0.1")
			d := newFakeDialer()
			rt := New(st, func(store.Host) (Dialer, error) { return d, nil })
			if _, err := rt.Start(tn.ID); err != nil {
				t.Fatal(err)
			}
			ready := waitStatus(t, rt, tn.ID, StatusReady)

			d.kill() // the SSH connection drops
			info := waitStatus(t, rt, tn.ID, StatusError)
			if info.Error != "connection lost" {
				t.Fatalf("error %q, want connection lost", info.Error)
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				c, err := net.DialTimeout("tcp", ready.ListenAddr, 200*time.Millisecond)
				if err != nil {
					break
				}
				c.Close()
				if time.Now().After(deadline) {
					t.Fatal("local listener still accepting after the connection was lost")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

func TestKeepaliveClosesUnresponsiveConnection(t *testing.T) {
	d := newFakeDialer()
	d.pingErr = errors.New("eof")
	done := make(chan struct{})
	go func() {
		keepalive(t.Context(), d, 5*time.Millisecond, time.Second)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive did not give up on a failing ping")
	}
	if d.closed.Load() == 0 {
		t.Fatal("keepalive did not close the dead connection")
	}
}

type hangingPinger struct{ *fakeDialer }

func (hangingPinger) Ping() error { select {} }

func TestKeepaliveTimesOutHangingPing(t *testing.T) {
	d := newFakeDialer()
	done := make(chan struct{})
	go func() {
		keepalive(t.Context(), hangingPinger{d}, 5*time.Millisecond, 20*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive waited forever on a ping with no reply")
	}
	if d.closed.Load() == 0 {
		t.Fatal("keepalive did not close the connection after the ping timed out")
	}
}

func TestDynamicTunnelMustBindLoopback(t *testing.T) {
	st := testStore(t)
	host, err := st.UpsertHost(store.Host{
		Name: "box", Address: "10.0.0.9", Port: 22, Username: "u",
		AuthType: "password", Password: "x", Color: "#6ea8ff",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, bind := range []string{"0.0.0.0", "192.168.1.5", "::"} {
		if _, err := st.UpsertTunnel(store.Tunnel{Name: "s", HostID: host.ID, Kind: "dynamic", BindAddr: bind}); err == nil {
			t.Errorf("dynamic tunnel on %s accepted", bind)
		}
	}
	for _, bind := range []string{"", "127.0.0.1", "localhost", "::1"} {
		if _, err := st.UpsertTunnel(store.Tunnel{Name: "s", HostID: host.ID, Kind: "dynamic", BindAddr: bind}); err != nil {
			t.Errorf("dynamic tunnel on %q rejected: %v", bind, err)
		}
	}
	if _, err := st.UpsertTunnel(store.Tunnel{
		Name: "l", HostID: host.ID, Kind: "local", BindAddr: "0.0.0.0",
		DestHost: "127.0.0.1", DestPort: 80,
	}); err != nil {
		t.Errorf("local forward on 0.0.0.0 (an explicit LAN share) rejected: %v", err)
	}
}
