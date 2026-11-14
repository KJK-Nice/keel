package native

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Approve before type on the fake backend: what the dialog shows is what is sent, nothing is sent before Type, and
// a follower or a session that is not ready cannot type.

var drivingState = keelclient.State{Status: "ready", Role: "driver", Subtitle: "deploy@10.0.0.1", YouID: 2, DriverID: 2}

// approveRig is a window with one session this client drives.
func approveRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "host api-1")
	st := drivingState
	r.fb.conns[0].st = &st
	r.tt.Frame()
	return r
}

// fixed is a command source that never changes.
func fixed(s string) func() (string, bool) { return func() (string, bool) { return s, true } }

func (r *rig) typed() []string { return r.fb.conns[0].injected }

func TestOpeningTheDialogSendsNothingAndShowsTheWebviewsWords(t *testing.T) {
	r := approveRig(t)
	r.w.askType(fixed("echo hi"))
	r.tt.Frame()
	for _, s := range []string{approveTitle, approveHint, "Session:", "deploy@10.0.0.1", "echo hi"} {
		if !r.tt.HasText(s) {
			t.Errorf("no text %q", s)
		}
	}
	if approveTitle != "Type into the terminal?" || !strings.Contains(approveHint, "The current line is cleared first. Nothing is typed until you approve.") {
		t.Fatal("the title or sentence is not the webview's")
	}
	if len(r.typed()) != 0 || r.fb.conns[0].wrote != 0 {
		t.Fatalf("opening the dialog typed %v / wrote %d", r.typed(), r.fb.conns[0].wrote)
	}
	if !r.tt.Focused("Cancel") {
		t.Error("the keyboard does not start on Cancel, as the webview's dialog does")
	}
}

func TestTypeSendsCtrlUThenExactlyWhatTheDialogShowed(t *testing.T) {
	r := approveRig(t)
	r.w.askType(fixed("echo hi"))
	r.tt.Frame()
	shown := r.w.approveDlg().shown()
	if shown != "echo hi\n" { // a trailing newline is added when there is none
		t.Fatalf("shown %q", shown)
	}
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15" + shown}) {
		t.Fatalf("sent %q, want Ctrl-U + %q", got, shown)
	}
	if r.w.approveOpen() {
		t.Error("the dialog stays open after a Type that went through")
	}
	if !r.tt.Focused("body 1") {
		t.Error("the keyboard is not given back to the pane")
	}
	if r.fb.conns[0].wrote != 0 {
		t.Error("the keystroke path was used")
	}
	// A command that already ends with a newline gets no second one.
	r.w.askType(fixed("ls\n"))
	r.tt.Frame()
	r.click(t, "Type")
	if got := r.typed(); got[len(got)-1] != "\x15ls\n" {
		t.Fatalf("sent %q", got[len(got)-1])
	}
}

func TestPreviewFollowsTheSourceAndNothingIsCached(t *testing.T) {
	r := approveRig(t)
	cmd := "alpha"
	r.w.askType(func() (string, bool) { return cmd, true })
	r.tt.Frame()
	if !r.tt.HasText("alpha") {
		t.Fatal("no alpha")
	}
	cmd = "second line\nthird"
	r.tt.Frame()
	if r.tt.HasText("alpha") {
		t.Error("the preview kept the old text")
	}
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15second line\nthird\n"}) {
		t.Fatalf("sent %q: not what the dialog showed last", got)
	}
}

func TestControlCharactersAreShownAsTheyAreSent(t *testing.T) {
	// The dialog shows the text as it is, and the same string is sent.
	r := approveRig(t)
	cmd := "printf 'a\tb'\x1b[2J\r"
	r.w.askType(fixed(cmd))
	r.tt.Frame()
	if r.w.approveDlg().shown() != cmd+"\n" {
		t.Fatalf("shown %q", r.w.approveDlg().shown())
	}
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15" + cmd + "\n"}) {
		t.Fatalf("sent %q", got)
	}
}

func TestEscapeCancelAndClickingOutsideSendNothing(t *testing.T) {
	r := approveRig(t)
	for _, how := range []string{"escape", "cancel", "outside"} {
		r.w.askType(fixed("rm -rf /tmp/x"))
		r.tt.Frame()
		switch how {
		case "escape":
			r.press(0, ui.KeyEscape)
		case "cancel":
			r.click(t, "Cancel")
		case "outside":
			r.tt.ClickAt(2, 2)
			r.tt.Frame()
		}
		if r.w.approveOpen() {
			t.Errorf("%s: the dialog is still open", how)
		}
		if len(r.typed()) != 0 || r.fb.conns[0].wrote != 0 {
			t.Fatalf("%s typed %v", how, r.typed())
		}
	}
}

func TestAFollowerOrASessionNotReadyCannotType(t *testing.T) {
	for _, c := range []struct {
		name string
		st   keelclient.State
		msg  string
	}{
		{"following", keelclient.State{Status: "ready", Role: "follower"}, msgFollowing},
		{"owner not driving", keelclient.State{Status: "ready", Role: "owner"}, msgFollowing},
		{"connecting", keelclient.State{Status: "connecting", Role: "driver"}, msgNotConnected},
		{"error", keelclient.State{Status: "error", Role: "driver"}, msgNotConnected},
	} {
		r := approveRig(t)
		st := c.st
		r.fb.conns[0].st = &st
		r.w.askType(fixed("ls"))
		r.tt.Frame()
		if !r.tt.HasText(c.msg) {
			t.Errorf("%s: the dialog does not say %q", c.name, c.msg)
		}
		_ = r.tt.Click("Type")
		r.tt.Frame()
		r.w.approveType(false) // even called directly, it refuses
		if len(r.typed()) != 0 {
			t.Errorf("%s typed %v", c.name, r.typed())
		}
		if !r.w.approveOpen() || r.w.approveDlg().err != c.msg {
			t.Errorf("%s: dialog gone or error %q", c.name, r.w.approveDlg().err)
		}
	}
	if msgFollowing != "You are following — take or request control to type" || msgNotConnected != "Session is not connected" || msgNoSession != "Open a session first" {
		t.Fatal("not the webview's toasts")
	}
}

func TestAServerRefusalShowsItsMessageAndStaysOpenWithoutRetry(t *testing.T) {
	r := approveRig(t)
	r.fb.conns[0].injectErr = keelclient.ErrNoTicket
	r.w.askType(fixed("ls"))
	r.tt.Frame()
	r.click(t, "Type")
	if !r.w.approveOpen() || r.w.approveDlg().err != "Session is not connected" {
		t.Fatalf("dialog %v", r.w.approveDlg())
	}
	if !r.tt.HasText("Session is not connected") {
		t.Error("the refusal is not shown")
	}
	if len(r.typed()) != 1 {
		t.Fatalf("typed %v: Keel retried on its own", r.typed())
	}
	// Another press is another request, made by the user.
	r.fb.conns[0].injectErr = errors.New("x")
	r.click(t, "Type")
	if len(r.typed()) != 2 {
		t.Fatalf("typed %v", r.typed())
	}
}

func TestADeletedSnippetCannotBeTyped(t *testing.T) {
	r := approveRig(t)
	gone := false
	r.w.askType(func() (string, bool) { return "ls", !gone })
	r.tt.Frame()
	gone = true
	r.tt.Frame()
	if !r.tt.HasText(msgGone) {
		t.Error("the dialog does not say it is gone")
	}
	_ = r.tt.Click("Type")
	if len(r.typed()) != 0 {
		t.Fatalf("typed %v", r.typed())
	}
}

func TestNoSessionPaneMeansNoDialog(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.w.askType(fixed("ls"))
	r.tt.Frame()
	if r.w.approveOpen() || !r.tt.HasText("Open a session first") {
		t.Fatalf("dialog %v, toast shown %v", r.w.approveDlg(), r.tt.HasText("Open a session first"))
	}
}

func TestAPaneClosedUnderTheDialogCannotBeTypedInto(t *testing.T) {
	r := approveRig(t)
	r.w.askType(fixed("ls"))
	r.tt.Frame()
	t0 := r.w.activeTab()
	r.w.closePane(t0, t0.focused)
	r.tt.Frame()
	r.w.approveType(false)
	if len(r.typed()) != 0 {
		t.Fatalf("typed %v into a closed pane", r.typed())
	}
}

func TestNoShortcutReachesTheTerminalWhileTheDialogIsOpen(t *testing.T) {
	r := approveRig(t)
	r.w.askType(fixed("ls"))
	r.tt.Frame()
	if !r.w.modalOpen() {
		t.Fatal("the dialog is not a modal")
	}
	r.tt.Type("abc")
	r.tt.Frame()
	if r.fb.conns[0].wrote != 0 || len(r.typed()) != 0 {
		t.Fatal("keys typed while the dialog was open reached the session")
	}
}
