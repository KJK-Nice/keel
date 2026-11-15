package service

import (
	"context"
	"testing"

	"keel/internal/agent"
)

// #147: Ask adds proposals to the conversation while the gate reads them
// (Type, and TypeDoneAt after it). Run under -race.
func TestAskWhileTypingNoRace(t *testing.T) {
	f := newFixture(t, "")
	p := askOne(t, f, "uptime", "s-web")
	req := TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: p.SessionID, Role: agent.RoleDriver, Bytes: p.Bytes}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_ = f.svc.AuthorizeType(req)
			f.svc.TypeDoneAt(conv, p.ID, 0)
		}
	}()
	for i := 0; i < 50; i++ {
		if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-web"}}); err != nil {
			t.Fatal(err)
		}
	}
	<-done
}
