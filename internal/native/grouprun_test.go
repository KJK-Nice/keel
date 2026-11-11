package native

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The group run on the fake backend (docs/ui-redesign.md §6 rules 10 and 11, §17): the run page, the group approve dialog and
// what its Type sends. A "pane" is a fakeConn that records each ApprovedInject; nothing here reaches a shell. Every command is
// a harmless echo and every value made up.

const rawBytes = "\x15echo grp-1\n" // what a proposal types: Ctrl-U, the command, a newline

// runRig is a window with n sessions this client drives, one per host wk-1…, and a group run of one step across them. The
// run is held by the fake keel serve; cells are what it has proposed.
func runRig(t *testing.T, n int) *rig {
	t.Helper()
	fb := okBackend()
	fb.groups = []keelclient.Group{{ID: "gx", Name: "gx"}}
	fb.hosts = nil
	for i := 1; i <= n; i++ {
		fb.hosts = append(fb.hosts, keelclient.Host{ID: fmt.Sprintf("h%d", i), Name: fmt.Sprintf("wk-%d", i), GroupID: "gx", Group: "gx", AuthType: "key", Address: "10.0.0." + fmt.Sprint(i), Username: "ops"})
	}
	r := newRig(t, fb)
	r.tt.SetSize(1280, 2000)
	r.tt.Frame()
	for _, h := range fb.hosts {
		r.w.openHost(h)
	}
	for _, c := range fb.conns {
		st := drivingState
		c.st = &st
	}
	r.tt.Frame()
	if len(fb.conns) != n {
		t.Fatalf("%d sessions, want %d", len(fb.conns), n)
	}
	return r
}

// runOf is a run with one step, a cell per host in state "proposed" with its own proposal (host i is on session s<i>).
func runOf(command, risk string, n int, mut func(i int, c *keelclient.GroupRunCell)) keelclient.GroupRun {
	st := keelclient.GroupRunStep{ID: "st1", Title: "Print a marker", Command: command, Risk: risk}
	run := keelclient.GroupRun{ID: "gr1", RunbookID: "rb1", Name: "Print markers", GroupID: "gx", Group: "gx", Mode: "parallel", Status: "active", Current: 0}
	for i := 1; i <= n; i++ {
		id := fmt.Sprint(i)
		c := keelclient.GroupRunCell{HostID: "h" + id, Host: "wk-" + id, State: "proposed", SessionID: "s" + id,
			Proposal: &keelclient.GroupProposal{ID: "p" + id, SessionID: "s" + id, HostID: "h" + id, Host: "wk-" + id, Command: command, Bytes: "\x15" + command + "\n", Risk: risk}}
		if mut != nil {
			mut(i, &c)
		}
		st.Cells = append(st.Cells, c)
		run.Members = append(run.Members, keelclient.GroupRunMember{HostID: "h" + id, Host: "wk-" + id})
	}
	run.Steps = []keelclient.GroupRunStep{st}
	return run
}

// startRun makes the fake hold the run and opens its page.
func (r *rig) startRun(run keelclient.GroupRun) {
	r.fb.grp.run = &run
	r.w.openGroupRun(run)
	r.tt.Frame()
}

// keel.css #page-grouprun container query: the steps (size-agentpage-context wide) are beside the detail; under 760 px of page
// width they are above it, across the width and at most 40% of the height.
func TestTheRunPagePutsTheStepsAboveTheDetailUnderSevenHundredSixtyPixels(t *testing.T) {
	r := runRig(t, 2)
	r.startRun(runOf("echo grp-1", "readonly", 2, nil))
	r.tt.Frame()
	r.tt.Frame()
	steps, ok1 := r.tt.Find("Steps")
	detail, ok2 := r.tt.Find("Group run")
	if !ok1 || !ok2 {
		t.Fatal("no steps or no detail")
	}
	if !(steps.X+steps.W <= detail.X+1) || steps.W < sizeAgentPageContext-1 || steps.W > sizeAgentPageContext+1 {
		t.Fatalf("wide: steps %v, detail %v: want the steps %v wide at the left", steps, detail, float32(sizeAgentPageContext))
	}
	r.tt.SetSize(900, 1000) // the page is what the sidebar leaves: 900 - 252 = 648
	r.tt.Frame()
	r.tt.Frame()
	r.tt.Frame()
	steps, _ = r.tt.Find("Steps")
	detail, _ = r.tt.Find("Group run")
	page, _ := r.tt.Find("group run page")
	if page.W >= groupRunNarrow || !(steps.Y+steps.H <= detail.Y+1) || steps.W < page.W-2 || steps.H > page.H*0.4+1 {
		t.Fatalf("narrow: page %v steps %v detail %v", page, steps, detail)
	}
	r.tt.SetSize(1280, 2000)
	r.tt.Frame()
	r.tt.Frame()
	r.tt.Frame()
	steps, _ = r.tt.Find("Steps")
	detail, _ = r.tt.Find("Group run")
	if !(steps.X+steps.W <= detail.X+1) {
		t.Errorf("wide again: steps %v, detail %v", steps, detail)
	}
}

// pressTypeAll presses "Type into N panes…" on the run page.
func (r *rig) pressTypeAll(t *testing.T, n int) {
	t.Helper()
	label := fmt.Sprintf("Type into %d panes…", n)
	if n == 1 {
		label = "Type into 1 pane…"
	}
	r.click(t, label)
}

func (r *rig) injected() (out [][]string) {
	for _, c := range r.fb.conns {
		out = append(out, c.injected)
	}
	return out
}

func (r *rig) nothingTyped(t *testing.T) {
	t.Helper()
	for i, c := range r.fb.conns {
		if len(c.injected) != 0 || c.wrote != 0 || len(c.directs) != 0 {
			t.Errorf("pane %d was typed into: %q, wrote %d, directs %d", i+1, c.injected, c.wrote, len(c.directs))
		}
	}
}

func (r *rig) groupDlg(t *testing.T) *approveGroup {
	t.Helper()
	if r.w.approve == nil || r.w.approve.group == nil {
		t.Fatal("the group approve dialog is not open")
	}
	return r.w.approve.group
}

// ---- rule 10 ----

func TestGroupDialogNoRowIsTickedInAdvanceWithTwoOrMoreHostsAndNothingIsTypedBeforeType(t *testing.T) {
	r := runRig(t, 3)
	r.startRun(runOf("echo grp-1", "readonly", 3, nil))
	if miss := r.lacks("Step 1 · Print a marker", "Run on", "every host", "Read-only", "1 line · 12 bytes · same bytes in every pane", "Type into 3 panes…"); len(miss) > 0 {
		t.Fatalf("the run page lacks %q: %v", miss, r.tt.Texts())
	}
	r.pressTypeAll(t, 3)
	g := r.groupDlg(t)
	if len(g.rows) != 3 || ticked(g.rows) != 0 {
		t.Fatalf("%d rows, %d ticked", len(g.rows), ticked(g.rows))
	}
	for _, s := range []string{groupApproveTitle, groupApproveHint, "1 line · 12 bytes · the same bytes in every pane", "Tick the hosts to type into", "wk-1", "wk-2", "wk-3"} {
		if !r.hasText(s) {
			t.Errorf("the dialog lacks %q: %v", s, r.tt.Texts())
		}
	}
	if !r.tt.Focused("Cancel") {
		t.Error("the keyboard does not start on Cancel")
	}
	// Opening it, polling and pressing Type with nothing ticked type nothing, and ask keel serve for no fill (no variables).
	r.click(t, "Type")
	r.w.approveType(false) // even called directly, with nothing ticked
	r.w.approveType(true)
	r.nothingTyped(t)
	if slices.ContainsFunc(r.fb.grp.runReqs, func(s string) bool { return strings.HasPrefix(s, "fill") }) {
		t.Errorf("a fill without variables: %v", r.fb.grp.runReqs)
	}
	if !r.tt.HasText("Tick the hosts to type into") {
		t.Error("the button does not say to tick the hosts")
	}
}

func TestGroupDialogOneHostIsTheOrdinaryApprovalAndItsRowIsTicked(t *testing.T) {
	r := runRig(t, 1)
	r.startRun(runOf("echo grp-1", "readonly", 1, nil))
	r.pressTypeAll(t, 1)
	g := r.groupDlg(t)
	if len(g.rows) != 1 || !g.rows[0].checked || !r.hasText("Type into 1 pane") {
		t.Fatalf("rows %+v", g.rows)
	}
	r.nothingTyped(t) // ticked is not typed: Type is the user's
	r.click(t, "Type into 1 pane")
	if got := r.injected(); !slices.Equal(got[0], []string{rawBytes}) {
		t.Fatalf("typed %q", got)
	}
}

func TestGroupDialogOnlyTickedRowsAreTypedEachWithItsOwnProposalAndConnection(t *testing.T) {
	r := runRig(t, 3)
	r.startRun(runOf("echo grp-1", "readonly", 3, nil))
	r.pressTypeAll(t, 3)
	r.click(t, "Type into wk-2")
	r.click(t, "Type into wk-3")
	if !r.hasText("Type into 2 panes") {
		t.Fatalf("the button: %v", r.tt.Texts())
	}
	r.click(t, "Type into 2 panes")
	got := r.injected()
	if len(got[0]) != 0 {
		t.Errorf("the unticked wk-1 was typed into: %q", got[0])
	}
	if !slices.Equal(got[1], []string{rawBytes}) || !slices.Equal(got[2], []string{rawBytes}) {
		t.Fatalf("typed %q", got)
	}
	// Each ticked host is its own proposal on its own connection: its own id, the run, and no conversation.
	for i, want := range map[int]string{1: "p2", 2: "p3"} {
		ref := r.fb.conns[i].agentRefs[0]
		if ref == nil || ref.GroupRunID != "gr1" || ref.ProposalID != want || ref.ConversationID != "" || ref.Shift {
			t.Errorf("pane %d typed with %+v, want proposal %s of run gr1", i+1, ref, want)
		}
	}
	if len(r.fb.conns[0].agentRefs) != 0 {
		t.Error("an agent reference went to the unticked pane")
	}
	if r.w.approve != nil {
		t.Error("the dialog stays after every ticked host typed")
	}
	// The run is read again afterwards.
	if !slices.Contains(r.fb.grp.runReqs, "get") {
		t.Errorf("requests %v", r.fb.grp.runReqs)
	}
}

func TestGroupDialogAHostThatNeedsShiftIsTypedOnlyWithShift(t *testing.T) {
	r := runRig(t, 2)
	r.startRun(runOf("sudo systemctl restart kubelet", "changes", 2, func(i int, c *keelclient.GroupRunCell) {
		if i == 2 {
			c.Proposal.Production, c.Proposal.NeedsShift = true, true
		}
	}))
	if !r.hasText("1 of these host is production. Typing there needs ⇧.") && !r.hasText("1 of these host is production.") {
		t.Errorf("no production note: %v", r.tt.Texts())
	}
	r.w.tabs[1].title = "ops" // wk-2's tab, named by the user: the row says which tab the pane is in
	r.tt.Frame()
	r.pressTypeAll(t, 2)
	for _, s := range []string{"tab ops", "Production", "⇧"} {
		if !r.hasText(s) {
			t.Errorf("the rows lack %q (the pane's tab, the Production and ⇧ badges): %v", s, r.tt.Texts())
		}
	}
	r.click(t, "Type into wk-1")
	if r.hasText(groupShiftNeeded) {
		t.Error("an unticked production host asks for ⇧")
	}
	r.click(t, "Type into wk-2")
	if !r.hasText(groupShiftNeeded) || !r.hasText("Type into 2 panes ⇧↵") {
		t.Fatalf("the dialog does not ask for ⇧: %v", r.tt.Texts())
	}
	// A plain press types nothing at all, not even the host that needs no ⇧.
	r.click(t, "Type")
	r.w.approveType(false)
	r.nothingTyped(t)
	if !r.groupDlg(t).shiftHint || !r.hasText(groupShiftRefused) {
		t.Error("the refusal is not said")
	}
	// ⇧ held with Type types both, and keel serve is told ⇧ was held.
	if err := r.tt.ClickWith(ui.Shift, "Type"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	got := r.injected()
	if len(got[0]) != 1 || len(got[1]) != 1 || !r.fb.conns[0].agentRefs[0].Shift || !r.fb.conns[1].agentRefs[0].Shift {
		t.Fatalf("typed %q, refs %+v %+v", got, r.fb.conns[0].agentRefs, r.fb.conns[1].agentRefs)
	}
}

func TestGroupFollowersAndClosedPanesCannotBeTicked(t *testing.T) {
	r := runRig(t, 4)
	r.startRun(runOf("echo grp-1", "readonly", 4, nil))
	// wk-2's pane is followed, wk-3's is closed: the window offers no row for them.
	r.fb.conns[1].st = &keelclient.State{Status: "ready", Role: "follower"}
	r.w.closePane(r.w.tabs[2], r.w.tabs[2].focused)
	r.tt.Frame()
	r.pressTypeAll(t, 2)
	g := r.groupDlg(t)
	var hosts []string
	for _, row := range g.rows {
		hosts = append(hosts, row.host)
	}
	if !slices.Equal(hosts, []string{"wk-1", "wk-4"}) {
		t.Fatalf("rows for %v, want wk-1 and wk-4 only", hosts)
	}
	// A pane that turns into a followed one, or closes, while the dialog is open: its row cannot be ticked and is never typed.
	r.click(t, "Type into wk-1")
	r.click(t, "Type into wk-4")
	r.fb.conns[0].st = &keelclient.State{Status: "ready", Role: "follower"}
	r.tt.Frame()
	if !r.hasText(msgGroupPaneFollow) {
		t.Errorf("the row does not say it is followed: %v", r.tt.Texts())
	}
	r.click(t, "Type into 1 pane") // wk-4 only: wk-1's tick no longer counts
	got := r.injected()
	if len(got[0]) != 0 || len(got[3]) != 1 || len(got[1]) != 0 || len(got[2]) != 0 {
		t.Fatalf("typed %q", got)
	}
}

func TestGroupDialogAClosedPaneWhileOpenIsNeverTypedAndTheOthersType(t *testing.T) {
	r := runRig(t, 2)
	r.startRun(runOf("echo grp-1", "readonly", 2, nil))
	r.pressTypeAll(t, 2)
	r.click(t, "Type into wk-1")
	r.click(t, "Type into wk-2")
	r.w.closePane(r.w.tabs[0], r.w.tabs[0].focused)
	r.tt.Frame()
	if !r.hasText(msgGroupPaneGone) {
		t.Errorf("the row does not say its pane is gone: %v", r.tt.Texts())
	}
	r.click(t, "Type into 1 pane")
	if got := r.injected(); len(got[0]) != 0 || len(got[1]) != 1 {
		t.Fatalf("typed %q", got)
	}
}

func TestGroupDialogARefusalForOneHostIsShownOnItsRowAndTheOthersStillTypeNothingIsRetried(t *testing.T) {
	r := runRig(t, 3)
	r.startRun(runOf("echo grp-1", "readonly", 3, nil))
	r.fb.conns[1].injectErr = errors.New("Not typed: wk-2 left the group")
	r.pressTypeAll(t, 3)
	for _, h := range []string{"wk-1", "wk-2", "wk-3"} {
		r.click(t, "Type into "+h)
	}
	r.click(t, "Type into 3 panes")
	got := r.injected()
	if len(got[0]) != 1 || len(got[1]) != 1 || len(got[2]) != 1 {
		t.Fatalf("every ticked host is asked once, whatever another answers: %q", got)
	}
	g := r.groupDlg(t) // a refusal keeps the dialog: each row shows its own outcome
	if !g.rows[0].typedOK || g.rows[1].typedOK || !g.rows[2].typedOK || g.rows[1].outcome != "Not typed: wk-2 left the group" {
		t.Fatalf("rows %+v %+v %+v", g.rows[0], g.rows[1], g.rows[2])
	}
	if !r.hasText("Not typed: wk-2 left the group") || !r.hasText("Typed.") {
		t.Errorf("the rows do not say what happened: %v", r.tt.Texts())
	}
	if _, ok := r.tt.Find("toast Not typed on wk-2: Not typed: wk-2 left the group"); !ok {
		t.Errorf("no failure toast: %v", r.tt.Texts())
	}
	// Nothing is retried: a reached row is final, a second Type types nothing, and its tick is off.
	r.w.approveType(false)
	r.w.approveType(true)
	r.click(t, "Close")
	got = r.injected()
	if len(got[0]) != 1 || len(got[1]) != 1 || len(got[2]) != 1 {
		t.Fatalf("a retry typed again: %q", got)
	}
	if r.w.approve != nil {
		t.Error("Close leaves the dialog")
	}
}

func TestGroupDialogCancelAndEscapeTypeNothing(t *testing.T) {
	r := runRig(t, 2)
	r.startRun(runOf("echo grp-1", "readonly", 2, nil))
	r.pressTypeAll(t, 2)
	r.click(t, "Type into wk-1")
	r.click(t, "Cancel")
	r.nothingTyped(t)
	if r.w.approve != nil || r.w.groupRunPageState().typing {
		t.Error("Cancel leaves the dialog open or the page typing")
	}
	r.pressTypeAll(t, 2)
	r.press(0, ui.KeyEscape)
	r.nothingTyped(t)
	if r.w.approve != nil {
		t.Error("Esc leaves the dialog")
	}
}

func TestAnUnreadyOrFollowedConnectionTypesNothingEvenIfAskedDirectly(t *testing.T) {
	r := runRig(t, 1)
	r.startRun(runOf("echo grp-1", "readonly", 1, nil))
	r.pressTypeAll(t, 1)
	r.fb.conns[0].st = &keelclient.State{Status: "connecting", Role: "driver"}
	r.w.approveType(false)
	r.nothingTyped(t)
}

// ---- rule 11 ----

func varsRun(n int) keelclient.GroupRun {
	run := runOf("echo port-{{port}}", "readonly", n, nil)
	for i := range run.Steps[0].Cells {
		run.Steps[0].Cells[i].Proposal.Vars = []keelclient.VarRef{{Name: "port"}}
	}
	run.Steps[0].Vars = []keelclient.VarRef{{Name: "port"}}
	return run
}

func TestGroupVariablesAreNamesOnTheRunPageValuesOnlyInTheDialogAndTypedBytesAreTheFill(t *testing.T) {
	r := runRig(t, 3)
	r.startRun(varsRun(3))
	r.fb.grp.fills = map[string]keelclient.GroupFill{
		"p1": {HostID: "h1", Host: "wk-1", Bytes: "\x15echo port-8080\n", Used: []keelclient.FillUsed{{Name: "port", From: "group variable"}}},
		"p2": {HostID: "h2", Host: "wk-2", Bytes: "\x15echo port-9090\n", Used: []keelclient.FillUsed{{Name: "port", From: "captured on cp-1 · step 1"}}},
		"p3": {HostID: "h3", Host: "wk-3", Error: "{{port}} has no value for wk-3: no earlier step of this run captured it"},
	}
	// The run page shows the name, the note and Copy — never a value, and no fill has been asked for.
	if miss := r.lacks("echo port-{{port}}", "Uses 1 group variable · filled in at Type", fillNote, "Copy"); len(miss) > 0 {
		t.Fatalf("the run page lacks %q: %v", miss, r.tt.Texts())
	}
	if slices.ContainsFunc(r.fb.grp.runReqs, func(s string) bool { return strings.HasPrefix(s, "fill") }) {
		t.Fatalf("a fill before Type into N panes: %v", r.fb.grp.runReqs)
	}
	r.click(t, "Copy the command without values")
	if got := r.tt.Clipboard(); got != "echo port-{{port}}" {
		t.Errorf("Copy put %q on the clipboard: the saved command with the names in", got)
	}
	r.pressTypeAll(t, 3)
	if want := "fill p1,p2,p3"; !slices.Contains(r.fb.grp.runReqs, want) {
		t.Fatalf("requests %v, want %q", r.fb.grp.runReqs, want)
	}
	// The dialog shows the exact filled bytes per host, where they came from, and the host whose fill failed cannot be ticked.
	for _, s := range []string{groupValuesDiffer, "echo port-8080", "echo port-9090", "{{port}} group variable", "{{port}} captured on cp-1 · step 1", "{{port}} has no value for wk-3: no earlier step of this run captured it"} {
		if !r.hasText(s) {
			t.Errorf("the dialog lacks %q: %v", s, r.tt.Texts())
		}
	}
	r.nothingTyped(t)
	r.click(t, "Type into wk-1")
	r.click(t, "Type into wk-2")
	r.click(t, "Type into wk-3") // refused: a failed fill cannot be ticked
	g := r.groupDlg(t)
	if g.rows[2].checked || ticked(g.rows) != 2 {
		t.Fatalf("the failed row was ticked: %+v", g.rows[2])
	}
	r.click(t, "Type into 2 panes")
	// The bytes sent are exactly what the fill returned for that proposal, byte for byte; the third pane got nothing.
	got := r.injected()
	if !slices.Equal(got[0], []string{"\x15echo port-8080\n"}) || !slices.Equal(got[1], []string{"\x15echo port-9090\n"}) || len(got[2]) != 0 {
		t.Fatalf("typed %q", got)
	}
	// After the Type the run page, the toasts and the status show names, not values.
	all := strings.Join(r.tt.Texts(), "\n")
	for _, v := range []string{"8080", "9090"} {
		if strings.Contains(all, v) {
			t.Errorf("the value %s is on screen after the dialog closed", v)
		}
	}
	if !strings.Contains(all, "{{port}}") {
		t.Error("the name is not on the run page")
	}
}

func TestGroupDialogSameFilledBytesAreShownOnceWithWhereTheyCameFrom(t *testing.T) {
	r := runRig(t, 2)
	r.startRun(varsRun(2))
	used := []keelclient.FillUsed{{Name: "port", From: "group variable"}}
	r.fb.grp.fills = map[string]keelclient.GroupFill{
		"p1": {Bytes: "\x15echo port-8080\n", Used: used},
		"p2": {Bytes: "\x15echo port-8080\n", Used: used},
	}
	r.pressTypeAll(t, 2)
	if !r.hasText("1 line · 16 bytes · the same bytes in every pane · filled in by Keel: {{port}} group variable") {
		t.Errorf("no single description: %v", r.tt.Texts())
	}
}

func TestGroupDialogAStepWithoutVariablesTypesTheProposalsOwnBytes(t *testing.T) {
	r := runRig(t, 2)
	run := runOf("echo grp-1", "readonly", 2, func(i int, c *keelclient.GroupRunCell) {
		c.Proposal.Bytes = "\x15echo grp-1\n"
	})
	r.startRun(run)
	r.pressTypeAll(t, 2)
	r.click(t, "Type into wk-1")
	r.click(t, "Type into 1 pane")
	if got := r.injected(); !slices.Equal(got[0], []string{rawBytes}) {
		t.Fatalf("typed %q", got)
	}
	if len(r.fb.grp.runReqs) > 0 && slices.ContainsFunc(r.fb.grp.runReqs, func(s string) bool { return strings.HasPrefix(s, "fill") }) {
		t.Error("a fill for a step that names no variable")
	}
}

// ---- the run page ----

func TestThePageProposesForTheDrivenPanesOnlyAndNeverWhileTheDialogIsOpenOrTheRunIsOver(t *testing.T) {
	r := runRig(t, 3)
	r.startRun(runOf("echo grp-1", "readonly", 3, nil))
	r.fb.conns[1].st = &keelclient.State{Status: "ready", Role: "follower"}
	r.fb.grp.runReqs = nil
	r.w.groupRunTick()
	if want := []string{"propose s1,s3"}; !slices.Equal(r.fb.grp.runReqs, want) {
		t.Fatalf("requests %v, want %v: the followed pane must not be offered", r.fb.grp.runReqs, want)
	}
	r.nothingTyped(t)
	// Not while the group approve dialog is open, nor a fill or a Type is under way.
	r.fb.grp.runReqs = nil
	r.tt.Frame()
	r.pressTypeAll(t, 2)
	r.w.groupRunTick()
	r.click(t, "Cancel")
	r.w.groupRunPageState().typing = true
	r.w.groupRunTick()
	r.w.groupRunPageState().typing = false
	if len(r.fb.grp.runReqs) != 0 {
		t.Errorf("a propose while the dialog was open: %v", r.fb.grp.runReqs)
	}
	// Not when the run has ended.
	r.fb.grp.run.Status = "stopped"
	r.w.loadGroupRunPage()
	r.fb.grp.runReqs = nil
	r.w.groupRunTick()
	if slices.Contains(r.fb.grp.runReqs, "propose s1,s3") {
		t.Errorf("a propose for a stopped run: %v", r.fb.grp.runReqs)
	}
}

func TestRollingOffersOneHostAtATimeAsTheServerProposesAndTheDialogIsTheOneHostCase(t *testing.T) {
	r := runRig(t, 3)
	run := runOf("echo grp-1", "readonly", 3, func(i int, c *keelclient.GroupRunCell) {
		if i > 1 { // rolling: only the first host has a proposal
			c.State, c.Proposal = "waiting", nil
		}
	})
	run.Mode = "rolling"
	r.startRun(run)
	r.fb.grp.onPropose = func(run *keelclient.GroupRun, panes []string) {
		// keel serve proposes the next host only once the previous one is done.
		cells := run.Steps[0].Cells
		for i := range cells {
			if cells[i].State == "done" && i+1 < len(cells) && cells[i+1].State == "waiting" {
				id := fmt.Sprint(i + 2)
				cells[i+1].State = "proposed"
				cells[i+1].Proposal = &keelclient.GroupProposal{ID: "p" + id, SessionID: "s" + id, HostID: "h" + id, Host: "wk-" + id, Bytes: rawBytes}
			}
		}
	}
	if !r.hasText("Type into 1 pane…") || r.hasText("Type into 2 panes…") {
		t.Fatalf("rolling must offer one host: %v", r.tt.Texts())
	}
	r.pressTypeAll(t, 1)
	if g := r.groupDlg(t); len(g.rows) != 1 || !g.rows[0].checked || g.rows[0].host != "wk-1" {
		t.Fatalf("rows %+v", g.rows)
	}
	r.click(t, "Type into 1 pane")
	if got := r.injected(); len(got[0]) != 1 || len(got[1]) != 0 || len(got[2]) != 0 {
		t.Fatalf("typed %q", got)
	}
	// keel serve marks wk-1 typed; wk-2 is offered only after wk-1 is done.
	r.fb.grp.run.Steps[0].Cells[0].State = "typed"
	r.w.groupRunTick()
	r.tt.Frame()
	if cells := typeable(r.w.groupRunPageState().run, r.w.groupPaneOpen); len(cells) != 0 || r.hasText("Type into 1 pane…") {
		t.Fatalf("the next host is offered before the previous one is done: %+v", cells)
	}
	r.fb.grp.run.Steps[0].Cells[0].State = "done"
	r.w.groupRunTick()
	r.tt.Frame()
	if cells := typeable(r.w.groupRunPageState().run, r.w.groupPaneOpen); len(cells) != 1 || cells[0].HostID != "h2" {
		t.Fatalf("after wk-1 is done: %+v", cells)
	}
}

func TestClosingAPaneKeepsTheRunAndOffersNothingForIt(t *testing.T) {
	r := runRig(t, 2)
	r.startRun(runOf("echo grp-1", "readonly", 2, nil))
	r.w.closePane(r.w.tabs[1], r.w.tabs[1].focused)
	r.tt.Frame()
	if r.w.groupRunPageState().run == nil || r.w.groupRunPageState().run.Status != "active" {
		t.Fatal("closing a pane ended the run")
	}
	if !r.hasText("Type into 1 pane…") {
		t.Errorf("the closed pane is still offered: %v", r.tt.Texts())
	}
	r.w.groupRunTick()
	if last := r.fb.grp.runReqs[len(r.fb.grp.runReqs)-1]; last != "propose s1" {
		t.Errorf("propose %q: the closed pane's session must not be named", last)
	}
	r.nothingTyped(t)
}

func TestHostsWithNoPaneHaveAnOpenButtonThatOpensASessionAndTypesNothing(t *testing.T) {
	r := runRig(t, 2)
	run := runOf("echo grp-1", "readonly", 2, nil)
	run.NeedPane = []keelclient.GroupRunHost{{HostID: "h2", Host: "wk-2"}}
	r.startRun(run)
	if !r.hasText("wk-2 has no pane open here.") {
		t.Fatalf("no note: %v", r.tt.Texts())
	}
	n := len(r.fb.spawned)
	r.click(t, "Open a pane on wk-2")
	if len(r.fb.spawned) != n+1 || r.fb.spawned[n] != "h2" {
		t.Fatalf("spawned %v", r.fb.spawned)
	}
	r.nothingTyped(t)
}

func TestTheModeSwitchIsOffWhileLockedAndSendsTheMode(t *testing.T) {
	r := runRig(t, 2)
	run := runOf("echo grp-1", "readonly", 2, nil)
	run.Mode = "rolling"
	r.startRun(run)
	r.click(t, "Parallel")
	if !slices.Contains(r.fb.grp.runReqs, "mode parallel") || r.w.groupRunPageState().run.Mode != "parallel" {
		t.Fatalf("requests %v", r.fb.grp.runReqs)
	}
	r.fb.grp.runReqs = nil
	r.fb.grp.run.ModeLocked = true
	r.w.loadGroupRunPage()
	r.tt.Frame()
	if !r.hasText(modeLockedText) {
		t.Errorf("no reason: %v", r.tt.Texts())
	}
	_ = r.tt.Click("Rolling")
	r.tt.Frame()
	if slices.Contains(r.fb.grp.runReqs, "mode rolling") {
		t.Errorf("the mode changed while locked: %v", r.fb.grp.runReqs)
	}
}

func TestHostAndStepActionsSendTheAppsRequestsAndRefusalsAreShown(t *testing.T) {
	r := runRig(t, 3)
	run := runOf("echo grp-1", "readonly", 3, func(i int, c *keelclient.GroupRunCell) {
		switch i {
		case 1:
			c.State, c.Exit, c.Result = "failed", ip(1), "reported"
		case 2:
			c.State = "typed"
		case 3:
			c.State, c.Confirm, c.Exit = "typed", true, ip(0)
		}
	})
	run.Halted = "wk-1"
	r.startRun(run)
	for _, s := range []string{"wk-1 failed with exit 1. Rolling stopped before wk-2.", "wk-2 and wk-3 were not touched."} {
		if !r.hasText(s) {
			t.Errorf("no %q: %v", s, r.tt.Texts())
		}
	}
	for _, c := range []struct{ button, want string }{
		{"Retry host", "mark st1 h1 retry"}, {"Skip host", "mark st1 h1 skip"},
		{"Mark done wk-2", "mark st1 h2 done"}, {"Retry wk-2", "mark st1 h2 retry"},
		{"Confirm wk-3", "mark st1 h3 confirm"}, {"Skip wk-1", "mark st1 h1 skip"}, {"Mark done wk-1", "mark st1 h1 done"},
	} {
		r.fb.grp.runReqs = nil
		r.click(t, c.button)
		if !slices.Equal(r.fb.grp.runReqs, []string{c.want}) {
			t.Errorf("%s sent %v, want %q", c.button, r.fb.grp.runReqs, c.want)
		}
	}
	// keel serve's 409 is shown in its words.
	r.fb.grp.runErr = &keelclient.StatusError{Code: 409, Msg: "wk-2 is still reporting"}
	r.click(t, "Mark done wk-2")
	if !r.hasText("wk-2 is still reporting") {
		t.Errorf("no refusal: %v", r.tt.Texts())
	}
	r.nothingTyped(t)
}

func TestTheMatrixSaysWhatTheShellReportedAndWhatIsLeftOut(t *testing.T) {
	r := runRig(t, 2)
	run := runOf("echo grp-1", "readonly", 2, func(i int, c *keelclient.GroupRunCell) {
		if i == 1 {
			c.State, c.Exit, c.Result = "done", ip(0), "reported"
		}
	})
	run.Members = append(run.Members, keelclient.GroupRunMember{HostID: "hoff", Host: "wx-off", Excluded: "Agent can read is off"})
	r.startRun(run)
	for _, s := range []string{"Results", "Step 1: 1 of 2 done", "✓ exit 0", "· waiting for you", "Left out: wx-off (Agent can read is off). The run never types into them.", runReportedLine} {
		if !r.hasText(s) {
			t.Errorf("no %q: %v", s, r.tt.Texts())
		}
	}
}

func TestStopRunbookAsksFirstWithTheAppsWords(t *testing.T) {
	r := runRig(t, 2)
	r.startRun(runOf("echo grp-1", "readonly", 2, nil))
	r.click(t, "Stop runbook")
	if !r.hasText(stopTitle) || !r.hasText(stopRunMessage) || slices.Contains(r.fb.grp.runReqs, "stop") {
		t.Fatalf("no confirm, or a stop before it: %v %v", r.tt.Texts(), r.fb.grp.runReqs)
	}
	r.click(t, "Cancel")
	r.tt.Frame() // the dialog goes on the frame after
	if slices.Contains(r.fb.grp.runReqs, "stop") {
		t.Fatal("Cancel stopped the run")
	}
	r.click(t, "Stop runbook")
	// The confirm's button has the page button's name, so the test presses it as the dialog's Remove does.
	cf := r.w.confirm
	if cf == nil || cf.action != stopAction || cf.title != stopTitle {
		t.Fatalf("confirm %+v", cf)
	}
	r.w.endConfirm(true)
	cf.run()
	r.tt.Frame()
	if !slices.Contains(r.fb.grp.runReqs, "stop") || !r.hasText("Runbook stopped at step 1.") {
		t.Fatalf("requests %v, texts %v", r.fb.grp.runReqs, r.tt.Texts())
	}
	// A stopped run offers nothing to type.
	if r.hasText("Type into 2 panes…") {
		t.Error("a stopped run still offers Type")
	}
	r.nothingTyped(t)
}

func TestACapturedValueIsAMaskAndALengthNeverTheValue(t *testing.T) {
	r := runRig(t, 2)
	run := runOf("kubeadm token create", "changes", 2, func(i int, c *keelclient.GroupRunCell) {
		c.State = "done"
		c.Captures = []keelclient.CaptureView{{Name: "join_token", State: "captured", Bytes: 23}}
		if i == 2 {
			c.Captures[0].State = "blocked"
		}
	})
	run.Steps[0].Captures = []keelclient.StepCapture{{Name: "join_token", Pattern: `--token\s+(\S+)`}}
	r.startRun(run)
	for _, s := range []string{"Captured from step 1 · Print a marker", "Captured by Keel, never sent to the agent", "{{join_token}}", `--token\s+(\S+)`, "••••••••••••", "23 bytes", "not kept: it holds a value from the vault"} {
		if !r.hasText(s) {
			t.Errorf("no %q: %v", s, r.tt.Texts())
		}
	}
}

func TestTheRunPageComesBackWithTheWorkspaceAndSaysWhenKeelServeLostTheRun(t *testing.T) {
	r := runRig(t, 1)
	r.startRun(runOf("echo grp-1", "readonly", 1, nil))
	d := r.w.snapshot()
	if d.RunID != "gr1" || !slices.Contains(d.Pages, "grouprun") {
		t.Fatalf("snapshot %+v", d)
	}
	b2 := okBackend()
	b2.grp.run = r.fb.grp.run
	w2 := newRig(t, b2)
	w2.w.restore(&d, nil)
	w2.tt.Frame()
	if p := w2.w.groupRunPageState(); p == nil || p.run == nil || p.id != "gr1" || !w2.hasText("Step 1 · Print a marker") {
		t.Fatalf("the run page did not come back: %+v", p)
	}
	b3 := okBackend() // a keel serve restart: the run is gone
	w3 := newRig(t, b3)
	w3.w.restore(&d, nil)
	w3.tt.Frame()
	if !w3.hasText(msgRunEnded) {
		t.Errorf("no word on the lost run: %v", w3.tt.Texts())
	}
}

func TestStartingARunFromTheGroupPageOpensItsPageRollingAndTypesNothing(t *testing.T) {
	r := runRig(t, 2)
	r.fb.mem.runbooks = []keelclient.Runbook{{ID: "rb1", Name: "Print markers", Steps: []keelclient.RunbookStep{{ID: "s1", Title: "Print", Command: "echo grp-1"}}}}
	r.fb.grp.startRun = runOf("echo grp-1", "readonly", 2, nil)
	r.w.folders = r.fb.groups
	r.w.openGroupPage("gx", true)
	r.tt.Frame()
	r.click(t, "run runbook Print markers")
	if want := []string{"start rb1 gx rolling"}; !slices.Equal(r.fb.grp.runReqs[:1], want) {
		t.Fatalf("requests %v, want %v", r.fb.grp.runReqs, want)
	}
	if p := r.w.groupRunPageState(); p == nil || r.w.tab() != r.w.pageTab("grouprun") || r.w.tab().name() != "Run · Print markers" {
		t.Fatalf("the run page is not shown: %+v", p)
	}
	r.nothingTyped(t)
}
