package runtime

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"keel/internal/inject"
)

// A burst of many small output chunks (yes | head over SSH) must not drop a
// client that is merely behind: what it has not read yet is well under the
// byte budget. Before, the queue held 512 events, so the 513th unread chunk
// closed the client's WebSocket.
func TestBurstOfSmallChunksDoesNotDropClient(t *testing.T) {
	_, sess, fb := spawnReady(t)
	cl, ch, _ := sess.Subscribe(Join{Name: "alice"})
	defer sess.Unsubscribe(cl.ID)

	var want bytes.Buffer
	for i := 0; i < 3000; i++ {
		line := fmt.Sprintf("y%05d\r\n", i)
		want.WriteString(line)
		if _, err := fb.outW.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}

	var got bytes.Buffer
	deadline := time.After(5 * time.Second)
	for got.Len() < want.Len() {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("client dropped after %d of %d bytes", got.Len(), want.Len())
			}
			if ev.Kind == "bytes" {
				got.Write(ev.Bytes)
			}
		case <-deadline:
			t.Fatalf("got %d of %d bytes", got.Len(), want.Len())
		}
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("bytes out of order or corrupted:\n got tail %q\nwant tail %q", tail(got.String()), tail(want.String()))
	}
	if !sess.hasClient(cl.ID) {
		t.Fatal("client was dropped")
	}
}

// Merging queued chunks for one client must not change what another client
// receives: chunks are shared between clients.
func TestMergedChunksStayPerClient(t *testing.T) {
	_, sess, fb := spawnReady(t)
	a, chA, _ := sess.Subscribe(Join{Name: "alice"})
	b, chB, _ := sess.Subscribe(Join{Name: "bob"})
	defer sess.Unsubscribe(a.ID)
	defer sess.Unsubscribe(b.ID)
	var want strings.Builder
	for i := 0; i < 200; i++ {
		line := fmt.Sprintf("line %03d\r\n", i)
		want.WriteString(line)
		if _, err := fb.outW.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	for name, ch := range map[string]<-chan Event{"alice": chA, "bob": chB} {
		if got := readBytes(t, ch, want.Len()); got != want.String() {
			t.Fatalf("%s got tail %q", name, tail(got))
		}
	}
}

func readBytes(t *testing.T, ch <-chan Event, n int) string {
	t.Helper()
	var out strings.Builder
	deadline := time.After(5 * time.Second)
	for out.Len() < n {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d of %d bytes", out.Len(), n)
			}
			if ev.Kind == "bytes" {
				out.Write(ev.Bytes)
			}
		case <-deadline:
			t.Fatalf("got %d of %d bytes", out.Len(), n)
		}
	}
	return out.String()
}

// A client that stops reading entirely is still dropped once its unread
// output passes the byte budget, so it cannot grow memory without bound.
func TestStalledClientDroppedPastBudget(t *testing.T) {
	_, sess, fb := spawnReady(t)
	cl, ch, _ := sess.Subscribe(Join{Name: "stalled"})
	chunk := bytes.Repeat([]byte("x"), 32<<10)
	go func() {
		for i := 0; i < (clientMaxQueuedBytes/len(chunk))*3; i++ {
			if _, err := fb.outW.Write(chunk); err != nil {
				return
			}
			if !sess.hasClient(cl.ID) {
				return
			}
		}
	}()
	deadline := time.After(5 * time.Second)
	for sess.hasClient(cl.ID) {
		select {
		case <-deadline:
			t.Fatal("stalled client was never dropped")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// The channel closes once the client is dropped.
	for range ch {
	}
}

func (s *Session) hasClient(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.clients[id]
	return ok
}

// The log names what overflowed: unread output or a pile of other events.
func TestOfferDropReason(t *testing.T) {
	cases := []struct {
		ev   Event
		want string
	}{
		{Event{Kind: "bytes", Bytes: bytes.Repeat([]byte("x"), 32<<10)}, "MB of output unread"},
		{Event{Kind: "presence"}, "events unread"},
	}
	for _, tc := range cases {
		c := newClientConn(Client{}, inject.SurfaceTerm)
		refused := false
		for i := 0; i < 2*clientMaxQueuedEvents+clientChanCap+1; i++ {
			if !c.offer(tc.ev) {
				refused = true
				break
			}
		}
		c.stop()
		if !refused {
			t.Fatalf("%s: never refused", tc.ev.Kind)
		}
		if got := c.dropped(); !strings.Contains(got, tc.want) {
			t.Fatalf("%s: reason %q, want %q", tc.ev.Kind, got, tc.want)
		}
	}
}
