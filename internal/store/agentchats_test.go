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

func sampleChat(id string, at time.Time) AgentChat {
	return AgentChat{
		ID: id, Title: "Why is pg-2 lagging?", CreatedAt: at, UpdatedAt: at,
		Turns: []AgentChatTurn{{
			At: at, Question: "why is pg-2 lagging behind?", Answer: "the network", Model: "the network",
			Trace:     []AgentChatStep{{SessionID: "s1", HostID: "h1", Host: "prod-db-01", Status: "read", Lines: 120}},
			Proposals: []AgentChatProposal{{ID: "p_1", SessionID: "s1", HostID: "h1", Host: "prod-db-01", Command: "uptime", Risk: "readonly", ProposedAt: at}},
		}},
	}
}

// Conversations are saved encrypted in keel.chats, never in keel.json,
// and survive a restart once the vault is open again.
func TestAgentChatsSavedEncryptedAndReloaded(t *testing.T) {
	st := testStore(t)
	at := time.Date(2026, 10, 14, 16, 2, 0, 0, time.UTC)
	if err := st.PutAgentChat(sampleChat("conv-0001", at)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(st.path)
	blob, err := os.ReadFile(filepath.Join(dir, "keel.chats"))
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"pg-2", "prod-db-01", "uptime", "network"} {
		if bytes.Contains(blob, []byte(word)) {
			t.Fatalf("keel.chats holds %q in the clear", word)
		}
	}
	js, _ := os.ReadFile(filepath.Join(dir, "keel.json"))
	if bytes.Contains(js, []byte("pg-2")) || bytes.Contains(js, []byte("agentchats")) {
		t.Fatal("keel.json holds conversation data")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "keel.chats")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("keel.chats mode %v, want 0600", fi.Mode().Perm())
	}

	// A new process with the same vault reads it back.
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st2.GetAgentChat("conv-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Why is pg-2 lagging?" || len(got.Turns) != 1 || got.Turns[0].Proposals[0].Command != "uptime" {
		t.Fatalf("reloaded %+v", got)
	}
	list, err := st2.ListAgentChats()
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	if s := list[0]; s.Turns != 1 || s.Proposals != 1 || len(s.Hosts) != 1 || s.Hosts[0] != "prod-db-01" {
		t.Fatalf("summary %+v", s)
	}
}

// The key that opens keel.chats is a vault secret: without the vault's
// passphrase the transcripts cannot be read.
func TestAgentChatsNeedTheVault(t *testing.T) {
	st := testStore(t)
	if err := st.PutAgentChat(sampleChat("conv-0001", time.Now())); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	t.Setenv("KEEL_KEYRING", "off")
	locked, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if !locked.Locked() {
		t.Fatal("store should be locked without the passphrase")
	}
	if _, err := locked.ListAgentChats(); !errors.Is(err, ErrLocked) {
		t.Fatalf("list while locked: %v, want ErrLocked", err)
	}
	if err := locked.Unlock("test-vault-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := locked.GetAgentChat("conv-0001"); err != nil {
		t.Fatalf("after unlock: %v", err)
	}
}

func TestAgentChatsRenameDeletePrune(t *testing.T) {
	st := testStore(t)
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	for i, age := range []time.Duration{0, 10 * 24 * time.Hour, 40 * 24 * time.Hour} {
		id := []string{"conv-new01", "conv-mid01", "conv-old01"}[i]
		if err := st.PutAgentChat(sampleChat(id, now.Add(-age))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.RenameAgentChat("conv-new01", "  Replica\nlag \x1b[31m "); err != nil {
		t.Fatal(err)
	}
	c, _ := st.GetAgentChat("conv-new01")
	if c.Title != "Replica lag [31m" {
		t.Fatalf("title %q", c.Title)
	}
	if !c.UpdatedAt.Equal(now) {
		t.Fatal("rename moved the conversation's time")
	}
	if _, err := st.RenameAgentChat("conv-new01", " \n "); !errors.Is(err, ErrAgentChat) {
		t.Fatalf("empty title: %v", err)
	}
	if _, err := st.RenameAgentChat("nope-0001", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename missing: %v", err)
	}

	gone, err := st.PruneAgentChats(now.Add(-30 * 24 * time.Hour))
	if err != nil || len(gone) != 1 || gone[0] != "conv-old01" {
		t.Fatalf("prune 30 days: %v %v", gone, err)
	}
	if err := st.DeleteAgentChat("conv-mid01"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAgentChat("conv-mid01"); err != nil {
		t.Fatalf("deleting twice: %v", err)
	}
	list, _ := st.ListAgentChats()
	if len(list) != 1 || list[0].ID != "conv-new01" {
		t.Fatalf("left %+v", list)
	}
	n, err := st.DeleteAllAgentChats()
	if err != nil || n != 1 {
		t.Fatalf("delete all: %d %v", n, err)
	}
	st2, _ := New(st.path)
	if list, _ := st2.ListAgentChats(); len(list) != 0 {
		t.Fatalf("after delete all a restart lists %d", len(list))
	}
}

func TestAgentChatsBounded(t *testing.T) {
	st := testStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < MaxAgentChats+3; i++ {
		id := "conv-" + strings.Repeat("0", 4) + string(rune('a'+i%26)) + time.Duration(i).String()
		if err := st.PutAgentChat(sampleChat(id, base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := st.ListAgentChats()
	if len(list) != MaxAgentChats {
		t.Fatalf("kept %d, want %d", len(list), MaxAgentChats)
	}
	if !list[len(list)-1].UpdatedAt.Equal(base.Add(3 * time.Minute)) {
		t.Fatalf("oldest kept is %v; the three oldest should have gone", list[len(list)-1].UpdatedAt)
	}
}

// The chat key is created on the first save only: listing on a fresh vault
// writes nothing.
func TestAgentChatsListWritesNothing(t *testing.T) {
	st := testStore(t)
	before, _ := os.ReadFile(filepath.Join(filepath.Dir(st.path), "keel.vault"))
	if list, err := st.ListAgentChats(); err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
	if _, err := st.PruneAgentChats(time.Now()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(filepath.Dir(st.path), "keel.vault"))
	if !bytes.Equal(before, after) {
		t.Fatal("listing rewrote keel.vault")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(st.path), "keel.chats")); !os.IsNotExist(err) {
		t.Fatal("listing created keel.chats")
	}
}

func TestCleanAgentChatTitle(t *testing.T) {
	long := strings.Repeat("é", 100)
	got := CleanAgentChatTitle(long)
	if len(got) > MaxAgentChatTitle || !strings.HasSuffix(got, "…") {
		t.Fatalf("long title %q (%d bytes)", got, len(got))
	}
	if !strings.HasPrefix(got, "éé") || strings.ContainsRune(got, '�') {
		t.Fatal("cut inside a rune")
	}
}
