package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/agent/provider/jev"
	"keel/internal/agent/provider/providertest"
	"keel/internal/agent/redact"
	"keel/internal/applog"
	"keel/internal/store"
)

// egress is the agent.read entries for text sent off the machine (they
// name a source), without the history step's local pane reads.
func (f *sfix) egress() []applog.Entry {
	var out []applog.Entry
	for _, e := range f.entries(agent.EventRead) {
		if e.Source != "" {
			out = append(out, e)
		}
	}
	return out
}

// setPane replaces a pane's scrollback.
func (f *sfix) setPane(id, host, text string) {
	f.panes.mu.Lock()
	defer f.panes.mu.Unlock()
	f.panes.panes[id] = pane{host: host, text: text}
}

// historyRequests is the Jev requests that were history picks.
func (f *sfix) historyRequests() []provider.JudgeRequest {
	var out []provider.JudgeRequest
	for _, r := range f.judge.Requests() {
		if _, ok := r.Maps[stateCandidates]; ok {
			out = append(out, r)
		}
	}
	return out
}

// confident makes the fake Jev sure of candidate key.
func (f *sfix) confident(key string) {
	f.judge.Choices["pick"] = provider.ChoiceAnswer{Choice: key, Probabilities: map[string]float64{key: 0.97, historyNone: 0.03}}
	f.judge.Nouls["exists"] = 0.95
	f.judge.Nouls["fit_"+key] = 0.93
}

func TestHistoryCommandsPromptStyles(t *testing.T) {
	text := strings.Join([]string{
		"Last login: Thu Oct 15 on ttys001",
		"user@web-1:~$ ls -la /var/log",
		"total 12",
		"[root@db etc]# systemctl status nginx",
		"sh-3.2$ echo keel-history-marker",
		"keel-history-marker",
		"❯ git status",
		"host% make test",
		"PS C:\\> dir",
		"(venv) kj@box:~/src$ docker ps",
		"$ kubectl get pods -n prod",
		"user@web-1:~$ ls -la /var/log", // again: kept once, as the most recent
		"user@web-1:~$ show running containers",
		"sh: show: command not found",
		"$ x",
		"$ echo 'unclosed",
		"$ tar czf logs.tgz \\",
		"> logs/",
		"# just a comment",
		"Total $ 5",
		"Price: 3 $ 100 dollars",
		"$ # show me disk usage",
		"user@web-1:~$ how much disk is left", // the line being edited
	}, "\n")
	got := historyCommands(text)
	want := []string{
		"ls -la /var/log",
		"kubectl get pods -n prod",
		"docker ps",
		"dir",
		"make test",
		"git status",
		"echo keel-history-marker",
		"systemctl status nginx",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("historyCommands =\n%q\nwant\n%q", got, want)
	}
}

// Raw PTY output as /bin/sh writes it (taken from an e2e run): the prompt
// redrawn with a carriage return, words cleared and a suggestion inserted
// in their place. Each line is read as the terminal shows it once done, so
// only the commands that were on screen count, never the drafts.
func TestHistoryReadsLinesAsShown(t *testing.T) {
	raw := "\x1b[?1034hsh-3.2$ \r\x1b[Ksh-3.2$ echo keel-history-marker\r\nkeel-history-marker\r\n" +
		"sh-3.2$ history marker\rsh-3.2$ \x1b[Kecho keel-other\r\nkeel-other\r\n" +
		"sh-3.2$ ech\rsh-3.2$ echo typed\r\ntyped\r\nsh-3.2$ "
	f := newSuggestFixture(t, "")
	f.setPane("s-web", "web", raw)
	ex, err := f.svc.audit.ReadPaneScreen(f.panes, agent.NewScope(agent.HostsFrom(f.st.hosts)), agent.Pane{SessionID: "s-web", HostID: "web"}, 100, redact.New(redact.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	got := historyCommands(ex.Text.String())
	if want := []string{"echo typed", "echo keel-other", "echo keel-history-marker"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("historyCommands = %q, want %q", got, want)
	}
}

func TestHistoryCommandsSkipPasswordsAndRedactions(t *testing.T) {
	text := strings.Join([]string{
		"$ mysql -u root --password=hunter2",
		"[sudo] password for kj: ",
		"$ sudo apt update",
		"Enter passphrase for key '/home/kj/.ssh/id_ed25519': ",
		"$ curl -H 'Authorization: Bearer [secret:1]' https://api",
		"$ ssh deploy@[host:2]",
		"$ ping [ip:1]",
		"$ df -h",
		"$ ",
	}, "\n")
	got := historyCommands(text)
	if want := []string{"df -h", "sudo apt update"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("historyCommands = %q, want %q", got, want)
	}
}

func TestPrefilterKeepsNearestThenRecent(t *testing.T) {
	var cands []historyCand
	for i := 0; i < 50; i++ {
		cands = append(cands, historyCand{command: fmt.Sprintf("echo filler-%d", i), source: SourceHistory})
	}
	cands = append(cands, historyCand{command: "docker ps --filter status=running", source: SourceHistory})
	cands = append(cands, historyCand{command: "journalctl -u nginx", source: SourceHistory})
	got := prefilter("show the running containers", cands, 40)
	if len(got) != 40 {
		t.Fatalf("kept %d", len(got))
	}
	if got[0].command != "docker ps --filter status=running" {
		t.Fatalf("first = %q, want the overlapping one", got[0].command)
	}
	if got[1].command != "echo filler-0" || got[39].command != "echo filler-38" {
		t.Fatalf("the rest keep their order: %q … %q", got[1].command, got[39].command)
	}
	// Room for all: nothing dropped, order kept.
	few := cands[:3]
	if got := prefilter("anything", few, 40); !reflect.DeepEqual(got, few) {
		t.Fatalf("prefilter changed a short list: %v", got)
	}
}

// The request Jev gets for a history pick, on the wire (jev.Client against
// an httptest server on loopback): state.request and state.candidates (a
// map keyed c0…), the pick Choice with none_of_the_above, the absolute
// exists Noul and one fit Noul per candidate. A confident answer is the
// suggestion, from history, and the model is not called.
func TestHistoryRequestShapeOnTheWire(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		qs := body["questions"].(map[string]any)
		answers := map[string]any{}
		for id, q := range qs {
			q := q.(map[string]any)
			switch {
			case id == "pick":
				answers[id] = map[string]any{"type": "choice", "choice": "c1", "confidence": 0.9, "probabilities": map[string]float64{"c1": 0.93, "c0": 0.05, historyNone: 0.02}}
			case q["type"] == "noul":
				v := 0.05
				if id == "exists" || id == "fit_c1" {
					v = 0.9
				}
				answers[id] = map[string]any{"type": "noul", "noul": v}
			default: // risk_N after the card
				answers[id] = map[string]any{"type": "choice", "choice": "readonly", "probabilities": map[string]float64{"readonly": 1}}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": jev.Model, "answers": answers, "usage": map[string]int{"input_tokens": 100}})
	}))
	defer srv.Close()

	f := newSuggestFixture(t, block("echo from-the-model", "Model.", "readonly"))
	svc, err := New(f.st, f.panes, f.log, Options{
		Factory: func(string, string) (provider.Provider, error) { return f.model, nil },
		Judge:   func(key string) (provider.Judge, error) { return jev.New(key, jev.WithBaseURL(srv.URL)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	f.setPane("s-web", "web", "$ uptime\n 10:00 up 3 days\n$ docker ps\nCONTAINER ID\n$ show running containers")
	s := f.suggest(t, "s-web", "show running containers")

	if len(s.Options) != 1 || s.Options[0].Command != "uptime" || s.Options[0].Source != SourceHistory {
		t.Fatalf("options = %+v", s.Options)
	}
	if n := len(f.model.Requests()); n != 0 {
		t.Fatalf("the model was called %d times after a confident pick", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("%d Jev requests, want the pick and the risk check", len(bodies))
	}
	b := bodies[0]
	if b["model"] != jev.Model {
		t.Fatalf("model = %v", b["model"])
	}
	state := b["state"].(map[string]any)
	if state[stateRequest] != "show running containers" {
		t.Fatalf("state.request = %v", state[stateRequest])
	}
	cands := state[stateCandidates].(map[string]any)
	if !reflect.DeepEqual(cands, map[string]any{"c0": "docker ps", "c1": "uptime"}) {
		t.Fatalf("state.candidates = %v", cands)
	}
	qs := b["questions"].(map[string]any)
	if len(qs) != 4 {
		t.Fatalf("questions = %v", qs)
	}
	pick := qs["pick"].(map[string]any)
	crit := pick["criteria"].(map[string]any)
	if pick["type"] != "choice" || len(crit) != 3 || crit["c0"] != "The command `candidates.c0`" || crit[historyNone] == nil {
		t.Fatalf("pick = %v", pick)
	}
	if !strings.Contains(pick["instructions"].(string), "`request`") {
		t.Fatalf("pick does not point at the request by path: %v", pick["instructions"])
	}
	for _, id := range []string{"exists", "fit_c0", "fit_c1"} {
		q := qs[id].(map[string]any)
		if q["type"] != "noul" || q["criteria"] == nil {
			t.Fatalf("%s = %v", id, q)
		}
	}
	if !strings.Contains(qs["fit_c1"].(map[string]any)["instructions"].(string), "`candidates.c1`") {
		t.Fatalf("fit_c1 = %v", qs["fit_c1"])
	}
	// No untrusted text in a question.
	raw, _ := json.Marshal(qs)
	if strings.Contains(string(raw), "uptime") || strings.Contains(string(raw), "running containers") {
		t.Fatalf("terminal text inside a question: %s", raw)
	}
}

// Confident: the history command is the suggestion, the model is not
// called, keel.log names the source, and the egress entry holds counts,
// never words or commands.
func TestHistoryConfidentSkipsTheModel(t *testing.T) {
	f := newSuggestFixture(t, block("echo from-the-model", "Model.", "readonly"))
	f.setPane("s-web", "web", "sh-3.2$ echo keel-history-marker\nkeel-history-marker\nsh-3.2$ print the marker")
	f.confident("c0")
	s := f.suggest(t, "s-web", "print the marker")
	if len(s.Options) != 1 || s.Options[0].Command != "echo keel-history-marker" || s.Options[0].Source != SourceHistory || s.Options[0].Bytes != "\x15echo keel-history-marker" {
		t.Fatalf("options = %+v", s.Options)
	}
	if n := len(f.model.Requests()); n != 0 {
		t.Fatalf("model called %d times", n)
	}
	sent := f.egress()
	var pickLog *applog.Entry
	for i := range sent {
		if sent[i].Source == SourceHistory {
			pickLog = &sent[i]
		}
	}
	if pickLog == nil || pickLog.Count != 2 || !strings.Contains(pickLog.Message, "1 candidate command (1 from this pane's history, 0 from snippets)") {
		t.Fatalf("history egress = %+v", sent)
	}
	for _, e := range f.log.Recent(100) {
		if strings.Contains(e.Message, "keel-history-marker") || strings.Contains(e.Message, "print the marker") {
			t.Fatalf("keel.log holds terminal text: %q", e.Message)
		}
	}
	sug := f.entries(agent.EventSuggest)
	if len(sug) != 1 || sug[0].Source != SourceHistory || sug[0].ModelMs != 0 || !strings.Contains(sug[0].Message, "history pick") {
		t.Fatalf("agent.suggest = %+v", sug)
	}
	g, err := f.svc.AuthorizeInsert(InsertRequest{SuggestionID: s.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: s.Options[0].Bytes})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.InsertDone(g, nil)
	if ins := f.entries(agent.EventInsert); len(ins) != 1 || ins[0].Source != SourceHistory {
		t.Fatalf("agent.insert = %+v", ins)
	}
}

// Unsure (any of the three below its bar), none_of_the_above, a Jev error
// or a missing answer: the model writes the command, as before.
func TestHistoryUnsureFallsThroughToTheModel(t *testing.T) {
	cases := map[string]func(f *sfix){
		"none": func(f *sfix) {
			f.judge.Choices["pick"] = provider.ChoiceAnswer{Choice: historyNone, Probabilities: map[string]float64{historyNone: 0.99}}
			f.judge.Nouls["exists"] = 0.05
		},
		"low fit": func(f *sfix) {
			f.confident("c0")
			f.judge.Nouls["fit_c0"] = HistoryFitMin - 0.01
		},
		"low exists": func(f *sfix) {
			f.confident("c0")
			f.judge.Nouls["exists"] = HistoryExistsMin - 0.01
		},
		"low pick": func(f *sfix) {
			f.confident("c0")
			f.judge.Choices["pick"] = provider.ChoiceAnswer{Choice: "c0", Probabilities: map[string]float64{"c0": HistoryPickMin - 0.01}}
		},
		"no answer": func(*sfix) {},
		"jev down":  func(f *sfix) { f.judge.Err = errors.New("jev: HTTP 503") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSuggestFixture(t, block("echo from-the-model", "Model.", "readonly"))
			f.setPane("s-web", "web", "$ echo keel-history-marker\nkeel-history-marker\n$ print the marker")
			setup(f)
			s := f.suggest(t, "s-web", "print the marker")
			if s.Options[0].Command != "echo from-the-model" || s.Options[0].Source != SourceGenerated {
				t.Fatalf("options = %+v", s.Options)
			}
			if n := len(f.model.Requests()); n != 1 {
				t.Fatalf("model called %d times", n)
			}
		})
	}
}

// No Jev key: no history read, no history egress; the model as before.
func TestHistorySkippedWithoutJevKey(t *testing.T) {
	f := newSuggestFixture(t, block("echo from-the-model", "Model.", "readonly"))
	f.st.setKey("jev", "")
	f.setPane("s-web", "web", "$ echo keel-history-marker\n$ print the marker")
	s := f.suggest(t, "s-web", "print the marker")
	if s.Options[0].Source != SourceGenerated {
		t.Fatalf("source = %q", s.Options[0].Source)
	}
	if reads := f.entries(agent.EventRead); len(reads) != 1 || reads[0].Source != "gemini" {
		t.Fatalf("reads = %+v", reads)
	}
}

// Suggestions off, or a host the agent cannot read: the history step never
// runs (no read, no Jev).
func TestHistoryNeverRunsWhenOffOrUnreadable(t *testing.T) {
	f := newSuggestFixture(t, block("echo from-the-model", "Model.", "readonly"))
	f.setPane("s-sec", "vault", "$ cat /etc/secret-thing\n$ show it")
	f.setPane("s-web", "web", "$ echo keel-history-marker\n$ print the marker")
	f.st.snippets = []store.Snippet{{ID: "1", Name: "disk", Command: "df -h\n", AgentUsable: true}}
	f.confident("c0")
	if _, _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-sec", Role: agent.RoleDriver, Line: "show it"}); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("unreadable → %v", err)
	}
	f.st.settings.Suggestions = "off"
	if _, _, err := f.svc.Suggest(context.Background(), SuggestRequest{SessionID: "s-web", Role: agent.RoleDriver, Line: "print the marker"}); !errors.Is(err, ErrSuggestOff) {
		t.Fatalf("off → %v", err)
	}
	if n := len(f.log.Recent(100)); n != 0 {
		t.Fatalf("keel.log has %d entries", n)
	}
	if len(f.judge.Requests()) != 0 || f.st.snipReads != 0 {
		t.Fatalf("judge requests %d, snippet reads %d", len(f.judge.Requests()), f.st.snipReads)
	}
	f.panes.mu.Lock()
	defer f.panes.mu.Unlock()
	if len(f.panes.reads) != 0 {
		t.Fatalf("scrollback read: %v", f.panes.reads)
	}
}

// History may hold rm -rf: the same rating and gates as a generated
// command. Plain ⇥ is refused; ⇧⇥ inserts.
func TestHistoryDestructiveNeedsShift(t *testing.T) {
	f := newSuggestFixture(t, "")
	f.setPane("s-web", "web", "$ rm -rf /tmp/keel-history-rm\n$ clean up the scratch folder")
	f.confident("c0")
	s := f.suggest(t, "s-web", "clean up the scratch folder")
	o := s.Options[0]
	if o.Command != "rm -rf /tmp/keel-history-rm" || o.Source != SourceHistory || o.Risk != agent.RiskDestructive || !o.NeedsShift {
		t.Fatalf("option = %+v", o)
	}
	if _, err := f.svc.AuthorizeInsert(InsertRequest{SuggestionID: s.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: o.Bytes}); !errors.Is(err, ErrNeedsShiftInsert) {
		t.Fatalf("plain ⇥ → %v", err)
	}
	if _, err := f.svc.AuthorizeInsert(InsertRequest{SuggestionID: s.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: o.Bytes, Shift: true}); err != nil {
		t.Fatalf("⇧⇥ → %v", err)
	}
}

// Jev's risk rating applies to history too, and only makes it stricter.
func TestHistoryRiskCheckMakesStricter(t *testing.T) {
	f := newSuggestFixture(t, "")
	f.setPane("s-db", "db", "$ touch /srv/flag\n$ make the flag")
	f.confident("c0")
	f.judge.Choices["risk_0"] = provider.ChoiceAnswer{Choice: "changes", Probabilities: map[string]float64{"changes": 0.9}}
	s := f.suggest(t, "s-db", "make the flag")
	if o := s.Options[0]; o.Source != SourceHistory || o.Risk != agent.RiskChanges || !o.NeedsShift || !o.Judged {
		t.Fatalf("option = %+v", o)
	}
}

// §6.3: a snippet is sent only when the user marked it agent-usable, and
// only a single-line one with no {{var}} field.
func TestSnippetsOnlyWhenAgentUsable(t *testing.T) {
	f := newSuggestFixture(t, block("echo from-the-model", "Model.", "readonly"))
	f.setPane("s-web", "web", "$ uptime\n$ show disk space")
	f.st.snippets = []store.Snippet{
		{ID: "1", Name: "private", Command: "echo keel-private-snippet\n"},
		{ID: "2", Name: "two lines", Command: "echo keel-line-1\necho keel-line-2\n", AgentUsable: true},
		{ID: "3", Name: "vars", Command: "ssh {{host}}\n", AgentUsable: true},
		{ID: "4", Name: "secret", Command: "export API_TOKEN=" + vaultPass + "\n", AgentUsable: true},
		{ID: "5", Name: "disk", Command: "df -h\n", AgentUsable: true},
	}
	f.confident("c0")
	s := f.suggest(t, "s-web", "show disk space")
	if o := s.Options[0]; o.Command != "df -h" || o.Source != SourceSnippet || o.Why != `Your snippet "disk".` {
		t.Fatalf("option = %+v", o)
	}
	reqs := f.historyRequests()
	if len(reqs) != 1 {
		t.Fatalf("%d history requests", len(reqs))
	}
	got := map[string]string{}
	for k, v := range reqs[0].Maps[stateCandidates] {
		got[k] = v.String()
	}
	if !reflect.DeepEqual(got, map[string]string{"c0": "df -h", "c1": "uptime"}) {
		t.Fatalf("candidates = %v", got)
	}
	body := f.judgeBodies()
	for _, never := range []string{"keel-private-snippet", "keel-line", "{{host}}", vaultPass, "API_TOKEN"} {
		if strings.Contains(body, never) {
			t.Fatalf("Jev was sent %q", never)
		}
	}
	if ins := f.entries(agent.EventSuggest); len(ins) != 1 || ins[0].Source != SourceSnippet {
		t.Fatalf("agent.suggest = %+v", ins)
	}
}

// Redaction runs before anything is sent: the words are redacted, and a
// history line that held a secret is never a candidate.
func TestHistoryRedactsBeforeEgress(t *testing.T) {
	f := newSuggestFixture(t, block("echo from-the-model", "Model.", "readonly"))
	f.setPane("s-web", "web", strings.Join([]string{
		"$ export GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"$ mysql -p" + vaultPass,
		"$ curl https://web.example.com/health",
		"$ git status",
		"$ deploy with " + vaultPass,
	}, "\n"))
	f.st.settings.Redact.Hostnames = true
	f.suggest(t, "s-web", "deploy with "+vaultPass)
	reqs := f.historyRequests()
	if len(reqs) != 1 {
		t.Fatalf("%d history requests", len(reqs))
	}
	if w := reqs[0].Fields[stateRequest].String(); strings.Contains(w, vaultPass) || !strings.Contains(w, "[secret:") {
		t.Fatalf("request = %q", w)
	}
	var cands []string
	for _, v := range reqs[0].Maps[stateCandidates] {
		cands = append(cands, v.String())
	}
	if !reflect.DeepEqual(cands, []string{"git status"}) {
		t.Fatalf("candidates = %q", cands)
	}
	body := f.judgeBodies() + f.modelBodies()
	for _, never := range []string{vaultPass, "ghp_abcdef", "web.example.com"} {
		if strings.Contains(body, never) {
			t.Fatalf("sent %q", never)
		}
	}
}

// The fake Jev keel serve uses in end-to-end tests maps words to a marker
// command, so the e2e flow can see a history card.
func TestScriptedJudgeHistoryPick(t *testing.T) {
	historyReq := func(words string, cmds []string) provider.JudgeRequest {
		r := redact.New(redact.Options{})
		keyed := map[string]redact.Safe{}
		qs := map[string]provider.Question{"pick": historyPickQuestion(len(cmds)), "exists": historyExistsQuestion}
		for i, c := range cmds {
			keyed[historyKey(i)] = r.Text(c)
			qs[historyFitID(i)] = historyFitQuestion(i)
		}
		return provider.JudgeRequest{
			Fields:    map[string]redact.Safe{stateRequest: r.Text(words)},
			Maps:      map[string]map[string]redact.Safe{stateCandidates: keyed},
			Questions: qs,
		}
	}
	cases := []struct {
		words string
		want  int
	}{
		{"history marker", 1},
		{"# history marker", 1},
		{"something else", -1},
		{"unsure about it", -1},
	}
	for _, c := range cases {
		j := providertest.NewScriptedJudge()
		resp, err := j.Judge(context.Background(), historyReq(c.words, []string{"echo keel-other", "echo keel-history-marker"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := historyConfident(resp, 2); got != c.want {
			t.Fatalf("%q → %d, want %d (%+v)", c.words, got, c.want, resp)
		}
	}
}
