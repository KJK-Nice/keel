package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/store"
)

// fakeChats is an in-memory ChatStore (the store's own tests cover
// keel.chats on disk).
type fakeChats struct {
	mu    sync.Mutex
	chats map[string]store.AgentChat
	puts  int
	// flush runs the service's queued saves (they are asynchronous) before
	// a test looks at what was saved.
	flush func()
	// broken makes every call fail as an unreadable keel.chats would,
	// until DeleteAllAgentChats.
	broken bool
	// failPut makes saves fail with an I/O error.
	failPut error
	// gate, when set, holds every save until it is closed.
	gate chan struct{}
}

func newFakeChats() *fakeChats { return &fakeChats{chats: map[string]store.AgentChat{}} }

func (f *fakeChats) ListAgentChats() ([]store.AgentChatSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return nil, store.ErrChatsUnreadable
	}
	out := []store.AgentChatSummary{}
	for _, c := range f.chats {
		out = append(out, c.Summary())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (f *fakeChats) GetAgentChat(id string) (store.AgentChat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return store.AgentChat{}, store.ErrChatsUnreadable
	}
	c, ok := f.chats[id]
	if !ok {
		return store.AgentChat{}, store.ErrNotFound
	}
	return c, nil
}

func (f *fakeChats) PutAgentChat(c store.AgentChat) error {
	if g := f.gate; g != nil {
		<-g // a slow disk: the save waits until the test lets it go
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return store.ErrChatsUnreadable
	}
	if f.failPut != nil {
		return f.failPut
	}
	f.puts++
	f.chats[c.ID] = c
	return nil
}

func (f *fakeChats) RenameAgentChat(id, title string) (store.AgentChatSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[id]
	if !ok {
		return store.AgentChatSummary{}, store.ErrNotFound
	}
	c.Title = title
	f.chats[id] = c
	return c.Summary(), nil
}

func (f *fakeChats) DeleteAgentChat(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return store.ErrChatsUnreadable
	}
	delete(f.chats, id)
	return nil
}

func (f *fakeChats) DeleteAllAgentChats() (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broken = false
	n := len(f.chats)
	f.chats = map[string]store.AgentChat{}
	return n, nil
}

func (f *fakeChats) PruneAgentChats(cutoff time.Time) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return nil, store.ErrChatsUnreadable
	}
	var gone []string
	for id, c := range f.chats {
		if c.UpdatedAt.Before(cutoff) {
			gone = append(gone, id)
			delete(f.chats, id)
		}
	}
	return gone, nil
}

// all is every saved byte, as JSON, to search the way a look at the
// decrypted keel.chats would.
func (f *fakeChats) all(t *testing.T) string {
	t.Helper()
	f.flush()
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := json.Marshal(f.chats)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// withChats gives the fixture a chat store and a clock, and returns a
// function that builds a second service on the same store and panes: keel
// serve after a restart.
func withChats(t *testing.T, f *fixture) (*fakeChats, *clock, func() *Service) {
	t.Helper()
	chats := newFakeChats()
	clk := &clock{t: time.Date(2026, 10, 14, 16, 2, 0, 0, time.UTC)}
	chats.flush = func() { f.svc.FlushSaves() }
	build := func() *Service {
		if f.svc != nil {
			f.svc.FlushSaves() // what the old keel serve queued reaches the store
		}
		svc, err := New(f.st, f.panes, f.log, Options{Chats: chats, Now: clk.now, Factory: func(string, string) (provider.Provider, error) { return f.fake, nil }})
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	f.svc = build()
	return chats, clk, build
}

const proposalReply = "Docker's build cache is big.\n```keel-proposal\n{\"pane\": 1, \"command\": \"docker builder prune --filter until=72h -f\", \"risk\": \"changes\"}\n```"

// A conversation is saved after the answer, redacted: no vault secret, no
// pane text, only what the provider saw and said.
func TestSessionSavedRedacted(t *testing.T) {
	f := newFixture(t, proposalReply)
	chats, _, _ := withChats(t, f)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "my password is " + vaultPass + ", why is the disk full?", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Title == "" || strings.Contains(resp.Title, vaultPass) {
		t.Fatalf("title %q", resp.Title)
	}
	saved := chats.all(t)
	for _, leak := range []string{vaultPass, geminiKey, "Sup3rS3cretValue99", "10:00 up 3 days", "cat .env"} {
		if strings.Contains(saved, leak) {
			t.Errorf("saved conversation holds %q", leak)
		}
	}
	for _, want := range []string{"why is the disk full", "docker builder prune", "Docker's build cache is big", "web-1"} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved conversation lacks %q", want)
		}
	}
	list, err := f.svc.ListSessions()
	if err != nil || len(list.Sessions) != 1 {
		t.Fatalf("list %+v %v", list, err)
	}
	if s := list.Sessions[0]; s.Proposals != 1 || len(s.Hosts) != 1 || s.Hosts[0] != "web-1" {
		t.Fatalf("summary %+v", s)
	}
}

// After a restart a resumed conversation's proposals are Expired: they
// cannot be typed. The next ask carries the earlier question and reply,
// and reads panes again under the scope rules as they are now.
func TestResumedConversation(t *testing.T) {
	f := newFixture(t, proposalReply)
	chats, clk, build := withChats(t, f)
	first := askOneIn(t, f, "s-web")
	clk.add(48 * time.Hour)

	f.svc = build() // keel serve restarted
	tr, err := f.svc.Transcript(conv)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Turns) != 1 || len(tr.Turns[0].Proposals) != 1 {
		t.Fatalf("transcript %+v", tr)
	}
	old := tr.Turns[0].Proposals[0]
	if !old.Expired || old.ID != first.ID || old.Bytes != first.Bytes || old.ProposedAt == nil {
		t.Fatalf("resumed proposal %+v, want expired copy of %+v", old, first)
	}
	if len(tr.Turns[0].Trace) == 0 || tr.Turns[0].Trace[0].Status != "read" {
		t.Fatalf("trace %+v", tr.Turns[0].Trace)
	}
	err = f.svc.AuthorizeType(TypeRequest{ConversationID: conv, ProposalID: old.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: old.Bytes, Shift: true})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("typing an expired proposal: %v, want ErrNotFound", err)
	}
	if _, err := f.svc.EditProposal(conv, old.ID, "ls"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("editing an expired proposal: %v", err)
	}

	// The host no longer lets the agent read: the resumed ask must not
	// read it, whatever the conversation read before.
	f.st.mu.Lock()
	f.st.hosts[0].AgentReadable = bptr(false)
	f.st.mu.Unlock()
	f.panes.mu.Lock()
	f.panes.reads = nil
	f.panes.mu.Unlock()
	f.fake.Reply = provider.Response{Text: "fine"}
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "and now?", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.panes.reads) != 0 {
		t.Fatalf("resumed ask read %v from a host that no longer allows it", f.panes.reads)
	}
	if resp.Trace[0].Status != "skipped" || resp.Turn != 2 {
		t.Fatalf("resumed ask %+v", resp)
	}
	reqs := f.fake.Requests()
	last := reqs[len(reqs)-1]
	if len(last.Messages) != 3 || !strings.Contains(last.Messages[0].Text.String(), "why is the disk full") {
		t.Fatalf("resumed ask carried %d messages", len(last.Messages))
	}
	// History holds questions and replies only, never pane text.
	if strings.Contains(last.Messages[0].Text.String()+last.Messages[1].Text.String()+last.Messages[2].Text.String(), "10:00 up 3 days") {
		t.Fatal("a resumed ask replayed pane text")
	}
	chats.flush()
	saved, _ := chats.GetAgentChat(conv)
	if len(saved.Turns) != 2 || !saved.UpdatedAt.Equal(clk.now()) {
		t.Fatalf("saved after resume: %d turns at %v", len(saved.Turns), saved.UpdatedAt)
	}
}

func askOneIn(t *testing.T, f *fixture, paneID string) Proposal {
	t.Helper()
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "why is the disk full?", Panes: []string{paneID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Proposals) != 1 {
		t.Fatalf("proposals %+v", resp.Proposals)
	}
	return resp.Proposals[0]
}

// Propose again makes a new proposal through the normal path: rated
// again, the host's production flag now, typeable only with the usual
// checks, and logged as a suggestion.
func TestProposeAgain(t *testing.T) {
	f := newFixture(t, proposalReply)
	_, _, build := withChats(t, f)
	old := askOneIn(t, f, "s-db") // prod-db-01: production
	f.svc = build()

	again, err := f.svc.ProposeAgain(conv, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == old.ID || again.Expired || again.SessionID != "s-db" || !again.Production || !again.NeedsShift || again.Risk == agent.RiskReadOnly {
		t.Fatalf("again %+v", again)
	}
	req := TypeRequest{ConversationID: conv, ProposalID: again.ID, SessionID: "s-db", Role: agent.RoleDriver, Bytes: again.Bytes}
	if err := f.svc.AuthorizeType(req); !errors.Is(err, ErrNeedsShift) {
		t.Fatalf("plain Type on production: %v", err)
	}
	req.Shift = true
	if err := f.svc.AuthorizeType(req); err != nil {
		t.Fatalf("⇧ Type: %v", err)
	}
	f.svc.TypeDone(conv, again.ID)
	if n := len(f.logKinds(agent.EventSuggest)); n < 2 {
		t.Fatalf("agent.suggest entries = %d, want the original and the new one", n)
	}

	tr, _ := f.svc.Transcript(conv)
	lastTurn := tr.Turns[len(tr.Turns)-1]
	if !lastTurn.Again || len(lastTurn.Proposals) != 1 || lastTurn.Proposals[0].Expired || lastTurn.Proposals[0].TypedAt == nil {
		t.Fatalf("transcript after propose again %+v", lastTurn)
	}
	if !tr.Turns[0].Proposals[0].Expired {
		t.Fatal("the original stays expired")
	}

	// The pane closed and the host was deleted: copy only, and production
	// (fail closed).
	delete(f.panes.panes, "s-db")
	f.st.mu.Lock()
	f.st.hosts = f.st.hosts[:1]
	f.st.mu.Unlock()
	gone, err := f.svc.ProposeAgain(conv, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gone.SessionID != "" || !gone.Production || !gone.NeedsShift {
		t.Fatalf("proposal for a gone pane %+v", gone)
	}
	if _, err := f.svc.ProposeAgain(conv, "p_nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown proposal: %v", err)
	}
}

// What the user types into an edit is saved redacted.
func TestEditedProposalSavedRedacted(t *testing.T) {
	f := newFixture(t, proposalReply)
	chats, _, _ := withChats(t, f)
	p := askOneIn(t, f, "s-web")
	if _, err := f.svc.EditProposal(conv, p.ID, "mysql -p"+vaultPass); err != nil {
		t.Fatal(err)
	}
	saved := chats.all(t)
	if strings.Contains(saved, vaultPass) {
		t.Fatal("an edited command with a vault secret was saved as typed")
	}
	if !strings.Contains(saved, "mysql -p") || !strings.Contains(saved, `"edited":true`) {
		t.Fatalf("edit not saved: %s", saved)
	}
}

// Retention: older than historyDays is pruned; 0 saves nothing and prunes
// everything saved, while the open conversation still works in memory.
func TestRetention(t *testing.T) {
	f := newFixture(t, "ok")
	chats, clk, _ := withChats(t, f)
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: "conv-old-0001", Question: "old"}); err != nil {
		t.Fatal(err)
	}
	clk.add(20 * 24 * time.Hour)
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: "conv-new-0001", Question: "new"}); err != nil {
		t.Fatal(err)
	}
	clk.add(15 * 24 * time.Hour) // old is 35 days, new 15
	// Past the retention it cannot be opened even before the prune.
	if _, err := f.svc.Transcript("conv-old-0001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired transcript: %v", err)
	}
	f.svc.FlushSaves()
	n, err := f.svc.Prune()
	if err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	list, _ := f.svc.ListSessions()
	if len(list.Sessions) != 1 || list.Sessions[0].ID != "conv-new-0001" || list.HistoryDays != 30 {
		t.Fatalf("after prune %+v", list)
	}

	f.st.settings.HistoryDays = 0
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: "conv-open-001", Question: "temporary"}); err != nil {
		t.Fatal(err)
	}
	chats.flush()
	if _, err := chats.GetAgentChat("conv-open-001"); err == nil {
		t.Fatal("history off saved a conversation")
	}
	list, _ = f.svc.ListSessions()
	if len(list.Sessions) != 0 || len(chats.chats) != 0 {
		t.Fatalf("history off still lists or keeps %+v / %d", list, len(chats.chats))
	}
	if tr, err := f.svc.Transcript("conv-open-001"); err != nil || len(tr.Turns) != 1 || tr.Saved {
		t.Fatalf("open conversation with history off: %+v %v", tr, err)
	}
}

func TestRenameAndDelete(t *testing.T) {
	f := newFixture(t, "ok")
	chats, _, build := withChats(t, f)
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "first question"}); err != nil {
		t.Fatal(err)
	}
	title, err := f.svc.Rename(conv, "Replica lag "+vaultPass)
	if err != nil || strings.Contains(title, vaultPass) || !strings.HasPrefix(title, "Replica lag") {
		t.Fatalf("rename %q %v", title, err)
	}
	chats.flush()
	if c, _ := chats.GetAgentChat(conv); c.Title != title {
		t.Fatalf("saved title %q", c.Title)
	}
	if _, err := f.svc.Rename(conv, "  "); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("empty rename: %v", err)
	}
	f.svc = build()
	if _, err := f.svc.Rename("conv-missing-1", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename missing: %v", err)
	}
	if err := f.svc.Delete(conv); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Transcript(conv); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted conversation: %v", err)
	}
	// Asking with the id again starts a fresh conversation.
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "again"}); err != nil {
		t.Fatal(err)
	}
	reqs := f.fake.Requests()
	if n := len(reqs[len(reqs)-1].Messages); n != 1 {
		t.Fatalf("after delete the ask carried %d messages", n)
	}
	n, err := f.svc.DeleteAll()
	if err != nil || n != 1 || len(chats.chats) != 0 {
		t.Fatalf("delete all %d %v", n, err)
	}
}

func (f *fakeChats) set(fn func(*fakeChats)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// Propose again targets the pane only while the agent may read its host
// now (§6.2); otherwise the new proposal is Copy only.
func TestProposeAgainNeedsReadableHost(t *testing.T) {
	f := newFixture(t, proposalReply)
	_, _, build := withChats(t, f)
	old := askOneIn(t, f, "s-web")
	f.svc = build()
	f.st.mu.Lock()
	f.st.hosts[0].AgentReadable = bptr(false) // web-1: "Agent can read" off
	f.st.mu.Unlock()
	again, err := f.svc.ProposeAgain(conv, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.SessionID != "" {
		t.Fatalf("proposal targets %q on a host the agent may no longer read", again.SessionID)
	}
	err = f.svc.AuthorizeType(TypeRequest{ConversationID: conv, ProposalID: again.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: again.Bytes, Shift: true})
	if !errors.Is(err, ErrNoTarget) {
		t.Fatalf("typing it: %v, want ErrNoTarget", err)
	}
}

// A keel.chats that cannot be read: asking still works (in memory), the
// list says it is unreadable, keel.log says why once, and Delete all
// starts over.
func TestUnreadableChats(t *testing.T) {
	f := newFixture(t, "ok")
	chats, _, _ := withChats(t, f)
	chats.set(func(c *fakeChats) { c.broken = true })
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q one"}); err != nil {
		t.Fatalf("ask with an unreadable keel.chats: %v", err)
	}
	f.svc.FlushSaves()
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q two"})
	if err != nil || !strings.Contains(resp.SaveError, "cannot be read") {
		t.Fatalf("second ask: %v, saveError %q", err, resp.SaveError)
	}
	f.svc.FlushSaves()
	list, err := f.svc.ListSessions()
	if err != nil || !list.Unreadable || list.SaveError == "" || len(list.Sessions) != 0 {
		t.Fatalf("list %+v %v", list, err)
	}
	var notes []string
	for _, e := range f.logKinds("agent") {
		notes = append(notes, e.Message)
	}
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "agent conversation not saved: ") || strings.Contains(notes[0], "q one") {
		t.Fatalf("log %q, want one line without content", notes)
	}
	if err := f.svc.Delete(conv); !errors.Is(err, store.ErrChatsUnreadable) {
		t.Fatalf("delete one must report the store error: %v", err)
	}
	if _, err := f.svc.DeleteAll(); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: "conv-after-0001", Question: "fresh"}); err != nil {
		t.Fatal(err)
	}
	list, _ = f.svc.ListSessions()
	if list.Unreadable || list.SaveError != "" || len(list.Sessions) != 1 {
		t.Fatalf("after delete all %+v", list)
	}
}

// A failed save (disk full, …) is shown and logged, and clears on the
// next good save.
func TestSaveFailureIsReported(t *testing.T) {
	f := newFixture(t, "ok")
	chats, _, _ := withChats(t, f)
	chats.set(func(c *fakeChats) { c.failPut = errors.New("write keel.chats: no space left on device") })
	_, _ = f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q"})
	f.svc.FlushSaves()
	if list, _ := f.svc.ListSessions(); !strings.Contains(list.SaveError, "no space left") {
		t.Fatalf("list %+v", list)
	}
	chats.set(func(c *fakeChats) { c.failPut = nil })
	_, _ = f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q2"})
	f.svc.FlushSaves()
	if list, _ := f.svc.ListSessions(); list.SaveError != "" || len(list.Sessions) != 1 {
		t.Fatalf("after a good save %+v", list)
	}
}

// TypeDone runs on the session socket's read loop: it must not wait for
// the save.
func TestTypeDoneDoesNotWaitForTheSave(t *testing.T) {
	f := newFixture(t, proposalReply)
	chats, _, _ := withChats(t, f)
	p := askOneIn(t, f, "s-web")
	f.svc.FlushSaves()
	gate := make(chan struct{})
	chats.gate = gate
	done := make(chan struct{})
	go func() {
		f.svc.TypeDone(conv, p.ID)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TypeDone waited for the disk")
	}
	close(gate)
	f.svc.FlushSaves()
	saved, _ := chats.GetAgentChat(conv)
	if saved.Turns[0].Proposals[0].TypedAt == nil {
		t.Fatal("typed time not saved")
	}
}

// The proposal's reason is kept redacted too.
func TestWhySavedRedacted(t *testing.T) {
	f := newFixture(t, "```keel-proposal\n{\"pane\": 1, \"command\": \"uptime\", \"why\": \"the password is "+vaultPass+"\"}\n```")
	chats, _, _ := withChats(t, f)
	askOneIn(t, f, "s-web")
	if strings.Contains(chats.all(t), vaultPass) {
		t.Fatal("a vault secret in the proposal's reason was saved")
	}
}
