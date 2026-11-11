//go:build nativee2e

package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/authtoken"
	"keel/internal/native/joinpeer"
	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

const e2eWait = 15 * time.Second

func repoRoot(t *testing.T) string {
	t.Helper()
	d, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if p := filepath.Dir(d); p != d {
			d = p
			continue
		}
		t.Fatal("go.mod not found above the working directory")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	for {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if p != 47241 && p != 47298 && p != 47299 {
			return p
		}
	}
}

// e2ePass is the scratch vault passphrase of these tests (KEEL_VAULT_PASSPHRASE of a daemon, typed by the one
// that tests Unlock). It opens nothing but a vault under .scratch-native; no test prints it.
const e2ePass = "native-e2e-scratch"

// scratchDir is the test's own directory under .scratch-native/e2e, emptied.
func scratchDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), ".scratch-native", "e2e", t.Name())
	os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// startServe runs a scratch keel serve (bin/keel) and stops it when the test ends. A passphrase
// opens a scratch vault; without one the vault is locked.
func startServe(t *testing.T, passphrase bool) (addr, dataDir string) {
	t.Helper()
	dir := scratchDir(t)
	addr, _ = startServeAt(t, dir, "serve", passphrase)
	return addr, filepath.Join(dir, "data")
}

// startServeAt runs a keel serve on dir/data, with its log in dir/<name>.log, and returns its address and a
// function that stops it (the cleanup does too). With a passphrase the daemon opens, or creates, the scratch
// vault; without one a vault that exists stays locked.
func startServeAt(t *testing.T, dir, name string, passphrase bool, extraEnv ...string) (addr string, stop func()) {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "keel")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("%s is missing: run make native-e2e (it builds bin/keel first)", bin)
	}
	dataDir := filepath.Join(dir, "data")
	addr = fmt.Sprintf("127.0.0.1:%d", freePort(t))
	env := append(os.Environ(), "KEEL_ADDR="+addr, "KEEL_DATA="+dataDir, "KEEL_KEYRING=off", "SHELL=/bin/sh")
	if passphrase {
		env = append(env, "KEEL_VAULT_PASSPHRASE="+e2ePass)
	}
	env = append(env, extraEnv...)
	logf, err := os.Create(filepath.Join(dir, name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "serve")
	cmd.Env, cmd.Stdout, cmd.Stderr = env, logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			s := exec.Command(bin, "serve", "stop")
			s.Env = env
			s.Run()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				cmd.Process.Kill() // our own child, by handle
				<-done
			}
			logf.Close()
		})
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(e2eWait)
	for time.Now().Before(deadline) {
		if r, err := http.Get("http://" + addr + "/api/health"); err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return addr, stop
			}
		}
		select {
		case <-done:
			t.Fatalf("keel serve exited early; see %s", filepath.Join(dir, name+".log"))
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("keel serve did not come up; see %s", filepath.Join(dir, name+".log"))
	return
}

// countingBackend counts calls to Sessions.
type countingBackend struct {
	backend
	sessions int
}

func (c *countingBackend) Sessions(ctx context.Context) ([]keelclient.Session, error) {
	c.sessions++
	return c.backend.Sessions(ctx)
}

// win is one workspace and Tester on a daemon. Everything touching them runs on the test goroutine.
type win struct {
	t     *testing.T
	w     *workspace
	tt    *ui.Tester
	cl    *keelclient.Client
	addr  string
	token string
	posts chan func()
	peers []*joinpeer.Peer // the joins this window made: every one has no ICE server (e2eJoiner)
	tees  []*teeConn       // what each of them was read for: the joined terminal's text
}

// newWin opens a window on the daemon: the real view, the real terminal, the real client.
func newWin(t *testing.T, addr, dataDir string, wrap func(backend) backend) *win {
	t.Helper()
	return newWinH(t, addr, dataDir, wrap, 800)
}

// newWinH is newWin with a window of the given height.
func newWinH(t *testing.T, addr, dataDir string, wrap func(backend) backend, height int) *win {
	t.Helper()
	tok, err := authtoken.Read(dataDir)
	if err != nil {
		t.Fatal("read token: ", err)
	}
	e := &win{t: t, addr: addr, token: tok, cl: keelclient.NewAt(addr, tok), posts: make(chan func(), 4096)}
	var be backend = clientBackend{e.cl}
	if wrap != nil {
		be = wrap(be)
	}
	e.w = newWorkspace(be)
	e.w.dataDir, e.w.termSize = dataDir, loadPrefs(dataDir).TermSize
	e.w.mkBody = liveBody
	e.w.work = func(fn func()) { go fn() }
	e.w.post = func(fn func()) { e.posts <- fn }
	e.w.joiner = e.joiner
	e.w.start()
	e.tt = ui.NewTester(e.w.view, 1280, height)
	e.tt.Frame()
	return e
}

func (e *win) step() {
	for {
		select {
		case fn := <-e.posts:
			fn()
		default:
			e.tt.Frame()
			return
		}
	}
}

// pump runs posted work and frames until cond holds, or fails with what.
func (e *win) pump(cond func() bool, what string) {
	e.t.Helper()
	deadline := time.Now().Add(e2eWait)
	for {
		e.step()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// settle pumps for d with no condition (to show that something does NOT happen).
func (e *win) settle(d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		e.step()
	}
}

func (e *win) click(label string) {
	e.t.Helper()
	if err := e.tt.Click(label); err != nil {
		e.t.Fatalf("click %q: %v", label, err)
	}
	e.step()
}

// clickBody clicks the middle of pane id's body, as a person does to type in it.
func (e *win) clickBody(id string) {
	e.t.Helper()
	r, ok := e.tt.Find("pane " + id)
	if !ok {
		e.t.Fatalf("pane %s not found", id)
	}
	e.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	e.step()
}

func (e *win) key(mods ui.Modifiers, k ui.Key) { e.tt.Key(mods, k); e.step() }

func (e *win) typeLine(s string) {
	e.tt.Type(s)
	e.key(0, ui.KeyEnter)
}

func (e *win) sessions() []keelclient.Session {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	s, err := e.cl.Sessions(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *win) scrollback(id string) string {
	req, _ := http.NewRequest("GET", "http://"+e.addr+"/api/sessions/"+id+"/scrollback", nil)
	req.Header.Set(authtoken.Header, e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var out struct{ Text string }
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Text
}

// paneSID is the session id of pane id in the active tab.
func (e *win) paneSID(id string) string {
	if t := e.w.tab(); t != nil && t.panes[id] != nil {
		return t.panes[id].sessionID()
	}
	return ""
}

func (e *win) sidOf(tabIdx int) string {
	if tabIdx >= len(e.w.tabs) {
		return ""
	}
	t := e.w.tabs[tabIdx]
	if t.panes[t.focused] == nil { // a New tab has no session
		return ""
	}
	return t.panes[t.focused].sessionID()
}

func (e *win) waitScrollback(sid, want string) {
	e.t.Helper()
	e.pump(func() bool { return strings.Contains(e.scrollback(sid), want) }, fmt.Sprintf("%q in the scrollback of %s", want, sid))
}

// openLocal opens the Local Shell and waits for its prompt.
func (e *win) openLocal() string {
	e.t.Helper()
	e.pump(func() bool { return e.tt.HasText("Local Shell") }, "the host tree")
	e.click("host Local Shell")
	e.pump(func() bool { return len(e.w.tabs) == 1 && e.sidOf(0) != "" }, "a tab with a session id")
	sid := e.sidOf(0)
	e.waitScrollback(sid, "$ ")
	return sid
}

// newSession starts the Local Shell from the first card of a New tab: the one that is showing, or one the +
// button opens.
func (e *win) newSession() {
	e.t.Helper()
	if t := e.w.tab(); t == nil || !t.isNew() {
		e.click("New tab")
	}
	e.pump(func() bool { return e.tt.HasText("Where to?") && e.hasLabel("start card Local Shell") }, "the New tab page")
	e.click("start card Local Shell")
}

func (e *win) shot(name string) {
	e.t.Helper()
	img := e.tt.Image()
	if img == nil {
		e.t.Fatal("Tester.Image gave no pixels")
	}
	dir := filepath.Join(repoRoot(e.t), ".scratch-native", "e2e")
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.t.Fatal(err)
	}
}

func (e *win) pixel(x, y float32) [3]uint8 {
	img := e.tt.Image()
	s := float64(img.Bounds().Dx()) / 1280
	o := img.PixOffset(int(float64(x)*s), int(float64(y)*s))
	return [3]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2]}
}

// inked counts pixels of r that differ from the stage ground: a terminal that drew text has many.
func (e *win) inked(r ui.Rect) (n int) {
	img := e.tt.Image()
	s := float64(img.Bounds().Dx()) / 1280
	b := image.Rect(int(float64(r.X)*s), int(float64(r.Y)*s), int(float64(r.X+r.W)*s), int(float64(r.Y+r.H)*s)).Intersect(img.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			o := img.PixOffset(x, y)
			if !is([3]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2]}, termBgColor()) {
				n++
			}
		}
	}
	return n
}

func termBgColor() ui.Color { return termBg }

// openPanes opens the Local Shell and splits it right until there are n sessions.
func (e *win) openPanes(n int) {
	e.t.Helper()
	e.openLocal()
	for i := 1; i < n; i++ {
		e.key(ui.Cmd|ui.Shift, ui.KeyD)
		e.pump(func() bool { return len(e.sessions()) == i+1 && e.w.paneCount() == i+1 }, fmt.Sprintf("%d sessions", i+1))
	}
	e.pump(func() bool {
		for _, p := range e.w.tab().panes {
			if p.sessionID() == "" {
				return false
			}
		}
		return true
	}, "every pane to be named")
}

func TestE2EOpenAndType(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.clickBody("1")
	e.typeLine("echo keel-native-$((6*7))")
	e.waitScrollback(sid, "keel-native-42")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EOpenAndType")
	r, _ := e.tt.Find("pane 1")
	if n := e.inked(r); n <= 200 {
		t.Errorf("only %d pixels of the terminal differ from its background: it drew no text", n)
	}
}

func TestE2ESplitAndFocus(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2)
	if !e.tt.HasText("2") {
		t.Error("no pane count badge 2 on the tab")
	}
	e.key(ui.Cmd|ui.Shift, ui.KeyE)
	e.pump(func() bool { return len(e.sessions()) == 3 && e.w.paneCount() == 3 }, "3 sessions")
	e.pump(func() bool { return e.w.tab().panes["3"].sessionID() != "" }, "pane 3 named")

	// Click inside the first terminal's body: the first pane takes focus and the ring.
	e.clickBody("1")
	if e.w.tab().focused != "1" {
		t.Fatalf("focused %q after clicking pane 1's body", e.w.tab().focused)
	}
	ring := focus.Alpha(focusRingAlpha).Over(chrome)
	a, _ := e.tt.Find("pane 1")
	b, _ := e.tt.Find("pane 2")
	if a.X < b.X { // pane 1 is left of pane 2: its ring is in the gap before it, or the area padding at the edge
		if got := e.pixel(a.X-1.5, a.Y+a.H/2); !is(got, ring) {
			t.Errorf("pane 1 outer edge %v, want ring %v", got, ring)
		}
	}
	if got := e.pixel(b.X-1.5, b.Y+b.H/2); is(got, ring) {
		t.Errorf("unfocused pane 2 has a ring at %v", got)
	}

	// Click a pane bar: that pane takes the keyboard, and typing lands in its session only.
	bars, _ := e.tt.Find("pane 2")
	e.tt.ClickAt(bars.X+bars.W/2, bars.Y+8) // the bar, above the body
	e.step()
	if e.w.tab().focused != "2" {
		t.Fatalf("focused %q after clicking pane 2's bar", e.w.tab().focused)
	}
	e.typeLine("echo bar-$((1+1))")
	sid2 := e.paneSID("2")
	e.waitScrollback(sid2, "bar-2")
	e.settle(300 * time.Millisecond)
	for _, id := range []string{"1", "3"} {
		if strings.Contains(e.scrollback(e.paneSID(id)), "bar-2") {
			t.Errorf("the line typed in pane 2 reached pane %s", id)
		}
	}
	e.shot("TestE2ESplitAndFocus")
}

func TestE2EGapDragResizes(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2)
	ids := []string{e.paneSID("1"), e.paneSID("2")}
	cols := func() (a, b int) {
		for _, s := range e.sessions() {
			switch s.ID {
			case ids[0]:
				a = s.Cols
			case ids[1]:
				b = s.Cols
			}
		}
		return
	}
	// Both terminals report their size once they have been laid out.
	var a0, b0 int
	e.pump(func() bool { a0, b0 = cols(); return a0 > 0 && b0 > 0 && a0 != startCols && b0 != startCols }, "sessions resized to their tiles")
	e.settle(300 * time.Millisecond)
	a0, b0 = cols()
	g, ok := e.tt.Find("split handle")
	if !ok {
		t.Fatal("no split handle")
	}
	x, y := g.X+g.W/2, g.Y+g.H/2
	e.tt.Press(x, y)
	e.tt.Move(x+100, y)
	e.tt.Move(x+200, y)
	e.tt.Release(x+200, y)
	e.step()
	e.pump(func() bool { a, b := cols(); return a > a0 && b < b0 }, fmt.Sprintf("first session wider than %d cols and second narrower than %d", a0, b0))
	// Drag the gap to the far left: the first tile is as narrow as it gets and its tools must all be inside it.
	g, _ = e.tt.Find("split handle")
	x, y = g.X+g.W/2, g.Y+g.H/2
	e.tt.Press(x, y)
	e.tt.Move(x-3000, y)
	e.tt.Move(x-3000, y)
	e.tt.Release(x-3000, y)
	e.step()
	tile, _ := e.tt.Find("pane 1")
	for _, tool := range []string{"Split right", "Split down", "Focus view", "More pane actions", "Close pane"} {
		b, ok := e.tt.Find(tool)
		if !ok || b.W == 0 || b.X < tile.X || b.X+b.W > tile.X+tile.W {
			t.Errorf("%s %v is not inside the narrow tile %v", tool, b, tile)
		}
	}
	e.shot("TestE2EGapDragResizes")
}

func TestE2EShortcutsDoNotReachTheShell(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.clickBody("1")
	// Hiding the sidebar resizes the terminal, and sh answers a resize by redrawing its prompt; that
	// is the only output a shortcut may cause. A key that reached the shell would echo or run.
	redraw := strings.NewReplacer("\r\x1b[Ksh-3.2$ ", "")
	before := redraw.Replace(e.scrollback(sid))
	e.key(ui.Cmd, ui.KeyB)
	if e.w.sidebar || e.tt.HasText("Filter hosts") {
		t.Error("Cmd+B did not hide the sidebar")
	}
	e.key(ui.Cmd, ui.KeyB)
	e.key(ui.Cmd, ui.Key1)
	e.key(ui.Cmd, ui.Key9)
	e.settle(300 * time.Millisecond)
	if !e.w.sidebar || !e.tt.HasText("Filter hosts") {
		t.Error("the sidebar did not come back")
	}
	if after := redraw.Replace(e.scrollback(sid)); after != before {
		t.Errorf("shortcuts reached the shell: scrollback changed from %q to %q", before, after)
	}
	e.shot("TestE2EShortcutsDoNotReachTheShell")
}

func TestE2ECloseKillsAndWindowCloseDetaches(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	first := e.openLocal()
	e.clickBody("1")
	e.typeLine("echo before-detach")
	e.waitScrollback(first, "before-detach")
	e.newSession()
	e.pump(func() bool { return len(e.w.tabs) == 2 && e.sidOf(1) != "" && len(e.sessions()) == 2 }, "a second tab")
	e.key(ui.Cmd, ui.KeyW)
	e.pump(func() bool { return len(e.sessions()) == 1 && len(e.w.tabs) == 1 }, "the second session killed and its tab gone")
	if got := e.sessions()[0].ID; got != first {
		t.Fatalf("surviving session %s, want %s", got, first)
	}

	e.w.closeWindow()
	e.settle(500 * time.Millisecond)
	if n := len(e.sessions()); n != 1 {
		t.Fatalf("%d sessions after closing the window, want 1", n)
	}

	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return len(e2.w.tabs) == 1 && e2.sidOf(0) == first }, "the surviving session reattached")
	e2.waitScrollback(first, "before-detach")
	e2.pump(func() bool { r, _ := e2.tt.Find("pane 1"); return e2.inked(r) > 200 }, "the reattached terminal to show earlier output")
	e2.shot("TestE2ECloseKillsAndWindowCloseDetaches")
}

// TestE2ELockedVault: a data dir with no vault and no passphrase needs setup: the window shows First run's step 1
// and reads no sessions.
func TestE2ELockedVault(t *testing.T) {
	addr, dir := startServe(t, false)
	var cb *countingBackend
	e := newWin(t, addr, dir, func(b backend) backend { cb = &countingBackend{backend: b}; return cb })
	e.pump(func() bool { return e.tt.HasText("Welcome to Keel") }, "First run step 1")
	e.settle(300 * time.Millisecond)
	if cb.sessions != 0 {
		t.Errorf("Sessions was called %d times on a locked vault", cb.sessions)
	}
	e.shot("TestE2ELockedVault")
}

func TestE2ETabs(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.tt.HasText("Local Shell") }, "the host tree")
	for i := 1; i <= 3; i++ {
		e.newSession()
		e.pump(func() bool { return len(e.w.tabs) == i && e.sidOf(i-1) != "" }, fmt.Sprintf("tab %d with a session", i))
	}
	sids := []string{e.sidOf(0), e.sidOf(1), e.sidOf(2)}
	for _, c := range []struct {
		k    ui.Key
		want int
	}{{ui.Key1, 0}, {ui.Key2, 1}, {ui.Key9, 2}} {
		e.key(ui.Cmd, c.k)
		if e.w.active != c.want {
			t.Errorf("active %d, want %d", e.w.active, c.want)
		}
		if got := e.w.focusedPane().sessionID(); got != sids[c.want] {
			t.Errorf("visible session %s, want %s", got, sids[c.want])
		}
	}
	e.shot("TestE2ETabs")
}

// pumpAll is pump for several windows on one daemon.
func pumpAll(t *testing.T, wins []*win, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(e2eWait)
	for {
		for _, w := range wins {
			w.step()
		}
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// snap is the first pane's last report in the active tab.
func (e *win) snap() keelclient.State {
	if p := e.w.focusedPane(); p != nil {
		return p.conn.Snapshot()
	}
	return keelclient.State{}
}

// sshFixture is a keel-sshtest (a throwaway SSH server on loopback) and the saved host that points at it.
type sshFixture struct {
	cmd         *exec.Cmd
	done        chan struct{}
	fingerprint string
	stopped     bool

	host, user string
	port       int
	password   string // typed into a field by the test that needs it; never printed or logged

	// The fixture's other server, the one that allows forwarding (the target refuses it): a forward goes through it.
	jump struct {
		host, user, password, fingerprint string
		port                              int
	}
}

// startSSH runs bin/keel-sshtest, creates a password host for its target on the daemon through
// POST /api/hosts (the client itself never creates hosts) and returns the fixture. The password is
// read from the file keel-sshtest writes and goes only into that request.
func startSSH(t *testing.T, addr, dataDir string) *sshFixture {
	t.Helper()
	f := startSSHOnly(t, dataDir)
	tok, err := authtoken.Read(dataDir)
	if err != nil {
		t.Fatal("read token: ", err)
	}
	body, _ := json.Marshal(map[string]any{
		"name": "e2e-ssh", "address": f.host, "port": f.port, "username": f.user,
		"authType": "password", "password": f.password, "noAutoReconnect": true,
	})
	req, _ := http.NewRequest("POST", "http://"+addr+"/api/hosts", strings.NewReader(string(body)))
	req.Header.Set(authtoken.Header, tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/hosts: %s", resp.Status) // the body is not printed: it could echo the request
	}
	return f
}

// startSSHOnly runs bin/keel-sshtest and reads its target; no host is saved. The password is in the fixture,
// for a test that types it into a field. It is never printed or logged.
func startSSHOnly(t *testing.T, dataDir string) *sshFixture {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "keel-sshtest")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("%s is missing: run make native-e2e (it builds it)", bin)
	}
	out := filepath.Join(filepath.Dir(dataDir), "sshtest.json")
	cmd := exec.Command(bin, "-out", out)
	cmd.Env = append(os.Environ(), "KEEL_KEYRING=off")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	f := &sshFixture{cmd: cmd, done: make(chan struct{})}
	go func() { cmd.Wait(); close(f.done) }()
	t.Cleanup(f.stop)
	type server struct {
		Host        string
		Port        int
		User        string
		Password    string
		Fingerprint string
	}
	var info struct{ Target, Jump server }
	for end := time.Now().Add(e2eWait); ; time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(out); err == nil && json.Unmarshal(b, &info) == nil && info.Target.Port != 0 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("keel-sshtest did not write its file")
		}
	}
	os.Remove(out) // it holds the fixture's password in the clear: it stays only in memory, never in a file of the run
	f.fingerprint = info.Target.Fingerprint
	f.host, f.port, f.user, f.password = info.Target.Host, info.Target.Port, info.Target.User, info.Target.Password
	f.jump.host, f.jump.port, f.jump.user, f.jump.password, f.jump.fingerprint = info.Jump.Host, info.Jump.Port, info.Jump.User, info.Jump.Password, info.Jump.Fingerprint
	return f
}

// stop ends the fixture's process, by the handle it was started with.
func (f *sshFixture) stop() {
	if f.stopped {
		return
	}
	f.stopped = true
	f.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		f.cmd.Process.Kill()
		<-f.done
	}
}

func (e *win) hasLabel(label string) bool { _, ok := e.tt.Find(label); return ok }

// connectSSH opens the e2e-ssh host, trusts its key in the dialog and waits for the pane to be ready.
func (e *win) connectSSH(f *sshFixture) {
	e.t.Helper()
	e.pump(func() bool { return e.tt.HasText("e2e-ssh") }, "the host tree")
	e.click("host e2e-ssh")
	e.pump(func() bool { return e.hasLabel("dialog Unknown host key") }, "the unknown host key dialog")
	if !e.tt.HasText(f.fingerprint) {
		e.t.Fatal("the dialog does not show the fingerprint the server offered")
	}
	e.click("Trust and connect")
	e.pump(func() bool { return e.snap().Status == "ready" }, "the session to be ready")
}

func TestE2EFollowerAndTakeControl(t *testing.T) {
	addr, dir := startServe(t, true)
	a := newWin(t, addr, dir, nil)
	sid := a.openLocal()
	a.clickBody("1")
	a.typeLine("echo from-a")
	a.waitScrollback(sid, "from-a")

	b := newWin(t, addr, dir, nil)
	both := []*win{a, b}
	pumpAll(t, both, func() bool {
		return len(b.w.tabs) == 1 && b.snap().Role == "follower" && len(a.snap().Attached) == 2 && b.hasLabel("role Following") &&
			b.hasLabel("banner following") && b.hasLabel("Ask for control") && a.hasLabel("role Driving")
	}, "B following with Ask for control, A driving")
	if a.hasLabel("banner following") || b.hasLabel("Take control") {
		t.Error("the driver shows the follower banner, or a non-owner offers Take control")
	}
	b.shot("TestE2EFollowerAndTakeControl-following")

	// The follower's keystrokes are dropped by keel serve.
	b.clickBody("1")
	b.typeLine("echo from-b")
	for end := time.Now().Add(500 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		a.step()
		b.step()
	}
	if strings.Contains(a.scrollback(sid), "from-b") {
		t.Fatal("a follower's keystrokes reached the PTY")
	}

	// A (the owner) passes control to B with a control frame, not a PTY write.
	conn := a.w.focusedPane().conn.(*keelclient.Conn)
	if err := conn.GrantControl(b.snap().YouID); err != nil {
		t.Fatal(err)
	}
	pumpAll(t, both, func() bool {
		return b.snap().Role == "driver" && a.snap().Role == "owner" && a.hasLabel("banner following") && a.hasLabel("Take control")
	}, "B driving, A following as the owner with Take control")
	a.shot("TestE2EFollowerAndTakeControl-owner")
	b.clickBody("1")
	b.typeLine("echo b-drives")
	a.waitScrollback(sid, "b-drives")

	// A takes control back.
	a.click("Take control")
	pumpAll(t, both, func() bool { return a.snap().Role == "driver" && b.snap().Role == "follower" }, "A driving again")
	a.clickBody("1")
	a.typeLine("echo a-again")
	a.waitScrollback(sid, "a-again")
}

func TestE2EUnknownHostKeyTrustAndConnect(t *testing.T) {
	addr, dir := startServe(t, true)
	f := startSSH(t, addr, dir)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.tt.HasText("e2e-ssh") }, "the host tree")
	e.click("host e2e-ssh")
	e.pump(func() bool { return e.hasLabel("dialog Unknown host key") }, "the unknown host key dialog")
	for _, s := range []string{"Unknown host key", f.fingerprint, "Trust and connect", "Abort"} {
		if !e.tt.HasText(s) {
			t.Errorf("the dialog lacks %q", s)
		}
	}
	e.shot("TestE2EUnknownHostKeyTrustAndConnect-dialog")
	e.click("Trust and connect")
	e.pump(func() bool { return e.snap().Status == "ready" }, "the session to be ready")
	sid := e.sidOf(0)
	e.clickBody("1")
	e.typeLine("echo trusted-ok")
	e.waitScrollback(sid, "trusted-ok")

	// The same host again connects with no dialog.
	e.click("host e2e-ssh")
	e.pump(func() bool { return len(e.w.tabs) == 2 && e.snap().Status == "ready" }, "the second session to be ready")
	if e.hasLabel("dialog Unknown host key") || e.hasLabel("dialog Host key changed") {
		t.Error("the dialog came back for a trusted host")
	}
	e.shot("TestE2EUnknownHostKeyTrustAndConnect")
}

// failedSSH leaves a daemon with one session that was ready and whose server then went away.
func failedSSH(t *testing.T) (addr, dir string, e *win, sid string) {
	addr, dir = startServe(t, true)
	f := startSSH(t, addr, dir)
	e = newWin(t, addr, dir, nil)
	e.connectSSH(f)
	sid = e.sidOf(0)
	f.stop()
	e.pump(func() bool { return e.hasLabel("banner failed") && e.hasLabel("Reconnect") }, "the failure banner")
	return
}

func TestE2EFailedSessionShowsBannerAndReconnects(t *testing.T) {
	_, _, e, sid := failedSSH(t)
	if !e.tt.HasText("Session ended") {
		t.Error("the banner does not say Session ended for a session whose server went away (status exited)")
	}
	e.shot("TestE2EFailedSessionShowsBannerAndReconnects-failed")
	before := e.snap().Message
	e.click("Reconnect")
	// Reconnect to a server that is gone: the session leaves error for connecting, or fails again with a new message.
	e.pump(func() bool {
		st := e.snap()
		return st.Status == "connecting" || (st.Status == "error" && st.Message != before)
	}, "the session to leave its failed state or fail with a new message")
	e.pump(func() bool { return e.hasLabel("banner connecting") || e.hasLabel("banner failed") }, "a banner after Reconnect")
	e.pump(func() bool { return e.hasLabel("banner failed") && e.hasLabel("Close") }, "the failure banner again")
	e.shot("TestE2EFailedSessionShowsBannerAndReconnects")
	e.click("Close")
	e.pump(func() bool { return len(e.w.tabs) == 1 && e.w.tabs[0].isNew() }, "the pane to go and the New tab to take its place")
	e.pump(func() bool {
		for _, s := range e.sessions() {
			if s.ID == sid {
				return false
			}
		}
		return true
	}, "the session to disappear from Sessions()")
}

func TestE2EFailedSessionAtStart(t *testing.T) {
	addr, dir, _, sid := failedSSH(t)
	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return len(e2.w.tabs) == 1 && e2.hasLabel("banner failed") && e2.hasLabel("Reconnect") }, "the failed session as a tab with its banner")
	if e2.sidOf(0) != sid {
		t.Errorf("tab shows session %s, want %s", e2.sidOf(0), sid)
	}
	e2.shot("TestE2EFailedSessionAtStart")
}

// ---- N3b: tabs, panes and the workspace ----

// sessionIDs is the ids keel serve lists.
func (e *win) sessionIDs() (ids []string) {
	for _, s := range e.sessions() {
		ids = append(ids, s.ID)
	}
	return
}

// tabRect is the pill of tab i (the pills of tabs with equal names are equally wide).
func (e *win) tabRect(i int) ui.Rect {
	e.t.Helper()
	r, ok := e.tt.Find("tab Local Shell")
	if !ok {
		e.t.Fatal("no tab in the strip")
	}
	r.X += float32(i) * (r.W + space1_5)
	return r
}

// namePanes waits until every pane of every tab has its session id.
func (e *win) namePanes() {
	e.t.Helper()
	e.pump(func() bool {
		for _, t := range e.w.tabs {
			for _, p := range t.panes {
				if p.sessionID() == "" {
					return false
				}
			}
		}
		return true
	}, "every pane to be named")
}

// structure is a tree with the session of each leaf and no ratios; ratios lists them in order.
func structure(t *tab) (string, []float32) {
	var ratios []float32
	var f func(n *layout.Node) string
	f = func(n *layout.Node) string {
		if n.A == nil {
			return t.panes[n.Pane].sessionID()
		}
		ratios = append(ratios, n.Ratio)
		return fmt.Sprintf("%d(%s %s)", n.Dir, f(n.A), f(n.B))
	}
	return f(t.root), ratios
}

func TestE2EWorkspaceComesBack(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2) // tab 1: pane 1 | pane 2
	e.key(ui.Cmd|ui.Shift, ui.KeyE)
	e.pump(func() bool { return len(e.sessions()) == 3 && e.w.paneCount() == 3 }, "3 sessions")
	e.namePanes()
	// Drag the first gap (between pane 1 and the column) to the right.
	e.settle(300 * time.Millisecond)
	g, ok := e.tt.Find("split handle")
	if !ok {
		t.Fatal("no split handle")
	}
	x, y := g.X+g.W/2, g.Y+g.H/2
	e.tt.Press(x, y)
	e.tt.Move(x+60, y)
	e.tt.Move(x+120, y)
	e.tt.Release(x+120, y)
	e.step()
	if r := e.w.tabs[0].root.Ratio; r < 0.55 {
		t.Fatalf("the gap did not move: ratio %v", r)
	}
	// Tab 2: one pane, renamed logs.
	e.newSession()
	e.pump(func() bool { return len(e.w.tabs) == 2 && e.sidOf(1) != "" }, "tab 2")
	p2 := e.tabRect(1)
	e.tt.RightClickAt(p2.X+p2.W/2, p2.Y+p2.H/2)
	e.step()
	if err := e.tt.ChooseMenuItem("Rename tab"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.tt.Type("logs")
	e.key(0, ui.KeyEnter)
	if e.w.tabs[1].title != "logs" {
		t.Fatalf("tab 2 is %q", e.w.tabs[1].title)
	}
	// Back to tab 1 with its first pane focused.
	e.key(ui.Cmd, ui.Key1)
	e.clickBody("1")
	if e.w.tab().focused != "1" || e.w.active != 0 {
		t.Fatalf("focused %q, active %d", e.w.tab().focused, e.w.active)
	}
	assertFits(t, e.w, e.tt, 1280, 800)
	e.shot("TestE2EWorkspaceComesBack-before")
	wantTree, wantRatios := structure(e.w.tabs[0])
	wantFocus := e.w.tab().panes[e.w.tab().focused].sessionID()
	wantTab2 := e.sidOf(1)
	before := e.sessionIDs()
	if len(before) != 4 {
		t.Fatalf("%d sessions, want 4", len(before))
	}

	e.w.closeWindow()
	e.settle(300 * time.Millisecond)
	if n := len(e.sessions()); n != 4 {
		t.Fatalf("%d sessions after the window closed, want 4", n)
	}

	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return len(e2.w.tabs) == 2 && e2.w.paneCount() == 4 }, "two tabs with four panes")
	gotTree, gotRatios := structure(e2.w.tabs[0])
	if gotTree != wantTree {
		t.Errorf("tab 1 tree %s, want %s", gotTree, wantTree)
	}
	if len(gotRatios) != len(wantRatios) {
		t.Fatalf("ratios %v, want %v", gotRatios, wantRatios)
	}
	for i := range gotRatios {
		if d := gotRatios[i] - wantRatios[i]; d < -0.02 || d > 0.02 {
			t.Errorf("ratio %d: %v, want %v", i, gotRatios[i], wantRatios[i])
		}
	}
	if got := e2.w.tab().panes[e2.w.tab().focused].sessionID(); got != wantFocus || e2.w.active != 0 {
		t.Errorf("focused session %s (want %s), active %d (want 0)", got, wantFocus, e2.w.active)
	}
	if e2.w.tabs[1].title != "logs" || e2.sidOf(1) != wantTab2 || e2.w.tabs[0].title != "" {
		t.Errorf("tab 2: %q on %s, want logs on %s", e2.w.tabs[1].title, e2.sidOf(1), wantTab2)
	}
	e2.pump(func() bool { return e2.tt.HasText("logs") }, "the tab titled logs")
	if got := e2.sessionIDs(); len(got) != 4 {
		t.Errorf("%d sessions, want 4", len(got))
	}
	e2.settle(500 * time.Millisecond)
	assertFits(t, e2.w, e2.tt, 1280, 800)
	e2.shot("TestE2EWorkspaceComesBack")
}

func TestE2EFocusViewAndPaneMenu(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2)
	s1, s2 := e.paneSID("1"), e.paneSID("2")
	e.clickBody("1")
	e.key(ui.Cmd|ui.Shift, ui.KeyEnter)
	if e.w.tab().zoomed != "1" || e.hasLabel("pane 2") {
		t.Fatalf("zoomed %q, pane 2 on screen %v", e.w.tab().zoomed, e.hasLabel("pane 2"))
	}
	for _, s := range e.sessions() {
		if (s.ID == s1 || s.ID == s2) && s.Status != "ready" {
			t.Errorf("session %s is %s while another pane is zoomed", s.ID, s.Status)
		}
	}
	if len(e.sessions()) != 2 {
		t.Fatalf("%d sessions in focus view, want 2", len(e.sessions()))
	}
	e.typeLine("echo zoomed-$((3+4))")
	e.waitScrollback(s1, "zoomed-7")
	e.settle(300 * time.Millisecond)
	if strings.Contains(e.scrollback(s2), "zoomed-7") {
		t.Error("the line typed in the zoomed pane reached the hidden one")
	}
	e.shot("TestE2EFocusViewAndPaneMenu-zoomed")

	// The pane menu by right-click: Focus view again leaves focus view.
	r, _ := e.tt.Find("pane bar 1")
	e.tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	e.step()
	if m := e.tt.Menu(); !strings.Contains(strings.Join(m, "|"), "Focus view") {
		t.Fatalf("pane menu %q", m)
	}
	e.shot("TestE2EFocusViewAndPaneMenu-menu")
	if err := e.tt.ChooseMenuItem("Focus view"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if e.w.tab().zoomed != "" || !e.hasLabel("pane 1") || !e.hasLabel("pane 2") {
		t.Fatalf("still zoomed: %q", e.w.tab().zoomed)
	}

	// Close pane from the menu of pane 2 kills that session and no other.
	r, _ = e.tt.Find("pane bar 2")
	e.tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	e.step()
	if err := e.tt.ChooseMenuItem("Close pane"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.pump(func() bool { return slices.Equal(e.sessionIDs(), []string{s1}) }, "only the first session left")
	e.shot("TestE2EFocusViewAndPaneMenu")
}

func TestE2ETabMenuAndKeys(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	for i := 2; i <= 4; i++ {
		e.newSession()
		e.pump(func() bool { return len(e.w.tabs) == i && e.sidOf(i-1) != "" }, fmt.Sprintf("tab %d", i))
	}
	sids := []string{e.sidOf(0), e.sidOf(1), e.sidOf(2), e.sidOf(3)}

	// Next and previous wrap.
	e.key(ui.Cmd|ui.Shift, ui.KeyBracketRight)
	if e.w.active != 0 {
		t.Errorf("next from the last tab: %d", e.w.active)
	}
	e.key(ui.Cmd|ui.Shift, ui.KeyBracketLeft)
	if e.w.active != 3 {
		t.Errorf("previous from the first tab: %d", e.w.active)
	}

	// Drag tab 4 to the first place; ⌘1 now shows it.
	a, d := e.tabRect(0), e.tabRect(3)
	e.tt.Press(d.X+d.W/2, d.Y+d.H/2)
	e.tt.Move(d.X+d.W/2-30, d.Y+d.H/2)
	e.tt.Move(a.X+4, a.Y+a.H/2)
	e.tt.Release(a.X+4, a.Y+a.H/2)
	e.step()
	if got := []string{e.sidOf(0), e.sidOf(1), e.sidOf(2), e.sidOf(3)}; !slices.Equal(got, []string{sids[3], sids[0], sids[1], sids[2]}) {
		t.Fatalf("order after the drag %v", got)
	}
	e.key(ui.Cmd, ui.Key2)
	e.key(ui.Cmd, ui.Key1)
	if got := e.w.focusedPane().sessionID(); got != sids[3] {
		t.Errorf("⌘1 shows %s, want %s", got, sids[3])
	}
	e.shot("TestE2ETabMenuAndKeys-reordered")

	// Close to the right of tab 2 kills the sessions of tabs 3 and 4 and no others.
	order := []string{e.sidOf(0), e.sidOf(1), e.sidOf(2), e.sidOf(3)}
	p := e.tabRect(1)
	e.tt.RightClickAt(p.X+p.W/2, p.Y+p.H/2)
	e.step()
	e.shot("TestE2ETabMenuAndKeys-menu")
	if err := e.tt.ChooseMenuItem("Close to the right"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.pump(func() bool { return len(e.w.tabs) == 2 && slices.Equal(sorted(e.sessionIDs()), sorted(order[:2])) }, "tabs 3 and 4 and only their sessions gone")

	// ⌘⇧T opens a new session on the closed tab's host.
	before := e.sessionIDs()
	e.key(ui.Cmd|ui.Shift, ui.KeyT)
	e.pump(func() bool { return len(e.sessions()) == len(before)+1 && len(e.w.tabs) == 3 }, "a new session in a new tab")
	for _, id := range e.sessionIDs() {
		if !slices.Contains(before, id) && (id == order[2] || id == order[3]) {
			t.Errorf("reopened as the old session %s", id)
		}
	}
	e.shot("TestE2ETabMenuAndKeys")
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

func TestE2EFindAndTextSize(t *testing.T) {
	addr, dir := startServe(t, true)
	prefs := filepath.Join(dir, "keel-native.json")
	if err := os.WriteFile(prefs, []byte(`{"opacity":100,"blur":10,"future":"kept"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	e.clickBody("1")
	e.typeLine("for i in 1 2 3; do echo needle-$i; done")
	e.waitScrollback(sid, "needle-3")
	e.settle(300 * time.Millisecond)
	// The find bar takes a row of height, so the terminal resizes and sh answers by redrawing its prompt;
	// that is the only change find may cause (a key that reached the shell would echo or run).
	redraw := strings.NewReplacer("\r\x1b[Ksh-3.2$ ", "")
	before := redraw.Replace(e.scrollback(sid))

	e.key(ui.Cmd, ui.KeyF)
	e.pump(func() bool { return e.hasLabel("find bar") && e.tt.Focused("Find in terminal") }, "the find bar with the keyboard")
	e.tt.Type("needle")
	var total int
	e.pump(func() bool {
		_, total = e.w.focusedPane().search.status()
		return total >= 3
	}, "the terminal to find three needles")
	e.pump(func() bool { return e.w.findCount(e.w.focusedPane()) != "" }, "the count in the bar")
	e.shot("TestE2EFindAndTextSize-find")
	e.settle(200 * time.Millisecond)
	if after := redraw.Replace(e.scrollback(sid)); after != before {
		t.Errorf("find changed the session's scrollback:\n%q\n%q", before, after)
	}
	e.key(0, ui.KeyEscape)
	if e.hasLabel("find bar") {
		t.Error("Escape did not close the find bar")
	}
	e.settle(300 * time.Millisecond)
	if a := redraw.Replace(e.scrollback(sid)); a != before {
		t.Errorf("closing find changed the scrollback: %q", a)
	}

	cols := func() int {
		for _, s := range e.sessions() {
			if s.ID == sid {
				return s.Cols
			}
		}
		return 0
	}
	var c0 int
	e.pump(func() bool { c0 = cols(); return c0 > 0 && c0 != startCols }, "the session to take the pane's size")
	e.settle(300 * time.Millisecond)
	c0 = cols()
	e.key(ui.Cmd, ui.KeyEqual)
	e.key(ui.Cmd, ui.KeyEqual)
	e.pump(func() bool { return cols() < c0 }, fmt.Sprintf("fewer than %d columns at the bigger size", c0))
	c2 := cols()
	e.shot("TestE2EFindAndTextSize")
	e.key(ui.Cmd, ui.Key0)
	e.pump(func() bool { return cols() == c0 }, fmt.Sprintf("%d columns back at the default size (was %d)", c0, c2))

	e.pump(func() bool {
		b, _ := os.ReadFile(prefs)
		var f struct {
			Opacity, Blur, TermSize int
			Future                  string
		}
		return json.Unmarshal(b, &f) == nil && f.TermSize == 12 && f.Opacity == 100 && f.Blur == 10 && f.Future == "kept"
	}, "keel-native.json with termSize 12 and its other fields intact")
	if fi, _ := os.Stat(prefs); fi.Mode().Perm() != 0o600 {
		t.Errorf("prefs mode %v", fi.Mode().Perm())
	}
}

// sshProcesses counts the processes whose command line has "ssh -p" in it: the ssh binary run for a pasted
// command would be one.
func sshProcesses(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", "ssh -p").Output() // exit 1 and no output: none
	if err != nil && len(out) != 0 {
		t.Fatal(err)
	}
	return len(bytes.Fields(out))
}

func (e *win) onStartPage() bool {
	return e.tt.HasText("Where to?") && e.hasLabel("start card Local Shell")
}

func TestE2ENewTabPage(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(e.onStartPage, "the New tab page of a window with no session")
	if len(e.w.tabs) != 1 || !e.w.tabs[0].isNew() || len(e.sessions()) != 0 {
		t.Fatalf("tabs %d, sessions %d", len(e.w.tabs), len(e.sessions()))
	}
	e.shot("TestE2ENewTabPage-empty")

	// The Local Shell card starts a session in this tab.
	e.click("start card Local Shell")
	e.pump(func() bool {
		return len(e.w.tabs) == 1 && !e.w.tabs[0].isNew() && e.sidOf(0) != "" && len(e.sessions()) == 1
	}, "a session tab")
	e.waitScrollback(e.sidOf(0), "$ ")

	// ⌘T: a New tab, with Local Shell first among the recent hosts (and kept in keel-native.json as an id).
	e.key(ui.Cmd, ui.KeyT)
	e.pump(func() bool { return len(e.w.tabs) == 2 && e.w.tabs[1].isNew() && e.onStartPage() }, "a New tab")
	local, _ := e.w.localHost()
	if cards := e.w.cards(e.w.tab().page); len(cards) == 0 || cards[0].ID != local.ID || len(e.w.recentIDs) == 0 || e.w.recentIDs[0] != local.ID {
		t.Fatalf("cards %v recents %v", cards, e.w.recentIDs)
	}
	e.pump(func() bool { return slices.Equal(loadRecentHosts(dir), []string{local.ID}) }, "the recent hosts in keel-native.json")

	// ⌥1 opens the first card in this tab: a second session.
	e.key(ui.Alt, ui.Key1)
	e.pump(func() bool {
		return len(e.sessions()) == 2 && len(e.w.tabs) == 2 && !e.w.tabs[1].isNew() && e.sidOf(1) != ""
	}, "two sessions in two tabs")
	e.waitScrollback(e.sidOf(1), "$ ")

	// Closing both tabs leaves a New tab and no session.
	e.key(ui.Cmd, ui.KeyW)
	e.pump(func() bool { return len(e.sessions()) == 1 && len(e.w.tabs) == 1 }, "the second session closed")
	e.key(ui.Cmd, ui.KeyW)
	e.pump(func() bool {
		return len(e.sessions()) == 0 && len(e.w.tabs) == 1 && e.w.tabs[0].isNew() && e.onStartPage()
	}, "no session and the New tab page")

	// Recently closed lists what was closed; a row starts a new session on that host.
	e.pump(func() bool { return e.tt.HasText("Recently closed") && e.hasLabel("reopen Local Shell") }, "Recently closed")
	e.shot("TestE2ENewTabPage-closed")
	e.click("reopen Local Shell")
	e.pump(func() bool {
		return len(e.sessions()) == 1 && len(e.w.tabs) == 1 && !e.w.tabs[0].isNew() && e.sidOf(0) != ""
	}, "a new session from Recently closed")
	e.waitScrollback(e.sidOf(0), "$ ")
	e.click("New tab") // the + button: ⌘T pressed again so soon would be held back as a key repeat
	e.pump(func() bool { return e.onStartPage() && e.tt.HasText("Recent") }, "the New tab page again")
	e.shot("TestE2ENewTabPage")
}

func TestE2EPalette(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2) // tab 1, split
	e.newSession() // tab 2
	e.pump(func() bool { return len(e.w.tabs) == 2 && e.sidOf(1) != "" && len(e.sessions()) == 3 }, "two tabs, three sessions")
	e.w.tabs[1].title = "logs"
	e.step()
	allPrompts := func() bool {
		ids := e.sessionIDs()
		for _, id := range ids {
			if !strings.Contains(e.scrollback(id), "$ ") {
				return false
			}
		}
		return len(ids) > 0
	}
	e.pump(allPrompts, "every session to show its prompt")

	// ⌘K: Open now, Hosts, Actions, in that order.
	e.key(ui.Cmd, ui.Key1) // tab 1 shows
	if e.w.active != 0 {
		t.Fatalf("active %d", e.w.active)
	}
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.hasLabel("dialog Command palette") && e.tt.Focused("Command palette") }, "the palette with the keyboard in its input")
	var groups []string
	for _, it := range e.w.pal.visible() {
		if len(groups) == 0 || groups[len(groups)-1] != it.group {
			groups = append(groups, it.group)
		}
	}
	// A new vault is seeded with four snippets, so the Snippets group follows Actions.
	if !slices.Equal(groups, []string{"Open now", "Hosts", "Actions", "Snippets"}) {
		t.Fatalf("groups %q", groups)
	}
	for _, s := range []string{"Open now", "Hosts", "Actions", "logs", "New session on Local Shell", "Split down"} {
		if !e.tt.HasText(s) {
			t.Errorf("the palette does not show %q", s)
		}
	}
	e.shot("TestE2EPalette-open")

	// An Open now row focuses its tab and starts nothing. A window key does not act while the palette is open.
	before := sorted(e.sessionIDs())
	e.key(ui.Cmd, ui.Key2)
	if e.w.pal == nil || e.w.active != 0 {
		t.Fatalf("a window key acted while the palette was open: pal %v, active %d", e.w.pal != nil, e.w.active)
	}
	e.tt.Type("logs")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.w.pal == nil && e.w.active == 1 }, "the logs tab to show")
	e.settle(300 * time.Millisecond)
	if !slices.Equal(sorted(e.sessionIDs()), before) {
		t.Fatalf("Open now started a session: %v (was %v)", e.sessionIDs(), before)
	}

	// A host with ⌥↵: a new pane in the focused tab.
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil }, "the palette")
	e.tt.Type("session on Local Shell")
	e.step()
	e.key(ui.Alt, ui.KeyEnter)
	e.pump(func() bool { return len(e.sessions()) == 4 && len(e.w.tabs) == 2 && len(e.w.tabs[1].panes) == 2 }, "a new pane in the focused tab")

	// An action: Split down.
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil }, "the palette")
	e.tt.Type("Split down")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return len(e.sessions()) == 5 && len(e.w.tabs[1].panes) == 3 }, "Split down: one more pane")
	e.pump(func() bool {
		for _, tb := range e.w.tabs {
			for _, p := range tb.panes {
				if p.sessionID() == "" {
					return false
				}
			}
		}
		return true
	}, "every pane to be named")
	e.pump(allPrompts, "every session to show its prompt")

	// Typing in the palette and Esc: no session sees a byte.
	scroll := map[string]string{}
	for _, id := range e.sessionIDs() {
		scroll[id] = e.scrollback(id)
	}
	e.key(ui.Cmd, ui.KeyK)
	e.pump(func() bool { return e.w.pal != nil && e.tt.Focused("Command palette") }, "the palette")
	e.tt.Type("qzxvkw")
	e.step()
	e.key(0, ui.KeyDown)
	e.key(0, ui.KeyEscape)
	e.pump(func() bool { return e.w.pal == nil }, "the palette to close")
	e.settle(500 * time.Millisecond)
	for id, was := range scroll {
		if got := e.scrollback(id); got != was {
			t.Errorf("session %s changed while the palette was used", id)
		}
	}
	if strings.Contains(e.scrollback(e.paneSID(e.w.tab().focused)), "qzxvkw") {
		t.Error("the palette's text reached a terminal")
	}
	e.shot("TestE2EPalette")
}

func TestE2EPastedSSHCommand(t *testing.T) {
	addr, dir := startServe(t, true)
	f := startSSH(t, addr, dir)
	e := newWin(t, addr, dir, nil)
	e.pump(e.onStartPage, "the New tab page")
	procs := sshProcesses(t)

	e.tt.Type(fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=no %s@%s uptime", f.port, f.user, f.host))
	e.pump(func() bool { return e.tt.HasText("Read as") }, "the offer")
	target := fmt.Sprintf("%s@%s:%d", f.user, f.host, f.port)
	for _, s := range []string{
		target, "Nothing has run.", "Left out (2), not applied", "-o StrictHostKeyChecking=no", "changes host key trust",
		"uptime", "remote command; Keel opens a shell and does not run it", "Connect once",
	} {
		if !e.tt.HasText(s) {
			t.Errorf("the offer lacks %q", s)
		}
	}
	e.shot("TestE2EPastedSSHCommand-offer")
	e.settle(500 * time.Millisecond)
	if n := len(e.sessions()); n != 0 {
		t.Fatalf("a pasted command started %d sessions", n)
	}
	if n := sshProcesses(t); n != procs {
		t.Fatalf("ssh processes: %d before, %d after the paste", procs, n)
	}
	if procs != 0 {
		t.Logf("%d unrelated processes match 'ssh -p' on this machine", procs)
	}

	// Connect once: the dialog asks for the password, the host key dialog follows, and the session is ready.
	e.click("Connect once")
	e.pump(func() bool { return e.w.quick != nil && e.hasLabel("dialog Quick connect") && e.tt.Focused("Password") }, "the Quick connect dialog with the Password field focused")
	if e.w.quick.target != target || e.w.quick.password != "" {
		t.Fatalf("target %q, password already typed: %v", e.w.quick.target, e.w.quick.password != "")
	}
	e.tt.Type(f.password)
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.hasLabel("dialog Unknown host key") }, "the unknown host key dialog")
	if !e.tt.HasText(f.fingerprint) {
		t.Fatal("the dialog does not show the fingerprint the server offered")
	}
	e.click("Trust and connect")
	e.pump(func() bool { return e.snap().Status == "ready" }, "the session to be ready")
	if n := len(e.sessions()); n != 1 {
		t.Fatalf("%d sessions", n)
	}
	if n := sshProcesses(t); n != procs {
		t.Fatalf("ssh processes: %d before, %d after Connect once", procs, n)
	}
	c, cancel := ctx()
	defer cancel()
	hosts, err := e.cl.Hosts(c)
	if err != nil {
		t.Fatal(err)
	}
	var saved []keelclient.Host
	for _, h := range hosts {
		if h.Group == "Quick connect" {
			saved = append(saved, h)
		}
	}
	if len(saved) != 1 || saved[0].Name != f.user+"@"+f.host || saved[0].Port != f.port || saved[0].Username != f.user || saved[0].AuthType != "password" {
		t.Fatalf("saved quick hosts %+v", saved)
	}
	e.shot("TestE2EPastedSSHCommand")
}

// ---- N5a: Unlock and the host dialog ----

// raw is one request to the daemon with the token (a test that creates or reads state behind the window's back).
// It returns the status and the body.
func (e *win) raw(method, path string, body any) (int, string) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, "http://"+e.addr+path, rd)
	req.Header.Set(authtoken.Header, e.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (e *win) hostNamed(name string) (keelclient.Host, bool) {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	hosts, err := e.cl.Hosts(c)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return keelclient.Host{}, false
}

func (e *win) texts() string { return strings.Join(e.tt.Texts(), "\n") }

// fillField clicks a field's label (which gives the field the keyboard), replaces what it holds and types s.
func (e *win) fillField(label, s string) {
	e.t.Helper()
	e.click(label)
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type(s)
	e.step()
}

func TestE2EUnlock(t *testing.T) {
	// A daemon with the scratch passphrase creates the vault; a second one on the same data does not get it.
	dir := scratchDir(t)
	dataDir := filepath.Join(dir, "data")
	first, stopFirst := startServeAt(t, dir, "serve", true)
	// Finish the first run, if one is open (a vault made from the environment has none), so that the workspace
	// shows after Unlock instead of First run.
	tok, err := authtoken.Read(dataDir)
	if err != nil {
		t.Fatal("read token: ", err)
	}
	req, _ := http.NewRequest("POST", "http://"+first+"/api/first-run/done", nil)
	req.Header.Set(authtoken.Header, tok)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first-run/done: %v", err)
	}
	stopFirst()
	addr, _ := startServeAt(t, dir, "serve-locked", false)
	var cb *countingBackend
	e := newWin(t, addr, dataDir, func(b backend) backend { cb = &countingBackend{backend: b}; return cb })
	e.pump(func() bool { return e.tt.HasText("Unlock your vault") }, "the Unlock screen")
	if e.tt.HasText("Welcome to Keel") || e.tt.HasText("Filter hosts") {
		t.Fatal("First run or the workspace shows with the Unlock screen")
	}
	if !e.tt.HasText("Keel was started with KEEL_KEYRING=off, so it needs your passphrase.") || !e.tt.Focused("Vault passphrase") {
		t.Error("the banner of a vault opened without the keychain, or the focused field, is missing")
	}
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EUnlock-locked")

	vaultLocked := func() bool {
		c, cancel := ctx()
		defer cancel()
		v, err := e.cl.Vault(c)
		if err != nil {
			t.Fatal(err)
		}
		return v.Locked
	}
	// A wrong passphrase: the error shows, the keyboard stays in the field and the daemon still says locked.
	e.tt.Type("wrong-" + e2ePass)
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.tt.HasText("That passphrase doesn't open the vault.") }, "the wrong passphrase error")
	if !vaultLocked() || !e.tt.Focused("Vault passphrase") || e.tt.HasText("Filter hosts") {
		t.Fatal("after a wrong passphrase: the vault is open, the focus left the field, or the workspace shows")
	}
	// The right one opens the workspace.
	e.tt.Type(e2ePass)
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.hasLabel("host Local Shell") && e.tt.HasText("Filter hosts") }, "the host tree with Local Shell")
	if vaultLocked() || e.w.gate != nil {
		t.Fatal("the vault is still locked, or the screen stayed")
	}
	if e.w.gate != nil || strings.Contains(fmt.Sprintf("%+v", e.w.gate), e2ePass) {
		t.Error("the passphrase is on the window")
	}
	e.settle(200 * time.Millisecond)
	e.shot("TestE2EUnlock")
}

func TestE2EHostDialogCreateTestEditDelete(t *testing.T) {
	addr, dir := startServe(t, true)
	f := startSSHOnly(t, dir)
	e := newWin(t, addr, dir, nil)
	e.openLocal() // a session tab: the New tab page's own New host is not on screen, so "New host" is the sidebar's +
	e.click("New host")
	e.pump(func() bool { return e.hasLabel("dialog New host") }, "the New host dialog")
	if !e.tt.Focused("Label") {
		t.Error("the Label field does not have the keyboard")
	}
	e.fillField("Label", "e2e-new")
	e.fillField("Hostname / IP", f.host)
	e.fillField("Port", fmt.Sprint(f.port))
	e.fillField("Username", f.user)
	e.fillField("Host password", f.password)
	if strings.Contains(e.texts(), f.password) {
		t.Fatal("the typed password is drawn: the field is not masked")
	}

	// Test connection: an unknown key, with the fixture's fingerprint; nothing is trusted by the test.
	e.click("Test connection")
	e.pump(func() bool { return e.tt.HasText("Host key unknown") }, "the unknown host key result")
	if !strings.Contains(e.texts(), f.fingerprint) || !e.hasLabel("Trust and save") {
		t.Fatal("the result does not show the fixture's fingerprint and the trust button")
	}
	e.settle(300 * time.Millisecond)
	if e.tt.HasText("Connected in") {
		t.Fatal("the test went on to connect without the key being trusted")
	}
	e.shot("TestE2EHostDialogCreateTestEditDelete-test-unknown")
	// Trust from the result (the user's click), which tests again.
	e.click("Trust and save")
	e.pump(func() bool { return strings.Contains(e.texts(), "Connected in") }, "the Connected result")
	e.shot("TestE2EHostDialogCreateTestEditDelete-test-ok")

	// Save: the server has the host with those fields, and the response carries no password.
	e.click("Save host")
	e.pump(func() bool { return e.w.hostDlg == nil && e.hasLabel("host e2e-new") }, "the saved host in the tree")
	h, ok := e.hostNamed("e2e-new")
	if !ok || h.Address != f.host || h.Port != f.port || h.Username != f.user || h.AuthType != "password" {
		t.Fatalf("saved host %+v", h)
	}
	if _, body := e.raw("GET", "/api/hosts", nil); strings.Contains(body, f.password) {
		t.Fatal("GET /api/hosts returns the password")
	}

	// Open it: ready with no host key dialog (the key was trusted from the test result).
	e.click("host e2e-new")
	e.pump(func() bool { return e.snap().Status == "ready" || e.hasLabel("dialog Unknown host key") }, "the session to be ready")
	if e.hasLabel("dialog Unknown host key") || e.snap().Status != "ready" {
		t.Fatalf("status %q, host key dialog %v", e.snap().Status, e.hasLabel("dialog Unknown host key"))
	}

	// Edit from the host menu: rename, leave the password blank. The saved password is kept.
	if err := e.tt.RightClick("host e2e-new"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Edit"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.hasLabel("dialog Edit host") }, "the Edit host dialog")
	e.pump(func() bool { return e.tt.HasText("Trusted") }, "the saved host key line")
	if strings.Contains(e.texts(), f.password) || e.w.hostDlg.f.password != "" {
		t.Fatal("the edit dialog shows or holds the saved password")
	}
	e.fillField("Label", "e2e-renamed")
	e.shot("TestE2EHostDialogCreateTestEditDelete-edit")
	e.click("Save host")
	e.pump(func() bool { _, ok := e.hostNamed("e2e-renamed"); return e.w.hostDlg == nil && ok }, "the renamed host on the server")
	e.pump(func() bool { return e.hasLabel("host e2e-renamed") && !e.hasLabel("host e2e-new") }, "the renamed host in the tree")
	n := len(e.w.tabs)
	e.click("host e2e-renamed")
	e.pump(func() bool { return len(e.w.tabs) == n+1 && (e.snap().Status == "ready" || e.snap().Status == "error") }, "the second session")
	if e.snap().Status != "ready" {
		t.Fatalf("after the rename the host no longer connects: %q %q", e.snap().Status, e.snap().Message)
	}

	// Delete with the confirm: gone from the server and the tree; the open sessions stay.
	sessions := len(e.sessions())
	if err := e.tt.RightClick("host e2e-renamed"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.tt.ChooseMenuItem("Edit")
	e.pump(func() bool { return e.hasLabel("dialog Edit host") }, "the Edit host dialog again")
	e.click("Delete host")
	e.pump(func() bool { return e.tt.HasText("Remove e2e-renamed?") }, "the confirm")
	if _, ok := e.hostNamed("e2e-renamed"); !ok {
		t.Fatal("the host was deleted before the confirm")
	}
	e.click("Remove")
	e.pump(func() bool {
		_, ok := e.hostNamed("e2e-renamed")
		return !ok && !e.hasLabel("host e2e-renamed") && e.w.hostDlg == nil
	}, "the host gone")
	if got := len(e.sessions()); got != sessions {
		t.Errorf("deleting the host changed the sessions: %d → %d", sessions, got)
	}
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EHostDialogCreateTestEditDelete")
}

func TestE2ESaveAsHostFromPastedCommand(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(e.onStartPage, "the New tab page")
	count := func() int {
		c, cancel := ctx()
		defer cancel()
		hosts, err := e.cl.Hosts(c)
		if err != nil {
			t.Fatal(err)
		}
		return len(hosts)
	}
	before := count()
	port := freePort(t)
	e.tt.Type(fmt.Sprintf("ssh -p %d deploy@127.0.0.1", port))
	e.pump(func() bool { return e.hasLabel("Save as host") }, "the offer with Save as host")
	e.click("Save as host")
	e.pump(func() bool { return e.hasLabel("dialog New host") }, "New host")
	d := e.w.hostDlg
	if d.f.address != "127.0.0.1" || d.f.port != fmt.Sprint(port) || d.f.username != "deploy" || d.f.name != "127.0.0.1" || d.id != "" {
		t.Fatalf("prefill %v", d.f)
	}
	e.shot("TestE2ESaveAsHostFromPastedCommand")
	e.click("Cancel")
	e.pump(func() bool { return e.w.hostDlg == nil }, "the dialog to close")
	e.settle(300 * time.Millisecond)
	if got := count(); got != before || len(e.sessions()) != 0 {
		t.Fatalf("hosts %d → %d, sessions %d: Cancel sent something", before, got, len(e.sessions()))
	}
}

func TestE2EHostListFollowsTheVault(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.hasLabel("host Local Shell") }, "the host tree")
	e.w.startPoll(200 * time.Millisecond) // Run's poll, a faster tick
	defer e.w.closeWindow()
	code, body := e.raw("POST", "/api/hosts", map[string]any{"name": "made-elsewhere", "address": "10.9.9.9", "port": 22, "username": "u", "authType": "password", "password": "x"})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	var made struct{ ID string }
	json.Unmarshal([]byte(body), &made)
	e.pump(func() bool { return e.hasLabel("host made-elsewhere") }, "the tree to show a host created behind the window's back")
	if code, _ := e.raw("DELETE", "/api/hosts/"+made.ID, nil); code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	e.pump(func() bool { return !e.hasLabel("host made-elsewhere") }, "the host to leave the tree")
	e.shot("TestE2EHostListFollowsTheVault")
}
