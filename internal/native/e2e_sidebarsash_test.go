//go:build nativee2e

package native

import (
	"testing"
	"time"
)

// The sidebar's sash in a real window: a drag widens the sidebar and the terminal pane gives way (keel serve sees the
// narrower terminal), the width is kept and a window opened later on the same data starts at it. Shots in both themes.
func TestE2ESidebarWidthIsKept(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	sid := e.openLocal()
	cols := func() int {
		for _, s := range e.sessions() {
			if s.ID == sid {
				return s.Cols
			}
		}
		return 0
	}
	e.settle(300 * time.Millisecond)
	before := cols()
	sash, ok := e.tt.Find("Resize sidebar")
	if !ok {
		t.Fatal("no sash")
	}
	x, y := sash.X+sash.W/2, sash.Y+sash.H/2
	e.tt.Press(x, y)
	e.tt.Move(x+60, y)
	e.tt.Move(x+120, y)
	e.step()
	e.tt.Release(x+120, y)
	e.pump(func() bool { return loadSidebarWidth(dir) > sidebarWidth()+100 }, "the width kept")
	e.pump(func() bool { return cols() < before }, "the terminal to give way: fewer columns on keel serve")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2ESidebarWidth-dark")
	e.w.setTheme(themeLight)
	e.pump(func() bool { return !e.w.dark }, "the light theme")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2ESidebarWidth-light")
	kept := loadSidebarWidth(dir)
	e.w.closeWindow()

	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool { return e2.w.sidebarNow() > kept-1 && e2.w.sidebarNow() < kept+1 }, "a new window to open the sidebar at the kept width")
}
