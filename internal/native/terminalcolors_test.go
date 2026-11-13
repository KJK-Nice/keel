package native

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Plan 032 section 11: the terminal's cursor is its text colour, the selection is the system accent's, not brass.

var macAccents = map[string]ui.Color{
	"blue": ui.RGB(0, 122, 255), "purple": ui.RGB(149, 61, 150), "pink": ui.RGB(247, 79, 158), "red": ui.RGB(224, 56, 62),
	"orange": ui.RGB(247, 130, 27), "yellow": ui.RGB(255, 198, 0), "green": ui.RGB(98, 186, 70), "graphite": ui.RGB(140, 140, 140),
}

func TestTerminalCursorIsTheTextColourAndSelectionTheAccents(t *testing.T) {
	for _, dark := range []bool{true, false} {
		p := paletteOf(dark)
		fg, bg := tok(p.termFg), tok(p.termBg)
		for name, col := range macAccents {
			a := deriveAccent(dark, col)
			th := terminalColors(a, fg, bg, ansi)
			if th.Cursor != fg || th.CursorText != bg {
				t.Errorf("%s dark=%v: cursor %v text under it %v, want the terminal's text %v and background %v", name, dark, th.Cursor, th.CursorText, fg, bg)
			}
			if th.Selection != a.selection {
				t.Errorf("%s dark=%v: the selection is %v, want the accent's %v", name, dark, th.Selection, a.selection)
			}
			if th.SearchSelected != a.base || th.Search == th.SearchSelected {
				t.Errorf("%s dark=%v: find colours: the current match %v, the others %v, the accent %v", name, dark, th.SearchSelected, th.Search, a.base)
			}
			if c := contrast(th.SearchSelectedText, th.SearchSelected); c < 3 {
				t.Errorf("%s dark=%v: the text of the current match has %.2f:1", name, dark, c)
			}
		}
		if c := contrast(fg, bg); c < 7 {
			t.Errorf("dark=%v: the cursor has %.2f:1 on the terminal's background", dark, c)
		}
	}
}

// The terminal's colours are not brass in either theme, whatever the system accent.
func TestTerminalColoursAreNotBrass(t *testing.T) {
	for _, dark := range []bool{true, false} {
		useTheme(dark)
		useAccent(dark, ui.RGB(0, 122, 255))
		th := terminalTheme()
		for _, brass := range []ui.Color{ui.RGB(0xe8, 0xb9, 0x60), ui.RGB(0x82, 0x51, 0x0d)} { // the old dark and light cursors
			if th.Cursor == brass {
				t.Errorf("dark=%v: the cursor is brass %v", dark, brass)
			}
		}
		if th.Cursor != termFg || th.CursorText != termBg || th.Selection != accent.selection {
			t.Errorf("dark=%v: cursor %v on %v, selection %v, want the text colour, the background and the accent's %v", dark, th.Cursor, th.CursorText, th.Selection, accent.selection)
		}
	}
}

// A new system accent while running gives every terminal the new selection, once.
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
	if got := terminalTheme().Selection; got != accent.selection {
		t.Errorf("the selection %v is not the new accent's %v", got, accent.selection)
	}
}
