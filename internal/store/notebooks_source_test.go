package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A notebook's source (docs/ui-redesign.md §18) is never taken from the
// caller of UpsertNotebook: only AgentCreateNotebook marks one "agent", it
// always makes a new notebook, and every later save keeps the mark. Files
// written before the field existed read back as the user's own.
func TestNotebookSourceIsNeverFromTheClient(t *testing.T) {
	st := testStore(t)
	mine, err := st.UpsertNotebook(Notebook{Name: "mine", Source: NotebookSourceAgent, Cells: []NotebookCell{{Body: "ls"}}})
	if err != nil {
		t.Fatal(err)
	}
	if mine.Source != "" {
		t.Fatalf("a client-sent source was kept: %+v", mine)
	}
	// An update cannot claim it either, and a PUT to an unknown id
	// creates the user's own.
	mine.Source = NotebookSourceAgent
	if up, err := st.UpsertNotebook(mine); err != nil || up.Source != "" {
		t.Fatalf("update → %+v %v", up, err)
	}
	if ghost, err := st.UpsertNotebook(Notebook{ID: "nb-unknown", Name: "x", Source: NotebookSourceAgent}); err != nil || ghost.Source != "" {
		t.Fatalf("unknown id → %+v %v", ghost, err)
	}

	// AgentCreateNotebook always creates a new one, whatever id it is given.
	agentNb, err := st.AgentCreateNotebook(Notebook{ID: mine.ID, Name: "draft", Cells: []NotebookCell{{Body: "## from the agent"}}})
	if err != nil {
		t.Fatal(err)
	}
	if agentNb.ID == mine.ID || agentNb.Source != NotebookSourceAgent {
		t.Fatalf("agent notebook = %+v", agentNb)
	}
	if got, _ := st.GetNotebook(mine.ID); got.Name != "mine" {
		t.Fatalf("the agent's save replaced the user's notebook: %+v", got)
	}
	// The user edits it: the mark stays, whatever the client sends.
	agentNb.Name = "edited"
	agentNb.Source = ""
	edited, err := st.UpsertNotebook(agentNb)
	if err != nil || edited.Source != NotebookSourceAgent || edited.Name != "edited" {
		t.Fatalf("edit → %+v %v", edited, err)
	}

	// Older files have no source field: they read as the user's own, and
	// the user's own notebooks are written without one.
	raw, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Notebooks []map[string]any `json:"notebooks"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	for _, nb := range file.Notebooks {
		_, has := nb["source"]
		if (nb["name"] == "edited") != has {
			t.Errorf("notebook %v: source field present = %v", nb["name"], has)
		}
	}
	st2, err := New(filepath.Join(filepath.Dir(st.path), "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := st2.ListNotebooks()
	if err != nil {
		t.Fatal(err)
	}
	agents := 0
	for _, nb := range list {
		if nb.Source == NotebookSourceAgent {
			agents++
		}
	}
	if agents != 1 {
		t.Fatalf("after reload %d agent notebooks, want 1", agents)
	}
	var old Notebook
	if err := json.Unmarshal([]byte(`{"id":"a","name":"old","cells":[]}`), &old); err != nil || old.Source != "" || strings.Contains(string(raw), `"source":""`) {
		t.Fatalf("old notebook = %+v %v", old, err)
	}
}
