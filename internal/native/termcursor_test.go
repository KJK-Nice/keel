package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
)

// Cursor style, Blink and the Option key load valid or as the default, are kept, and reach the panes opened afterwards
// (the plugin takes them when a terminal is made); a pane already open keeps what it was made with.
func TestCursorAndOptionKeyApplyToNewPanesAndAreKept(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(`{"cursorStyle": "beam", "cursorBlink": "no", "optionKey": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := loadPrefs(dir); p.CursorStyle != cursorBlock || !p.CursorBlink || p.OptionKey != optionSpecial {
		t.Fatalf("bad values load as the defaults: %q %v %q", p.CursorStyle, p.CursorBlink, p.OptionKey)
	}
	r := settingsRig(t, settingsBackend())
	r.w.openHost(testHosts[1])
	old := r.w.focusedPane()
	if old.cursor != terminal.CursorBlock || old.noBlink || old.optionAsAlt {
		t.Fatalf("defaults: %v %v %v", old.cursor, old.noBlink, old.optionAsAlt)
	}
	r.w.setCursorStyle(cursorBar)
	r.w.setCursorBlink(false)
	r.w.setOptionKey(optionAlt)
	r.w.setCursorStyle("beam") // not a style: the first
	if r.w.prefs.CursorStyle != cursorBlock {
		t.Errorf("an unknown style left %q", r.w.prefs.CursorStyle)
	}
	r.w.setCursorStyle(cursorUnderline)
	if old.cursor != terminal.CursorBlock || old.noBlink || old.optionAsAlt {
		t.Error("an open pane changed: the plugin cannot change these on a live terminal")
	}
	r.w.openHost(testHosts[1])
	if p := r.w.focusedPane(); p.cursor != terminal.CursorUnderline || !p.noBlink || !p.optionAsAlt {
		t.Errorf("a new pane: %v %v %v", p.cursor, p.noBlink, p.optionAsAlt)
	}
	r.w.flushPrefs()
	if got := loadPrefs(r.w.dataDir); got.CursorStyle != cursorUnderline || got.CursorBlink || got.OptionKey != optionAlt {
		t.Errorf("kept %q %v %q", got.CursorStyle, got.CursorBlink, got.OptionKey)
	}
	r.w.showPage("settings")
	r.tt.Frame()
	r.section(t, "Terminal")
	r.texts(t, "Cursor", "Cursor style", "Blink", "Applies to panes opened from now on.")
	if !r.has("cursor preview underline") {
		t.Error("the preview does not show the underline cursor")
	}
	if r.has("Option key") != isMac() {
		t.Errorf("the Option key row is macOS only")
	}
}
