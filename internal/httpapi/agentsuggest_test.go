package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/agent"
)

func sendJSON(t *testing.T, conn *websocket.Conn, v map[string]any) {
	t.Helper()
	if err := conn.WriteJSON(v); err != nil {
		t.Fatal(err)
	}
}

// insertOverSocket runs ⇥ on a suggestion: challenge, then inject with
// the suggestion marker. It returns the agent-insert reply.
func insertOverSocket(t *testing.T, conn *websocket.Conn, bytes, suggestionID string, option int, shift bool) map[string]any {
	t.Helper()
	sendJSON(t, conn, map[string]any{"type": "inject-challenge", "command": bytes})
	chal := waitJSON(t, conn, "inject-challenge")
	sendJSON(t, conn, map[string]any{"type": "inject", "command": bytes, "nonce": chal["nonce"], "suggestion": map[string]any{"suggestionId": suggestionID, "option": option, "shift": shift}})
	return waitJSON(t, conn, "agent-insert")
}

func noPTY(t *testing.T, got <-chan string, what string) {
	t.Helper()
	select {
	case leak := <-got:
		t.Fatalf("%s reached the PTY: %q", what, leak)
	case <-time.After(150 * time.Millisecond):
	}
}

// §5.5 end to end over the real session socket: Notice (detect), Suggest,
// Insert (not run, logged), the ⇧ rule for destructive, and §6.6: a
// follower gets no notice, no suggestion and cannot insert.
func TestSuggestOverSessionSocket(t *testing.T) {
	f := newAskFixture(t, true)
	if err := f.st.SetAgentKey("jev", "jev-test-key-0123"); err != nil {
		t.Fatal(err)
	}
	web := f.open(t, f.webID, "ready\n")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	follower := dialSession(t, f.srv.URL, web.ID, "other", "web")
	defer follower.Close()
	waitJSON(t, follower, "ready")
	bridge := f.bridge(t, f.webID)

	// Notice: words are plain, a command is not, #-lines always are.
	sendJSON(t, driver, map[string]any{"type": "agent-detect", "seq": 1, "line": "show the files here"})
	if r := waitJSON(t, driver, "agent-detect"); r["plain"] != true || r["source"] != "jev" || r["seq"] != float64(1) {
		t.Fatalf("detect words → %v", r)
	}
	sendJSON(t, driver, map[string]any{"type": "agent-detect", "seq": 2, "line": "ls -la /tmp"})
	if r := waitJSON(t, driver, "agent-detect"); r["plain"] != false {
		t.Fatalf("detect command → %v", r)
	}
	// Jev offline: silent fallback.
	sendJSON(t, driver, map[string]any{"type": "agent-detect", "seq": 3, "line": "jev-offline words here"})
	if r := waitJSON(t, driver, "agent-detect"); r["plain"] != false || r["source"] != "fallback" {
		t.Fatalf("detect offline → %v", r)
	}
	// Follower: nothing, and Jev is not asked.
	n := len(f.judge.Requests())
	sendJSON(t, follower, map[string]any{"type": "agent-detect", "seq": 4, "line": "# show the files here"})
	if r := waitJSON(t, follower, "agent-detect"); r["plain"] != false || r["source"] != "off" {
		t.Fatalf("follower detect → %v", r)
	}
	sendJSON(t, follower, map[string]any{"type": "agent-suggest", "seq": 5, "line": "show the files here"})
	if r := waitJSON(t, follower, "agent-suggest"); r["ok"] != false || r["reason"] != "following" {
		t.Fatalf("follower suggest → %v", r)
	}
	if len(f.judge.Requests()) != n {
		t.Fatal("a follower's line reached Jev")
	}

	// Suggest (⇥ on the notice).
	sendJSON(t, driver, map[string]any{"type": "agent-suggest", "seq": 6, "line": "show the files here"})
	r := waitJSON(t, driver, "agent-suggest")
	if r["ok"] != true {
		t.Fatalf("suggest → %v", r)
	}
	sug := r["suggestion"].(map[string]any)
	opts := sug["options"].([]any)
	first := opts[0].(map[string]any)
	if len(opts) != 3 || first["command"] != "echo keel-suggest-ok" || first["bytes"] != "\x15echo keel-suggest-ok" || first["source"] != "generated" {
		t.Fatalf("suggestion = %v", sug)
	}
	id := sug["id"].(string)

	// A follower cannot insert it.
	got := readPTY(bridge)
	sendJSON(t, follower, map[string]any{"type": "inject", "command": first["bytes"], "nonce": "", "suggestion": map[string]any{"suggestionId": id}})
	if r := waitJSON(t, follower, "agent-insert"); r["ok"] != false || !strings.Contains(r["message"].(string), "follower") {
		t.Fatalf("follower insert → %v", r)
	}
	noPTY(t, got, "a follower's insert")

	// Insert: the bytes, no newline; logged with the source.
	if r := insertOverSocket(t, driver, first["bytes"].(string), id, 0, false); r["ok"] != true {
		t.Fatalf("insert → %v", r)
	}
	select {
	case hit := <-got:
		if hit != "\x15echo keel-suggest-ok" {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the insert never reached the PTY")
	}
	ins := f.logEntries(agent.EventInsert)
	if len(ins) != 1 || ins[0].Source != "generated" || ins[0].SessionID != web.ID {
		t.Fatalf("agent.insert = %+v", ins)
	}
	if sugs := f.logEntries(agent.EventSuggest); len(sugs) != 1 || sugs[0].Source != "generated" {
		t.Fatalf("agent.suggest = %+v", sugs)
	}

	// Destructive: plain ⇥ refused before the write, ⇧⇥ inserts.
	sendJSON(t, driver, map[string]any{"type": "agent-suggest", "seq": 7, "line": "clean up old files"})
	r = waitJSON(t, driver, "agent-suggest")
	de := r["suggestion"].(map[string]any)
	deOpt := de["options"].([]any)[0].(map[string]any)
	if deOpt["risk"] != "destructive" || deOpt["needsShift"] != true {
		t.Fatalf("destructive = %v", deOpt)
	}
	got = readPTY(bridge)
	if r := insertOverSocket(t, driver, deOpt["bytes"].(string), de["id"].(string), 0, false); r["ok"] != false || !strings.Contains(r["message"].(string), "⇧⇥") {
		t.Fatalf("unshifted destructive → %v", r)
	}
	noPTY(t, got, "an unshifted destructive insert")
	if r := insertOverSocket(t, driver, deOpt["bytes"].(string), de["id"].(string), 0, true); r["ok"] != true {
		t.Fatalf("shifted destructive → %v", r)
	}
	select {
	case hit := <-got:
		if hit != "\x15rm -rf /tmp/keel-suggest-e2e" {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the shifted insert never reached the PTY")
	}
	// keel.log never holds the words or the command.
	for _, e := range f.logs.Recent(200) {
		if strings.Contains(e.Message, "files here") || strings.Contains(e.Message, "keel-suggest") {
			t.Fatalf("keel.log holds content: %q", e.Message)
		}
	}
	// The lines sent to Jev are logged (the first of a window at once).
	reads := 0
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		reads = 0
		for _, e := range f.logEntries(agent.EventRead) {
			if e.Source == "jev" {
				reads += e.Count
			}
		}
		if reads >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if reads < 1 {
		t.Fatal("no agent.read for the prompt lines sent to Jev")
	}
}

// §6.1 at the source: the suggestion file cannot write to a session.
func TestAgentSuggestFileHasNoWritePath(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "agentsuggest.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{"Write": true, "Inject": true, "InjectTicket": true, "IssueInjectTicket": true, "Keystroke": true, "Spawn": true, "Resize": true, "SetFocus": true, "Kill": true}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && banned[sel.Sel.Name] {
			t.Errorf("agentsuggest.go uses .%s", sel.Sel.Name)
		}
		return true
	})
}
