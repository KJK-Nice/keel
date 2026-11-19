package native

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// Settings · Terminal · Colors: Improve text contrast (the plugin's MinimumContrast), and the selection's background
// and text, each the theme's or a colour of the ANSI palette (tokens only: no free colour). Kept in keel-native.json
// ("termContrast", "selectionBg", "selectionText") and applied to every open pane at once.

const (
	contrastOff, contrastSome, contrastHigh = "off", "some", "high"
	colorTheme                              = "theme" // the theme's own: the system accent's selection, the text's own colour
)

var contrastModes = [][2]string{{contrastOff, "Off"}, {contrastSome, "Some"}, {contrastHigh, "High"}}

// contrastRatio is the least WCAG contrast a mode keeps text at: 3:1 is WCAG's for large text, 4.5:1 its AA for body text.
func contrastRatio(mode string) float32 {
	switch mode {
	case contrastSome:
		return 3
	case contrastHigh:
		return 4.5
	}
	return 1
}

func validContrast(v string) string {
	for _, m := range contrastModes {
		if m[0] == v {
			return v
		}
	}
	return contrastOff
}

// selColors are the selection's choices beside the theme's: the six hues of the ANSI palette, by index.
var selColors = []struct {
	value, label string
	index        int
}{{"red", "Red", 1}, {"green", "Green", 2}, {"yellow", "Yellow", 3}, {"blue", "Blue", 4}, {"magenta", "Magenta", 5}, {"cyan", "Cyan", 6}}

func validSelColor(v string) string {
	for _, s := range selColors {
		if s.value == v {
			return v
		}
	}
	return colorTheme
}

// paletteColor is the palette colour a value names, and false for the theme's.
func paletteColor(v string) (ui.Color, bool) {
	for _, s := range selColors {
		if s.value == v {
			return ansi[s.index], true
		}
	}
	return ui.Color{}, false
}

// selEntries are a selection picker's choices: the theme's first, described by what it is for that picker.
func selEntries(themeLabel string) []pickEntry {
	out := []pickEntry{{value: colorTheme, label: "Theme", desc: themeLabel}}
	for _, s := range selColors {
		out = append(out, pickEntry{value: s.value, label: s.label})
	}
	return out
}

// termColors is the pane's copy of the Colors preferences.
type termColors struct{ selBg, selText string }

// theme is the terminal theme p draws with: Slime's, its selection as the preferences say, and its cursor the driver's
// colour while someone else drives (presence.go).
func (p *pane) theme() *terminal.Theme {
	t := terminalTheme()
	if c, ok := paletteColor(p.colors.selBg); ok { // as translucent as the accent's selection, so the text shows through
		t.Selection = c.Alpha(float32(accent.selection.A) / 255)
	}
	if c, ok := paletteColor(p.colors.selText); ok {
		t.SelectionText = c
	}
	if p.cursorCol != (ui.Color{}) {
		t.Cursor = p.cursorCol
	}
	return t
}

// applyTermColors gives every open pane the Colors preferences.
func (w *workspace) applyTermColors() {
	for _, t := range w.tabs {
		for _, p := range t.panes {
			p.colors, p.contrast = termColors{w.prefs.SelectionBg, w.prefs.SelectionText}, contrastRatio(w.prefs.Contrast)
			if p.setTheme != nil {
				p.setTheme()
			}
			if p.setContrast != nil {
				p.setContrast(p.contrast)
			}
		}
	}
}

// setContrast is the Improve text contrast row.
func (w *workspace) setContrast(v string) {
	if v = validContrast(v); v != w.prefs.Contrast {
		w.prefs.Contrast = v
		w.setPref(keyContrast)
		w.applyTermColors()
	}
}

// setSelectionBg is the Selection background row.
func (w *workspace) setSelectionBg(v string) {
	if v = validSelColor(v); v != w.prefs.SelectionBg {
		w.prefs.SelectionBg = v
		w.setPref(keySelectionBg)
		w.applyTermColors()
	}
}

// setSelectionText is the Selection text row.
func (w *workspace) setSelectionText(v string) {
	if v = validSelColor(v); v != w.prefs.SelectionText {
		w.prefs.SelectionText = v
		w.setPref(keySelectionText)
		w.applyTermColors()
	}
}
