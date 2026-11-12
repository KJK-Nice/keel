//go:build nativee2e

package native

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// opacityBackdrop is the flat colour shotOn puts behind a translucent window: the Tester draws over nothing (premultiplied
// pixels with alpha), a PNG viewer would put its own colour behind them. A mid blue-grey shows both a stickered control
// (it stays the same colour) and a glass one (it takes the backdrop). Real blur behind is a person-only check.
var opacityBackdrop = ui.RGB(110, 138, 176)

// shotOn writes the Tester's picture composited over backdrop.
func (e *win) shotOn(name string, back ui.Color) {
	e.t.Helper()
	img := e.tt.Image()
	if img == nil {
		e.t.Fatal("Tester.Image gave no pixels")
	}
	out := image.NewRGBA(img.Bounds())
	for i := 0; i+3 < len(img.Pix); i += 4 {
		k := 255 - uint16(img.Pix[i+3]) // what the window leaves of the backdrop
		out.Pix[i] = uint8(min(255, uint16(img.Pix[i])+uint16(back.R)*k/255))
		out.Pix[i+1] = uint8(min(255, uint16(img.Pix[i+1])+uint16(back.G)*k/255))
		out.Pix[i+2] = uint8(min(255, uint16(img.Pix[i+2])+uint16(back.B)*k/255))
		out.Pix[i+3] = 255
	}
	f, err := os.Create(filepath.Join(repoRoot(e.t), ".scratch-native", "e2e", name+".png"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		e.t.Fatal(err)
	}
}

// Plan 028 step 2: the window at 70% opacity with the sidebar, a pane, the New tab page, Settings and the agent panel, in
// both themes, over opacityBackdrop.
func TestE2EControlsUnderWindowOpacity(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	e.w.translucentWin, e.w.win.Opacity = true, 70
	e.w.alpha = alphaFor(70)
	e.tt.Frame()
	e.openLocal()
	e.settle(300 * time.Millisecond)
	for _, th := range []string{themeDark, themeLight} {
		e.w.setTheme(th)
		e.pump(func() bool { return e.w.dark == (th == themeDark) }, th+" theme")
		if e.w.alpha != 0.7 {
			t.Fatalf("window alpha %v in the %s theme, want 0.7", e.w.alpha, th)
		}
		e.key(ui.Cmd, ui.Key1)
		e.settle(300 * time.Millisecond)
		e.shotOn("TestE2EOpacity-workspace-"+th, opacityBackdrop)
		e.openAgent()
		e.settle(300 * time.Millisecond)
		e.shotOn("TestE2EOpacity-agent-"+th, opacityBackdrop)
		e.key(agentMods(), ui.KeyI)
		e.pump(func() bool { return !e.w.agent.open }, "the agent panel to close")
		e.click("New tab")
		e.pump(func() bool { return e.tt.HasText("Where to?") }, "the New tab page")
		e.settle(300 * time.Millisecond)
		e.shotOn("TestE2EOpacity-newtab-"+th, opacityBackdrop)
		e.openSettings()
		e.settle(300 * time.Millisecond)
		e.shotOn("TestE2EOpacity-settings-"+th, opacityBackdrop)
	}
}
