//go:build nativee2e

package native

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// In real shells: two panes in each theme and three in a mixed split, the ring on the focused one.
func TestE2EPaneRing(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2)
	e.settle(500 * time.Millisecond)
	e.shot("TestE2EPaneRing-dark")
	e.w.setTheme(themeLight)
	e.pump(func() bool { return !e.w.dark }, "the light theme")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EPaneRing-light")
	e.key(ui.Cmd|ui.Shift, ui.KeyE)
	e.pump(func() bool { return len(e.sessions()) == 3 && e.w.paneCount() == 3 }, "3 sessions")
	e.settle(500 * time.Millisecond)
	e.shot("TestE2EPaneRing-mixed-light")
	e.w.setTheme(themeDark)
	e.pump(func() bool { return e.w.dark }, "the dark theme")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EPaneRing-mixed-dark")
}
