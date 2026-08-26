package runtime

import (
	"io"
	"sync"
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

type fakeBridge struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
	once sync.Once
}

func newFakeBridge() *fakeBridge {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	return &fakeBridge{inR: inR, inW: inW, outR: outR, outW: outW}
}

func (f *fakeBridge) Read(p []byte) (int, error)  { return f.outR.Read(p) }
func (f *fakeBridge) Write(p []byte) (int, error) { return f.inW.Write(p) }
func (f *fakeBridge) Resize(int, int) error       { return nil }
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

func TestDetachKeepsSession(t *testing.T) {
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
	id, ch, _ := sess.Subscribe()
	if _, err := fb.outW.Write([]byte("hello-herd")); err != nil {
		t.Fatal(err)
	}
	got := waitBytes(t, ch, "hello-herd")
	if got == "" {
		t.Fatal("expected output")
	}
	sess.Unsubscribe(id)

	if _, err := fb.outW.Write([]byte("still-here")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)

	_, ch2, snap := sess.Subscribe()
	if string(snap) != "hello-herdstill-here" {
		t.Fatalf("snapshot = %q", snap)
	}
	sess.Unsubscribe(uint64(2))
	_ = ch2

	if err := rt.Kill(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Get(info.ID); err != ErrNotFound {
		t.Fatal("killed session should disappear")
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
