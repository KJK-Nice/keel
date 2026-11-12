package native

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// fakeSearch stands for a terminal's own search and font.
type fakeSearch struct {
	texts      []string
	next, prev int
	sel, total int
	fonts      []float32
}

// withSearch gives every pane opened from now on a fake search and a fake font.
func (r *rig) withSearch() *fakeSearch {
	fs := &fakeSearch{}
	base := r.w.mkBody
	r.w.mkBody = func(p *pane) {
		base(p)
		p.search = &termSearch{
			start:  func(s string) { fs.texts = append(fs.texts, s) },
			next:   func() { fs.next++ },
			prev:   func() { fs.prev++ },
			status: func() (int, int) { return fs.sel, fs.total },
		}
		p.setFont = func(sz float32) { fs.fonts = append(fs.fonts, sz) }
	}
	return fs
}

func TestFindBarOpensSearchesAndReturnsTheKeyboard(t *testing.T) {
	r := newRig(t, okBackend())
	fs := r.withSearch()
	r.tt.Frame()
	r.click(t, "host api-1")
	if _, ok := r.tt.Find("find bar"); ok {
		t.Fatal("the find bar is open before ⌘F")
	}
	r.tt.Key(ui.Cmd, ui.KeyF)
	r.tt.Frame()
	r.tt.Frame()
	if _, ok := r.tt.Find("find bar"); !ok || !r.tt.Focused("Find in terminal") {
		t.Fatal("⌘F did not open the find bar with the keyboard in its field")
	}
	for _, s := range []string{"Previous match", "Next match", "Close"} {
		if _, ok := r.tt.Find(s); !ok {
			t.Errorf("the find bar has no %q", s)
		}
	}
	r.tt.Type("needle")
	r.tt.Frame()
	if len(fs.texts) == 0 || fs.texts[len(fs.texts)-1] != "needle" {
		t.Fatalf("the terminal was asked to search for %q", fs.texts)
	}
	fs.sel, fs.total = 1, 3
	r.tt.Frame()
	if !r.tt.HasText("1/3") {
		t.Error("the bar does not show the terminal's match count")
	}

	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	r.tt.Key(ui.Shift, ui.KeyEnter)
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.KeyG)
	r.tt.Frame()
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyG)
	r.tt.Frame()
	if fs.next != 2 || fs.prev != 2 {
		t.Errorf("next %d prev %d, want 2 and 2 (Enter and ⌘G, ⇧Enter and ⌘⇧G)", fs.next, fs.prev)
	}
	r.click(t, "Next match")
	r.click(t, "Previous match")
	if fs.next != 3 || fs.prev != 3 {
		t.Errorf("the buttons: next %d prev %d", fs.next, fs.prev)
	}

	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	r.tt.Frame()
	if _, ok := r.tt.Find("find bar"); ok || r.w.findPane != nil {
		t.Error("Escape did not close the bar")
	}
	if fs.texts[len(fs.texts)-1] != "" {
		t.Errorf("closing did not end the search: %q", fs.texts)
	}
	if !r.tt.Focused("body 1") {
		t.Error("the keyboard did not go back to the terminal")
	}
	if r.fb.conns[0].wrote != 0 {
		t.Errorf("find wrote %d bytes to the session", r.fb.conns[0].wrote)
	}
}

func TestFindBarIsInOnePaneAndGoesWithIt(t *testing.T) {
	r := newRig(t, okBackend())
	fs := r.withSearch()
	r.tt.Frame()
	r.click(t, "host api-1")
	r.w.splitFocused(0)
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.KeyF)
	r.tt.Frame()
	if r.w.findPane != r.w.tab().panes["2"] {
		t.Fatal("find opened in a pane that is not the focused one")
	}
	r.w.closeFocused()
	r.tt.Frame()
	if r.w.findPane != nil || fs.texts[len(fs.texts)-1] != "" {
		t.Error("closing the pane left its find open")
	}
	// A pane with no search (a terminal that failed to start) opens nothing.
	r.w.tab().panes["1"].search = nil
	r.tt.Key(ui.Cmd, ui.KeyF)
	r.tt.Frame()
	if r.w.findPane != nil {
		t.Error("find opened on a pane without a search")
	}
}

func TestTextSizeKeysClampAndSave(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, okBackend())
	r.w.useDataDir(t, dir)
	fs := r.withSearch()
	r.tt.Frame()
	r.click(t, "host api-1")
	r.w.splitFocused(0)
	r.tt.Frame()
	press := func(mods ui.Modifiers, k ui.Key) {
		r.tt.Key(mods, k)
		r.tt.Frame()
	}
	if r.w.termSize != 12 {
		t.Fatalf("default size %d, want 12", r.w.termSize)
	}
	press(ui.Cmd, ui.KeyEqual)
	press(ui.Cmd|ui.Shift, ui.KeyEqual) // ⌘+
	if r.w.termSize != 14 {
		t.Errorf("after two ⌘+: %d", r.w.termSize)
	}
	for _, p := range r.w.tab().panes {
		if p.fontSize != 14 {
			t.Errorf("pane %s font %v", p.id, p.fontSize)
		}
	}
	press(ui.Cmd, ui.KeyMinus)
	if r.w.termSize != 13 {
		t.Errorf("⌘-: %d", r.w.termSize)
	}
	press(ui.Cmd, ui.Key0)
	if r.w.termSize != 12 {
		t.Errorf("⌘0: %d", r.w.termSize)
	}
	for i := 0; i < 20; i++ {
		press(ui.Cmd, ui.KeyEqual)
	}
	if r.w.termSize != 22 {
		t.Errorf("clamped high: %d", r.w.termSize)
	}
	for i := 0; i < 20; i++ {
		press(ui.Cmd, ui.KeyMinus)
	}
	if r.w.termSize != 10 {
		t.Errorf("clamped low: %d", r.w.termSize)
	}
	if len(fs.fonts) == 0 {
		t.Error("no pane was given a new font")
	}
	if got := loadPrefs(dir).TermSize; got != 10 {
		t.Errorf("saved size %d, want 10", got)
	}
	if r.fb.conns[0].wrote != 0 {
		t.Error("a text size key reached the session")
	}
}

func TestSavePrefsKeepsOtherFieldsAndWritesPrivately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keel-native.json")
	if err := os.WriteFile(path, []byte(`{"opacity":80,"blur":10,"future":{"a":[1,2]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := savePrefs(dir, map[string]any{"termSize": 15}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var future bytes.Buffer
	json.Compact(&future, got["future"])
	if !slices.Equal(keys, []string{"blur", "future", "opacity", "termSize"}) || future.String() != `{"a":[1,2]}` {
		t.Errorf("fields %v, future %s", keys, got["future"])
	}
	if p := loadPrefs(dir); p.Opacity != 80 || p.Blur != 10 || p.TermSize != 15 {
		t.Errorf("loaded %+v", p)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
	// A file that is not JSON is not overwritten.
	os.WriteFile(path, []byte("{"), 0o600)
	if err := savePrefs(dir, map[string]any{"termSize": 11}); err == nil {
		t.Error("saved over a file that is not JSON")
	}
	if b, _ := os.ReadFile(path); string(b) != "{" {
		t.Errorf("the file became %q", b)
	}
	// Out of range values in the file give the default.
	os.WriteFile(path, []byte(`{"termSize":99}`), 0o600)
	if loadPrefs(dir).TermSize != 12 {
		t.Error("an out of range termSize was used")
	}
}
