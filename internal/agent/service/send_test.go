package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/redact"
	"keel/internal/applog"
	"keel/internal/store"
)

// §6 rule 4, by type: what send can carry holds terminal text only as
// redact.Safe, and a Safe cannot be made from a string outside the redact
// package. A new field that could carry raw text fails here.
func TestSendCarriesOnlySafeText(t *testing.T) {
	safe := reflect.TypeOf(redact.Safe{})
	for i := 0; i < safe.NumField(); i++ {
		if safe.Field(i).IsExported() {
			t.Fatalf("redact.Safe.%s is exported: a Safe could be built from any string", safe.Field(i).Name)
		}
	}
	// Strings that are not text from a pane: names Keel picks from its own
	// tables, and the judge's questions, written in Keel's source.
	plain := map[string]bool{
		"Request.Model": true, "Request.Thinking": true, "Message.Role": true,
		"Question.Type": true, "Question.Instructions": true, "Question.True": true, "Question.False": true,
		"Option.Key": true, "Option.Description": true,
	}
	var walk func(owner string, typ reflect.Type)
	walk = func(owner string, typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(owner, typ.Elem())
		case reflect.Map:
			if typ.Key().Kind() != reflect.String {
				t.Errorf("%s: a map keyed by %s", owner, typ.Key())
			}
			walk(owner, typ.Elem())
		case reflect.Struct:
			if typ == safe {
				return
			}
			for i := 0; i < typ.NumField(); i++ {
				walk(typ.Name()+"."+typ.Field(i).Name, typ.Field(i).Type)
			}
		case reflect.String:
			if !plain[owner] {
				t.Errorf("%s is a plain string in a provider request: text must be redact.Safe", owner)
			}
		case reflect.Int:
		default:
			t.Errorf("%s: a %s in a provider request", owner, typ.Kind())
		}
	}
	out := reflect.TypeOf(outbound{})
	for _, name := range []string{"model", "judge"} {
		f, ok := out.FieldByName(name)
		if !ok {
			t.Fatalf("outbound.%s is gone: the check is out of date", name)
		}
		walk("outbound."+name, f.Type)
	}
	if out.NumField() != 4 {
		t.Errorf("outbound has %d fields: a new one must be checked here", out.NumField())
	}
}

// failLog is keel.log that can stop taking entries.
type failLog struct {
	*applog.Book
	mu   sync.Mutex
	fail bool
}

func (l *failLog) Append(e applog.Entry) error {
	l.mu.Lock()
	fail := l.fail
	l.mu.Unlock()
	if fail {
		return errors.New("disk full")
	}
	return l.Book.Append(e)
}

func (l *failLog) set(fail bool) {
	l.mu.Lock()
	l.fail = fail
	l.mu.Unlock()
}

// §6 rule 7: a question with no pane in scope is logged like any other
// request, and when keel.log cannot be written nothing is read, sent or
// typed.
func TestNothingLeavesWithoutALogEntry(t *testing.T) {
	f := newFixture(t, "an answer")
	log := &failLog{Book: f.log}
	svc, err := New(f.st, f.panes, log, Options{Factory: func(string, string) (provider.Provider, error) { return f.fake, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc

	if _, err := svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "what is a pipe?"}); err != nil {
		t.Fatal(err)
	}
	reads := f.logKinds(agent.EventRead)
	if len(reads) != 1 || !strings.Contains(reads[0].Message, "a question with 0 panes in scope to Google Gemini") || reads[0].Source != "gemini" {
		t.Fatalf("a question with no pane: agent.read = %+v", reads)
	}

	log.set(true)
	sent := len(f.fake.Requests())
	_, err = svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "and a socket?", Panes: []string{"s-web"}})
	if !errors.Is(err, agent.ErrAuditLog) {
		t.Fatalf("ask with keel.log failing: %v", err)
	}
	if n := len(f.fake.Requests()); n != sent {
		t.Fatalf("%d requests were sent unlogged", n-sent)
	}
	log.set(false)
	p := askOne(t, f, "uptime", "s-web")
	req := TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: p.Bytes}
	log.set(true)
	if err := svc.AuthorizeType(req); !errors.Is(err, agent.ErrAuditLog) {
		t.Fatalf("type with keel.log failing: %v", err)
	}
	log.set(false)
	if err := svc.AuthorizeType(req); err != nil {
		t.Fatal(err)
	}
	if n := len(f.logKinds(agent.EventType)); n != 1 {
		t.Fatalf("agent.type entries = %d: the refused type must not be logged later", n)
	}
}

// A write that happened and could not be logged is owed: everything is
// refused until its entry is written, and it is written first.
func TestAnUnloggedWriteIsOwed(t *testing.T) {
	f := newSuggestFixture(t, block("ls /tmp", "Lists it.", "readonly"))
	log := &failLog{Book: f.log}
	svc, err := New(f.st, f.panes, log, Options{
		Factory: func(string, string) (provider.Provider, error) { return f.model, nil },
		Judge:   func(string) (provider.Judge, error) { return f.judge, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	s := f.suggest(t, "s-web", "list the temp folder")
	ins := InsertRequest{SuggestionID: s.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: s.Options[0].Bytes}
	g, err := svc.AuthorizeInsert(ins)
	if err != nil {
		t.Fatal(err)
	}
	log.set(true)
	if err := svc.InsertDone(g, nil); !errors.Is(err, agent.ErrAuditLog) {
		t.Fatalf("InsertDone with keel.log failing: %v", err)
	}
	if _, _, err := svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "list it again"}); !errors.Is(err, agent.ErrAuditLog) {
		t.Fatalf("a suggestion while an entry is owed: %v", err)
	}
	log.set(false)
	if n := len(f.entries(agent.EventInsert)); n != 0 {
		t.Fatalf("agent.insert entries = %d before the log came back", n)
	}
	f.suggest(t, "s-web", "list it again")
	if n := len(f.entries(agent.EventInsert)); n != 1 {
		t.Fatalf("agent.insert entries = %d: the owed entry was not written", n)
	}
}

// A type without the dialog fails closed on anything Keel could not find
// out about Jev: only "no key" means Jev is not asked.
func TestDirectFailsClosedWhenTheJudgeCannotBeBuilt(t *testing.T) {
	f := newSuggestFixture(t, "")
	f.model.Reply = provider.Response{Text: "```keel-proposal\n{\"pane\": 1, \"command\": \"uptime\"}\n```"}
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-web"}})
	if err != nil || len(resp.Proposals) != 1 {
		t.Fatalf("ask: %v", err)
	}
	p := resp.Proposals[0]
	req := TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: p.Command}
	f.judge.Choices["risk_0"] = provider.ChoiceAnswer{Choice: "readonly", Probabilities: map[string]float64{"readonly": 0.9}}

	// The vault cannot give the key.
	broken := &brokenKeys{keyStore: f.st}
	svc, err := New(broken, f.panes, f.log, Options{
		Factory: func(string, string) (provider.Provider, error) { return f.model, nil },
		Judge:   func(string) (provider.Judge, error) { return f.judge, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-web"}}); err != nil {
		t.Fatal(err)
	}
	p2 := func() Proposal {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return *svc.convs[conv].proposals[0]
	}()
	req2 := req
	req2.ProposalID = p2.ID
	broken.set(errors.New("vault: cannot read"))
	_, err = svc.AuthorizeDirect(context.Background(), req2, true)
	if reason, ok := NeedsDialog(err); !ok || reason != ReasonJevNone {
		t.Fatalf("a vault error reading the Jev key: %v, want the dialog", err)
	}
	// No key at all: Jev is not asked and the allowlist decides.
	broken.set(nil)
	f.st.setKey("jev", "")
	if _, err := svc.AuthorizeDirect(context.Background(), req2, true); err != nil {
		t.Fatalf("no Jev key: %v", err)
	}
	// With the key back and Jev answering, the first service types it.
	f.st.setKey("jev", jevKey)
	if _, err := f.svc.AuthorizeDirect(context.Background(), req, true); err != nil {
		t.Fatalf("Jev says read-only: %v", err)
	}
}

// brokenKeys is keyStore whose Jev key read can fail.
type brokenKeys struct {
	*keyStore
	bmu sync.Mutex
	err error
}

func (b *brokenKeys) set(err error) {
	b.bmu.Lock()
	b.err = err
	b.bmu.Unlock()
}

func (b *brokenKeys) AgentKey(id string) (string, error) {
	b.bmu.Lock()
	err := b.err
	b.bmu.Unlock()
	if err != nil && id == JudgeProvider {
		return "", err
	}
	return b.keyStore.AgentKey(id)
}

// brokenVars is a store whose group variables cannot be read.
type brokenVars struct {
	*fakeStore
}

func (brokenVars) SetGroupVar(string, string, string) (store.Group, error) {
	return store.Group{}, errors.New("no")
}

func (brokenVars) AllGroupVars() (map[string][]store.GroupVar, error) {
	return nil, errors.New("vault: cannot read the groups")
}

// §6 rule 11: when the values Keel withholds cannot be read, no Redactor
// that needs them is built, so nothing is redacted without them and
// nothing is sent.
func TestNoRedactorWithoutTheWithheldValues(t *testing.T) {
	f := newFixture(t, "an answer")
	svc, err := New(brokenVars{f.st}, f.panes, f.log, Options{Factory: func(string, string) (provider.Provider, error) { return f.fake, nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []redactFor{forRequest, forKept, forSave} {
		if _, err := svc.redactorFor(p); err == nil {
			t.Errorf("purpose %d: a Redactor was built without the group variables", p)
		}
	}
	// A purpose that keeps group variables as written does not read them.
	for _, p := range []redactFor{forKeptCommand, forSaveCommand, forVaultCheck} {
		if _, err := svc.redactorFor(p); err != nil {
			t.Errorf("purpose %d: %v", p, err)
		}
	}
	if _, err := svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-web"}}); err == nil {
		t.Fatal("an Ask went on without the withheld values")
	}
	if n := len(f.fake.Requests()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
	if len(f.panes.reads) != 0 {
		t.Fatalf("panes were read: %v", f.panes.reads)
	}
}
