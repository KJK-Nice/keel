package runtime

import (
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

// recorder drains a fake bridge's input (what Keel typed into the PTY).
type recorder struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (r *recorder) drain(fb *fakeBridge) {
	go func() {
		b := make([]byte, 1024)
		for {
			n, err := fb.inR.Read(b)
			r.mu.Lock()
			r.buf.Write(b[:n])
			r.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func TestStartupCommandSentOnceNotOnReconnectOrFollow(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	host.StartupCommand = "tmux new -A -s main"
	if _, err := st.UpsertHost(host); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bridges []*fakeBridge
	var recs []*recorder
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		rec := &recorder{}
		rec.drain(fb)
		bridges = append(bridges, fb)
		recs = append(recs, rec)
		return fb, nil
	})
	rt.backoff = time.Millisecond
	var logMu sync.Mutex
	var logs []string
	rt.SetNotify(func(kind, msg, hostID, sessionID string) {
		logMu.Lock()
		logs = append(logs, kind+": "+msg)
		logMu.Unlock()
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
	waitFor(t, func() bool { return recs[0].String() == "tmux new -A -s main\r" }, func() string { return recs[0].String() })

	// A follower attaching sends nothing.
	owner, _, _ := sess.Subscribe(Join{Name: "owner"})
	follower, _, _ := sess.Subscribe(Join{Name: "joiner"})
	sess.mu.Lock()
	role := sess.roleLocked(follower.ID)
	sess.mu.Unlock()
	if role != RoleFollower {
		t.Fatalf("second client role = %s", role)
	}
	_ = owner

	// A reconnect opens a new shell and does not type it again.
	if _, err := rt.Reconnect(info.ID); err != nil {
		t.Fatal(err)
	}
	waitBridges(t, sess, &mu, &bridges, 2)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	second := recs[1].String()
	first := recs[0].String()
	mu.Unlock()
	if second != "" {
		t.Fatalf("reconnect typed %q", second)
	}
	if first != "tmux new -A -s main\r" {
		t.Fatalf("first shell got %q", first)
	}

	logMu.Lock()
	defer logMu.Unlock()
	joined := strings.Join(logs, "\n")
	if strings.Count(joined, "startup command sent to box") != 1 {
		t.Fatalf("logs = %q", joined)
	}
	if strings.Contains(joined, "tmux") {
		t.Fatalf("the log must not hold the command text: %q", joined)
	}
}

func TestNoStartupCommandForLocalShell(t *testing.T) {
	st := testStore(t)
	host := firstHost(t, st)
	host.StartupCommand = "echo hi"
	if _, err := st.UpsertHost(host); err == nil {
		t.Fatal("the local shell must not take a startup command")
	}
}

func waitFor(t *testing.T, ok func() bool, got func() string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out; got %q", got())
}
