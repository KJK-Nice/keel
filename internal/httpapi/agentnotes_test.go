package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/applog"
	"keel/internal/store"
)

// The notes routes end to end with the scripted provider: a note proposed
// in an answer is saved only on the user's Save, is sent with the next
// question that reads a pane on its host (and shows in the trace), is not
// sent once "Agent can read" is off, and the user can add, edit and delete
// notes. keel.memory is encrypted and holds no vault secret.
func TestAgentNotesRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "$ uptime\n")

	code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "remember this", "panes": []string{web.ID}})
	if code != http.StatusOK {
		t.Fatalf("ask → %d %v", code, resp)
	}
	notes, _ := resp["notes"].([]any)
	if len(notes) != 1 {
		t.Fatalf("proposals %v", resp["notes"])
	}
	np := notes[0].(map[string]any)
	if np["for"] != "host" || np["hostId"] != f.webID || np["fromPane"] != false || np["status"] != "open" {
		t.Fatalf("proposal %v", np)
	}
	if list, _ := f.st.ListAgentNotes(); len(list) != 0 {
		t.Fatal("a proposal was kept before Save")
	}

	nid := np["id"].(string)
	code, b := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/notes/"+nid, map[string]string{})
	if code != 200 || !bytes.Contains(b, []byte(`"status":"saved"`)) {
		t.Fatalf("save → %d %s", code, b)
	}
	if code, _ := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/notes/"+nid, map[string]string{}); code != http.StatusConflict {
		t.Fatalf("save twice → %d", code)
	}
	list, _ := f.st.ListAgentNotes()
	if len(list) != 1 || list[0].Source != store.NoteSourceAgent || list[0].HostID != f.webID {
		t.Fatalf("kept %+v", list)
	}

	// The next question reads web-1: its note goes with it, in the trace.
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "and now?", "panes": []string{web.ID}})
	if code != 200 || !strings.Contains(resp["answer"].(string), "1 host note and 0 of your notes") {
		t.Fatalf("ask with a note → %d %v", code, resp["answer"])
	}
	trace := resp["trace"].([]any)
	if st := trace[0].(map[string]any); st["status"] != "notes" || st["noteKind"] != "host" || st["host"] != "web-1" || st["notes"] != float64(1) {
		t.Fatalf("trace %v", trace)
	}

	// A user note, with a vault secret in it: redacted on save.
	code, b = f.call(t, "POST", "/api/agent/notes", map[string]string{"text": "My password is " + askHostPass + "; prefer journalctl"})
	var saved struct {
		Note     store.AgentNote `json:"note"`
		Redacted int             `json:"redacted"`
	}
	if code != 200 || json.Unmarshal(b, &saved) != nil || saved.Redacted != 1 || strings.Contains(saved.Note.Text, askHostPass) {
		t.Fatalf("add → %d %s", code, b)
	}
	if code, b := f.call(t, "PUT", "/api/agent/notes/"+saved.Note.ID, map[string]string{"text": "prefer journalctl"}); code != 200 || !bytes.Contains(b, []byte(`"text":"prefer journalctl"`)) {
		t.Fatalf("edit → %d %s", code, b)
	}
	if code, _ := f.call(t, "POST", "/api/agent/notes", map[string]string{"hostId": "nope", "text": "x"}); code != 404 {
		t.Fatalf("note for a missing host → %d", code)
	}
	if code, _ := f.call(t, "POST", "/api/agent/notes", map[string]string{"text": " "}); code != 400 {
		t.Fatalf("empty note → %d", code)
	}

	// "Agent can read" off: neither the host's notes nor the pane are sent.
	h, _ := f.st.GetHost(f.webID)
	h.AgentReadable = bp(false)
	if _, err := f.st.UpsertHost(h); err != nil {
		t.Fatal(err)
	}
	code, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "again?", "panes": []string{web.ID}})
	if code != 200 || strings.Contains(resp["answer"].(string), "host note") {
		t.Fatalf("ask with agent read off → %d %v", code, resp["answer"])
	}
	for _, st := range resp["trace"].([]any) {
		if st.(map[string]any)["status"] == "notes" {
			t.Fatalf("notes sent for a host the agent may not read: %v", resp["trace"])
		}
	}

	// List, delete.
	code, b = f.call(t, "GET", "/api/agent/notes", nil)
	if code != 200 || !bytes.Contains(b, []byte(`"source":"agent"`)) || !bytes.Contains(b, []byte(`prefer journalctl`)) {
		t.Fatalf("list → %d %s", code, b)
	}
	if code, _ := f.call(t, "DELETE", "/api/agent/notes/"+saved.Note.ID, nil); code != 204 {
		t.Fatalf("delete → %d", code)
	}
	if code, _ := f.call(t, "DELETE", "/api/agent/notes/"+saved.Note.ID, nil); code != 404 {
		t.Fatalf("delete twice → %d", code)
	}

	blob, err := os.ReadFile(filepath.Join(filepath.Dir(f.logPath), "keel.memory"))
	if err != nil || bytes.Contains(blob, []byte("journalctl")) || bytes.Contains(blob, []byte("keel-note-e2e")) {
		t.Fatalf("keel.memory not encrypted (%v)", err)
	}
	all, _ := f.st.ListAgentNotes()
	plain, _ := json.Marshal(all)
	if bytes.Contains(plain, []byte(askHostPass)) {
		t.Fatal("a note holds a vault secret")
	}

	// Every send and every change is in keel.log, without the text.
	var kinds []applog.Entry
	for _, e := range f.logs.Recent(500) {
		if e.Kind == agent.EventNote {
			kinds = append(kinds, e)
			if strings.Contains(e.Message, "journalctl") || strings.Contains(e.Message, "pgbackrest") {
				t.Fatalf("keel.log holds note text: %q", e.Message)
			}
		}
	}
	if len(kinds) != 5 { // saved, sent, added, edited, deleted
		t.Fatalf("agent.note entries %d: %+v", len(kinds), kinds)
	}

	// Dismiss and undo on a fresh proposal.
	f.st.UpsertHost(func() store.Host { h.AgentReadable = bp(true); return h }())
	_, resp = f.ask(t, map[string]any{"conversationId": askConv, "question": "remember my preference", "panes": []string{web.ID}})
	np = resp["notes"].([]any)[0].(map[string]any)
	if np["for"] != "user" {
		t.Fatalf("user proposal %v", np)
	}
	nid = np["id"].(string)
	if code, b := f.call(t, "DELETE", "/api/agent/conversations/"+askConv+"/notes/"+nid, nil); code != 200 || !bytes.Contains(b, []byte(`"status":"dismissed"`)) {
		t.Fatalf("dismiss → %d %s", code, b)
	}
	if code, b := f.call(t, "POST", "/api/agent/conversations/"+askConv+"/notes/"+nid+"/undo", nil); code != 200 || !bytes.Contains(b, []byte(`"status":"open"`)) {
		t.Fatalf("undo → %d %s", code, b)
	}
}
