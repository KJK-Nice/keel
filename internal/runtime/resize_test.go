package runtime

import (
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

// A driver that resizes while the bridge is still connecting (an SSH
// connect takes seconds) must get that size on the PTY once it opens; the
// same size sent again later is skipped as unchanged.
func TestResizeWhileConnectingReachesPTY(t *testing.T) {
	st := testStore(t)
	host := firstHost(t, st)
	fb := newFakeBridge()
	entered := make(chan [2]int, 1)
	release := make(chan struct{})
	rt := New(st, func(_ store.Host, cols, rows int) (session.Bridge, error) {
		entered <- [2]int{cols, rows} // connecting at this size…
		<-release                     // …and still connecting
		return fb, nil
	})
	t.Cleanup(rt.KillAll)
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-entered; got != [2]int{80, 24} {
		t.Fatalf("connecting at %v, want the spawn size 80x24", got)
	}
	driver, _, _ := sess.Subscribe(Join{Name: "alice", Kind: "web"})
	if err := sess.Resize(driver.ID, 140, 40); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitReady(t, sess)
	deadline := time.Now().Add(2 * time.Second)
	for {
		fb.mu.Lock()
		cols, rows := fb.cols, fb.rows
		fb.mu.Unlock()
		if cols == 140 && rows == 40 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("PTY is %dx%d, want the driver's 140x40", cols, rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The client re-sends its size on "ready"; it is unchanged, so skipped.
	if err := sess.Resize(driver.ID, 140, 40); err != nil {
		t.Fatal(err)
	}
}
