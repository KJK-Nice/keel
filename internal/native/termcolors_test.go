package native

import (
	"os"
	"path/filepath"
	"testing"
)

// Colors: the contrast and selection rows load valid or as the default, reach an open pane's theme and contrast at
// once, and are kept.
func TestColorsRowsApplyToOpenPanesAndAreKept(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(`{"termContrast": "max", "selectionBg": "crimson", "selectionText": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := loadPrefs(dir); p.Contrast != contrastOff || p.SelectionBg != colorTheme || p.SelectionText != colorTheme {
		t.Fatalf("bad values load as the defaults: %q %q %q", p.Contrast, p.SelectionBg, p.SelectionText)
	}
	r := settingsRig(t, settingsBackend())
	r.w.openHost(testHosts[1])
	p := r.w.focusedPane()
	if th := p.theme(); th.Selection != accent.selection || th.SelectionText != (terminalTheme().SelectionText) || p.contrast != 1 {
		t.Fatalf("defaults: selection %v text %v contrast %v", th.Selection, th.SelectionText, p.contrast)
	}
	themes, ratios := 0, []float32{}
	p.setTheme = func() { themes++ }
	p.setContrast = func(r float32) { ratios = append(ratios, r) }
	r.w.setContrast(contrastHigh)
	r.w.setSelectionBg("blue")
	r.w.setSelectionText("yellow")
	th := p.theme()
	if want := ansi[4].Alpha(float32(accent.selection.A) / 255); th.Selection != want {
		t.Errorf("selection %v, want blue as translucent as the accent's: %v", th.Selection, want)
	}
	if th.SelectionText != ansi[3] || p.contrast != 4.5 || themes != 3 || ratios[len(ratios)-1] != 4.5 {
		t.Errorf("text %v contrast %v, setTheme %d, ratios %v", th.SelectionText, p.contrast, themes, ratios)
	}
	p.cursorCol = ansi[2] // someone else drives: the cursor stays theirs
	if p.theme().Cursor != ansi[2] {
		t.Error("the driver's cursor colour is lost")
	}
	r.w.flushPrefs()
	if got := loadPrefs(r.w.dataDir); got.Contrast != contrastHigh || got.SelectionBg != "blue" || got.SelectionText != "yellow" {
		t.Errorf("kept %q %q %q", got.Contrast, got.SelectionBg, got.SelectionText)
	}
	r.w.showPage("settings")
	r.tt.Frame()
	r.section(t, "Terminal")
	r.texts(t, "Colors", "Improve text contrast", "Selection background", "Selection text")
	r.reveal(t, "Selection background picker")
	r.click(t, "Selection background picker")
	if err := r.tt.ChooseMenuItem("Theme"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.w.prefs.SelectionBg != colorTheme || p.theme().Selection != accent.selection {
		t.Errorf("Theme in the picker: %q", r.w.prefs.SelectionBg)
	}
}
