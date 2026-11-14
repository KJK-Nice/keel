package native

import (
	"testing"

	"keel/internal/wire"
)

// Issue #139: while a pane's session is not ready (connecting, failed, lost) its terminal view is not in the
// tree, so its last picture is not readable through the overlay and no caret blinks; when the session is ready the view is
// back, and the pane takes the keyboard again if it had it. The terminal object (here the placeholder's p.view) is not rebuilt.
func TestTheTerminalViewIsAbsentWhileTheSessionIsNotReadyAndBackWhenItIs(t *testing.T) {
	r := newSugRig(t)
	r.tt.Frame()
	if !r.has("body 1") {
		t.Fatal("no terminal view in a ready pane")
	}
	if !r.tt.Focused("body 1") {
		t.Fatal("the ready pane does not have the keyboard to begin with")
	}
	for _, status := range []wire.Status{wire.StatusConnecting, wire.StatusError, wire.StatusExited} {
		st := drivingState
		st.Status = status
		r.c.st = &st
		r.tt.Frame()
		if r.has("body 1") {
			t.Errorf("the terminal view is in the tree while the session is %s", status)
		}
		if !r.has("Close") {
			t.Errorf("no Close while the session is %s", status)
		}
		// Typed keys have no terminal to reach, and no pane's session is written to.
		r.tt.Type("x")
		r.tt.Frame()
		if r.c.wrote != 0 || len(r.c.injected) != 0 || len(r.c.directs) != 0 {
			t.Errorf("typing in a %s pane wrote to a session", status)
		}
	}
	r.c.st = &drivingState
	r.tt.Frame()
	r.tt.Frame()
	if !r.has("body 1") {
		t.Fatal("the terminal view did not come back")
	}
	if !r.tt.Focused("body 1") {
		t.Error("the pane did not take the keyboard back")
	}
}
