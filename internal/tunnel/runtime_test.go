package tunnel

import (
	"io"
	"net"
	"testing"
	"time"

	"keel/internal/store"
)

type loopDialer struct {
	backend string
}

func (d loopDialer) Dial(n, addr string) (net.Conn, error) {
	return net.Dial("tcp", d.backend)
}

func (d loopDialer) Listen(n, addr string) (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

func (d loopDialer) Close() error { return nil }

func echoServer(t *testing.T) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestLocalForward(t *testing.T) {
	backend, stop := echoServer(t)
	defer stop()
	st := testStore(t)
	host, err := st.UpsertHost(store.Host{
		Name: "box", Address: "10.0.0.9", Port: 22, Username: "u",
		AuthType: "password", Password: "x", Color: "var(--ansi-12)",
	})
	if err != nil {
		t.Fatal(err)
	}
	tn, err := st.UpsertTunnel(store.Tunnel{
		Name: "echo", HostID: host.ID, Kind: "local",
		BindAddr: "127.0.0.1", BindPort: 0,
		DestHost: "127.0.0.1", DestPort: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := New(st, func(store.Host) (Dialer, error) {
		return loopDialer{backend: backend}, nil
	})
	info, err := rt.Start(tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, _ = rt.Get(tn.ID)
		if info.Status == StatusReady && info.ListenAddr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if info.Status != StatusReady {
		t.Fatalf("status %s err %s", info.Status, info.Error)
	}
	c, err := net.Dial("tcp", info.ListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q", buf)
	}
	_ = rt.Stop(tn.ID)
}
