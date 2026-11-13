//go:build nativee2e

package native

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Issue #139 (plan 037 part A): a session that ends while its pane is open leaves no terminal picture and no blinking caret
// behind the overlay, in a translucent window too (where a ground that is not opaque lets the picture through). The shots are
// opened by eye.
func TestE2ELostWhileOpenShowsNoTerminalBehindTheOverlay(t *testing.T) {
	for _, th := range []string{themeDark, themeLight} {
		t.Run(th, func(t *testing.T) {
			addr, dir := startServe(t, true)
			e := newWin(t, addr, dir, nil)
			e.w.setTheme(th)
			e.w.alpha = 0.5 // translucent: the overlay's ground lets what lies under it show
			e.step()
			e.openLocal()
			e.clickBody("1")
			e.typeLine("echo still-here-after-exit")
			e.settleClock(300 * time.Millisecond)
			e.typeLine("exit")
			e.pump(func() bool { return e.tt.HasText("Session ended") }, "the Session ended banner")
			e.settleClock(1200 * time.Millisecond) // a caret would blink on or off in this time
			e.shot("TestE2ELostWhileOpen-" + th)
			t.Logf("labels with Terminal: %v", e.hasLabel("Terminal"))
			if e.hasLabel("Terminal") {
				t.Error("the terminal view is still in the tree behind the overlay")
			}
			if !e.hasLabel("Reconnect") || !e.hasLabel("Close") {
				t.Error("no Reconnect or Close on the lost pane")
			}
			// Typed keys go nowhere: not to the dead session, not to another pane.
			e.key(0, ui.KeyX)
			e.key(0, ui.KeyEnter)
			// Reconnect: the terminal is back and takes the keyboard again.
			e.click("Reconnect")
			e.pump(func() bool { return e.hasLabel("Terminal") && e.snap().Status == "ready" }, "the terminal back after Reconnect")
			e.pump(func() bool { return e.tt.Focused("Terminal") }, "the keyboard back on the terminal")
		})
	}
}
