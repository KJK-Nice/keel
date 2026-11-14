package native

import (
	"fmt"
	"strings"

	"keel/internal/native/keelclient"
)

// The pure rules of a group run (docs/ui-redesign.md §17, §6 rules 10 and 11): the words and decisions of
// cmd/keel/web/js/lib/grouprun.js and of fillRows and approveGroups in lib/groupvars.js, pinned there by
// test/grouprun.test.mjs and test/groupvars.test.mjs and here by grouprunlogic_test.go and grouprun_test.go. Nothing here
// talks to keel serve, draws or types. A row of the approve dialog holds bytes keel serve gave (the proposal's, or the
// fill's); nothing here builds, edits or substitutes into them.

// The words of the page (lib/grouprun.js).
const (
	modeHelpRolling  = "Rolling: one host at a time, in order. The first failure stops the step before the next host."
	modeHelpParallel = "Parallel: every host at once. A failure does not stop the others. Best for read-only steps."
	modeLockedText   = "The mode can change once no host of this step is proposed or waiting for its result, and no failure stopped the step."
)

func modeHelp(mode string) string {
	if mode == "parallel" {
		return modeHelpParallel
	}
	return modeHelpRolling
}

func modeShort(mode string) string {
	if mode == "parallel" {
		return "parallel"
	}
	return "rolling, one at a time"
}

// targetLabel says where a step runs: "Role worker · 5 hosts", "One control-plane host", "Every host · 8 hosts", "You check it".
func targetLabel(step keelclient.GroupRunStep) string {
	if step.Command == "" {
		return "No command · you check it"
	}
	n := len(step.Cells)
	if step.OneHost {
		n = 1
		if step.Role != "" {
			return "One " + step.Role + " host"
		}
		return "One host in the group"
	}
	if n == 0 {
		if step.Role != "" {
			return "Role " + step.Role + " · no host has it"
		}
		return "No host the agent may read"
	}
	hosts := countWord(n, "host", "hosts")
	if step.Role != "" {
		return "Role " + step.Role + " · " + hosts
	}
	return "Every host · " + hosts
}

// cellText is how a host × step shows: its glyph, words and kind of colour (ok, fail, run, wait).
type cellText struct{ glyph, text, kind, state string }

var cellGlyph = map[string]string{"done": "✓", "failed": "✗", "typed": "◌", "typing": "◌", "proposed": "·", "waiting": "·", "skipped": "–"}

// cellView never says more than the shell reported (lib/grouprun.js cellView).
func cellView(c *keelclient.GroupRunCell) cellText {
	if c == nil {
		return cellText{kind: "wait", state: "none"}
	}
	text, kind := "", "wait"
	switch c.State {
	case "done":
		kind = "ok"
		switch {
		case c.Exit != nil && c.Result == "reported":
			text = fmt.Sprintf("exit %d", *c.Exit)
		case c.Result == "user":
			text = "marked done"
		default:
			text = "done"
		}
	case "failed":
		kind = "fail"
		text = "failed"
		if c.Exit != nil {
			text = fmt.Sprintf("exit %d", *c.Exit)
		}
	case "typed":
		kind = "run"
		switch {
		case c.Confirm:
			text = "exit 0 · confirm"
		case c.Result == "unreported":
			text = "no exit status"
		case c.Result == "abandoned":
			text = "shell went away"
		default:
			text = "running"
		}
	case "typing":
		kind, text = "run", "typing"
	case "proposed":
		kind, text = "run", "waiting for you"
	case "skipped":
		text = "skipped"
	default:
		text = "waiting"
		if c.Blocked != "" {
			text = "needs a pane"
		}
	}
	g, ok := cellGlyph[c.State]
	if !ok {
		g = "·"
	}
	return cellText{glyph: g, text: text, kind: kind, state: c.State}
}

// stepSummary is the results head: "2 of 5 done · 1 failed".
func stepSummary(step keelclient.GroupRunStep) string {
	if len(step.Cells) == 0 {
		switch {
		case step.Done:
			return "Checked"
		case step.Skipped:
			return "Skipped"
		}
		return "No hosts"
	}
	ok, failed, skipped, idle := 0, 0, 0, 0
	for _, c := range step.Cells {
		switch c.State {
		case "done":
			ok++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}
		if c.State == "waiting" && c.Proposal == nil {
			idle++
		}
	}
	if idle == len(step.Cells) {
		return "Not started"
	}
	parts := []string{fmt.Sprintf("%d of %d done", ok, len(step.Cells))}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	return strings.Join(parts, " · ")
}

// typeable are the current step's cells whose proposal waits to be typed, with a pane open in this window and driven by it
// (paneOpen(sessionID)). Nothing else is ever offered for typing: not a typed, skipped or waiting host, not a stopped run,
// not a pane that is closed, followed or joined.
func typeable(run *keelclient.GroupRun, paneOpen func(sessionID string) bool) (out []keelclient.GroupRunCell) {
	if run == nil || run.Status != "active" || run.Current < 0 || run.Current >= len(run.Steps) {
		return nil
	}
	for _, c := range run.Steps[run.Current].Cells {
		if c.State == "proposed" && c.Proposal != nil && paneOpen(c.Proposal.SessionID) {
			out = append(out, c)
		}
	}
	return out
}

// groupRow is a row of the group approve dialog: a host, the pane it would be typed into, and the exact bytes keel serve
// gave for that host (the proposal's own bytes, or the fill's, with the values put in). checked is the user's tick and
// nothing else sets it.
type groupRow struct {
	hostID, host string
	sessionID    string
	proposalID   string
	bytes        string
	used         []keelclient.FillUsed
	production   bool
	needsShift   bool
	checked      bool
	err          string // why the row cannot be ticked (a failed fill)
	paneLabel    string

	attempted bool   // the Type reached this row: it is final, nothing is retried
	outcome   string // "Typed." or keel serve's refusal in its words
	typedOK   bool
}

// approveRows are the dialog's checklist: one row per host, never ticked in advance unless it is the only one (the one-host
// case, which is the ordinary approval).
func approveRows(cells []keelclient.GroupRunCell) []*groupRow {
	one := len(cells) == 1
	var out []*groupRow
	for _, c := range cells {
		p := c.Proposal
		out = append(out, &groupRow{hostID: c.HostID, host: c.Host, sessionID: p.SessionID, proposalID: p.ID, bytes: p.Bytes,
			production: p.Production, needsShift: p.NeedsShift, checked: one})
	}
	return out
}

// fillRows puts keel serve's fills on the dialog's rows: each row's exact bytes, what went in, and the ⇧ rule on the filled
// command. A row whose fill failed (a missing or unsafe value) keeps no bytes, cannot be ticked and says why; a row with no
// fill at all cannot be ticked either.
func fillRows(rows []*groupRow, fills []keelclient.GroupFill) {
	byID := map[string]keelclient.GroupFill{}
	for _, f := range fills {
		byID[f.ProposalRef] = f
	}
	for _, r := range rows {
		f, ok := byID[r.proposalID]
		switch {
		case !ok:
			r.bytes, r.used, r.checked, r.err = "", nil, false, "This host's proposal is no longer waiting; it is proposed again."
		case f.Error != "":
			r.bytes, r.used, r.checked, r.err = "", nil, false, f.Error
		default:
			r.bytes, r.used = f.Bytes, f.Used
			r.needsShift = r.needsShift || f.NeedsShift
		}
	}
}

// rowsNeedShift: any ticked row needs ⇧ (a changes-state step on a production host, or a destructive one).
func rowsNeedShift(rows []*groupRow) bool {
	for _, r := range rows {
		if r.checked && r.needsShift {
			return true
		}
	}
	return false
}

func ticked(rows []*groupRow) (n int) {
	for _, r := range rows {
		if r.checked {
			n++
		}
	}
	return n
}

// typeLabel is the dialog's button: "Type into 3 panes".
func typeLabel(rows []*groupRow) string {
	n := ticked(rows)
	if n == 0 {
		return "Tick the hosts to type into"
	}
	return "Type into " + countWord(n, "pane", "panes")
}

// rowGroups splits the rows that can be typed by their bytes: one group when every host types the same, else one per
// distinct bytes (values captured per host can differ).
func rowGroups(rows []*groupRow) (n int) {
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.bytes] {
			seen[r.bytes] = true
			n++
		}
	}
	return n
}

// sameSources: every row's values came from the same places, so the dialog says it once; otherwise each row says its own.
func sameSources(rows []*groupRow) bool {
	seen := map[string]bool{}
	for _, r := range rows {
		seen[usedLine(r.used)] = true
	}
	return len(seen) <= 1
}

// bytesLine is "1 line · 75 bytes": the lines of the command (a leading Ctrl-U and the last newline do not count) and the
// bytes sent.
func bytesLine(b string) string {
	body := strings.TrimSuffix(strings.TrimPrefix(b, "\x15"), "\n")
	lines := 0
	if body != "" {
		lines = strings.Count(body, "\n") + 1
	}
	return fmt.Sprintf("%s · %d bytes", countWord(lines, "line", "lines"), len(b))
}

// haltLine is the rolling stop banner: which host failed and which were not touched.
type halt struct{ title, sub, hostID string }

func haltLine(run *keelclient.GroupRun) *halt {
	if run == nil || run.Halted == "" || run.Current < 0 || run.Current >= len(run.Steps) {
		return nil
	}
	cells := run.Steps[run.Current].Cells
	i := -1
	for k, c := range cells {
		if c.Host == run.Halted && c.State == "failed" {
			i = k
			break
		}
	}
	var failed keelclient.GroupRunCell
	var rest []string
	if i >= 0 {
		failed = cells[i]
		for _, c := range cells[i+1:] {
			if c.State != "done" && c.State != "skipped" {
				rest = append(rest, c.Host)
			}
		}
	}
	exit, next, untouched := "", "", ""
	if failed.Exit != nil {
		exit = fmt.Sprintf(" with exit %d", *failed.Exit)
	}
	if len(rest) > 0 {
		next = " Rolling stopped before " + rest[0] + "."
		verb := "were"
		if len(rest) == 1 {
			verb = "was"
		}
		untouched = listWords(rest) + " " + verb + " not touched."
	}
	return &halt{title: run.Halted + " failed" + exit + "." + next, sub: untouched, hostID: failed.HostID}
}

func listWords(xs []string) string {
	if len(xs) <= 1 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// stepMark is a step's mark in the steps list.
func stepMark(run *keelclient.GroupRun, i int) (mark, state string) {
	st := run.Steps[i]
	all := len(st.Cells) > 0
	failed := false
	for _, c := range st.Cells {
		if c.State != "done" && c.State != "skipped" {
			all = false
		}
		if c.State == "failed" {
			failed = true
		}
	}
	switch {
	case st.Skipped:
		return "–", "skipped"
	case st.Done || all:
		return "✓", "done"
	case failed:
		return "✗", "failed"
	case i == run.Current:
		return fmt.Sprint(i + 1), "now"
	}
	return fmt.Sprint(i + 1), "next"
}

// runStatusLine is the run's line in the head and the status bar.
func runStatusLine(run *keelclient.GroupRun) string {
	if run == nil {
		return ""
	}
	switch run.Status {
	case "finished":
		return "Runbook finished"
	case "stopped":
		return fmt.Sprintf("Runbook stopped at step %d", stoppedAt(run))
	}
	return fmt.Sprintf("Runbook · step %d of %d · %s", run.Current+1, len(run.Steps), modeShort(run.Mode))
}

// stoppedAt is "stopped at step N": the current step, else the last.
func stoppedAt(run *keelclient.GroupRun) int {
	if n := run.Current + 1; n > 0 {
		return n
	}
	return len(run.Steps)
}
