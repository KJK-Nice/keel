package native

import (
	"slices"
	"testing"

	"keel/internal/native/keelclient"
)

// Issue #160: Reconnect on a pane whose socket was lost opens a new connection to the session and swaps it in.
func TestReconnectAfterALostSocketAttachesANewConnection(t *testing.T) {
	r := failedRig(t, keelclient.State{Status: "error", WasReady: true, SocketLost: true, Message: "The connection to keel serve was lost"}, "h-api")
	old := r.fb.conns[0]
	r.fb.sessions = []keelclient.Session{{ID: "s-x", HostID: "h-api"}}
	r.click(t, "Reconnect")
	if !slices.Equal(r.fb.attached, []string{"s-x", "s-x"}) {
		t.Fatalf("attached %v: the lost socket was not re-attached", r.fb.attached)
	}
	p := r.w.tabAt(0).panes[r.w.tabAt(0).focused]
	if p.conn == session(old) || !old.closed || r.has("banner failed") {
		t.Errorf("the pane still holds the dead connection (closed %v, banner %v)", old.closed, r.has("banner failed"))
	}
}

func TestReconnectAfterALostSocketSaysWhenTheSessionIsGone(t *testing.T) {
	r := failedRig(t, keelclient.State{Status: "error", WasReady: true, SocketLost: true, Message: "The connection to keel serve was lost"}, "h-api")
	r.fb.sessions = nil
	r.click(t, "Reconnect")
	if len(r.fb.attached) != 1 || !r.tt.HasText("This session no longer exists on keel serve") {
		t.Errorf("attached %v, no message", r.fb.attached)
	}
}

func TestAFailedConnectStillAsksKeelServeToReconnect(t *testing.T) {
	r := failedRig(t, keelclient.State{Status: "error", Message: "refused"}, "h-api")
	r.click(t, "Reconnect")
	if !slices.Equal(r.fb.reconnected, []string{"s-x"}) || len(r.fb.attached) != 1 {
		t.Errorf("reconnected %v attached %v", r.fb.reconnected, r.fb.attached)
	}
}
