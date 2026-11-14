//go:build nativeautomation

package nativeautomation

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// openShell presses the Local Shell host and waits for its terminal to have the keyboard.
func openShell(r *rig) {
	r.t.Helper()
	r.press("host Local Shell")
	r.waitFor("the terminal", has("Terminal"))
}

// 1. A fresh data directory shows First run.
func TestStartsAndShowsFirstRun(t *testing.T) {
	r := newRig(t)
	r.startApp() // no vault: the app starts its own daemon on the scratch data
	r.waitFor("the First run heading", has("Welcome to Keel"))
	r.capture("first-run")
	r.quit()
}

// 2. Every File and View item of shell.go is at its path, and the ones that change the window do.
func TestMenuBar(t *testing.T) {
	// The menu bar is made from the command table (commands.go): each item is a menuSpec{section, title, position}.
	table, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "native", "commands.go"))
	if err != nil {
		t.Fatal(err)
	}
	var file, view []string
	for _, m := range regexp.MustCompile(`menuSpec\{"(File|View)", "([^"]+)"`).FindAllStringSubmatch(string(table), -1) {
		if m[1] == "File" {
			file = append(file, m[2])
		} else {
			view = append(view, m[2])
		}
	}
	if len(file) < 9 || len(view) < 7 {
		t.Fatalf("read %d File and %d View items from commands.go", len(file), len(view))
	}
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "native", "shell.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	r := newRig(t)
	r.startServe()
	r.startApp()
	chosen := map[string]bool{}
	choose := func(menu, item string) {
		t.Helper()
		r.mustMenu(menu, item)
		chosen[menu+"/"+item] = true
	}
	// A dialog an item opens is closed with Escape, as a person does.
	dismiss := func() { r.key("Escape"); r.call("idle", map[string]any{"timeout": 3000}) }

	openShell(r)
	choose("File", "Split Right")
	r.waitFor("a second pane", has("pane 2"))
	choose("File", "Split Down")
	r.waitFor("a third pane", has("pane 3"))

	choose("View", "Toggle Vault Sidebar")
	r.waitFor("the sidebar hidden", lacks("Filter hosts"))
	choose("View", "Toggle Vault Sidebar")
	r.waitFor("the sidebar back", has("Filter hosts"))

	choose("View", "Toggle Agent Panel")
	r.waitFor("the agent panel", has("agent panel"))
	choose("View", "Toggle Agent Panel")
	r.waitFor("the agent panel closed", lacks("agent panel"))

	size := func() string {
		for _, n := range r.tree() {
			if strings.HasPrefix(n.Label, "Local Shell · ") {
				return n.Label
			}
		}
		return ""
	}
	before := size()
	choose("View", "Zoom In")
	r.waitFor("a larger text size", func(ns []node) bool { return before == "" || size() != before })
	choose("View", "Actual Size")
	choose("View", "Zoom Out")
	choose("View", "Actual Size")
	choose("View", "Focus View")
	choose("View", "Focus View")
	choose("View", "Find in Terminal")
	dismiss()

	choose("File", "New Tab")
	r.waitFor("a New Tab", has("tab New Tab"))
	choose("File", "Close Tab")
	r.waitFor("the New Tab closed", lacks("tab New Tab"))
	choose("File", "Reopen Tab")
	for _, item := range []string{"New Host", "Quick Connect", "Join Session", "Split Notebook", "Stop Keel runtime"} {
		choose("File", item)
		dismiss()
	}

	for _, l := range file {
		if l != "Close Window" && !chosen["File/"+l] {
			choose("File", l)
			dismiss()
		}
	}
	for _, l := range view {
		if !chosen["View/"+l] {
			choose("View", l)
			dismiss()
		}
	}
	if !strings.Contains(s, `Label: "Close Window"`) {
		t.Error("shell.go has no Close Window item")
	}
	if err := r.menu("File", "No Such Item"); err == nil {
		t.Error("a menu path that does not exist was accepted")
	}
	r.capture("menu-bar-after")
	r.quit()
}

// 3. Close Window hides the window and keeps the app. Bringing it back is the menu bar item's Show (macOS tray) or
// the Dock, and the automation socket's menu cannot reach either, so the scenario ends at "hidden and alive".
func TestCloseWindowHidesAndKeepsRunning(t *testing.T) {
	r := newRig(t)
	r.startServe()
	r.startApp()
	openShell(r)
	r.mustMenu("File", "Close Window")
	r.waitWindows(func(ws []window) bool { return len(ws) == 1 && !ws[0].Visible }, "the window to hide")
	select {
	case <-r.appDone:
		t.Fatal("Close Window quit the app")
	case <-time.After(time.Second):
	}
	if len(r.tree()) == 0 {
		t.Error("the hidden window has no tree")
	}
	t.Log("hidden and alive; Show is the tray's and not reachable through the socket")
	r.quit()
}

// 4. Real keys into a real terminal. The terminal's text is not in the tree (the "Terminal" element has no value),
// so the proof is keel serve's scrollback, read with the scratch token (never printed).
func TestRealKeysIntoARealTerminal(t *testing.T) {
	r := newRig(t)
	r.startServe()
	r.startApp()
	openShell(r)
	r.typeText("echo auto-$((6*7))")
	r.key("Enter")

	tok, err := os.ReadFile(filepath.Join(r.data, "keel-serve.token"))
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string, out any) {
		t.Helper()
		req, _ := http.NewRequest("GET", "http://"+r.addr+path, nil)
		req.Header.Set("X-Keel-Token", strings.TrimSpace(string(tok)))
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || json.Unmarshal(b, out) != nil {
			t.Fatalf("GET %s: %d", path, resp.StatusCode)
		}
	}
	var sessions []struct{ ID string }
	get("/api/sessions", &sessions)
	if len(sessions) != 1 {
		t.Fatalf("%d sessions, want 1", len(sessions))
	}
	ok := false
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end) && !ok; time.Sleep(200 * time.Millisecond) {
		var sb struct{ Text string }
		get("/api/sessions/"+sessions[0].ID+"/scrollback", &sb)
		ok = strings.Contains(sb.Text, "auto-42")
	}
	if !ok {
		t.Fatal("the shell never printed auto-42")
	}
	for _, n := range r.tree() {
		if strings.Contains(n.Label+n.Value, "auto-42") {
			t.Log("the tree does expose the terminal's text")
		}
	}
	r.quit()
}

// 6. Both themes, from Settings, captured from the workspace.
func TestBothThemes(t *testing.T) {
	r := newRig(t)
	r.startServe()
	r.startApp()
	openShell(r)
	r.press("Settings")
	r.waitFor("Settings", has("settings nav Appearance"))
	shot := func(theme, name string) image.Image {
		r.pressIn("Color theme", theme)
		r.press("tab Local Shell")
		r.waitFor("the shell tab", has("pane 1"))
		img := r.capture(name)
		r.press("tab Settings")
		r.waitFor("Settings", has("settings nav Appearance"))
		return img
	}
	dark := shot("Dark", "theme-dark")
	light := shot("Light", "theme-light")
	if d, l := luma(dark), luma(light); d+40 > l {
		t.Errorf("mean luminance dark %.0f, light %.0f: the themes do not differ", d, l)
	}
	r.quit()
}

// luma is the mean brightness of a picture, 0 to 255, ignoring alpha.
func luma(img image.Image) float64 {
	b := img.Bounds()
	var sum, n float64
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257
			n++
		}
	}
	return sum / n
}

// 7. The guard at run time: the tagged binary with the socket on and the default data directory (named or not) exits
// non-zero with the error and opens no window. A free loopback port keeps it off the user's daemon, and a 5 s limit
// ends it if the guard ever failed. The default directory is only stat'ed, never listed.
func TestGuardRefusesTheDefaultDataDir(t *testing.T) {
	r := newRig(t)
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	realDefault := filepath.Join(cfg, "keel")
	// The child's HOME is a scratch directory, so its "default" data directory is a scratch one too: were the
	// guard ever broken, the app would still not meet real data. The real default is only stat'ed, as asserted below.
	home := filepath.Join(r.dir, "home")
	def := filepath.Join(home, "Library", "Application Support", "keel")
	if err := os.MkdirAll(def, 0o700); err != nil {
		t.Fatal(err)
	}
	mtime := func() string {
		st, err := os.Stat(realDefault)
		if err != nil {
			return "absent"
		}
		return st.ModTime().String()
	}
	before := mtime()
	for name, data := range map[string]string{"unset": "", "default": def} {
		env := []string{}
		for _, kv := range r.env("MYGO_AUTOMATION="+r.sock, "HOME="+home) {
			if !strings.HasPrefix(kv, "KEEL_DATA=") {
				env = append(env, kv)
			}
		}
		if data != "" {
			env = append(env, "KEEL_DATA="+data)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, r.bin, "app")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		cancel()
		ee, isExit := err.(*exec.ExitError)
		if !isExit || ee.ExitCode() == 0 {
			t.Errorf("%s: the app did not exit with an error: %v", name, err)
		}
		if !strings.Contains(string(out), "automation refused") {
			t.Errorf("%s: no refusal in its output: %q", name, out)
		}
		if _, err := os.Lstat(r.sock); err == nil {
			t.Errorf("%s: the refused app left its socket", name)
			os.Remove(r.sock)
		}
	}
	if after := mtime(); after != before {
		t.Errorf("the default data directory changed: %s -> %s", before, after)
	}
}
