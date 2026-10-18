package store

import (
	"testing"
)

// A host created under an id the caller chose starts with no notes, even
// when notes for that id were left behind (here: the host was removed
// without its notes, as when keel.memory could not be written).
func TestHostWithReusedIDStartsWithoutNotes(t *testing.T) {
	st := testStore(t)
	h := noteHost(t, st, "old")
	if _, err := st.AddAgentNote(AgentNote{HostID: h.ID, Text: "about the old host"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddAgentNote(AgentNote{Text: "mine"}); err != nil {
		t.Fatal(err)
	}
	if err := st.deleteHost(h.ID); err != nil { // the host only, notes stay
		t.Fatal(err)
	}
	if _, err := st.UpsertHost(Host{ID: h.ID, Name: "new", Address: "new.example.com", Username: "u", AuthType: "password"}); err != nil {
		t.Fatal(err)
	}
	notes, _ := st.ListAgentNotes()
	if len(notes) != 1 || notes[0].HostID != "" {
		t.Fatalf("notes after re-creating the id: %+v", notes)
	}
	// Updating an existing host never touches its notes.
	if _, err := st.AddAgentNote(AgentNote{HostID: h.ID, Text: "about the new host"}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetHost(h.ID)
	got.Name = "renamed"
	if _, err := st.UpsertHost(got); err != nil {
		t.Fatal(err)
	}
	if notes, _ := st.ListAgentNotes(); len(notes) != 2 {
		t.Fatalf("an update removed notes: %+v", notes)
	}
}
