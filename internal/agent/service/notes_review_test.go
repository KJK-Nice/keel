package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"keel/internal/store"
)

// Vault values with runs of spaces, tabs, zero-width or bidi characters
// are still redacted from a note after it is cleaned to one line (save),
// and from a note that holds their cleaned form (send).
func TestNoteRedactionSurvivesCleaning(t *testing.T) {
	zw := string(rune(0x200b))
	bidi := string(rune(0x202e))
	secrets := map[string]string{
		"double space": "hunter2  secretpass",
		"tab":          "tabbed\tsecret991",
		"zero width":   "zero" + zw + "widthsecret77",
		"bidi":         "bidi" + bidi + "secret55x",
	}
	cleaned := map[string][]string{
		"double space": {"hunter2 secretpass"},
		"tab":          {"tabbed secret991"},
		"zero width":   {"zero widthsecret77", "zerowidthsecret77"},
		"bidi":         {"bidi secret55x", "bidisecret55x"},
	}
	for name, secret := range secrets {
		t.Run(name, func(t *testing.T) {
			f, ns := notesFixture(t, "ok")
			f.st.mu.Lock()
			f.st.secrets = append(f.st.secrets, secret)
			f.st.mu.Unlock()
			out, err := f.svc.AddNote("web", "login is "+secret+" on web")
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range append([]string{secret}, cleaned[name]...) {
				if strings.Contains(out.Note.Text, c) {
					t.Fatalf("kept %q holds the vault value", out.Note.Text)
				}
			}
			if out.Redacted == 0 {
				t.Fatal("nothing reported redacted")
			}
			// A note that already holds a cleaned form (kept before the
			// value was in the vault) is redacted when it is sent.
			for _, c := range cleaned[name] {
				ns.put(store.AgentNote{HostID: "web", Text: "old note " + c})
			}
			if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "x", Panes: []string{"s-web"}}); err != nil {
				t.Fatal(err)
			}
			body := f.requestBodies(t)
			for _, c := range cleaned[name] {
				if strings.Contains(body, c) {
					t.Fatalf("a cleaned form %q of a vault value reached the provider", c)
				}
			}
		})
	}
}

// Many Saves (Save and Save anyway, two windows) on one proposal make one
// note; the others get ErrNoteClosed.
func TestSaveNoteProposalOnce(t *testing.T) {
	f, ns := notesFixture(t, noteReply)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "x", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	id := resp.Notes[0].ID
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, closed := 0, 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.SaveNoteProposal(conv, id, "")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrNoteClosed):
				closed++
			default:
				t.Errorf("save: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || closed != 15 || len(ns.notes) != 1 {
		t.Fatalf("ok %d closed %d notes %d", ok, closed, len(ns.notes))
	}
	// Undo and Dismiss while nothing runs work; a deleted conversation is
	// not found (never a crash).
	if _, err := f.svc.UndoNoteProposal(conv, id); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(conv); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DismissNoteProposal(conv, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dismiss in a deleted conversation: %v", err)
	}
	if _, err := f.svc.UndoNoteProposal(conv, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("undo in a deleted conversation: %v", err)
	}
}

// A host note is saved only while the agent may read that host; and every
// proposal says which hosts' panes were read for its answer.
func TestSaveHostNoteNeedsAgentRead(t *testing.T) {
	f, ns := notesFixture(t, noteReply)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "x", Panes: []string{"s-web", "s-db"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Notes[0].PanesRead; len(got) != 2 || got[0] != "web-1" || got[1] != "prod-db-01" {
		t.Fatalf("panesRead = %v", got)
	}
	f.st.mu.Lock()
	f.st.hosts[0].AgentReadable = bptr(false)
	f.st.mu.Unlock()
	if _, err := f.svc.SaveNoteProposal(conv, resp.Notes[0].ID, ""); !errors.Is(err, ErrNoteNotReadable) {
		t.Fatalf("save with agent read off: %v", err)
	}
	if len(ns.notes) != 0 {
		t.Fatal("kept a note for a host the agent may not read")
	}
	// The refusal frees the proposal: once the host is readable again it saves.
	f.st.mu.Lock()
	f.st.hosts[0].AgentReadable = bptr(true)
	f.st.mu.Unlock()
	if _, err := f.svc.SaveNoteProposal(conv, resp.Notes[0].ID, ""); err != nil {
		t.Fatal(err)
	}
}
