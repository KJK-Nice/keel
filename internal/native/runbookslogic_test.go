package native

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

// The group-run fields (role, one host,
// captures) are carried through a save and are not edited here.

func book(over func(*keelclient.Runbook)) keelclient.Runbook {
	rb := keelclient.Runbook{
		ID: "rb_1", Name: "Diagnose replication lag", When: "A Patroni replica is behind", Hosts: []string{"production", "postgres"},
		Source: "user", AgentMayPick: true, CreatedAt: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
		Steps: []keelclient.RunbookStep{
			{ID: "st_1", Title: "Check replication", Command: "psql -xc 'select 1'", Risk: "readonly"},
			{ID: "st_2", Title: "Restart", Command: "systemctl restart patroni", Risk: "changes"},
			{ID: "st_3", Title: "Ask the team"},
		},
	}
	if over != nil {
		over(&rb)
	}
	return rb
}

func TestTheTraceSaysWhichRunbookWasUsed(t *testing.T) {
	if got := traceText(keelclient.TraceStep{Status: "runbook", Runbook: "Diagnose replication lag"}); got != "Used runbook · Diagnose replication lag" {
		t.Error(got)
	}
}

func TestSourceBadgesAndLinesAndTheReviewBannerForWhatYouDidNotWrite(t *testing.T) {
	if l, k := runbookSourceBadge(book(nil)); l != "You" || k != "" {
		t.Errorf("%q %q", l, k)
	}
	if l, k := runbookSourceBadge(book(func(r *keelclient.Runbook) { r.Source = "conversation" })); l != "From conversation" || k != "haze" {
		t.Errorf("%q %q", l, k)
	}
	if l, k := runbookSourceBadge(book(func(r *keelclient.Runbook) { r.Source = srcAgent })); l != "Proposed" || k != "signal" {
		t.Errorf("%q %q", l, k)
	}
	match := func(what, got, re string) {
		t.Helper()
		if !regexp.MustCompile(re).MatchString(got) {
			t.Errorf("%s: %q does not match %s", what, got, re)
		}
	}
	match("user", runbookSourceLine(book(nil)), `^Written by you · Oct 1[12]$`)
	match("conversation", runbookSourceLine(book(func(r *keelclient.Runbook) { r.Source, r.SourceRef = "conversation", "nginx 502" })), `^From the conversation “nginx 502” · Oct`)
	match("agent", runbookSourceLine(book(func(r *keelclient.Runbook) { r.Source, r.FromPane = srcAgent, true })), `^Proposed by the agent after reading panes`)
	if runbookNeedsReview(book(nil)) {
		t.Error("a runbook you wrote needs a review")
	}
	if !runbookNeedsReview(book(func(r *keelclient.Runbook) { r.Source, r.AgentMayPick = srcAgent, false })) {
		t.Error("a runbook the agent proposed, off, needs no review")
	}
	if runbookNeedsReview(book(func(r *keelclient.Runbook) { r.Source = "notebook" })) {
		t.Error("a notebook runbook that is on needs a review")
	}
	match("review", runbookReviewText(book(func(r *keelclient.Runbook) { r.Source, r.FromPane = srcAgent, true })), `^The agent proposed it after reading terminal output\. .*Step 2 changes state\.$`)
}

func TestStepsLineAndKinds(t *testing.T) {
	eq := func(got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
	eq(runbookStepsLine(book(nil)), "3 steps · 1 changes state")
	eq(runbookStepsLine(book(func(r *keelclient.Runbook) {
		r.Steps = []keelclient.RunbookStep{{Title: "a", Command: "ls", Risk: "readonly"}}
	})), "1 step · all read-only")
	eq(runbookStepsLine(book(func(r *keelclient.Runbook) { r.Steps = []keelclient.RunbookStep{{Title: "a"}} })), "1 step · no commands")
	eq(runbookStepsLine(book(func(r *keelclient.Runbook) {
		r.Steps = []keelclient.RunbookStep{{Command: "a", Risk: "changes"}, {Command: "b", Risk: "destructive"}}
	})), "2 steps · 2 change state")
	l, _ := runbookStepKind("ls", "readonly")
	eq(l, "Read-only")
	l, _ = runbookStepKind("rm -rf /", "destructive")
	eq(l, "Destructive")
	l, _ = runbookStepKind("", "")
	eq(l, "Check")
}

func TestUsedLabel(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	at := func(d time.Time) func(*keelclient.Runbook) {
		return func(r *keelclient.Runbook) { r.LastUsedAt, r.LastUsedOn = &d, "" }
	}
	if got := runbookUsedLabel(book(nil), now); got != "Never used" {
		t.Error(got)
	}
	rb := book(at(time.Date(2026, 10, 18, 9, 15, 0, 0, time.Local)))
	rb.LastUsedOn = "prod-db-02"
	if got := runbookUsedLabel(rb, now); got != "Last used on prod-db-02 · today 09:15" {
		t.Error(got)
	}
	if got := runbookUsedLabel(book(at(time.Date(2026, 10, 15, 9, 15, 0, 0, time.Local))), now); got != "Last used · Oct 15, 09:15" {
		t.Error(got)
	}
}

func TestFilterByNameUseWhenHostsAndStepTitles(t *testing.T) {
	list := []keelclient.Runbook{book(nil), book(func(r *keelclient.Runbook) {
		r.ID, r.Name, r.When, r.Hosts, r.Steps = "rb_2", "nginx 502", "upstream errors", []string{"nginx"}, []keelclient.RunbookStep{{Title: "tail the log"}}
	})}
	ids := func(q string) string {
		var out []string
		for _, r := range filterRunbooks(list, q) {
			out = append(out, r.ID)
		}
		return strings.Join(out, ",")
	}
	for q, want := range map[string]string{"": "rb_1,rb_2", "nginx": "rb_2", "PATRONI behind": "rb_1", "tail": "rb_2"} {
		if got := ids(q); got != want {
			t.Errorf("%q: %s, want %s", q, got, want)
		}
	}
}

func TestOfferedForAPaneOnAHostInTheRunbooksFoldersOrTagsLikeKeelServe(t *testing.T) {
	rb := book(nil)
	if !runbookFitsPane(rb, &rbPane{group: "Postgres"}) || !runbookFitsPane(rb, &rbPane{tags: []string{"production"}}) {
		t.Error("a folder or a tag in any case fits")
	}
	if runbookFitsPane(rb, &rbPane{group: "web", tags: []string{"nginx"}}) {
		t.Error("another host fits")
	}
	if !runbookFitsPane(book(func(r *keelclient.Runbook) { r.Hosts = nil }), &rbPane{}) {
		t.Error("no hosts means any host")
	}
	ok, why := runbookOfferFor(rb, []rbPane{{group: "web", readable: true}})
	if ok || why != "Not offered: no production or postgres host in scope" {
		t.Errorf("%v %q", ok, why)
	}
	if ok, _ := runbookOfferFor(rb, []rbPane{{group: "postgres", readable: false}}); ok {
		t.Error("a pane the agent cannot read is offered")
	}
	if ok, _ := runbookOfferFor(rb, []rbPane{{group: "postgres", readable: true}}); !ok {
		t.Error("a postgres pane is not offered")
	}
	if ok, why := runbookOfferFor(rb, nil); ok || why != "Put a pane the agent may read in scope first" {
		t.Errorf("%v %q", ok, why)
	}
}

func TestTheEditorsInputCleanedLinesEmptyStepsDroppedChecks(t *testing.T) {
	input := toRunbookInput(rbEdit{
		name: "  Disk\nfull ", when: "runner\u2028above 85%", hosts: []string{"ci", " "}, agentMayPick: true,
		steps: []rbEditStep{{title: "See\tdocker", command: "docker system df\r\n"}, {}, {title: "Prune", description: "Keeps\n3 days", command: "docker builder prune"}},
	})
	if input.Name != "Disk full" || input.When != "runner above 85%" || !reflect.DeepEqual(input.Hosts, []string{"ci"}) || len(input.Steps) != 2 {
		t.Fatalf("%+v", input)
	}
	if input.Steps[0].Title != "See docker" || input.Steps[0].Command != "docker system df" || input.Steps[1].Description != "Keeps 3 days" {
		t.Errorf("%+v", input.Steps)
	}
	if cleanLine("a\u200bb") != "a b" {
		t.Error("cleanLine")
	}
	if got := checkRunbookInput(input); got != "" {
		t.Error(got)
	}
	bad := input
	bad.Name = ""
	if got := checkRunbookInput(bad); got != "Name the runbook." {
		t.Error(got)
	}
	bad = input
	bad.Steps = nil
	if got := checkRunbookInput(bad); got != "Add at least one step." {
		t.Error(got)
	}
	bad = input
	bad.Steps = []keelclient.RunbookStepInput{{Command: "ls"}}
	if got := checkRunbookInput(bad); got != "Step 1 needs a title." {
		t.Error(got)
	}
	if got := addHost([]string{"ci"}, "CI"); !reflect.DeepEqual(got, []string{"ci"}) {
		t.Error(got)
	}
	if got := addHost([]string{"ci"}, " nginx "); !reflect.DeepEqual(got, []string{"ci", "nginx"}) {
		t.Error(got)
	}
	if got := moveItem([]string{"a", "b", "c"}, 0, -1); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Error(got)
	}
	if got := moveItem([]string{"a", "b", "c"}, 0, 1); !reflect.DeepEqual(got, []string{"b", "a", "c"}) {
		t.Error(got)
	}
	// What the editor was given and does not edit goes back as it came.
	kept := toRunbookInput(rbEdit{name: "x", steps: []rbEditStep{{id: "s1", title: "t", role: " Web ", oneHost: true, captures: []keelclient.StepCapture{{Name: "n", Pattern: "p"}}}}})
	if s := kept.Steps[0]; s.ID != "s1" || s.Role != "web" || !s.OneHost || len(s.Captures) != 1 {
		t.Errorf("%+v", s)
	}
}

func run(over func(*keelclient.ChatRunbook), steps ...keelclient.RunStep) *keelclient.ChatRunbook {
	r := &keelclient.ChatRunbook{ID: "ru_1", RunbookID: "rb_1", Name: "Diagnose replication lag", PickedBy: srcAgent, Why: "matched the WAL timeouts", Status: "active"}
	for i, s := range steps {
		s.ID, s.Title = "st_"+string(rune('1'+i)), "Step "+string(rune('1'+i))
		if i != 3 {
			s.Command = "ls"
		}
		r.Steps = append(r.Steps, s)
	}
	if over != nil {
		over(r)
	}
	return r
}

func intp(n int) *int { return &n }

func TestProgressRowsDoneWithAReportedExitTheCurrentStepTheNextOnes(t *testing.T) {
	now := time.Now()
	r := run(nil, keelclient.RunStep{State: "done", Result: "reported", Exit: intp(0)}, keelclient.RunStep{State: "proposed", ProposalRef: "p_2"}, keelclient.RunStep{State: "next"}, keelclient.RunStep{State: "next"})
	if currentStep(r) != 1 {
		t.Error("current")
	}
	for i, want := range []runRow{{"done", "✓", "exit 0 · reported"}, {"now", "2", "proposed"}, {"next", "3", ""}} {
		if got := stepRow(r, i, now); got != want {
			t.Errorf("row %d: %+v, want %+v", i, got, want)
		}
	}
	f := run(nil, keelclient.RunStep{State: "failed", Result: "reported", Exit: intp(3)}, keelclient.RunStep{State: "next"})
	if got := stepRow(f, 0, now); got != (runRow{"failed", "✗", "exit 3 · reported"}) {
		t.Errorf("%+v", got)
	}
	typed := now
	m := run(nil, keelclient.RunStep{State: "done", Result: "user"}, keelclient.RunStep{State: "skipped", Result: "user"}, keelclient.RunStep{State: "typed", TypedAt: &typed, Result: "unreported"})
	if stepRow(m, 0, now).note != "marked done" || stepRow(m, 1, now).mark != "–" {
		t.Error("marked rows")
	}
	if !regexp.MustCompile(`^typed \d\d:\d\d · no exit status$`).MatchString(stepRow(m, 2, now).note) {
		t.Errorf("%q", stepRow(m, 2, now).note)
	}
}

func TestWhatTheRunCardOffersNextNeverALaterStepBeforeThisOne(t *testing.T) {
	live := func(id string) bool { return id == "p_2" }
	kind := func(r *keelclient.ChatRunbook) string { return nextAction(r, live).kind }
	st := func(s string) keelclient.RunStep { return keelclient.RunStep{State: s} }
	if kind(run(nil, st("done"), st("next"))) != "propose" || nextAction(run(nil, st("done"), st("next")), live).step.n != 2 {
		t.Error("propose step 2")
	}
	if kind(run(nil, st("done"), keelclient.RunStep{State: "proposed", ProposalRef: "p_2"})) != "proposed" {
		t.Error("proposed")
	}
	if kind(run(nil, st("done"), keelclient.RunStep{State: "proposed", ProposalRef: "p_old"})) != "propose" {
		t.Error("an old proposal is proposed again")
	}
	if kind(run(nil, st("typed"))) != "waiting" || kind(run(nil, keelclient.RunStep{State: "typed", Result: "abandoned"})) != "noexit" {
		t.Error("typed")
	}
	if kind(run(nil, keelclient.RunStep{State: "failed", Exit: intp(2)}, st("next"))) != "failed" {
		t.Error("failed")
	}
	if kind(run(nil, st("done"), st("done"), st("skipped"), st("next"))) != "check" {
		t.Error("check")
	}
	if kind(run(func(r *keelclient.ChatRunbook) { r.Status = "finished" }, st("done"))) != "finished" {
		t.Error("finished")
	}
	if kind(run(func(r *keelclient.ChatRunbook) { r.Status = "stopped" }, st("next"))) != "stopped" {
		t.Error("stopped")
	}
	if nextAction(nil, nil).kind != "none" {
		t.Error("none")
	}
	if !waitingForShell(run(nil, st("typed"))) || waitingForShell(run(nil, keelclient.RunStep{State: "typed", Result: "abandoned"})) ||
		waitingForShell(run(func(r *keelclient.ChatRunbook) { r.Status = "stopped" }, st("typed"))) {
		t.Error("waitingForShell")
	}
}

func TestWhyLineAndTheQuestionsTheCardAsks(t *testing.T) {
	eq := func(got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
	eq(runWhyLine(run(nil)), "Why: matched the WAL timeouts")
	eq(runWhyLine(run(func(r *keelclient.ChatRunbook) { r.PickedBy = "user" })), "You picked this runbook.")
	eq(continueQuestion(runStepRef{n: 2, of: 4, title: "Look for errors"}), "Continue with step 2 of 4: Look for errors")
	eq(startQuestion("nginx\n502"), "Walk me through the runbook “nginx 502”.")
}

func TestARunbookTheAgentProposesIsWarnedAboutAfterAnyPaneRead(t *testing.T) {
	if runbookProposalWarn(keelclient.RunbookProposal{}) || !runbookProposalWarn(keelclient.RunbookProposal{PanesRead: []string{"web-1"}}) || !runbookProposalWarn(keelclient.RunbookProposal{FromPane: true}) {
		t.Error("warn")
	}
	if !strings.Contains(runbookProposalWarning(keelclient.RunbookProposal{FromPane: true, SeenIn: "web-1"}), "matches pane web-1") {
		t.Error(runbookProposalWarning(keelclient.RunbookProposal{FromPane: true, SeenIn: "web-1"}))
	}
	if !strings.Contains(runbookProposalDone(keelclient.RunbookProposal{Status: "saved"}), "may not pick it until you review it") || runbookProposalDone(keelclient.RunbookProposal{Status: "dismissed"}) != "Dismissed. Nothing was saved." {
		t.Error("done")
	}
}

func TestReviewFixesPlaceholdersAreNeverTypedProductionExitZeroWaitsDraftsCarryTheirID(t *testing.T) {
	pane := &agentPane{sessionID: "s1", driving: true, p: &pane0, t: &tab0}
	if got := typeOffer(keelclient.Proposal{SessionID: "s1", Placeholders: true}, pane); got != offerPlaceholder {
		t.Errorf("offer %q", got)
	}
	if ok, why := typeGate(keelclient.Proposal{SessionID: "s1", Placeholders: true}, pane, true); ok || why != offerPlaceholder {
		t.Errorf("gate %v %q", ok, why)
	}
	r := run(nil, keelclient.RunStep{State: "typed", Result: "reported", Exit: intp(0)}, keelclient.RunStep{State: "next"})
	if nextAction(r, nil).kind != "confirm" || stepRow(r, 0, time.Now()).note != "exit 0 · reported — confirm" || waitingForShell(r) {
		t.Error("exit 0 waits for a confirm")
	}
	if n := stepRow(run(nil, keelclient.RunStep{State: "typed", Edited: true}), 0, time.Now()).note; !strings.HasSuffix(n, "· edited") {
		t.Error(n)
	}
	if got := toRunbookInput(rbEdit{name: "x", steps: []rbEditStep{{title: "t"}}, draftID: "dr_1"}).DraftID; got != "dr_1" {
		t.Error(got)
	}
}

var (
	pane0 pane
	tab0  tab
)
