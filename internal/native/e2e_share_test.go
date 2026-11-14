//go:build nativee2e

package native

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/joinpeer"
)

// Share and Join end to end. The owner is a window on a scratch keel serve with KEEL_SIGNAL on a loopback
// keel-signal; the joiner is a second window on its own scratch keel serve, as a second machine would be. The joiner's
// peers have no ICE server (e2eJoiner) and the signal server is loopback. The one request that leaves the machine is
// the STUN binding request keel serve's own share hub makes after Allow (internal/share/hub.go acceptPeer), which
// the maintainer allowed for these tests.

// e2eJoiner opens the peer of a join with no ICE server and loopback candidates, and keeps it for the assertions.
func (e *win) joiner(l joinpeer.Link, name string) (session, error) {
	p, err := joinpeer.Open(l, name, joinpeer.WithICEServers(nil), joinpeer.WithLoopback())
	if err != nil {
		return nil, err
	}
	t := &teeConn{Peer: p}
	e.peers, e.tees = append(e.peers, p), append(e.tees, t)
	return t, nil
}

// teeConn is a joined pane's connection that keeps a copy of what the terminal read, so a test can read the joined
// terminal's text (the terminal view has no text accessor).
type teeConn struct {
	*joinpeer.Peer
	mu  sync.Mutex
	buf strings.Builder
}

func (t *teeConn) Read(b []byte) (int, error) {
	n, err := t.Peer.Read(b)
	t.mu.Lock()
	t.buf.Write(b[:n])
	t.mu.Unlock()
	return n, err
}

func (t *teeConn) text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

// wait30 is the bound for a WebRTC connect: the hub's STUN request may go unanswered.
const wait30 = 30 * time.Second

func pump30(t *testing.T, wins []*win, cond func() bool, what string) {
	t.Helper()
	for end := time.Now().Add(wait30); ; time.Sleep(20 * time.Millisecond) {
		for _, w := range wins {
			w.step()
		}
		if cond() {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("timed out (30 s) waiting for %s", what)
		}
	}
}

// bodyInk counts the drawn pixels of a joined pane's terminal, below its bar and banner.
func (e *win) bodyInk() int {
	r, ok := e.tt.Find("pane " + e.w.focusedPane().id)
	if !ok {
		return 0
	}
	return e.inked(ui.Rect{X: r.X, Y: r.Y + 110, W: r.W, H: r.H - 110})
}

// admitAndWait presses Allow on A and waits for B's joined tab to be connected; it returns the seconds it took.
func (r *shareE2E) admitAndWait(t *testing.T) time.Duration {
	t.Helper()
	pump30(t, r.wins(), func() bool { return r.a.hasLabel("Allow") }, "A's prompt")
	start := time.Now()
	r.a.click("Allow")
	pump30(t, r.wins(), func() bool {
		return r.b.snap().Role == "follower" && r.b.snap().Status == "ready" && len(r.a.snap().Attached) == 2
	}, "B connected over WebRTC")
	return time.Since(start)
}

// noOffMachine asserts what the plan asks of every scenario: the window's peers were built with an empty ICE list and
// dial a loopback signal server.
func (e *win) noOffMachine() {
	e.t.Helper()
	for _, p := range e.peers {
		if len(p.ICEServers()) != 0 {
			e.t.Errorf("a peer was built with ICE servers: %v", p.ICEServers())
		}
		if !strings.HasPrefix(p.SignalURL(), "ws://127.0.0.1:") {
			e.t.Errorf("a peer dials %s", strings.SplitN(p.SignalURL(), "?", 2)[0])
		}
	}
}

// startSignal runs bin/keel-signal on a free loopback port and returns its URL.
func startSignal(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(repoRoot(t), "bin", "keel-signal")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("%s is missing: run make native-e2e (it builds bin/keel-signal first)", bin)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	logf, err := os.Create(filepath.Join(dir, "signal.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "KEEL_SIGNAL_ADDR="+addr)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	var once sync.Once
	t.Cleanup(func() {
		once.Do(func() {
			cmd.Process.Signal(syscall.SIGTERM) // our own child, by handle
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				cmd.Process.Kill()
				<-done
			}
			logf.Close()
		})
	})
	for end := time.Now().Add(e2eWait); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if r, err := http.Get("http://" + addr + "/healthz"); err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return "http://" + addr
			}
		}
		select {
		case <-done:
			t.Fatalf("keel-signal exited early; see %s", filepath.Join(dir, "signal.log"))
		default:
		}
	}
	t.Fatalf("keel-signal did not come up; see %s", filepath.Join(dir, "signal.log"))
	return ""
}

// noPasteboard is a directory with a pbcopy that does nothing: keel serve puts a new link on the system pasteboard
// (internal/httpapi shareSession), which a test must not do to the person running it.
func noPasteboard(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "pbcopy"), []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin + string(os.PathListSeparator) + os.Getenv("PATH")
}

// shareRig is the owner A (on a keel serve with the signal server) and the joiner B (its own keel serve and data).
type shareE2E struct {
	a, b *win
	sid  string // A's session
	sig  string
}

func newShareE2E(t *testing.T, withSignal bool) *shareE2E {
	t.Helper()
	dir := scratchDir(t)
	r := &shareE2E{}
	env := []string{"PATH=" + noPasteboard(t, dir)}
	if withSignal {
		r.sig = startSignal(t, dir)
		env = append(env, "KEEL_SIGNAL="+r.sig)
	}
	addrA, _ := startServeAt(t, dir, "serve-a", true, env...)
	dirB := filepath.Join(dir, "b")
	if err := os.MkdirAll(filepath.Join(dirB, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	addrB, _ := startServeAt(t, dirB, "serve-b", true, "PATH="+noPasteboard(t, dirB))
	r.a = newWin(t, addrA, filepath.Join(dir, "data"), nil)
	r.b = newWin(t, addrB, filepath.Join(dirB, "data"), nil)
	r.sid = r.a.openLocal()
	return r
}

var linkRE = regexp.MustCompile(`^http://127\.0\.0\.1:\d+/join\?room=[^&#]+#k=[0-9a-f]{32}$`)

// openShare clicks Share on A and waits until the popover has its link; it returns the link (kept in memory: the
// key is never printed or written).
func (r *shareE2E) openShare() string {
	r.a.t.Helper()
	r.a.click("Share")
	pumpAll(r.a.t, r.wins(), func() bool { return r.a.w.share != nil && r.a.w.share.status == "ready" && r.a.hasLabel("Copied") }, "the Share popover with its link")
	return r.a.tt.Clipboard()
}

func (r *shareE2E) wins() []*win { return []*win{r.a, r.b} }

// joinWith opens Join a session on B, pastes link and presses Join.
func (r *shareE2E) joinWith(link string) {
	r.b.t.Helper()
	r.b.key(ui.Cmd, ui.KeyJ)
	r.b.pump(func() bool { return r.b.hasLabel("dialog Join a session") }, "the Join a session dialog")
	r.b.fillField("Join link", link)
	r.b.click("Join")
}

func TestE2EShareAndAdmissionPrompt(t *testing.T) {
	r := newShareE2E(t, true)
	link := r.openShare()
	if !linkRE.MatchString(link) {
		t.Fatalf("the link on the clipboard does not have the shape /join?room=…#k=<32 hex> on loopback")
	}
	for _, s := range []string{"Share session", "Live link", "Join link", "Copied", "In this session · 1", "owner · this window", "Stop sharing"} {
		if !r.a.tt.HasText(s) && !r.a.hasLabel(s) {
			t.Errorf("the popover lacks %q", s)
		}
	}
	if !r.a.tt.HasText(link) { // the field shows the whole link; the shot holds a scratch room's link
		t.Fatalf("the Join link field does not show the link")
	}
	r.a.shot("TestE2EShareAndAdmissionPrompt-popover")

	r.joinWith(link)
	pumpAll(t, r.wins(), func() bool {
		return r.b.w.tabCount() == 2 && r.b.w.activeTab().remote && r.b.hasLabel("banner joining") && r.a.hasLabel("alertdialog "+r.b.w.joinName())
	}, "B waiting for the host and A's prompt")
	if !r.a.tt.HasText("wants to watch Local Shell.") || !r.a.hasLabel("Share requests") {
		t.Errorf("A's prompt: %q", r.a.tt.Texts())
	}
	r.a.shot("TestE2EShareAndAdmissionPrompt-admit")
	r.b.shot("TestE2EShareAndAdmissionPrompt-joiner-waiting")
	if r.b.hasLabel("sidebar hosts") || r.b.hasLabel("Share") || r.b.hasLabel("Toggle sidebar") {
		t.Error("B's joined tab shows the Vault sidebar or Share")
	}

	// Nobody is let in: the prompt stays until A answers, whatever B does.
	r.a.settle(500 * time.Millisecond)
	if !r.a.hasLabel("Allow") || len(r.a.snap().Attached) != 1 {
		t.Fatal("the prompt went away or someone attached without Allow")
	}

	// Deny: B shows the refusal and has no terminal output, ever.
	r.a.click("Deny")
	pumpAll(t, r.wins(), func() bool {
		return r.b.snap().Status == "error" && !r.a.hasLabel("Allow") && r.b.hasLabel("banner failed")
	}, "B refused and A's prompt answered")
	if r.b.snap().Message != "The host declined your request to join." || !r.b.tt.HasText("The host declined your request to join.") {
		t.Errorf("B's message %q", r.b.snap().Message)
	}
	if len(r.a.snap().Attached) != 1 {
		t.Errorf("A has %d attached after Deny", len(r.a.snap().Attached))
	}
	r.b.shot("TestE2EShareAndAdmissionPrompt-joiner-denied")
	r.a.noOffMachine()
	r.b.noOffMachine()
	if len(r.b.peers) != 1 {
		t.Errorf("B made %d peers", len(r.b.peers))
	}
}

func TestE2EStopWhileARequestWaits(t *testing.T) {
	r := newShareE2E(t, true)
	old := r.openShare()
	r.joinWith(old)
	pumpAll(t, r.wins(), func() bool { return r.a.hasLabel("Allow") && r.b.hasLabel("banner joining") }, "A's prompt and B waiting")

	// Stop sharing while a request waits: the prompt goes, B is told the host left, and nothing was admitted.
	r.a.click("Stop sharing")
	pumpAll(t, r.wins(), func() bool {
		return r.a.w.share == nil && !r.a.hasLabel("Allow") && r.b.tt.HasText("Host left")
	}, "A's popover and prompt gone and B told")
	if !r.a.tt.HasText("Sharing stopped. The join link no longer works.") {
		t.Error("no confirmation on A")
	}
	if n := len(r.a.snap().Attached); n != 1 {
		t.Errorf("A has %d attached", n)
	}
	r.b.click("Leave")
	pumpAll(t, r.wins(), func() bool { return r.b.w.tabCount() == 1 }, "B's tab left")

	// The old link does not admit: B's join does not even start.
	r.joinWith(old)
	pumpAll(t, r.wins(), func() bool { return r.b.tt.HasText("join: signal server: websocket: bad handshake") }, "the old link refused")
	r.settle(300 * time.Millisecond)
	if r.a.hasLabel("Allow") || r.b.w.tabCount() != 1 {
		t.Error("the old link was accepted")
	}

	// A new Share gives a different link, which asks again.
	fresh := r.openShare()
	if fresh == old || !linkRE.MatchString(fresh) {
		t.Fatal("a new Share did not give a new link")
	}
	r.joinWith(fresh)
	pumpAll(t, r.wins(), func() bool { return r.a.hasLabel("Allow") }, "A asked again")
	r.a.click("Deny")
	pumpAll(t, r.wins(), func() bool { return r.b.snap().Status == "error" }, "B refused")
	r.a.noOffMachine()
	r.b.noOffMachine()
}

func (r *shareE2E) settle(d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		r.a.step()
		r.b.step()
	}
}

func TestE2ENoSignalServer(t *testing.T) {
	r := newShareE2E(t, false)
	r.a.click("Share")
	pumpAll(t, r.wins(), func() bool { return r.a.w.share != nil && r.a.w.share.status == "error" }, "the popover's error")
	if !r.a.tt.HasText("set a signal server in Settings · Sharing to share") || !r.a.tt.HasText("Not shared") {
		t.Errorf("popover: %q", r.a.tt.Texts())
	}
	if r.a.hasLabel("Stop sharing") || r.a.hasLabel("Join link") {
		t.Error("an error popover offers a link or Stop sharing")
	}
	if r.a.tt.Clipboard() != "" {
		t.Error("nothing was copied")
	}
	r.a.shot("TestE2ENoSignalServer-popover")
}

// A second window on the same keel serve follows like a joiner would, over the session socket instead of WebRTC: it
// takes no hub and no STUN, so the control rules can run for real. Its typing goes nowhere until A hands over.
func TestE2EFollowerAsksForControlAndTheOwnerAnswers(t *testing.T) {
	addr, dir := startServe(t, true)
	a := newWin(t, addr, dir, nil)
	sid := a.openLocal()
	a.clickBody("1")
	a.typeLine("echo from-a")
	a.waitScrollback(sid, "from-a")
	b := newWin(t, addr, dir, nil)
	both := []*win{a, b}
	pumpAll(t, both, func() bool {
		return b.w.tabCount() == 1 && b.snap().Role == "follower" && len(a.snap().Attached) == 2 && b.hasLabel("Ask for control")
	}, "B following")

	// B types: nothing is sent, and the notice offers Ask for control.
	b.clickBody("1")
	b.typeLine("echo from-b")
	pumpAll(t, both, func() bool { return b.tt.HasText("Typing is off while you follow") }, "the follow notice")
	b.settle(500 * time.Millisecond)
	a.step()
	if strings.Contains(a.scrollback(sid), "from-b") {
		t.Fatal("a follower's keystrokes reached the PTY")
	}
	if b.snap().Blocked == 0 {
		t.Error("the dropped keystrokes were not counted")
	}

	// Ask for control: A is asked, with Hand over and Keep driving. Keep driving sends nothing.
	b.click("Ask for control")
	pumpAll(t, both, func() bool { return a.hasLabel("Hand over") && a.hasLabel("Keep driving") }, "A's control request")
	if !a.tt.HasText("asks for control") || !a.tt.HasText("You follow until you take it back.") {
		t.Errorf("A's prompt: %q", a.tt.Texts())
	}
	a.shot("TestE2EFollowerAsksForControlAndTheOwnerAnswers-asks")
	a.click("Keep driving")
	pumpAll(t, both, func() bool { return !a.hasLabel("Hand over") }, "the prompt dismissed")
	b.settle(500 * time.Millisecond)
	if b.snap().Role != "follower" || a.snap().Role != "driver" {
		t.Fatalf("roles after Keep driving: A %s B %s", a.snap().Role, b.snap().Role)
	}

	// Ask again; Hand over: B drives and its typing reaches the PTY.
	b.click("Ask for control")
	pumpAll(t, both, func() bool { return a.hasLabel("Hand over") }, "A's second control request")
	a.click("Hand over")
	pumpAll(t, both, func() bool { return b.snap().Role == "driver" && a.snap().Role == "owner" }, "B driving")
	b.clickBody("1")
	b.typeLine("echo b-drives")
	a.waitScrollback(sid, "b-drives")
	a.shot("TestE2EFollowerAsksForControlAndTheOwnerAnswers-handed-over")

	// A takes control back: B's typing is off again.
	a.click("Take control")
	pumpAll(t, both, func() bool { return a.snap().Role == "driver" && b.snap().Role == "follower" }, "A driving again")
	b.clickBody("1")
	b.typeLine("echo b-again")
	b.settle(500 * time.Millisecond)
	a.step()
	if strings.Contains(a.scrollback(sid), "b-again") {
		t.Fatal("B typed after A took control back")
	}
}

// The whole path: A shares, B joins by the link, A allows, A types, B's terminal shows the output. The hub's own
// STUN request (internal/share/hub.go acceptPeer) is the one off-machine request the maintainer allowed;
// B's peer has no ICE server and the signal server is loopback.
func TestE2EShareAndJoin(t *testing.T) {
	r := newShareE2E(t, true)
	link := r.openShare()
	r.joinWith(link)
	took := r.admitAndWait(t)
	t.Logf("WebRTC connect after Allow: %v", took)
	r.b.noOffMachine()
	if len(r.b.tees) != 1 {
		t.Fatalf("B made %d peers", len(r.b.tees))
	}
	if !r.b.hasLabel("Ask for control") || !r.b.hasLabel("Leave") || r.b.hasLabel("sidebar hosts") {
		t.Errorf("B's banner: %q", r.b.tt.Texts())
	}
	before := r.b.bodyInk()
	// The popover is still open on A and, at the theme's sizes, covers the pane's centre: click the pane's left side, outside it.
	pr, _ := r.a.tt.Find("pane 1")
	r.a.tt.ClickAt(pr.X+pr.W/4, pr.Y+pr.H/2)
	r.a.step()
	r.a.typeLine("echo hello-$((1+1))")
	pump30(t, r.wins(), func() bool { return strings.Contains(r.b.tees[0].text(), "hello-2") }, "hello-2 in B's terminal")
	pump30(t, r.wins(), func() bool { return r.b.bodyInk() > before }, "B's terminal drawn")
	if !strings.Contains(r.a.scrollback(r.sid), "hello-2") {
		t.Error("A's scrollback lacks the output")
	}
	if !r.b.hasLabel("role You follow") || !textHas2(r.b, "'s session on Local Shell · read-only") || !r.b.hasLabel("role "+r.a.snap().Attached[0].Name+" is driving") {
		t.Errorf("B's banner or chips: %q", r.b.tt.Texts())
	}
	r.b.shot("TestE2EShareAndJoin-joined")

	// A: "1 watching" and the people list with their roles.
	pumpAll(t, r.wins(), func() bool { return r.a.hasLabel("1 watching") }, "A's watchers badge")
	r.a.click("1 watching")
	pumpAll(t, r.wins(), func() bool { return r.a.w.share != nil }, "the popover")
	for _, s := range []string{"In this session · 2", "joined by link", "Following", "Driving", "owner · this window"} {
		if !r.a.tt.HasText(s) && !textHas2(r.a, s) {
			t.Errorf("A's people list lacks %q", s)
		}
	}
	r.a.shot("TestE2EShareAndJoin-people")
	r.a.noOffMachine()
}

func textHas2(e *win, s string) bool {
	for _, x := range e.tt.Texts() {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

func TestE2EJoinerCannotTypeUntilHandedOver(t *testing.T) {
	r := newShareE2E(t, true)
	link := r.openShare()
	r.joinWith(link)
	r.admitAndWait(t)
	r.a.click("Share") // close the popover
	pid := r.b.w.focusedPane().id
	r.b.clickBody(pid)
	r.b.typeLine("echo from-b")
	pump30(t, r.wins(), func() bool { return r.b.tt.HasText("Typing is off while you follow") }, "B's notice")
	r.settle(1 * time.Second)
	if strings.Contains(r.a.scrollback(r.sid), "from-b") || strings.Contains(r.b.tees[0].text(), "from-b") {
		t.Fatal("what a follower typed reached the host")
	}
	r.b.tt.Advance(toastTimeout + time.Second) // the notice's own "Ask for control" is gone: the banner's is the one
	r.b.step()
	r.b.click("Ask for control")
	pump30(t, r.wins(), func() bool { return r.a.hasLabel("Hand over") }, "A's request prompt")
	if !r.a.tt.HasText("asks for control") {
		t.Errorf("A's prompt: %q", r.a.tt.Texts())
	}
	r.a.shot("TestE2EJoinerCannotTypeUntilHandedOver-asks")
	r.a.click("Keep driving")
	r.settle(1 * time.Second)
	if r.b.snap().Role != "follower" {
		t.Fatalf("B's role after Keep driving: %s", r.b.snap().Role)
	}
	r.b.click("Ask for control")
	pump30(t, r.wins(), func() bool { return r.a.hasLabel("Hand over") }, "A's second request")
	r.a.click("Hand over")
	pump30(t, r.wins(), func() bool { return r.b.snap().Role == "driver" && r.a.snap().Role == "owner" }, "B driving")
	if !textHas2(r.b, "you have control") || r.b.hasLabel("role You follow") {
		t.Errorf("B after the hand-over: %q", r.b.tt.Texts())
	}
	r.b.clickBody(pid)
	r.b.typeLine("echo b-drives")
	r.a.pump(func() bool { return strings.Contains(r.a.scrollback(r.sid), "b-drives") }, "b-drives in A's scrollback")
	r.b.shot("TestE2EJoinerCannotTypeUntilHandedOver-driving")

	// A takes control back: B's typing is off again.
	r.a.click("Take control")
	pump30(t, r.wins(), func() bool { return r.a.snap().Role == "driver" && r.b.snap().Role == "follower" }, "A driving again")
	r.b.clickBody(pid)
	r.b.typeLine("echo b-again")
	r.settle(1 * time.Second)
	if strings.Contains(r.a.scrollback(r.sid), "b-again") {
		t.Fatal("B typed after A took control back")
	}
	r.a.noOffMachine()
	r.b.noOffMachine()
}

func TestE2EDenyAllowAndStop(t *testing.T) {
	r := newShareE2E(t, true)
	old := r.openShare()
	r.joinWith(old)
	pump30(t, r.wins(), func() bool { return r.a.hasLabel("Deny") }, "A's prompt")
	r.a.click("Deny")
	pump30(t, r.wins(), func() bool { return r.b.snap().Status == "error" }, "B refused")
	r.b.click("Leave")
	pump30(t, r.wins(), func() bool { return r.b.w.tabCount() == 1 }, "B's tab left")

	// A second request is allowed.
	r.joinWith(old)
	r.admitAndWait(t)
	r.b.shot("TestE2EDenyAllowAndStop-connected")

	// Stop sharing: B's tab shows the ended state, and the old link does not admit.
	if r.a.w.share == nil {
		r.a.click("Share")
	}
	r.a.click("Stop sharing")
	pump30(t, r.wins(), func() bool {
		return r.b.tt.HasText("Host left") && strings.Contains(r.b.tees[len(r.b.tees)-1].text(), "Host detached.") && r.b.snap().Status != "ready"
	}, "B told the host left")
	if len(r.a.snap().Attached) != 1 {
		t.Errorf("A has %d attached after Stop", len(r.a.snap().Attached))
	}
	r.b.shot("TestE2EDenyAllowAndStop-ended")
	r.b.click("Leave")
	pump30(t, r.wins(), func() bool { return r.b.w.tabCount() == 1 }, "B's tab left")
	r.joinWith(old)
	pump30(t, r.wins(), func() bool { return r.b.tt.HasText("join: signal server: websocket: bad handshake") }, "the old link refused")
	r.settle(500 * time.Millisecond)
	if r.a.hasLabel("Allow") {
		t.Error("the old link asked A")
	}
	r.a.noOffMachine()
	r.b.noOffMachine()
}

func TestE2EWrongKey(t *testing.T) {
	r := newShareE2E(t, true)
	link := r.openShare()
	bad := link[:len(link)-1]
	if link[len(link)-1] == '0' {
		bad += "1"
	} else {
		bad += "0"
	}
	r.joinWith(bad)
	// A cannot tell a wrong key from a right one: it is asked as for any joiner.
	pump30(t, r.wins(), func() bool { return r.a.hasLabel("Allow") }, "A's prompt")
	r.a.click("Allow")
	pump30(t, r.wins(), func() bool { return r.b.snap().Status == "error" }, "B's verification error")
	if m := r.b.snap().Message; !strings.Contains(m, "host could not be verified") {
		t.Fatalf("B's message %q", m)
	}
	// The snapshot says error a frame before B's window draws it: wait for the words, not for one look.
	pump30(t, r.wins(), func() bool {
		return r.b.tt.HasText("host could not be verified: the link is wrong or the signal server tampered with the connection")
	}, "B's words")
	r.settle(1 * time.Second)
	if got := r.b.tees[0].text(); got != "" {
		t.Errorf("B's terminal output after a failed verification: %q", got)
	}
	if n := len(r.a.snap().Attached); n != 1 {
		t.Errorf("A has %d attached: nobody connected", n)
	}
	if r.a.hasLabel("Allow") {
		t.Error("A's prompt is still there")
	}
	r.b.shot("TestE2EWrongKey-joiner")
	r.a.noOffMachine()
	r.b.noOffMachine()
}
