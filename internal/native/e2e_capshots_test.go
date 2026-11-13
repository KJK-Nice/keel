//go:build nativee2e

package native

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Plan 028 step 5: the flat key caps where they show beside text: the palette's footer and rows, the New tab page, and the
// Keyboard settings page, in both themes. (The ⌘-held caps are TestE2EKeyCaps-dark and -light.)
func TestE2EFlatKeyCapShots(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	for _, th := range []string{themeDark, themeLight} {
		e.w.setTheme(th)
		e.pump(func() bool { return e.w.dark == (th == themeDark) }, th+" theme")
		e.key(ui.Cmd, ui.KeyK)
		e.pump(func() bool { return e.w.pal != nil && e.tt.Focused("Command palette") }, "the palette")
		e.settle(300 * time.Millisecond)
		e.shot("TestE2EFlatCaps-palette-" + th)
		e.key(0, ui.KeyEscape)
		e.pump(func() bool { return e.w.pal == nil }, "the palette to close")
		e.click("New tab")
		e.pump(func() bool { return e.tt.HasText("Where to?") }, "the New tab page")
		e.settle(300 * time.Millisecond)
		e.shot("TestE2EFlatCaps-newtab-" + th)
		e.key(ui.Cmd, ui.KeyComma)
		e.pump(func() bool { return e.hasLabel("settings nav Keyboard") }, "Settings")
		e.click("settings nav Keyboard")
		e.settle(300 * time.Millisecond)
		e.shot("TestE2EFlatCaps-keyboard-" + th)
	}
}
