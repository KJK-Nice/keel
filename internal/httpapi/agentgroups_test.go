package httpapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/store"
)

func typeGroupOverSocket(t *testing.T, conn *websocket.Conn, bytes, runID, proposalID string, shift bool) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "inject-challenge", "command": bytes}); err != nil {
		t.Fatal(err)
	}
	chal := waitJSON(t, conn, "inject-challenge")
	nonce, _ := chal["nonce"].(string)
	if err := conn.WriteJSON(map[string]any{"type": "inject", "command": bytes, "nonce": nonce, "agent": map[string]any{"groupRunId": runID, "proposalId": proposalID, "shift": shift}}); err != nil {
		t.Fatal(err)
	}
	return waitJSON(t, conn, "agent-type")
}

// Groups, part 1 end to end (docs/ui-redesign.md §17): roles through the
// API; a group note sent with a pane read on a member host; a group run
// whose step goes to each readable member with a pane, never to the member
// with Agent can read off; each host's step typed only over the session
// socket with its own nonce and proposal; its exit status from OSC 133;
// agent.runbook entries with ids and counts only.
func TestAgentGroupRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	g, err := f.st.UpsertGroup(store.Group{Name: "GROUP-NAME-MARKER"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.AssignHostsGroup([]string{f.webID, f.prodID, f.offID}, g.ID); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{f.webID, f.prodID, f.offID} {
		if code, b := f.call(t, "PUT", "/api/groups/"+g.ID+"/roles", map[string]string{"hostId": h, "role": "worker"}); code != 200 {
			t.Fatalf("role → %d %s", code, b)
		}
	}
	if code, _ := f.call(t, "PUT", "/api/groups/"+g.ID+"/roles", map[string]string{"hostId": f.webID, "role": "two words"}); code != 400 {
		t.Fatalf("bad role → %d", code)
	}
	if code, b := f.call(t, "GET", "/api/groups/"+g.ID+"/members", nil); code != 200 || bytes.Count(b, []byte(`"role":"worker"`)) != 3 {
		t.Fatalf("members → %d %s", code, b)
	}
	if code, b := f.call(t, "POST", "/api/agent/notes", map[string]string{"groupId": g.ID, "text": "GROUP-NOTE-MARKER drain before restart, pw " + askHostPass}); code != 200 || strings.Contains(string(b), askHostPass) {
		t.Fatalf("group note → %d %s", code, b)
	}
	if code, _ := f.call(t, "POST", "/api/agent/notes", map[string]string{"groupId": g.ID, "hostId": f.webID, "text": "x"}); code != 400 {
		t.Fatalf("host and group → %d", code)
	}

	web := f.open(t, f.webID, "$ uptime\n")
	prod := f.open(t, f.prodID, "$ uptime\n")
	off := f.open(t, f.offID, "$ uptime\n")

	// The group note goes with a read of a member's pane.
	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "hi", "panes": []string{web.ID}})
	reqs := f.fake.Requests()
	last := ""
	for _, m := range reqs[len(reqs)-1].Messages {
		last += m.Text.String()
	}
	if !strings.Contains(last, "GROUP-NOTE-MARKER") || strings.Contains(last, askHostPass) || !strings.Contains(last, "role of web-1 in this group: worker") {
		t.Fatalf("group note not sent: %v", resp["trace"])
	}

	code, b := f.call(t, "POST", "/api/agent/runbooks", service.RunbookInput{Name: "RUNBOOK-NAME-MARKER", Steps: []store.RunbookStep{{Title: "STEP-TITLE", Command: "echo kubelet-ok", Role: "worker"}}})
	var saved service.SavedRunbook
	if code != 200 || json.Unmarshal(b, &saved) != nil {
		t.Fatalf("runbook → %d %s", code, b)
	}
	code, b = f.call(t, "POST", "/api/agent/groupruns", map[string]string{"runbookId": saved.Runbook.ID, "groupId": g.ID, "mode": "parallel"})
	var run service.GroupRun
	if code != 200 || json.Unmarshal(b, &run) != nil {
		t.Fatalf("start → %d %s", code, b)
	}
	code, b = f.call(t, "POST", "/api/agent/groupruns/"+run.ID+"/propose", map[string]any{"panes": []string{web.ID, prod.ID, off.ID}})
	run = service.GroupRun{}
	if code != 200 || json.Unmarshal(b, &run) != nil {
		t.Fatalf("propose → %d %s", code, b)
	}
	props := map[string]*service.Proposal{}
	for _, c := range run.Steps[0].Cells {
		if c.HostID == f.offID {
			t.Fatal("the unreadable member is a target")
		}
		if c.Proposal != nil {
			props[c.HostID] = c.Proposal
		}
	}
	if len(props) != 2 || props[f.webID].SessionID != web.ID || props[f.prodID].SessionID != prod.ID || props[f.webID].ID == props[f.prodID].ID {
		t.Fatalf("proposals %+v", props)
	}

	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	bridge := f.bridge(t, f.webID)
	got := readPTY(bridge)
	// prod's proposal cannot be typed into web's pane.
	if r := typeGroupOverSocket(t, driver, props[f.prodID].Bytes, run.ID, props[f.prodID].ID, true); r["ok"] != false {
		t.Fatalf("other host's proposal → %v", r)
	}
	if r := typeGroupOverSocket(t, driver, props[f.webID].Bytes, run.ID, props[f.webID].ID, false); r["ok"] != true {
		t.Fatalf("type → %v", r)
	}
	select {
	case hit := <-got:
		if hit != "\x15echo kubelet-ok\n" {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the approved step never reached the PTY")
	}
	// Typed once: a second type is refused.
	if r := typeGroupOverSocket(t, driver, props[f.webID].Bytes, run.ID, props[f.webID].ID, false); r["ok"] != false {
		t.Fatalf("typed twice → %v", r)
	}
	go func() {
		_, _ = bridge.outW.Write([]byte("echo kubelet-ok\r\n\x1b]133;C\x07kubelet-ok\r\n\x1b]133;D;0\x07$ "))
	}()
	deadline := time.Now().Add(3 * time.Second)
	var cell service.GroupRunCell
	for time.Now().Before(deadline) {
		_, b := f.call(t, "GET", "/api/agent/groupruns/"+run.ID, nil)
		run = service.GroupRun{}
		_ = json.Unmarshal(b, &run)
		for _, c := range run.Steps[0].Cells {
			if c.HostID == f.webID {
				cell = c
			}
		}
		if cell.State == "done" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cell.State != "done" || cell.Result != "reported" || cell.Exit == nil || *cell.Exit != 0 {
		t.Fatalf("web cell %+v", cell)
	}
	if code, _ := f.call(t, "POST", "/api/agent/groupruns/"+run.ID+"/steps/"+run.Steps[0].ID, map[string]string{"hostId": f.prodID, "action": "skip"}); code != 200 {
		t.Fatalf("skip → %d", code)
	}
	if code, b := f.call(t, "GET", "/api/agent/groupruns/"+run.ID, nil); code != 200 || !bytes.Contains(b, []byte(`"status":"finished"`)) {
		t.Fatalf("finished → %d %s", code, b)
	}
	if code, _ := f.call(t, "GET", "/api/agent/groupruns/gr_nope", nil); code != 404 {
		t.Fatalf("unknown run → %d", code)
	}
	for _, e := range append(f.logEntries(agent.EventRunbook), f.logEntries(agent.EventNote)...) {
		for _, bad := range []string{"RUNBOOK-NAME-MARKER", "STEP-TITLE", "kubelet", "GROUP-NAME-MARKER", "GROUP-NOTE-MARKER", askHostPass} {
			if strings.Contains(e.Message, bad) {
				t.Fatalf("log %q holds %q", e.Message, bad)
			}
		}
	}
	types := f.logEntries(agent.EventType)
	if len(types) != 1 || types[0].SessionID != web.ID {
		t.Fatalf("agent.type %+v", types)
	}
}
