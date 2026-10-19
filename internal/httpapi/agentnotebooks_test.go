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

// Notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12) over the
// routes, with the scripted provider.

func (f *askFixture) callJSON(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	code, raw := f.call(t, method, path, body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return code, out
}

func (f *askFixture) notebookText(t *testing.T, id string) string {
	t.Helper()
	nb, err := f.st.GetNotebook(id)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, c := range nb.Cells {
		parts = append(parts, c.Body)
	}
	return strings.Join(parts, "\n\n")
}

func cards(t *testing.T, resp map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	list, _ := resp["notebooks"].([]any)
	for _, c := range list {
		out = append(out, c.(map[string]any))
	}
	return out
}

// Private is the user's switch: a client's notebook body cannot set or
// clear it, only PUT …/private can; the agent never sees a private
// notebook (not in the index, not read, not attached, not edited).
func TestNotebookPrivateRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	code, nb := f.callJSON(t, "POST", "/api/notebooks", map[string]any{"name": "keel-private PRIV-NAME-ROUTE", "private": true, "cells": []map[string]any{{"body": "PRIV-BODY-ROUTE keel-edit-me"}}})
	if code != http.StatusCreated || nb["private"] != nil {
		t.Fatalf("create → %d %v", code, nb)
	}
	id := nb["id"].(string)
	if code, out := f.callJSON(t, "PUT", "/api/notebooks/"+id+"/private", map[string]any{"private": true}); code != http.StatusOK || out["private"] != true {
		t.Fatalf("private → %d %v", code, out)
	}
	// A stale editor's autosave (no private in the body) keeps it private.
	if code, out := f.callJSON(t, "PUT", "/api/notebooks/"+id, map[string]any{"name": "keel-private PRIV-NAME-ROUTE", "private": false, "rev": 1, "cells": []map[string]any{{"body": "PRIV-BODY-ROUTE keel-edit-me"}}}); code != http.StatusOK || out["private"] != true {
		t.Fatalf("save → %d %v", code, out)
	}
	for _, q := range []string{"read the notebook keel-private priv-name-route", "edit the notebook keel-private priv-name-route"} {
		code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": q, "notebooks": []string{id}})
		if code != http.StatusOK {
			t.Fatalf("ask → %d %v", code, resp)
		}
		if len(cards(t, resp)) != 0 {
			t.Fatalf("cards = %v", resp["notebooks"])
		}
		trace := resp["trace"].([]any)
		if st := trace[0].(map[string]any); st["reason"] != "notebook-private" {
			t.Fatalf("trace = %v", trace)
		}
	}
	body := f.requests(t)
	if strings.Contains(body, "PRIV-NAME-ROUTE") || strings.Contains(body, "PRIV-BODY-ROUTE") {
		t.Fatal("a private notebook reached the provider")
	}
	if !strings.Contains(f.notebookText(t, id), "keel-edit-me") {
		t.Fatal("a private notebook was changed")
	}
	// The user's own routes log a notebook's name as before; nothing the
	// agent logged names it, and no entry holds its text.
	raw, _ := os.ReadFile(f.logPath)
	if strings.Contains(string(raw), "PRIV-BODY") {
		t.Error("keel.log holds a private notebook's text")
	}
	for _, kind := range []string{agent.EventRead, agent.EventNotebook} {
		for _, e := range f.logEntries(kind) {
			if strings.Contains(e.Message, id) || strings.Contains(e.Message, "PRIV-") {
				t.Errorf("%s entry names the private notebook: %s", kind, e.Message)
			}
		}
	}
}

// The agent reads a notebook on its own (no attachment), edits it at once
// with history, the card's Undo restores it, History lists and restores
// versions, a user save from an older revision keeps the user's text and
// says the agent's edit is in History; a delete waits for the user's
// confirm, goes to Recently deleted and comes back. keel.log holds ids and
// counts only.
func TestAgentNotebookReadEditDeleteRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	code, nb := f.callJSON(t, "POST", "/api/notebooks", map[string]any{"name": "keel-ops NB-NAME-ROUTE", "source": "agent", "cells": []map[string]any{{"body": "NB-BODY-ROUTE keel-edit-me ssh with " + askHostPass + "\n" + askPaneToken}}})
	if code != http.StatusCreated || nb["source"] != nil || nb["rev"].(float64) != 1 {
		t.Fatalf("create → %d %v", code, nb)
	}
	id := nb["id"].(string)

	// Read on its own: two provider calls, the trace says the agent asked.
	code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "read the notebook keel-ops nb-name-route"})
	if code != http.StatusOK {
		t.Fatalf("ask → %d %v", code, resp)
	}
	if !strings.Contains(resp["answer"].(string), "I read 1 notebook I asked for") {
		t.Fatalf("answer = %v", resp["answer"])
	}
	body := f.requests(t)
	for _, bad := range []string{askHostPass, "Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK"} {
		if strings.Contains(body, bad) {
			t.Errorf("provider request holds %q", bad)
		}
	}
	asked := false
	for _, s := range resp["trace"].([]any) {
		st := s.(map[string]any)
		if st["status"] == "notebook" && st["asked"] == true && st["notebookId"] == id {
			asked = true
		}
	}
	if !asked {
		t.Fatalf("trace = %v", resp["trace"])
	}

	// Edit: applied at once, the card can undo it.
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "edit the notebook keel-ops nb-name-route"})
	if code != http.StatusOK || len(cards(t, resp)) != 1 {
		t.Fatalf("ask → %d %v", code, resp)
	}
	c := cards(t, resp)[0]
	if c["status"] != "edited" || c["record"] == nil {
		t.Fatalf("card = %v", c)
	}
	if got := f.notebookText(t, id); !strings.Contains(got, "keel-edited-by-agent") || !strings.Contains(got, askHostPass) {
		t.Fatalf("notebook = %q", got)
	}
	code, act := f.callJSON(t, "POST", "/api/agent/conversations/"+askConv+"/notebooks/"+c["id"].(string)+"/undo", nil)
	if code != http.StatusOK || act["proposal"].(map[string]any)["status"] != "undone" {
		t.Fatalf("undo → %d %v", code, act)
	}
	if got := f.notebookText(t, id); strings.Contains(got, "keel-edited-by-agent") || !strings.Contains(got, "keel-edit-me") {
		t.Fatalf("after undo %q", got)
	}
	if code, _ := f.callJSON(t, "POST", "/api/agent/conversations/"+askConv+"/notebooks/"+c["id"].(string)+"/undo", nil); code != http.StatusConflict {
		t.Fatalf("undo twice → %d", code)
	}

	// History: the version before the edit, and before the undo.
	code, hist := f.callJSON(t, "GET", "/api/notebooks/"+id+"/history", nil)
	vs, _ := hist["versions"].([]any)
	if code != http.StatusOK || len(vs) != 2 {
		t.Fatalf("history → %d %v", code, hist)
	}
	agentV := vs[0].(map[string]any)
	if agentV["by"] != "agent" || agentV["cells"] != nil {
		t.Fatalf("newest version = %v", agentV)
	}
	code, v := f.callJSON(t, "GET", "/api/notebooks/"+id+"/history/"+agentV["id"].(string), nil)
	if code != http.StatusOK || !strings.Contains(v["cells"].([]any)[0].(map[string]any)["body"].(string), "keel-edited-by-agent") {
		t.Fatalf("version → %d %v", code, v)
	}
	cur, _ := f.st.GetNotebook(id)
	if code, _ := f.callJSON(t, "POST", "/api/notebooks/"+id+"/history/"+agentV["id"].(string)+"/restore", map[string]any{"rev": cur.Rev - 1}); code != http.StatusConflict {
		t.Fatalf("restore at a stale rev → %d", code)
	}
	if code, out := f.callJSON(t, "POST", "/api/notebooks/"+id+"/history/"+agentV["id"].(string)+"/restore", map[string]any{"rev": cur.Rev}); code != http.StatusOK || out["lastBy"] != nil {
		t.Fatalf("restore → %d %v", code, out)
	}

	// The agent edits again while the editor still shows an older rev: the
	// user's save wins, and says so.
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "edit the notebook keel-ops nb-name-route"})
	if code != http.StatusOK || cards(t, resp)[0]["status"] != "refused" { // the text holds no keel-edit-me now
		t.Fatalf("ask → %d %v", code, resp["notebooks"])
	}
	cur, _ = f.st.GetNotebook(id)
	cur.Cells = []store.NotebookCell{{Body: "keel-edit-me again"}}
	if _, err := f.st.UpsertNotebook(cur); err != nil {
		t.Fatal(err)
	}
	cur, _ = f.st.GetNotebook(id)
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "edit the notebook keel-ops nb-name-route"})
	if code != http.StatusOK || cards(t, resp)[0]["status"] != "edited" {
		t.Fatalf("ask → %d %v", code, resp["notebooks"])
	}
	code, saved := f.callJSON(t, "PUT", "/api/notebooks/"+id, map[string]any{"name": "keel-ops NB-NAME-ROUTE", "rev": cur.Rev, "cells": []map[string]any{{"body": "keel-edit-me again, typed"}}})
	if code != http.StatusOK || saved["replacedAgentEdit"] != true || saved["lastBy"] != nil {
		t.Fatalf("stale save → %d %v", code, saved)
	}
	if got := f.notebookText(t, id); got != "keel-edit-me again, typed" {
		t.Fatalf("the user's typing was lost: %q", got)
	}

	// Delete: proposed, nothing deleted until the confirm route.
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "drop the notebook keel-ops nb-name-route"})
	if code != http.StatusOK || len(cards(t, resp)) != 1 {
		t.Fatalf("ask → %d %v", code, resp)
	}
	d := cards(t, resp)[0]
	if d["kind"] != "delete" || d["status"] != "proposed" {
		t.Fatalf("card = %v", d)
	}
	if _, err := f.st.GetNotebook(id); err != nil {
		t.Fatalf("deleted without the user: %v", err)
	}
	code, act = f.callJSON(t, "POST", "/api/agent/conversations/"+askConv+"/notebooks/"+d["id"].(string)+"/delete", nil)
	if code != http.StatusOK || act["proposal"].(map[string]any)["status"] != "deleted" {
		t.Fatalf("confirm → %d %v", code, act)
	}
	if _, err := f.st.GetNotebook(id); err == nil {
		t.Fatal("not deleted")
	}
	code, trash := f.callJSON(t, "GET", "/api/notebook-trash", nil)
	if tv, _ := trash["versions"].([]any); code != http.StatusOK || len(tv) != 1 {
		t.Fatalf("trash → %d %v", code, trash)
	}
	code, act = f.callJSON(t, "POST", "/api/agent/conversations/"+askConv+"/notebooks/"+d["id"].(string)+"/restore", nil)
	if code != http.StatusOK || act["notebook"].(map[string]any)["id"] != id {
		t.Fatalf("restore → %d %v", code, act)
	}

	// The create route (write this up) still makes a notebook marked from
	// the agent, at once.
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "write this up with a placeholder"})
	if code != http.StatusOK || len(cards(t, resp)) != 1 || cards(t, resp)[0]["status"] != "created" {
		t.Fatalf("ask → %d %v", code, resp["notebooks"])
	}
	created, _ := f.st.GetNotebook(cards(t, resp)[0]["notebookId"].(string))
	if created.Source != store.NotebookSourceAgent {
		t.Fatalf("created = %+v", created)
	}

	// keel.log: ids and counts, never names or text.
	raw, _ := os.ReadFile(f.logPath)
	for _, bad := range []string{"NB-BODY-ROUTE", "What we did", askHostPass, "keel-edit"} {
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
}

// Recently deleted lists a notebook the user deleted, and restores it.
func TestNotebookTrashRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	_, nb := f.callJSON(t, "POST", "/api/notebooks", map[string]any{"name": "keel-trash", "cells": []map[string]any{{"body": "x"}}})
	id := nb["id"].(string)
	if code, _ := f.call(t, "DELETE", "/api/notebooks/"+id, nil); code != http.StatusNoContent {
		t.Fatalf("delete → %d", code)
	}
	_, trash := f.callJSON(t, "GET", "/api/notebook-trash", nil)
	tv := trash["versions"].([]any)
	vid := tv[0].(map[string]any)["id"].(string)
	if code, out := f.callJSON(t, "POST", "/api/notebook-trash/"+vid+"/restore", nil); code != http.StatusOK || out["id"] != id {
		t.Fatalf("restore → %d %v", code, out)
	}
	if code, _ := f.callJSON(t, "POST", "/api/notebook-trash/"+vid+"/restore", nil); code != http.StatusNotFound {
		t.Fatalf("restore twice → %d", code)
	}
	_ = time.Second
}

// Every response restricts images to keel serve itself and data:/blob:
// URLs, so a markdown image in a notebook cannot make the webview fetch a
// URL of its author's choosing (§18). Only img-src: Datastar evaluates its
// expressions, so scripts are not restricted here.
func TestImagePolicyHeader(t *testing.T) {
	f := newAskFixture(t, true)
	for _, path := range []string{"/", "/api/health", "/api/notebooks", "/js/term.js"} {
		res, err := http.Get(f.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Content-Security-Policy"); got != "img-src 'self' data: blob:" {
			t.Errorf("GET %s: Content-Security-Policy = %q", path, got)
		}
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

// Rename and pin are metadata only, so a stale window cannot write old
// text back over an agent edit; Recently deleted shows a deleted notebook's
// text, and versions and deleted notebooks can be removed for good.
func TestNotebookMetaPreviewAndPurgeRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	_, nb := f.callJSON(t, "POST", "/api/notebooks", map[string]any{"name": "keel-meta", "cells": []map[string]any{{"body": "keel-edit-me"}}})
	id := nb["id"].(string)
	code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "edit the notebook keel-meta"})
	if code != http.StatusOK || cards(t, resp)[0]["status"] != "edited" {
		t.Fatalf("ask → %d %v", code, resp["notebooks"])
	}
	if code, out := f.callJSON(t, "PATCH", "/api/notebooks/"+id, map[string]any{"pinned": true, "cells": []map[string]any{{"body": "OLD"}}}); code != http.StatusOK || out["pinned"] != true {
		t.Fatalf("pin → %d %v", code, out)
	}
	if code, out := f.callJSON(t, "PATCH", "/api/notebooks/"+id, map[string]any{"name": "keel-meta renamed"}); code != http.StatusOK || out["name"] != "keel-meta renamed" {
		t.Fatalf("rename → %d %v", code, out)
	}
	if got := f.notebookText(t, id); got != "keel-edited-by-agent" {
		t.Fatalf("text after pin and rename = %q", got)
	}
	// Recently deleted: the preview has the text; Delete forever removes it.
	if code, _ := f.call(t, "DELETE", "/api/notebooks/"+id, nil); code != http.StatusNoContent {
		t.Fatalf("delete → %d", code)
	}
	_, trash := f.callJSON(t, "GET", "/api/notebook-trash", nil)
	vid := trash["versions"].([]any)[0].(map[string]any)["id"].(string)
	code, v := f.callJSON(t, "GET", "/api/notebook-trash/"+vid, nil)
	if code != http.StatusOK || v["cells"].([]any)[0].(map[string]any)["body"] != "keel-edited-by-agent" {
		t.Fatalf("preview → %d %v", code, v)
	}
	if code, _ := f.call(t, "DELETE", "/api/notebook-trash/"+vid, nil); code != http.StatusNoContent {
		t.Fatalf("delete forever → %d", code)
	}
	if code, _ := f.callJSON(t, "POST", "/api/notebook-trash/"+vid+"/restore", nil); code != http.StatusNotFound {
		t.Fatalf("restore after delete forever → %d", code)
	}
	// History: delete one version for good.
	_, nb2 := f.callJSON(t, "POST", "/api/notebooks", map[string]any{"name": "keel-meta two", "cells": []map[string]any{{"body": "keel-edit-me"}}})
	id2 := nb2["id"].(string)
	f.ask(t, map[string]any{"conversationId": askConv, "question": "edit the notebook keel-meta two"})
	_, hist := f.callJSON(t, "GET", "/api/notebooks/"+id2+"/history", nil)
	hv := hist["versions"].([]any)[0].(map[string]any)["id"].(string)
	if code, _ := f.call(t, "DELETE", "/api/notebooks/"+id2+"/history/"+hv, nil); code != http.StatusNoContent {
		t.Fatalf("delete version → %d", code)
	}
	if _, hist = f.callJSON(t, "GET", "/api/notebooks/"+id2+"/history", nil); len(hist["versions"].([]any)) != 0 {
		t.Fatalf("history after delete = %v", hist)
	}
}
