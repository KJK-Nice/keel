package native

import (
	"strings"
	"testing"

	"keel/internal/native/keelclient"
)

// The rules of a group run, plus fillRows / approveGroups / sameSources.

func tcell(hostID, state string, mut func(*keelclient.GroupRunCell)) keelclient.GroupRunCell {
	c := keelclient.GroupRunCell{HostID: hostID, Host: strings.Replace(hostID, "h", "wk-", 1), State: state}
	if mut != nil {
		mut(&c)
	}
	return c
}

func tprop(id string, mut func(*keelclient.GroupProposal)) *keelclient.GroupProposal {
	p := &keelclient.GroupProposal{ID: "p_" + id, SessionID: "s_" + id, Bytes: "\x15sudo systemctl restart kubelet\n"}
	if mut != nil {
		mut(p)
	}
	return p
}

func ip(n int) *int { return &n }

func TestWhereAStepRuns(t *testing.T) { // "where a step runs"
	for _, c := range []struct {
		step keelclient.GroupRunStep
		want string
	}{
		{keelclient.GroupRunStep{Command: "x", Role: "worker", Cells: make([]keelclient.GroupRunCell, 2)}, "Role worker · 2 hosts"},
		{keelclient.GroupRunStep{Command: "x", Role: "control-plane", OneHost: true, Cells: make([]keelclient.GroupRunCell, 1)}, "One control-plane host"},
		{keelclient.GroupRunStep{Command: "x"}, "No host the agent may read"},
		{keelclient.GroupRunStep{}, "No command · you check it"},
		{keelclient.GroupRunStep{Command: "x", Cells: make([]keelclient.GroupRunCell, 1)}, "Every host · 1 host"},
		{keelclient.GroupRunStep{Command: "x", Role: "db"}, "Role db · no host has it"},
		{keelclient.GroupRunStep{Command: "x", OneHost: true}, "One host in the group"},
	} {
		if got := targetLabel(c.step); got != c.want {
			t.Errorf("targetLabel = %q, want %q", got, c.want)
		}
	}
}

func TestACellsWordsNeverSayMoreThanTheShellReported(t *testing.T) { // "a cell's words never say more than the shell reported"
	for _, c := range []struct {
		cell keelclient.GroupRunCell
		want string
	}{
		{tcell("h1", "done", func(c *keelclient.GroupRunCell) { c.Exit, c.Result = ip(0), "reported" }), "exit 0"},
		{tcell("h1", "done", func(c *keelclient.GroupRunCell) { c.Result = "user" }), "marked done"},
		{tcell("h1", "failed", func(c *keelclient.GroupRunCell) { c.Exit = ip(3) }), "exit 3"},
		{tcell("h1", "typed", func(c *keelclient.GroupRunCell) { c.Exit, c.Confirm = ip(0), true }), "exit 0 · confirm"},
		{tcell("h1", "typed", func(c *keelclient.GroupRunCell) { c.Result = "unreported" }), "no exit status"},
		{tcell("h1", "typed", func(c *keelclient.GroupRunCell) { c.Result = "abandoned" }), "shell went away"},
		{tcell("h1", "typed", nil), "running"},
		{tcell("h1", "waiting", func(c *keelclient.GroupRunCell) { c.Blocked = "No pane" }), "needs a pane"},
		{tcell("h1", "typing", nil), "typing"},
		{tcell("h1", "proposed", nil), "waiting for you"},
		{tcell("h1", "skipped", nil), "skipped"},
	} {
		if got := cellView(&c.cell).text; got != c.want {
			t.Errorf("%s: %q, want %q", c.cell.State, got, c.want)
		}
	}
}

func TestTheApproveDialogOneRowPerHostNothingTickedUnlessItIsTheOnlyOne(t *testing.T) { // "the approve dialog: one row per host, …"
	cells := []keelclient.GroupRunCell{
		tcell("h1", "proposed", func(c *keelclient.GroupRunCell) { c.Proposal = tprop("1", nil) }),
		tcell("h2", "proposed", func(c *keelclient.GroupRunCell) {
			c.Proposal = tprop("2", func(p *keelclient.GroupProposal) { p.Production, p.NeedsShift = true, true })
		}),
	}
	rows := approveRows(cells)
	if len(rows) != 2 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if r.checked {
			t.Errorf("%s is ticked in advance", r.host)
		}
	}
	if rows[0].proposalID != "p_1" || rows[1].sessionID != "s_2" {
		t.Errorf("rows %+v %+v", rows[0], rows[1])
	}
	if got := typeLabel(rows); got != "Tick the hosts to type into" {
		t.Errorf("label %q", got)
	}
	if rowsNeedShift(rows) {
		t.Error("an unticked production host asks for nothing")
	}
	rows[1].checked = true
	if !rowsNeedShift(rows) || typeLabel(rows) != "Type into 1 pane" {
		t.Errorf("shift %v, label %q", rowsNeedShift(rows), typeLabel(rows))
	}
	if rowGroups(rows) != 1 {
		t.Error("the rows type different bytes")
	}
	if one := approveRows(cells[:1]); !one[0].checked {
		t.Error("one host: the ordinary approval is ticked")
	}
	if got := bytesLine(rows[0].bytes); got != "1 line · 32 bytes" {
		t.Errorf("bytesLine = %q", got)
	}
	rows[0].checked = true
	if typeLabel(rows) != "Type into 2 panes" {
		t.Errorf("label %q", typeLabel(rows))
	}
}

func TestOnlyProposalsWaitingInAnOpenPaneCanBeTyped(t *testing.T) { // "only proposals waiting in an open pane can be typed"
	run := &keelclient.GroupRun{Status: "active", Steps: []keelclient.GroupRunStep{{Cells: []keelclient.GroupRunCell{
		tcell("h1", "proposed", func(c *keelclient.GroupRunCell) { c.Proposal = tprop("1", nil) }),
		tcell("h2", "typed", func(c *keelclient.GroupRunCell) { c.Proposal = tprop("2", nil) }),
		tcell("h3", "proposed", func(c *keelclient.GroupRunCell) { c.Proposal = tprop("3", nil) }),
		tcell("h4", "proposed", nil),
	}}}}
	got := typeable(run, func(sid string) bool { return sid != "s_3" })
	if len(got) != 1 || got[0].HostID != "h1" {
		t.Errorf("typeable %v", got)
	}
	stopped := *run
	stopped.Status = "stopped"
	if got := typeable(&stopped, func(string) bool { return true }); len(got) != 0 {
		t.Errorf("a stopped run offers %v", got)
	}
	none := *run
	none.Current = -1
	if got := typeable(&none, func(string) bool { return true }); len(got) != 0 {
		t.Errorf("a run with no current step offers %v", got)
	}
	if got := typeable(nil, func(string) bool { return true }); got != nil {
		t.Errorf("no run offers %v", got)
	}
}

func TestRollingHaltSaysWhichHostsWereNotTouched(t *testing.T) { // "rolling halt says which hosts were not touched"
	run := &keelclient.GroupRun{Halted: "wk-3", Steps: []keelclient.GroupRunStep{{Cells: []keelclient.GroupRunCell{
		tcell("h1", "done", nil), tcell("h2", "done", nil),
		tcell("h3", "failed", func(c *keelclient.GroupRunCell) { c.Exit = ip(1) }),
		tcell("h4", "waiting", nil), tcell("h5", "waiting", nil)}}}}
	h := haltLine(run)
	if h == nil || h.title != "wk-3 failed with exit 1. Rolling stopped before wk-4." || h.sub != "wk-4 and wk-5 were not touched." || h.hostID != "h3" {
		t.Fatalf("halt %+v", h)
	}
	if got := haltLine(&keelclient.GroupRun{Steps: run.Steps}); got != nil {
		t.Errorf("no halt says %+v", got)
	}
	if got := stepSummary(run.Steps[0]); got != "2 of 5 done · 1 failed" {
		t.Errorf("summary %q", got)
	}
	if _, state := stepMark(run, 0); state != "failed" {
		t.Errorf("mark %q", state)
	}
	if _, state := stepMark(&keelclient.GroupRun{Current: 0, Steps: []keelclient.GroupRunStep{{Skipped: true}}}, 0); state != "skipped" {
		t.Errorf("skipped step mark %q", state)
	}
}

func TestFillRowsExactFilledBytesPerHostAFailedFillCannotBeTicked(t *testing.T) {
	mk := func(id, host string, checked bool) *groupRow {
		return &groupRow{proposalID: id, host: host, bytes: "\x15x {{t}}\n", checked: checked}
	}
	rows := []*groupRow{mk("p1", "wk-1", true), mk("p2", "wk-2", false), mk("p3", "wk-3", false)}
	fillRows(rows, []keelclient.GroupFill{
		{ProposalRef: "p1", Bytes: "\x15x abc\n", Used: []keelclient.FillUsed{{Name: "t", From: "group variable"}}, NeedsShift: true},
		{ProposalRef: "p2", Error: "{{t}} has no value for wk-2"},
	})
	if rows[0].bytes != "\x15x abc\n" || !rows[0].needsShift {
		t.Errorf("row 1 %+v", rows[0])
	}
	if rows[1].bytes != "" || rows[1].checked || !strings.Contains(rows[1].err, "no value") {
		t.Errorf("row 2 %+v", rows[1])
	}
	if !strings.Contains(rows[2].err, "no longer waiting") || rows[2].bytes != "" {
		t.Errorf("row 3 %+v", rows[2])
	}
	// A failed fill unticks a row even if it was the only one: it cannot be typed.
	one := []*groupRow{mk("p1", "wk-1", true)}
	fillRows(one, []keelclient.GroupFill{{ProposalRef: "p1", Error: "nope"}})
	if one[0].checked || one[0].err != "nope" {
		t.Errorf("a failed fill left the row ticked: %+v", one[0])
	}
	// approveGroups: one group when every host types the same, else one per distinct bytes.
	if n := rowGroups([]*groupRow{{bytes: "1"}, {bytes: "1"}, {bytes: "2"}}); n != 2 {
		t.Errorf("rowGroups = %d", n)
	}
	if n := rowGroups([]*groupRow{{bytes: "1"}, {bytes: "1"}}); n != 1 {
		t.Errorf("rowGroups = %d", n)
	}
}

func TestSameSourcesSameBytesFromDifferentPlacesAreListedPerHost(t *testing.T) { // "sameSources: …"
	a := &groupRow{used: []keelclient.FillUsed{{Name: "t", From: "captured on cp-1 · step 1"}}}
	b := &groupRow{used: []keelclient.FillUsed{{Name: "t", From: "group variable"}}}
	a2 := &groupRow{used: []keelclient.FillUsed{{Name: "t", From: "captured on cp-1 · step 1"}}}
	if !sameSources([]*groupRow{a, a2}) || sameSources([]*groupRow{a, b}) || !sameSources(nil) {
		t.Error("sameSources is not the app's")
	}
}

func TestRunStatusLines(t *testing.T) {
	run := &keelclient.GroupRun{Status: "active", Current: 1, Mode: "parallel", Steps: make([]keelclient.GroupRunStep, 3)}
	if got := runStatusLine(run); got != "Runbook · step 2 of 3 · parallel" {
		t.Errorf("active: %q", got)
	}
	run.Status, run.Current = "stopped", 1
	if got := runStatusLine(run); got != "Runbook stopped at step 2" {
		t.Errorf("stopped: %q", got)
	}
	run.Status = "finished"
	if got := runStatusLine(run); got != "Runbook finished" {
		t.Errorf("finished: %q", got)
	}
	if modeShort("rolling") != "rolling, one at a time" || modeShort("parallel") != "parallel" {
		t.Error("modeShort")
	}
}
