package runtime

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/inject"
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
	drop    bool
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

func (f *fakeBridge) ExitKind() session.ExitKind {
	if f.drop {
		return session.ExitDrop
	}
	return session.ExitClean
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

func firstHost(t *testing.T, st *store.Store) store.Host {
	t.Helper()
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) == 0 {
		t.Fatal("no hosts")
	}
	return hosts[0]
}

func spawnReady(t *testing.T) (*Runtime, *Session, *fakeBridge) {
	t.Helper()
	st := testStore(t)
	host := firstHost(t, st)
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
	host := firstHost(t, rt.store)
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

func TestInjectWithoutApproveDoesNotWritePTY(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()

	if err := sess.Inject(alice.ID, []byte("rm -rf /\n"), inject.Approval{}); !errors.Is(err, inject.ErrNotApproved) {
		t.Fatalf("unapproved inject err = %v", err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received unapproved inject %q", leak)
	case <-time.After(80 * time.Millisecond):
	}

	if err := sess.Inject(alice.ID, []byte("ls\n"), inject.Approve()); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "ls\n" {
			t.Fatalf("got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for approved inject")
	}
}

func TestInjectApprovedStillRequiresDriver(t *testing.T) {
	_, sess, _ := spawnReady(t)
	_, _, _ = sess.Subscribe(Join{Name: "alice"})
	bob, _, _ := sess.Subscribe(Join{Name: "bob"})
	if err := sess.Inject(bob.ID, []byte("ls\n"), inject.Approve()); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("follower inject err = %v", err)
	}
}

func TestInjectTicketRejectsReplayAndClientFlag(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})

	got := make(chan []byte, 1)
	readPTY := func() {
		go func() {
			buf := make([]byte, 32)
			n, _ := fb.inR.Read(buf)
			got <- append([]byte(nil), buf[:n]...)
		}()
	}

	readPTY()
	if err := sess.InjectTicket(alice.ID, "ls\n", ""); !errors.Is(err, inject.ErrNotApproved) {
		t.Fatalf("empty nonce err = %v", err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received empty-nonce inject %q", leak)
	case <-time.After(80 * time.Millisecond):
	}

	nonce, err := sess.IssueInjectTicket(alice.ID, "ls\n")
	if err != nil || nonce == "" {
		t.Fatalf("issue nonce=%q err=%v", nonce, err)
	}
	readPTY()
	if err := sess.InjectTicket(alice.ID, "ls\n", nonce); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "ls\n" {
			t.Fatalf("got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ticket inject")
	}
	if err := sess.InjectTicket(alice.ID, "ls\n", nonce); !errors.Is(err, inject.ErrBadTicket) {
		t.Fatalf("replay err = %v", err)
	}
}

func TestKeystrokeFromPaletteOrChromeDoesNotWritePTY(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()

	for _, focused := range []inject.Surface{"", inject.SurfacePalette, inject.SurfaceChrome} {
		if err := sess.Keystroke(alice.ID, []byte("i"), focused); !errors.Is(err, inject.ErrNotTerminalFocus) {
			t.Fatalf("focused %q err = %v", focused, err)
		}
	}
	if err := sess.Inject(alice.ID, []byte("rm\n"), inject.Approval{}); !errors.Is(err, inject.ErrNotApproved) {
		t.Fatalf("palette inject without approve err = %v", err)
	}
	select {
	case leak := <-got:
		t.Fatalf("PTY received chrome/palette input %q", leak)
	case <-time.After(80 * time.Millisecond):
	}

	if err := sess.Keystroke(alice.ID, []byte("x"), inject.SurfaceTerm); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "x" {
			t.Fatalf("got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for terminal keystroke")
	}
}

func TestApprovedInjectWorksWhilePaletteFocused(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := fb.inR.Read(buf)
		done <- string(buf[:n])
	}()
	if err := sess.Keystroke(alice.ID, []byte("i"), inject.SurfacePalette); !errors.Is(err, inject.ErrNotTerminalFocus) {
		t.Fatalf("palette keystroke err = %v", err)
	}
	if err := sess.Inject(alice.ID, []byte("ls\n"), inject.Approve()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got != "ls\n" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for approved inject")
	}
}

func TestSetFocusPaletteBlocksKeystrokeUntilTermRestored(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	if sess.Focus(alice.ID) != inject.SurfaceTerm {
		t.Fatalf("default focus = %q", sess.Focus(alice.ID))
	}

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()

	if err := sess.SetFocus(alice.ID, inject.SurfacePalette); err != nil {
		t.Fatal(err)
	}
	if sess.Focus(alice.ID) != inject.SurfacePalette {
		t.Fatalf("palette focus = %q", sess.Focus(alice.ID))
	}
	if err := sess.Keystroke(alice.ID, []byte("i"), sess.Focus(alice.ID)); !errors.Is(err, inject.ErrNotTerminalFocus) {
		t.Fatalf("palette keystroke err = %v", err)
	}
	if err := sess.Write(alice.ID, []byte("live")); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "live" {
			t.Fatalf("Write while palette focused got %q, want live WS still ungated", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ungated Write")
	}

	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()
	if err := sess.Inject(alice.ID, []byte("ls\n"), inject.Approve()); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "ls\n" {
			t.Fatalf("approved inject got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for approved inject")
	}

	if err := sess.SetFocus(alice.ID, inject.SurfaceTerm); err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()
	if err := sess.Keystroke(alice.ID, []byte("x"), sess.Focus(alice.ID)); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "x" {
			t.Fatalf("restored term keystroke got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for restored keystroke")
	}
}

func TestSetFocusChromeBlocksKeystrokeUntilTermRestored(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	if sess.Focus(alice.ID) != inject.SurfaceTerm {
		t.Fatalf("default focus = %q", sess.Focus(alice.ID))
	}

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()

	if err := sess.SetFocus(alice.ID, inject.SurfaceChrome); err != nil {
		t.Fatal(err)
	}
	if sess.Focus(alice.ID) != inject.SurfaceChrome {
		t.Fatalf("chrome focus = %q", sess.Focus(alice.ID))
	}
	if err := sess.Keystroke(alice.ID, []byte("i"), sess.Focus(alice.ID)); !errors.Is(err, inject.ErrNotTerminalFocus) {
		t.Fatalf("chrome keystroke err = %v", err)
	}
	if err := sess.Write(alice.ID, []byte("live")); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "live" {
			t.Fatalf("Write while chrome focused got %q, want live WS still ungated", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ungated Write")
	}

	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()
	if err := sess.Inject(alice.ID, []byte("ls\n"), inject.Approve()); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "ls\n" {
			t.Fatalf("approved inject got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for approved inject")
	}

	if err := sess.SetFocus(alice.ID, inject.SurfaceTerm); err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 32)
		n, _ := fb.inR.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()
	if err := sess.Keystroke(alice.ID, []byte("x"), sess.Focus(alice.ID)); err != nil {
		t.Fatal(err)
	}
	select {
	case hit := <-got:
		if string(hit) != "x" {
			t.Fatalf("restored term keystroke got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for restored keystroke")
	}
}

func TestKeystrokeWriteDoesNotNeedInjectApprove(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := fb.inR.Read(buf)
		done <- string(buf[:n])
	}()
	if err := sess.Write(alice.ID, []byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got != "x" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for keystroke")
	}
}

func TestReadScrollbackRedactsByDefault(t *testing.T) {
	_, sess, fb := spawnReady(t)
	cl, ch, _ := sess.Subscribe(Join{Name: "alice"})
	const secret = "super-secret-token"
	payload := "ok password=" + secret + " done\n"
	if _, err := fb.outW.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	waitBytes(t, ch, payload)

	got := string(sess.ReadScrollback())
	if strings.Contains(got, secret) {
		t.Fatalf("read path leaked secret: %q", got)
	}
	if !strings.Contains(got, "ok password=") || !strings.Contains(got, "done") {
		t.Fatalf("innocent scrollback missing: %q", got)
	}

	_, _, snap := sess.Subscribe(Join{Name: "replay"})
	if !strings.Contains(string(snap), secret) {
		t.Fatal("live PTY replay snapshot must stay raw")
	}
	sess.Unsubscribe(cl.ID)
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

func TestWebRTCJoinerIsNeverOwner(t *testing.T) {
	_, sess, _ := spawnReady(t)
	guest, _, _ := sess.Subscribe(Join{Name: "guest", Kind: "webrtc"})
	if guest.Role != RoleFollower {
		t.Fatalf("webrtc first role = %s", guest.Role)
	}
	info := sess.Info()
	if info.OwnerID != 0 || info.DriverID != 0 {
		t.Fatalf("webrtc-only owner/driver = %d/%d", info.OwnerID, info.DriverID)
	}
	if err := sess.TakeControl(guest.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("take = %v", err)
	}
	if err := sess.Write(guest.ID, []byte("x")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("write = %v", err)
	}
}

func TestOwnerLeaveDoesNotPromoteWebRTC(t *testing.T) {
	_, sess, _ := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice", Kind: "web"})
	guest, _, _ := sess.Subscribe(Join{Name: "guest", Kind: "webrtc"})
	pane, _, _ := sess.Subscribe(Join{Name: "pane", Kind: "web"})
	sess.Unsubscribe(alice.ID)
	info := sess.Info()
	if info.OwnerID != pane.ID || info.DriverID != pane.ID {
		t.Fatalf("promoted owner/driver = %d/%d want local pane %d (not webrtc %d)", info.OwnerID, info.DriverID, pane.ID, guest.ID)
	}
}

func TestOwnerLeaveWebRTCStaysFollow(t *testing.T) {
	_, sess, _ := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice", Kind: "web"})
	guest, _, _ := sess.Subscribe(Join{Name: "guest", Kind: "webrtc"})
	sess.Unsubscribe(alice.ID)
	info := sess.Info()
	if info.OwnerID != 0 || info.DriverID != 0 {
		t.Fatalf("after host detach owner/driver = %d/%d", info.OwnerID, info.DriverID)
	}
	if err := sess.Write(guest.ID, []byte("nope")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("webrtc write after host leave = %v", err)
	}
	bob, _, _ := sess.Subscribe(Join{Name: "bob", Kind: "web"})
	if bob.Role != RoleDriver {
		t.Fatalf("local reattach role = %s", bob.Role)
	}
	if sess.Info().OwnerID != bob.ID {
		t.Fatal("local reattach should become owner")
	}
}

func TestGrantControlToWebRTC(t *testing.T) {
	_, sess, fb := spawnReady(t)
	alice, _, _ := sess.Subscribe(Join{Name: "alice"})
	guest, _, _ := sess.Subscribe(Join{Name: "guest", Kind: "webrtc"})
	if err := sess.GrantControl(alice.ID, guest.ID); err != nil {
		t.Fatal(err)
	}
	if sess.Info().DriverID != guest.ID {
		t.Fatal("guest should be driving")
	}
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := fb.inR.Read(buf)
		done <- string(buf[:n])
	}()
	if err := sess.Write(guest.ID, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got != "ok" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for webrtc driver write")
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
	host := firstHost(t, st)
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

func waitBridges(t *testing.T, s *Session, mu *sync.Mutex, bridges *[]*fakeBridge, min int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*bridges)
		mu.Unlock()
		if n >= min && s.Info().Status == StatusReady {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	n := len(*bridges)
	mu.Unlock()
	t.Fatalf("expected %d bridges, opener calls = %d status=%s", min, n, s.Info().Status)
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

func TestConnectUnknownHostKey(t *testing.T) {
	st := testStore(t)
	host := firstHost(t, st)
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		return nil, &store.UnknownHostError{Host: "box:22", Fingerprint: "SHA256:abc"}
	})
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := sess.Info()
		if got.Status == StatusError {
			if got.ErrorKind != "unknown-host" || got.HostKeyHost != "box:22" || got.HostKeyFingerprint != "SHA256:abc" {
				t.Fatalf("info = %+v", got)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session never recorded unknown host: %+v", sess.Info())
}

func sshHost(t *testing.T, st *store.Store) store.Host {
	t.Helper()
	h, err := st.UpsertHost(store.Host{
		Name: "box", Address: "10.0.0.1", Username: "u", AuthType: "password",
		Password: "x", Color: "#6ea8ff",
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestReconnectPreservesScrollback(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	host.NoAutoReconnect = true
	if _, err := st.UpsertHost(host); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bridges []*fakeBridge
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		bridges = append(bridges, fb)
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)

	cl, ch, _ := sess.Subscribe(Join{Name: "alice"})
	const marker = "pre-reconnect-scrollback-marker"
	const secret = "leaked-reconnect-secret"
	payload := marker + " password=" + secret + "\n"
	if _, err := bridges[0].outW.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	waitBytes(t, ch, payload)
	ring := sess.buf

	if _, err := rt.Reconnect(info.ID); err != nil {
		t.Fatal(err)
	}
	waitBridges(t, sess, &mu, &bridges, 2)
	if sess.buf != ring {
		t.Fatal("reconnect replaced the scrollback ring")
	}

	got := string(sess.ReadScrollback())
	if len(got) == 0 {
		t.Fatal("ReadScrollback empty after reconnect")
	}
	if !strings.Contains(got, marker) {
		t.Fatalf("scrollback lost marker after reconnect: %q", got)
	}
	if strings.Contains(got, secret) {
		t.Fatalf("read path leaked secret after reconnect: %q", got)
	}

	sess.Unsubscribe(cl.ID)
	_, _, snap := sess.Subscribe(Join{Name: "bob"})
	if len(snap) == 0 {
		t.Fatal("Subscribe snapshot empty after reconnect")
	}
	if !strings.Contains(string(snap), marker) {
		t.Fatalf("live snapshot lost marker after reconnect: %q", snap)
	}
	if !strings.Contains(string(snap), secret) {
		t.Fatal("live PTY replay snapshot must stay raw after reconnect")
	}
}

func TestAutoReconnectAfterDrop(t *testing.T) {
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
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	cl, ch, _ := sess.Subscribe(Join{Name: "alice"})
	const marker = "pre-flap-scrollback-marker"
	if _, err := bridges[0].outW.Write([]byte(marker)); err != nil {
		t.Fatal(err)
	}
	waitBytes(t, ch, marker)
	_ = bridges[0].Close()
	waitBridges(t, sess, &mu, &bridges, 2)
	if sess.Info().Status != StatusReady {
		t.Fatalf("status = %s", sess.Info().Status)
	}

	sess.Unsubscribe(cl.ID)
	_, ch2, snap := sess.Subscribe(Join{Name: "bob"})
	if len(snap) == 0 {
		t.Fatal("Subscribe snapshot empty after connection flap")
	}
	if !strings.Contains(string(snap), marker) {
		t.Fatalf("snapshot lost pre-flap scrollback: %q", snap)
	}
	gotRead := string(sess.ReadScrollback())
	if len(gotRead) == 0 || !strings.Contains(gotRead, marker) {
		t.Fatalf("ReadScrollback empty/missing after flap: %q", gotRead)
	}

	if _, err := bridges[1].outW.Write([]byte("back")); err != nil {
		t.Fatal(err)
	}
	if got := waitBytes(t, ch2, "back"); !strings.Contains(got, "back") {
		t.Fatalf("got %q", got)
	}
}

func TestCleanExitDoesNotReconnect(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	var mu sync.Mutex
	var n int
	fb := newFakeBridge()
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	_ = fb.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sess.Info().Status == StatusExited {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if sess.Info().Status != StatusExited {
		t.Fatalf("status = %s", sess.Info().Status)
	}
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	calls := n
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("clean exit should not reconnect, calls=%d", calls)
	}
}

func TestManualReconnectAfterExit(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	var mu sync.Mutex
	var bridges []*fakeBridge
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		bridges = append(bridges, fb)
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	_ = bridges[0].Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sess.Info().Status == StatusExited {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := rt.Reconnect(info.ID); err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	mu.Lock()
	n := len(bridges)
	mu.Unlock()
	if n < 2 {
		t.Fatalf("manual reconnect calls = %d", n)
	}
}

func TestManualReconnectWhileConnected(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	host.NoAutoReconnect = true
	if _, err := st.UpsertHost(host); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bridges []*fakeBridge
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		bridges = append(bridges, fb)
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	if _, err := rt.Reconnect(info.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(bridges)
		mu.Unlock()
		if n >= 2 && sess.Info().Status == StatusReady {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	n := len(bridges)
	mu.Unlock()
	t.Fatalf("manual reconnect while connected calls = %d status=%s", n, sess.Info().Status)
}

func TestReconnectDuringExitRestartsLoop(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	host.NoAutoReconnect = true
	if _, err := st.UpsertHost(host); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bridges []*fakeBridge
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		defer mu.Unlock()
		fb := newFakeBridge()
		bridges = append(bridges, fb)
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	_ = bridges[0].Close()
	for i := 0; i < 8; i++ {
		if _, err := rt.Reconnect(info.ID); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	waitReady(t, sess)
	mu.Lock()
	n := len(bridges)
	mu.Unlock()
	if n < 2 {
		t.Fatalf("reconnect during exit calls = %d status=%s", n, sess.Info().Status)
	}
}

func TestDialFailureRetriesThenConnects(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	var mu sync.Mutex
	var n int
	fb := newFakeBridge()
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			return nil, errors.New("ssh dial 10.0.0.1:22: connection refused")
		}
		return fb, nil
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, sess)
	mu.Lock()
	calls := n
	mu.Unlock()
	if calls < 2 {
		t.Fatalf("expected retry after dial failure, calls=%d", calls)
	}
}

func TestManualReconnectConnectErrorDoesNotLoop(t *testing.T) {
	st := testStore(t)
	host := sshHost(t, st)
	host.NoAutoReconnect = true
	if _, err := st.UpsertHost(host); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var n int
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return nil, errors.New("ssh dial 10.0.0.1:22: connection refused")
	})
	rt.backoff = time.Millisecond
	info, err := rt.Spawn(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sess.Info().Status == StatusError {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := rt.Reconnect(info.ID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sess.Info().Status == StatusError {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	calls := n
	mu.Unlock()
	if calls > 3 {
		t.Fatalf("manual reconnect should not spin, calls=%d", calls)
	}
	if sess.Info().Status != StatusError {
		t.Fatalf("status = %s", sess.Info().Status)
	}
}
