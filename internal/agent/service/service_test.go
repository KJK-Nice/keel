package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/provider/providertest"
	"keel/internal/applog"
	"keel/internal/store"
)

// --- fakes ---

type fakeStore struct {
	mu        sync.Mutex
	settings  store.AgentSettings
	hosts     []store.Host
	secrets   []string
	key       string
	keyReads  int
	snippets  []store.Snippet
	snipReads int
}

func (f *fakeStore) KnownSecretValues() ([]string, error) { return f.secrets, nil }
func (f *fakeStore) AgentSettings() (store.AgentSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings, nil
}

// setSettings changes the settings under the lock: the service's save
// worker reads them from another goroutine.
func (f *fakeStore) setSettings(fn func(*store.AgentSettings)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.settings)
}
func (f *fakeStore) ListHosts() ([]store.Host, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Host(nil), f.hosts...), nil
}
func (f *fakeStore) ListSnippets() ([]store.Snippet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snipReads++
	return append([]store.Snippet(nil), f.snippets...), nil
}
func (f *fakeStore) AgentKeyConfigured(string) (bool, error) { return f.key != "", nil }
func (f *fakeStore) AgentKey(string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyReads++
	if f.key == "" {
		return "", store.ErrNotFound
	}
	return f.key, nil
}
func (f *fakeStore) setProduction(id string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.hosts {
		if f.hosts[i].ID == id {
			f.hosts[i].Production = &on
		}
	}
}

type pane struct {
	host string
	text string
}

type fakePanes struct {
	mu    sync.Mutex
	panes map[string]pane
	reads []string
}

func (f *fakePanes) Pane(id string) (agent.Pane, bool) {
	p, ok := f.panes[id]
	if !ok {
		return agent.Pane{}, false
	}
	return agent.Pane{SessionID: id, HostID: p.host}, true
}

func (f *fakePanes) ReadScrollback(id string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, id)
	p, ok := f.panes[id]
	if !ok {
		return nil, errors.New("no session")
	}
	return []byte(p.text), nil
}

func bptr(b bool) *bool { return &b }

const (
	geminiKey  = "AIzaSyD-test-gemini-key-0123456789abcd"
	vaultPass  = "vault-host-password-7f3c9e"
	vaultPass2 = "identity-passphrase-QmV9"
)

type fixture struct {
	st    *fakeStore
	panes *fakePanes
	fake  *providertest.Fake
	log   *applog.Book
	path  string
	svc   *Service
}

func newFixture(t *testing.T, reply string) *fixture {
	t.Helper()
	set := store.DefaultAgentSettings()
	set.ScrollbackLines = 50
	st := &fakeStore{
		settings: set,
		hosts: []store.Host{
			{ID: "web", Name: "web-1", Address: "web.example.com", AgentReadable: bptr(true)},
			{ID: "db", Name: "prod-db-01", Address: "10.0.0.9", AgentReadable: bptr(true), Production: bptr(true)},
			{ID: "vault", Name: "secrets-box", Address: "s.example.com", AgentReadable: bptr(false)},
		},
		secrets: []string{vaultPass, vaultPass2, geminiKey},
		key:     geminiKey,
	}
	panes := &fakePanes{panes: map[string]pane{
		"s-web": {host: "web", text: "$ cat .env\nDB_PASSWORD=Sup3rS3cretValue99\nlogin with " + vaultPass + "\nGEMINI=" + geminiKey + "\n$ uptime\n 10:00 up 3 days\n"},
		"s-db":  {host: "db", text: "postgres replication lag 14MB\n"},
		"s-sec": {host: "vault", text: "UNREADABLE-HOST-MARKER\n"},
		"s-out": {host: "web", text: "OUT-OF-SCOPE-MARKER\n"},
	}}
	fake := &providertest.Fake{Reply: provider.Response{Text: reply}}
	path := filepath.Join(t.TempDir(), "keel.log")
	log := applog.New(path)
	svc, err := New(st, panes, log, Options{Factory: func(id, key string) (provider.Provider, error) {
		if key != geminiKey {
			t.Errorf("factory got key %q", key)
		}
		return fake, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{st: st, panes: panes, fake: fake, log: log, path: path, svc: svc}
}

func (f *fixture) logKinds(kind string) []applog.Entry {
	var out []applog.Entry
	for _, e := range f.log.Recent(1000) {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// requestBodies is everything the provider received, as JSON, so a test
// can search it the way a network capture would.
func (f *fixture) requestBodies(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, r := range f.fake.Requests() {
		msgs := []string{}
		for _, m := range r.Messages {
			msgs = append(msgs, string(m.Role)+": "+m.Text.String())
		}
		j, err := json.Marshal(map[string]any{"model": r.Model, "system": r.System.String(), "messages": msgs})
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
	}
	return b.String()
}

const conv = "conv-test-0001"

// --- §6.2 and §6.4 ---

// §6.2: only panes in the request's scope are read, only on hosts with
// "Agent can read" on; §6.7: each read is logged.
func TestAskReadsOnlyScopedReadablePanes(t *testing.T) {
	f := newFixture(t, "ok")
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "why slow?", Panes: []string{"s-web", "s-sec", "s-gone"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.panes.reads; len(got) != 1 || got[0] != "s-web" {
		t.Fatalf("scrollback reads = %v, want only s-web", got)
	}
	body := f.requestBodies(t)
	for _, marker := range []string{"UNREADABLE-HOST-MARKER", "OUT-OF-SCOPE-MARKER", "replication lag"} {
		if strings.Contains(body, marker) {
			t.Errorf("provider request holds %q from a pane out of scope", marker)
		}
	}
	if !strings.Contains(body, "10:00 up 3 days") {
		t.Error("the scoped pane's text did not reach the request")
	}
	want := map[string]string{"s-web": "read", "s-sec": "skipped", "s-gone": "skipped"}
	for _, st := range resp.Trace {
		if want[st.SessionID] != st.Status {
			t.Errorf("trace %s = %s (%s), want %s", st.SessionID, st.Status, st.Reason, want[st.SessionID])
		}
	}
	if resp.Trace[1].Reason != "not-readable" || resp.Trace[2].Reason != "not-open" {
		t.Errorf("skip reasons = %q, %q", resp.Trace[1].Reason, resp.Trace[2].Reason)
	}
	reads := f.logKinds(agent.EventRead)
	if len(reads) != 1 || reads[0].SessionID != "s-web" || reads[0].Lines != 6 {
		t.Fatalf("agent.read log = %+v", reads)
	}
}

// §6.4: redaction runs before anything leaves; the fake provider records
// the request and it holds placeholders, not the values.
func TestAskRedactsBeforeTheProvider(t *testing.T) {
	f := newFixture(t, "ok")
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "my token is ghp_abcdefghijklmnopqrstuvwxyz0123456789 is it leaked?", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	body := f.requestBodies(t)
	for _, secret := range []string{"Sup3rS3cretValue99", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"} {
		if strings.Contains(body, secret) {
			t.Errorf("provider request holds %q", secret)
		}
	}
	if !strings.Contains(body, "[secret:") {
		t.Error("no placeholder in the request")
	}
	if resp.Trace[0].Redactions == nil || resp.QuestionRedactions == nil {
		t.Errorf("redaction counts missing: trace %v, question %v", resp.Trace[0].Redactions, resp.QuestionRedactions)
	}
	if e := f.logKinds(agent.EventRead); len(e) != 1 || len(e[0].Redactions) == 0 {
		t.Errorf("agent.read must carry redaction counts: %+v", e)
	}
	raw, _ := os.ReadFile(f.path)
	if strings.Contains(string(raw), "Sup3rS3cretValue99") || strings.Contains(string(raw), "10:00 up") {
		t.Error("keel.log holds pane content")
	}
}

// §6.3: no vault value (host passwords, passphrases, the provider key)
// is ever in a provider request, even when it appears in a pane or in the
// question. The key is read once per ask, for the client only.
func TestAskNeverSendsVaultContents(t *testing.T) {
	f := newFixture(t, "ok")
	_, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "is " + vaultPass2 + " my passphrase?", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	body := f.requestBodies(t)
	for _, v := range []string{vaultPass, vaultPass2, geminiKey} {
		if strings.Contains(body, v) {
			t.Errorf("provider request holds vault value %q", v)
		}
	}
	if f.st.keyReads != 1 {
		t.Errorf("key read %d times, want 1", f.st.keyReads)
	}
}

func TestAskWithoutKeyCallsNothing(t *testing.T) {
	f := newFixture(t, "ok")
	f.st.key = ""
	_, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "hi", Panes: []string{"s-web"}})
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
	if len(f.fake.Requests()) != 0 || len(f.panes.reads) != 0 || len(f.logKinds(agent.EventRead)) != 0 {
		t.Fatal("an ask without a key read a pane or called the provider")
	}
}

func TestProviderErrorsAreScrubbedOfTheKey(t *testing.T) {
	f := newFixture(t, "")
	f.fake.Err = errors.New("gemini: HTTP 400: API key " + geminiKey + " not valid")
	_, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "hi"})
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want ProviderError", err)
	}
	if strings.Contains(err.Error(), geminiKey) || !strings.Contains(err.Error(), "[key]") {
		t.Fatalf("error not scrubbed: %v", err)
	}
}

type blocking struct{}

func (blocking) ID() string { return "block" }
func (blocking) Generate(ctx context.Context, _ provider.Request) (provider.Response, error) {
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
}

func TestAskTimeoutAndCancel(t *testing.T) {
	f := newFixture(t, "")
	svc, _ := New(f.st, f.panes, f.log, Options{Timeout: 30 * time.Millisecond, Factory: func(string, string) (provider.Provider, error) { return blocking{}, nil }})
	_, err := svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "hi"})
	var pe *ProviderError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("timeout err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	svc2, _ := New(f.st, f.panes, f.log, Options{Timeout: time.Minute, Factory: func(string, string) (provider.Provider, error) { return blocking{}, nil }})
	if _, err := svc2.Ask(ctx, AskRequest{ConversationID: conv, Question: "hi"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err = %v", err)
	}
	// The conversation is free again after a failed ask.
	if _, err := svc2.begin(conv); err != nil {
		t.Fatalf("conversation still busy: %v", err)
	}
}

func TestOneAskAtATimePerConversation(t *testing.T) {
	f := newFixture(t, "")
	release := make(chan struct{})
	started := make(chan struct{})
	svc, _ := New(f.st, f.panes, f.log, Options{Factory: func(string, string) (provider.Provider, error) {
		return gate{started: started, release: release}, nil
	}})
	done := make(chan error, 1)
	go func() {
		_, err := svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "one"})
		done <- err
	}()
	<-started
	if _, err := svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "two"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second ask err = %v, want ErrBusy", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type gate struct{ started, release chan struct{} }

func (gate) ID() string { return "gate" }
func (g gate) Generate(context.Context, provider.Request) (provider.Response, error) {
	close(g.started)
	<-g.release
	return provider.Response{Text: "done"}, nil
}

func TestBadAsks(t *testing.T) {
	f := newFixture(t, "ok")
	for _, req := range []AskRequest{
		{ConversationID: "short", Question: "hi"},
		{ConversationID: conv, Question: "  "},
		{ConversationID: conv, Question: strings.Repeat("a", maxQuestionBytes+1)},
		{ConversationID: conv, Question: "hi", Panes: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}},
	} {
		if _, err := f.svc.Ask(context.Background(), req); !errors.Is(err, ErrBadRequest) {
			t.Errorf("Ask(%+v) err = %v, want ErrBadRequest", req.ConversationID, err)
		}
	}
}

// --- proposals ---

const twoProposals = "Lag is from the WAL sender.\n\n```keel-proposal\n" +
	`{"pane": 2, "command": "sudo -u postgres psql -xc \"select * from pg_stat_replication\"", "why": "Shows the replica state", "risk": "readonly"}` +
	"\n```\n\nIf that is stuck:\n\n```keel-proposal\n" +
	`{"pane": 1, "command": "ls /var/log", "why": "List logs", "risk": "destructive"}` +
	"\n```\n```keel-proposal\nnot json\n```\n" +
	"```keel-proposal\n" + `{"pane": 1, "command": "echo \u001b[31m"}` + "\n```\n"

func TestProposalsAreParsedRatedAndTargeted(t *testing.T) {
	f := newFixture(t, twoProposals)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "why lag", Panes: []string{"s-web", "s-db"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.Answer, "keel-proposal") || !strings.Contains(resp.Answer, "Lag is from the WAL sender.") {
		t.Fatalf("answer = %q", resp.Answer)
	}
	if len(resp.Proposals) != 2 {
		t.Fatalf("proposals = %+v, want 2 (bad JSON and control bytes dropped)", resp.Proposals)
	}
	p0, p1 := resp.Proposals[0], resp.Proposals[1]
	if p0.SessionID != "s-db" || p0.Host != "prod-db-01" || !p0.Production || p0.Risk != agent.RiskReadOnly || p0.NeedsShift {
		t.Errorf("proposal 0 = %+v", p0)
	}
	if p0.Bytes != "\x15"+p0.Command+"\n" {
		t.Errorf("bytes = %q", p0.Bytes)
	}
	// The model rated ls as destructive: stricter than the rule, so kept.
	if p1.SessionID != "s-web" || p1.Risk != agent.RiskDestructive || !p1.NeedsShift {
		t.Errorf("proposal 1 = %+v", p1)
	}
	if got := f.logKinds(agent.EventSuggest); len(got) != 2 {
		t.Errorf("agent.suggest entries = %d, want 2", len(got))
	}
}

func TestModelCannotLoosenTheRating(t *testing.T) {
	reply := "```keel-proposal\n{\"pane\": 1, \"command\": \"rm -rf /srv/data\", \"risk\": \"readonly\"}\n```"
	f := newFixture(t, reply)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "clean up", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	if p := resp.Proposals[0]; p.Risk != agent.RiskDestructive || !p.NeedsShift {
		t.Fatalf("proposal = %+v; the model's readonly must not loosen rm -rf", p)
	}
}

func TestScriptedFakeSpeaksTheProposalFormat(t *testing.T) {
	f := newFixture(t, "")
	scripted := providertest.NewScripted()
	svc, _ := New(f.st, f.panes, f.log, Options{Factory: func(string, string) (provider.Provider, error) { return scripted, nil }})
	for q, want := range map[string]agent.Risk{"what now": agent.RiskReadOnly, "touch it": agent.RiskChanges, "delete it": agent.RiskDestructive} {
		resp, err := svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: q, Panes: []string{"s-web"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Proposals) != 1 || resp.Proposals[0].Risk != want || resp.Proposals[0].SessionID != "s-web" {
			t.Fatalf("%q: proposals = %+v", q, resp.Proposals)
		}
		if !strings.Contains(resp.Answer, "1 pane excerpt") || strings.Contains(resp.Answer, "0 redacted") {
			t.Fatalf("%q: answer = %q", q, resp.Answer)
		}
	}
	for _, r := range scripted.Requests() {
		for _, m := range r.Messages {
			if strings.Contains(m.Text.String(), "Sup3rS3cretValue99") {
				t.Fatal("the fake received an unredacted secret")
			}
		}
	}
}

func TestFromEnvOnlyFakeSwitches(t *testing.T) {
	for _, v := range []string{"", "gemini", "FAKE", "fake ", "http://example.com"} {
		if _, _, test := FromEnv(func(string) string { return v }); test {
			t.Errorf("KEEL_AGENT_PROVIDER=%q selected the fake", v)
		}
	}
	fac, judge, test := FromEnv(func(k string) string {
		if k == TestProviderEnv {
			return "fake"
		}
		return ""
	})
	if !test {
		t.Fatal("fake not selected")
	}
	p, err := fac("gemini", "any")
	if err != nil || p.ID() != "fake" {
		t.Fatalf("fake factory = %v, %v", p, err)
	}
	j, err := judge("any")
	if err != nil || j.ID() != "fake-judge" {
		t.Fatalf("fake judge = %v, %v", j, err)
	}
}

func TestHistoryIsBoundedAndReset(t *testing.T) {
	f := newFixture(t, "answer")
	for i := 0; i < maxHistoryMessages; i++ {
		if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	reqs := f.fake.Requests()
	last := reqs[len(reqs)-1]
	if len(last.Messages) > maxHistoryMessages+1 {
		t.Fatalf("request carried %d messages", len(last.Messages))
	}
	_ = f.svc.Delete(conv)
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "fresh"}); err != nil {
		t.Fatal(err)
	}
	reqs = f.fake.Requests()
	if n := len(reqs[len(reqs)-1].Messages); n != 1 {
		t.Fatalf("after reset the request carried %d messages, want 1", n)
	}
}

// --- typing (§6.1, §6.5, §6.6, §6.7) ---

func askOne(t *testing.T, f *fixture, cmd string, paneID string) Proposal {
	t.Helper()
	f.fake.Reply = provider.Response{Text: "```keel-proposal\n{\"pane\": 1, \"command\": " + jsonString(cmd) + "}\n```"}
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{paneID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Proposals) != 1 {
		t.Fatalf("proposals = %+v", resp.Proposals)
	}
	return resp.Proposals[0]
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestAuthorizeTypeRules(t *testing.T) {
	f := newFixture(t, "")
	ro := askOne(t, f, "uptime", "s-db")
	ch := askOne(t, f, "systemctl restart postgresql", "s-db")
	de := askOne(t, f, "rm -rf /tmp/x", "s-web")

	type tc struct {
		name string
		req  TypeRequest
		want error
	}
	typeReq := func(p Proposal, role agent.Role, shift bool) TypeRequest {
		return TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: p.SessionID, Role: role, Bytes: p.Bytes, Shift: shift}
	}
	cases := []tc{
		{"follower is refused (§6.6)", typeReq(ro, agent.RoleFollower, false), agent.ErrFollower},
		{"owner not driving is refused", typeReq(ro, agent.RoleOwner, false), agent.ErrNotDriving},
		{"changes on production needs shift (§6.5)", typeReq(ch, agent.RoleDriver, false), ErrNeedsShift},
		{"destructive needs shift anywhere", typeReq(de, agent.RoleDriver, false), ErrNeedsShift},
		{"other bytes are refused", func() TypeRequest {
			r := typeReq(ro, agent.RoleDriver, false)
			r.Bytes = "uptime; rm -rf /\n"
			return r
		}(), ErrChanged},
		{"other pane is refused", func() TypeRequest { r := typeReq(ro, agent.RoleDriver, false); r.SessionID = "s-web"; return r }(), ErrWrongPane},
		{"unknown proposal", func() TypeRequest { r := typeReq(ro, agent.RoleDriver, false); r.ProposalID = "p_nope"; return r }(), ErrNotFound},
	}
	for _, c := range cases {
		if err := f.svc.AuthorizeType(c.req); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if n := len(f.logKinds(agent.EventType)); n != 0 {
		t.Fatalf("refused types were logged as agent.type (%d)", n)
	}
	for _, r := range []TypeRequest{typeReq(ro, agent.RoleDriver, false), typeReq(ch, agent.RoleDriver, true), typeReq(de, agent.RoleDriver, true)} {
		if err := f.svc.AuthorizeType(r); err != nil {
			t.Fatalf("allowed type refused: %v", err)
		}
	}
	types := f.logKinds(agent.EventType)
	if len(types) != 3 || types[2].SessionID != "s-db" || types[2].Bytes != len("\x15uptime\n") || types[2].Lines != 1 { // newest first
		t.Fatalf("agent.type log = %+v", types)
	}
	// Production is read at type time: turning it on later still gates.
	f.st.setProduction("web", true)
	ch2 := askOne(t, f, "touch /tmp/y", "s-web")
	if err := f.svc.AuthorizeType(typeReq(ch2, agent.RoleDriver, false)); !errors.Is(err, ErrNeedsShift) {
		t.Fatalf("err = %v, want ErrNeedsShift after production turned on", err)
	}
}

func TestEditProposalRatesAgain(t *testing.T) {
	f := newFixture(t, "")
	p := askOne(t, f, "ls /tmp", "s-web")
	if p.Risk != agent.RiskReadOnly {
		t.Fatalf("risk = %s", p.Risk)
	}
	ed, err := f.svc.EditProposal(conv, p.ID, "rm -rf /tmp/*")
	if err != nil {
		t.Fatal(err)
	}
	if ed.Risk != agent.RiskDestructive || !ed.NeedsShift || !ed.Edited || ed.Bytes != "\x15rm -rf /tmp/*\n" {
		t.Fatalf("edited = %+v", ed)
	}
	// The old bytes no longer authorize.
	if err := f.svc.AuthorizeType(TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: "\x15ls /tmp\n"}); !errors.Is(err, ErrChanged) {
		t.Fatalf("old bytes err = %v", err)
	}
	if _, err := f.svc.EditProposal(conv, p.ID, "echo \x1b[2J"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("control bytes err = %v", err)
	}
}

func TestParseResponseLimits(t *testing.T) {
	block := "```keel-proposal\n{\"pane\":1,\"command\":\"uptime\"}\n```\n"
	_, props := parseResponse(strings.Repeat(block, 5))
	if len(props) != maxProposals {
		t.Fatalf("got %d proposals, want %d", len(props), maxProposals)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", maxCommandBytes+1), strings.Repeat("a\n", maxCommandLines+1), "a\x00b", "a\rb", "a\u0085b"} {
		if _, ok := cleanCommand(bad); ok {
			t.Errorf("cleanCommand(%q) accepted", bad)
		}
	}
}

func TestResetAllDropsEveryConversation(t *testing.T) {
	f := newFixture(t, "answer")
	for _, id := range []string{conv, "conv-other-02"} {
		if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: id, Question: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := f.svc.DeleteAll(); n != 2 {
		t.Fatalf("ResetAll dropped %d, want 2", n)
	}
	if n, _ := f.svc.DeleteAll(); n != 0 {
		t.Fatalf("second ResetAll dropped %d, want 0", n)
	}
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "fresh"}); err != nil {
		t.Fatal(err)
	}
	reqs := f.fake.Requests()
	if n := len(reqs[len(reqs)-1].Messages); n != 1 {
		t.Fatalf("after ResetAll the request carried %d messages, want 1", n)
	}
}
