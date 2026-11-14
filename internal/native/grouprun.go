package native

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// ctxT is the context a call about the run is made with.
type ctxT = context.Context

// The group run page (docs/ui-redesign.md §17, board GroupRunbook; cmd/keel/web/js/ui/grouprun.js, internal/ui/groupspage.templ):
// one runbook walked across a folder's hosts, rolling or parallel. keel serve holds the run (at most 8, in its memory only) and
// makes every proposal; this window shows it, asks keel serve every 1.5 s to read the shells' results and to make the current
// step's due proposals for the panes this window has open and drives, and types — never by itself. Typing is the user's:
// "Type into N panes" opens the group approve dialog (approve.go, groupapproveview.go), which shows the exact bytes and one
// row per host that the user ticks; each ticked host is then typed by the dialog's Type action (approve.go), which is the only
// writer, once per ticked host, each with its own proposal and its own server nonce.
//
// The run's JSON carries names, states and lengths, never a variable's or a captured value. The one route that returns values
// is the fill, which the window asks for only after the user pressed "Type into N panes", and whose bytes it shows in the
// dialog and sends unchanged. Nothing here writes a value to disk, a log or a toast.
//
// Not built: one click typing a read-only one-line step into every pane with no dialog (§6 rule 13, "for review, not yet
// accepted" in the spec): every group step goes through the dialog, which is the stricter path and always available.

// groupRunEvery is how often the page asks keel serve to propose and to read results (grouprun.js POLL_MS).
const groupRunEvery = 1500 * time.Millisecond

// The words of the page.
const (
	msgRunEnded     = "keel serve no longer holds this run (it ended with keel serve). Start a runbook from a group's page: Run a runbook on this group."
	stopTitle       = "Stop this runbook?"
	stopRunMessage  = "Nothing more is proposed in this run. What was typed stays typed, and Logs keep it."
	stopAction      = "Stop runbook"
	fillNote        = "Keel fills these in only when you press Type, and shows you the exact bytes first. Logs record how many values were filled in, never a value. Copy leaves the {{names}} in."
	runSafetyLine   = "Each step is one proposal per host. Nothing runs until you press Enter in each pane: a step is typed only into the hosts you tick. Each host's result comes from its shell's exit code."
	runReportedLine = "Exit codes are reported by each pane's shell (OSC 133 marks): they are not proof. On a production host a reported exit 0 waits for you to confirm it."
)

// groupRunPage is the state of the Group run tab.
type groupRunPage struct {
	id      string // the run shown: kept in the saved workspace while keel serve holds it
	title   string // "Run · <runbook>"
	run     *keelclient.GroupRun
	err     string
	loaded  bool
	polling bool // a propose is out
	typing  bool // a fill or a Type is under way: no propose meanwhile
	busy    int  // marks, mode changes and stops that are out
	calls   int  // how many have been made: only the newest answer is shown
	gen     int
	width   float32 // the page's width as last drawn: under groupRunNarrow the steps go above the detail
}

// groupRunNarrow is #page-grouprun's container query (keel.css): under this width the steps sit above the detail.
const groupRunNarrow = 760

func (p *groupRunPage) load(w *workspace)                 { w.loadGroupRunPage() }
func (p *groupRunPage) view(w *workspace, c *ui.Context)  { w.groupRunView(c, p) }
func (p *groupRunPage) tabTitle() string                  { return p.title }
func (p *groupRunPage) snapshot(d *savedDoc)              { d.RunID = p.id }
func (p *groupRunPage) restore(_ *workspace, d *savedDoc) { p.id = d.RunID }

// groupRunPageState is the page of the Group run tab, or nil.
func (w *workspace) groupRunPageState() *groupRunPage {
	return pageAs[*groupRunPage](w.pageTab("grouprun"))
}

// openGroupRun opens the run's page, or shows it in the tab that is open: one instance.
func (w *workspace) openGroupRun(run keelclient.GroupRun) {
	if run.ID == "" {
		return
	}
	t := w.pageTab("grouprun")
	if t == nil {
		t = w.addPageTab("grouprun")
	}
	p := pageAs[*groupRunPage](t)
	if p.id != run.ID {
		*p = groupRunPage{}
	}
	p.id, p.title, p.run, p.loaded, p.err = run.ID, "Run · "+run.Name, &run, true, ""
	w.openPage("grouprun")
}

// loadGroupRunPage reads the run the page is on again (the page was selected, or the vault changed): keel serve first reads
// the results of typed steps from their panes' shell marks. A run keel serve no longer holds is said so.
func (w *workspace) loadGroupRunPage() {
	p := w.groupRunPageState()
	if p == nil || p.id == "" {
		return
	}
	p.gen++
	gen, id := p.gen, p.id
	callIf(w, func() bool { return w.groupRunPageState() == p && p.gen == gen }, func(c context.Context) (keelclient.GroupRun, error) { return w.be.GroupRun(c, id) },
		func(run keelclient.GroupRun, err error) {
			p.loaded = true
			if err != nil {
				var se *keelclient.StatusError
				if errors.As(err, &se) && se.Code == 404 {
					p.run, p.err = nil, msgRunEnded
				} else {
					p.err = hostError(err)
				}
				return
			}
			p.run, p.err, p.title = &run, "", "Run · "+run.Name
		})
}

// groupPaneOpen reports a pane this window has open for the session and drives: the only kind a proposal is offered for.
func (w *workspace) groupPaneOpen(sessionID string) bool {
	p := w.agentPaneBySession(sessionID)
	return p.drives()
}

// groupPaneIDs are the sessions this window has open and drives, for the propose: keel serve resolves each pane's host itself.
func (w *workspace) groupPaneIDs() (out []string) {
	for _, p := range w.agentPanes() {
		if p.drives() {
			out = append(out, p.sessionID)
		}
	}
	return out
}

// groupRunTick is one tick of the 1.5 s propose: it runs only while a run is active and shown by a tab, never while a call,
// a fill or a Type is out, and never while the approve dialog is open (grouprun.js tick).
func (w *workspace) groupRunTick() {
	p := w.groupRunPageState()
	if w.closed || p == nil || p.run == nil || p.run.Status != "active" || p.typing || p.busy > 0 || w.approve != nil {
		return
	}
	id, panes, seq := p.run.ID, w.groupPaneIDs(), p.calls
	callOnce(w, &p.polling, func() bool { return w.groupRunPageState() == p && p.id == id && p.calls == seq }, // a press since: its answer is newer
		func(c context.Context) (keelclient.GroupRun, error) { return w.be.ProposeGroupStep(c, id, panes) },
		func(run keelclient.GroupRun, err error) { w.groupRunAnswered(p, run, err) })
}

// groupRunAnswered takes keel serve's answer to any call about the run.
func (w *workspace) groupRunAnswered(p *groupRunPage, run keelclient.GroupRun, err error) {
	if err != nil {
		p.err = hostError(err)
		return
	}
	p.run, p.err, p.title = &run, "", "Run · "+run.Name
}

// groupRunCall runs one request about the run (a mode change, a mark, a stop) and takes the answer; keel serve's refusal (409)
// is shown in its words.
func (w *workspace) groupRunCall(p *groupRunPage, do func(c ctxT, id string) (keelclient.GroupRun, error)) {
	if p.run == nil {
		return
	}
	// Like the app's page, a second press while one call is out is a call of its own (keel serve takes them one at a time and
	// refuses what no longer applies); only the newest answer is shown, which holds the earlier ones' effects.
	p.calls++
	seq := p.calls
	p.busy++
	id := p.run.ID
	call(w, func(c context.Context) (keelclient.GroupRun, error) { return do(c, id) }, func(run keelclient.GroupRun, err error) {
		p.busy--
		if w.groupRunPageState() != p || p.id != id || seq != p.calls {
			return
		}
		w.groupRunAnswered(p, run, err)
	})
}

func (w *workspace) setRunMode(p *groupRunPage, mode string) {
	if p.run == nil || p.run.Mode == mode || p.run.ModeLocked {
		return
	}
	w.groupRunCall(p, func(c ctxT, id string) (keelclient.GroupRun, error) { return w.be.SetGroupRunMode(c, id, mode) })
}

// markStep is Retry, Skip, Mark done or Confirm on a host of the current step; with no host, Mark step done / Skip step.
func (w *workspace) markStep(p *groupRunPage, hostID, action string) {
	if p.run == nil || p.run.Current < 0 || p.run.Current >= len(p.run.Steps) {
		return
	}
	step := p.run.Steps[p.run.Current].ID
	w.groupRunCall(p, func(c ctxT, id string) (keelclient.GroupRun, error) {
		return w.be.MarkGroupStep(c, id, step, hostID, action)
	})
}

// askStopRun is Stop runbook: it asks first, with the app's words.
func (w *workspace) askStopRun(p *groupRunPage) {
	w.askConfirm(&hostConfirm{title: stopTitle, message: stopRunMessage, action: stopAction, run: func() {
		w.groupRunCall(p, func(c ctxT, id string) (keelclient.GroupRun, error) { return w.be.StopGroupRun(c, id) })
	}})
}

// openRunHost is "Open <host>": a session on a host that has no pane here, exactly as clicking it in the sidebar. It is the
// user's click; nothing is typed into it until approved.
func (w *workspace) openRunHost(hostID string) {
	if h, ok := hostByID(w.hosts, hostID); ok {
		w.openHost(h)
		return
	}
	w.warn("Host not found")
}

// startGroupRun starts a run of the runbook on this folder, rolling (the app's default), and opens its page. keel serve fixes
// the hosts per step; nothing is typed and no pane is read.
func (w *workspace) startGroupRun(p *groupPage, runbookID string) {
	if p.run.busy {
		return
	}
	p.run.open, p.run.busy = false, true
	id := p.id
	call(w, func(c context.Context) (keelclient.GroupRun, error) {
		return w.be.StartGroupRun(c, runbookID, id, "rolling")
	}, func(run keelclient.GroupRun, err error) {
		p.run.busy = false
		if err != nil {
			w.warn(hostError(err))
			return
		}
		w.openGroupRun(run)
	})
}

// ---- Type into N panes ----

// typeGroupRun is "Type into N panes". It types nothing: it asks keel serve to fill the variables the proposals name (the
// fill writes nothing), then opens the group approve dialog on the exact bytes, nothing ticked unless there is one host.
func (w *workspace) typeGroupRun() {
	p := w.groupRunPageState()
	if p == nil || p.run == nil || p.typing {
		return
	}
	run := p.run
	ready := typeable(run, w.groupPaneOpen)
	if len(ready) == 0 {
		return
	}
	rows := approveRows(ready)
	for _, r := range rows {
		if pn := w.agentPaneBySession(r.sessionID); pn != nil && pn.sub != "" && pn.sub != r.host {
			r.paneLabel = "tab " + pn.sub
		}
	}
	vars := false
	var ids []string
	for _, c := range ready {
		ids = append(ids, c.Proposal.ID)
		vars = vars || len(c.Proposal.Vars) > 0
	}
	open := func() {
		p.typing = true
		w.openApproveGroup(&approveGroup{
			runID: run.ID, rows: rows,
			pane:      w.agentPaneBySession,
			done:      func(typed int, failed []string) { w.groupTypeDone(p, typed, failed) },
			cancelled: func() { p.typing = false },
		})
	}
	if !vars {
		open()
		return
	}
	// A step that names variables: keel serve fills each host's values in now, and the dialog shows those exact bytes (Type is
	// refused for any other bytes).
	p.typing = true
	callIf(w, func() bool { return w.groupRunPageState() == p }, func(c context.Context) ([]keelclient.GroupFill, error) { return w.be.FillGroupStep(c, run.ID, ids) },
		func(fills []keelclient.GroupFill, err error) {
			p.typing = false
			if err != nil {
				w.warn(hostError(err))
				return
			}
			fillRows(rows, fills)
			open()
		})
}

// groupTyped takes the answers of a group Type: each reached row shows what happened to it, in keel serve's words, and is
// final. When every row typed the dialog closes, as the app's does; when one was refused it stays, with that row's reason and
// the others' "Typed.", so the user sees each.
func (w *workspace) groupTyped(d *approveDialog, g *approveGroup, jobs []typeJob, errs []error) {
	typed := 0
	var failed []string
	for i, j := range jobs {
		r := j.row
		r.attempted, r.checked = true, false
		if errs[i] == nil {
			r.typedOK, r.outcome = true, "Typed."
			typed++
			continue
		}
		r.outcome = errs[i].Error()
		failed = append(failed, r.host+": "+orElse(r.outcome, "not typed"))
	}
	for _, r := range g.rows { // a row refused before it was asked (its pane closed) is a failure too
		if r.attempted && !r.typedOK && r.outcome != "" && !contains(failed, r.host+": "+r.outcome) {
			failed = append(failed, r.host+": "+r.outcome)
		}
	}
	if len(failed) == 0 && w.approve == d {
		w.closeApprove()
	}
	if len(failed) > 0 {
		w.warn("Not typed on " + strings.Join(failed, "; "))
	}
	g.done(typed, failed)
}

// groupTypeDone reads the run again after a Type: keel serve first reads the shells' results.
func (w *workspace) groupTypeDone(p *groupRunPage, typed int, failed []string) {
	p.typing = false
	w.loadGroupRunPage()
}
