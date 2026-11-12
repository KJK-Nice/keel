//go:build nativeautomation

package nativeautomation

// Plan 032 section 9: ghosting under a low Window opacity. A translucent window's shadow is cut from its content's alpha and
// shows through its grounds; Keel's window kept the shadow of the first page (the New tab page) under every later page, so
// "Where to?" and the cards stayed faintly visible under the terminal. The socket's capture does not show it (it is not the
// compositor's picture), so the scenario photographs the screen with screencapture(1) over a solid window of its own
// (backdrop.swift), where nothing else can show through, and checks that a part of the terminal pane with nothing in it is
// one flat colour.

import (
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// ghostCfg writes the scratch keel-native.json with a window opacity, a blur and a theme, before the app starts.
func (r *rig) ghostCfg(opacity, blur int, theme string) {
	r.t.Helper()
	body := `{"opacity":` + strconv.Itoa(opacity) + `,"blur":` + strconv.Itoa(blur) + `,"theme":"` + theme + `"}`
	if err := os.WriteFile(filepath.Join(r.data, "keel-native.json"), []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// backdrop puts a solid window of this test's own just below the app's window, so the screen captures show a known
// backdrop and never another window that happens to lie behind (a person's Keel, a terminal). It ends with the test.
func (r *rig) backdrop(margin ...int) {
	r.t.Helper()
	args := []string{strconv.Itoa(r.app.Process.Pid)}
	if len(margin) > 0 {
		args = append(args, strconv.Itoa(margin[0]))
	}
	bin := filepath.Join(r.dir, "backdrop")
	if o, err := exec.Command("swiftc", "-O", filepath.Join(r.root, "tests", "native-automation", "backdrop.swift"), "-o", bin).CombinedOutput(); err != nil {
		r.t.Skipf("swiftc: %v %s", err, o)
	}
	cmd := exec.Command(bin, args...)
	in, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { in.Close(); cmd.Wait() })
	time.Sleep(1500 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		r.t.Fatal("the backdrop window did not start")
	}
}

// shadowEdge photographs the window with margin points of the backdrop around it, and returns how much darker the backdrop is
// just below the window than a little further down: the window's shadow. The capture goes to .scratch-native/automation/.
func (r *rig) shadowEdge(name string, margin int) int {
	r.t.Helper()
	r.settle()
	time.Sleep(700 * time.Millisecond)
	var ws struct {
		Windows []struct {
			Bounds struct{ X, Y, Width, Height float64 }
		}
	}
	r.must("windows", nil, &ws)
	b := ws.Windows[0].Bounds
	out := filepath.Join(r.root, ".scratch-native", "automation", name+".png")
	m := float64(margin)
	rect := strconv.Itoa(int(b.X-m)) + "," + strconv.Itoa(int(b.Y-m)) + "," + strconv.Itoa(int(b.Width+2*m)) + "," + strconv.Itoa(int(b.Height+2*m))
	if o, err := exec.Command("screencapture", "-x", "-R", rect, out).CombinedOutput(); err != nil {
		r.t.Fatalf("screencapture: %v %s", err, o)
	}
	f, err := os.Open(out)
	if err != nil {
		r.t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		r.t.Fatal(err)
	}
	scale := float64(img.Bounds().Dx()) / (b.Width + 2*m)
	x := int((m + b.Width/2) * scale)
	below := int((m + b.Height) * scale) // the window's bottom edge
	lum := func(y int) int { r, g, bl, _ := img.At(x, y).RGBA(); return int(r>>8 + g>>8 + bl>>8) }
	return lum(below+int(40*scale)) - lum(below+int(6*scale))
}

// line types a command as text (there is no key for every character) and presses Enter.
func (r *rig) line(cmd string) { r.t.Helper(); r.typeText(cmd); r.key("Enter") }

func readPNG(t *testing.T, p string) image.Image {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// flatness is the largest difference, in one colour channel, between a pixel of rect and the pixel at its middle.
func flatness(img image.Image, rect image.Rectangle) int {
	mid := img.At((rect.Min.X+rect.Max.X)/2, (rect.Min.Y+rect.Max.Y)/2)
	mr, mg, mb, ma := mid.RGBA()
	worst := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			for _, d := range []int{int(r>>8) - int(mr>>8), int(g>>8) - int(mg>>8), int(b>>8) - int(mb>>8), int(a>>8) - int(ma>>8)} {
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst = d
				}
			}
		}
	}
	return worst
}

// TestWindowShadowSwitch (plan 032 section 10): the shadow is there by default, Settings · Appearance · Window shadow takes it
// away at once and gives it back, and with it off the pane at opacity 30 is flat (no ghost, because no shadow).
func TestWindowShadowSwitch(t *testing.T) {
	const margin = 60
	r := newRig(t)
	r.ghostCfg(30, 0, "dark")
	r.startServe()
	r.startApp()
	openShell(r)
	r.backdrop(margin)
	r.line("clear")
	on := r.shadowEdge("shadow-1-default", margin)
	t.Logf("default: the backdrop is %d lighter 40 pt below the window than 6 pt below it", on)
	if on < 15 {
		t.Fatalf("the window has no shadow by default (%d)", on)
	}
	r.press("Settings")
	r.waitFor("Settings", has("settings nav Appearance"))
	r.press("Window shadow switch")
	off := r.shadowEdge("shadow-2-off", margin)
	t.Logf("switched off: %d", off)
	if off > 5 {
		t.Errorf("the shadow is still there after turning it off (%d)", off)
	}
	r.press("tab Local Shell")
	r.line("clear")
	r.settle()
	r.screenShot("shadow-3-off-shell")
	img := readPNG(t, filepath.Join(r.root, ".scratch-native", "automation", "shadow-3-off-shell.png"))
	if d := flatness(img, image.Rect(700, 300, 2300, 1300)); d > ghostFlatMax {
		t.Errorf("with the shadow off the pane is not flat (%d)", d)
	}
	r.press("tab Settings")
	r.press("Window shadow switch")
	if back := r.shadowEdge("shadow-4-on-again", margin); back < 15 {
		t.Errorf("the shadow did not come back (%d)", back)
	}
	r.quit()
}

// TestTerminalCursorAndSelectionAreNotBrass (plan 032 section 11): in a real window the terminal's cursor and selection are the
// system accent's, not the old brass (#e8b960 dark, #82510d light). The shots (term-accent-dark.png, term-accent-light.png) are
// opened by eye; the scan fails on any pixel within 10 of the old cursor colour.
func TestTerminalCursorAndSelectionAreNotBrass(t *testing.T) {
	for theme, brass := range map[string][3]int{"dark": {0xe8, 0xb9, 0x60}, "light": {0x82, 0x51, 0x0d}} {
		t.Run(theme, func(t *testing.T) {
			r := newRig(t)
			r.ghostCfg(100, 0, theme)
			r.startServe()
			r.startApp()
			openShell(r)
			r.backdrop()
			r.line("clear")
			r.line("echo select this text")
			r.settle()
			p := find(r.tree(), "pane 1")
			y := p.Bounds.Y + 58
			r.drag(p.Bounds.X+8, y, p.Bounds.X+8+120, y)
			r.settle()
			r.screenShot("term-accent-" + theme)
			img := readPNG(t, filepath.Join(r.root, ".scratch-native", "automation", "term-accent-"+theme+".png"))
			near := 0
			b := img.Bounds()
			for y := b.Min.Y + 140; y < b.Min.Y+400; y++ {
				for x := b.Min.X + 500; x < b.Min.X+1400; x++ {
					cr, cg, cb, _ := img.At(x, y).RGBA()
					if abs(int(cr>>8)-brass[0]) < 10 && abs(int(cg>>8)-brass[1]) < 10 && abs(int(cb>>8)-brass[2]) < 10 {
						near++
					}
				}
			}
			if near > 0 {
				t.Errorf("%d pixels of the old brass cursor colour in the terminal (term-accent-%s.png)", near, theme)
			}
			r.quit()
		})
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ghostFlatMax is what an empty part of the pane may differ by: its own shadow's soft edge. A ghost of the New tab page is
// 20 to 60 in this measure (see the captures ghost-*).
const ghostFlatMax = 8

// TestNoGhostUnderLowOpacity: at Window opacity 30 (the lowest), with the blur off and with the default, in both themes, the
// pane of a terminal that has just been opened, and again after a visit to the New tab page, shows nothing of an earlier
// page. Failed before the fix: the pane's middle showed the New tab page's heading, input and cards.
func TestNoGhostUnderLowOpacity(t *testing.T) {
	cases := []struct {
		opacity, blur int
		theme         string
	}{{30, 0, "dark"}, {30, 0, "light"}, {70, 20, "dark"}}
	if os.Getenv("GHOST_MATRIX") == "full" {
		cases = nil
		for _, op := range []int{30, 50, 70, 100} {
			for _, th := range []string{"dark", "light"} {
				for _, bl := range []int{0, 20} {
					cases = append(cases, struct {
						opacity, blur int
						theme         string
					}{op, bl, th})
				}
			}
		}
	}
	for _, c := range cases {
		name := "ghost-" + strconv.Itoa(c.opacity) + "-" + strconv.Itoa(c.blur) + "-" + c.theme
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.ghostCfg(c.opacity, c.blur, c.theme)
			r.startServe()
			r.startApp()
			openShell(r)
			r.backdrop()
			r.line("clear")
			dir := filepath.Join(r.root, ".scratch-native", "automation")
			check := func(step string) {
				t.Helper()
				r.settle()
				time.Sleep(700 * time.Millisecond) // the shadow is cut again 150 ms after the last frame
				r.screenShot(name + "-" + step)
				f, err := os.Open(filepath.Join(dir, name+"-"+step+".png"))
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(f)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				// The pane's middle, in pixels of the 2x capture: below the prompt, clear of the pane's edges.
				d := flatness(img, image.Rect(700, 300, 2300, 1300))
				t.Logf("%s: the empty pane differs by %d at most", step, d)
				if d > ghostFlatMax {
					t.Errorf("%s: the empty pane is not flat (differs by %d, at most %d): an earlier page shows through (%s.png)", step, d, ghostFlatMax, name+"-"+step)
				}
			}
			check("1-shell")
			r.press("New tab")
			r.waitFor("the New tab page", has("tab New Tab"))
			r.settle()
			r.press("tab Local Shell")
			check("2-after-new-tab")
			r.quit()
		})
	}
}
