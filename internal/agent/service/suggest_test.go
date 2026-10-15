package service

import (
	"context"
	"errors"
	"fmt"
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

// keyStore is fakeStore with one key per provider.
type keyStore struct {
	fakeStore
	kmu  sync.Mutex
	keys map[string]string
}

func (k *keyStore) AgentKeyConfigured(id string) (bool, error) {
	k.kmu.Lock()
	defer k.kmu.Unlock()
	return k.keys[id] != "", nil
}

func (k *keyStore) AgentKey(id string) (string, error) {
	k.kmu.Lock()
	defer k.kmu.Unlock()
	if k.keys[id] == "" {
		return "", store.ErrNotFound
	}
	return k.keys[id], nil
}

func (k *keyStore) setKey(id, v string) {
	k.kmu.Lock()
	defer k.kmu.Unlock()
	k.keys[id] = v
}

const jevKey = "jev-test-key-0123456789"

type sfix struct {
	st    *keyStore
	panes *fakePanes
	model *providertest.Fake
	judge *providertest.FakeJudge
	log   *applog.Book
	svc   *Service
}

func block(cmd, why, risk string) string {
	return fmt.Sprintf("```keel-proposal\n{\"command\": %q, \"why\": %q, \"risk\": %q}\n```\n", cmd, why, risk)
}

func newSuggestFixture(t *testing.T, reply string) *sfix {
	t.Helper()
	set := store.DefaultAgentSettings()
	st := &keyStore{
		fakeStore: fakeStore{
			settings: set,
			hosts: []store.Host{
				{ID: "web", Name: "web-1", Address: "web.example.com", AgentReadable: bptr(true)},
				{ID: "db", Name: "prod-db-01", Address: "10.0.0.9", AgentReadable: bptr(true), Production: bptr(true)},
				{ID: "vault", Name: "secrets-box", Address: "s.example.com", AgentReadable: bptr(false)},
			},
			secrets: []string{vaultPass, geminiKey, jevKey},
		},
		keys: map[string]string{"gemini": geminiKey, "jev": jevKey},
	}
	panes := &fakePanes{panes: map[string]pane{
		"s-web": {host: "web", text: "$ \n"},
		"s-db":  {host: "db", text: "$ \n"},
		"s-sec": {host: "vault", text: "$ \n"},
	}}
	model := &providertest.Fake{Reply: provider.Response{Text: reply}}
	judge := &providertest.FakeJudge{Nouls: map[string]float64{"plain": 0.9}, Choices: map[string]provider.ChoiceAnswer{}}
	log := applog.New(filepath.Join(t.TempDir(), "keel.log"))
	svc, err := New(st, panes, log, Options{
		Factory: func(id, key string) (provider.Provider, error) {
			if key != geminiKey {
				t.Errorf("model built with key %q", key)
			}
			return model, nil
		},
		Judge: func(key string) (provider.Judge, error) {
			if key != jevKey {
				t.Errorf("judge built with key %q", key)
			}
			return judge, nil
		},
		DetectLogWindow: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &sfix{st: st, panes: panes, model: model, judge: judge, log: log, svc: svc}
}

func (f *sfix) entries(kind string) []applog.Entry {
	var out []applog.Entry
	for _, e := range f.log.Recent(1000) {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func (f *sfix) judgeBodies() string {
	var b strings.Builder
	for _, r := range f.judge.Requests() {
		for k, v := range r.Fields {
			b.WriteString(k + "=" + v.String() + "\n")
		}
		for k, l := range r.Lists {
			for _, v := range l {
				b.WriteString(k + "[]=" + v.String() + "\n")
			}
		}
		for _, q := range r.Questions {
			b.WriteString(q.Instructions + "\n")
		}
	}
	return b.String()
}

func (f *sfix) modelBodies() string {
	var b strings.Builder
	for _, r := range f.model.Requests() {
		b.WriteString(r.System.String())
		for _, m := range r.Messages {
			b.WriteString(m.Text.String())
		}
	}
	return b.String()
}

func (f *sfix) detect(t *testing.T, session string, role agent.Role, line string) DetectResult {
	t.Helper()
	res, err := f.svc.Detect(context.Background(), DetectRequest{SessionID: session, Role: role, Line: line})
	if err != nil {
		t.Fatalf("detect %q: %v", line, err)
	}
	return res
}

// §7: Jev decides for plain words above the threshold; #-lines need no
// call; one-word lines are never sent.
func TestDetectUsesJevAndHashLines(t *testing.T) {
	f := newSuggestFixture(t, "")
	if r := f.detect(t, "s-web", agent.RoleDriver, "which process is listening on 5432"); !r.Plain || r.Source != DetectJev {
		t.Fatalf("plain words → %+v", r)
	}
	f.judge.Nouls["plain"] = NoticeThreshold - 0.01
	if r := f.detect(t, "s-web", agent.RoleDriver, "git status --short"); r.Plain || r.Source != DetectJev {
		t.Fatalf("below threshold → %+v", r)
	}
	n := len(f.judge.Requests())
	if r := f.detect(t, "s-web", agent.RoleDriver, "# restart the pooler"); !r.Plain || r.Source != DetectHash {
		t.Fatalf("#-line → %+v", r)
	}
	if r := f.detect(t, "s-web", agent.RoleDriver, "uptime"); r.Plain || r.Source != DetectNone {
		t.Fatalf("one word → %+v", r)
	}
	if got := len(f.judge.Requests()); got != n {
		t.Fatalf("#-line or one word went to Jev (%d → %d requests)", n, got)
	}
	q := f.judge.Requests()[0]
	if len(q.Questions) != 1 || q.Questions["plain"].Type != provider.QuestionNoul || !strings.Contains(q.Questions["plain"].Instructions, "`"+statePromptLine+"`") {
		t.Fatalf("question = %+v", q.Questions)
	}
	if strings.Contains(q.Questions["plain"].Instructions, "5432") {
		t.Fatal("the prompt line was pasted into the question instead of the state")
	}
}

// Fallback: no Jev key, a Jev error or a timeout → #-lines only, and no
// error reaches the caller (no toast while typing).
func TestDetectFallsBackSilently(t *testing.T) {
	f := newSuggestFixture(t, "")
	f.judge.Err = errors.New("jev: HTTP 529 overloaded")
	r, err := f.svc.Detect(context.Background(), DetectRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "show me disk usage"})
	if err != nil || r.Plain || r.Source != DetectFallback {
		t.Fatalf("jev error → %+v, %v", r, err)
	}
	if r := f.detect(t, "s-web", agent.RoleDriver, "# show me disk usage"); !r.Plain {
		t.Fatal("#-lines must still be noticed when Jev fails")
	}
	f.st.setKey("jev", "")
	n := len(f.judge.Requests())
	if r := f.detect(t, "s-web", agent.RoleDriver, "show me disk usage"); r.Plain || r.Source != DetectFallback {
		t.Fatalf("no jev key → %+v", r)
	}
	if len(f.judge.Requests()) != n {
		t.Fatal("Jev called without a key")
	}
	// Timeout: a context that is already done.
	f.st.setKey("jev", jevKey)
	f.judge.Err = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := f.svc.Detect(ctx, DetectRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "show me disk usage"}); err != nil || r.Plain || r.Source != DetectFallback {
		t.Fatalf("timeout → %+v, %v", r, err)
	}
}

// Settings, model key, scope and role all gate detection before anything
// is sent.
func TestDetectGates(t *testing.T) {
	f := newSuggestFixture(t, "")
	// §6.6: a follower gets nothing.
	if _, err := f.svc.Detect(context.Background(), DetectRequest{SessionID: "s-web", Role: agent.RoleFollower, Line: "show me disk usage"}); !errors.Is(err, agent.ErrFollower) {
		t.Fatalf("follower → %v", err)
	}
	if _, err := f.svc.Detect(context.Background(), DetectRequest{SessionID: "s-web", Role: agent.RoleFollower, Line: "# show me disk usage"}); !errors.Is(err, agent.ErrFollower) {
		t.Fatalf("follower #-line → %v", err)
	}
	// §6.2: a host the agent cannot read.
	if r := f.detect(t, "s-sec", agent.RoleDriver, "show me disk usage"); r.Plain || r.Source != DetectOff {
		t.Fatalf("unreadable host → %+v", r)
	}
	// Hash-only detection.
	f.st.settings.Detection = store.DetectionHashOnly
	if r := f.detect(t, "s-web", agent.RoleDriver, "show me disk usage"); r.Plain || r.Source != DetectHashOnly {
		t.Fatalf("hash-only → %+v", r)
	}
	if r := f.detect(t, "s-web", agent.RoleDriver, "#show me disk usage"); !r.Plain {
		t.Fatal("hash-only must notice #-lines")
	}
	f.st.settings.Detection = store.DetectionAuto
	// Suggestions off.
	f.st.settings.Suggestions = store.SuggestionsOff
	if r := f.detect(t, "s-web", agent.RoleDriver, "# show me disk usage"); r.Plain || r.Source != DetectOff {
		t.Fatalf("off → %+v", r)
	}
	f.st.settings.Suggestions = store.SuggestionsOn
	// No model key: no hint at all.
	f.st.setKey("gemini", "")
	if r := f.detect(t, "s-web", agent.RoleDriver, "# show me disk usage"); r.Plain || r.Source != DetectOff {
		t.Fatalf("no model key → %+v", r)
	}
	if n := len(f.judge.Requests()); n != 0 {
		t.Fatalf("Jev called %d times through a closed gate", n)
	}
	if n := len(f.entries(agent.EventRead)); n != 0 {
		t.Fatalf("%d reads logged with nothing sent", n)
	}
}

// §6.4: the prompt line is redacted before it reaches Jev; §6.3: no vault
// value is sent.
func TestDetectRedactsBeforeJev(t *testing.T) {
	f := newSuggestFixture(t, "")
	f.detect(t, "s-web", agent.RoleDriver, "login with "+vaultPass+" and token=ghp_abcdefghijklmnopqrstuvwxyz0123456789")
	body := f.judgeBodies()
	for _, bad := range []string{vaultPass, "ghp_abcdefghijklmnopqrstuvwxyz0123456789", geminiKey, jevKey} {
		if strings.Contains(body, bad) {
			t.Fatalf("Jev request holds %q", bad)
		}
	}
	if !strings.Contains(body, "login with [secret:") {
		t.Fatalf("Jev request = %q", body)
	}
}

// §6.7: detection reads are logged, summarised per window so typing does
// not flood keel.log, and none is dropped.
func TestDetectLoggingIsSummarised(t *testing.T) {
	f := newSuggestFixture(t, "")
	for i := 0; i < 5; i++ {
		f.detect(t, "s-web", agent.RoleDriver, fmt.Sprintf("show me disk usage %d", i))
	}
	reads := f.entries(agent.EventRead)
	if len(reads) != 1 || reads[0].Count != 1 || reads[0].Source != DetectJev || reads[0].SessionID != "s-web" {
		t.Fatalf("first window = %+v", reads)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(f.entries(agent.EventRead)) < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	reads = f.entries(agent.EventRead) // newest first
	if len(reads) != 2 || reads[0].Count != 4 || reads[1].Count != 1 {
		t.Fatalf("after the window = %+v", reads)
	}
	total := 0
	for _, e := range reads {
		total += e.Count
	}
	if total != len(f.judge.Requests()) {
		t.Fatalf("logged %d lines, sent %d", total, len(f.judge.Requests()))
	}
	// keel.log holds counts, not the words.
	for _, e := range reads {
		if strings.Contains(e.Message, "disk usage") {
			t.Fatalf("the prompt line is in keel.log: %q", e.Message)
		}
	}
}

func (f *sfix) suggest(t *testing.T, session string, line string) Suggestion {
	t.Helper()
	s, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: session, Role: agent.RoleDriver, Line: line})
	if err != nil {
		t.Fatalf("suggest %q: %v", line, err)
	}
	return s
}

func TestSuggestReturnsACommandAndTwoAlternatives(t *testing.T) {
	f := newSuggestFixture(t, block("ss -ltnp 'sport = :5432'", "Lists listeners on 5432.", "readonly")+
		block("lsof -iTCP:5432 -sTCP:LISTEN", "Same with lsof.", "readonly")+
		block("netstat -ltnp", "Same with netstat.", "readonly"))
	s := f.suggest(t, "s-web", "# which process is listening on 5432")
	if len(s.Options) != 3 || s.Options[0].Command != "ss -ltnp 'sport = :5432'" || s.Options[0].Bytes != "\x15ss -ltnp 'sport = :5432'" {
		t.Fatalf("options = %+v", s.Options)
	}
	for _, o := range s.Options {
		if o.Risk != agent.RiskReadOnly || o.NeedsShift || o.Source != SourceGenerated || strings.HasSuffix(o.Bytes, "\n") {
			t.Fatalf("option = %+v", o)
		}
	}
	if s.SessionID != "s-web" || s.Host != "web-1" || s.ID == "" {
		t.Fatalf("suggestion = %+v", s)
	}
	// The words went to the model without the leading #.
	if body := f.modelBodies(); !strings.Contains(body, SuggestPrefix+"which process is listening on 5432") {
		t.Fatalf("model got %q", body)
	}
	// One Jev request rated all three.
	if n := len(f.judge.Requests()); n != 1 || len(f.judge.Requests()[0].Questions) != 3 {
		t.Fatalf("judge requests = %d", n)
	}
	sugs := f.entries(agent.EventSuggest)
	if len(sugs) != 1 || sugs[0].Source != SourceGenerated || !strings.Contains(sugs[0].Message, "Read-only · generated") {
		t.Fatalf("agent.suggest = %+v", sugs)
	}
	if strings.Contains(sugs[0].Message, "5432") {
		t.Fatal("the command is in keel.log")
	}
}

// Risk: the code denylist runs first and is final when it matches; Jev
// may only make a rating stricter.
func TestSuggestRiskDenylistWinsAndJevOnlyTightens(t *testing.T) {
	f := newSuggestFixture(t, block("rm -rf /var/lib/postgresql/16/main/pg_wal/old", "Deletes old WAL.", "readonly")+
		block("cat /etc/hostname", "Shows the host name.", "readonly")+
		block("systemctl restart pgbouncer", "Restarts the pooler.", "readonly"))
	f.judge.Choices = map[string]provider.ChoiceAnswer{
		// Asked only about the 2nd and 3rd (the denylist settled the 1st).
		"risk_0": {Choice: "destructive", Probabilities: map[string]float64{"readonly": 0.1, "changes": 0.2, "destructive": 0.7}},
		"risk_1": {Choice: "readonly", Probabilities: map[string]float64{"readonly": 0.99, "changes": 0.01}},
	}
	s := f.suggest(t, "s-web", "clean up the old wal files")
	if s.Options[0].Risk != agent.RiskDestructive || s.Options[0].Judged || !s.Options[0].NeedsShift {
		t.Fatalf("denylisted = %+v", s.Options[0])
	}
	if s.Options[1].Risk != agent.RiskDestructive || !s.Options[1].Judged {
		t.Fatalf("Jev should make cat stricter: %+v", s.Options[1])
	}
	if s.Options[2].Risk != agent.RiskChanges || !s.Options[2].Judged {
		t.Fatalf("Jev must not loosen restart to read-only: %+v", s.Options[2])
	}
	req := f.judge.Requests()[0]
	if len(req.Lists[stateCommands]) != 2 || req.Lists[stateCommands][0].String() != "cat /etc/hostname" {
		t.Fatalf("Jev asked about %v", req.Lists[stateCommands])
	}
	for _, q := range req.Questions {
		if strings.Contains(q.Instructions, "hostname") {
			t.Fatal("a command was pasted into a question instead of the state")
		}
	}
	// The thresholds: a weak destructive vote does not count, a weak
	// "changes or worse" does.
	for _, tc := range []struct {
		p    map[string]float64
		want agent.Risk
	}{
		{map[string]float64{"readonly": 0.6, "changes": 0.1, "destructive": 0.3}, agent.RiskReadOnly},
		{map[string]float64{"readonly": 0.4, "changes": 0.3, "destructive": 0.3}, agent.RiskChanges},
		{map[string]float64{"readonly": 0.2, "changes": 0.2, "destructive": 0.6}, agent.RiskDestructive},
	} {
		if got := judgedRisk(provider.ChoiceAnswer{Probabilities: tc.p}); got != tc.want {
			t.Errorf("judgedRisk(%v) = %s, want %s", tc.p, got, tc.want)
		}
	}
}

// Jev failing leaves the code rating; it never blocks a suggestion.
func TestSuggestWithoutJevUsesTheCodeRating(t *testing.T) {
	f := newSuggestFixture(t, block("df -h /var", "Free space.", "readonly")+block("touch /tmp/x", "Marker.", "readonly"))
	f.judge.Err = errors.New("jev down")
	s := f.suggest(t, "s-db", "how full is var")
	if s.Options[0].Risk != agent.RiskReadOnly || s.Options[0].Judged || s.Options[0].NeedsShift {
		t.Fatalf("df = %+v", s.Options[0])
	}
	// touch changes state; on a production host that needs ⇧.
	if s.Options[1].Risk != agent.RiskChanges || !s.Options[1].NeedsShift || !s.Production {
		t.Fatalf("touch on prod = %+v (production %v)", s.Options[1], s.Production)
	}
	f.st.setKey("jev", "")
	if s := f.suggest(t, "s-web", "how full is var"); s.Options[0].Judged {
		t.Fatal("judged without a Jev key")
	}
}

// §6.4 / §6.3 for suggestions: the words and the commands are redacted
// before the model and Jev see them.
func TestSuggestRedactsBeforeEgress(t *testing.T) {
	f := newSuggestFixture(t, block("echo [secret:1]", "Echoes it.", "readonly"))
	s := f.suggest(t, "s-web", "print the password "+vaultPass+" and API_TOKEN=Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK")
	all := f.modelBodies() + f.judgeBodies()
	for _, bad := range []string{vaultPass, "Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK", geminiKey, jevKey} {
		if strings.Contains(all, bad) {
			t.Fatalf("egress holds %q", bad)
		}
	}
	if !s.Options[0].Placeholders {
		t.Fatal("placeholder not flagged")
	}
}

// §6.6 and the other gates: a follower gets nothing and nothing is sent.
func TestSuggestGates(t *testing.T) {
	f := newSuggestFixture(t, block("ls", "Lists.", "readonly"))
	for _, role := range []agent.Role{agent.RoleFollower, agent.RoleOwner} {
		if _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: role, Line: "list files"}); !errors.Is(err, agent.ErrFollower) && !errors.Is(err, agent.ErrNotDriving) {
			t.Fatalf("%s → %v", role, err)
		}
	}
	if _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-sec", Role: agent.RoleDriver, Line: "list files"}); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("unreadable → %v", err)
	}
	f.st.settings.Suggestions = store.SuggestionsOff
	if _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "list files"}); !errors.Is(err, ErrSuggestOff) {
		t.Fatalf("off → %v", err)
	}
	f.st.settings.Suggestions = store.SuggestionsOn
	f.st.setKey("gemini", "")
	if _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "list files"}); !errors.Is(err, ErrNoKey) {
		t.Fatalf("no key → %v", err)
	}
	if len(f.model.Requests()) != 0 || len(f.judge.Requests()) != 0 {
		t.Fatal("a provider was called through a closed gate")
	}
	if len(f.entries(agent.EventSuggest)) != 0 {
		t.Fatal("a refused suggestion was logged as made")
	}
}

// §6.1, §6.5, §6.6, §6.7: an insert is authorised only for the driver,
// with the shown bytes, with ⇧ when the rule needs it, and is logged with
// its source once the write happened. AuthorizeInsert writes nothing.
func TestAuthorizeInsertRules(t *testing.T) {
	f := newSuggestFixture(t, block("rm -rf /tmp/keel-x", "Deletes it.", "destructive")+block("ls /tmp", "Lists.", "readonly"))
	s := f.suggest(t, "s-web", "delete the scratch folder")
	de, ro := s.Options[0], s.Options[1]
	req := func(sug Suggestion, opt int, bytes string, shift bool, role agent.Role, session string) (InsertGrant, error) {
		return f.svc.AuthorizeInsert(InsertRequest{SuggestionID: sug.ID, Option: opt, SessionID: session, Role: role, Bytes: bytes, Shift: shift})
	}
	if _, err := req(s, 0, de.Bytes, false, agent.RoleDriver, "s-web"); !errors.Is(err, ErrNeedsShiftInsert) {
		t.Fatalf("plain ⇥ on destructive → %v", err)
	}
	if _, err := req(s, 1, ro.Bytes, false, agent.RoleFollower, "s-web"); !errors.Is(err, agent.ErrFollower) {
		t.Fatalf("follower → %v", err)
	}
	if _, err := req(s, 1, ro.Bytes+"\n", false, agent.RoleDriver, "s-web"); !errors.Is(err, ErrChanged) {
		t.Fatalf("a newline added (run, not insert) → %v", err)
	}
	if _, err := req(s, 1, ro.Bytes, false, agent.RoleDriver, "s-db"); !errors.Is(err, ErrWrongPane) {
		t.Fatalf("other pane → %v", err)
	}
	if _, err := f.svc.AuthorizeInsert(InsertRequest{SuggestionID: "s_nope", SessionID: "s-web", Role: agent.RoleDriver, Bytes: ro.Bytes}); !errors.Is(err, ErrExpired) {
		t.Fatalf("unknown → %v", err)
	}
	if n := len(f.entries(agent.EventInsert)); n != 0 {
		t.Fatalf("refused inserts logged: %d", n)
	}
	g, err := req(s, 1, ro.Bytes, false, agent.RoleDriver, "s-web")
	if err != nil {
		t.Fatalf("⇥ read-only → %v", err)
	}
	if n := len(f.entries(agent.EventInsert)); n != 0 {
		t.Fatal("agent.insert logged before the write")
	}
	f.svc.InsertDone(g, nil)
	s2 := f.suggest(t, "s-web", "delete the scratch folder")
	g, err = req(s2, 0, s2.Options[0].Bytes, true, agent.RoleDriver, "s-web")
	if err != nil {
		t.Fatalf("⇧⇥ destructive → %v", err)
	}
	f.svc.InsertDone(g, nil)
	ins := f.entries(agent.EventInsert) // newest first
	if len(ins) != 2 || ins[0].Source != SourceGenerated || ins[0].Bytes != len(de.Bytes) || ins[1].Bytes != len(ro.Bytes) {
		t.Fatalf("agent.insert = %+v", ins)
	}
	// Expiry.
	f.svc.now = func() time.Time { return time.Now().Add(suggestionTTL + time.Minute) }
	s3 := s2
	if _, err := req(s3, 0, de.Bytes, true, agent.RoleDriver, "s-web"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired → %v", err)
	}
}

// Replay: a suggestion is inserted once. Resending the same insert, or
// another option of it, is refused and not logged again.
func TestInsertIsNotReplayable(t *testing.T) {
	f := newSuggestFixture(t, block("ls /tmp", "Lists.", "readonly")+block("ls -la /tmp", "Lists more.", "readonly"))
	s := f.suggest(t, "s-web", "list tmp")
	r := InsertRequest{SuggestionID: s.ID, Option: 0, SessionID: "s-web", Role: agent.RoleDriver, Bytes: s.Options[0].Bytes}
	g, err := f.svc.AuthorizeInsert(r)
	if err != nil {
		t.Fatal(err)
	}
	// Reserved while the write is in flight: a second insert is refused.
	if _, err := f.svc.AuthorizeInsert(r); !errors.Is(err, ErrUsed) {
		t.Fatalf("concurrent resend → %v", err)
	}
	f.svc.InsertDone(g, nil)
	if _, err := f.svc.AuthorizeInsert(r); !errors.Is(err, ErrUsed) {
		t.Fatalf("resend → %v", err)
	}
	r.Option, r.Bytes = 1, s.Options[1].Bytes
	if _, err := f.svc.AuthorizeInsert(r); !errors.Is(err, ErrUsed) {
		t.Fatalf("other option of a used suggestion → %v", err)
	}
	if n := len(f.entries(agent.EventInsert)); n != 1 {
		t.Fatalf("agent.insert entries = %d, want 1", n)
	}
}

// Log truth: a write that failed is not logged as an insert, and the
// suggestion can be inserted again.
func TestFailedWriteIsNotLoggedAsInserted(t *testing.T) {
	f := newSuggestFixture(t, block("ls /tmp", "Lists.", "readonly"))
	s := f.suggest(t, "s-web", "list tmp")
	r := InsertRequest{SuggestionID: s.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: s.Options[0].Bytes}
	g, err := f.svc.AuthorizeInsert(r)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.InsertDone(g, errors.New("inject: missing or spent approve ticket"))
	if n := len(f.entries(agent.EventInsert)); n != 0 {
		t.Fatalf("a failed write was logged as inserted (%d)", n)
	}
	g, err = f.svc.AuthorizeInsert(r)
	if err != nil {
		t.Fatalf("retry after a failed write → %v", err)
	}
	f.svc.InsertDone(g, nil)
	if n := len(f.entries(agent.EventInsert)); n != 1 {
		t.Fatalf("agent.insert entries = %d, want 1", n)
	}
}

// The production gate fails closed: production when suggested OR now,
// and a host that can no longer be found counts as production.
func TestProductionGateFailsClosed(t *testing.T) {
	ins := func(f *sfix, s Suggestion, shift bool) error {
		_, err := f.svc.AuthorizeInsert(InsertRequest{SuggestionID: s.ID, SessionID: s.SessionID, Role: agent.RoleDriver, Bytes: s.Options[0].Bytes, Shift: shift})
		return err
	}
	// Became production after the suggestion.
	f := newSuggestFixture(t, block("touch /tmp/marker", "Marker.", "changes"))
	s := f.suggest(t, "s-web", "make a marker file")
	f.st.setProduction("web", true)
	if err := ins(f, s, false); !errors.Is(err, ErrNeedsShiftInsert) {
		t.Fatalf("changes on now-production host → %v", err)
	}
	// Was production when suggested, flag turned off since.
	f = newSuggestFixture(t, block("touch /tmp/marker", "Marker.", "changes"))
	s = f.suggest(t, "s-db", "make a marker file")
	f.st.setProduction("db", false)
	if err := ins(f, s, false); !errors.Is(err, ErrNeedsShiftInsert) {
		t.Fatalf("changes on a host that was production → %v", err)
	}
	// Host deleted since.
	f = newSuggestFixture(t, block("touch /tmp/marker", "Marker.", "changes"))
	s = f.suggest(t, "s-web", "make a marker file")
	f.st.mu.Lock()
	f.st.hosts = f.st.hosts[1:]
	f.st.mu.Unlock()
	if err := ins(f, s, false); !errors.Is(err, ErrNeedsShiftInsert) {
		t.Fatalf("changes on a missing host → %v", err)
	}
	if err := ins(f, s, true); err != nil {
		t.Fatalf("⇧⇥ on a missing host → %v", err)
	}
	// Read-only stays plain ⇥ even then.
	f = newSuggestFixture(t, block("ls /tmp", "Lists.", "readonly"))
	s = f.suggest(t, "s-web", "list tmp")
	f.st.mu.Lock()
	f.st.hosts = f.st.hosts[1:]
	f.st.mu.Unlock()
	if err := ins(f, s, false); err != nil {
		t.Fatalf("read-only on a missing host → %v", err)
	}
}

// §6.7: everything sent off the machine for a suggestion is logged before
// the call, including a call that fails, and the Jev rating call too.
func TestSuggestEgressIsLoggedEvenWhenItFails(t *testing.T) {
	f := newSuggestFixture(t, "")
	f.model.Err = errors.New("gemini: HTTP 503")
	if _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "list tmp"}); err == nil {
		t.Fatal("no error")
	}
	reads := f.entries(agent.EventRead)
	if len(reads) != 1 || reads[0].Source != "gemini" || !strings.Contains(reads[0].Message, "the words on the prompt line") {
		t.Fatalf("failed suggest left %+v", reads)
	}
	// Empty answer: logged too.
	f.model.Err = nil
	if _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "list tmp"}); !errors.Is(err, ErrNoSuggestion) {
		t.Fatalf("empty → %v", err)
	}
	if n := len(f.entries(agent.EventRead)); n != 2 {
		t.Fatalf("reads after an empty answer = %d", n)
	}
	// A suggestion: the words to the model, the commands to Jev.
	g := newSuggestFixture(t, block("ls /tmp", "Lists.", "readonly")+block("rm -rf /tmp/x", "Deletes.", "destructive"))
	g.suggest(t, "s-web", "list tmp")
	reads = g.entries(agent.EventRead) // newest first
	if len(reads) != 2 || reads[0].Source != DetectJev || reads[0].Count != 1 || !strings.Contains(reads[0].Message, "to rate their risk") || reads[1].Source != "gemini" {
		t.Fatalf("reads = %+v", reads)
	}
	for _, e := range g.log.Recent(100) {
		if strings.Contains(e.Message, "/tmp") {
			t.Fatalf("keel.log holds a command: %q", e.Message)
		}
	}
	// Jev failing: still logged (the commands were sent).
	h := newSuggestFixture(t, block("ls /tmp", "Lists.", "readonly"))
	h.judge.Err = errors.New("jev down")
	h.suggest(t, "s-web", "list tmp")
	if reads := h.entries(agent.EventRead); len(reads) != 2 || reads[0].Source != DetectJev {
		t.Fatalf("reads with Jev down = %+v", reads)
	}
}

// Close writes the detection summaries still open (keel serve stopping).
func TestCloseFlushesDetectionLog(t *testing.T) {
	f := newSuggestFixture(t, "")
	f.svc.detectWindow = time.Hour
	for i := 0; i < 3; i++ {
		f.detect(t, "s-web", agent.RoleDriver, fmt.Sprintf("show me disk usage %d", i))
	}
	if n := len(f.entries(agent.EventRead)); n != 1 {
		t.Fatalf("before close: %d entries", n)
	}
	f.svc.Close()
	reads := f.entries(agent.EventRead)
	if len(reads) != 2 || reads[0].Count != 2 {
		t.Fatalf("after close = %+v", reads)
	}
	f.svc.Close() // idempotent
	if n := len(f.entries(agent.EventRead)); n != 2 {
		t.Fatalf("second close wrote %d entries", n-2)
	}
}

// An insert is never run: a candidate with a second line is dropped.
func TestSuggestDropsMultiLineCommands(t *testing.T) {
	f := newSuggestFixture(t, block("cd /tmp\nrm -rf x", "Two lines.", "readonly")+block("ls /tmp", "Lists.", "readonly"))
	s := f.suggest(t, "s-web", "list tmp")
	if len(s.Options) != 1 || s.Options[0].Command != "ls /tmp" {
		t.Fatalf("options = %+v", s.Options)
	}
	g := newSuggestFixture(t, block("cd /tmp\nls", "Two lines.", "readonly"))
	if _, err := g.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "list tmp"}); !errors.Is(err, ErrNoSuggestion) {
		t.Fatalf("only a multi-line candidate → %v", err)
	}
}

// The e2e fakes speak the same formats as the service.
func TestScriptedFakesSpeakTheSuggestionFormat(t *testing.T) {
	if providertest.SuggestPrefix != SuggestPrefix {
		t.Fatal("providertest.SuggestPrefix drifted from service.SuggestPrefix")
	}
	sc := providertest.NewScripted()
	f := newSuggestFixture(t, "")
	f.svc.factory = func(string, string) (provider.Provider, error) { return sc, nil }
	f.svc.judge = func(string) (provider.Judge, error) { return providertest.NewScriptedJudge(), nil }
	s := f.suggest(t, "s-web", "show the files here")
	if len(s.Options) != 3 || s.Options[0].Command != "echo keel-suggest-ok" || s.Options[0].Risk != agent.RiskReadOnly {
		t.Fatalf("default = %+v", s.Options)
	}
	if s := f.suggest(t, "s-web", "clean up old files"); s.Options[0].Risk != agent.RiskDestructive || !s.Options[0].NeedsShift {
		t.Fatalf("clean up = %+v", s.Options)
	}
	if s := f.suggest(t, "s-web", "make it stricter"); s.Options[0].Risk != agent.RiskChanges || !s.Options[0].Judged {
		t.Fatalf("stricter = %+v", s.Options)
	}
	if r := f.detect(t, "s-web", agent.RoleDriver, "show the files here"); !r.Plain {
		t.Fatal("scripted judge did not notice words")
	}
	if r := f.detect(t, "s-web", agent.RoleDriver, "ls -la /tmp"); r.Plain {
		t.Fatal("scripted judge noticed a command")
	}
	if r := f.detect(t, "s-web", agent.RoleDriver, "jev-offline please"); r.Plain || r.Source != DetectFallback {
		t.Fatalf("scripted offline → %+v", r)
	}
}
