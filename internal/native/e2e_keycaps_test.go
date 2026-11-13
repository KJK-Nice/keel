//go:build nativee2e

package native

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// holdCaps holds ⌘ until the caps have faded in, and says which are drawn.
func (e *win) holdCaps() []string {
	e.t.Helper()
	e.tt.HoldModifiers(ui.Cmd)
	e.step()
	e.tt.Advance(capDelay + capFade + 40*time.Millisecond)
	e.step()
	var caps []string
	for _, s := range e.tt.Texts() {
		if len(s) > 8 && s[:8] == "key cap " {
			caps = append(caps, s[8:])
		}
	}
	return caps
}

// A split tab with the sidebar open while ⌘ is held, in both themes: the shots are for people to look at.
func TestE2EKeyCaps(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2)
	e.settle(500 * time.Millisecond)
	if !e.w.sidebar {
		e.key(ui.Cmd, ui.KeyB)
	}
	caps := e.holdCaps()
	if len(caps) < 8 {
		t.Fatalf("caps %v since %v off %v alpha %v", caps, e.w.capsSince, e.w.capsOff, e.w.capAlpha)
	}
	e.shot("TestE2EKeyCaps-dark")
	e.tt.HoldModifiers(0)
	e.step()
	if e.tt.HasText("key cap") {
		t.Error("caps stay after ⌘ is let go")
	}
	e.openSettings()
	e.click("Light")
	e.pump(func() bool { return !e.w.dark }, "the light theme")
	e.key(ui.Cmd, ui.Key1)
	e.settle(500 * time.Millisecond)
	if len(e.holdCaps()) < 8 {
		t.Fatal("no caps in the light theme")
	}
	e.shot("TestE2EKeyCaps-light")
	e.tt.HoldModifiers(0)
	e.w.closeWindow()
}
