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
func (r *rig) backdrop() {
	r.t.Helper()
	bin := filepath.Join(r.dir, "backdrop")
	if o, err := exec.Command("swiftc", "-O", filepath.Join(r.root, "tests", "native-automation", "backdrop.swift"), "-o", bin).CombinedOutput(); err != nil {
		r.t.Skipf("swiftc: %v %s", err, o)
	}
	cmd := exec.Command(bin, strconv.Itoa(r.app.Process.Pid))
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

// line types a command as text (there is no key for every character) and presses Enter.
func (r *rig) line(cmd string) { r.t.Helper(); r.typeText(cmd); r.key("Enter") }

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
