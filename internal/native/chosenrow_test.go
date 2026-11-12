package native

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The chosen row of the sidebar (the host of the focused pane) has ui.Sidebar's source-list look: the accent, with the
// accent's text colour on it, while the sidebar's list has the keyboard in an active window; the neutral pressed grey when
// the list does not have it, and when the window is not the user's.
func TestTheChosenSidebarRowIsAccentWhileTheListHasTheKeyboardAndGreyOtherwise(t *testing.T) {
	for _, th := range []string{themeDark, themeLight} {
		r := splitRig(t)
		r.w.setTheme(th)
		r.tt.Frame()
		r.tt.Frame()
		row, ok := r.tt.Find("host api-1")
		if !ok {
			t.Fatal("no host row")
		}
		px := func() [3]uint8 { return r.pixel(row.X+row.W-4, row.Y+row.H/2) }
		base := ui.LightTheme()
		if th == themeDark {
			base = ui.DarkTheme()
		}
		grey := controlFill(base.SurfacePressed).Over(chrome)
		if got := px(); !is(got, grey) {
			t.Errorf("%s: the chosen row without the keyboard is %v, want the grey %v", th, got, grey)
		}
		// Give the keyboard to a row of the list.
		for i := 0; i < 40 && !r.tt.Focused("host api-1"); i++ {
			r.tt.Key(0, ui.KeyTab)
			r.tt.Frame()
		}
		if !r.tt.Focused("host api-1") {
			t.Fatalf("%s: Tab never reached the host row", th)
		}
		r.tt.Frame()
		if got := px(); !is(got, accentFill) {
			t.Errorf("%s: the chosen row with the keyboard is %v, want the accent %v", th, got, accentFill)
		}
		if c := contrast(onAccent, accentFill); c < 4.5 {
			t.Errorf("%s: the text on the accent has contrast %.2f", th, c)
		}
		r.tt.SetWindowActive(false)
		r.tt.Frame()
		if got := px(); !is(got, grey) {
			t.Errorf("%s: the chosen row in an inactive window is %v, want the grey %v", th, got, grey)
		}
		r.tt.SetWindowActive(true)
	}
}
