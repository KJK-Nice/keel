package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nbText(nb Notebook) string {
	parts := []string{}
	for _, c := range nb.Cells {
		parts = append(parts, c.Body)
	}
	return strings.Join(parts, "\n")
}

func appendEdit(add string) func(Notebook) (Notebook, error) {
	return func(nb Notebook) (Notebook, error) {
		nb.Cells = []NotebookCell{{ID: nb.Cells[0].ID, Body: nb.Cells[0].Body + add}}
		return nb, nil
	}
}

// Private (§6 rule 12): off for new and older notebooks, never set by a
// save from the client, changed only by SetNotebookPrivate, and every
// agent path refuses a private notebook.
func TestNotebookPrivateOnlyFromItsRoute(t *testing.T) {
	st := testStore(t)
	nb, err := st.UpsertNotebook(Notebook{Name: "mine", Private: true, Cells: []NotebookCell{{Body: "ls"}}})
	if err != nil || nb.Private || nb.Rev != 1 {
		t.Fatalf("new notebook = %+v %v", nb, err)
	}
	if nb, err = st.SetNotebookPrivate(nb.ID, true); err != nil || !nb.Private || nb.Rev != 1 {
		t.Fatalf("private → %+v %v", nb, err)
	}
	// A stale editor's save (Private false in the body) keeps it private.
	nb.Private = false
	nb.Cells = []NotebookCell{{Body: "ls -la"}}
	if nb, err = st.UpsertNotebook(nb); err != nil || !nb.Private || nb.Rev != 2 {
		t.Fatalf("save → %+v %v", nb, err)
	}
	if list, _ := st.AgentListNotebooks(); len(list) != 0 {
		t.Fatalf("agent list holds a private notebook: %+v", list)
	}
	if _, err := st.AgentGetNotebook(nb.ID); !errors.Is(err, ErrNotebookPrivate) {
		t.Fatalf("agent read → %v", err)
	}
	if _, _, err := st.AgentEditNotebook(nb.ID, nb.Rev, "", appendEdit("x")); !errors.Is(err, ErrNotebookPrivate) {
		t.Fatalf("agent edit → %v", err)
	}
	if _, err := st.DeleteNotebookAgentProposed(nb.ID, -1); !errors.Is(err, ErrNotebookPrivate) {
		t.Fatalf("agent delete → %v", err)
	}
	if got, _ := st.GetNotebook(nb.ID); nbText(got) != "ls -la" {
		t.Fatalf("private notebook changed: %+v", got)
	}
	// The user turns it off: the agent sees it again.
	if _, err := st.SetNotebookPrivate(nb.ID, false); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.AgentListNotebooks(); len(list) != 1 {
		t.Fatalf("agent list = %+v", list)
	}
	// Reload: the flag is on disk; an agent-created notebook is never private.
	if _, err := st.SetNotebookPrivate(nb.ID, true); err != nil {
		t.Fatal(err)
	}
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := st2.GetNotebook(nb.ID); !got.Private {
		t.Fatalf("after reload %+v", got)
	}
	created, err := st2.AgentCreateNotebook(Notebook{Name: "agent", Private: true, Pinned: true, Cells: []NotebookCell{{Body: "x"}}})
	if err != nil || created.Private || created.Pinned || created.Source != NotebookSourceAgent || created.LastBy != NotebookByAgent {
		t.Fatalf("agent create → %+v %v", created, err)
	}
}

// An agent edit applies only at the revision read, keeps the version before
// it, marks the notebook, and Undo (a restore at the agent's revision)
// puts the user's text back; once the user changed it, Undo is a conflict.
func TestAgentEditKeepsHistoryAndUndo(t *testing.T) {
	st := testStore(t)
	nb, _ := st.UpsertNotebook(Notebook{Name: "runbook", Cells: []NotebookCell{{Body: "user text"}}})
	if _, _, err := st.AgentEditNotebook(nb.ID, nb.Rev+1, "", appendEdit(" agent")); !errors.Is(err, ErrNotebookConflict) {
		t.Fatalf("stale base → %v", err)
	}
	edited, vid, err := st.AgentEditNotebook(nb.ID, nb.Rev, "Adds a line", appendEdit(" agent"))
	if err != nil {
		t.Fatal(err)
	}
	if nbText(edited) != "user text agent" || edited.Rev != nb.Rev+1 || edited.LastBy != NotebookByAgent || !edited.AgentUnreviewed || edited.AgentEditedAt == nil || edited.Source != "" {
		t.Fatalf("edited = %+v", edited)
	}
	hist, err := st.NotebookHistory(nb.ID)
	if err != nil || len(hist) != 1 || hist[0].ID != vid || hist[0].Event != "before-agent-edit" || hist[0].By != "user" || hist[0].Summary != "Adds a line" || hist[0].Cells != nil {
		t.Fatalf("history = %+v %v", hist, err)
	}
	v, err := st.NotebookVersionByID(nb.ID, vid)
	if err != nil || nbText(Notebook{Cells: v.Cells}) != "user text" {
		t.Fatalf("version = %+v %v", v, err)
	}
	undone, err := st.RestoreNotebookVersion(nb.ID, vid, edited.Rev)
	if err != nil || nbText(undone) != "user text" || undone.LastBy != "" || undone.AgentUnreviewed {
		t.Fatalf("undo → %+v %v", undone, err)
	}
	// The agent's version was kept before the restore: Restore it again.
	hist, _ = st.NotebookHistory(nb.ID)
	if len(hist) != 2 || hist[0].Event != "before-restore" || hist[0].By != "agent" {
		t.Fatalf("history after undo = %+v", hist)
	}
	// A second agent edit, then the user types: Undo at the agent's rev is
	// a conflict.
	e2, vid2, err := st.AgentEditNotebook(nb.ID, undone.Rev, "", appendEdit(" again"))
	if err != nil {
		t.Fatal(err)
	}
	e2.Cells = []NotebookCell{{Body: "user typed over it"}}
	saved, replaced, err := st.SaveUserNotebook(e2)
	if err != nil || replaced || saved.LastBy != "" || !saved.AgentUnreviewed {
		t.Fatalf("user save → %+v %v %v", saved, replaced, err)
	}
	if _, err := st.RestoreNotebookVersion(nb.ID, vid2, e2.Rev); !errors.Is(err, ErrNotebookConflict) {
		t.Fatalf("undo after a user edit → %v", err)
	}
	// The agent's version was kept when the user replaced it.
	hist, _ = st.NotebookHistory(nb.ID)
	if hist[0].Event != "agent-version" || hist[0].By != "agent" {
		t.Fatalf("history = %+v", hist)
	}
	if _, err := st.MarkNotebookReviewed(nb.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetNotebook(nb.ID); got.AgentUnreviewed {
		t.Fatalf("still unreviewed: %+v", got)
	}
}

// A user save from an older revision over the agent's edit keeps the user's
// text (no keystroke is lost), keeps the agent's version in History and
// says so.
func TestUserSaveOverAgentEditKeepsBoth(t *testing.T) {
	st := testStore(t)
	nb, _ := st.UpsertNotebook(Notebook{Name: "n", Cells: []NotebookCell{{Body: "a"}}})
	if _, _, err := st.AgentEditNotebook(nb.ID, nb.Rev, "", appendEdit(" agent")); err != nil {
		t.Fatal(err)
	}
	nb.Cells = []NotebookCell{{Body: "a typed"}}
	saved, replaced, err := st.SaveUserNotebook(nb) // nb.Rev is the old one
	if err != nil || !replaced || nbText(saved) != "a typed" {
		t.Fatalf("save → %+v %v %v", saved, replaced, err)
	}
	hist, _ := st.NotebookHistory(nb.ID)
	v, _ := st.NotebookVersionByID(nb.ID, hist[0].ID)
	if hist[0].Event != "agent-version" || nbText(Notebook{Cells: v.Cells}) != "a agent" {
		t.Fatalf("history = %+v %+v", hist, v)
	}
}

// Autosave writes no history; a save that starts a new editing session (the
// version it replaces is 15 minutes old) keeps that version once.
func TestUserSavesKeepOneVersionPerSession(t *testing.T) {
	st := testStore(t)
	nb, _ := st.UpsertNotebook(Notebook{Name: "n", Cells: []NotebookCell{{Body: "v1"}}})
	for i := 0; i < 10; i++ {
		nb.Cells = []NotebookCell{{Body: "v1" + strings.Repeat("x", i)}}
		nb, _ = st.UpsertNotebook(nb)
	}
	if hist, _ := st.NotebookHistory(nb.ID); len(hist) != 0 {
		t.Fatalf("autosave kept %d versions", len(hist))
	}
	if _, err := os.Stat(st.historyPath()); !os.IsNotExist(err) {
		t.Fatalf("history file written by autosave: %v", err)
	}
	st.mu.Lock()
	st.data.Notebooks[st.notebookIndexLocked(nb.ID)].UpdatedAt = time.Now().Add(-time.Hour)
	st.mu.Unlock()
	nb.Cells = []NotebookCell{{Body: "v2"}}
	nb, _ = st.UpsertNotebook(nb)
	nb.Cells = []NotebookCell{{Body: "v3"}}
	nb, _ = st.UpsertNotebook(nb)
	hist, _ := st.NotebookHistory(nb.ID)
	if len(hist) != 1 || hist[0].Event != "session" || hist[0].By != "user" {
		t.Fatalf("history = %+v", hist)
	}
}

// The history keeps at most 20 versions per notebook, nothing older than
// 30 days, and at most 16 MiB of text.
func TestNotebookHistoryBounds(t *testing.T) {
	st := testStore(t)
	nb, _ := st.UpsertNotebook(Notebook{Name: "n", Cells: []NotebookCell{{Body: "0"}}})
	for i := 0; i < 25; i++ {
		var err error
		if nb, _, err = st.AgentEditNotebook(nb.ID, nb.Rev, "", appendEdit("+")); err != nil {
			t.Fatal(err)
		}
	}
	hist, _ := st.NotebookHistory(nb.ID)
	if len(hist) != maxVersionsPerNotebook {
		t.Fatalf("%d versions kept, want %d", len(hist), maxVersionsPerNotebook)
	}
	if hist[0].Rev != nb.Rev-1 {
		t.Fatalf("newest kept is rev %d, want %d", hist[0].Rev, nb.Rev-1)
	}

	now := time.Now()
	old := []NotebookVersion{{ID: "a", NotebookID: "x", KeptAt: now.Add(-31 * 24 * time.Hour)}, {ID: "b", NotebookID: "x", KeptAt: now}}
	if got := pruneVersions(old, now); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("ttl prune = %+v", got)
	}
	big := []NotebookVersion{{ID: "a", NotebookID: "x", Bytes: 5 << 20, KeptAt: now}, {ID: "b", NotebookID: "y", Bytes: 5 << 20, KeptAt: now}}
	if got := pruneVersions(big, now); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("size prune = %+v", got)
	}
	var dels []NotebookVersion
	for i := 0; i < 60; i++ {
		dels = append(dels, NotebookVersion{ID: string(rune('A' + i)), NotebookID: "d", Event: "deleted", KeptAt: now})
	}
	if got := pruneVersions(dels, now); len(got) != maxDeletedNotebooks {
		t.Fatalf("%d deleted kept", len(got))
	}
}

// A deleted notebook goes to Recently deleted and comes back with its
// id, source and Private flag. The agent-proposed delete refuses a
// private notebook and a changed revision.
func TestNotebookTrashAndRestore(t *testing.T) {
	st := testStore(t)
	nb, _ := st.UpsertNotebook(Notebook{Name: "gone", Cells: []NotebookCell{{Body: "text"}}})
	if _, err := st.DeleteNotebookAgentProposed(nb.ID, nb.Rev+1); !errors.Is(err, ErrNotebookConflict) {
		t.Fatalf("stale delete → %v", err)
	}
	vid, err := st.DeleteNotebookAgentProposed(nb.ID, nb.Rev)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetNotebook(nb.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
	del, _ := st.DeletedNotebooks()
	if len(del) != 1 || del[0].ID != vid || del[0].Name != "gone" {
		t.Fatalf("deleted = %+v", del)
	}
	back, err := st.RestoreDeletedNotebook(vid)
	if err != nil || back.ID != nb.ID || nbText(back) != "text" {
		t.Fatalf("restore → %+v %v", back, err)
	}
	if del, _ := st.DeletedNotebooks(); len(del) != 0 {
		t.Fatalf("still in trash: %+v", del)
	}
	// A user delete goes to the trash too, private flag and all.
	_, _ = st.SetNotebookPrivate(nb.ID, true)
	if err := st.DeleteNotebook(nb.ID); err != nil {
		t.Fatal(err)
	}
	del, _ = st.DeletedNotebooks()
	back, err = st.RestoreDeletedNotebook(del[0].ID)
	if err != nil || !back.Private {
		t.Fatalf("restore → %+v %v", back, err)
	}
}

// When the history cannot be read, it is never overwritten: an agent edit
// is refused (it could not be undone), the user's own save still works.
func TestNotebookHistoryUnreadable(t *testing.T) {
	st := testStore(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(st.path), notebookHistoryFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	nb, _ := st.UpsertNotebook(Notebook{Name: "n", Cells: []NotebookCell{{Body: "a"}}})
	if _, _, err := st.AgentEditNotebook(nb.ID, nb.Rev, "", appendEdit("b")); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("agent edit → %v", err)
	}
	nb.Cells = []NotebookCell{{Body: "user"}}
	if _, err := st.UpsertNotebook(nb); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteNotebook(nb.ID); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(st.historyPath())
	if string(b) != "{not json" {
		t.Fatalf("history overwritten: %q", b)
	}
}

// Agent writes are bounded like the notebook limits.
func TestAgentNotebookSizeLimits(t *testing.T) {
	st := testStore(t)
	if _, err := st.AgentCreateNotebook(Notebook{Name: strings.Repeat("n", MaxNotebookNameBytes+1)}); !errors.Is(err, ErrNotebookTooLarge) {
		t.Fatalf("long name → %v", err)
	}
	nb, _ := st.UpsertNotebook(Notebook{Name: "n", Cells: []NotebookCell{{Body: "a"}}})
	if _, _, err := st.AgentEditNotebook(nb.ID, nb.Rev, "", appendEdit(strings.Repeat("x", MaxNotebookBytes))); !errors.Is(err, ErrNotebookTooLarge) {
		t.Fatalf("large edit → %v", err)
	}
	if got, _ := st.GetNotebook(nb.ID); nbText(got) != "a" || got.Rev != nb.Rev {
		t.Fatalf("notebook changed: %+v", got)
	}
}
