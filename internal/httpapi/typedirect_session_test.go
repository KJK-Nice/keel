package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/runtime"
	"keel/internal/wire"
)

// Over the session socket: lines that need the approve dialog go to it, a Jev call that got no
// answer fails closed, a joined driver is refused, and agent.type is logged only after the write.

func TestDirectTypeRefusesLinesThatNeedTheDialog(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ ")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	bridge := f.bridge(t, f.webID)
	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	id := proposalsOf(t, resp)[0]["id"].(string)
	got := readPTY(bridge)
	for _, poc := range []string{
		`kubectl -n get delete ns prod`,
		`kubectl --namespace get delete ns prod`,
		`kubectl -n get exec x -- touch /tmp/a`,
		`systemctl --no-pager -p status stop sshd`,
		`systemctl --no-pager --property status restart sshd`,
		`echo '\e]0;x\a'`,
		`echo '\033]52;c;aGk=\007'`,
		`ps auxe`,
		`cat /proc/self/environ`,
		`cat /home/u/.bash_history`,
		`grep -r PRIVATE /home/u`,
		`lsof +r 1`,
	} {
		if code, b := f.call(t, "PUT", "/api/agent/conversations/"+askConv+"/proposals/"+id, map[string]string{"command": poc}); code != 200 {
			t.Fatalf("edit %q → %d %s", poc, code, b)
		}
		wantDialog(t, poc, directOverSocket(t, driver, poc, agentDirect(), convPart(id)), service.ReasonRisk)
	}
	nothingWritten(t, got, "a review line")
}

// A Jev key is set and the call is made, but no answer comes (here, a
// timeout): the dialog decides.
func TestDirectTypeJevNoAnswerFailsClosed(t *testing.T) {
	f := newAskFixture(t, true)
	if err := f.st.SetAgentKey("jev", "jev-test-key-0123"); err != nil {
		t.Fatal(err)
	}
	f.judge.SetSlowDelay(5 * time.Second)
	web := f.open(t, f.webID, "$ ")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	id := proposalsOf(t, resp)[0]["id"].(string)
	if code, _ := f.call(t, "PUT", "/api/agent/conversations/"+askConv+"/proposals/"+id, map[string]string{"command": "ls keel-jev-slow"}); code != 200 {
		t.Fatal("edit")
	}
	got := readPTY(f.bridge(t, f.webID))
	r := directAnswerWithin(t, driver, "ls keel-jev-slow", agentDirect(), convPart(id), 6*time.Second)
	wantDialog(t, "Jev timed out", r, service.ReasonJevNone)
	nothingWritten(t, got, "a type Jev never answered")
}

// directAnswerWithin is directOverSocket with a longer wait for the answer.
func directAnswerWithin(t *testing.T, conn *websocket.Conn, command string, direct, agentPart map[string]any, wait time.Duration) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "inject-challenge", "command": command}); err != nil {
		t.Fatal(err)
	}
	chal := waitJSON(t, conn, "inject-challenge")
	if err := conn.WriteJSON(map[string]any{"type": "inject", "command": command, "nonce": chal["nonce"], "direct": direct, "agent": agentPart}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		var body map[string]any
		if err := conn.ReadJSON(&body); err != nil {
			t.Fatal(err)
		}
		if body["type"] == "type-direct" {
			return body
		}
	}
	t.Fatal("no type-direct answer")
	return nil
}

// A pane a share peer drives is refused on the server too, not only in
// the window: rule 13 says joined panes never take a direct type. A joined
// peer is one that came in over a share's data channel: the WebSocket makes
// no joined client, whatever kind it is asked for; the session issues a
// joined driver no inject nonce; and the direct type's own check refuses a
// pane a joined peer drives. (That the data channel refuses every inject
// frame is internal/sessionsock's and internal/share's test.)
func TestDirectTypeRefusesAJoinedDriver(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ ")
	owner := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer owner.Close()
	waitJSON(t, owner, "ready")
	// kind=webrtc on the WebSocket is still the window.
	claimed := dialSession(t, f.srv.URL, web.ID, "claims", wire.KindJoined)
	if you := waitJSON(t, claimed, "ready")["you"].(map[string]any); you["kind"] != wire.KindWindow {
		t.Fatalf("a WebSocket client asked to be %q and is %v", wire.KindJoined, you)
	}
	claimed.Close()

	sess, err := f.rt.Get(web.ID)
	if err != nil {
		t.Fatal(err)
	}
	peer, _, _ := sess.Subscribe(runtime.Join{Name: "peer", Kind: wire.KindJoined})
	defer sess.Unsubscribe(peer.ID)
	if err := owner.WriteJSON(map[string]any{"type": "grant-control", "targetId": peer.ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sess.Role(peer.ID) != runtime.RoleDriver && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !sess.DrivenByJoined() {
		t.Fatal("the joined peer was not handed control")
	}
	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	id := proposalsOf(t, resp)[0]["id"].(string)
	got := readPTY(f.bridge(t, f.webID))
	const cmd = "echo keel-agent-ok"
	if nonce, err := sess.IssueInjectTicket(peer.ID, cmd); !errors.Is(err, runtime.ErrNotLocal) || nonce != "" {
		t.Fatalf("a joined driver's nonce → %q, %v", nonce, err)
	}
	if err := sess.InjectTicket(peer.ID, cmd, ""); err == nil {
		t.Fatal("a joined driver's inject with no nonce was written")
	}
	_, err = f.server.directCheck(context.Background(), sess, peer.ID, cmd, &wire.DirectType{Ref: "r1", Kind: wire.DirectAgent}, &wire.AgentType{ConversationID: askConv, ProposalID: id})
	if !errors.Is(err, errJoined) {
		t.Fatalf("a joined driver → %v", err)
	}
	if _, dialog := service.NeedsDialog(err); dialog {
		t.Fatal("a joined driver was offered the dialog")
	}
	// The owner no longer drives: it gets no nonce, and its direct type is
	// refused as a follower's.
	if err := owner.WriteJSON(map[string]any{"type": "inject", "command": cmd, "nonce": "", "direct": agentDirect(), "agent": convPart(id)}); err != nil {
		t.Fatal(err)
	}
	if r := waitJSON(t, owner, "type-direct"); r["ok"] != false || r["dialog"] == true || !strings.Contains(r["message"].(string), "follow") {
		t.Fatalf("the owner, while a joined peer drives → %v", r)
	}
	nothingWritten(t, got, "a direct type into a pane a share peer drives")
}

// A direct type whose write fails (a spent nonce) is not logged as typed.
func TestDirectTypeLogsOnlyAfterTheWrite(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ ")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	id := proposalsOf(t, resp)[0]["id"].(string)
	if code, _ := f.call(t, "PUT", "/api/agent/conversations/"+askConv+"/proposals/"+id, map[string]string{"command": "uptime"}); code != 200 {
		t.Fatal("edit")
	}
	if err := driver.WriteJSON(map[string]any{"type": "inject", "command": "uptime", "nonce": "not-a-nonce", "direct": agentDirect(), "agent": convPart(id)}); err != nil {
		t.Fatal(err)
	}
	if r := waitJSON(t, driver, "type-direct"); r["ok"] != false || r["dialog"] == true {
		t.Fatalf("a spent nonce → %v", r)
	}
	if n := len(f.logEntries(agent.EventType)); n != 0 {
		t.Fatalf("agent.type logged for a write that failed: %d", n)
	}
	got := readPTY(f.bridge(t, f.webID))
	if r := directOverSocket(t, driver, "uptime", agentDirect(), convPart(id)); r["ok"] != true {
		t.Fatalf("direct → %v", r)
	}
	written(t, got, "uptime", "the direct type")
	if n := len(f.entriesFrom(agent.EventType, agent.TypeDirect)); n != 1 {
		t.Fatalf("agent.type after the write = %d", n)
	}
}
