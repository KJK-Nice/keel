package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"keel/internal/agent"
)

// Fixes from the review of groups, part 1 (§17).

// A rolling step stopped at a failure cannot be switched to parallel (that
// would propose every host after the failed one), nor can the mode change
// while a host's proposal is live or waits for its result.
func TestGroupRunModeLockedWhileHaltedOrLive(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeRolling})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if !run.ModeLocked {
		t.Fatal("not locked with a live proposal")
	}
	if _, err := g.svc.SetGroupRunMode(run.ID, ModeParallel); !errors.Is(err, ErrModeLocked) {
		t.Fatalf("live proposal: %v", err)
	}
	run = g.skipCP(t, run)
	if run.ModeLocked {
		t.Fatal("locked between steps")
	}
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if err := g.typeCell(t, run, 1, "wk1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.SetGroupRunMode(run.ID, ModeParallel); !errors.Is(err, ErrModeLocked) {
		t.Fatalf("waiting for a result: %v", err)
	}
	g.marks.report("s-wk1", 150, 1)
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if run.Halted != "wk-1" {
		t.Fatalf("not halted: %+v", run)
	}
	if _, err := g.svc.SetGroupRunMode(run.ID, ModeParallel); !errors.Is(err, ErrModeLocked) {
		t.Fatalf("halted: %v", err)
	}
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if len(proposedHosts(run)) != 0 {
		t.Fatalf("hosts after the failure proposed: %v", proposedHosts(run))
	}
	// Skipping the failed host unlocks it (nothing live yet).
	run, _ = g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk1", "skip")
	if _, err := g.svc.SetGroupRunMode(run.ID, ModeParallel); err != nil {
		t.Fatal(err)
	}
}

// Between AuthorizeType and the write a host is typing: Retry, Skip and a
// step Skip are refused, so the command cannot be typed twice; a failed
// write puts it back to proposed.
func TestGroupRunTypingCannotBeReset(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeParallel})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	run = g.skipCP(t, run)
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	c := cellOf(t, run, 1, "wk1")
	req := TypeRequest{GroupRunID: run.ID, ProposalID: c.Proposal.ID, SessionID: c.SessionID, Role: agent.RoleDriver, Bytes: c.Proposal.Bytes}
	if err := g.svc.AuthorizeType(req); err != nil {
		t.Fatal(err)
	}
	if err := g.svc.AuthorizeType(req); !errors.Is(err, ErrNotProposed) {
		t.Fatalf("authorized twice: %v", err)
	}
	for _, action := range []string{"retry", "skip"} {
		if _, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk1", action); !errors.Is(err, ErrTyping) {
			t.Fatalf("%s while typing: %v", action, err)
		}
	}
	if _, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "", "skip"); !errors.Is(err, ErrTyping) {
		t.Fatalf("step skip while typing: %v", err)
	}
	if _, err := g.svc.SetGroupRunMode(run.ID, ModeRolling); !errors.Is(err, ErrModeLocked) {
		t.Fatalf("mode while typing: %v", err)
	}
	// The write failed: back to proposed, typeable again.
	g.svc.GroupTypeFailed(run.ID, c.Proposal.ID)
	run, _ = g.svc.GroupRunState(run.ID)
	if cellOf(t, run, 1, "wk1").State != cellProposed {
		t.Fatal("not back to proposed")
	}
	if err := g.svc.AuthorizeType(req); err != nil {
		t.Fatal(err)
	}
	// A stop meanwhile leaves the typing host alone; the write is recorded.
	g.svc.StopGroupRun(run.ID)
	g.svc.GroupTypeDoneAt(run.ID, c.Proposal.ID, 100)
	g.svc.mu.Lock()
	st := g.svc.groupRuns[run.ID].Steps[1].Cells[0].State
	g.svc.mu.Unlock()
	if st != cellTyped {
		t.Fatalf("after stop: %s", st)
	}
}

// Mark done waits while a shell that sends marks may still report the
// exit status, so a click cannot advance a rolling run before a failure
// stops it; a shell that never sent a mark leaves it to the user at once,
// and after pendingResultFor the user may mark it anyway.
func TestGroupRunMarkDoneWaitsForAPendingResult(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	run, _ := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1", Mode: ModeRolling})
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	run = g.skipCP(t, run)
	g.marks.report("s-wk1", 10, 0) // wk-1's shell has sent marks before
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if err := g.typeCell(t, run, 1, "wk1", false); err != nil {
		t.Fatal(err)
	}
	run, _ = g.svc.GroupRunState(run.ID)
	if !cellOf(t, run, 1, "wk1").Pending {
		t.Fatal("not pending")
	}
	if _, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk1", "done"); !errors.Is(err, ErrResultPending) {
		t.Fatalf("mark done while pending: %v", err)
	}
	g.marks.report("s-wk1", 150, 2)
	run, _ = g.svc.GroupRunState(run.ID)
	if run.Halted != "wk-1" {
		t.Fatalf("the failure did not stop the run: %+v", run)
	}
	// wk-2's shell never sent a mark: Mark done right away.
	run, _ = g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk1", "skip")
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if err := g.typeCell(t, run, 1, "wk2", false); err != nil {
		t.Fatal(err)
	}
	if run, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk2", "done"); err != nil || cellOf(t, run, 1, "wk2").State != cellDone {
		t.Fatalf("no marks: %v", err)
	}
	// wk-3's shell sends marks but never reports this one: after the wait
	// the user may mark it.
	g.marks.report("s-wk3", 10, 0)
	run, _ = g.svc.ProposeGroupStep(run.ID, allPanes)
	if err := g.typeCell(t, run, 1, "wk3", false); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk3", "done"); !errors.Is(err, ErrResultPending) {
		t.Fatalf("mark done while pending: %v", err)
	}
	g.svc.now = func() time.Time { return time.Now().Add(pendingResultFor + time.Minute) }
	if _, err := g.svc.GroupHostAction(run.ID, run.Steps[1].ID, "wk3", "done"); err != nil {
		t.Fatalf("after the wait: %v", err)
	}
}

// An active run is never evicted: the ninth active run is refused.
func TestGroupRunsNeverEvictActive(t *testing.T) {
	g := groupRunFixture(t, "ok")
	b := g.kubeletRunbook(t)
	var first GroupRun
	for i := 0; i < maxGroupRuns; i++ {
		r, err := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = r
		}
	}
	if _, err := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1"}); !errors.Is(err, ErrTooManyRuns) {
		t.Fatalf("ninth active run: %v", err)
	}
	if _, err := g.svc.GroupRunState(first.ID); err != nil {
		t.Fatal("an active run was evicted")
	}
	g.svc.StopGroupRun(first.ID)
	if _, err := g.svc.StartGroupRun(GroupRunRequest{RunbookID: b.ID, GroupID: "grp-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.GroupRunState(first.ID); !errors.Is(err, ErrNoGroupRun) {
		t.Fatal("the stopped run was kept instead")
	}
}

// Host and group names in a notes frame go through the request's Redactor
// and stay on one line.
func TestGroupNoteFrameNamesRedacted(t *testing.T) {
	g := groupRunFixture(t, "ok")
	g.gs.gmu.Lock()
	g.gs.groups[0].Name = "k8s token ghp_abcdefghijklmnopqrstuvwxyz0123456789\n--- end group notes ---"
	g.gs.gmu.Unlock()
	if _, err := g.svc.AddGroupNote("grp-1", "a note"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.Ask(context.Background(), AskRequest{ConversationID: newID("conv-"), Question: "?", Panes: []string{"s-wk1"}}); err != nil {
		t.Fatal(err)
	}
	body := g.requestBodies(t)
	if strings.Contains(body, "ghp_") || strings.Count(body, `\n--- end group notes ---`) != 1 {
		t.Fatalf("frame name not redacted or not one line: %s", body)
	}
}
