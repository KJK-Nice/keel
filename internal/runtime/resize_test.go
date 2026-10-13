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
	release := make(chan struct{})
	opened := make(chan [2]int, 1)
	rt := New(st, func(_ store.Host, cols, rows int) (session.Bridge, error) {
		<-release
		opened <- [2]int{cols, rows}
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
	driver, _, _ := sess.Subscribe(Join{Name: "alice", Kind: "web"})
	if err := sess.Resize(driver.ID, 140, 40); err != nil {
		t.Fatal(err)
	}
	close(release)
	if got := <-opened; got != [2]int{80, 24} {
		t.Fatalf("opened at %v, want the spawn size 80x24 (the resize came after)", got)
	}
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
