package native

import (
	"strings"
	"testing"
)

// shellRows lists a menu as "label accel" lines, "-" for a separator.
func shellRows(es []menuEntry) (out []string) {
	for _, e := range es {
		if e.label == "" {
			out = append(out, "-")
			continue
		}
		out = append(out, e.label+" "+e.accel)
	}
	return out
}

// The labels and chords are those of the webview app's menus (internal/desktop/desktop.go fileMenu and viewMenu).
func TestShellMenusAreTheWebviewsMenus(t *testing.T) {
	file := "New Tab CmdOrCtrl+t|Close Tab CmdOrCtrl+w|Reopen Tab CmdOrCtrl+Shift+t|-|New Host CmdOrCtrl+n|" +
		"Quick Connect CmdOrCtrl+Shift+o|Join Session CmdOrCtrl+j|-|Split Right CmdOrCtrl+Shift+d|" +
		"Split Down CmdOrCtrl+Shift+e|Split Notebook "
	if got := strings.Join(shellRows(fileMenu()), "|"); got != file {
		t.Errorf("File:\n%s\nwant\n%s", got, file)
	}
	view := func(agent string) string {
		return "Find in Terminal CmdOrCtrl+f|Toggle Vault Sidebar CmdOrCtrl+b|Toggle Agent Panel " + agent +
			"|-|Focus View CmdOrCtrl+Shift+enter|-|Zoom In CmdOrCtrl+=|Zoom Out CmdOrCtrl+-|Actual Size CmdOrCtrl+0"
	}
	for goos, agent := range map[string]string{"darwin": "CmdOrCtrl+i", "linux": "Ctrl+Shift+i", "windows": "Ctrl+Shift+i"} {
		if got := strings.Join(shellRows(viewMenu(goos)), "|"); got != view(agent) {
			t.Errorf("View on %s:\n%s\nwant\n%s", goos, got, view(agent))
		}
	}
}

// Every item has an action, and the ones that change the workspace do what the chord does.
func TestShellMenuActions(t *testing.T) {
	for _, e := range append(append(fileMenu(), viewMenu("darwin")...), stopRuntimeEntry) {
		if e.label != "" && e.run == nil {
			t.Errorf("%s has no action", e.label)
		}
	}
	w := newWorkspace(setupBackend())
	w.work = func(fn func()) { fn() }
	w.post = func(fn func()) { fn() }
	byLabel := func(es []menuEntry, label string) menuEntry {
		for _, e := range es {
			if e.label == label {
				return e
			}
		}
		t.Fatalf("no item %q", label)
		return menuEntry{}
	}
	view := viewMenu("darwin")

	byLabel(fileMenu(), "New Tab").run(w)
	if len(w.tabs) != 1 {
		t.Errorf("New Tab: %d tabs", len(w.tabs))
	}
	was := w.sidebar
	byLabel(view, "Toggle Vault Sidebar").run(w)
	if w.sidebar == was {
		t.Error("Toggle Vault Sidebar did nothing")
	}
	size := w.termSize
	byLabel(view, "Zoom In").run(w)
	if w.termSize != size+1 {
		t.Errorf("Zoom In: %d, was %d", w.termSize, size)
	}
	byLabel(view, "Zoom Out").run(w)
	byLabel(view, "Actual Size").run(w)
	if w.termSize != termSizeDefault {
		t.Errorf("Actual Size: %d", w.termSize)
	}
	byLabel(fileMenu(), "Join Session").run(w)
	if w.joinDlg == nil {
		t.Error("Join Session opened nothing")
	}
}

func TestTrayStatusLabelAndMenuBar(t *testing.T) {
	for _, c := range []struct {
		running, locked bool
		n               int
		want            string
	}{
		{false, false, 0, "keel serve is not running"},
		{true, true, 3, "keel serve · vault locked"},
		{true, false, 1, "keel serve · 1 session"},
		{true, false, 0, "keel serve · 0 sessions"},
		{true, false, 4, "keel serve · 4 sessions"},
	} {
		if got := statusLabel(c.running, c.locked, c.n); got != c.want {
			t.Errorf("%v: %q", c, got)
		}
	}
	for goos, want := range map[string]bool{"darwin": true, "linux": false, "windows": false} {
		if menuBarItem(goos) != want {
			t.Errorf("menuBarItem(%s) != %v", goos, want)
		}
	}
}
