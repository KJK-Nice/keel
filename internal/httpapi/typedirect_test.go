package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/applog"
	"keel/internal/store"
)

// Type without the approve dialog (docs/ui-redesign.md §6 rule 13, for
// review), over the real session socket: keel serve decides, on the exact
// bytes, at the moment of the write; it never adds a newline; anything it
// cannot call a read-only line goes to the dialog.

// directOverSocket runs the page's direct Type: a server nonce for the
// bytes, then an inject marked direct. It returns the type-direct answer.
func directOverSocket(t *testing.T, conn *websocket.Conn, command string, direct, agentPart map[string]any) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": "inject-challenge", "command": command}); err != nil {
		t.Fatal(err)
	}
	chal := waitJSON(t, conn, "inject-challenge")
	msg := map[string]any{"type": "inject", "command": command, "nonce": chal["nonce"], "direct": direct}
	if agentPart != nil {
		msg["agent"] = agentPart
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}
	return waitJSON(t, conn, "type-direct")
}

// checkOverSocket asks type-check: would it go without the dialog now.
func checkOverSocket(t *testing.T, conn *websocket.Conn, command string, direct, agentPart map[string]any) map[string]any {
	t.Helper()
	msg := map[string]any{"type": "type-check", "command": command, "direct": direct}
	if agentPart != nil {
		msg["agent"] = agentPart
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}
	return waitJSON(t, conn, "type-check")
}

func agentDirect() map[string]any { return map[string]any{"kind": "agent", "ref": "r1"} }

func convPart(id string) map[string]any {
	return map[string]any{"conversationId": askConv, "proposalId": id}
}

// wantDialog fails unless the answer sends the user to the dialog.
func wantDialog(t *testing.T, what string, r map[string]any, reason string) {
	t.Helper()
	if r["ok"] != false || r["dialog"] != true || !strings.Contains(r["reason"].(string), reason) {
		t.Fatalf("%s → %v, want the dialog (%s)", what, r, reason)
	}
}

// nothingWritten fails if the PTY got bytes within a short wait.
func nothingWritten(t *testing.T, got <-chan string, what string) {
	t.Helper()
	select {
	case leak := <-got:
		t.Fatalf("%s reached the PTY: %q", what, leak)
	case <-time.After(120 * time.Millisecond):
	}
}

func written(t *testing.T, got <-chan string, want, what string) {
	t.Helper()
	select {
	case hit := <-got:
		if hit != want {
			t.Fatalf("%s: PTY got %q, want %q", what, hit, want)
		}
		if strings.ContainsAny(hit, "\r\n") {
			t.Fatalf("%s: a direct type sent Enter: %q", what, hit)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s never reached the PTY", what)
	}
}

func (f *askFixture) entriesFrom(kind, source string) []applog.Entry {
	var out []applog.Entry
	for _, e := range f.logEntries(kind) {
		if e.Source == source {
			out = append(out, e)
		}
	}
	return out
}

func TestDirectTypeAgentProposal(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ ")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	follower := dialSession(t, f.srv.URL, web.ID, "other", "web")
	defer follower.Close()
	waitJSON(t, follower, "ready")
	bridge := f.bridge(t, f.webID)

	_, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "check", "panes": []string{web.ID}})
	ro := proposalsOf(t, resp)[0]
	_, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "touch the marker", "panes": []string{web.ID}})
	ch := proposalsOf(t, resp)[0]
	if ro["risk"] != "readonly" || ch["risk"] != "changes" {
		t.Fatalf("proposals %v %v", ro, ch)
	}
	roID, chID := ro["id"].(string), ch["id"].(string)

	// type-check says so and writes and logs nothing.
	got := readPTY(bridge)
	if r := checkOverSocket(t, driver, "echo keel-agent-ok", agentDirect(), convPart(roID)); r["direct"] != true || r["ref"] != "r1" {
		t.Fatalf("type-check read-only → %v", r)
	}
	if r := checkOverSocket(t, driver, "touch /tmp/keel-agent-e2e-touched", agentDirect(), convPart(chID)); r["direct"] != false || r["dialog"] != true {
		t.Fatalf("type-check changes → %v", r)
	}
	nothingWritten(t, got, "a type-check")
	if n := len(f.logEntries(agent.EventType)); n != 0 {
		t.Fatalf("type-check logged %d agent.type", n)
	}

	// Not read-only: the dialog decides, nothing is written.
	wantDialog(t, "changes-state proposal", directOverSocket(t, driver, "touch /tmp/keel-agent-e2e-touched", agentDirect(), convPart(chID)), service.ReasonRisk)
	// The dialog's bytes (^U … newline) hold control characters.
	wantDialog(t, "the dialog's bytes", directOverSocket(t, driver, ro["bytes"].(string), agentDirect(), convPart(roID)), service.ReasonLines)
	// Bytes other than the proposal's command.
	wantDialog(t, "other bytes", directOverSocket(t, driver, "echo other", agentDirect(), convPart(roID)), service.ReasonBytes)
	// A follower gets no nonce, and its direct inject is refused, not
	// offered the dialog.
	if err := follower.WriteJSON(map[string]any{"type": "inject", "command": "echo keel-agent-ok", "nonce": "", "direct": agentDirect(), "agent": convPart(roID)}); err != nil {
		t.Fatal(err)
	}
	if r := waitJSON(t, follower, "type-direct"); r["ok"] != false || r["dialog"] == true || !strings.Contains(r["message"].(string), "follow") {
		t.Fatalf("follower → %v", r)
	}
	if r := checkOverSocket(t, follower, "echo keel-agent-ok", agentDirect(), convPart(roID)); r["direct"] != false {
		t.Fatalf("follower type-check → %v", r)
	}
	nothingWritten(t, got, "a refused direct type")

	// A full-screen program on the alternate screen: keystrokes, not a
	// command line. Back on the normal screen it goes.
	alt := func(seq string) {
		go func() { _, _ = bridge.outW.Write([]byte(seq)) }()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			sess, _ := f.rt.Get(web.ID)
			if sess.AltScreen() == strings.HasSuffix(seq, "h") {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("alt screen never followed %q", seq)
	}
	alt("\x1b[?1049h")
	wantDialog(t, "alternate screen", directOverSocket(t, driver, "echo keel-agent-ok", agentDirect(), convPart(roID)), "full-screen")
	nothingWritten(t, got, "a type into vim")
	alt("\x1b[?1049l")

	// Read-only and one line: typed without ^U and without Enter.
	if r := directOverSocket(t, driver, "echo keel-agent-ok", agentDirect(), convPart(roID)); r["ok"] != true {
		t.Fatalf("direct read-only → %v", r)
	}
	written(t, got, "echo keel-agent-ok", "the direct type")
	typed := f.entriesFrom(agent.EventType, agent.TypeDirect)
	if len(typed) != 1 || typed[0].Bytes != len("echo keel-agent-ok") || typed[0].Lines != 1 || typed[0].SessionID != web.ID || strings.Contains(typed[0].Message, "keel-agent-ok") {
		t.Fatalf("agent.type direct = %+v", typed)
	}
	if n := len(f.logEntries(agent.EventType)); n != 1 {
		t.Fatalf("agent.type entries = %d", n)
	}

	// The dialog path is unchanged, and now says how it was approved.
	got = readPTY(bridge)
	if r := typeOverSocket(t, driver, ch["bytes"].(string), chID, false); r["ok"] != true {
		t.Fatalf("approved type → %v", r)
	}
	written2 := <-got
	if written2 != "\x15touch /tmp/keel-agent-e2e-touched\n" {
		t.Fatalf("approved PTY got %q", written2)
	}
	if n := len(f.entriesFrom(agent.EventType, agent.TypeApproved)); n != 1 {
		t.Fatalf("agent.type approved = %d", n)
	}

	// An edit of two lines: the dialog. One holding a placeholder: refused.
	edit := func(cmd string) {
		code, b := f.call(t, "PUT", "/api/agent/conversations/"+askConv+"/proposals/"+roID, map[string]string{"command": cmd})
		if code != 200 {
			t.Fatalf("edit → %d %s", code, b)
		}
	}
	edit("echo one\necho two")
	got = readPTY(bridge)
	wantDialog(t, "two lines", directOverSocket(t, driver, "echo one\necho two", agentDirect(), convPart(roID)), service.ReasonLines)
	edit("echo [secret:1]")
	if r := directOverSocket(t, driver, "echo [secret:1]", agentDirect(), convPart(roID)); r["ok"] != false || r["dialog"] == true || !strings.Contains(r["message"].(string), "placeholder") {
		t.Fatalf("placeholder → %v", r)
	}
	nothingWritten(t, got, "a refused edit")
}

func TestDirectTypeSnippetsAreRatedServerSide(t *testing.T) {
	f := newAskFixture(t, true)
	if err := f.st.SetAgentKey("jev", "jev-test-key-0123"); err != nil {
		t.Fatal(err)
	}
	web := f.open(t, f.webID, "$ ")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	bridge := f.bridge(t, f.webID)
	snip := func(cmd string, usable bool) string {
		sn, err := f.st.UpsertSnippet(store.Snippet{Name: "s", Command: cmd, AgentUsable: usable})
		if err != nil {
			t.Fatal(err)
		}
		return sn.ID
	}
	dir := func(id string) map[string]any { return map[string]any{"kind": "snippet", "snippetId": id, "ref": "s"} }

	// Read-only by the code rule, not agent-usable: typed without its
	// newline, and its body never went to Jev (§6 rule 3).
	up := snip("uptime\n", false)
	got := readPTY(bridge)
	if r := directOverSocket(t, driver, "uptime", dir(up), nil); r["ok"] != true {
		t.Fatalf("uptime → %v", r)
	}
	written(t, got, "uptime", "a read-only snippet")
	if n := len(f.judge.Requests()); n != 0 {
		t.Fatalf("a snippet not marked agent-usable went to Jev (%d requests)", n)
	}
	logged := f.entriesFrom("snippet", agent.TypeDirect)
	if len(logged) != 1 || logged[0].Bytes != 6 || !strings.Contains(logged[0].Message, up) || strings.Contains(logged[0].Message, "uptime") {
		t.Fatalf("snippet log = %+v", logged)
	}

	// Agent-usable: Jev may make it stricter (never looser), and is logged.
	stricter := snip("echo keel-jev-stricter", true)
	got = readPTY(bridge)
	wantDialog(t, "Jev says changes", directOverSocket(t, driver, "echo keel-jev-stricter", dir(stricter), nil), service.ReasonRisk)
	if n := len(f.judge.Requests()); n != 1 {
		t.Fatalf("Jev requests = %d", n)
	}
	sent := false
	for _, e := range f.logEntries(agent.EventRead) {
		sent = sent || strings.Contains(e.Message, "Jev")
	}
	if !sent {
		t.Fatal("the rating's egress was not logged")
	}
	nothingWritten(t, got, "a snippet Jev rated stricter")

	// Jev unavailable (no key): the code rule decides. It calls echo
	// read-only, and anything it cannot call read-only goes to the dialog.
	if err := f.st.ClearAgentKey("jev"); err != nil {
		t.Fatal(err)
	}
	if r := directOverSocket(t, driver, "echo keel-jev-stricter", dir(stricter), nil); r["ok"] != true {
		t.Fatalf("no Jev, read-only by the code rule → %v", r)
	}
	written(t, got, "echo keel-jev-stricter", "a snippet rated by the code rule alone")
	got = readPTY(bridge)
	unknown := snip("frobnicate --all", true)
	wantDialog(t, "a command the code rule does not know", directOverSocket(t, driver, "frobnicate --all", dir(unknown), nil), service.ReasonRisk)
	touch := snip("touch /tmp/x", false)
	wantDialog(t, "changes state", directOverSocket(t, driver, "touch /tmp/x", dir(touch), nil), service.ReasonRisk)

	// Two lines, a placeholder, other bytes, an unknown snippet: dialog.
	two := snip("uptime\ndf -h\n", false)
	wantDialog(t, "two lines", directOverSocket(t, driver, "uptime\ndf -h", dir(two), nil), service.ReasonLines)
	ph := snip("echo [secret:1]", false)
	wantDialog(t, "placeholder", directOverSocket(t, driver, "echo [secret:1]", dir(ph), nil), service.ReasonLines)
	wantDialog(t, "other bytes", directOverSocket(t, driver, "df -h", dir(up), nil), service.ReasonBytes)
	wantDialog(t, "unknown snippet", directOverSocket(t, driver, "uptime", dir("sn_nope"), nil), service.ReasonBytes)
	wantDialog(t, "tab completion", directOverSocket(t, driver, "upt\t", dir(up), nil), service.ReasonLines)
	nothingWritten(t, got, "a snippet sent to the dialog")
}

func TestDirectTypeNotebookBlocks(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ ")
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	bridge := f.bridge(t, f.webID)
	nb, err := f.st.UpsertNotebook(store.Notebook{Name: "ops", Cells: []store.NotebookCell{{Body: "Check disk:\n\n```sh\ndf -h\n```\n\n```sh\necho a\necho b\n```\n\n```sh\nls [secret:1]\n```\n\n```sh\nrm -rf /tmp/x\n```"}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := map[string]any{"kind": "notebook", "notebookId": nb.ID, "ref": "n"}

	got := readPTY(bridge)
	wantDialog(t, "a block of two lines", directOverSocket(t, driver, "echo a\necho b", dir, nil), service.ReasonLines)
	wantDialog(t, "destructive", directOverSocket(t, driver, "rm -rf /tmp/x", dir, nil), service.ReasonRisk)
	wantDialog(t, "not in the notebook", directOverSocket(t, driver, "uptime", dir, nil), service.ReasonBytes)
	if r := directOverSocket(t, driver, "ls [secret:1]", dir, nil); r["ok"] != false || r["dialog"] == true {
		t.Fatalf("placeholder → %v", r)
	}
	nothingWritten(t, got, "a notebook block sent to the dialog")

	if r := directOverSocket(t, driver, "df -h", dir, nil); r["ok"] != true {
		t.Fatalf("df -h → %v", r)
	}
	written(t, got, "df -h", "a read-only notebook block")
	if n := len(f.entriesFrom("notebook", agent.TypeDirect)); n != 1 {
		t.Fatalf("notebook direct log = %d", n)
	}

	// An agent edit the user has not reviewed: the dialog says so (§6
	// rule 12), so it decides.
	if _, _, err := f.st.AgentEditNotebook(nb.ID, nb.Rev, "edit", func(n store.Notebook) (store.Notebook, error) {
		n.Cells[0].Body += "\n"
		return n, nil
	}); err != nil {
		t.Fatal(err)
	}
	got = readPTY(bridge)
	wantDialog(t, "unreviewed agent edit", directOverSocket(t, driver, "df -h", dir, nil), "not reviewed")
	nothingWritten(t, got, "a block of an unreviewed notebook")
}

// A group run: one click types a read-only step into every host's pane,
// each with its own nonce on its own socket and its own check, and no
// Enter. A step whose values are filled in is checked on the filled
// bytes; anything else keeps the group approve dialog.
func TestDirectTypeGroupRun(t *testing.T) {
	f := newAskFixture(t, true)
	g, err := f.st.UpsertGroup(store.Group{Name: "fleet"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.AssignHostsGroup([]string{f.webID, f.prodID}, g.ID); err != nil {
		t.Fatal(err)
	}
	if code, b := f.call(t, "PUT", "/api/groups/"+g.ID+"/vars", map[string]string{"name": "ver", "value": "1.31.2"}); code != 200 {
		t.Fatalf("var → %d %s", code, b)
	}
	code, b := f.call(t, "POST", "/api/agent/runbooks", service.RunbookInput{Name: "rb", Steps: []store.RunbookStep{
		{Title: "check", Command: "echo kubelet-ok {{ver}}"},
		{Title: "restart", Command: "systemctl restart kubelet"},
	}})
	var saved service.SavedRunbook
	if code != 200 || json.Unmarshal(b, &saved) != nil {
		t.Fatalf("runbook → %d %s", code, b)
	}
	code, b = f.call(t, "POST", "/api/agent/groupruns", map[string]string{"runbookId": saved.Runbook.ID, "groupId": g.ID, "mode": "parallel"})
	var run service.GroupRun
	if code != 200 || json.Unmarshal(b, &run) != nil {
		t.Fatalf("start → %d %s", code, b)
	}
	web := f.open(t, f.webID, "$ ")
	prod := f.open(t, f.prodID, "$ ")
	propose := func() map[string]*service.Proposal {
		t.Helper()
		_, b := f.call(t, "POST", "/api/agent/groupruns/"+run.ID+"/propose", map[string]any{"panes": []string{web.ID, prod.ID}})
		run = service.GroupRun{}
		if err := json.Unmarshal(b, &run); err != nil {
			t.Fatal(err)
		}
		out := map[string]*service.Proposal{}
		for _, c := range run.Steps[run.Current].Cells {
			if c.Proposal != nil && c.State == "proposed" {
				out[c.HostID] = c.Proposal
			}
		}
		return out
	}
	props := propose()
	if len(props) != 2 {
		t.Fatalf("proposals %+v", props)
	}
	conns := map[string]*websocket.Conn{}
	for host, sess := range map[string]string{f.webID: web.ID, f.prodID: prod.ID} {
		c := dialSession(t, f.srv.URL, sess, "me", "web")
		defer c.Close()
		waitJSON(t, c, "ready")
		conns[host] = c
	}
	part := func(p *service.Proposal) map[string]any {
		return map[string]any{"groupRunId": run.ID, "proposalId": p.ID}
	}
	filled := "echo kubelet-ok 1.31.2"

	// The saved command (with {{ver}}) is not the bytes typed: dialog.
	gotWeb := readPTY(f.bridge(t, f.webID))
	gotProd := readPTY(f.bridge(t, f.prodID))
	wantDialog(t, "unfilled", directOverSocket(t, conns[f.webID], "echo kubelet-ok {{ver}}", agentDirect(), part(props[f.webID])), service.ReasonBytes)
	// Another host's proposal in this pane: refused.
	if r := checkOverSocket(t, conns[f.webID], filled, agentDirect(), part(props[f.prodID])); r["direct"] != false || r["dialog"] == true {
		t.Fatalf("other host's proposal → %v", r)
	}
	// Every host is checked first (nothing written), then each is typed.
	for host, p := range props {
		if r := checkOverSocket(t, conns[host], filled, agentDirect(), part(p)); r["direct"] != true {
			t.Fatalf("type-check %s → %v", host, r)
		}
	}
	nothingWritten(t, gotWeb, "a group type-check")
	// The value changed after the check: the old bytes are not the bytes
	// filled in now (rule 11), so the dialog decides.
	if code, b := f.call(t, "PUT", "/api/groups/"+g.ID+"/vars", map[string]string{"name": "ver", "value": "1.32.0"}); code != 200 {
		t.Fatalf("var → %d %s", code, b)
	}
	wantDialog(t, "stale value", directOverSocket(t, conns[f.webID], filled, agentDirect(), part(props[f.webID])), service.ReasonBytes)
	filled = "echo kubelet-ok 1.32.0"
	for host, p := range props {
		if r := directOverSocket(t, conns[host], filled, agentDirect(), part(p)); r["ok"] != true {
			t.Fatalf("direct %s → %v", host, r)
		}
	}
	written(t, gotWeb, filled, "web-1's step")
	written(t, gotProd, filled, "prod-db-01's step (read-only goes direct on production too)")
	direct := f.entriesFrom(agent.EventType, agent.TypeDirect)
	if len(direct) != 2 || direct[0].SessionID == direct[1].SessionID {
		t.Fatalf("one agent.type per host: %+v", direct)
	}
	for _, e := range direct {
		if strings.Contains(e.Message, "1.32.0") || strings.Contains(e.Message, "kubelet") {
			t.Fatalf("log holds the command: %q", e.Message)
		}
	}
	// Typed once: the host is no longer proposed.
	if r := directOverSocket(t, conns[f.webID], filled, agentDirect(), part(props[f.webID])); r["ok"] != false {
		t.Fatalf("typed twice → %v", r)
	}

	// The next step changes state: the dialog decides, on every host.
	for _, host := range []string{f.webID, f.prodID} {
		if code, _ := f.call(t, "POST", "/api/agent/groupruns/"+run.ID+"/steps/"+run.Steps[0].ID, map[string]string{"hostId": host, "action": "done"}); code != 200 {
			t.Fatalf("done %s → %d", host, code)
		}
	}
	props = propose()
	if len(props) != 2 {
		t.Fatalf("step 2 proposals %+v", props)
	}
	for host, p := range props {
		wantDialog(t, "systemctl restart on "+host, directOverSocket(t, conns[host], "systemctl restart kubelet", agentDirect(), part(p)), service.ReasonRisk)
	}
}
