//go:build nativee2e

package native

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/authtoken"
	"keel/internal/native/keelclient"
)

// Port forwarding, the sidebar's Panes section, Settings and toasts against a scratch keel serve. Every daemon has
// KEEL_KEYRING=off and its own KEEL_DATA under .scratch-native/e2e. Every listener (the daemon, the SSH fixture, each
// forward's local port, the test's own target) is on 127.0.0.1 and on a port the kernel chose. No forward binds anything
// else.

// addJumpHost saves a password host for the fixture's forwarding server (the target refuses direct-tcpip) with
// POST /api/hosts. The password goes only into that request.
func addJumpHost(t *testing.T, addr, dataDir string, f *sshFixture) {
	t.Helper()
	tok, err := authtoken.Read(dataDir)
	if err != nil {
		t.Fatal("read token: ", err)
	}
	body, _ := json.Marshal(map[string]any{
		"name": "e2e-jump", "address": f.jump.host, "port": f.jump.port, "username": f.jump.user,
		"authType": "password", "password": f.jump.password, "noAutoReconnect": true,
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
}

// startPong listens on a free loopback port and answers every connection with "pong-<n>\n". It returns the port.
func startPong(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	var mu sync.Mutex
	n := 0
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			n++
			k := n
			mu.Unlock()
			fmt.Fprintf(c, "pong-%d\n", k)
			c.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

// dialPong connects to a local port and reads the line the target answers with.
func dialPong(port int) (string, error) {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(line), err
}

// tunnelsOnServer is GET /api/tunnels.
func (e *win) tunnelsOnServer() []keelclient.Tunnel {
	e.t.Helper()
	_, body := e.raw("GET", "/api/tunnels", nil)
	var out []keelclient.Tunnel
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// tunnelNamed is the forward with that name on the daemon.
func (e *win) tunnelNamed(name string) (keelclient.Tunnel, bool) {
	for _, tn := range e.tunnelsOnServer() {
		if tn.Name == name {
			return tn, true
		}
	}
	return keelclient.Tunnel{}, false
}

// openForwardsPage opens the Port forwarding page the way a person does: the sidebar's Forwards section, then Manage forwards.
func (e *win) openForwardsPage() {
	e.t.Helper()
	e.click("Forwards")
	e.click("Manage forwards")
	e.pump(func() bool {
		return e.hasLabel("tab Port forwarding") && e.tt.HasText("Each forward opens its own SSH connection.")
	}, "the Port forwarding page")
}

func TestE2ELocalForward(t *testing.T) {
	addr, dir := startServe(t, true)
	f := startSSHOnly(t, dir)
	addJumpHost(t, addr, dir, f)
	target := startPong(t)
	local := freePort(t)
	e := newWin(t, addr, dir, nil)
	e.w.startPoll(200 * time.Millisecond)
	defer e.w.closeWindow()

	// Trust the forwarding server's key once, by opening it as a session and answering the dialog.
	e.pump(func() bool { return e.hasLabel("host e2e-jump") }, "the host tree")
	e.click("host e2e-jump")
	e.pump(func() bool { return e.hasLabel("dialog Unknown host key") }, "the unknown host key dialog")
	if !e.tt.HasText(f.jump.fingerprint) {
		t.Fatal("the dialog does not show the fingerprint the server offered")
	}
	e.click("Trust and connect")
	e.pump(func() bool { return e.snap().Status == "ready" }, "the session to be ready")
	e.click("Close pane")
	e.pump(func() bool { return len(e.w.tabs) == 1 && e.w.tabs[0].isNew() }, "the pane to close")

	e.openForwardsPage()
	e.click("New forward")
	e.pump(func() bool { return e.hasLabel("dialog New forward") }, "the New forward dialog")
	e.fillField("Forward name", "e2e fwd")
	e.click("Forward host picker")
	if err := e.tt.ChooseMenuItem("e2e-jump"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.fillField("Bind port", strconv.Itoa(local))
	e.fillField("Dest port", strconv.Itoa(target))
	e.click("Save forward")
	e.pump(func() bool { _, ok := e.tunnelNamed("e2e fwd"); return ok && e.hasLabel("forward e2e fwd") }, "the forward to be saved and listed")
	tn, _ := e.tunnelNamed("e2e fwd")
	if tn.Kind != "local" || tn.BindAddr != "127.0.0.1" || tn.BindPort != local || tn.DestHost != "127.0.0.1" || tn.DestPort != target || tn.Status != "stopped" {
		t.Fatalf("saved %+v", tn)
	}
	if !e.tt.HasText("Stopped") || e.tt.HasText("1 forward") {
		t.Error("a new forward shows as running before it is turned on")
	}

	// Turn it on: the daemon says running, and a connection to the local port reaches the test's listener through SSH.
	e.click("e2e fwd on")
	e.pump(func() bool { tn, _ := e.tunnelNamed("e2e fwd"); return tn.Status == "ready" }, "the forward to run")
	var got string
	e.pump(func() bool { var err error; got, err = dialPong(local); return err == nil }, "the local port to answer")
	if !strings.HasPrefix(got, "pong-") {
		t.Fatalf("the local port answered %q", got)
	}
	e.pump(func() bool {
		return e.tt.HasText("1 forward") && e.hasLabel("sidebar forward e2e fwd") && e.tt.HasText("Running")
	}, "the status bar, the sidebar and the page to show it running")
	e.tt.Advance(toastTimeout + time.Second) // the switch's slide and the "Forward saved" toast are over
	e.step()
	e.shot("TestE2ELocalForward-running")

	// Turn it off: the daemon says stopped and the port refuses.
	e.click("e2e fwd on")
	e.pump(func() bool { tn, _ := e.tunnelNamed("e2e fwd"); return tn.Status == "stopped" }, "the forward to stop")
	e.pump(func() bool { _, err := dialPong(local); return err != nil }, "the local port to refuse")
	e.pump(func() bool { return !e.tt.HasText("1 forward") && e.tt.HasText("Stopped") }, "the window to show it stopped")

	// Delete asks first.
	if err := e.tt.RightClick("forward e2e fwd"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.pump(func() bool { return e.hasLabel("alertdialog Confirm") && e.tt.HasText("Delete forward “e2e fwd”?") }, "the confirm")
	e.click("Cancel")
	if _, ok := e.tunnelNamed("e2e fwd"); !ok {
		t.Fatal("Cancel deleted the forward")
	}
	if err := e.tt.RightClick("forward e2e fwd"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	e.step()
	e.click("Remove")
	e.pump(func() bool { return len(e.tunnelsOnServer()) == 0 && !e.hasLabel("forward e2e fwd") }, "the forward to go")
	e.shot("TestE2ELocalForward")
}

func TestE2EForwardFailureShows(t *testing.T) {
	addr, dir := startServe(t, true)
	f := startSSHOnly(t, dir)
	addJumpHost(t, addr, dir, f)
	jump, ok := (&win{t: t, addr: addr, cl: keelclient.NewAt(addr, mustToken(t, dir))}).hostNamed("e2e-jump")
	if !ok {
		t.Fatal("the host was not saved")
	}
	e := newWin(t, addr, dir, nil)
	e.w.startPoll(200 * time.Millisecond)
	defer e.w.closeWindow()
	code, _ := e.raw("POST", "/api/tunnels", map[string]any{"name": "e2e dead", "hostId": jump.ID, "kind": "local",
		"bindAddr": "127.0.0.1", "bindPort": freePort(t), "destHost": "127.0.0.1", "destPort": freePort(t)})
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("create forward: %d", code)
	}
	f.stop() // the host's server is gone
	e.pump(func() bool { return e.hasLabel("host e2e-jump") }, "the host tree")
	e.openForwardsPage()
	e.pump(func() bool { return e.hasLabel("forward e2e dead") }, "the forward in the list")
	e.click("e2e dead on")
	var cause string
	e.pump(func() bool {
		tn, _ := e.tunnelNamed("e2e dead")
		cause = tn.Error
		return tn.Status == "error" && cause != "" && strings.Contains(e.texts(), "Failed · "+cause)
	}, "the row to show the failure and its cause")
	e.pump(func() bool { return e.hasLabel("Retry") }, "Retry")
	if e.tt.HasText("1 forward") {
		t.Error("a failed forward counts as running in the status bar")
	}
	e.shot("TestE2EForwardFailureShows")
}

func mustToken(t *testing.T, dataDir string) string {
	t.Helper()
	tok, err := authtoken.Read(dataDir)
	if err != nil {
		t.Fatal("read token: ", err)
	}
	return tok
}

func TestE2EPanesSection(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	if e.hasLabel("Panes 1") || e.tt.HasText("Panes") {
		t.Fatal("Panes shows for a one-pane tab")
	}
	e.key(ui.Cmd|ui.Shift, ui.KeyD) // split right twice: three panes in a row
	e.pump(func() bool { return e.w.paneCount() == 2 && len(e.sessions()) == 2 }, "two sessions")
	e.key(ui.Cmd|ui.Shift, ui.KeyD)
	e.pump(func() bool { return e.w.paneCount() == 3 && len(e.sessions()) == 3 }, "three sessions")
	e.namePanes()
	e.pump(func() bool { return e.hasLabel("Panes 3") }, "the Panes segment with its count")
	if e.w.sideSeg == "panes" {
		t.Error("a split showed Panes by itself")
	}
	// A click into a terminal of the split tab shows Panes and leaves the keyboard in the terminal.
	e.clickBody("1")
	e.pump(func() bool { return e.hasLabel("Filter panes") }, "the Panes section after a click into a pane")
	if !e.tt.Focused("pane 1") && e.w.tab().focused != "1" {
		t.Errorf("focused %q", e.w.tab().focused)
	}
	n := 0
	for _, i := range []string{"1", "2", "3"} {
		for _, s := range e.tt.Texts() {
			if strings.HasPrefix(s, "Focus pane "+i+", ") {
				n++
				break
			}
		}
	}
	if n != 3 {
		t.Errorf("the map has %d cells, want 3: %v", n, e.tt.Texts())
	}
	// The second row (rows share the host's name: they are told apart by position) focuses the second pane, and typing lands in its session.
	row, ok := e.tt.Find("pane row Local Shell")
	if !ok {
		t.Fatal("no pane rows")
	}
	e.tt.ClickAt(row.X+row.W/2, row.Y+(paneRowHeight+space1)+row.H/2)
	e.step()
	if e.w.tab().focused != "2" {
		t.Fatalf("the second row focused pane %q", e.w.tab().focused)
	}
	e.typeLine("echo row-$((2+3))")
	e.waitScrollback(e.paneSID("2"), "row-5")
	e.settle(300 * time.Millisecond)
	for _, id := range []string{"1", "3"} {
		if strings.Contains(e.scrollback(e.paneSID(id)), "row-5") {
			t.Errorf("the line typed after picking row 2 reached pane %s", id)
		}
	}
	assertFits(t, e.w, e.tt, 1280, 800) // three whole tiles inside the window
	e.shot("TestE2EPanesSection")
	// Closing down to one pane hides the segment and falls back to Hosts.
	for want := 2; want >= 1; want-- {
		e.click("Close pane")
		e.pump(func() bool { return e.w.paneCount() == want && len(e.sessions()) == want }, fmt.Sprintf("%d sessions", want))
	}
	e.pump(func() bool { return !e.tt.HasText("Panes") && e.hasLabel("Filter hosts") }, "the sidebar to fall back to Hosts")
	if e.w.sideSeg == "panes" {
		t.Error("the sidebar stayed on Panes")
	}
}

// useSavedWindowPrefs is what Run does at start: the window prefs from keel-native.json, and a translucent window
// when the opacity is below 100.
func (e *win) useSavedWindowPrefs() {
	e.w.prefs = loadPrefs(e.w.dataDir)
	if e.w.prefs.Opacity < opacityMax {
		e.w.translucentWin, e.w.alpha = true, e.w.prefs.alpha()
	}
	e.tt.Frame()
}

func TestE2ESettingsAppearanceLive(t *testing.T) {
	dir := scratchDir(t)
	data := filepath.Join(dir, "data")
	seed := `{"opacity":66,"blur":20,"termSize":12,"recentHosts":["nowhere"],"future":{"keep":true}}`
	if err := os.WriteFile(filepath.Join(data, "keel-native.json"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	addr, _ := startServeAt(t, dir, "serve", true)
	e := newWin(t, addr, data, nil)
	e.useSavedWindowPrefs()
	e.pump(func() bool { return e.hasLabel("host Local Shell") }, "the host tree")
	titleAlpha := func() uint8 {
		img := e.tt.Image()
		s := float64(img.Bounds().Dx()) / 1280
		return img.RGBAAt(int(900*s), int(10*s)).A
	}
	if a := titleAlpha(); a < 166 || a > 170 {
		t.Fatalf("the title bar starts at alpha %d, want about 168 (0.66)", a)
	}
	e.key(ui.Cmd, ui.KeyComma)
	e.pump(func() bool { return e.hasLabel("tab Settings") && e.hasLabel("Window opacity slider") }, "Settings · Appearance")
	e.shot("TestE2ESettingsAppearanceLive-appearance")
	// 66 to 80 by the keyboard: a click puts the slider in the middle (65), the arrow keys step it.
	e.click("Window opacity slider")
	for e.w.prefs.Opacity < 80 {
		e.key(0, ui.KeyRight)
	}
	e.pump(func() bool { a := titleAlpha(); return a >= 202 && a <= 206 }, "the title bar to repaint at about 0.8")
	var f map[string]json.RawMessage
	e.pump(func() bool {
		b, err := os.ReadFile(filepath.Join(data, "keel-native.json"))
		return err == nil && json.Unmarshal(b, &f) == nil && string(f["opacity"]) == "80"
	}, "keel-native.json to hold opacity 80")
	if string(f["blur"]) != "20" || string(f["termSize"]) != "12" || !strings.Contains(string(f["recentHosts"]), "nowhere") || !strings.Contains(string(f["future"]), "keep") {
		t.Errorf("the file lost a field: %v", f)
	}
	e.w.closeWindow()
	// A second window on the same data starts at 80.
	e2 := newWin(t, addr, data, nil)
	e2.useSavedWindowPrefs()
	if e2.w.prefs.Opacity != 80 || !(e2.w.alpha > 0.79 && e2.w.alpha < 0.81) {
		t.Fatalf("the second window starts at opacity %d alpha %v", e2.w.prefs.Opacity, e2.w.alpha)
	}
	e2.key(ui.Cmd, ui.KeyComma)
	e2.pump(func() bool { return e2.hasLabel("Window opacity slider") }, "Settings in the second window")
	if e2.w.pageTab("settings").pg.settings.opacityV != 80 {
		t.Errorf("the slider starts at %v", e2.w.pageTab("settings").pg.settings.opacityV)
	}
	e2.w.closeWindow()
}

func TestE2ESettingsRuntimeStop(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	e.key(ui.Cmd, ui.KeyComma)
	e.pump(func() bool { return e.hasLabel("tab Settings") }, "Settings")
	e.click("settings nav Runtime")
	e.pump(func() bool { return e.hasLabel("value 1") && e.tt.HasText("Running sessions") }, "the Runtime section with one session")
	if want := versionOf(e.w.pageTab("settings").pg.settings.rt.Build); want == "" || !e.tt.HasText(want) {
		t.Errorf("the version %q is not shown", want)
	}
	e.shot("TestE2ESettingsRuntimeStop-runtime")
	health := func() bool {
		c := http.Client{Timeout: time.Second}
		r, err := c.Get("http://" + addr + "/api/health")
		if err != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == 200
	}
	// Stop asks first with the app's words; Cancel changes nothing.
	e.click("Stop runtime…")
	e.pump(func() bool { return e.hasLabel("alertdialog Stop the Keel runtime?") }, "the confirm")
	if !e.tt.HasText("This ends the 1 running session and every port forward, then closes keel serve. Reopen Keel to start it again.") {
		t.Errorf("the confirm says %v", e.tt.Texts())
	}
	e.click("Cancel")
	e.settle(500 * time.Millisecond)
	if !health() || len(e.sessions()) != 1 {
		t.Fatal("Cancel stopped the runtime")
	}
	// Confirm: the daemon stops answering within 5 s, and the window says so.
	e.click("Stop runtime…")
	e.pump(func() bool { return e.hasLabel("alertdialog Stop the Keel runtime?") }, "the confirm")
	e.click("Stop runtime")
	deadline := time.Now().Add(5 * time.Second)
	e.pump(func() bool { return !health() }, "keel serve to stop answering")
	if time.Now().After(deadline) {
		t.Error("keel serve took more than 5 s to stop")
	}
	e.pump(func() bool { return e.tt.HasText("Keel runtime stopped") }, "the Keel runtime stopped toast")
	e.shot("TestE2ESettingsRuntimeStop")
}

func TestE2EToasts(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.hasLabel("host Local Shell") }, "the host tree")
	// A success toast: duplicating a host from its menu says "Duplicated as …".
	code, _ := e.raw("POST", "/api/hosts", map[string]any{"name": "toast-host", "address": "10.9.9.9", "port": 22, "username": "u", "authType": "password", "password": "x"})
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	e.w.startPoll(200 * time.Millisecond)
	defer e.w.closeWindow()
	e.pump(func() bool { return e.hasLabel("host toast-host") }, "the host tree to show the host")
	if err := e.tt.RightClick("host toast-host"); err != nil {
		t.Fatal(err)
	}
	e.step()
	if err := e.tt.ChooseMenuItem("Duplicate"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.tt.HasText("Duplicated as toast-host (copy)") }, "the success toast")
	e.shot("TestE2EToasts-success")
	e.tt.Advance(toastTimeout + time.Second)
	e.step()
	e.pump(func() bool { return !e.tt.HasText("Duplicated as toast-host (copy)") }, "the success toast to expire")
	// An error toast: keel serve refuses a signal server that is not a URL, and says why.
	e.key(ui.Cmd, ui.KeyComma)
	e.pump(func() bool { return e.hasLabel("tab Settings") }, "Settings")
	e.click("settings nav Sharing")
	e.pump(func() bool { return e.hasLabel("Signal server input") && e.w.pageTab("settings").pg.settings.rt != nil }, "the Sharing section")
	code, body := e.raw("PUT", "/api/share/signal", map[string]string{"url": "ftp://not-a-signal-server"})
	var refusal struct{ Error string }
	json.Unmarshal([]byte(body), &refusal)
	if code != http.StatusBadRequest || refusal.Error == "" {
		t.Fatalf("keel serve answered %d %q: expected a refusal with a reason", code, body)
	}
	e.fillField("Signal server input", "ftp://not-a-signal-server")
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.hasLabel("toast " + refusal.Error) }, "the error toast with keel serve's reason")
	e.shot("TestE2EToasts-error")
	e.tt.Advance(toastTimeout + time.Second)
	e.step()
	e.pump(func() bool { return !e.errToastShown() }, "the error toast to expire")
	if got, ok := e.hostNamed("toast-host"); !ok || got.Name == "" {
		t.Error("the host went")
	}
}

// errToastShown reports a toast on screen: its label is "toast <text>".
func (e *win) errToastShown() bool {
	for _, s := range e.tt.Texts() {
		if strings.HasPrefix(s, "toast ") {
			return true
		}
	}
	return false
}
