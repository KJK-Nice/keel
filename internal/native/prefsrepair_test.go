package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPrefsFileThatIsNotJSONIsPutAsideAndWrittenAgainWithOneToast(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keel-native.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRigIn(t, okBackend(), dir)
	r.w.prefs.TermSize = 15
	r.w.setPref(keyTermSize)
	r.w.setPref(keyTermSize)
	if b, _ := os.ReadFile(path + ".bad"); string(b) != "{" {
		t.Errorf("the bad file is %q", b)
	}
	if loadPrefs(dir).TermSize != 15 {
		t.Error("the new file does not hold the setting")
	}
	toasts := 0
	for _, q := range r.w.queued {
		if strings.Contains(q.Title, "keel-native.json.bad") {
			toasts++
		}
	}
	if toasts != 1 || len(r.w.queued) != 1 {
		t.Errorf("toasts %+v", r.w.queued)
	}
}
