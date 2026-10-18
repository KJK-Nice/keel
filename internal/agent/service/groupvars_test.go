package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/shellmark"
	"keel/internal/store"
)

// Groups, part 2 (docs/ui-redesign.md §17): captured values and group
// variables in a group run.

// varGroupStore adds group variables to groupStore.
type varGroupStore struct {
	*groupStore
	vmu  sync.Mutex
	vars map[string][]store.GroupVar
}

func (v *varGroupStore) SetGroupVar(groupID, name, value string) (store.Group, error) {
	name, value, err := store.CleanGroupVar(name, value)
	if err != nil {
		return store.Group{}, err
	}
	v.vmu.Lock()
	defer v.vmu.Unlock()
	var kept []store.GroupVar
	for _, x := range v.vars[groupID] {
		if x.Name != name {
			kept = append(kept, x)
		}
	}
	if value != "" {
		kept = append(kept, store.GroupVar{Name: name, Value: value})
	}
	v.vars[groupID] = kept
	return store.Group{ID: groupID, Vars: kept}, nil
}

func (v *varGroupStore) AllGroupVars() (map[string][]store.GroupVar, error) {
	v.vmu.Lock()
	defer v.vmu.Unlock()
	out := map[string][]store.GroupVar{}
	for k, x := range v.vars {
		out[k] = append([]store.GroupVar(nil), x...)
	}
	return out, nil
}

// outPanes adds each command's raw output to markPanes.
type outPanes struct {
	*markPanes
	omu   sync.Mutex
	out   map[string]map[uint64]string
	reads int
}

func (o *outPanes) CommandOutput(id string, c shellmark.Command) ([]byte, bool) {
	o.omu.Lock()
	defer o.omu.Unlock()
	o.reads++
	t, ok := o.out[id][c.OutputStart]
	return []byte(t), ok
}

func (o *outPanes) say(id string, at uint64, exit int, text string) {
	o.omu.Lock()
	if o.out[id] == nil {
		o.out[id] = map[uint64]string{}
	}
	o.out[id][at] = text
	o.omu.Unlock()
	o.report(id, at, exit)
}

type varFixture struct {
	*groupFixture
	vs *varGroupStore
	op *outPanes
}

const (
	joinToken = "abcdef.0123456789abcdef"
	joinOut   = "\x1b[1mYour control-plane is ready\x1b[0m\r\nkubeadm join 10.20.0.10:6443 --token " + joinToken + " \\\r\n\t--discovery-token-ca-cert-hash sha256:4f1c\r\n"
)

func varRunFixture(t *testing.T) *varFixture {
	t.Helper()
	g := groupRunFixture(t, "ok")
	vs := &varGroupStore{groupStore: g.gs, vars: map[string][]store.GroupVar{}}
	op := &outPanes{markPanes: g.marks, out: map[string]map[uint64]string{}}
	svc, err := New(vs, op, g.log, Options{Runbooks: g.books, Notes: g.notes, Factory: func(string, string) (provider.Provider, error) { return g.fake, nil }})
	if err != nil {
		t.Fatal(err)
	}
	g.svc = svc
	return &varFixture{groupFixture: g, vs: vs, op: op}
}

// joinRunbook: step 1 on one control-plane host captures join_token; step
// 2 joins every worker with it and the group's k8s_version.
func (v *varFixture) joinRunbook(t *testing.T, join string) store.Runbook {
	t.Helper()
	saved, err := v.svc.AddRunbook(RunbookInput{Name: "bootstrap", Steps: []store.RunbookStep{
		{Title: "init", Command: "kubeadm token create --print-join-command", Role: "control-plane", OneHost: true,
			Captures: []store.StepCapture{{Name: "join_token", Pattern: `--token\s+(\S+)`}}},
		{Title: "join", Command: join, Role: "worker"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return saved.Runbook.Runbook
}

// startToStep2 runs step 1 on cp-1 with output and returns the run at step
// 2 with its proposals made.
func (v *varFixture) startToStep2(t *testing.T, b store.Runbook, out string) GroupRun {
	t.Helper()
	run, err := v.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeParallel})
	if err != nil {
		t.Fatal(err)
	}
	if run, err = v.svc.ProposeGroupStep(run.ID, allPanes); err != nil {
		t.Fatal(err)
	}
	if err := v.typeCell(t, run, 0, "cp1", true); err != nil {
		t.Fatal(err)
	}
	v.op.say("s-cp1", 120, 0, out)
	if run, err = v.svc.GroupRunState(run.ID); err != nil {
		t.Fatal(err)
	}
	if run, err = v.svc.GroupHostAction(run.ID, run.Steps[0].ID, "cp1", "confirm"); err != nil {
		t.Fatal(err)
	}
	if run, err = v.svc.ProposeGroupStep(run.ID, allPanes); err != nil {
		t.Fatal(err)
	}
	if run.Current != 1 {
		t.Fatalf("not at step 2: %d", run.Current)
	}
	return run
}

func (v *varFixture) logText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, e := range v.log.Recent(1000) {
		j, _ := json.Marshal(e)
		b.Write(j)
	}
	return b.String()
}

// The whole path: capture on cp-1 (by keel serve, from the output between
// the marks), the stored proposal keeps {{join_token}}, the fill for the
// approve dialog has the value, and Type is authorized only with exactly
// the filled bytes. Neither the view, the logs nor the provider see the
// value.
func TestCaptureFillTypeExactBytes(t *testing.T) {
	v := varRunFixture(t)
	if _, err := v.svc.SetGroupVar("grp-1", "k8s_version", "1.31.2"); err != nil {
		t.Fatal(err)
	}
	b := v.joinRunbook(t, "sudo kubeadm join 10.20.0.10:6443 --token {{join_token}} --version {{k8s_version}}")
	run := v.startToStep2(t, b, joinOut)

	c0 := cellOf(t, run, 0, "cp1")
	if len(c0.Captures) != 1 || c0.Captures[0].State != CaptureCaptured || c0.Captures[0].Bytes != len(joinToken) || c0.Captures[0].ExpiresAt == nil {
		t.Fatalf("capture view %+v", c0.Captures)
	}
	view, _ := json.Marshal(run)
	if strings.Contains(string(view), joinToken) {
		t.Fatal("the run view holds the captured value")
	}
	if len(run.Steps[1].Vars) != 2 || run.Steps[1].Vars[0].Name != "join_token" {
		t.Fatalf("step vars %+v", run.Steps[1].Vars)
	}

	c := cellOf(t, run, 1, "wk1")
	if c.Proposal == nil || !strings.Contains(c.Proposal.Bytes, "{{join_token}}") || strings.Contains(c.Proposal.Bytes, joinToken) {
		t.Fatalf("stored proposal %+v", c.Proposal)
	}
	fills, err := v.svc.FillGroupProposals(run.ID, []string{c.Proposal.ID})
	if err != nil || len(fills) != 1 || fills[0].Error != "" {
		t.Fatalf("fill %+v %v", fills, err)
	}
	want := "\x15sudo kubeadm join 10.20.0.10:6443 --token " + joinToken + " --version 1.31.2\n"
	if fills[0].Bytes != want {
		t.Fatalf("filled %q", fills[0].Bytes)
	}
	if len(fills[0].Used) != 2 || fills[0].Used[0].From != "captured on cp-1 · step 1" || fills[0].Used[1].From != "group variable" {
		t.Fatalf("used %+v", fills[0].Used)
	}

	// The approved bytes are the filled ones: the template, or anything
	// else, is refused.
	for _, bad := range []string{c.Proposal.Bytes, strings.Replace(want, joinToken, "other.0123456789abcdef", 1), want + " "} {
		if err := v.svc.AuthorizeType(TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: bad, Shift: true}); !errors.Is(err, ErrChanged) {
			t.Fatalf("bytes %q: %v", bad, err)
		}
	}
	if err := v.svc.AuthorizeType(TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: want, Shift: true}); err != nil {
		t.Fatal(err)
	}
	v.svc.GroupTypeDoneAt(run.ID, c.Proposal.ID, 200)

	// Logs: counts only.
	logs := v.logText(t)
	if strings.Contains(logs, joinToken) || strings.Contains(logs, "1.31.2") || strings.Contains(logs, "join_token") {
		t.Fatalf("a value or name reached keel.log: %s", logs)
	}
	if !strings.Contains(logs, "1 value captured from the step's output, 0 not (values not logged)") || !strings.Contains(logs, "2 variables filled in at Type (values not logged)") {
		t.Fatalf("capture or fill not logged: %s", logs)
	}
	if len(v.fake.Requests()) != 0 {
		t.Fatal("a group run called the provider")
	}
}

// §6.2–6.4 for values: a pane that shows a captured value or a group
// variable's value is read with {{name}} in their place.
func TestCapturedValuesNeverReachTheModel(t *testing.T) {
	v := varRunFixture(t)
	if _, err := v.svc.SetGroupVar("grp-1", "api_vip", "10.20.0.10"); err != nil {
		t.Fatal(err)
	}
	b := v.joinRunbook(t, "sudo kubeadm join {{api_vip}}:6443 --token {{join_token}}")
	v.startToStep2(t, b, joinOut)
	v.panes.mu.Lock()
	v.panes.panes["s-wk1"] = pane{host: "wk1", text: "$ sudo kubeadm join 10.20.0.10:6443 --token " + joinToken + "\n[preflight] ok\n"}
	v.panes.mu.Unlock()
	if _, err := v.svc.Ask(context.Background(), AskRequest{ConversationID: newID("conv-"), Question: "did " + joinToken + " work?", Panes: []string{"s-wk1"}}); err != nil {
		t.Fatal(err)
	}
	sent := v.requestBodies(t)
	if strings.Contains(sent, joinToken) || strings.Contains(sent, "10.20.0.10") {
		t.Fatalf("a value reached the provider: %s", sent)
	}
	if !strings.Contains(sent, "{{join_token}}") || !strings.Contains(sent, "{{api_vip}}") {
		t.Fatalf("names not sent in their place: %s", sent)
	}
}

// Shell safety: an unquoted value must be one plain word; {{name|q}}
// single-quotes it; nothing with a quote, backslash or line break is ever
// typed.
func TestFillShellSafety(t *testing.T) {
	v := varRunFixture(t)
	if _, err := v.svc.SetGroupVar("grp-1", "opts", "a b; rm -rf /"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.svc.SetGroupVar("grp-1", "flag", "-rf"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.svc.SetGroupVar("grp-1", "bad", "it's"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.svc.SetGroupVar("grp-1", "nl", "a\nb"); err == nil {
		t.Fatal("a line break was kept in a group variable")
	}
	cases := []struct {
		cmd, want, errHas string
	}{
		{"echo {{opts}}", "", "{{opts|q}}"},
		{"echo {{opts|q}}", "\x15echo 'a b; rm -rf /'\n", ""},
		// A leading - is an option to the command even in quotes.
		{"rm {{flag|q}} /tmp/x", "", "starts with -"},
		{"rm {{flag}} /tmp/x", "", "starts with -"},
		{"echo {{bad|q}}", "", "single quote"},
		{"echo {{nope}}", "", "has no value"},
		{"echo {{NOPE}}", "", "not a variable name"},
		{"kubectl get pods -o go-template='{{range .items}}{{.metadata.name}}{{end}}'", "\x15kubectl get pods -o go-template='{{range .items}}{{.metadata.name}}{{end}}'\n", ""},
	}
	for _, tc := range cases {
		b := v.joinRunbook(t, tc.cmd)
		run := v.startToStep2(t, b, joinOut)
		c := cellOf(t, run, 1, "wk2")
		fills, err := v.svc.FillGroupProposals(run.ID, []string{c.Proposal.ID})
		if err != nil || len(fills) != 1 {
			t.Fatalf("%s: %v %+v", tc.cmd, err, fills)
		}
		f := fills[0]
		aerr := v.svc.AuthorizeType(TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: f.Bytes, Shift: true})
		if tc.errHas != "" {
			if f.Bytes != "" || !strings.Contains(f.Error, tc.errHas) {
				t.Fatalf("%s: fill %+v", tc.cmd, f)
			}
			if aerr == nil {
				t.Fatalf("%s: typed", tc.cmd)
			}
			continue
		}
		if f.Error != "" || f.Bytes != tc.want || aerr != nil {
			t.Fatalf("%s: fill %+v, type %v", tc.cmd, f, aerr)
		}
		if _, err := v.svc.StopGroupRun(run.ID); err != nil {
			t.Fatal(err)
		}
	}
}

// A captured value that holds a vault value is never kept or typed; a
// value with a line break is refused; no output (no marks) captures
// nothing and the later step's variable is missing.
func TestCaptureBlockedRefusedMissing(t *testing.T) {
	cases := []struct {
		name, out, state string
	}{
		{"vault", "kubeadm join x --token " + vaultPass + "\n", CaptureBlocked},
		{"nomatch", "nothing here\n", CaptureNoMatch},
	}
	for _, tc := range cases {
		v := varRunFixture(t)
		b := v.joinRunbook(t, "echo {{join_token}}")
		run := v.startToStep2(t, b, tc.out)
		if got := cellOf(t, run, 0, "cp1").Captures; len(got) != 1 || got[0].State != tc.state || got[0].Bytes != 0 {
			t.Fatalf("%s: %+v", tc.name, got)
		}
		c := cellOf(t, run, 1, "wk1")
		fills, _ := v.svc.FillGroupProposals(run.ID, []string{c.Proposal.ID})
		if len(fills) != 1 || !strings.Contains(fills[0].Error, "has no value") {
			t.Fatalf("%s: %+v", tc.name, fills)
		}
		if err := v.svc.AuthorizeType(TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: c.Proposal.Bytes, Shift: true}); !errors.Is(err, ErrFill) {
			t.Fatalf("%s: typed: %v", tc.name, err)
		}
		if strings.Contains(v.logText(t), vaultPass) {
			t.Fatal("vault value in the log")
		}
	}

	// A pattern spanning lines yields a value with a line break: refused.
	v := varRunFixture(t)
	saved, err := v.svc.AddRunbook(RunbookInput{Name: "multi", Steps: []store.RunbookStep{
		{Title: "init", Command: "cat x", Role: "control-plane", OneHost: true, Captures: []store.StepCapture{{Name: "two", Pattern: `(?s)BEGIN(.*)END`}}},
		{Title: "use", Command: "echo {{two|q}}", Role: "worker"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	run := v.startToStep2(t, saved.Runbook.Runbook, "BEGIN a\nb END\n")
	if got := cellOf(t, run, 0, "cp1").Captures; len(got) != 1 || got[0].State != CaptureRefused {
		t.Fatalf("multi-line: %+v", got)
	}
}

// A capture is kept for an hour at most, and dropped when the run ends.
func TestCaptureExpiresAndEndsWithRun(t *testing.T) {
	v := varRunFixture(t)
	now := time.Now()
	v.svc.now = func() time.Time { return now }
	b := v.joinRunbook(t, "echo {{join_token}}")
	run := v.startToStep2(t, b, joinOut)
	if n := liveCaptures(v.svc); n != 1 {
		t.Fatalf("held values %d", n)
	}
	now = now.Add(captureTTL + time.Second)
	run, _ = v.svc.GroupRunState(run.ID)
	if got := cellOf(t, run, 0, "cp1").Captures; got[0].State != CaptureExpired {
		t.Fatalf("not expired: %+v", got)
	}
	c := cellOf(t, run, 1, "wk1")
	if fills, _ := v.svc.FillGroupProposals(run.ID, []string{c.Proposal.ID}); !strings.Contains(fills[0].Error, "expired") {
		t.Fatalf("expired value filled: %+v", fills)
	}

	v2 := varRunFixture(t)
	run = v2.startToStep2(t, v2.joinRunbook(t, "echo {{join_token}}"), joinOut)
	if _, err := v2.svc.StopGroupRun(run.ID); err != nil {
		t.Fatal(err)
	}
	if n := liveCaptures(v2.svc); n != 0 {
		t.Fatalf("a stopped run still holds %d values", n)
	}
}

// Retrying the capturing step (here on a production host whose reported
// exit 0 waits for Confirm) forgets its value until it is captured again.
func TestRetryDropsCapture(t *testing.T) {
	v := varRunFixture(t)
	saved, err := v.svc.AddRunbook(RunbookInput{Name: "one", Steps: []store.RunbookStep{
		{Title: "init", Command: "kubeadm token create", Role: "control-plane", Captures: []store.StepCapture{{Name: "join_token", Pattern: `--token\s+(\S+)`}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := v.svc.StartGroupRun(GroupRunRequest{RunbookID: saved.Runbook.ID, GroupID: "grp-1", Mode: ModeParallel})
	run, _ = v.svc.ProposeGroupStep(run.ID, allPanes)
	if err := v.typeCell(t, run, 0, "cp1", true); err != nil {
		t.Fatal(err)
	}
	v.op.say("s-cp1", 120, 0, joinOut)
	run, _ = v.svc.GroupRunState(run.ID)
	if liveCaptures(v.svc) != 1 {
		t.Fatal("not captured")
	}
	if _, err := v.svc.GroupHostAction(run.ID, run.Steps[0].ID, "cp1", "retry"); err != nil {
		t.Fatal(err)
	}
	if liveCaptures(v.svc) != 0 {
		t.Fatal("retry kept the old value")
	}
}

// Outside a group run nothing fills {{name}}: a conversation's proposal
// that names one is not typed.
func TestVarsOutsideGroupRunRefused(t *testing.T) {
	reply := "```keel-proposal\n{\"pane\": 1, \"command\": \"kubeadm join --token {{join_token}}\", \"why\": \"join\", \"risk\": \"changes\"}\n```"
	f, _, _ := booksFixture(t, reply)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "join", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	p := resp.Proposals[0]
	if err := f.svc.AuthorizeType(TypeRequest{ConversationID: conv, ProposalID: p.ID, SessionID: "s-web", Role: agent.RoleDriver, Bytes: p.Bytes, Shift: true}); !errors.Is(err, ErrVarsOutsideRun) {
		t.Fatalf("typed a variable outside a run: %v", err)
	}
}

// Group variables are config, never secrets: a vault value or a
// secret-looking value is refused.
func TestGroupVarRefusesSecrets(t *testing.T) {
	v := varRunFixture(t)
	for _, val := range []string{vaultPass, "password=hunter2hunter2", "AKIAIOSFODNN7EXAMPLE"} {
		if _, err := v.svc.SetGroupVar("grp-1", "x", val); !errors.Is(err, ErrGroupVarSecret) {
			t.Fatalf("%q kept: %v", val, err)
		}
	}
	if _, err := v.svc.SetGroupVar("grp-1", "9bad", "x"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("bad name: %v", err)
	}
	// A piece of a vault value is refused too.
	if _, err := v.svc.SetGroupVar("grp-1", "x", vaultPass[5:]); !errors.Is(err, ErrGroupVarSecret) {
		t.Fatalf("a piece of a vault value kept: %v", err)
	}
}

// Every Redactor the service builds replaces group variables' values, but
// a command the user writes with one (a runbook step) is kept as written:
// a variable is config, not a secret.
func TestGroupVarsInKeptText(t *testing.T) {
	v := varRunFixture(t)
	if _, err := v.svc.SetGroupVar("grp-1", "api_vip", "10.20.0.10"); err != nil {
		t.Fatal(err)
	}
	saved, err := v.svc.AddRunbook(RunbookInput{Name: "ping", Steps: []store.RunbookStep{{Title: "ping the VIP 10.20.0.10", Command: "ping -c1 10.20.0.10"}}})
	if err != nil {
		t.Fatalf("a command holding a variable's value refused: %v", err)
	}
	if st := saved.Runbook.Steps[0]; st.Command != "ping -c1 10.20.0.10" || st.Title != "ping the VIP {{api_vip}}" {
		t.Fatalf("step %+v", st)
	}
	n, err := v.svc.AddNote("", "the API VIP is 10.20.0.10")
	if err != nil || n.Note.Text != "the API VIP is {{api_vip}}" {
		t.Fatalf("note %+v %v", n.Note, err)
	}
}

// liveCaptures counts the values runs hold for filling.
func liveCaptures(s *Service) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.groupRuns {
		for _, byName := range r.captures {
			n += len(byName)
		}
	}
	return n
}

// A captured value stays in the scrollback after its run: finished,
// stopped or expired, it is still withheld from every read for the life of
// keel serve, though no longer filled in.
func TestCapturedValueWithheldAfterRunEnds(t *testing.T) {
	for _, end := range []string{"stop", "finish", "expire"} {
		v := varRunFixture(t)
		now := time.Now()
		v.svc.now = func() time.Time { return now }
		run := v.startToStep2(t, v.joinRunbook(t, "echo {{join_token}}"), joinOut)
		switch end {
		case "stop":
			if _, err := v.svc.StopGroupRun(run.ID); err != nil {
				t.Fatal(err)
			}
		case "finish":
			if _, err := v.svc.GroupHostAction(run.ID, run.Steps[1].ID, "", "skip"); err != nil {
				t.Fatal(err)
			}
			if r, _ := v.svc.GroupRunState(run.ID); r.Status != runFinished {
				t.Fatalf("not finished: %s", r.Status)
			}
		case "expire":
			now = now.Add(captureTTL + time.Minute)
			_, _ = v.svc.GroupRunState(run.ID)
		}
		if liveCaptures(v.svc) != 0 {
			t.Fatalf("%s: the value is still held for filling", end)
		}
		v.panes.mu.Lock()
		v.panes.panes["s-cp1"] = pane{host: "cp1", text: "kubeadm join 10.20.0.10:6443 --token " + joinToken + "\n$ "}
		v.panes.mu.Unlock()
		if _, err := v.svc.Ask(context.Background(), AskRequest{ConversationID: newID("conv-"), Question: "what now?", Panes: []string{"s-cp1"}}); err != nil {
			t.Fatal(err)
		}
		sent := v.requestBodies(t)
		if strings.Contains(sent, joinToken) || !strings.Contains(sent, "{{join_token}}") {
			t.Fatalf("%s: the value reached the provider after its run: %s", end, sent)
		}
		// And a note saved after the run keeps the name, not the value.
		if r, err := v.svc.saveRedactor(); err != nil || strings.Contains(r.Text("token "+joinToken).String(), joinToken) {
			t.Fatalf("%s: save Redactor kept the value", end)
		}
	}
}

// A value captured by a step that runs on each host is that host's own:
// it is never filled in on another host. Only a One host only step's value
// serves the whole run.
func TestPerHostCaptureNotFilledElsewhere(t *testing.T) {
	v := varRunFixture(t)
	saved, err := v.svc.AddRunbook(RunbookInput{Name: "per host", Steps: []store.RunbookStep{
		{Title: "id", Command: "cat /etc/machine-id", Role: "worker", Captures: []store.StepCapture{{Name: "node_id", Pattern: `id=(\S+)`}}},
		{Title: "use", Command: "echo {{node_id}}", Role: "worker"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := v.svc.StartGroupRun(GroupRunRequest{RunbookID: saved.Runbook.ID, GroupID: "grp-1", Mode: ModeParallel})
	run, _ = v.svc.ProposeGroupStep(run.ID, allPanes)
	if err := v.typeCell(t, run, 0, "wk1", false); err != nil {
		t.Fatal(err)
	}
	v.op.say("s-wk1", 120, 0, "id=abc123def\n")
	run, _ = v.svc.GroupRunState(run.ID)
	for _, h := range []string{"wk2", "wk3"} {
		if run, err = v.svc.GroupHostAction(run.ID, run.Steps[0].ID, h, "skip"); err != nil {
			t.Fatal(err)
		}
	}
	run, _ = v.svc.ProposeGroupStep(run.ID, allPanes)
	var ids []string
	for _, h := range []string{"wk1", "wk2"} {
		ids = append(ids, cellOf(t, run, 1, h).Proposal.ID)
	}
	fills, err := v.svc.FillGroupProposals(run.ID, ids)
	if err != nil || len(fills) != 2 {
		t.Fatal(err)
	}
	if fills[0].Bytes != "\x15echo abc123def\n" || fills[0].Error != "" {
		t.Fatalf("own host: %+v", fills[0])
	}
	if fills[1].Bytes != "" || !strings.Contains(fills[1].Error, "has no value for wk-2: it was captured on wk-1 by a step that runs on each host") {
		t.Fatalf("other host filled with wk-1's value: %+v", fills[1])
	}
}

// A captured piece of a vault value (6 bytes or more) is blocked like the
// whole value.
func TestCapturedPieceOfVaultValueBlocked(t *testing.T) {
	v := varRunFixture(t)
	piece := vaultPass[6:20]
	run := v.startToStep2(t, v.joinRunbook(t, "echo {{join_token}}"), "kubeadm join x --token "+piece+"\n")
	if got := cellOf(t, run, 0, "cp1").Captures; len(got) != 1 || got[0].State != CaptureBlocked {
		t.Fatalf("piece kept: %+v", got)
	}
}
