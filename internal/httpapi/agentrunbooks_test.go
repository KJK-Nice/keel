package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/store"
)

// The runbook routes end to end with the scripted provider (docs/ui-
// redesign.md §16): a runbook saved through the API is redacted and kept
// encrypted; asking with it proposes step 1 only; the step is typed only
// through the session socket's approve-before-type path; the exit status
// the pane's shell reports with OSC 133 marks is recorded for the step;
// nothing skips ahead; agent.runbook entries hold ids and counts, never
// the runbook's text.
func TestAgentRunbookRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ uptime\n")

	in := service.RunbookInput{
		Name: "RUNBOOK-NAME-MARKER", When: "502s on the web tier", AgentMayPick: true,
		Steps: []store.RunbookStep{
			{Title: "STEP-ONE check", Command: "echo step-one"},
			{Title: "STEP-TWO login", Command: "login --password " + askHostPass},
			{Title: "STEP-THREE look"},
		},
	}
	code, b := f.call(t, "POST", "/api/agent/runbooks", in)
	var saved service.SavedRunbook
	if code != 200 || json.Unmarshal(b, &saved) != nil || saved.Redacted != 1 || strings.Contains(string(b), askHostPass) {
		t.Fatalf("add → %d %s", code, b)
	}
	blob, _ := os.ReadFile(filepath.Join(filepath.Dir(f.logPath), "keel.memory"))
	if len(blob) == 0 || bytes.Contains(blob, []byte("RUNBOOK-NAME-MARKER")) {
		t.Fatal("keel.memory missing or not encrypted")
	}
	if code, b := f.call(t, "GET", "/api/agent/runbooks", nil); code != 200 || !bytes.Contains(b, []byte("RUNBOOK-NAME-MARKER")) {
		t.Fatalf("list → %d %s", code, b)
	}
	if code, _ := f.call(t, "POST", "/api/agent/runbooks", map[string]any{"name": "", "steps": []any{}}); code != 400 {
		t.Fatalf("empty runbook → %d", code)
	}

	// Asked with the runbook: one proposal, step 1, from the saved command.
	code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "walk me through it", "panes": []string{web.ID}, "runbookId": saved.Runbook.ID})
	if code != 200 {
		t.Fatalf("ask → %d %v", code, resp)
	}
	props := proposalsOf(t, resp)
	if len(props) != 1 || props[0]["command"] != "echo step-one" || props[0]["runbook"].(map[string]any)["step"] != float64(1) {
		t.Fatalf("proposals %v", props)
	}
	if !strings.Contains(resp["answer"].(string), "step 1 is current") {
		t.Fatalf("answer %v", resp["answer"])
	}
	if tr := resp["trace"].([]any)[0].(map[string]any); tr["status"] != "runbook" || tr["runbook"] != "RUNBOOK-NAME-MARKER" {
		t.Fatalf("trace %v", resp["trace"])
	}
	stepIDs := []string{}
	for _, st := range resp["runbook"].(map[string]any)["steps"].([]any) {
		stepIDs = append(stepIDs, st.(map[string]any)["id"].(string))
	}
	// Skipping ahead is refused.
	if code, _ := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/runbook/steps/"+stepIDs[2], map[string]string{"action": "done"}); code != http.StatusConflict {
		t.Fatalf("skip ahead → %d", code)
	}

	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	bridge := f.bridge(t, f.webID)
	got := readPTY(bridge)
	// Other bytes than the step's: refused, nothing written.
	if r := typeOverSocket(t, driver, "echo other\n", props[0]["id"].(string), false); r["ok"] != false {
		t.Fatalf("changed bytes → %v", r)
	}
	if r := typeOverSocket(t, driver, props[0]["bytes"].(string), props[0]["id"].(string), false); r["ok"] != true {
		t.Fatalf("type → %v", r)
	}
	select {
	case hit := <-got:
		if hit != "\x15echo step-one\n" {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the approved step never reached the PTY")
	}
	// The shell reports the command with OSC 133 marks.
	go func() {
		_, _ = bridge.outW.Write([]byte("echo step-one\r\n\x1b]133;C\x07step-one\r\n\x1b]133;D;0\x07$ "))
	}()
	var state service.RunState
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, b := f.call(t, "GET", "/api/agent/conversations/"+askConv+"/runbook", nil)
		state = service.RunState{}
		_ = json.Unmarshal(b, &state)
		if state.Runbook != nil && state.Runbook.Steps[0].State == "done" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s0 := state.Runbook.Steps[0]
	if s0.State != "done" || s0.Result != "reported" || s0.Exit == nil || *s0.Exit != 0 || s0.TypedAt == nil {
		t.Fatalf("step 1 %+v", s0)
	}

	// The next ask proposes step 2, whose saved command was redacted.
	_, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "next", "panes": []string{web.ID}})
	props = proposalsOf(t, resp)
	if len(props) != 1 || props[0]["runbook"].(map[string]any)["step"] != float64(2) || !strings.Contains(props[0]["command"].(string), "[secret:") || props[0]["placeholders"] != true {
		t.Fatalf("step 2 %v", props)
	}
	if code, b := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/runbook/steps/"+stepIDs[1], map[string]string{"action": "skip"}); code != 200 || !bytes.Contains(b, []byte(`"state":"skipped"`)) {
		t.Fatalf("skip → %d %s", code, b)
	}
	if code, b := f.call(t, "DELETE", "/api/agent/conversations/"+askConv+"/runbook", nil); code != 200 || !bytes.Contains(b, []byte(`"noRunbook":true`)) {
		t.Fatalf("don't use → %d %s", code, b)
	}

	// A draft from this conversation: its typed proposal is a step.
	code, b = f.call(t, "POST", "/api/agent/runbooks/draft", map[string]string{"conversationId": askConv})
	if code != 200 || !bytes.Contains(b, []byte("echo step-one")) || !bytes.Contains(b, []byte(`"source":"conversation"`)) {
		t.Fatalf("draft → %d %s", code, b)
	}

	if code, _ := f.call(t, "PUT", "/api/agent/runbooks/"+saved.Runbook.ID+"/pick", map[string]bool{"agentMayPick": false}); code != 200 {
		t.Fatalf("pick switch → %d", code)
	}
	if code, _ := f.call(t, "DELETE", "/api/agent/runbooks/"+saved.Runbook.ID, nil); code != 204 {
		t.Fatalf("delete → %d", code)
	}
	if code, _ := f.call(t, "DELETE", "/api/agent/runbooks/"+saved.Runbook.ID, nil); code != 404 {
		t.Fatalf("delete twice → %d", code)
	}

	entries := f.logEntries(agent.EventRunbook)
	if len(entries) < 6 {
		t.Fatalf("agent.runbook entries %+v", entries)
	}
	for _, e := range entries {
		for _, bad := range []string{"RUNBOOK-NAME-MARKER", "STEP-", "echo step-one", askHostPass} {
			if strings.Contains(e.Message, bad) {
				t.Fatalf("log %q holds %q", e.Message, bad)
			}
		}
	}
	all, _ := json.Marshal(entries)
	if !bytes.Contains(all, []byte("exit 0 reported by the shell")) {
		t.Fatalf("no result logged: %s", all)
	}
}

// A runbook the agent proposes is kept only on Save, with Agent may pick
// off.
func TestAgentRunbookProposalRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ uptime\n")
	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "save this as a runbook", "panes": []string{web.ID}})
	list, _ := resp["runbooks"].([]any)
	if len(list) != 1 {
		t.Fatalf("runbook proposals %v", resp["runbooks"])
	}
	rp := list[0].(map[string]any)
	if books, _ := f.st.ListRunbooks(); len(books) != 0 {
		t.Fatal("kept before Save")
	}
	id := rp["id"].(string)
	if code, b := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/runbooks/"+id, nil); code != 200 || !bytes.Contains(b, []byte(`"status":"saved"`)) {
		t.Fatalf("save → %d %s", code, b)
	}
	if code, _ := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/runbooks/"+id, nil); code != http.StatusConflict {
		t.Fatalf("save twice → %d", code)
	}
	books, _ := f.st.ListRunbooks()
	if len(books) != 1 || books[0].Source != store.RunbookSourceAgent || books[0].AgentMayPick || !books[0].FromPane {
		t.Fatalf("kept %+v", books)
	}
	if code, _ := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/runbooks/"+id+"/undo", nil); code != 200 {
		t.Fatalf("undo → %d", code)
	}
	if books, _ := f.st.ListRunbooks(); len(books) != 0 {
		t.Fatal("Undo kept the runbook")
	}
	if code, b := f.call(t, "DELETE", "/api/agent/conversations/"+askConv+"/runbooks/"+id, nil); code != 200 || !bytes.Contains(b, []byte(`"status":"dismissed"`)) {
		t.Fatalf("dismiss → %d %s", code, b)
	}
}
