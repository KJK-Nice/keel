package native

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Runbooks (docs/ui-redesign.md §16): the page, the cards and the walk. The service is internal/httpapi/agentrunbooks.go;
// the words and rules are in runbookslogic.go.
//
// What this file does and does not do:
//   - Runbooks live in keel serve (keel.memory, in the vault). This client holds them in memory only and writes none to disk.
//     keel serve redacts every text for secrets before it keeps it; the editor shows what it kept and how many values it
//     replaced, and a command the redactor would change is refused by keel serve and the editor shows its reason.
//   - The agent never writes a runbook. One it proposes is a card; the card's Save (saveBookProposal) is the only thing that
//     keeps it, with Agent may pick off, and Dismiss keeps nothing.
//   - A runbook step is an ordinary proposal. Propose step N is an ordinary ask: keel serve makes the proposal for the current
//     step only, rates it, and the card is typed by agent.go's Type path like any other (Approve before type). Nothing here
//     types, builds a proposal or knows a step's bytes; Mark done, Skip and Retry only tell keel serve what the user decided.
//   - A step that holds a redaction placeholder is never typed: its card has no Type button (typeOffer) and keel serve refuses
//     it too.
//   - "Agent may pick" is off unless the user turns it on.

// ---- a runbook the agent proposes ----

// agentBook is a runbook proposal on the thread: what keel serve holds, and what the card is doing.
type agentBook struct {
	b    keelclient.RunbookProposal
	busy bool
	err  string
}

func (a *agentPanel) registerBook(b keelclient.RunbookProposal) { a.books[b.ID] = &agentBook{b: b} }

// resetMemory forgets the notes, runbooks and the run of the conversation that was left.
func (a *agentPanel) resetMemory() {
	a.notes, a.books, a.runs = map[string]*agentNote{}, map[string]*agentBook{}, runUse{}
	a.nbProps, a.attached, a.nbPicker = map[string]*agentNbProp{}, nil, ""
}

// saveBookProposal is the card's Save (or Save anyway): the one thing that keeps a runbook the agent proposed, with Agent
// may pick off. A card that is busy ignores a second press.
func (w *workspace) saveBookProposal(id string) {
	a := w.agent
	rec := a.books[id]
	if rec == nil || rec.busy || rec.b.Expired || rec.b.Status != "open" {
		return
	}
	conv := a.conv
	rec.busy, rec.err = true, ""
	call(w, func(c context.Context) (keelclient.SavedRunbook, error) { return w.be.SaveProposedRunbook(c, conv, id) }, func(res keelclient.SavedRunbook, err error) {
		rec.busy = false
		if a.conv != conv {
			return
		}
		if err != nil {
			rec.err = hostError(err)
			return
		}
		if res.Proposal != nil {
			rec.b = *res.Proposal
		} else {
			rec.b.Status, rec.b.RunbookID = "saved", res.Runbook.ID
		}
		if t := savedRedactedToast(res.Redacted); t != "" {
			w.say(t)
		}
		w.reloadRunbooksPage()
	})
}

func (w *workspace) dismissBookProposal(id string) { w.bookProposalAction(id, false) }
func (w *workspace) undoBookProposal(id string)    { w.bookProposalAction(id, true) }

func (w *workspace) bookProposalAction(id string, undo bool) {
	a := w.agent
	rec := a.books[id]
	if rec == nil || rec.busy || rec.b.Expired {
		return
	}
	conv := a.conv
	rec.busy, rec.err = true, ""
	call(w, func(c context.Context) (keelclient.RunbookProposal, error) {
		if undo {
			return w.be.UndoProposedRunbook(c, conv, id)
		}
		return w.be.DismissProposedRunbook(c, conv, id)
	}, func(b keelclient.RunbookProposal, err error) {
		rec.busy = false
		if a.conv != conv {
			return
		}
		if err != nil {
			rec.err = hostError(err)
			return
		}
		rec.b = b
		if undo {
			w.reloadRunbooksPage()
		}
	})
}

// ---- a runbook in use ----

// runUse is the runbook in use in the conversation: keel serve owns the run, this
// shows it and tells keel serve what the user decides.
type runUse struct {
	run   *keelclient.ChatRunbook
	none  bool // the user's "Don't use": the agent is not offered runbooks again here
	names map[string]string

	busy     bool
	err      string
	choosing bool
	others   []runOther // nil while the list is read
	listed   bool

	pollLeft  int  // reads left for a typed step that waits for the exit status its shell reports (about 10 minutes)
	pollForce bool // one read after a step was typed
	polling   bool
}

// runOther is a row of "Use a different runbook".
type runOther struct {
	id, name, when string
	ok             bool
	reason         string
}

const (
	runPollReads = 400 // about 10 minutes at one read every 1.5 s
	runEvery     = 1500 * time.Millisecond
)

// runOnLabel is a proposal card's "Run on", or "Step 2 · Run on" for the step of a runbook .
func runOnLabel(p keelclient.Proposal) string {
	if p.Runbook != nil && p.Runbook.Step > 0 {
		return "Step " + strconv.Itoa(p.Runbook.Step) + " · Run on"
	}
	return "Run on"
}

// setRun makes run the one in use, and puts its card under "Agent" of the turn that started it: once per run.
func (w *workspace) setRun(run *keelclient.ChatRunbook, turn *agentTurn) {
	u := &w.agent.runs
	u.run = run
	if run == nil {
		return
	}
	if u.names == nil {
		u.names = map[string]string{}
	}
	u.names[run.ID] = run.Name
	if turn != nil {
		placed := false
		for _, t := range w.agent.turns {
			placed = placed || t.runID == run.ID
		}
		if !placed {
			turn.runID = run.ID
		}
	}
	if waitingForShell(run) && u.pollLeft <= 0 {
		u.pollLeft = runPollReads
	}
}

// showSavedRun is a resumed conversation's run: its card sits under the agent turn that started it, else the last one.
func (w *workspace) showSavedRun(t keelclient.Transcript) {
	a := w.agent
	a.runs.none = t.NoRunbook
	if t.Runbook == nil {
		return
	}
	var asked []*agentTurn
	for _, turn := range a.turns {
		if !turn.again {
			asked = append(asked, turn)
		}
	}
	var at *agentTurn
	switch n := t.Runbook.Turn; {
	case n >= 1 && n <= len(asked):
		at = asked[n-1]
	case len(asked) > 0:
		at = asked[len(asked)-1]
	}
	w.setRun(t.Runbook, at)
}

// runStepTyped is told that a proposal was typed: one made from a runbook step makes the window read the run once, then
// while the step waits for its result.
func (w *workspace) runStepTyped(rec *agentProp) {
	if rec.p.Runbook == nil {
		return
	}
	u := &w.agent.runs
	u.pollLeft, u.pollForce = runPollReads, true
}

// runTick is one tick of the 1.5 s read of the run in use: it reads only while a typed step
// waits for its result, or once after a step was typed, and never while a read is out.
func (w *workspace) runTick() {
	a := w.agent
	u := &a.runs
	if w.closed || u.polling || u.pollLeft <= 0 || (!u.pollForce && !waitingForShell(u.run)) {
		return
	}
	u.pollForce = false
	u.pollLeft--
	conv := a.conv
	callOnce(w, &u.polling, func() bool { return a.conv == conv },
		func(c context.Context) (keelclient.RunState, error) { return w.be.RunbookState(c, conv) },
		func(res keelclient.RunState, err error) {
			if err == nil && res.Runbook != nil { // a locked vault or a restart: the next change tries again
				w.setRun(res.Runbook, nil)
			}
		})
}

// scopeRbPanes are the panes in scope as a runbook's offer sees them.
func (w *workspace) scopeRbPanes() (out []rbPane) {
	for _, p := range w.pageScopePanes() {
		h := p.p.host
		for i := range w.hosts {
			if w.hosts[i].ID == h.ID {
				h = w.hosts[i]
			}
		}
		out = append(out, rbPane{group: h.Group, tags: h.Tags, readable: p.readable})
	}
	return out
}

// proposeRunStep is Propose step N (and the Retry after a step was reset): an ordinary ask. keel serve makes the proposal for
// the current step, and only that one.
func (w *workspace) proposeRunStep() {
	a := w.agent
	act := nextAction(a.runs.run, func(pid string) bool { r := a.props[pid]; return r != nil && !r.p.Expired })
	if act.kind != "propose" && act.kind != "failed" {
		return
	}
	readable := false
	for _, p := range w.scopeRbPanes() {
		readable = readable || p.readable
	}
	if !readable {
		a.runs.err = "Put a pane in scope first: a step is proposed only for a pane the agent reads."
		return
	}
	a.runs.err = ""
	w.agentAsk(continueQuestion(act.step))
}

// runStepAction is Mark done, Confirm done, Skip and Retry on the current step: keel serve refuses any other step.
func (w *workspace) runStepAction(stepID, action string) {
	a := w.agent
	u := &a.runs
	if u.busy {
		return
	}
	conv := a.conv
	u.busy, u.err = true, ""
	call(w, func(c context.Context) (keelclient.RunState, error) { return w.be.StepAction(c, conv, stepID, action) }, func(res keelclient.RunState, err error) {
		u.busy = false
		if a.conv != conv {
			return
		}
		if err != nil {
			u.err = hostError(err)
			return
		}
		w.setRun(res.Runbook, nil)
		if action == "retry" {
			w.proposeRunStep()
		}
	})
}

// stopRun is Don't use: the agent is not offered runbooks again in this conversation until the user starts one.
func (w *workspace) stopRun() {
	w.runCall(w.be.StopRunbook, func(res keelclient.RunState) {
		u := &w.agent.runs
		u.none, u.choosing, u.err = true, false, ""
		w.setRun(res.Runbook, nil)
	})
}

// moveRun is Move the run to another host: the user's choice only.
func (w *workspace) moveRun() {
	w.runCall(w.be.MoveRunbook, func(res keelclient.RunState) {
		w.agent.runs.err = ""
		w.setRun(res.Runbook, nil)
	})
}

// runCall is a call on the conversation's run that takes no body (Don't use, Move the run).
func (w *workspace) runCall(do func(context.Context, string) (keelclient.RunState, error), ok func(keelclient.RunState)) {
	a := w.agent
	u := &a.runs
	conv := a.conv
	u.busy = true
	call(w, func(c context.Context) (keelclient.RunState, error) { return do(c, conv) }, func(res keelclient.RunState, err error) {
		u.busy = false
		if a.conv != conv {
			return
		}
		if err != nil {
			u.err = hostError(err)
			return
		}
		ok(res)
	})
}

// useRunbook starts a runbook in the window's conversation (the Runbooks page's Use in agent, and a choice in "Use a different
// runbook"): an ask with its id, never a type. The settings are read first, so a panel opened just now asks with its panes in
// scope.
func (w *workspace) useRunbook(id, name string) {
	a := w.agent
	if w.joinedActive() {
		w.say(msgAgentJoined)
		return
	}
	a.runs.choosing, a.runs.err = false, ""
	if !w.prefs.AgentPanel {
		w.setAgentOpen(true)
	}
	ask := func() {
		w.agentSync()
		w.agentAskRunbook(startQuestion(orElse(name, "this runbook")), id)
	}
	if a.settings != nil {
		ask()
		return
	}
	call(w, w.be.AgentSettings, func(s keelclient.AgentSettings, err error) {
		if err == nil {
			w.takeAgentSettings(s)
		}
		ask()
	})
}

// toggleChoose is Use a different runbook: it lists the runbooks, with why one is not offered.
func (w *workspace) toggleChoose() {
	a := w.agent
	u := &a.runs
	u.choosing = !u.choosing
	u.others, u.listed = nil, false
	if !u.choosing {
		return
	}
	conv := a.conv
	panes := w.scopeRbPanes()
	callIf(w, func() bool { return a.conv == conv && u.choosing }, w.be.Runbooks, func(l keelclient.RunbookList, err error) {
		u.listed = true
		u.others = []runOther{}
		if err != nil {
			u.err = hostError(err)
			return
		}
		for _, rb := range l.Runbooks {
			if u.run != nil && rb.ID == u.run.RunbookID && !u.none {
				continue
			}
			ok, why := runbookOfferFor(rb, panes)
			u.others = append(u.others, runOther{id: rb.ID, name: rb.Name, when: rb.When, ok: ok, reason: why})
		}
	})
}

// ---- the Runbooks page ----

// runbooksPage is the state of the Runbooks tab: the list, and the one editor.
type runbooksPage struct {
	filter  string
	filterH ui.Handle

	list       []keelclient.Runbook
	unreadable bool
	err        string
	loaded     bool

	listState ui.ListState // the list's keyboard choice (Up, Down, Home, End), MyGo's ui.List
	choice    int          // the row of listState.Selected

	selected string // the saved runbook open in the editor
	ed       *rbEditor
	saving   bool

	menuNB    bool // From a notebook…: the list is the window's notebooks
	menu      bool // From a conversation…
	menuItems []keelclient.ConversationSummary
	menuEmpty string
	menuLoad  bool
}

func (p *runbooksPage) load(w *workspace)                { w.loadRunbooks() }
func (p *runbooksPage) view(w *workspace, c *ui.Context) { w.runbooksView(c, p) }
func (p *runbooksPage) newItem(w *workspace)             { w.newRunbook() }

// rbStepState is a step in the editor: the fields the user edits (the group-run fields of §17 too: a role, One host only and
// captures), and what is only carried (the rating).
type rbStepState struct {
	rbEditStep
	risk                      string
	titleH, descH, cmdH, expH ui.Handle
	roleH                     ui.Handle
	capH                      [][2]ui.Handle // the name and pattern fields of each capture
}

// rbEditor is the editor beside the list.
type rbEditor struct {
	id           string // "" for a new runbook or a draft
	name, when   string
	hosts        []string
	hostInput    string
	agentMayPick bool
	source       string
	sourceRef    string
	draftID      string
	fromPane     bool
	fromPrivate  bool
	createdAt    time.Time
	lastUsedAt   *time.Time
	lastUsedOn   string
	steps        []*rbStepState

	orig   string // the input as it was loaded: the editor is dirty when it differs
	forced bool   // a draft is dirty from the start
	err    string

	nameH, whenH, hostH  ui.Handle
	focusName, focusHost bool
	focusNew             int // 1-based step whose title takes the keyboard (a step just added); 0 for none
}

func (e *rbEditor) edit() rbEdit {
	ed := rbEdit{name: e.name, when: e.when, hosts: e.hosts, agentMayPick: e.agentMayPick, draftID: e.draftID}
	for _, s := range e.steps {
		ed.steps = append(ed.steps, s.rbEditStep)
	}
	return ed
}

func (e *rbEditor) input() keelclient.RunbookInput { return toRunbookInput(e.edit()) }

func (e *rbEditor) sig() string {
	b, _ := json.Marshal(e.input())
	return string(b)
}

func (e *rbEditor) dirty() bool { return e.forced || e.sig() != e.orig }

// asRunbook is the editor as a runbook, for the lines that describe it (source, last used, steps).
func (e *rbEditor) asRunbook() keelclient.Runbook {
	rb := keelclient.Runbook{ID: e.id, Name: e.name, Source: e.source, SourceRef: e.sourceRef, FromPane: e.fromPane, AgentMayPick: e.agentMayPick,
		CreatedAt: e.createdAt, LastUsedAt: e.lastUsedAt, LastUsedOn: e.lastUsedOn}
	for _, s := range e.steps {
		rb.Steps = append(rb.Steps, keelclient.RunbookStep{Title: s.title, Command: s.command, Risk: s.risk})
	}
	return rb
}

func newRbStep(s keelclient.RunbookStep) *rbStepState {
	return &rbStepState{rbEditStep: rbEditStep{id: s.ID, title: s.Title, description: s.Description, command: s.Command, expect: s.Expect,
		role: s.Role, oneHost: s.OneHost, captures: slices.Clone(s.Captures)}, risk: s.Risk}
}

func editorFromRunbook(rb keelclient.Runbook) *rbEditor {
	e := &rbEditor{id: rb.ID, name: rb.Name, when: rb.When, hosts: slices.Clone(rb.Hosts), agentMayPick: rb.AgentMayPick, source: orElse(rb.Source, "user"),
		sourceRef: rb.SourceRef, fromPane: rb.FromPane, createdAt: rb.CreatedAt, lastUsedAt: rb.LastUsedAt, lastUsedOn: rb.LastUsedOn}
	for _, s := range rb.Steps {
		e.steps = append(e.steps, newRbStep(s))
	}
	e.orig = e.sig()
	return e
}

// editorFromDraft is an unsaved draft keel serve made from a conversation: dirty from the start, with Agent may pick off.
func editorFromDraft(d keelclient.RunbookDraft) *rbEditor {
	e := editorFromRunbook(keelclient.Runbook{Name: d.Name, When: d.When, Hosts: d.Hosts, Steps: d.Steps, Source: d.Source, SourceRef: d.SourceRef})
	e.draftID, e.fromPrivate, e.forced = d.DraftID, d.FromPrivate, true
	return e
}

func (w *workspace) runbooksPage() *runbooksPage {
	return pageAs[*runbooksPage](w.pageTab(pageRunbooks))
}

// loadRunbooks reads the list (GET /api/agent/runbooks). The editor is read again from it unless the user has changed it.
func (w *workspace) loadRunbooks() {
	p := w.runbooksPage()
	if p == nil {
		return
	}
	callIf(w, func() bool { return w.runbooksPage() == p }, w.be.Runbooks, func(l keelclient.RunbookList, err error) {
		p.loaded = true
		var se *keelclient.StatusError
		switch {
		case errors.As(err, &se) && se.Code == 423:
			p.list, p.err = nil, "Unlock the vault to see runbooks."
			return
		case err != nil:
			p.list, p.err = nil, hostError(err)
			return
		}
		p.list, p.unreadable, p.err = l.Runbooks, l.Unreadable, ""
		if p.selected != "" && p.ed != nil && !p.ed.dirty() {
			if rb, ok := p.find(p.selected); ok {
				p.ed = editorFromRunbook(rb)
			} else {
				p.closeEditor()
			}
		}
	})
}

// reloadRunbooksPage reads the list again when the page is open (a runbook was saved or undone from a card).
func (w *workspace) reloadRunbooksPage() {
	if w.runbooksPage() != nil {
		w.loadRunbooks()
	}
}

func (p *runbooksPage) find(id string) (keelclient.Runbook, bool) {
	for _, rb := range p.list {
		if rb.ID == id {
			return rb, true
		}
	}
	return keelclient.Runbook{}, false
}

func (p *runbooksPage) closeEditor() { p.ed, p.selected = nil, "" }

// leaveEditor runs then at once, or after the user agrees to drop what they changed.
func (w *workspace) leaveEditor(p *runbooksPage, then func()) {
	if p.ed == nil || !p.ed.dirty() {
		then()
		return
	}
	w.askConfirm(&confirmDialog{title: "Discard changes?", message: "Your changes to this runbook are not saved.", action: "Discard", run: then})
}

func (w *workspace) newRunbook() {
	p := w.runbooksPage()
	if p == nil {
		return
	}
	w.leaveEditor(p, func() {
		p.selected, p.ed = "", editorFromRunbook(keelclient.Runbook{Source: "user", AgentMayPick: true, Steps: []keelclient.RunbookStep{{}}})
		p.ed.focusName = true
	})
}

func (w *workspace) selectRunbook(id string) {
	p := w.runbooksPage()
	if p == nil || id == p.selected {
		return
	}
	w.leaveEditor(p, func() {
		if rb, ok := p.find(id); ok {
			p.selected, p.ed = rb.ID, editorFromRunbook(rb)
		}
	})
}

// saveRunbook is Save (⌘S): POST or PUT the editor; the editor then shows what keel serve kept.
func (w *workspace) saveRunbook() {
	p := w.runbooksPage()
	if p == nil || p.ed == nil || p.saving {
		return
	}
	e := p.ed
	in := e.input()
	if problem := checkRunbookInput(in); problem != "" {
		e.err = problem
		return
	}
	p.saving, e.err = true, ""
	call(w, func(c context.Context) (keelclient.SavedRunbook, error) { return w.be.SaveRunbook(c, e.id, in) }, func(res keelclient.SavedRunbook, err error) {
		p.saving = false
		if err != nil {
			e.err = hostError(err)
			return
		}
		rb := res.Runbook
		p.list = append(slices.DeleteFunc(slices.Clone(p.list), func(b keelclient.Runbook) bool { return b.ID == rb.ID }), rb)
		slices.SortFunc(p.list, func(a, b keelclient.Runbook) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		p.selected, p.ed = rb.ID, editorFromRunbook(rb)
		if t := savedRedactedToast(res.Redacted); t != "" {
			w.say(t)
		} else {
			w.say("Runbook saved")
		}
	})
}

// askDeleteRunbook is Delete (or Discard on an unsaved one): Delete asks first.
func (w *workspace) askDeleteRunbook() {
	p := w.runbooksPage()
	if p == nil || p.ed == nil {
		return
	}
	e := p.ed
	if e.id == "" {
		p.closeEditor()
		return
	}
	w.askConfirm(&confirmDialog{title: "Delete runbook?", message: "“" + e.name + "” is deleted from the vault. Conversations that used it keep their copy of its steps.", action: "Delete",
		run: func() { w.deleteRunbook(e.id) }})
}

func (w *workspace) deleteRunbook(id string) {
	callErr(w, func(c context.Context) error { return w.be.DeleteRunbook(c, id) }, func(err error) {
		if err != nil {
			w.warn(hostError(err))
			return
		}
		if p := w.runbooksPage(); p != nil {
			p.list = slices.DeleteFunc(slices.Clone(p.list), func(b keelclient.Runbook) bool { return b.ID == id })
			p.closeEditor()
		}
		w.say("Runbook deleted")
	})
}

// setRunbookPick is the list's "Agent may pick" switch and the review banner's Enable: only the user's press turns it on.
func (w *workspace) setRunbookPick(id string, on bool) {
	call(w, func(c context.Context) (keelclient.Runbook, error) { return w.be.SetRunbookPick(c, id, on) }, func(rb keelclient.Runbook, err error) {
		p := w.runbooksPage()
		if err != nil {
			w.warn(hostError(err))
			if p != nil {
				w.loadRunbooks()
			}
			return
		}
		if p == nil {
			return
		}
		for i := range p.list {
			if p.list[i].ID == rb.ID {
				p.list[i] = rb
			}
		}
		if p.ed != nil && p.ed.id == rb.ID {
			if p.ed.dirty() {
				p.ed.agentMayPick = rb.AgentMayPick
			} else {
				p.ed = editorFromRunbook(rb)
			}
		}
	})
}

// openConversationMenu is From a conversation…: the saved conversations to make a draft from.
func (w *workspace) openConversationMenu() {
	p := w.runbooksPage()
	if p == nil {
		return
	}
	if p.menu {
		p.menu = false
		return
	}
	p.menuNB = false // one source menu at a time
	p.menu, p.menuLoad, p.menuItems, p.menuEmpty = true, true, nil, ""
	callIf(w, func() bool { return p.menu }, w.be.Conversations, func(l keelclient.ConversationList, err error) {
		p.menuLoad = false
		switch {
		case err != nil:
			p.menuEmpty = hostError(err)
		case l.HistoryDays == 0:
			p.menuEmpty = "History is off, so no conversation is saved."
		default:
			p.menuItems, p.menuEmpty = l.Sessions, "No saved conversations."
		}
	})
}

// openNotebookMenu is From a notebook…: the notebooks to make a draft from.
func (w *workspace) openNotebookMenu() {
	p := w.runbooksPage()
	if p == nil {
		return
	}
	p.menu = false
	p.menuNB = !p.menuNB
}

// draftRunbook makes an unsaved draft from a saved conversation: keel serve turns its proposals into steps. Nothing is kept
// until the user presses Save, and the draft starts with Agent may pick off.
func (w *workspace) draftRunbook(conversationID string) {
	p := w.runbooksPage()
	if p == nil {
		return
	}
	p.menu, p.menuNB = false, false
	w.leaveEditor(p, func() {
		call(w, func(c context.Context) (keelclient.RunbookDraft, error) { return w.be.DraftRunbook(c, conversationID) }, func(d keelclient.RunbookDraft, err error) {
			w.showDraft(p, d, err)
		})
	})
}

// draftRunbookOfNotebook makes the same draft from a notebook: its headings become steps and its shell blocks their
// commands. One request with the notebook's id; keel serve reads the text it stores. Typing in the notebook is saved first,
// so the draft is of what the user sees. Nothing is kept until the user presses Save in the editor.
func (w *workspace) draftRunbookOfNotebook(notebookID string) {
	p := w.runbooksPage()
	if p == nil {
		return
	}
	p.menu, p.menuNB = false, false
	w.leaveEditor(p, func() {
		w.nbFlushID(notebookID, func() {
			call(w, func(c context.Context) (keelclient.RunbookDraft, error) {
				return w.be.DraftRunbookOfNotebook(c, notebookID)
			}, func(d keelclient.RunbookDraft, err error) {
				w.showDraft(p, d, err)
			})
		})
	})
}

// showDraft opens a draft in the editor, or says why there is none (a 422 is "nothing to make steps from").
func (w *workspace) showDraft(p *runbooksPage, d keelclient.RunbookDraft, err error) {
	if w.closed {
		return
	}
	var se *keelclient.StatusError
	switch {
	case errors.As(err, &se) && se.Code == 422:
		w.warn("Nothing to make steps from: no headings or shell blocks, or no proposals.")
		return
	case err != nil:
		w.warn(hostError(err))
		return
	}
	p.selected, p.ed = "", editorFromDraft(d)
	p.ed.agentMayPick = false
	p.ed.focusName = true
}

// useEditedRunbook is Use in agent: it starts the saved runbook in the agent panel.
func (w *workspace) useEditedRunbook() {
	p := w.runbooksPage()
	if p == nil || p.ed == nil || p.ed.id == "" {
		return
	}
	w.useRunbook(p.ed.id, p.ed.name)
}

// rbKeys is ⌘S on the page.
func (w *workspace) rbKeys(ev ui.InputEvent) {
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyS && ev.Mods == ui.Cmd {
		w.act(w.saveRunbook)
	}
}

// openRunbooksPage is the palette's Runbooks.
func (w *workspace) openRunbooksPage() { w.openPage(pageRunbooks) }
