package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/shellmark"
	"keel/internal/store"
)

// groupStore adds a group (folder k8s, roles) to fakeStore: GroupMembers
// and ListGroups, as *store.Store has them.
type groupStore struct {
	*fakeStore
	gmu     sync.Mutex
	groups  []store.Group
	members map[string][]store.GroupMember
}

func (g *groupStore) GroupMembers(id string) (store.Group, []store.GroupMember, error) {
	g.gmu.Lock()
	defer g.gmu.Unlock()
	for _, gr := range g.groups {
		if gr.ID == id {
			return gr, append([]store.GroupMember(nil), g.members[id]...), nil
		}
	}
	return store.Group{}, nil, store.ErrNotFound
}

func (g *groupStore) ListGroups() ([]store.Group, error) {
	g.gmu.Lock()
	defer g.gmu.Unlock()
	return append([]store.Group(nil), g.groups...), nil
}

func (g *groupStore) leave(groupID, hostID string) {
	g.gmu.Lock()
	defer g.gmu.Unlock()
	var kept []store.GroupMember
	for _, m := range g.members[groupID] {
		if m.HostID != hostID {
			kept = append(kept, m)
		}
	}
	g.members[groupID] = kept
}

func (f *fakeStore) setReadable(id string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.hosts {
		if f.hosts[i].ID == id {
			f.hosts[i].AgentReadable = &on
		}
	}
}

type groupFixture struct {
	*fixture
	gs    *groupStore
	books *fakeBooks
	marks *markPanes
	notes *fakeNotes
}

// groupRunFixture: folder k8s (id grp-1) with wk-1, wk-2, wk-3 (worker),
// cp-1 (control-plane, production) and off-1 (worker, Agent can read off);
// web-1 is outside the group. Each host has one pane, s-<id>.
func groupRunFixture(t *testing.T, reply string) *groupFixture {
	t.Helper()
	f := newFixture(t, reply)
	f.st.hosts = append(f.st.hosts,
		store.Host{ID: "wk1", Name: "wk-1", AgentReadable: bptr(true), Group: "k8s", GroupID: "grp-1"},
		store.Host{ID: "wk2", Name: "wk-2", AgentReadable: bptr(true), Group: "k8s", GroupID: "grp-1"},
		store.Host{ID: "wk3", Name: "wk-3", AgentReadable: bptr(true), Group: "k8s", GroupID: "grp-1"},
		store.Host{ID: "cp1", Name: "cp-1", AgentReadable: bptr(true), Production: bptr(true), Group: "k8s", GroupID: "grp-1"},
		store.Host{ID: "off1", Name: "off-1", AgentReadable: bptr(false), Group: "k8s", GroupID: "grp-1"},
	)
	for _, id := range []string{"wk1", "wk2", "wk3", "cp1", "off1"} {
		f.panes.panes["s-"+id] = pane{host: id, text: "PANE-TEXT-" + id + "\n$ "}
	}
	gs := &groupStore{fakeStore: f.st,
		groups: []store.Group{{ID: "grp-1", Name: "k8s", Roles: map[string]string{"wk1": "worker", "wk2": "worker", "wk3": "worker", "cp1": "control-plane", "off1": "worker"}}},
		members: map[string][]store.GroupMember{"grp-1": {
			{HostID: "cp1", Name: "cp-1", Role: "control-plane"},
			{HostID: "off1", Name: "off-1", Role: "worker"},
			{HostID: "wk1", Name: "wk-1", Role: "worker"},
			{HostID: "wk2", Name: "wk-2", Role: "worker"},
			{HostID: "wk3", Name: "wk-3", Role: "worker"},
		}},
	}
	mp := &markPanes{fakePanes: f.panes, cmds: map[string][]shellmark.Command{}}
	books := &fakeBooks{}
	notes := &fakeNotes{}
	svc, err := New(gs, mp, f.log, Options{Runbooks: books, Notes: notes, Factory: func(string, string) (provider.Provider, error) { return f.fake, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return &groupFixture{fixture: f, gs: gs, books: books, marks: mp, notes: notes}
}

const restartCmd = "sudo systemctl restart kubelet"

func (g *groupFixture) kubeletRunbook(t *testing.T) store.Runbook {
	t.Helper()
	saved, err := g.svc.AddRunbook(RunbookInput{Name: "RUNBOOK-NAME restart kubelet", Steps: []store.RunbookStep{
		{Title: "STEP-TITLE nodes ready", Command: "kubectl get nodes", Role: "control-plane", OneHost: true},
		{Title: "STEP-TITLE restart", Command: restartCmd, Role: "worker"},
		{Title: "STEP-TITLE look", Description: "check by eye"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return saved.Runbook.Runbook
}

var allPanes = []string{"s-web", "s-db", "s-sec", "s-wk1", "s-wk2", "s-wk3", "s-cp1", "s-off1"}

func proposedHosts(run GroupRun) []string {
	var out []string
	if run.Current < 0 {
		return nil
	}
	for _, c := range run.Steps[run.Current].Cells {
		if c.State == cellProposed {
			out = append(out, c.HostID)
		}
	}
	return out
}

func cellOf(t *testing.T, run GroupRun, step int, host string) GroupRunCell {
	t.Helper()
	for _, c := range run.Steps[step].Cells {
		if c.HostID == host {
			return c
		}
	}
	t.Fatalf("no cell for %s in step %d: %+v", host, step+1, run.Steps[step].Cells)
	return GroupRunCell{}
}

// typeCell authorizes and records a type the way the session socket does.
func (g *groupFixture) typeCell(t *testing.T, run GroupRun, step int, host string, shift bool) error {
	t.Helper()
	c := cellOf(t, run, step, host)
	if c.Proposal == nil {
		t.Fatalf("%s has no proposal: %+v", host, c)
	}
	err := g.svc.AuthorizeType(TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: c.Proposal.Bytes, Shift: shift})
	if err == nil {
		g.svc.GroupTypeDoneAt(run.ID, c.Proposal.ID, 100)
	}
	return err
}

func (g *groupFixture) skipCP(t *testing.T, run GroupRun) GroupRun {
	t.Helper()
	// Step 1 (one control-plane host, production) goes to cp-1: typed,
	// exit 0, confirmed.
	if got := proposedHosts(run); len(got) != 1 || got[0] != "cp1" {
		t.Fatalf("step 1 proposed %v", got)
	}
	if err := g.typeCell(t, run, 0, "cp1", false); err != nil {
		t.Fatal(err)
	}
	g.marks.report("s-cp1", 120, 0)
	run, err := g.svc.GroupRunState(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c := cellOf(t, run, 0, "cp1"); c.State != cellTyped || !c.Confirm || c.Exit == nil || *c.Exit != 0 {
		t.Fatalf("production exit 0 did not wait for confirm: %+v", c)
	}
	if run.Current != 0 {
		t.Fatalf("moved on before confirm: %d", run.Current)
	}
	if run, err = g.svc.GroupHostAction(run.ID, run.Steps[0].ID, "cp1", "confirm"); err != nil {
		t.Fatal(err)
	}
	if run.Current != 1 {
		t.Fatalf("current %d after confirm", run.Current)
	}
	return run
}

// Per-host targeting: a step for role worker goes only to the group's
// workers the agent may read, never to off-1 (Agent can read off), never
// to a host outside the group, whatever panes the client sends.
func TestGroupRunTargetsOnlyReadableMembers(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, err := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeParallel})
	if err != nil {
		t.Fatal(err)
	}
	var excluded []string
	for _, m := range run.Members {
		if m.Excluded != "" {
			excluded = append(excluded, m.HostID)
		}
	}
	if len(excluded) != 1 || excluded[0] != "off1" {
		t.Fatalf("excluded %v", excluded)
	}
	var workers []string
	for _, c := range run.Steps[1].Cells {
		workers = append(workers, c.HostID)
	}
	if strings.Join(workers, ",") != "wk1,wk2,wk3" {
		t.Fatalf("worker step hosts %v", workers)
	}
	if run.Steps[2].Cells == nil || len(run.Steps[2].Cells) != 0 {
		t.Fatalf("a step with no command has hosts: %+v", run.Steps[2])
	}
	if run, err = g.svc.ProposeGroupStep(run.ID, allPanes); err != nil {
		t.Fatal(err)
	}
	run = g.skipCP(t, run)
	if run, err = g.svc.ProposeGroupStep(run.ID, allPanes); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(proposedHosts(run), ","); got != "wk1,wk2,wk3" {
		t.Fatalf("parallel proposed %q", got)
	}
	for _, c := range run.Steps[1].Cells {
		if c.SessionID != "s-"+c.HostID || c.Proposal.Bytes != "\x15"+restartCmd+"\n" || c.Proposal.Runbook == nil || c.Proposal.Runbook.Step != 2 {
			t.Fatalf("cell %+v proposal %+v", c, c.Proposal)
		}
	}
	// Calling again makes no second proposal.
	again, _ := g.svc.ProposeGroupStep(run.ID, allPanes)
	for i, c := range again.Steps[1].Cells {
		if c.Proposal.ID != run.Steps[1].Cells[i].Proposal.ID {
			t.Fatal("a second proposal for a host with a live one")
		}
	}
	// A proposal cannot be aimed at another host's pane.
	c := cellOf(t, run, 1, "wk1")
	for _, sid := range []string{"s-wk2", "s-web", "s-off1"} {
		if err := g.svc.AuthorizeType(TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: sid, Role: agent.RoleDriver, Bytes: c.Proposal.Bytes}); !errors.Is(err, ErrWrongPane) {
			t.Fatalf("typed into %s: %v", sid, err)
		}
	}
	// The scrollback of no pane was read: a group run reads nothing.
	if len(g.panes.reads) != 0 {
		t.Fatalf("panes read: %v", g.panes.reads)
	}
	if len(g.fake.Requests()) != 0 {
		t.Fatal("a group run called the provider")
	}
}

// Nothing is typed without the gate: every host × step is its own
// proposal; AuthorizeType refuses other bytes, a follower, a host that left
// the group or whose Agent can read was turned off, a second type, and a
// stopped run; production needs ⇧ per host.
func TestGroupRunNoTypeWithoutAuthorization(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeParallel})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	// cp-1 is production and the step is read-only: no ⇧ needed.
	run = g.skipCP(t, run)
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	ids := map[string]bool{}
	for _, c := range run.Steps[1].Cells {
		if ids[c.Proposal.ID] {
			t.Fatal("two hosts share a proposal")
		}
		ids[c.Proposal.ID] = true
	}
	c1 := cellOf(t, run, 1, "wk1")
	req := func(c GroupRunCell) TypeRequest {
		return TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: c.Proposal.Bytes}
	}
	r := req(c1)
	r.Bytes = "\x15rm -rf /\n"
	if err := g.svc.AuthorizeType(r); !errors.Is(err, ErrChanged) {
		t.Fatalf("other bytes: %v", err)
	}
	r = req(c1)
	r.Role = agent.RoleFollower
	if err := g.svc.AuthorizeType(r); !errors.Is(err, agent.ErrFollower) {
		t.Fatalf("follower: %v", err)
	}
	r = req(c1)
	r.GroupRunID = ""
	r.ConversationID = conv
	if err := g.svc.AuthorizeType(r); !errors.Is(err, ErrNotFound) {
		t.Fatalf("as a conversation's: %v", err)
	}
	r = req(c1)
	r.GroupRunID = "gr_nope"
	if err := g.svc.AuthorizeType(r); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
	// wk-2's Agent can read goes off after the proposal; wk-3 leaves.
	g.st.setReadable("wk2", false)
	if err := g.svc.AuthorizeType(req(cellOf(t, run, 1, "wk2"))); !errors.Is(err, ErrNotTarget) {
		t.Fatalf("unreadable now: %v", err)
	}
	g.gs.leave("grp-1", "wk3")
	if err := g.svc.AuthorizeType(req(cellOf(t, run, 1, "wk3"))); !errors.Is(err, ErrNotTarget) {
		t.Fatalf("left the group: %v", err)
	}
	// wk-1 is production now: a changes-state step needs ⇧.
	g.st.setProduction("wk1", true)
	if err := g.svc.AuthorizeType(req(c1)); !errors.Is(err, ErrNeedsShift) {
		t.Fatalf("production without shift: %v", err)
	}
	r = req(c1)
	r.Shift = true
	if err := g.svc.AuthorizeType(r); err != nil {
		t.Fatal(err)
	}
	g.svc.GroupTypeDoneAt(run.ID, c1.Proposal.ID, 100)
	if err := g.svc.AuthorizeType(r); !errors.Is(err, ErrNotProposed) {
		t.Fatalf("typed twice: %v", err)
	}
	if n := len(g.logKinds(agent.EventType)); n != 2 {
		t.Fatalf("agent.type entries %d", n)
	}
	// Proposing again leaves wk-2 and wk-3 blocked, never re-aimed.
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	for _, host := range []string{"wk2", "wk3"} {
		if c := cellOf(t, run, 1, host); c.State != cellProposed {
			t.Fatalf("%s %+v", host, c)
		}
	}
	// Stopped: nothing can be typed.
	if _, err := g.svc.StopGroupRun(run.ID); err != nil {
		t.Fatal(err)
	}
	g.st.setReadable("wk2", true)
	if err := g.svc.AuthorizeType(req(cellOf(t, run, 1, "wk2"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stopped run: %v", err)
	}
	if _, err := g.svc.ProposeGroupStep(run.ID, allPanes); !errors.Is(err, ErrNoGroupRun) {
		t.Fatalf("propose after stop: %v", err)
	}
}

// Rolling: one host at a time, in order. A host's exit 0 lets the next be
// proposed; a failure stops the step before the next host until the user
// retries or skips it; the next step waits for every host.
func TestGroupRunRolling(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeRolling})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	run = g.skipCP(t, run)
	step := func() GroupRun {
		r, err := g.svc.ProposeGroupStep(run.ID, allPanes)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	run = step()
	if got := strings.Join(proposedHosts(run), ","); got != "wk1" {
		t.Fatalf("rolling proposed %q", got)
	}
	if err := g.typeCell(t, run, 1, "wk1", false); err != nil {
		t.Fatal(err)
	}
	run = step()
	if len(proposedHosts(run)) != 0 {
		t.Fatal("next host proposed before wk-1 reported")
	}
	g.marks.report("s-wk1", 150, 0)
	run = step()
	if c := cellOf(t, run, 1, "wk1"); c.State != cellDone || *c.Exit != 0 || c.Result != "reported" {
		t.Fatalf("wk-1 %+v", c)
	}
	if got := strings.Join(proposedHosts(run), ","); got != "wk2" {
		t.Fatalf("after wk-1: %q", got)
	}
	if err := g.typeCell(t, run, 1, "wk2", false); err != nil {
		t.Fatal(err)
	}
	g.marks.report("s-wk2", 160, 1)
	run = step()
	if run.Halted != "wk-2" || cellOf(t, run, 1, "wk2").State != cellFailed || cellOf(t, run, 1, "wk3").State != cellWaiting {
		t.Fatalf("not halted at wk-2: %+v", run)
	}
	// The step after waits; wk-3 is not touched.
	if _, err := g.svc.GroupHostAction(run.ID, run.Steps[2].ID, "", "done"); !errors.Is(err, ErrStepNotCurrent) {
		t.Fatalf("skipped ahead: %v", err)
	}
	// Retry proposes wk-2 again, with a new proposal.
	old := cellOf(t, run, 1, "wk2").Proposal.ID
	if run, _ = g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk2", "retry"); run.Halted != "" {
		t.Fatal("still halted after retry")
	}
	run = step()
	if c := cellOf(t, run, 1, "wk2"); c.State != cellProposed || c.Proposal.ID == old {
		t.Fatalf("retry %+v", c)
	}
	// The old proposal cannot be typed.
	if err := g.svc.AuthorizeType(TypeRequest{GroupRunID: run.ID, ProposalID: old, SessionID: "s-wk2", Role: agent.RoleDriver, Bytes: "\x15" + restartCmd + "\n"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old proposal: %v", err)
	}
	if run, _ = g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk2", "skip"); cellOf(t, run, 1, "wk2").State != cellSkipped {
		t.Fatal("skip")
	}
	run = step()
	if got := strings.Join(proposedHosts(run), ","); got != "wk3" {
		t.Fatalf("after skipping wk-2: %q", got)
	}
	if err := g.typeCell(t, run, 1, "wk3", false); err != nil {
		t.Fatal(err)
	}
	// No marks: the user marks it done.
	run, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk3", "done")
	if err != nil || cellOf(t, run, 1, "wk3").Result != "user" || run.Current != 2 {
		t.Fatalf("mark done: %v %+v", err, run)
	}
	// Step 3 has no command: checked by the user, and the run finishes.
	if run, err = g.svc.GroupHostAction(run.ID, run.Steps[2].ID, "", "done"); err != nil || run.Status != runFinished || run.Current != -1 {
		t.Fatalf("finish: %v %+v", err, run)
	}
	// Logs: ids and counts, never a name, a title or a command.
	for _, m := range g.runbookLogs(t) {
		for _, bad := range []string{"RUNBOOK-NAME", "STEP-TITLE", "kubelet", "wk-", "cp-1", "k8s"} {
			if strings.Contains(m, bad) {
				t.Fatalf("log %q holds %q", m, bad)
			}
		}
	}
	joined := strings.Join(g.runbookLogs(t), "\n")
	for _, want := range []string{"group run " + run.ID + " started by you on group grp-1 (3 steps, 4 hosts, 1 left out", "step 2 of 3 · exit 1 reported by the shell", "step 2 of 3 skipped by you", "step 1 of 3 · exit 0 reported by the shell · waiting for you to confirm"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("logs lack %q:\n%s", want, joined)
		}
	}
}

// Parallel: every host at once; a failure does not stop the others, but
// the next step waits until the failed host is retried, skipped or marked.
// A host with no pane open is listed, not skipped.
func TestGroupRunParallelAndNeedPane(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeParallel})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	run = g.skipCP(t, run)
	// wk-3 has no pane in this window.
	run, _ = g.svc.ProposeGroupStep(run.ID, []string{"s-wk1", "s-wk2"})
	if got := strings.Join(proposedHosts(run), ","); got != "wk1,wk2" {
		t.Fatalf("proposed %q", got)
	}
	if len(run.NeedPane) != 1 || run.NeedPane[0].HostID != "wk3" {
		t.Fatalf("need pane %+v", run.NeedPane)
	}
	for _, h := range []string{"wk1", "wk2"} {
		if err := g.typeCell(t, run, 1, h, false); err != nil {
			t.Fatal(err)
		}
	}
	g.marks.report("s-wk1", 150, 2)
	g.marks.report("s-wk2", 150, 0)
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if cellOf(t, run, 1, "wk1").State != cellFailed || cellOf(t, run, 1, "wk2").State != cellDone || cellOf(t, run, 1, "wk3").State != cellProposed {
		t.Fatalf("parallel %+v", run.Steps[1].Cells)
	}
	if run.Halted != "" {
		t.Fatal("parallel halted")
	}
	if err := g.typeCell(t, run, 1, "wk3", false); err != nil {
		t.Fatal(err)
	}
	g.marks.report("s-wk3", 150, 0)
	run, _ = g.svc.GroupRunState(run.ID)
	if run.Current != 1 {
		t.Fatal("moved on with wk-1 failed")
	}
	run, _ = g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk1", "done")
	if run.Current != 2 {
		t.Fatalf("current %d", run.Current)
	}
	// Switching mode is logged; a stopped run takes no action.
	if _, err := g.svc.SetGroupRunMode(run.ID, "sideways"); !errors.Is(err, ErrBadRequest) {
		t.Fatal(err)
	}
	if _, err := g.svc.SetGroupRunMode(run.ID, ModeRolling); err != nil {
		t.Fatal(err)
	}
	g.svc.StopGroupRun(run.ID)
	if _, err := g.svc.GroupHostAction(run.ID, run.Steps[2].ID, "", "done"); !errors.Is(err, ErrNoGroupRun) {
		t.Fatalf("action after stop: %v", err)
	}
}

// A multi-line step waits for its last command on each host; a step for a
// role no host has waits for the user to skip it.
func TestGroupRunMultiLineAndEmptyRole(t *testing.T) {
	g := groupRunFixture(t, "ok")
	saved, err := g.svc.AddRunbook(RunbookInput{Name: "two lines", Steps: []store.RunbookStep{
		{Title: "two", Command: "echo a\necho b", Role: "worker"},
		{Title: "nobody", Command: "echo c", Role: "etcd"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: saved.Runbook.ID, GroupID: "grp-1", Mode: ModeParallel})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	for _, h := range []string{"wk1", "wk2", "wk3"} {
		if err := g.typeCell(t, run, 0, h, false); err != nil {
			t.Fatal(err)
		}
		g.marks.report("s-"+h, 150, 0)
	}
	run, _ = g.svc.GroupRunState(run.ID)
	if c := cellOf(t, run, 0, "wk1"); c.State != cellTyped {
		t.Fatalf("one of two commands ended: %+v", c)
	}
	for _, h := range []string{"wk1", "wk2", "wk3"} {
		g.marks.report("s-"+h, 160, 0)
	}
	run, _ = g.svc.GroupRunState(run.ID)
	if run.Current != 1 || len(run.Steps[1].Cells) != 0 {
		t.Fatalf("%+v", run)
	}
	if _, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "", "done"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("a command step with no host marked done: %v", err)
	}
	run, _ = g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "", "skip")
	if run.Status != runFinished {
		t.Fatalf("%+v", run)
	}
}

// Group notes (§17) go only with a read pane on a host in the group that
// the agent may read: not with a pane outside the group, not for a member
// whose Agent can read is off. They come with the roles of the hosts read,
// logged with the group's id and count only.
func TestGroupNotesScope(t *testing.T) {
	g := groupRunFixture(t, "ok")
	if _, err := g.svc.AddGroupNote("grp-1", "GROUP-NOTE-MARKER Calico pod CIDR 10.244.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.AddGroupNote("g-nope", "x"); err != nil {
		// fakeNotes does not check the group; the store does.
		t.Log(err)
	}
	ask := func(panes ...string) string {
		before := len(g.fake.Requests())
		if _, err := g.svc.Ask(context.Background(), AskRequest{ConversationID: newID("conv-"), Question: "why?", Panes: panes}); err != nil {
			t.Fatal(err)
		}
		reqs := g.fake.Requests()
		if len(reqs) != before+1 {
			t.Fatal("no request")
		}
		var b strings.Builder
		for _, m := range reqs[len(reqs)-1].Messages {
			b.WriteString(m.Text.String())
		}
		return b.String()
	}
	if body := ask("s-web"); strings.Contains(body, "GROUP-NOTE-MARKER") {
		t.Fatal("group note sent with a pane outside the group")
	}
	if body := ask("s-off1"); strings.Contains(body, "GROUP-NOTE-MARKER") || strings.Contains(body, "PANE-TEXT-off1") {
		t.Fatal("group note or pane sent for an unreadable member")
	}
	if body := ask(); strings.Contains(body, "GROUP-NOTE-MARKER") {
		t.Fatal("group note sent with no pane read")
	}
	body := ask("s-wk1")
	if !strings.Contains(body, "--- group notes · group k8s") || !strings.Contains(body, "GROUP-NOTE-MARKER") || !strings.Contains(body, "role of wk-1 in this group: worker") {
		t.Fatalf("group note missing: %s", body)
	}
	if strings.Contains(body, "role of cp-1") || strings.Contains(body, "role of off-1") {
		t.Fatal("roles of hosts not read were sent")
	}
	var sent []string
	for _, e := range g.logKinds(agent.EventNote) {
		if e.Source == "group" && strings.Contains(e.Message, "agent sent") {
			sent = append(sent, e.Message)
		}
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "1 group note for group grp-1") || strings.Contains(sent[0], "MARKER") {
		t.Fatalf("logs %v", sent)
	}
	// The user's own notes never include group notes.
	if body := ask("s-wk1"); strings.Contains(body, "your notes") {
		t.Fatal("a group note was sent as one of the user's notes")
	}
}
