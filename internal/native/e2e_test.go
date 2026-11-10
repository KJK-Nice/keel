//go:build nativee2e

package native

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/authtoken"
	"keel/internal/native/keelclient"
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

// startServe runs a scratch keel serve (bin/keel) and stops it when the test ends. A passphrase
// opens a scratch vault; without one the vault is locked.
func startServe(t *testing.T, passphrase bool) (addr, dataDir string) {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "keel")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("%s is missing: run make native-e2e (it builds bin/keel first)", bin)
	}
	dir := filepath.Join(root, ".scratch-native", "e2e", t.Name())
	os.RemoveAll(dir)
	dataDir = filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	addr = fmt.Sprintf("127.0.0.1:%d", freePort(t))
	env := append(os.Environ(), "KEEL_ADDR="+addr, "KEEL_DATA="+dataDir, "KEEL_KEYRING=off", "SHELL=/bin/sh")
	if passphrase {
		env = append(env, "KEEL_VAULT_PASSPHRASE=native-e2e-scratch")
	}
	logf, err := os.Create(filepath.Join(dir, "serve.log"))
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
	t.Cleanup(func() {
		stop := exec.Command(bin, "serve", "stop")
		stop.Env = env
		stop.Run()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill() // our own child, by handle
			<-done
		}
		logf.Close()
	})
	deadline := time.Now().Add(e2eWait)
	for time.Now().Before(deadline) {
		if r, err := http.Get("http://" + addr + "/api/health"); err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return addr, dataDir
			}
		}
		select {
		case <-done:
			t.Fatalf("keel serve exited early; see %s", filepath.Join(dir, "serve.log"))
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("keel serve did not come up; see %s", filepath.Join(dir, "serve.log"))
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
}

// newWin opens a window on the daemon: the real view, the real terminal, the real client.
func newWin(t *testing.T, addr, dataDir string, wrap func(backend) backend) *win {
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
	e.w.mkBody = liveBody
	e.w.work = func(fn func()) { go fn() }
	e.w.post = func(fn func()) { e.posts <- fn }
	e.w.start()
	e.tt = ui.NewTester(e.w.view, 1280, 800)
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

func termBgColor() ui.Color { return tok(hexTermBg) }

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
	for _, tool := range []string{"Split right", "Split down", "Close pane"} {
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
	e.click("New tab")
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

func TestE2ELockedVault(t *testing.T) {
	addr, dir := startServe(t, false)
	var cb *countingBackend
	e := newWin(t, addr, dir, func(b backend) backend { cb = &countingBackend{backend: b}; return cb })
	e.pump(func() bool { return e.tt.HasText(lockedMessage) }, "the locked message")
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
		e.click("New tab")
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
}

// startSSH runs bin/keel-sshtest, creates a password host for its target on the daemon through
// POST /api/hosts (the client itself never creates hosts) and returns the fixture. The password is
// read from the file keel-sshtest writes and goes only into that request.
func startSSH(t *testing.T, addr, dataDir string) *sshFixture {
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
	var info struct {
		Target struct {
			Host        string
			Port        int
			User        string
			Password    string
			Fingerprint string
		}
	}
	for end := time.Now().Add(e2eWait); ; time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(out); err == nil && json.Unmarshal(b, &info) == nil && info.Target.Port != 0 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("keel-sshtest did not write its file")
		}
	}
	f.fingerprint = info.Target.Fingerprint
	tok, err := authtoken.Read(dataDir)
	if err != nil {
		t.Fatal("read token: ", err)
	}
	body, _ := json.Marshal(map[string]any{
		"name": "e2e-ssh", "address": info.Target.Host, "port": info.Target.Port, "username": info.Target.User,
		"authType": "password", "password": info.Target.Password, "noAutoReconnect": true,
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
		return len(b.w.tabs) == 1 && b.snap().Role != "" && a.snap().Role != "" && len(a.snap().Attached) == 2
	}, "both windows attached")
	pumpAll(t, both, func() bool {
		return b.hasLabel("role Following") && b.hasLabel("banner following") && a.hasLabel("role Driving")
	}, "the chips and the follower banner")
	if a.hasLabel("banner following") {
		t.Error("the driver shows the follower banner")
	}
	b.shot("TestE2EFollowerAndTakeControl")

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

	// keel serve lets only the session's owner take control (runtime.Session.TakeControl: ErrNotOwner), and
	// window A, which opened the session, owns it. So B's click must change nothing.
	b.click("Take control")
	for end := time.Now().Add(1 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		a.step()
		b.step()
	}
	if b.snap().Role != "follower" || a.snap().Role != "driver" {
		t.Fatalf("roles after Take control: b %q a %q", b.snap().Role, a.snap().Role)
	}
	t.Skip("plan 006 step 4.1 cannot pass as written: B is not the owner, so keel serve refuses take-control (ErrNotOwner); the rest of the scenario (B drives after Take control, A shows the follower banner) needs the owner to be the follower, which needs grant-control (phase N6)")
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
	e.pump(func() bool { return len(e.w.tabs) == 0 }, "the pane to go")
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
