package native

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/cmdrule"
	"keel/internal/native/keelclient"
)

// Approve before type (docs/ui-redesign.md §6): keel-native types nothing on the user's behalf until the user has seen
// the exact bytes and pressed Type, except one plainly read-only line that keel serve agrees to type with no dialog and
// no Enter (typenow.go, rule 13). This file is the only caller of keelclient.ApprovedInject, and Type is its only
// trigger (guard_test.go keeps both true).
//
// The group approve dialog (docs/ui-redesign.md §6 rule 10, §17; groupapproveview.go) is this same dialog and this same Type
// action: its Type presses approveType, which makes one job per ticked row (groupJobs) and runs them in one loop through the
// one ApprovedInject call, each on its row's own pane with that row's own proposal and so its own server nonce. Nothing is ticked
// for the user unless there is only one host; only ticked rows are typed; a refusal for one row is that row's and does not
// stop the others; nothing is retried. A row's bytes are keel serve's: the proposal's, or the fill's with the values in (rule 11).
//
// The bytes: the command gets a trailing newline if it has none, the dialog shows that
// text, and what is sent is Ctrl-U (it clears the line first, as the dialog says) followed by exactly that text.
// keel serve writes the bytes it is given and adds nothing (runtime.Session.InjectTicket). Nothing is kept: the
// dialog asks its source for the command every time it draws and every time Type is pressed.
//
// There is no production or ⇧ rule for snippets. An agent proposal is typed through the same
// dialog (agent.go typeAgentProposal): the dialog then shows the proposal's own bytes, as keel serve holds them (Ctrl-U,
// the command, a newline, the control characters drawn as glyphs), sends exactly those with the reference to the
// proposal, and shows keel serve's answer: a refusal types nothing and is never retried. The ⇧ rule is keel serve's, and
// the card's.

// clearLine is Ctrl-U: the shell clears what is typed on the line before the snippet.
const clearLine = "\x15"

const approvePreviewMax = 130

const (
	approveTitle = "Type into the terminal?"
	approveHint  = "This sends keystrokes to the focused session. The current line is cleared first. Nothing is typed until you approve."
	// A notebook fence's bytes carry no Ctrl-U, so the dialog does not say the line is cleared.
	approveHintNotebook = "This sends keystrokes to the focused session. Nothing is typed until you approve."
	msgNoSession        = "Open a session first"
	msgNotConnected     = "Session is not connected"
	msgFollowing        = "You are following — take or request control to type"
	msgGone             = "It was deleted elsewhere; close this dialog." // hostError for a 404
)

type approveDialog struct {
	p     *pane // nil for the group dialog, which types into a pane per ticked row
	src   approveSource
	err   string // why the last Type did not type
	busy  bool
	focus bool

	cancelH, typeH ui.Handle
}

// approveSource is where the dialog's bytes come from, and what differs between the sources: exactly one of
// approveSnippet, approveNotebook, *approveAgent and *approveGroup. The group dialog has no pane of its own, draws its
// own view (groupapproveview.go) and makes its own jobs (groupJobs), so words, prepare and answered are the one-pane
// dialog's.
type approveSource interface {
	// shown is the text the dialog previews and Type sends, as it is now; false when it is gone (deleted elsewhere).
	shown() (string, bool)
	// words are the sentence under the title, the note under the bytes, and whether the bytes are drawn as glyphs.
	words() (hint, note string, glyphs bool)
	// sessionLine is the line after "Session:" for pane p.
	sessionLine(p *pane) string
	// prepare turns the one-pane job (Ctrl-U and the bytes shown) into this source's, or says why it cannot be typed.
	prepare(j *typeJob, shown string) string
	// answered takes keel serve's answer to a Type, and reports that the dialog closes whatever it was.
	answered(err error) bool
	// dropped is told the dialog closed with nothing typed; busy: a Type was still waiting.
	dropped(busy bool)
}

// approveSnippet is a snippet's command (or any text the user chose to type): it is sent behind Ctrl-U.
type approveSnippet struct {
	text func() (string, bool)
}

func (s approveSnippet) shown() (string, bool) {
	cmd, ok := s.text()
	if !strings.HasSuffix(cmd, "\n") {
		cmd += "\n"
	}
	return cmd, ok
}

func (approveSnippet) words() (string, string, bool)   { return approveHint, "", false }
func (approveSnippet) sessionLine(p *pane) string      { return paneSession(p) }
func (approveSnippet) prepare(*typeJob, string) string { return "" }
func (approveSnippet) answered(error) bool             { return false }
func (approveSnippet) dropped(bool)                    {}

// approveNotebook is a notebook fence (notebooktype.go): the bytes are the fence's line and a newline, with no Ctrl-U
// and the inject carries notebook:true. note is the line under the bytes
// when the agent edited the notebook and the user has not reviewed it (§6 rule 12).
type approveNotebook struct {
	approveSnippet
	note string
}

func (n approveNotebook) words() (string, string, bool) { return approveHintNotebook, n.note, false }

func (approveNotebook) prepare(j *typeJob, shown string) string {
	if cmdrule.HasPlaceholder(shown) { // keel serve refuses it too (the notebook-labelled inject)
		return placeholderRefused
	}
	j.command, j.opts = shown, []keelclient.InjectOption{keelclient.FromNotebook}
	return ""
}

// shown is the proposal's bytes as keel serve holds them: nothing is added, not even a newline.
func (a *approveAgent) shown() (string, bool) { return a.source() }

func (a *approveAgent) words() (string, string, bool) { return approveHint, "", true }
func (a *approveAgent) sessionLine(*pane) string      { return a.session }

// prepare: the proposal's bytes already start with Ctrl-U and end with the newline.
func (a *approveAgent) prepare(j *typeJob, shown string) string {
	j.command = shown
	j.ref = &keelclient.AgentRef{ConversationID: a.conv, ProposalID: a.proposal, Shift: a.shift}
	return ""
}

// answered: keel serve's answer is final, shown on the card.
func (a *approveAgent) answered(err error) bool { a.done(err); return true }
func (a *approveAgent) dropped(bool)            { a.cancelled() }

// shown is nothing: a group has no bytes of its own, each row has (groupRow.bytes).
func (g *approveGroup) shown() (string, bool)           { return "", true }
func (g *approveGroup) words() (string, string, bool)   { return "", "", false }
func (g *approveGroup) sessionLine(*pane) string        { return "" }
func (g *approveGroup) prepare(*typeJob, string) string { return "" }
func (g *approveGroup) answered(error) bool             { return false }

// dropped: the run is told only when no Type is out.
func (g *approveGroup) dropped(busy bool) {
	if g.cancelled != nil && !busy {
		g.cancelled()
	}
}

func (d *approveDialog) group() *approveGroup { g, _ := d.src.(*approveGroup); return g }

// approveGroup is what the group approve dialog adds (docs/ui-redesign.md §6 rule 10, §17): one row per host, none ticked in
// advance unless there is only one, and the run they belong to. Its Type is the same approveType: each ticked row is its own
// proposal, typed through the one ApprovedInject call on that row's own pane, with that call's own server nonce.
type approveGroup struct {
	runID     string
	rows      []*groupRow
	pane      func(sessionID string) *agentPane // the pane a row would be typed into, as it is now (nil: closed)
	shiftHint bool                              // a plain press was refused: the line says to hold ⇧
	done      func(typed int, failed []string)  // after a Type: the run is read again
	cancelled func()                            // the dialog closed with nothing typed
}

type typeJob struct {
	conn    session
	command string
	ref     *keelclient.AgentRef
	opts    []keelclient.InjectOption // FromNotebook for a notebook fence
	row     *groupRow                 // nil for the one-pane dialogs
}

// The words of a row that cannot be typed.
const (
	msgGroupPaneGone   = "that pane is not open in this window"
	msgGroupPaneFollow = "you follow this pane"
	msgGroupNoBytes    = "there are no bytes for this host"
)

// approveAgent is what an agent proposal's Type adds to the dialog: which proposal it is, the line after "Session:", where
// the proposal's bytes are read from (the panel's copy of what keel serve holds), and what the card is told.
type approveAgent struct {
	conv, proposal string
	shift          bool
	session        string
	source         func() (string, bool)
	done           func(err error) // keel serve's answer: nil when it typed, else its refusal
	cancelled      func()          // the dialog closed with nothing typed
}

func (d *approveDialog) shown() string {
	b, _ := d.src.shown()
	return b
}

func (d *approveDialog) has() bool {
	_, ok := d.src.shown()
	return ok
}

func (d *approveDialog) session() string { return d.src.sessionLine(d.p) }

func paneSession(p *pane) string {
	st := p.conn.Snapshot()
	switch {
	case st.Subtitle != "":
		return st.Subtitle
	case st.Title != "":
		return st.Title
	}
	return "session"
}

// blocked is why Type cannot type into the pane now, or "" (rule: driver only).
func (d *approveDialog) blocked() string {
	if d.group() != nil { // every row says for itself why it cannot be typed
		return ""
	}
	st := d.p.conn.Snapshot()
	switch {
	case !d.has():
		return msgGone
	case !st.Ready():
		return msgNotConnected
	case !st.Driving():
		return msgFollowing
	}
	return ""
}

// openApprove opens the dialog for pane p: a snippet's or a notebook fence's bytes. It sends nothing. Opening it again
// replaces it.
func (w *workspace) openApprove(p *pane, src approveSource) {
	w.approve = &approveDialog{p: p, src: src, focus: true}
}

// openApproveAgent opens the dialog for an agent proposal in pane p, showing that pane's tab first so the approval names
// the pane you can see. It sends nothing.
func (w *workspace) openApproveAgent(p *pane, ag *approveAgent) {
	w.showPane(p)
	w.approve = &approveDialog{p: p, src: ag, focus: true}
}

// typeTarget is the pane a snippet types into: the focused pane of the tab shown, else of the session tab shown
// last. It says why there is none.
func (w *workspace) typeTarget() (*pane, string) {
	t := w.tab()
	if t == nil || len(t.panes) == 0 {
		t = w.lastSession
		if t == nil || !slices.Contains(w.tabs, t) || len(t.panes) == 0 {
			return nil, msgNoSession
		}
	}
	p := t.panes[t.focused]
	if p != nil && p.nb != nil { // a notebook is not a terminal: the tab's first terminal takes it
		p = t.firstTerm()
	}
	if p == nil || p.remote || t.remote { // a joined pane takes no snippet: Approve before type is for a local session
		return nil, msgNoSession
	}
	return p, ""
}

// askType is every entry point's way to type a snippet: it opens the Approve dialog for the target pane, or says
// why it cannot. It types nothing and never calls the writer.
func (w *workspace) askType(source func() (string, bool)) {
	p, why := w.typeTarget()
	if p == nil {
		w.say(why)
		return
	}
	w.showPane(p)
	w.openApprove(p, approveSnippet{source})
}

// cancelApprove is Escape, Cancel and a click outside: the dialog closes, nothing is sent, and an agent card is told.
func (w *workspace) cancelApprove() {
	if d := w.approve; d != nil {
		d.src.dropped(d.busy)
	}
	w.closeApprove()
}

func (w *workspace) closeApprove() {
	d := w.approve
	if d == nil {
		return
	}
	w.approve = nil
	if d.p == nil {
		return
	}
	if t := w.tabOf(d.p); t != nil && t == w.tab() {
		w.wantFocus = d.p.id
	}
}

// approveType is the Type button (and its key): the one place keel-native asks to type. It sends the bytes the
// dialog shows now, behind Ctrl-U, through the one writer; keel serve decides, and a refusal stays in the dialog.
func (w *workspace) approveType(shift bool) {
	d := w.approve
	if d == nil || d.busy {
		return
	}
	var jobs []typeJob
	if d.group() != nil {
		// The group dialog (§6 rule 10): only the ticked rows, each its own job. shift is the key as it was pressed with Type.
		if jobs = w.groupJobs(d, shift); len(jobs) == 0 {
			return
		}
	} else {
		if msg := d.blocked(); msg != "" {
			d.err = msg
			return
		}
		if w.tabOf(d.p) == nil {
			d.err = msgNoSession
			return
		}
		job := typeJob{conn: d.p.conn, command: clearLine + d.shown()}
		if msg := d.src.prepare(&job, d.shown()); msg != "" {
			d.err = msg
			return
		}
		jobs = []typeJob{job}
	}
	d.busy, d.err = true, ""
	w.work(func() {
		// One call, for every dialog: a snippet's, a proposal's and each ticked row of a group's. Each job is its own
		// ApprovedInject on its own pane's connection, so each asks keel serve for its own nonce for exactly its own bytes. A
		// refusal in one does not stop the others, and nothing is retried.
		errs := make([]error, len(jobs))
		for i, j := range jobs {
			c, cancel := ctx()
			errs[i] = j.conn.ApprovedInject(c, j.command, j.ref, j.opts...)
			cancel()
		}
		w.post(func() {
			d.busy = false
			if g := d.group(); g != nil {
				w.groupTyped(d, g, jobs, errs)
				return
			}
			err := errs[0]
			if w.approve == d && d.src.answered(err) {
				w.closeApprove()
				return
			}
			switch {
			case err != nil:
				d.err = err.Error()
			case w.approve == d:
				w.closeApprove()
			}
		})
	})
}

func (w *workspace) openApproveGroup(g *approveGroup) {
	w.approve = &approveDialog{src: g, focus: true}
}

// rowBlocked is why a row cannot be ticked or typed now, or "": a failed fill, a Type that already
// reached it, and a pane that closed or became a followed one since the dialog opened.
func (g *approveGroup) rowBlocked(r *groupRow) string {
	switch {
	case r.err != "":
		return r.err
	case r.attempted:
		return ""
	case r.bytes == "":
		return msgGroupNoBytes
	}
	p := g.pane(r.sessionID)
	switch {
	case p == nil || p.p == nil || p.remote:
		return msgGroupPaneGone
	case !p.driving:
		return msgGroupPaneFollow
	case !p.p.conn.Snapshot().Ready():
		return msgNotConnected
	}
	return ""
}

// effective are the rows a Type would type now: ticked by the user, not reached by an earlier Type, with bytes keel serve gave,
// and a pane that is open, driven and ready. A row ticked whose pane has since closed or become a followed one is not among them.
func (g *approveGroup) effective() (out []*groupRow) {
	for _, r := range g.rows {
		if r.checked && !r.attempted && g.rowBlocked(r) == "" {
			out = append(out, r)
		}
	}
	return out
}

// groupJobs is what the group dialog's Type presses: the ticked rows that can be typed, each as a job of its own with its own
// proposal and the bytes its row showed. Nothing is ticked for the user, an unticked row never produces a job, a row already
// attempted never does again, and a row whose pane closed, is followed or is not ready is left out (its row says why). A row
// that needs ⇧ is typed only when ⇧ was held with Type; otherwise nothing at all is typed and the dialog says to hold it.
func (w *workspace) groupJobs(d *approveDialog, shift bool) (jobs []typeJob) {
	g := d.group()
	pick := g.effective()
	if len(pick) == 0 {
		return nil
	}
	if rowsNeedShift(pick) && !shift {
		g.shiftHint = true
		return nil
	}
	g.shiftHint = false
	for _, r := range pick {
		p := g.pane(r.sessionID)
		jobs = append(jobs, typeJob{conn: p.p.conn, command: r.bytes, row: r,
			ref: &keelclient.AgentRef{GroupRunID: g.runID, ProposalID: r.proposalID, Shift: shift}})
	}
	return jobs
}
