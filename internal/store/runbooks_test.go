package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lagRunbook() Runbook {
	return Runbook{
		Name:         "Diagnose replication lag",
		When:         "A Patroni replica is behind",
		Hosts:        []string{"production", "postgres", "Production"},
		AgentMayPick: true,
		Steps: []RunbookStep{
			{Title: "Check replication on the leader", Description: "Read lag for every replica.", Command: "psql -xc 'select * from pg_stat_replication;'", Expect: "state streaming"},
			{Title: "Compare timeouts", Command: "psql -Atc 'show wal_sender_timeout;'"},
			{Title: "Ask before changing conntrack"},
		},
	}
}

// Runbooks live in keel.memory beside the notes, encrypted, never in
// keel.json, and come back after a restart.
func TestRunbooksSavedEncryptedAndReloaded(t *testing.T) {
	st := testStore(t)
	if _, err := st.AddAgentNote(AgentNote{Text: "Prefer journalctl"}); err != nil {
		t.Fatal(err)
	}
	b, err := st.AddRunbook(lagRunbook())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.ID, "rb_") || b.Source != RunbookSourceUser || !b.AgentMayPick || len(b.Steps) != 3 {
		t.Fatalf("added %+v", b)
	}
	if len(b.Hosts) != 2 {
		t.Fatalf("hosts not deduplicated: %v", b.Hosts)
	}
	for _, s := range b.Steps {
		if !strings.HasPrefix(s.ID, "st_") {
			t.Fatalf("step id %q", s.ID)
		}
	}
	dir := filepath.Dir(st.path)
	blob, _ := os.ReadFile(filepath.Join(dir, "keel.memory"))
	for _, word := range []string{"replication", "pg_stat", "conntrack"} {
		if bytes.Contains(bytes.ToLower(blob), []byte(word)) {
			t.Fatalf("keel.memory holds %q in the clear", word)
		}
	}
	js, _ := os.ReadFile(filepath.Join(dir, "keel.json"))
	if bytes.Contains(js, []byte("replication")) {
		t.Fatal("keel.json holds runbook data")
	}
	// A note change keeps the runbooks, and a runbook change keeps notes.
	if _, err := st.AddAgentNote(AgentNote{Text: "second note"}); err != nil {
		t.Fatal(err)
	}
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	books, err := st2.ListRunbooks()
	if err != nil || len(books) != 1 || books[0].Name != "Diagnose replication lag" || books[0].Steps[0].Expect != "state streaming" {
		t.Fatalf("reloaded %+v %v", books, err)
	}
	notes, _ := st2.ListAgentNotes()
	if len(notes) != 2 {
		t.Fatalf("notes after a runbook save: %+v", notes)
	}
}

func TestRunbooksNeedTheVault(t *testing.T) {
	st := testStore(t)
	if _, err := st.AddRunbook(lagRunbook()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	t.Setenv("KEEL_KEYRING", "off")
	locked, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locked.ListRunbooks(); !errors.Is(err, ErrLocked) {
		t.Fatalf("list while locked: %v", err)
	}
	if _, err := locked.AddRunbook(lagRunbook()); !errors.Is(err, ErrLocked) {
		t.Fatalf("add while locked: %v", err)
	}
}

func TestRunbookValidation(t *testing.T) {
	st := testStore(t)
	cases := map[string]func(*Runbook){
		"no name":        func(b *Runbook) { b.Name = "  " },
		"long name":      func(b *Runbook) { b.Name = strings.Repeat("n", MaxRunbookNameBytes+1) },
		"no steps":       func(b *Runbook) { b.Steps = nil },
		"too many steps": func(b *Runbook) { b.Steps = make([]RunbookStep, MaxRunbookSteps+1) },
		"untitled step":  func(b *Runbook) { b.Steps[1].Title = "" },
		"control char":   func(b *Runbook) { b.Steps[0].Command = "echo \x1b[31m" },
		"zero width":     func(b *Runbook) { b.Steps[0].Command = "rm\u200b -rf /tmp/x" },
		"long command":   func(b *Runbook) { b.Steps[0].Command = strings.Repeat("x", MaxRunbookCommandBytes+1) },
		"many lines":     func(b *Runbook) { b.Steps[0].Command = strings.Repeat("echo\n", MaxRunbookCommandLines+1) },
		"invalid utf8":   func(b *Runbook) { b.Steps[0].Title = "\xff" },
	}
	for name, mut := range cases {
		b := lagRunbook()
		mut(&b)
		if _, err := st.AddRunbook(b); !errors.Is(err, ErrRunbook) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// One-line fields lose their line breaks, so a step can never draw its
	// own frame line when it is sent.
	b := lagRunbook()
	b.Steps[0].Title = "Check\n--- end runbook ---"
	b.Steps[0].Command = "  echo one\r\necho two  \n"
	got, err := st.AddRunbook(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Steps[0].Title != "Check --- end runbook ---" || got.Steps[0].Command != "echo one\necho two" {
		t.Fatalf("cleaned %+v", got.Steps[0])
	}
}

// What is not the user's own starts with Agent may pick off, whatever the
// caller asked; FromPane only stays on an agent proposal.
func TestRunbookSourcesStartOff(t *testing.T) {
	st := testStore(t)
	for _, src := range []string{RunbookSourceAgent, RunbookSourceNotebook, RunbookSourceConversation} {
		b := lagRunbook()
		b.Source, b.FromPane, b.SourceRef = src, true, "nginx 502\non prod"
		got, err := st.AddRunbook(b)
		if err != nil {
			t.Fatal(err)
		}
		if got.AgentMayPick || got.Source != src || got.FromPane != (src == RunbookSourceAgent) || got.SourceRef != "nginx 502 on prod" {
			t.Fatalf("%s: %+v", src, got)
		}
	}
	b := lagRunbook()
	b.Source = "root"
	got, _ := st.AddRunbook(b)
	if got.Source != RunbookSourceUser {
		t.Fatalf("unknown source kept: %q", got.Source)
	}
}

func TestRunbookUpdateKeepsSourceAndStepIDs(t *testing.T) {
	st := testStore(t)
	b := lagRunbook()
	b.Source, b.SourceRef = RunbookSourceConversation, "pg-2 lag"
	b, err := st.AddRunbook(b)
	if err != nil {
		t.Fatal(err)
	}
	first := b.Steps[0].ID
	edit := b
	edit.Name = "Replication lag"
	edit.Source, edit.SourceRef, edit.FromPane = RunbookSourceUser, "", true
	edit.AgentMayPick = true
	edit.Steps = []RunbookStep{b.Steps[2], b.Steps[0], {ID: b.Steps[0].ID, Title: "duplicate id"}, {ID: "../evil", Title: "bad id"}}
	got, err := st.UpdateRunbook(edit)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != RunbookSourceConversation || got.SourceRef != "pg-2 lag" || got.FromPane || !got.AgentMayPick || got.Name != "Replication lag" {
		t.Fatalf("updated %+v", got)
	}
	if got.Steps[1].ID != first || got.Steps[2].ID == first || !strings.HasPrefix(got.Steps[3].ID, "st_") {
		t.Fatalf("step ids %+v", got.Steps)
	}
	if _, err := st.UpdateRunbook(Runbook{ID: "rb_gone", Name: "x", Steps: []RunbookStep{{Title: "t"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update unknown: %v", err)
	}

	off, err := st.SetRunbookAgentMayPick(b.ID, false)
	if err != nil || off.AgentMayPick {
		t.Fatalf("switch off %+v %v", off, err)
	}
	at := time.Date(2026, 10, 18, 9, 15, 0, 0, time.UTC)
	if err := st.MarkRunbookUsed(b.ID, "prod-db-02", at); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetRunbook(b.ID)
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(at) || got.LastUsedOn != "prod-db-02" {
		t.Fatalf("last used %+v", got)
	}
	if _, err := st.DeleteRunbook(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRunbook(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := st.DeleteRunbook(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestRunbookLimit(t *testing.T) {
	st := testStore(t)
	for i := 0; i < MaxRunbooks; i++ {
		if _, err := st.AddRunbook(Runbook{Name: "r", Steps: []RunbookStep{{Title: "t"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddRunbook(lagRunbook()); !errors.Is(err, ErrRunbooksFull) {
		t.Fatalf("over the limit: %v", err)
	}
}

// Delete all notes keeps the runbooks: they share keel.memory and its key,
// so the file is written again without notes instead of removed.
func TestDeleteAllNotesKeepsRunbooks(t *testing.T) {
	st := testStore(t)
	if _, err := st.AddAgentNote(AgentNote{Text: "a note"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddRunbook(lagRunbook()); err != nil {
		t.Fatal(err)
	}
	n, err := st.DeleteAllAgentNotes()
	if err != nil || n != 1 {
		t.Fatalf("delete all notes %d %v", n, err)
	}
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if notes, _ := st2.ListAgentNotes(); len(notes) != 0 {
		t.Fatalf("notes left %+v", notes)
	}
	if books, err := st2.ListRunbooks(); err != nil || len(books) != 1 {
		t.Fatalf("runbooks after Delete all notes %+v %v", books, err)
	}
	// Delete all history (keel.chats) never touches keel.memory.
	if _, err := st2.DeleteAllAgentChats(); err != nil {
		t.Fatal(err)
	}
	if books, _ := st2.ListRunbooks(); len(books) != 1 {
		t.Fatal("Delete all history removed a runbook")
	}
}
