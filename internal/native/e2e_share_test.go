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

// Share and Join end to end (plan 014). The owner is a window on a scratch keel serve with KEEL_SIGNAL on a loopback
// keel-signal; the joiner is a second window on its own scratch keel serve, as a second machine would be. Nothing in
// these tests reaches off the machine: the joiner's peers have no ICE server (e2eJoiner) and the signal server is
// loopback.
//
// What these tests do not do: let anyone in. keel serve's share hub (internal/share/hub.go acceptPeer) builds its peer
// with a public STUN server in its configuration, and gathers candidates from it the moment the owner allows a
// joiner; a test that pressed Allow would make keel serve send a request off this machine. So every scenario ends
// before Allow (Deny, Stop sharing, a join that never starts); admitting, typing in the joined tab and the hand-over
// are covered by internal/native/joinpeer (the real signal server and a pion peer in place of keel serve) and by the
// workspace's unit tests, and are left for a person to try on two machines (docs/native.md).

// e2eJoiner opens the peer of a join with no ICE server and loopback candidates, and keeps it for the assertions.
func (e *win) joiner(l joinpeer.Link, name string) (session, error) {
	p, err := joinpeer.Open(l, name, joinpeer.WithICEServers(nil), joinpeer.WithLoopback())
	if err != nil {
		return nil, err
	}
	e.peers = append(e.peers, p)
	return p, nil
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
		return len(r.b.w.tabs) == 2 && r.b.w.tab().remote && r.b.hasLabel("banner joining") && r.a.hasLabel("alertdialog "+r.b.w.joinName())
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

	// Deny: B shows the app's refusal and has no terminal output, ever.
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

func TestE2EDenyAndStop(t *testing.T) {
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
	pumpAll(t, r.wins(), func() bool { return len(r.b.w.tabs) == 1 }, "B's tab left")

	// The old link no longer admits: B's join does not even start.
	r.joinWith(old)
	pumpAll(t, r.wins(), func() bool { return r.b.tt.HasText("join: signal server: websocket: bad handshake") }, "the old link refused")
	r.settle(300 * time.Millisecond)
	if r.a.hasLabel("Allow") || len(r.b.w.tabs) != 1 {
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
		return len(b.w.tabs) == 1 && b.snap().Role == "follower" && len(a.snap().Attached) == 2 && b.hasLabel("Ask for control")
	}, "B following")

	// B types: nothing is sent, and the notice is the app's, with Ask for control.
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
