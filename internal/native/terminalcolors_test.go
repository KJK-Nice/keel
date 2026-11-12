package native

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Plan 032 section 11: the terminal's cursor and selection are the system accent's, not brass.

var macAccents = map[string]ui.Color{
	"blue": ui.RGB(0, 122, 255), "purple": ui.RGB(149, 61, 150), "pink": ui.RGB(247, 79, 158), "red": ui.RGB(224, 56, 62),
	"orange": ui.RGB(247, 130, 27), "yellow": ui.RGB(255, 198, 0), "green": ui.RGB(98, 186, 70), "graphite": ui.RGB(140, 140, 140),
}

func TestTerminalCursorAndSelectionFollowTheAccent(t *testing.T) {
	for name, col := range macAccents {
		for _, dark := range []bool{true, false} {
			p := paletteOf(dark)
			fg, bg := tok(p.termFg), tok(p.termBg)
			a := deriveAccent(dark, col)
			th := terminalColors(a, fg, bg, ansi)
			fell := contrast(a.base, bg) < 3
			switch {
			case fell && th.Cursor != fg:
				t.Errorf("%s dark=%v: %.2f:1 on the terminal, the cursor should fall back to the foreground, got %v", name, dark, contrast(a.base, bg), th.Cursor)
			case !fell && th.Cursor != a.base:
				t.Errorf("%s dark=%v: the cursor is %v, want the accent %v", name, dark, th.Cursor, a.base)
			}
			if c := contrast(th.Cursor, bg); c < 3 {
				t.Errorf("%s dark=%v: the cursor has %.2f:1 on the terminal's background, want 3:1", name, dark, c)
			}
			if c := contrast(th.CursorText, th.Cursor); c < 3 {
				t.Errorf("%s dark=%v: the text under the cursor has %.2f:1 on it", name, dark, c)
			}
			if th.Selection != a.selection {
				t.Errorf("%s dark=%v: the selection is %v, want the accent's %v", name, dark, th.Selection, a.selection)
			}
			if th.SearchSelected != th.Cursor || th.Search == th.SearchSelected {
				t.Errorf("%s dark=%v: find colours: the current match %v, the others %v, the cursor %v", name, dark, th.SearchSelected, th.Search, th.Cursor)
			}
			t.Logf("%-8s dark=%-5v cursor %v (fallback %v) cursor text %v selection %v", name, dark, th.Cursor, fell, th.CursorText, th.Selection)
		}
	}
}

// The terminal's colours are not brass in either theme, whatever the system accent.
func TestTerminalColoursAreNotBrass(t *testing.T) {
	for _, dark := range []bool{true, false} {
		useTheme(dark)
		useAccent(dark, ui.RGB(0, 122, 255))
		th := terminalTheme()
		for _, brass := range []string{"#e8b960", "#82510d"} {
			if th.Cursor == tok(brass) {
				t.Errorf("dark=%v: the cursor is brass %s", dark, brass)
			}
		}
		if th.Cursor != accent.base || th.Selection != accent.selection {
			t.Errorf("dark=%v: cursor %v selection %v, want the accent %v and %v", dark, th.Cursor, th.Selection, accent.base, accent.selection)
		}
	}
}

// A new system accent while running gives every terminal the new colours, once.
func TestAccentChangeRetintsTerminals(t *testing.T) {
	r := newRig(t, settingsBackend())
	r.w.openHost(testHosts[1])
	r.tt.Frame()
	n := 0
	for _, tb := range r.w.tabs {
		for _, p := range tb.panes {
			p.setTheme = func() { n++ }
		}
	}
	r.tt.Frame()
	if n != 0 {
		t.Fatalf("a frame that changes nothing told the terminal %d times", n)
	}
	r.tt.SetPreferences(ui.Preferences{Accent: ui.RGB(247, 79, 158)})
	r.tt.Frame()
	r.tt.Frame()
	if n != 1 {
		t.Fatalf("a new accent told the terminal %d times, want once", n)
	}
	if got := terminalTheme().Cursor; got != accent.base {
		t.Errorf("the cursor %v is not the new accent %v", got, accent.base)
	}
}
