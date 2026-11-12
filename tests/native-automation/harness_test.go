//go:build nativeautomation

// Package nativeautomation drives the built native app from outside, through MyGo's automation socket (plan 029):
// a test binary (bin/keel-e2e, built with the tag keelautomation by make native-automation-build) opens a real
// window, and these tests speak the socket's newline-delimited JSON protocol to it. Everything runs on scratch
// data in a fresh temp directory, with a free loopback port, no keychain and the fake agent provider.
package nativeautomation

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// scratchPass opens the scratch vault of a daemon the test starts; it opens nothing else and is never printed.
const scratchPass = "native-automation-scratch"

type node struct {
	ID     string   `json:"id"`
	Parent string   `json:"parent"`
	Role   string   `json:"role"`
	Label  string   `json:"label"`
	Value  string   `json:"value"`
	States []string `json:"states"`
	Bounds struct{ X, Y, Width, Height float64 }
}

type rig struct {
	t                          *testing.T
	root, bin, dir, data, sock string
	addr                       string
	app, serve                 *exec.Cmd
	appDone, serveDone         chan struct{}
	conn                       net.Conn
	rd                         *bufio.Reader
	n                          int
	appEnv                     []string // extra environment of the app only
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

// freePort is a free loopback port that is none of the user's own Keel ports.
func freePort(t *testing.T) int {
	t.Helper()
	for i := 0; i < 20; i++ {
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
	t.Fatal("no free port")
	return 0
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

// newRig makes the scratch directory (mode 0700, with the socket's own 0700 directory inside) and registers the
// cleanup that leaves no process, socket or directory behind.
func newRig(t *testing.T) *rig {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "keel-e2e")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("%s is missing: run make native-automation (it builds it)", bin)
	}
	dir, err := os.MkdirTemp("", "kauto")
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, root: root, bin: bin, dir: dir, data: filepath.Join(dir, "data"),
		sock: filepath.Join(dir, "s", "a.sock"), addr: fmt.Sprintf("127.0.0.1:%d", freePort(t))}
	for _, d := range []string{r.data, filepath.Dir(r.sock), filepath.Join(dir, "ssh")} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(r.cleanup)
	return r
}

// env is the process environment of every Keel binary here. Nothing of the user's KEEL_ or MYGO_ settings passes.
func (r *rig) env(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "KEEL_") && !strings.HasPrefix(kv, "MYGO_") && !strings.HasPrefix(kv, "SHELL=") {
			env = append(env, kv)
		}
	}
	env = append(env, "KEEL_ADDR="+r.addr, "KEEL_DATA="+r.data, "KEEL_KEYRING=off", "KEEL_AGENT_PROVIDER=fake",
		"KEEL_SSH_DIR="+filepath.Join(r.dir, "ssh"), "SHELL=/bin/sh")
	return append(env, extra...)
}

func (r *rig) start(name string, extraEnv []string, args ...string) (*exec.Cmd, chan struct{}) {
	r.t.Helper()
	logf, err := os.Create(filepath.Join(r.dir, name+".log"))
	if err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command(r.bin, args...)
	cmd.Env, cmd.Stdout, cmd.Stderr = r.env(extraEnv...), logf, logf
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); logf.Close(); close(done) }()
	return cmd, done
}

// startServe runs the scratch daemon with a scratch vault (no first run), so the window opens on the workspace.
func (r *rig) startServe() {
	r.t.Helper()
	r.serve, r.serveDone = r.start("serve", []string{"KEEL_VAULT_PASSPHRASE=" + scratchPass}, "serve")
	for i := 0; i < 150; i++ {
		if c, err := net.DialTimeout("tcp", r.addr, time.Second); err == nil {
			c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	r.t.Fatal("the scratch daemon did not listen")
}

// startApp runs the window with the socket on, connects, and waits for the first window.
func (r *rig) startApp() {
	r.t.Helper()
	r.app, r.appDone = r.start("app", append([]string{"MYGO_AUTOMATION=" + r.sock}, r.appEnv...), "app")
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case <-r.appDone:
			r.t.Fatal("the app exited before its socket answered (see app.log in the scratch dir)")
		default:
		}
		if c, err := net.Dial("unix", r.sock); err == nil {
			r.conn, r.rd = c, bufio.NewReader(c)
			break
		}
		if time.Now().After(deadline) {
			r.t.Fatal("the automation socket never appeared")
		}
		time.Sleep(100 * time.Millisecond)
	}
	r.waitWindows(func(ws []window) bool { return len(ws) > 0 }, "a window")
}

type window struct {
	ID      int  `json:"id"`
	Visible bool `json:"visible"`
	Focused bool `json:"focused"`
}

func (r *rig) windows() []window {
	var res struct{ Windows []window }
	r.must("windows", nil, &res)
	return res.Windows
}

func (r *rig) waitWindows(cond func([]window) bool, what string) {
	r.t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if cond(r.windows()) {
			return
		}
	}
	r.t.Fatalf("timed out waiting for %s", what)
}

// call sends one request and reads its answer, with a deadline.
func (r *rig) call(method string, params any) (json.RawMessage, error) {
	r.n++
	line, err := json.Marshal(map[string]any{"id": r.n, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	r.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := r.conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	ans, err := r.rd.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var res struct {
		Result json.RawMessage
		Error  *struct{ Code, Message string }
	}
	if err := json.Unmarshal(ans, &res); err != nil {
		return nil, err
	}
	if res.Error != nil {
		return nil, fmt.Errorf("%s: %s", res.Error.Code, res.Error.Message)
	}
	return res.Result, nil
}

func (r *rig) must(method string, params any, out any) {
	r.t.Helper()
	raw, err := r.call(method, params)
	if err != nil {
		r.t.Fatalf("%s: %v", method, err)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			r.t.Fatalf("%s: %v", method, err)
		}
	}
}

func (r *rig) tree() []node {
	r.t.Helper()
	var res struct{ Nodes []node }
	r.must("tree", nil, &res)
	return res.Nodes
}

func has(label string) func([]node) bool {
	return func(ns []node) bool { return find(ns, label) != nil }
}

func lacks(label string) func([]node) bool {
	return func(ns []node) bool { return find(ns, label) == nil }
}

func find(ns []node, label string) *node {
	for i := range ns {
		if ns[i].Label == label {
			return &ns[i]
		}
	}
	return nil
}

func hasPrefix(p string) func([]node) bool {
	return func(ns []node) bool {
		for _, n := range ns {
			if strings.HasPrefix(n.Label, p) {
				return true
			}
		}
		return false
	}
}

// waitFor settles the window and reads its tree until cond holds. No sleeps outside this loop.
func (r *rig) waitFor(what string, cond func([]node) bool) []node {
	r.t.Helper()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(150 * time.Millisecond) {
		r.call("idle", map[string]any{"timeout": 3000})
		if ns := r.tree(); cond(ns) {
			return ns
		}
	}
	var labels []string
	for _, n := range r.tree() {
		if n.Label != "" && len(labels) < 40 {
			labels = append(labels, n.Label)
		}
	}
	r.t.Fatalf("timed out waiting for %s; the tree has %q ...", what, labels)
	return nil
}

func (r *rig) act(id, action string) {
	r.t.Helper()
	r.must("act", map[string]any{"id": id, "action": action}, nil)
}

// press waits for the element with the label, and presses it.
func (r *rig) press(label string) {
	r.t.Helper()
	ns := r.waitFor("element "+label, has(label))
	r.act(find(ns, label).ID, "press")
}

// pressIn presses the child labelled label of the element labelled parent.
func (r *rig) pressIn(parent, label string) {
	r.t.Helper()
	ns := r.waitFor("element "+parent, has(parent))
	p := find(ns, parent)
	for _, n := range ns {
		if n.Parent == p.ID && n.Label == label {
			r.act(n.ID, "press")
			return
		}
	}
	r.t.Fatalf("%q has no child %q", parent, label)
}

func (r *rig) input(events ...map[string]any) {
	r.t.Helper()
	r.must("input", map[string]any{"events": events}, nil)
}

func (r *rig) typeText(s string) { r.t.Helper(); r.input(map[string]any{"type": "text", "text": s}) }
func (r *rig) key(k string)      { r.t.Helper(); r.input(map[string]any{"type": "key", "key": k}) }

func (r *rig) menu(path ...string) error {
	_, err := r.call("menu", map[string]any{"path": path})
	return err
}

func (r *rig) mustMenu(path ...string) {
	r.t.Helper()
	if err := r.menu(path...); err != nil {
		r.t.Fatalf("menu %q: %v", path, err)
	}
	r.call("idle", map[string]any{"timeout": 3000})
}

// capture saves the window as .scratch-native/automation/<name>.png and returns the decoded image.
func (r *rig) capture(name string) image.Image {
	r.t.Helper()
	var res struct {
		PNG           []byte `json:"png"`
		Width, Height int
	}
	r.must("capture", nil, &res)
	out := filepath.Join(r.root, ".scratch-native", "automation")
	if err := os.MkdirAll(out, 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, name+".png"), res.PNG, 0o644); err != nil {
		r.t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(res.PNG))
	if err != nil || img.Bounds().Dx() < 400 {
		r.t.Fatalf("capture %s is not a window-sized PNG: %v", name, err)
	}
	return img
}

// quit chooses Quit from the app menu, as a person does, and waits for the process to end.
func (r *rig) quit() {
	r.t.Helper()
	name := filepath.Base(r.bin)
	r.menu(name, "Quit "+name) // the answer may be lost as the app exits
	select {
	case <-r.appDone:
	case <-time.After(15 * time.Second):
		r.t.Error("the app did not quit through its menu")
	}
}

// cleanup stops what this test started, by handle and by the pid file of its own data directory, and removes its
// directory. A hung app or daemon fails the test.
func (r *rig) cleanup() {
	if r.conn != nil {
		r.conn.Close()
	}
	if r.app != nil {
		select {
		case <-r.appDone:
		default:
			r.t.Error("the app was still running at the end of the test; killing it")
			r.app.Process.Kill()
			<-r.appDone
		}
	}
	b, _ := os.ReadFile(filepath.Join(r.data, "keel-serve.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid > 0 {
		s := exec.Command(r.bin, "serve", "stop")
		s.Env = r.env()
		s.Run()
		for end := time.Now().Add(10 * time.Second); alive(pid) && time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		}
		if alive(pid) {
			r.t.Errorf("the daemon (pid %d) did not stop; killing it", pid)
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	if r.serve != nil {
		select {
		case <-r.serveDone:
		case <-time.After(10 * time.Second):
			r.t.Error("the daemon's process did not end")
			r.serve.Process.Kill()
		}
	}
	if _, err := os.Lstat(r.sock); err == nil {
		r.t.Errorf("the socket file is left at the end of the test")
	}
	os.RemoveAll(r.dir)
}
