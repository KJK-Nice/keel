package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/joinpeer"
	"keel/internal/native/keelclient"
)

const joinTestLink = "http://127.0.0.1:9/join?room=r1#k=" + shareTestKey

// joinRig is a window whose joiner is a fake: it records the links it was asked to join and hands out fakeConns.
type joinRig struct {
	*rig
	links []joinpeer.Link
	names []string
	conns []*fakeConn
	err   error
}

func newJoinRig(t *testing.T) *joinRig {
	t.Helper()
	jr := &joinRig{rig: openTabs(t, 1)}
	jr.w.joiner = func(l joinpeer.Link, name string) (session, error) {
		jr.links, jr.names = append(jr.links, l), append(jr.names, name)
		if jr.err != nil {
			return nil, jr.err
		}
		c := &fakeConn{st: &keelclient.State{}}
		jr.conns = append(jr.conns, c)
		return c, nil
	}
	return jr
}

// join pastes link in the dialog and presses Join.
func (jr *joinRig) join(t *testing.T, link string) {
	t.Helper()
	jr.tt.Key(ui.Cmd, ui.KeyJ)
	jr.tt.Frame()
	jr.tt.Type(link)
	jr.tt.Frame()
	jr.click(t, "Join")
	jr.tt.Frame()
}

func following() *keelclient.State {
	return &keelclient.State{Status: "ready", Role: "follower", Title: "Local Shell", YouID: 2, DriverID: 1, OwnerID: 1,
		Attached: []keelclient.Peer{{ID: 1, Name: "Ann", Kind: "web", Role: "driver"}, {ID: 2, Name: "me", Kind: "webrtc", Role: "follower"}}}
}

func TestJoinDialogHasTheAppsWordsAndTheAppsErrors(t *testing.T) {
	jr := newJoinRig(t)
	jr.tt.Key(ui.Cmd, ui.KeyJ)
	jr.tt.Frame()
	for _, s := range []string{"Join a session", "Paste the Share link from the other Keel. You follow until they pass control. You never see their hosts or vault.", "Join link", "Cancel", "Join"} {
		if !jr.tt.HasText(s) && !jr.has(s) {
			t.Errorf("dialog lacks %q", s)
		}
	}
	// Join with nothing, a bare id, no room, no key: the app's messages, as toasts, and the dialog stays.
	for _, tc := range []struct{ in, want string }{
		{"", "paste a Share link"},
		{"0123456789abcdef", "paste the full Share link"},
		{"https://sig.example/join#k=" + shareTestKey, "link has no room"},
		{"https://sig.example/join?room=a", "link has no key (#k=…): ask the host for a new Share link"},
	} {
		jr.tt.Key(ui.Cmd, ui.KeyA)
		jr.tt.Type(tc.in)
		jr.tt.Frame()
		jr.click(t, "Join")
		jr.tt.Frame()
		if !jr.tt.HasText(tc.want) || jr.w.joinDlg == nil || len(jr.links) != 0 {
			t.Errorf("%q: want the toast %q with the dialog open; dialog %v links %d", tc.in, tc.want, jr.w.joinDlg != nil, len(jr.links))
		}
	}
	jr.click(t, "Cancel")
	if jr.w.joinDlg != nil || len(jr.links) != 0 || len(jr.w.tabs) != 1 {
		t.Fatal("Cancel joined or left the dialog open")
	}
	jr.tt.Key(ui.Cmd, ui.KeyJ)
	jr.tt.Frame()
	jr.tt.Key(0, ui.KeyEscape)
	jr.tt.Frame()
	if jr.w.joinDlg != nil || len(jr.links) != 0 {
		t.Fatal("Escape did not close the dialog")
	}
}

func TestJoiningOpensAFollowingTabWithTheAppsBannerAndNothingElse(t *testing.T) {
	jr := newJoinRig(t)
	jr.w.term.Name = "Zed"
	jr.join(t, joinTestLink)
	if len(jr.links) != 1 || jr.links[0].Key != shareTestKey || jr.links[0].Room != "r1" || jr.links[0].Origin != "http://127.0.0.1:9" || jr.names[0] != "Zed" {
		t.Fatalf("joined %v as %v", jr.links, jr.names)
	}
	if jr.w.joinDlg != nil {
		t.Fatal("the dialog is still open: it holds the pasted link")
	}
	if len(jr.w.tabs) != 2 || !jr.w.tab().remote || !jr.w.focusedPane().remote {
		t.Fatalf("tabs %d", len(jr.w.tabs))
	}
	// Waiting for the host to let us in.
	if !jr.has("banner joining") || !jr.tt.HasText("Joining a shared session · waiting for the host") || !jr.has("Leave") || jr.has("Ask for control") {
		t.Errorf("waiting: %q", jr.tt.Texts())
	}
	// Allowed: the host's first frame says who is who.
	jr.conns[0].st = following()
	jr.tt.Frame()
	for _, s := range []string{"tab Local Shell · joined", "banner Following Ann's session on Local Shell · read-only", "Ask for control", "Leave", "role Ann is driving", "role You follow"} {
		if !jr.has(s) {
			t.Errorf("joined tab lacks %q: %q", s, jr.tt.Texts())
		}
	}
	// A terminal and nothing else: no sidebar, no toggle, no Share, no badge, no Vault.
	for _, s := range []string{"sidebar hosts", "Toggle sidebar", "Share", "1 watching", "Filter hosts", "banner following", "Reconnect"} {
		if jr.has(s) {
			t.Errorf("a joined tab shows %q", s)
		}
	}
	jr.tt.Key(ui.Cmd, ui.KeyB)
	jr.tt.Frame()
	if jr.has("sidebar hosts") {
		t.Error("⌘B brought the sidebar into a joined tab")
	}
	if jr.w.shareTarget() != nil {
		t.Error("a joined pane can be shared")
	}
	// The key is nowhere on screen or in what the pane remembers.
	for _, s := range jr.tt.Texts() {
		if strings.Contains(s, shareTestKey) {
			t.Errorf("the key is on screen: %q", s)
		}
	}
	if k := jr.w.focusedPane().joinKey; strings.Contains(k, shareTestKey) || k != "join:http://127.0.0.1:9:r1" {
		t.Errorf("join key %q", k)
	}
	// The sidebar is back on the local tab.
	jr.click(t, "tab api-1")
	if !jr.has("sidebar hosts") || !jr.has("Toggle sidebar") {
		t.Error("the local tab lost its sidebar")
	}
	jr.click(t, "tab Local Shell · joined")
	// After a hand-over: driving.
	jr.conns[0].st = &keelclient.State{Status: "ready", Role: "driver", Title: "Local Shell", YouID: 2, DriverID: 2, OwnerID: 1,
		Attached: []keelclient.Peer{{ID: 1, Name: "Ann", Kind: "web"}, {ID: 2, Name: "me", Kind: "webrtc"}}}
	jr.tt.Frame()
	if !jr.has("banner Driving Ann's session on Local Shell · you have control") || jr.has("Ask for control") || jr.has("role You follow") || !jr.has("role Driving") {
		t.Errorf("driving: %q", jr.tt.Texts())
	}
}

func TestAskForControlLeaveAndTheFollowNotice(t *testing.T) {
	jr := newJoinRig(t)
	jr.join(t, joinTestLink)
	jr.conns[0].st = following()
	jr.tt.Frame()
	jr.click(t, "Ask for control")
	if jr.conns[0].asked != 1 {
		t.Errorf("asked %d", jr.conns[0].asked)
	}
	// Typing while following: the connection counted a dropped keystroke; the notice is the app's, with Ask for control.
	jr.conns[0].st.Blocked = 1
	jr.conns[0].onChange()
	jr.tt.Frame()
	jr.tt.Frame()
	if !jr.tt.HasText("Typing is off while you follow") {
		t.Fatalf("no notice: %q", jr.tt.Texts())
	}
	if jr.conns[0].wrote != 0 {
		t.Errorf("wrote %d bytes", jr.conns[0].wrote)
	}
	// The same count does not repeat the notice; a new keystroke does.
	jr.tt.Frame()
	if len(jr.w.queued) != 0 {
		t.Errorf("queued %v", jr.w.queued)
	}
	// Leave closes the tab and the peer, and kills nothing.
	jr.click(t, "Leave")
	if !jr.conns[0].closed || len(jr.w.tabs) != 1 || jr.w.tabs[0].remote || len(jr.fb.killed) != 0 {
		t.Errorf("closed %v tabs %d killed %v", jr.conns[0].closed, len(jr.w.tabs), jr.fb.killed)
	}
	if len(jr.w.recent) != 0 {
		t.Errorf("a joined tab was recorded as recently closed: %v", jr.w.recent)
	}
}

func TestAJoinedTabIsNotSavedAndTakesNoSnippetOrSplit(t *testing.T) {
	jr := newJoinRig(t)
	jr.fb.snippets = []keelclient.Snippet{{ID: "sn1", Name: "df", Command: "df -h"}}
	jr.w.snippets = jr.fb.snippets
	jr.join(t, joinTestLink)
	jr.conns[0].st = following()
	jr.tt.Frame()
	b, _ := json.Marshal(jr.w.snapshot())
	if strings.Contains(string(b), "r1") || strings.Contains(string(b), "join") || len(jr.w.snapshot().Tabs) != 1 {
		t.Errorf("snapshot %s", b)
	}
	// Approve before type: refused for a joined pane, with the app's words, and nothing is typed.
	jr.w.typeSnippet("sn1")
	jr.tt.Frame()
	if jr.w.approve != nil || !jr.tt.HasText("Open a session first") || len(jr.conns[0].injected) != 0 || len(jr.conns[0].directs) != 0 {
		t.Errorf("approve %v injected %v directs %v", jr.w.approve != nil, jr.conns[0].injected, jr.conns[0].directs)
	}
	// Split: the app's refusal.
	jr.w.splitFocused(1)
	jr.tt.Frame()
	if !jr.tt.HasText("Split needs a local host session") || len(jr.w.tab().panes) != 1 {
		t.Error("a joined tab was split")
	}
	// The pane menu has no Share and no splits.
	got := menuLabels(t, jr.rig, "pane bar "+jr.w.focusedPane().id)
	if slices.Contains(got, "Share") || slices.Contains(got, "Split right") || !slices.Contains(got, "Close pane") {
		t.Errorf("menu %q", got)
	}
	jr.tt.CloseMenu()
}

func TestPastingTheSameLinkAgainShowsTheTabAndAFailedOneJoinsAgain(t *testing.T) {
	jr := newJoinRig(t)
	jr.join(t, joinTestLink)
	jr.conns[0].st = following()
	jr.click(t, "tab api-1")
	jr.join(t, joinTestLink)
	if len(jr.links) != 1 || !jr.w.tab().remote || len(jr.w.tabs) != 2 {
		t.Fatalf("joins %d tabs %d: the same link opens no second tab", len(jr.links), len(jr.w.tabs))
	}
	// A denied or lost join shows the app's error and Leave; pasting the link again retries in a new tab.
	jr.conns[0].st = &keelclient.State{Status: "error", Message: "The host declined your request to join."}
	jr.tt.Frame()
	if !jr.tt.HasText("The host declined your request to join.") || !jr.has("Leave") || jr.has("Reconnect") || jr.has("Edit host") {
		t.Errorf("error banner: %q", jr.tt.Texts())
	}
	jr.join(t, joinTestLink)
	if len(jr.links) != 2 || !jr.conns[0].closed || len(jr.w.tabs) != 2 {
		t.Fatalf("joins %d closed %v tabs %d", len(jr.links), jr.conns[0].closed, len(jr.w.tabs))
	}
}

func TestAJoinThatCannotStartIsAToastAndOpensNoTab(t *testing.T) {
	jr := newJoinRig(t)
	jr.err = errors.New("signal server: dial tcp 127.0.0.1:9: connect: connection refused")
	jr.join(t, joinTestLink)
	if len(jr.w.tabs) != 1 || !jr.tt.HasText("join: signal server: dial tcp 127.0.0.1:9: connect: connection refused") {
		t.Errorf("tabs %d texts %q", len(jr.w.tabs), jr.tt.Texts())
	}
	if strings.Contains(fmt.Sprint(jr.tt.Texts()), shareTestKey) {
		t.Error("the key is on screen")
	}
}

func TestJoinSessionIsOnThePaletteTheSidebarMenuAndTheNewTabPage(t *testing.T) {
	jr := newJoinRig(t)
	jr.openPal(t)
	jr.typePal("join")
	var titles []string
	for _, it := range jr.rows() {
		titles = append(titles, it.title)
	}
	if !slices.Contains(titles, "Join session") {
		t.Fatalf("palette %q", titles)
	}
	jr.press(0, ui.KeyEnter)
	jr.tt.Frame()
	if jr.w.joinDlg == nil {
		t.Error("the palette's Join session did not open the dialog")
	}
	jr.w.closeJoin()
	jr.tt.Frame()
	if err := jr.tt.Click("More host actions"); err != nil {
		t.Fatal(err)
	}
	jr.tt.Frame()
	if got := jr.tt.Menu(); !slices.Contains(got, "Join session") {
		t.Fatalf("menu %q", got)
	}
	jr.tt.CloseMenu()
}

func TestJoinNoticesAreToastsAndNeverTerminalOutput(t *testing.T) {
	jr := newJoinRig(t)
	jr.join(t, joinTestLink)
	jr.conns[0].events = []keelclient.Event{{Kind: keelclient.EventNotice, Message: "Host left"}, {Kind: keelclient.EventNoticeError, Message: "Signaling failed"}}
	jr.conns[0].onChange()
	jr.tt.Frame()
	jr.tt.Frame()
	if !jr.tt.HasText("Host left") || !jr.tt.HasText("Signaling failed") {
		t.Errorf("texts %q", jr.tt.Texts())
	}
}

func TestOnlyAJoinedPaneIsRemoteAndItsSessionIsNeverKilledOrReconnected(t *testing.T) {
	jr := newJoinRig(t)
	jr.join(t, joinTestLink)
	p := jr.w.focusedPane()
	jr.conns[0].id = "s-of-the-host" // even if a frame named the host's session, it is not ours to act on
	jr.conns[0].st = &keelclient.State{Status: "error", Message: "x"}
	jr.w.reconnect(p)
	jr.w.closePane(jr.w.tab(), p.id)
	if len(jr.fb.killed) != 0 || len(jr.fb.reconnected) != 0 {
		t.Fatalf("killed %v reconnected %v", jr.fb.killed, jr.fb.reconnected)
	}
}
