package native

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// A confirm opens with the keyboard on Cancel, so ↵ on open asks nothing of the destructive action,
// whichever way the confirm was asked; the action is a click, or Tab and ↵.
func TestEveryConfirmStartsOnCancelAndEnterDoesNotDestroy(t *testing.T) {
	for _, name := range []string{"standalone", "inside the host dialog"} {
		t.Run(name, func(t *testing.T) {
			r := approveRig(t)
			ran := false
			cf := &hostConfirm{title: "Confirm", message: "Remove x?", action: "Remove", run: func() { ran = true }}
			if name == "standalone" {
				r.w.askConfirm(cf)
			} else {
				r.w.newHost()
				r.tt.Frame()
				r.w.hostDlg.confirm = cf
			}
			r.tt.Frame()
			r.tt.Frame()
			if !r.tt.Focused("Cancel") {
				t.Fatal("the keyboard does not start on Cancel")
			}
			r.tt.Key(0, ui.KeyEnter)
			for i := 0; i < 4; i++ {
				r.tt.Frame()
			}
			if ran {
				t.Fatal("↵ on open ran the destructive action")
			}
			if r.w.confirm != nil || (r.w.hostDlg != nil && r.w.hostDlg.confirm != nil) {
				t.Error("↵ on Cancel did not close the confirm")
			}
		})
	}
}

// The same, closing by a button, by a click outside, and by choosing a menu entry that opens nothing.
func TestClosingByButtonClickOrMenuGivesTheKeyboardBackToThePane(t *testing.T) {
	local := keelclient.Host{ID: "h-api", Name: "api-1", Address: "10.0.0.1", AuthType: "key"}
	for _, c := range []struct {
		name  string
		open  func(w *workspace)
		up    func(w *workspace) bool
		close func(r *rig)
	}{
		{"new host, Cancel", func(w *workspace) { w.newHost() }, func(w *workspace) bool { return w.hostDlg != nil }, func(r *rig) { r.tt.Click("Cancel") }},
		{"new snippet, Cancel", func(w *workspace) { w.newSnippet() }, func(w *workspace) bool { return w.snipDlg != nil }, func(r *rig) { r.tt.Click("Cancel") }},
		{"delete host, Cancel", func(w *workspace) { w.askDeleteHost(local) }, func(w *workspace) bool { return w.confirm != nil }, func(r *rig) { r.tt.Click("Cancel") }},
		{"palette, click outside", func(w *workspace) { w.openPalette() }, func(w *workspace) bool { return w.pal != nil }, func(r *rig) { r.tt.ClickAt(1270, 710) }},
		{"share, click outside", func(w *workspace) { w.openShare() }, func(w *workspace) bool { return w.share != nil }, func(r *rig) { r.tt.ClickAt(1270, 710) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := approveRig(t)
			c.open(r.w)
			r.tt.Frame()
			r.tt.Frame()
			if !c.up(r.w) {
				t.Fatal("it did not open")
			}
			c.close(r)
			for i := 0; i < 4; i++ {
				r.tt.Frame()
			}
			if c.up(r.w) {
				t.Fatal("it did not close")
			}
			if !r.tt.Focused("body 1") {
				t.Error("the keyboard did not go back to the pane")
			}
		})
	}
	t.Run("a menu entry that opens nothing", func(t *testing.T) {
		r := approveRig(t)
		if err := r.tt.RightClick("host api-1"); err != nil {
			t.Fatal(err)
		}
		if err := r.tt.ChooseMenuItem("Agent can read"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			r.tt.Frame()
		}
		if !r.tt.Focused("body 1") {
			t.Error("the keyboard did not go back to the pane")
		}
	})
}

// The pane has the keyboard right after a session opens, after a split (the new pane), and after the
// sidebar was clicked and then the terminal. (The headless tester does not deliver typed text to a fake session, so the
// keyboard owner is what can be checked here; text reaching the session needs the real window.)
func TestThePaneHasTheKeyboardAtOnce(t *testing.T) {
	t.Run("right after opening a host", func(t *testing.T) {
		r := newRig(t, okBackend())
		r.tt.Frame()
		r.click(t, "host api-1")
		r.tt.Frame()
		if !r.tt.Focused("body 1") {
			t.Error("the new session's pane does not have the keyboard")
		}
	})
	t.Run("after a split", func(t *testing.T) {
		r := approveRig(t)
		r.w.act(func() { r.w.split(r.w.tab(), r.w.tab().focused, layout.Row) })
		r.tt.Frame()
		r.tt.Frame()
		if len(r.fb.conns) < 2 {
			t.Skip("the split opened no second session in this rig")
		}
		r.tt.Frame()
		if !r.tt.Focused("body 2") {
			t.Error("the new pane does not have the keyboard")
		}
	})
	t.Run("after the sidebar was clicked, then the terminal", func(t *testing.T) {
		r := approveRig(t)
		r.click(t, "Filter hosts")
		r.click(t, "body 1")
		if !r.tt.Focused("body 1") {
			t.Error("the pane does not have the keyboard")
		}
	})
}

// Found in a real window: a drag on a sash gave the sash the keyboard and kept it, so the next typing (text
// goes to the focused element) reached nothing until the terminal was clicked. A pointer drag gives the pane the keyboard back;
// a sash reached by Tab keeps it for its arrow keys.
func TestDraggingASashGivesTheKeyboardBackToThePane(t *testing.T) {
	r := approveRig(t)
	s, ok := r.tt.Find("Resize sidebar")
	if !ok {
		t.Fatal("no sidebar sash")
	}
	x, y := s.X+s.W/2, s.Y+s.H/2
	r.tt.Press(x, y)
	r.tt.Move(x+30, y)
	r.tt.Frame()
	r.tt.Release(x+30, y)
	for i := 0; i < 4; i++ {
		r.tt.Frame()
	}
	if !r.tt.Focused("body 1") {
		t.Error("after a drag the keyboard is not on the pane")
	}
}

// Whatever opens over a terminal pane (a dialog, a popover, a menu, the palette, a confirm), closing it
// by Escape gives the keyboard back to the pane that had it. Typing then reaches the terminal, not the page behind it.
func TestEscapeGivesTheKeyboardBackToThePane(t *testing.T) {
	local := keelclient.Host{ID: "h-api", Name: "api-1", Address: "10.0.0.1", AuthType: "key"}
	for _, c := range []struct {
		name string
		open func(w *workspace)
		up   func(w *workspace) bool
	}{
		{"command palette", func(w *workspace) { w.openPalette() }, func(w *workspace) bool { return w.pal != nil }},
		{"new host", func(w *workspace) { w.newHost() }, func(w *workspace) bool { return w.hostDlg != nil }},
		{"new snippet", func(w *workspace) { w.newSnippet() }, func(w *workspace) bool { return w.snipDlg != nil }},
		{"new folder", func(w *workspace) { w.newFolder("") }, func(w *workspace) bool { return w.folderDlg != nil }},
		{"new forward", func(w *workspace) { w.newForward() }, func(w *workspace) bool { return w.fwdDlg != nil }},
		{"new identity", func(w *workspace) { w.newIdentity() }, func(w *workspace) bool { return w.identDlg != nil }},
		{"join a session", func(w *workspace) { w.openJoin() }, func(w *workspace) bool { return w.joinDlg != nil }},
		{"delete host confirm", func(w *workspace) { w.askDeleteHost(local) }, func(w *workspace) bool { return w.confirm != nil }},
		{"approve before type", func(w *workspace) { w.askType(fixed("echo hi")) }, func(w *workspace) bool { return w.approve != nil }},
		{"share popover", func(w *workspace) { w.openShare() }, func(w *workspace) bool { return w.share != nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := approveRig(t)
			if !r.tt.Focused("body 1") {
				t.Fatal("the pane does not start with the keyboard")
			}
			c.open(r.w)
			r.tt.Frame()
			r.tt.Frame()
			if !c.up(r.w) {
				t.Fatal("it did not open")
			}
			r.tt.Key(0, ui.KeyEscape)
			for i := 0; i < 4; i++ {
				r.tt.Frame()
			}
			if c.up(r.w) {
				t.Fatal("Escape did not close it")
			}
			if !r.tt.Focused("body 1") {
				t.Error("the keyboard did not go back to the pane")
			}
		})
	}
}
