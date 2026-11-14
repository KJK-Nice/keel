package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Settings sliders and sash drags set a pref many times in a row. However the writes are scheduled, the file ends with
// the last value set.
func TestPrefsFileEndsWithTheLastValueSet(t *testing.T) {
	dir := t.TempDir()
	w := newWorkspace(okBackend())
	w.dataDir, w.prefs = dir, loadPrefs(dir)
	var queued []func()
	w.work = func(fn func()) { queued = append(queued, fn) }
	w.post = func(fn func()) { fn() }
	for n := termSizeMin; n <= termSizeMax; n++ {
		w.setTermSize(n)
	}
	for i := len(queued) - 1; i >= 0; i-- { // the scheduler runs them newest first
		queued[i]()
	}
	if got := loadPrefs(dir).TermSize; got != termSizeMax {
		t.Errorf("keel-native.json holds termSize %d, want %d", got, termSizeMax)
	}
	if len(queued) != 1 {
		t.Errorf("%d writes for one burst of sets", len(queued))
	}

	// A failed write names the setting.
	os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte("{"), 0o600)
	w.work = func(fn func()) { fn() }
	w.setTermSize(termSizeMin)
	if len(w.queued) != 1 || !strings.HasPrefix(w.queued[0].Title, "save the text size: ") {
		t.Errorf("toasts %+v", w.queued)
	}

	// A write still waiting when the window closes is made by the close.
	os.Remove(filepath.Join(dir, "keel-native.json"))
	w.work = func(func()) {}
	w.setTermSize(15)
	w.closeWindow()
	if got := loadPrefs(dir).TermSize; got != 15 {
		t.Errorf("after close keel-native.json holds termSize %d, want 15", got)
	}
}
