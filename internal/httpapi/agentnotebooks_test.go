package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"keel/internal/agent"
	"keel/internal/store"
)

// Notebooks and the agent (docs/ui-redesign.md §18) over the routes, with
// the scripted provider.

func (f *askFixture) callJSON(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	code, raw := f.call(t, method, path, body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return code, out
}

func TestAgentNotebookAttachAndDraftRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	// A client cannot mark its own notebook as the agent's.
	code, nb := f.callJSON(t, "POST", "/api/notebooks", map[string]any{"name": "Ops NB-NAME-ROUTE", "source": "agent", "cells": []map[string]any{{"body": "NB-BODY-ROUTE ssh with " + askHostPass + "\n" + askPaneToken}}})
	if code != http.StatusCreated || nb["source"] != nil {
		t.Fatalf("create → %d %v", code, nb)
	}
	nbID := nb["id"].(string)

	// Attached: sent redacted, traced, logged with the id and bytes only.
	code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "explain this notebook", "notebooks": []string{nbID}})
	if code != http.StatusOK {
		t.Fatalf("ask → %d %v", code, resp)
	}
	body := f.requests(t)
	for _, bad := range []string{askHostPass, "Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK"} {
		if strings.Contains(body, bad) {
			t.Errorf("provider request holds %q", bad)
		}
	}
	if !strings.Contains(body, "| NB-BODY-ROUTE") || !strings.Contains(resp["answer"].(string), "1 attached notebook") {
		t.Fatalf("the notebook was not sent: %v", resp["answer"])
	}
	trace := resp["trace"].([]any)
	st := trace[0].(map[string]any)
	if st["status"] != "notebook" || st["notebookId"] != nbID || st["notebook"] != "Ops NB-NAME-ROUTE" || st["bytes"].(float64) <= 0 {
		t.Fatalf("trace = %v", trace)
	}
	reads := f.logEntries(agent.EventRead)
	if len(reads) != 1 || reads[0].Source != "notebook" || !strings.Contains(reads[0].Message, nbID) {
		t.Fatalf("agent.read = %+v", reads)
	}

	// A draft: proposed, opened, saved by the user, marked from the agent.
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "write this up with a placeholder"})
	if code != http.StatusOK {
		t.Fatalf("ask → %d %v", code, resp)
	}
	props := resp["notebooks"].([]any)
	if len(props) != 1 {
		t.Fatalf("notebook proposals = %v", resp["notebooks"])
	}
	p := props[0].(map[string]any)
	if p["placeholders"] != true || !strings.Contains(p["body"].(string), "[secret:1]") {
		t.Fatalf("proposal = %v", p)
	}
	before, _ := f.st.ListNotebooks()
	code, d := f.callJSON(t, "POST", "/api/agent/conversations/"+askConv+"/notebooks/"+p["id"].(string)+"/draft", nil)
	if code != http.StatusOK || d["draftId"] == "" {
		t.Fatalf("open draft → %d %v", code, d)
	}
	if after, _ := f.st.ListNotebooks(); len(after) != len(before) {
		t.Fatal("opening a draft saved a notebook")
	}
	draft := d["draftId"].(string)
	code, saved := f.callJSON(t, "POST", "/api/agent/notebooks/drafts/"+draft, map[string]any{"name": d["name"], "cells": []map[string]any{{"body": d["body"]}}})
	if code != http.StatusOK {
		t.Fatalf("save → %d %v", code, saved)
	}
	snb := saved["notebook"].(map[string]any)
	if snb["source"] != store.NotebookSourceAgent || saved["proposal"].(map[string]any)["status"] != "saved" {
		t.Fatalf("saved = %v", saved)
	}
	if code, _ := f.callJSON(t, "POST", "/api/agent/notebooks/drafts/"+draft, map[string]any{"name": "again"}); code != http.StatusGone {
		t.Fatalf("second save → %d, want 410", code)
	}
	if code, _ := f.callJSON(t, "POST", "/api/agent/notebooks/drafts/nbd_madeup", map[string]any{"name": "x"}); code != http.StatusGone {
		t.Fatalf("made-up draft → %d, want 410", code)
	}
	if code, _ := f.callJSON(t, "POST", "/api/agent/conversations/"+askConv+"/notebooks/"+p["id"].(string)+"/draft", nil); code != http.StatusConflict {
		t.Fatalf("open a saved draft → %d, want 409", code)
	}
	// The user edits it with the plain notebook route: the mark stays.
	code, up := f.callJSON(t, "PUT", "/api/notebooks/"+snb["id"].(string), map[string]any{"name": "edited", "source": "", "cells": snb["cells"]})
	if code != http.StatusOK || up["source"] != store.NotebookSourceAgent {
		t.Fatalf("edit → %d %v", code, up)
	}
	// keel.log: the agent's entries hold ids and counts, never names or
	// text (the plain notebook routes' own "notebook created: <name>" is
	// the user's, as before); no entry holds a notebook's text.
	raw, _ := os.ReadFile(f.logPath)
	for _, bad := range []string{"NB-BODY-ROUTE", "What we did", askHostPass} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("keel.log holds %q", bad)
		}
	}
	for _, kind := range []string{agent.EventRead, agent.EventNotebook} {
		for _, e := range f.logEntries(kind) {
			if strings.Contains(e.Message, "NB-NAME-ROUTE") || strings.Contains(e.Message, "keel-notebook-e2e draft") {
				t.Errorf("%s entry holds a notebook name: %s", kind, e.Message)
			}
		}
	}
	if n := len(f.logEntries(agent.EventNotebook)); n != 3 {
		t.Fatalf("agent.notebook entries = %d, want 3 (proposed, opened, saved)", n)
	}
}

// A notebook fence holding a redaction placeholder is never typed: the
// session socket refuses a notebook-labelled inject that holds one, before
// the ticket writes anything.
func TestNotebookPlaceholderFenceRefused(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "ready\n")
	conn := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer conn.Close()
	waitJSON(t, conn, "ready")
	bridge := f.bridge(t, f.webID)
	got := readPTY(bridge)
	cmd := "export KEEL_TOKEN=[secret:1]\n"
	if err := conn.WriteJSON(map[string]any{"type": "inject-challenge", "command": cmd}); err != nil {
		t.Fatal(err)
	}
	nonce, _ := waitJSON(t, conn, "inject-challenge")["nonce"].(string)
	if err := conn.WriteJSON(map[string]any{"type": "inject", "command": cmd, "nonce": nonce, "notebook": true}); err != nil {
		t.Fatal(err)
	}
	if r := waitJSON(t, conn, "error"); !strings.Contains(r["message"].(string), "placeholder") {
		t.Fatalf("placeholder fence → %v", r)
	}
	select {
	case leak := <-got:
		t.Fatalf("a placeholder reached the PTY: %q", leak)
	case <-time.After(150 * time.Millisecond):
	}
}
