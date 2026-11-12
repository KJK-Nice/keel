//go:build nativeautomation

package nativeautomation

// Plan 032 in a real window: what the headless Tester cannot show. Typing is made as a keyboard makes it (a key press, the
// text it produces, the release), a drag has a frame between its events, and the text on screen is judged by keel serve's
// scrollback and by captures (.scratch-native/automation/, with the pane corners cut out at 5x as <name>-corners.png).

import (
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// scrollback is the text keel serve holds for the first session.
func (r *rig) scrollback() string {
	r.t.Helper()
	tok, err := os.ReadFile(filepath.Join(r.data, "keel-serve.token"))
	if err != nil {
		r.t.Fatal(err)
	}
	get := func(path string, out any) {
		req, _ := http.NewRequest("GET", "http://"+r.addr+path, nil)
		req.Header.Set("X-Keel-Token", strings.TrimSpace(string(tok)))
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			r.t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || json.Unmarshal(b, out) != nil {
			r.t.Fatalf("GET %s: %d", path, resp.StatusCode)
		}
	}
	var sessions []struct{ ID string }
	get("/api/sessions", &sessions)
	if len(sessions) == 0 {
		return ""
	}
	var sb struct{ Text string }
	get("/api/sessions/"+sessions[0].ID+"/scrollback", &sb)
	return sb.Text
}

// put sends a PUT with the scratch token (never printed).
func (r *rig) put(path string, body map[string]string) int {
	r.t.Helper()
	tok, _ := os.ReadFile(filepath.Join(r.data, "keel-serve.token"))
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "http://"+r.addr+path, strings.NewReader(string(b)))
	req.Header.Set("X-Keel-Token", strings.TrimSpace(string(tok)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (r *rig) settle() { r.call("idle", map[string]any{"timeout": 3000}) }

// drag drags with the left button, a frame between the events as a hand makes.
func (r *rig) drag(x0, y0, x1, y1 float64) {
	step := func(ev map[string]any) { r.input(ev); r.settle() }
	step(map[string]any{"type": "pointerMove", "x": x0, "y": y0})
	step(map[string]any{"type": "pointerDown", "x": x0, "y": y0})
	step(map[string]any{"type": "pointerMove", "x": (x0 + x1) / 2, "y": y0})
	step(map[string]any{"type": "pointerMove", "x": x1, "y": y1})
	step(map[string]any{"type": "pointerUp", "x": x1, "y": y1})
}

// typeKeys types s as a keyboard does.
func (r *rig) typeKeys(s string) {
	r.t.Helper()
	for _, c := range s {
		k := string(c)
		if c == ' ' {
			k = "Space"
		}
		r.input(map[string]any{"type": "keyDown", "key": k}, map[string]any{"type": "text", "text": string(c)}, map[string]any{"type": "keyUp", "key": k})
	}
}

// sh types a command and presses Enter.
func (r *rig) sh(cmd string) { r.t.Helper(); r.typeKeys(cmd); r.key("Enter") }

// typedReaches types "echo <marker>" with Enter and reports whether the shell printed the marker.
func (r *rig) typedReaches(marker string) bool {
	r.t.Helper()
	r.sh("echo " + marker)
	for end := time.Now().Add(4 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if strings.Contains(strings.ReplaceAll(r.scrollback(), "echo "+marker, ""), marker) {
			return true
		}
	}
	return false
}

// focusLabel is the label of the element that has the keyboard.
func (r *rig) focusLabel() string {
	var tr struct {
		Focus string
		Nodes []node
	}
	r.must("tree", nil, &tr)
	for _, n := range tr.Nodes {
		if n.ID == tr.Focus {
			return n.Role + " " + n.Label
		}
	}
	return ""
}

// shot captures the window after it has settled, and the four corners of every pane at 5x beside it.
func (r *rig) shot(name string) {
	r.t.Helper()
	r.settle()
	img := r.capture(name)
	const sz, zoom = 48, 5
	var panes []node
	for _, n := range r.tree() {
		if strings.HasPrefix(n.Label, "pane ") && !strings.HasPrefix(n.Label, "pane bar") {
			panes = append(panes, n)
		}
	}
	if len(panes) == 0 {
		return
	}
	out := image.NewRGBA(image.Rect(0, 0, 4*sz*zoom, len(panes)*sz*zoom))
	for row, p := range panes {
		x0, y0 := int(p.Bounds.X*2), int(p.Bounds.Y*2)
		x1, y1 := int((p.Bounds.X+p.Bounds.Width)*2), int((p.Bounds.Y+p.Bounds.Height)*2)
		for i, c := range [][2]int{{x0 - 8, y0 - 8}, {x1 - sz + 8, y0 - 8}, {x0 - 8, y1 - sz + 8}, {x1 - sz + 8, y1 - sz + 8}} {
			for y := 0; y < sz*zoom; y++ {
				for x := 0; x < sz*zoom; x++ {
					out.Set(i*sz*zoom+x, row*sz*zoom+y, img.At(c[0]+x/zoom, c[1]+y/zoom))
				}
			}
		}
	}
	f, err := os.Create(filepath.Join(r.root, ".scratch-native", "automation", name+"-corners.png"))
	if err != nil {
		r.t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, out)
}

// Plan 032 step 4 (item 3). A drag on the sidebar's sash gave the sash the keyboard and kept it: the next typing (text goes to
// the focused element) reached nothing until the terminal was clicked. Found here: before the fix the keyboard stayed on
// "Resize sidebar" and no echo came back.
func TestTypingWorksAfterDraggingTheSash(t *testing.T) {
	r := newRig(t)
	r.startServe()
	r.startApp()
	r.press("host Local Shell")
	r.waitFor("the terminal", has("Terminal"))
	if !r.typedReaches("kone") {
		t.Fatalf("typing right after opening reached nothing; the keyboard is on %q", r.focusLabel())
	}
	s := find(r.tree(), "Resize sidebar")
	x, y := s.Bounds.X+s.Bounds.Width/2, s.Bounds.Y+s.Bounds.Height/2
	r.drag(x, y, x+60, y)
	if !r.typedReaches("ktwo") {
		t.Fatalf("typing after dragging the sash reached nothing; the keyboard is on %q", r.focusLabel())
	}
	r.press("Split right")
	r.waitFor("pane 2", has("pane 2"))
	r.settle()
	if got := r.focusLabel(); got != "group Terminal" {
		t.Errorf("after a split the keyboard is on %q", got)
	}
	r.quit()
}

// Plan 032 step 3 (items 2 and 8): lots of output, then the layout changes, with a capture after each. Looked at by eye
// (the pane corners for a straight line out of the rounded border, the text against keel serve's scrollback); the test
// asserts only that the shell kept up. Not reproduced in the run of 2026-11-12 (see the plan 032 follow-up report).
func TestTerminalCapturesAfterLayoutChanges(t *testing.T) {
	r := newRig(t)
	r.startServe()
	r.startApp()
	openShell(r)
	r.sh("seq 1 300")
	r.typeText("ls -la /usr/bin | head -150") // text, not keys: there is no key for the pipe
	r.key("Enter")
	r.sh("clear")
	r.sh("seq 1 120")
	if !r.typedReaches("kmark") {
		t.Fatal("the shell did not keep up")
	}
	r.shot("terminal-01-output")
	s := find(r.tree(), "Resize sidebar")
	x, y := s.Bounds.X+s.Bounds.Width/2, s.Bounds.Y+s.Bounds.Height/2
	r.drag(x, y, x+150, y)
	r.shot("terminal-02-sash-wide")
	r.drag(x+150, y, x, y)
	r.shot("terminal-03-sash-back")
	r.press("Split right")
	r.waitFor("pane 2", has("pane 2"))
	r.sh("seq 1 100")
	r.shot("terminal-04-split")
	r.press("Close pane")
	r.shot("terminal-05-closed")
	r.press("New tab")
	r.shot("terminal-06-new-tab")
	r.press("tab Local Shell")
	r.press("Agent")
	r.shot("terminal-07-agent")
	r.press("Agent")
	r.mustMenu("View", "Zoom In")
	r.shot("terminal-08-zoom")
	r.mustMenu("View", "Actual Size")
	r.quit()
}

// Plan 032 step 5 (item 4), in a real window and with the fake provider: the keys and settings are stored the way the
// agent panel does, a plain-language line is typed as keys, and the notice, the card and esc are followed by label.
// Findings: with the default settings (no provider key, suggestions off) the pane shows nothing at all and ⇥ stays the
// shell's, which is the app's behaviour too; the daemon's log shows no suggestion route because the questions are
// WebSocket frames (agent-detect, agent-suggest), not HTTP.
func TestSuggestionInARealWindow(t *testing.T) {
	r := newRig(t)
	r.startServe()
	for _, p := range []struct {
		path string
		body map[string]string
	}{
		{"/api/agent/keys/gemini", map[string]string{"key": "fake-key-for-tests-only-0000"}},
		{"/api/agent/keys/jev", map[string]string{"key": "fake-jev-key-for-tests-only-0000"}},
		{"/api/agent/settings", map[string]string{"suggestions": "on", "detection": "auto"}},
	} {
		if code := r.put(p.path, p.body); code != 200 {
			t.Fatalf("PUT %s: %d", p.path, code)
		}
	}
	r.startApp()
	openShell(r)
	r.waitFor("the prompt", func([]node) bool { return strings.HasSuffix(strings.TrimSpace(r.scrollback()), "$") })

	r.typeKeys("show the files here")
	r.waitFor("the notice", has("suggestion notice"))
	r.shot("suggest-01-notice")
	r.key("Tab")
	r.waitFor("the card", has("suggested command"))
	r.shot("suggest-02-card")
	r.key("Escape")
	r.waitFor("the card gone at once", lacks("suggested command"))
	if find(r.tree(), "suggestion notice") != nil {
		t.Error("the notice came back after esc on the card")
	}
	r.shot("suggest-03-after-esc")

	r.typeKeys(" now") // new words: noticed again; esc takes the notice away too
	r.waitFor("the notice again", has("suggestion notice"))
	r.key("Escape")
	r.waitFor("the notice gone", lacks("suggestion notice"))

	// The notice's esc is also the shell's (the page's twin does the same), and a shell's line editor takes the next key
	// within its timeout (bash: 500 ms) as Meta+key: typed at once, the "x" below is lost ("\a" in the scrollback). That is
	// a candidate for "the first keys do not register"; the scenario waits it out and does not decide it.
	time.Sleep(900 * time.Millisecond)
	r.typeKeys(" please") // typing is never held back; a new pause, a new notice
	r.waitFor("a notice for the longer line", has("suggestion notice"))
	r.key("Tab")
	r.waitFor("a card", has("suggested command"))
	r.key("Escape")
	r.waitFor("the card gone", lacks("suggested command"))
	r.shot("suggest-04-second-esc")
	r.quit()
}

// screenShot captures the window's rectangle of the screen with screencapture(1), which, unlike the socket's capture, shows a
// native menu that is open. It needs the screen-recording permission; without it the file is the wallpaper only.
func (r *rig) screenShot(name string) {
	r.t.Helper()
	var ws struct {
		Windows []struct {
			Bounds struct{ X, Y, Width, Height float64 }
		}
	}
	r.must("windows", nil, &ws)
	b := ws.Windows[0].Bounds
	out := filepath.Join(r.root, ".scratch-native", "automation", name+".png")
	rect := strconv.Itoa(int(b.X)) + "," + strconv.Itoa(int(b.Y)) + "," + strconv.Itoa(int(b.Width)) + "," + strconv.Itoa(int(b.Height))
	if o, err := exec.Command("screencapture", "-x", "-R", rect, out).CombinedOutput(); err != nil {
		r.t.Logf("screencapture: %v %s", err, o)
	}
}

// Plan 032 step 7 / plan 031 looks, in both themes: both sashes hovered, and a row's context menu (a native menu, so it is
// taken from the screen). Looked at by eye; the sash pixels are the headless tests' (sashpixels_test.go).
func TestContextMenuAndSashesBothThemes(t *testing.T) {
	r := newRig(t)
	r.startServe()
	r.startApp()
	openShell(r)
	r.press("Settings")
	r.waitFor("Settings", has("settings nav Appearance"))
	for _, theme := range []string{"Dark", "Light"} {
		r.pressIn("Color theme", theme)
		r.press("tab Local Shell")
		r.waitFor("the shell tab", has("pane 1"))
		tag := strings.ToLower(theme)
		hover := func(label, name string) {
			s := find(r.tree(), label)
			r.input(map[string]any{"type": "pointerMove", "x": s.Bounds.X + s.Bounds.Width/2, "y": s.Bounds.Y + s.Bounds.Height/2})
			r.shot(name + "-" + tag)
		}
		hover("Resize sidebar", "sash-sidebar-hover")
		r.press("Agent")
		r.waitFor("the agent panel", has("agent panel"))
		hover("Resize agent panel", "sash-agent-hover")
		r.press("Agent")
		r.waitFor("the agent panel closed", lacks("agent panel"))

		h := find(r.tree(), "host Local Shell")
		r.input(map[string]any{"type": "click", "button": "right", "x": h.Bounds.X + 60, "y": h.Bounds.Y + h.Bounds.Height/2})
		time.Sleep(700 * time.Millisecond)
		r.screenShot("context-menu-" + tag)
		r.key("Escape")
		r.settle()

		r.press("tab Settings")
		r.waitFor("Settings", has("settings nav Appearance"))
	}
	r.press("tab Local Shell")
	r.quit()
}
