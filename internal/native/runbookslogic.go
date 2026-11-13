package native

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
)

// The pure rules of runbooks (docs/ui-redesign.md §16): the words and states the screens show for a saved runbook, a
// runbook in use in a conversation and a runbook the agent proposes. They are cmd/keel/web/js/lib/runbooks.js, pinned there
// by test/runbooks.test.mjs and here by runbookslogic_test.go, without the parts that belong to group runs (§17: roles,
// "one host only", captures) and to notebooks. Nothing here talks to keel serve, draws or types.

const maxRunbookSteps = limits.MaxRunbookSteps

type rbSource struct{ label, long string }

// srcAgent names the runbook source of the agent's proposals. It is a constant because guard_test.go keeps the quoted key
// of an agent reference for keelclient/inject.go.
const srcAgent = "agent"

var rbSources = map[string]rbSource{
	"user":         {"You", "Written by you"},
	"conversation": {"From conversation", "From the conversation"},
	"notebook":     {"From notebook", "From the notebook"},
	srcAgent:       {"Proposed", "Proposed by the agent"},
}

func sourceOf(source string) rbSource {
	if s, ok := rbSources[source]; ok {
		return s
	}
	return rbSources["user"]
}

// runbookSourceBadge is the small badge on a list row: its label, and whether it is the plain (user) one.
func runbookSourceBadge(rb keelclient.Runbook) (label, kind string) {
	switch rb.Source {
	case "conversation", "notebook":
		return sourceOf(rb.Source).label, "haze"
	case srcAgent:
		return sourceOf(rb.Source).label, "signal"
	}
	return sourceOf(rb.Source).label, ""
}

// runbookSourceLine is the detail's Source: "Written by you · Oct 12", "From the conversation “nginx 502” · Oct 15",
// "Proposed by the agent after reading panes · Oct 15".
func runbookSourceLine(rb keelclient.Runbook) string {
	head := sourceOf(rb.Source).long
	if (rb.Source == "conversation" || rb.Source == "notebook") && rb.SourceRef != "" {
		head += " “" + rb.SourceRef + "”"
	}
	if rb.Source == "agent" && rb.FromPane {
		head += " after reading panes"
	}
	return joinNonEmpty(" · ", head, dayLabel(rb.CreatedAt))
}

// runbookNeedsReview is a runbook that was not written by the user and is not turned on yet: the page asks for a review
// before Agent may pick.
func runbookNeedsReview(rb keelclient.Runbook) bool {
	return rb.Source != "" && rb.Source != "user" && !rb.AgentMayPick
}

// runbookReviewText is the review banner's words.
func runbookReviewText(rb keelclient.Runbook) string {
	var from string
	switch rb.Source {
	case srcAgent:
		from = "The agent proposed it"
	case "notebook":
		from = "Made from the notebook"
		if rb.SourceRef != "" {
			from += " “" + rb.SourceRef + "”"
		}
	default:
		from = "Made from the conversation"
		if rb.SourceRef != "" {
			from += " “" + rb.SourceRef + "”"
		}
	}
	pane := ""
	if rb.FromPane {
		pane = " after reading terminal output"
	}
	var changes []string
	for i, s := range rb.Steps {
		if s.Risk != "" && s.Risk != keelclient.RiskReadOnly {
			changes = append(changes, fmt.Sprint(i+1))
		}
	}
	note := ""
	switch len(changes) {
	case 0:
	case 1:
		note = " Step " + changes[0] + " changes state."
	default:
		note = " Steps " + strings.Join(changes, ", ") + " change state."
	}
	return from + pane + ". The agent reads a runbook as a checklist, so read every step." + note
}

// runbookUsedLabel is "Last used on prod-db-02 · today 09:15", or "Never used".
func runbookUsedLabel(rb keelclient.Runbook, now time.Time) string {
	if rb.LastUsedAt == nil {
		return "Never used"
	}
	when := stampLabel(*rb.LastUsedAt, now)
	on := ""
	if rb.LastUsedOn != "" {
		on = " on " + rb.LastUsedOn
	}
	if !strings.Contains(when, ",") {
		when = "today " + when
	}
	return "Last used" + on + " · " + when
}

// runbookStepsLine is "4 steps · all read-only" or "3 steps · 1 changes state".
func runbookStepsLine(rb keelclient.Runbook) string {
	changes, commands := 0, 0
	for _, s := range rb.Steps {
		if s.Command != "" {
			commands++
			if s.Risk != "" && s.Risk != keelclient.RiskReadOnly {
				changes++
			}
		}
	}
	n := countWord(len(rb.Steps), "step", "steps")
	switch {
	case commands == 0:
		return n + " · no commands"
	case changes == 0:
		return n + " · all read-only"
	case changes == 1:
		return n + " · 1 changes state"
	}
	return fmt.Sprintf("%s · %d change state", n, changes)
}

// runbookStepKind is a step's kind on the page: Check, Read-only, Destructive or Changes (Keel's own rating of the saved
// command), with the badge colour.
func runbookStepKind(command, risk string) (label, kind string) {
	switch {
	case command == "":
		return "Check", ""
	case risk == keelclient.RiskReadOnly:
		return "Read-only", "tide"
	case risk == keelclient.RiskDestructive:
		return "Destructive", "flare"
	}
	return "Changes", "signal"
}

// filterRunbooks narrows the list by name, Use when, hosts and step titles: every word must be in one of them.
func filterRunbooks(list []keelclient.Runbook, q string) []keelclient.Runbook {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return list
	}
	var out []keelclient.Runbook
	for _, rb := range list {
		hay := []string{rb.Name, rb.When}
		hay = append(hay, rb.Hosts...)
		for _, s := range rb.Steps {
			hay = append(hay, s.Title)
		}
		all := strings.ToLower(strings.Join(hay, " "))
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(all, w)
		}
		if ok {
			out = append(out, rb)
		}
	}
	return out
}

// rbPane is a pane in scope as a runbook's offer sees it: the folder and tags of its host, and whether the agent may
// read it.
type rbPane struct {
	group    string
	tags     []string
	readable bool
}

// runbookFitsPane is keel serve's rule for offering a runbook: no hosts, or a pane whose host's folder or one of its tags
// is listed (any case).
func runbookFitsPane(rb keelclient.Runbook, p *rbPane) bool {
	if len(rb.Hosts) == 0 {
		return true
	}
	if p == nil {
		return false
	}
	for _, h := range rb.Hosts {
		if p.group != "" && strings.EqualFold(h, p.group) {
			return true
		}
		for _, t := range p.tags {
			if strings.EqualFold(h, t) {
				return true
			}
		}
	}
	return false
}

// runbookOfferFor says whether a runbook can be started for the panes in scope, and why not ("Not offered: no nginx host
// in scope").
func runbookOfferFor(rb keelclient.Runbook, panes []rbPane) (ok bool, reason string) {
	var list []rbPane
	for _, p := range panes {
		if p.readable {
			list = append(list, p)
		}
	}
	if len(list) == 0 {
		return false, "Put a pane the agent may read in scope first"
	}
	for i := range list {
		if runbookFitsPane(rb, &list[i]) {
			return true, ""
		}
	}
	return false, "Not offered: no " + strings.Join(rb.Hosts, " or ") + " host in scope"
}

// cleanLine is one line, as keel serve keeps a runbook's text fields.
func cleanLine(text string) string {
	var b strings.Builder
	for _, r := range text {
		if isInvisibleRune(r) {
			r = ' '
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.FieldsFunc(b.String(), unicode.IsSpace), " ")
}

// rbEditStep and rbEdit are the editor's state as toRunbookInput reads it.
type rbEditStep struct {
	id, title, description, command, expect string
	role                                    string
	oneHost                                 bool
	captures                                []keelclient.StepCapture
}

type rbEdit struct {
	name, when   string
	hosts        []string
	agentMayPick bool
	steps        []rbEditStep
	draftID      string
}

// toRunbookInput is the editor's state as POST/PUT /api/agent/runbooks takes it. Empty steps (no title and no command)
// are dropped. keel serve takes the source from its own record of the draft.
func toRunbookInput(ed rbEdit) keelclient.RunbookInput {
	steps := []keelclient.RunbookStepInput{}
	for _, s := range ed.steps {
		st := keelclient.RunbookStepInput{
			ID: s.id, Title: cleanLine(s.title), Description: cleanLine(s.description),
			Command: strings.TrimSpace(strings.ReplaceAll(s.command, "\r\n", "\n")), Expect: cleanLine(s.expect),
			Role: strings.ToLower(strings.TrimSpace(s.role)), OneHost: s.oneHost, Captures: cleanCaptures(s.captures),
		}
		if st.Title != "" || st.Command != "" {
			steps = append(steps, st)
		}
	}
	hosts := []string{}
	for _, h := range ed.hosts {
		if h = cleanLine(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	return keelclient.RunbookInput{Name: cleanLine(ed.name), When: cleanLine(ed.when), Hosts: hosts, AgentMayPick: ed.agentMayPick, Steps: steps, DraftID: ed.draftID}
}

// checkRunbookInput says what must change before a save, or "" when it can be saved.
func checkRunbookInput(in keelclient.RunbookInput) string {
	switch {
	case in.Name == "":
		return "Name the runbook."
	case len(in.Steps) == 0:
		return "Add at least one step."
	case len(in.Steps) > maxRunbookSteps:
		return fmt.Sprintf("A runbook has at most %d steps.", maxRunbookSteps)
	}
	for i, s := range in.Steps {
		if s.Title == "" {
			return fmt.Sprintf("Step %d needs a title.", i+1)
		}
	}
	for i, s := range in.Steps {
		if s.Role != "" && !validRole(s.Role) {
			return fmt.Sprintf("Step %d: a role is one word of letters, digits, -, _ or .", i+1)
		}
	}
	return checkCaptures(in.Steps)
}

// validRole is one role word as keel serve keeps it (limits.ValidRole).
func validRole(r string) bool { return limits.ValidRole(r) }

// addHost adds a folder or tag chip (no duplicates, any case).
func addHost(hosts []string, text string) []string {
	t := cleanLine(text)
	if t == "" {
		return hosts
	}
	for _, h := range hosts {
		if strings.EqualFold(h, t) {
			return hosts
		}
	}
	return append(append([]string{}, hosts...), t)
}

// moveItem moves item i by delta (-1 up, +1 down), within bounds, in a copy.
func moveItem[T any](items []T, i, delta int) []T {
	j := i + delta
	if i < 0 || i >= len(items) || j < 0 || j >= len(items) {
		return items
	}
	out := append([]T{}, items...)
	out[i], out[j] = out[j], out[i]
	return out
}

// ---- a runbook in use in a conversation ----

// currentStep is the first step not done or skipped, or -1.
func currentStep(run *keelclient.ChatRunbook) int {
	if run == nil {
		return -1
	}
	for i, s := range run.Steps {
		if s.State != "done" && s.State != "skipped" {
			return i
		}
	}
	return -1
}

// runRow is one row of the progress list: its state for the marks, the mark, and a short note.
type runRow struct{ state, mark, note string }

// stepRow is runbooks.js stepRow.
func stepRow(run *keelclient.ChatRunbook, i int, now time.Time) runRow {
	st := run.Steps[i]
	cur := currentStep(run)
	exit := ""
	if st.Exit != nil {
		exit = fmt.Sprintf("exit %d", *st.Exit)
	}
	reported := ""
	if st.Result == "reported" {
		reported = exit + " · reported"
	}
	n := fmt.Sprint(i + 1)
	switch st.State {
	case "done":
		note := "done"
		switch {
		case st.Result == "user":
			note = "marked done"
		case reported != "":
			note = reported
		}
		if st.Edited {
			note += " · edited"
		}
		return runRow{"done", "✓", note}
	case "skipped":
		return runRow{"skipped", "–", "skipped"}
	case "failed":
		note := reported
		if st.Result == "user" {
			note = "failed"
		}
		return runRow{"failed", "✗", note}
	case "typed":
		t := "typed"
		if st.TypedAt != nil {
			t = "typed " + stampLabel(*st.TypedAt, now)
		}
		if st.Edited {
			t += " · edited"
		}
		switch {
		case st.Result == "reported" && st.Exit != nil && *st.Exit == 0:
			return runRow{"now", n, "exit 0 · reported — confirm"}
		case st.Result == "abandoned":
			return runRow{"now", n, t + " · shell went away"}
		case st.Result == "unreported":
			return runRow{"now", n, t + " · no exit status"}
		}
		return runRow{"now", n, t}
	case "proposed":
		if i == cur {
			return runRow{"now", n, "proposed"}
		}
		return runRow{"next", n, "proposed"}
	}
	if i == cur {
		return runRow{"now", n, ""}
	}
	return runRow{"next", n, ""}
}

// runAction is what the run card offers for the current step (runbooks.js nextAction).
type runAction struct {
	kind string // propose | proposed | confirm | waiting | noexit | failed | check | finished | stopped | none
	step runStepRef
	exit *int
}

type runStepRef struct {
	index, n, of int
	title, id    string
}

// nextAction says what the run card offers: never a later step before this one. live says whether a proposal id is still a
// live card.
func nextAction(run *keelclient.ChatRunbook, live func(id string) bool) runAction {
	if run == nil {
		return runAction{kind: "none"}
	}
	if run.Status == "stopped" {
		return runAction{kind: "stopped"}
	}
	i := currentStep(run)
	if i < 0 || run.Status == "finished" {
		return runAction{kind: "finished"}
	}
	st := run.Steps[i]
	step := runStepRef{index: i, n: i + 1, of: len(run.Steps), title: st.Title, id: st.ID}
	switch {
	case st.State == "failed":
		return runAction{kind: "failed", step: step, exit: st.Exit}
	case st.State == "typed" && st.Result == "reported" && st.Exit != nil && *st.Exit == 0:
		return runAction{kind: "confirm", step: step}
	case st.State == "typed" && (st.Result == "abandoned" || st.Result == "unreported"):
		return runAction{kind: "noexit", step: step}
	case st.State == "typed":
		return runAction{kind: "waiting", step: step}
	case st.Command == "":
		return runAction{kind: "check", step: step}
	case st.State == "proposed" && live != nil && live(st.ProposalRef):
		return runAction{kind: "proposed", step: step}
	}
	return runAction{kind: "propose", step: step}
}

// waitingForShell is true while a typed step has no result: the window reads the run again for the exit status the pane's
// shell reports.
func waitingForShell(run *keelclient.ChatRunbook) bool {
	if run == nil || run.Status != "active" {
		return false
	}
	for _, s := range run.Steps {
		if s.State == "typed" && s.Result == "" {
			return true
		}
	}
	return false
}

// runWhyLine is the run card's second line.
func runWhyLine(run *keelclient.ChatRunbook) string {
	if run.PickedBy == "agent" {
		if run.Why != "" {
			return "Why: " + run.Why
		}
		return "The agent picked this runbook."
	}
	return "You picked this runbook."
}

// continueQuestion is what Propose step N asks the agent.
func continueQuestion(step runStepRef) string {
	return fmt.Sprintf("Continue with step %d of %d: %s", step.n, step.of, step.title)
}

// startQuestion is what starting a runbook asks.
func startQuestion(name string) string {
	return "Walk me through the runbook “" + cleanLine(name) + "”."
}

// ---- a runbook the agent proposes ----

// runbookProposalWarn is any proposal from an answer that read a pane: its steps may hold text from terminal output.
func runbookProposalWarn(p keelclient.RunbookProposal) bool {
	return p.FromPane || len(p.PanesRead) > 0
}

func runbookProposalWarning(p keelclient.RunbookProposal) string {
	where := ""
	if p.FromPane && p.SeenIn != "" {
		where = " Some of it matches pane " + p.SeenIn + "."
	}
	return "This runbook was written after reading terminal output." + where + " Check every step before saving: a saved runbook can be offered to the agent, and text in a pane can be written to mislead it."
}

func runbookProposalDone(p keelclient.RunbookProposal) string {
	switch p.Status {
	case "saved":
		return "Saved to Runbooks. The agent may not pick it until you review it and turn it on."
	case "dismissed":
		return "Dismissed. Nothing was saved."
	}
	return ""
}
