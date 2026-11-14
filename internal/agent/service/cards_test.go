package service

import (
	"context"
	"errors"
	"testing"
)

// A note card claimed for an action, whose conversation is deleted while
// the action runs, is not finished: the action answers ErrNotFound and the
// card's status does not change.
func TestNoteCardGoneWhileClaimed(t *testing.T) {
	f, _ := notesFixture(t, noteReply)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "x", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	c, np, _, err := noteCards.claim(f.svc, conv, resp.Notes[0].ID, noteOpen)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := noteCards.claim(f.svc, conv, np.ID, noteOpen); !errors.Is(err, ErrNoteClosed) {
		t.Fatalf("second claim: %v, want ErrNoteClosed", err)
	}
	if err := f.svc.Delete(conv); err != nil {
		t.Fatal(err)
	}
	if out, err := f.svc.setNoteStatus(c, np, CardDismissed, ""); !errors.Is(err, ErrNotFound) || out != nil {
		t.Fatalf("finish on a deleted conversation: %v, %v; want ErrNotFound", out, err)
	}
	if np.Status != CardOpen {
		t.Fatalf("status = %q, want it left open", np.Status)
	}
}
