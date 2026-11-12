package native

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Plan 031 step 2, group 7: a confirm opens with the keyboard on Cancel, so ↵ on open asks nothing of the destructive action,
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

// Plan 031 step 0: whatever opens over a terminal pane (a dialog, a popover, a menu, the palette, a confirm), closing it
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
