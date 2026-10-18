package runtime

import (
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

// A command running when the connection drops is abandoned there: after
// the reconnect it is not "still running" over Keel's note and the new
// shell's banner, which come after it instead.
func TestLastCommandAbandonedOnReconnect(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	var mu sync.Mutex
	var bridges []*fakeBridge
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		if len(bridges) == 0 {
			fb.drop = true
		}
		bridges = append(bridges, fb)
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := rt.Get(info.ID)
	waitReady(t, sess)
	_, ch, _ := sess.Subscribe(Join{Name: "alice"})
	if _, err := bridges[0].outW.Write([]byte("\x1b]133;B\x07top\r\n\x1b]133;C\x07load average 0.1")); err != nil {
		t.Fatal(err)
	}
	waitBytes(t, ch, "load average")
	_ = bridges[0].Close()
	waitBridges(t, sess, &mu, &bridges, 2)
	if _, err := bridges[1].outW.Write([]byte("Welcome to box\r\n$ ")); err != nil {
		t.Fatal(err)
	}
	after := func() string { b, _ := rt.LastCommand(info.ID); return b.After }
	waitFor(t, func() bool { return strings.Contains(after(), "Welcome") }, after)
	blk, ok := rt.LastCommand(info.ID)
	if !ok || !blk.Done || !blk.Abandoned || blk.HasExit || blk.Output != "load average 0.1" {
		t.Fatalf("block = %+v", blk)
	}
	if !strings.Contains(blk.After, "reconnecting") {
		t.Errorf("after = %q", blk.After)
	}
}

// What a pane printed after the last command ended is read too (masked),
// so a fake end mark printed by a program cannot hide the rest.
func TestLastCommandAfterPart(t *testing.T) {
	s := markSession(1 << 20)
	s.output([]byte("\x1b]133;B\x07cat evil.log\r\n\x1b]133;C\x07" +
		"\x1b]133;D;0\x07\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07total 0\r\n\x1b]133;D;0\x07" +
		"rm -rf / password=hunter2\r\n\x1b]133;D;0\x07$ "))
	blk, ok := s.LastCommand()
	if !ok || blk.Command != "ls\r\n" || blk.Output != "total 0\r\n" {
		t.Fatalf("block = %+v", blk)
	}
	if !strings.Contains(blk.After, "rm -rf /") || strings.Contains(blk.After, "hunter2") || blk.AfterTruncated {
		t.Fatalf("after = %q", blk.After)
	}
	// Bounded: only the last maxAfterText bytes.
	s.output([]byte(strings.Repeat("y", 2*maxAfterText)))
	blk, _ = s.LastCommand()
	if len(blk.After) != maxAfterText || !blk.AfterTruncated {
		t.Fatalf("after %d bytes, truncated %v", len(blk.After), blk.AfterTruncated)
	}
}
