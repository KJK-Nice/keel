package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/agent"
	"keel/internal/agent/service"
)

// The third review of #95, end to end over the session socket: the lines
// it found typed without the dialog now go to it, a Jev call that got no
// answer fails closed, and agent.type is logged only after the write.

func TestDirectTypeReview95Refuses(t *testing.T) {
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
// the page: rule 13 says joined panes never take a direct type.
func TestDirectTypeRefusesAJoinedDriver(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ ")
	owner := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer owner.Close()
	waitJSON(t, owner, "ready")
	peer := dialSession(t, f.srv.URL, web.ID, "peer", "webrtc")
	defer peer.Close()
	ready := waitJSON(t, peer, "ready")
	target := uint64(ready["you"].(map[string]any)["id"].(float64))
	if err := owner.WriteJSON(map[string]any{"type": "grant-control", "targetId": target}); err != nil {
		t.Fatal(err)
	}
	waitJSON(t, peer, "control")
	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	id := proposalsOf(t, resp)[0]["id"].(string)
	got := readPTY(f.bridge(t, f.webID))
	if r := directOverSocket(t, peer, "echo keel-agent-ok", agentDirect(), convPart(id)); r["ok"] != false || r["dialog"] == true || !strings.Contains(r["message"].(string), "joined") {
		t.Fatalf("a joined driver → %v", r)
	}
	nothingWritten(t, got, "a direct type from a share peer")
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
