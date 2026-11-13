package native

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The agent panel (docs/ui-redesign.md §5.4, phase 6b): ask about the panes in scope, read the trace, and type a
// proposal only through Approve before type. The feature is cmd/keel/web/js/ui/agentpanel.js and agentthread.js, its
// words and rules in lib/agent.js (agentlogic.go).
//
// What this file does and does not do:
//   - keel serve reads the panes. An ask sends the question and the session ids of the panes in scope, and nothing else
//     (keelclient.AskRequest has no field for pane text). This client reads no scrollback for the agent.
//   - Nothing here writes to a session. A card's Type into pane goes to typeNow (typenow.go). A read-only one-line proposal
//     keel serve agrees to is typed with no dialog and no Enter, the command alone with the reference to the proposal; any
//     other opens the Approve before type dialog (approve.go) with the proposal's bytes, as keel serve holds them, whose
//     Type action is the one caller of ApprovedInject. keel serve checks the proposal, the bytes, the role and the ⇧ rule
//     again before it writes either, and answers. A refusal is shown and nothing is tried again.
//   - The provider key goes from a masked field to PUT /api/agent/keys/{provider} once; the field is emptied when the key
//     is sent and nothing keeps it.
//
// Saved conversations (agentconv.go), the Agent page (agentpage.go) and Settings · Agent (agentsettings.go) show and keep
// the window's one conversation. Notes and runbooks are notes.go and runbooks.go, notebooks agentnotebooks.go. Not here, and
// in the app: Claude Code's mcp (docs/native.md).

const askTimeout = 160 * time.Second // keel serve's own limit is 150 s

// agentStatus is a card's line after a Type: what happened.
type agentStatus struct {
	ok   bool
	text string
}

// agentProp is a proposal on the thread: what keel serve holds, what the card says about it, and an edit in progress.
type agentProp struct {
	p       keelclient.Proposal
	status  *agentStatus
	editing bool
	edit    string    // the edit field; never typed anywhere, only sent to keel serve by Save command
	editH   ui.Handle // the field in the panel
	editHP  ui.Handle // the same field on the Agent page
	saving  bool

	focusEdit bool
}

// agentTurn is one question and its answer: asked in this window, or read from a saved conversation (saved). A Propose
// again turn (again) has no question: it is only the new card.
type agentTurn struct {
	question string
	at       time.Time // when it was asked; set on a saved turn
	scoped   int       // panes in scope when it was asked
	pending  bool
	stopped  bool
	err      error

	done         bool // the answer is in (an ask that finished, or a saved turn)
	saved, again bool
	trace        []keelclient.TraceStep
	answer, cut  string
	testProvider bool
	props        []string // the proposals under it, by id (agentPanel.props holds them)
	notes        []string // the notes it proposed (agentPanel.notes), shown after the proposals
	books        []string // the runbooks it proposed (agentPanel.books)
	nbs          []string // what the agent did to notebooks, and the deletes it proposed (agentPanel.nbProps), shown last
	runID        string   // the runbook in use that started here: its card sits under "Agent" (agentthread.js placeRun)
}

type agentPanel struct {
	conv string
	// title is the conversation's name from keel serve; "" is "New conversation".
	title string

	scope     []string // session ids
	scopeAuto bool     // the scope follows the setting until the user picks
	picker    bool

	settings      *keelclient.AgentSettings
	keyConfigured *bool // nil until the settings are read
	provider      *keelclient.AgentProvider
	provs         []keelclient.AgentProvider // the providers Settings · Agent draws cards for (agentsettings.go)
	loading       bool
	loadTried     bool // the first read for a panel that was open at start

	turns []*agentTurn
	props map[string]*agentProp

	// Notes and runbooks the agent proposed, and the runbook in use (notes.go, runbooks.go). Everything here is read from keel
	// serve into memory; a proposal is kept only by its card's Save.
	notes map[string]*agentNote
	books map[string]*agentBook
	runs  runUse

	// Notebooks (agentnotebooks.go): the ids the user attached to the next questions (at most two, none private), the cards
	// of what the agent did to notebooks, and which composer has the picker open ("panel" or "page").
	attached []string
	nbProps  map[string]*agentNbProp
	nbPicker string
	nbRowH   map[string]*ui.Handle // the picker's rows, to walk between with ↑ ↓
	nbWalk   string                // the row that takes the keyboard in the next frame

	// The saved conversations (agentconv.go). Everything here is read from keel serve into memory; none of it is
	// written anywhere by this client, and none of it is in keel-native.json.
	list        convListState
	createdAt   time.Time // when the conversation started (the page's header)
	resumedAt   time.Time // set on a resumed conversation: the divider says how long ago it was last used
	resumedSeq  int       // a resume answered after another one began is dropped
	sessOpen    bool      // Recent conversations
	sess        sessUI
	renameID    string // the row (or, on the page, the heading) being renamed; "" for none
	renameText  string
	renameH     ui.Handle
	renameFocus bool
	listLoading bool

	input      string
	inputH     ui.Handle
	focusInput bool
	busy       context.CancelFunc // the ask in flight

	keyField string // the masked field: emptied the moment the key is sent
	keyErr   string
	keyH     ui.Handle
	focusKey bool
	keyBusy  bool

	scroll ui.ScrollState
	stick  bool // scroll to the end in the next frame

	sash sashState
}

func newAgentPanel() *agentPanel {
	return &agentPanel{conv: newConversationID(), scopeAuto: true, props: map[string]*agentProp{}, notes: map[string]*agentNote{}, books: map[string]*agentBook{}, nbProps: map[string]*agentNbProp{}}
}

const (
	msgAgentJoined     = "The agent is off in joined sessions" // term.js toggleAgentPanel
	msgAgentNotOpen    = "That pane is not open in this window."
	msgAgentFollowing  = "You follow this pane. The agent cannot type into it."
	titleNewConv       = "New conversation"
	savedInVault       = "Saved in the vault"
	agentEmptyTitle    = "Ask about your sessions"
	agentInputHint     = "Ask about these sessions…"
	agentComposerHint  = "@ add a pane · ↵ send"
	agentFootNote      = "Nothing reaches a pane until you press Type. The agent cannot type into panes you follow, and never sees the vault."
	agentNoModelTitle  = "No model is connected."
	agentNoModelDetail = "Add a key to ask the agent."
)

// shiftWord is how the app names the key in its sentences.
func shiftWord() string {
	if isMac() {
		return "⇧"
	}
	return "Shift"
}

// agentKeys is the toggle's key cap: ⌘I on macOS, Ctrl+Shift+I elsewhere (Ctrl+I is Tab in a terminal).
func agentKeys() string { return commandKeyLabel("agent.toggle") }

// The panel's width (ui/agentpanel.js "width"): at least --size-agent and at most half the window, kept as agentWidth in
// keel-native.json; 0 is the default. The sash on its left edge changes it (agentview.go).

// clampAgentWidth is applyWidth: a width of 0 is the default, else between size-agent and half the window (never below
// size-agent, whatever the window).
func clampAgentWidth(width, windowW float32) float32 {
	if width <= 0 {
		return sizeAgent
	}
	hi := max(sizeAgent, float32(int(windowW*0.5)))
	return min(hi, max(sizeAgent, float32(int(width+0.5))))
}

// setAgentWidth keeps a new width (0 puts the default back) in keel-native.json.
func (w *workspace) setAgentWidth(width float32) {
	w.prefs.AgentWidth = width
	w.savePref("the agent panel width", keyAgentWidth, width)
}

// ---- the panes in scope ----

// agentPanes lists the panes of this window as the panel sees them: the terminal panes with a session, in every tab but
// a New tab or a page (term.js agentPanes). "Focused" and "this tab" mean the tab shown, or the session tab shown last
// when a page is.
func (w *workspace) agentPanes() (out []agentPane) {
	sessionTab := w.tab()
	if sessionTab == nil || sessionTab.isNew() || sessionTab.isPage() {
		sessionTab = w.lastSession
	}
	for _, t := range w.tabs {
		if t.isNew() || t.isPage() {
			continue
		}
		for _, id := range t.root.Leaves() {
			p := t.panes[id]
			if p == nil || p.sessionID() == "" {
				continue
			}
			var host *keelclient.Host // the host as it is now: reading may have been turned off since the pane opened
			for i := range w.hosts {
				if w.hosts[i].ID == p.host.ID {
					host = &w.hosts[i]
				}
			}
			label := p.title()
			if host != nil {
				label = host.Name
			}
			out = append(out, agentPane{
				sessionID: p.sessionID(), label: label, sub: t.name(),
				focused:     t == sessionTab && t.focused == id,
				inActiveTab: t == sessionTab,
				// The server decides; this only shapes the picker. A host it does not know is not readable there either.
				readable:   host != nil && (host.AgentReadable == nil || *host.AgentReadable),
				production: host != nil && host.Production != nil && *host.Production,
				driving:    p.conn.Snapshot().Role == "driver",
				remote:     p.remote || t.remote,
				p:          p, t: t,
			})
		}
	}
	return out
}

func (w *workspace) agentPaneBySession(id string) *agentPane {
	for _, p := range w.agentPanes() {
		if p.sessionID == id {
			return &p
		}
	}
	return nil
}

// agentSync keeps the scope true to the panes that are open and readable, every frame: it follows the setting until the
// user picks, then only drops what closed or can no longer be read (agentpanel.js refresh).
func (w *workspace) agentSync() {
	a := w.agent
	a.attached = reconcileAttached(a.attached, w.notebooks) // a notebook deleted or made private is no longer attached
	all := w.agentPanes()
	if a.scopeAuto && a.settings != nil {
		a.scope = defaultScope(a.settings.Settings.DefaultScope, all)
	} else {
		a.scope = reconcileScope(a.scope, all)
	}
}

func (w *workspace) setAgentScope(ids []string) {
	w.agent.scopeAuto = false
	w.agent.scope = reconcileScope(ids, w.agentPanes())
}

// ---- open, close, settings ----

// toggleAgent is ⌘I, the Agent button and the palette's Toggle agent panel. A joined tab has no panel: it says why.
func (w *workspace) toggleAgent() {
	if w.joinedActive() {
		w.say(msgAgentJoined)
		return
	}
	w.setAgentOpen(!w.prefs.AgentPanel)
}

// setAgentOpen shows or hides the panel and keeps the choice in keel-native.json.
func (w *workspace) setAgentOpen(open bool) {
	a := w.agent
	w.prefs.AgentPanel = open
	w.savePref("the agent panel", keyAgentPanel, open)
	if open {
		a.focusInput = true
		w.loadAgent()
		return
	}
	a.picker, a.sessOpen = false, false
	if t := w.tab(); t != nil && t.panes[t.focused] != nil { // the keyboard goes back to the pane
		w.wantFocus = t.focused
	}
}

// loadAgent reads the agent settings (the default scope, whether a key is saved) and, with no key, the provider's words
// for the Connect card.
func (w *workspace) loadAgent() {
	a := w.agent
	if a.loading {
		return
	}
	a.loading = true
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		s, err := w.be.AgentSettings(c)
		var prov *keelclient.AgentProvider
		if err == nil && !s.KeyConfigured() {
			if list, lerr := w.be.AgentProviders(c); lerr == nil {
				for i := range list {
					if list[i].ID == s.Settings.Provider {
						prov = &list[i]
					}
				}
			}
		}
		w.post(func() {
			a.loading = false
			if w.closed {
				return
			}
			var se *keelclient.StatusError
			switch {
			case errors.As(err, &se) && se.Code == 423: // the vault is locked: the Unlock screen says so
			case err != nil:
				a.keyConfigured = nil
			default:
				a.settings = &s
				k := s.KeyConfigured()
				a.keyConfigured = &k
				a.provider = prov
			}
		})
	})
}

// ---- asking ----

// agentAsk sends a question about the panes in scope. It returns false when nothing was sent.
func (w *workspace) agentAsk(question string) bool { return w.agentAskRunbook(question, "") }

// agentAskRunbook is agentAsk that starts the runbook runbookID in this conversation (the user picked it): keel serve then
// proposes its current step itself. Starting a runbook is an ask, never a type.
func (w *workspace) agentAskRunbook(question, runbookID string) bool {
	a := w.agent
	q := strings.TrimSpace(question)
	if q == "" || a.busy != nil {
		return false
	}
	if a.keyConfigured != nil && !*a.keyConfigured {
		a.focusInput, a.focusKey = false, true
		return false // the Connect card is what the thread shows
	}
	a.picker, a.sessOpen, a.nbPicker = false, false, ""
	w.agentSync()
	// A resumed conversation carries on under its own id: that is all a continued ask changes (agentpanel.js ask).
	panes := append([]string{}, a.scope...)
	turn := &agentTurn{question: q, at: time.Now(), scoped: len(panes), pending: true}
	a.turns = append(a.turns, turn)
	if a.createdAt.IsZero() {
		a.createdAt = turn.at
	}
	a.stick = true
	c, cancel := context.WithTimeout(context.Background(), askTimeout)
	a.busy = cancel
	conv := a.conv
	// Typing in a notebook saves first, so the agent reads it; the ask then names the notebooks still not saved (§18).
	w.nbFlushAll(func() {
		w.sendAsk(c, cancel, turn, keelclient.AskRequest{ConversationID: conv, Question: q, Panes: panes, RunbookID: runbookID})
	})
	return true
}

// sendAsk sends the ask once the notebooks are saved. It names the attached notebooks by id (never their text) and the ones
// with typing not saved yet.
func (w *workspace) sendAsk(c context.Context, cancel context.CancelFunc, turn *agentTurn, req keelclient.AskRequest) {
	a := w.agent
	conv, runbookID := req.ConversationID, req.RunbookID
	if w.closed || a.conv != conv { // a new conversation started while the notebooks saved
		cancel()
		return
	}
	a.attached = reconcileAttached(a.attached, w.notebooks)
	req.Notebooks, req.Editing = slices.Clone(a.attached), w.editingNotebooks()
	w.work(func() {
		res, err := w.be.Ask(c, req)
		cancel()
		w.post(func() {
			if w.closed || a.conv != conv { // a new conversation started meanwhile: this answer belongs to the old one
				return
			}
			a.busy = nil
			turn.pending = false
			a.stick = true
			switch {
			case errors.Is(err, context.Canceled):
				turn.stopped = true
			case err != nil:
				turn.err = err
				var se *keelclient.StatusError
				if errors.As(err, &se) && se.Code == 412 {
					k := false
					a.keyConfigured, a.provider = &k, nil
					w.loadAgent()
				}
			default:
				turn.done, turn.trace, turn.answer, turn.cut, turn.testProvider = true, res.Trace, res.Answer, res.Cut, res.TestProvider
				a.list.saveError = res.SaveError
				if res.Title != "" {
					a.title = res.Title
				}
				for _, p := range res.Proposals {
					a.props[p.ID] = &agentProp{p: p, status: typedStatus(p)}
					turn.props = append(turn.props, p.ID)
				}
				for _, n := range res.Notes {
					a.registerNote(n)
					turn.notes = append(turn.notes, n.ID)
				}
				for _, b := range res.Runbooks {
					a.registerBook(b)
					turn.books = append(turn.books, b.ID)
				}
				for _, nb := range res.Notebooks {
					a.registerNbProp(nb)
					turn.nbs = append(turn.nbs, nb.ID)
					if nb.Record != nil { // a notebook the agent created or edited: the window shows it now
						w.notebookChanged(*nb.Record)
					}
				}
				if runbookID != "" {
					a.runs.none = false
				}
				if res.Runbook != nil {
					w.setRun(res.Runbook, turn)
				}
				if w.agentPageShown() { // the answer saved the conversation: a list on screen has it
					w.loadConversations()
				}
			}
		})
	})
}

// typedStatus is the card's line for a proposal keel serve already knows was typed.
func typedStatus(p keelclient.Proposal) *agentStatus {
	if p.TypedAt == nil || p.Expired {
		return nil
	}
	return &agentStatus{ok: true, text: "Typed into " + orElse(p.Host, "the pane") + " at " + stampLabel(*p.TypedAt, time.Now()) + "."}
}

// agentStop is Stop: it cancels the ask in flight, as the app's AbortController does.
func (w *workspace) agentStop() {
	if w.agent.busy != nil {
		w.agent.busy()
	}
}

// newAgentConversation is New conversation: it stops an ask in flight and starts again with the setting's scope.
func (w *workspace) newAgentConversation() {
	a := w.agent
	if a.busy != nil {
		a.busy()
		a.busy = nil
	}
	a.resumedSeq++ // a resume still being read belongs to the conversation that was left
	a.conv, a.title = newConversationID(), ""
	a.turns, a.props = nil, map[string]*agentProp{}
	a.resetMemory()
	a.createdAt, a.resumedAt, a.renameID = time.Time{}, time.Time{}, ""
	a.scopeAuto, a.picker, a.sessOpen = true, false, false
	a.focusInput = true
}

// ---- the provider key ----

// saveAgentKey sends the key in the masked field, once. The field is emptied as it is sent: nothing keeps the key, and
// when keel serve refuses it the user types it again.
func (w *workspace) saveAgentKey() {
	a := w.agent
	if a.keyBusy {
		return
	}
	key := strings.TrimSpace(a.keyField)
	a.keyField = ""
	if key == "" {
		a.keyErr, a.focusKey = "Paste a key first.", true
		return
	}
	pid := "gemini"
	if a.settings != nil && a.settings.Settings.Provider != "" {
		pid = a.settings.Settings.Provider
	}
	short := "Gemini"
	if a.provider != nil && a.provider.ShortName != "" {
		short = a.provider.ShortName
	}
	in := keelclient.AgentKeyInput{Provider: pid, Key: key}
	key = ""
	a.keyBusy, a.keyErr = true, ""
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.SetAgentKey(c, in)
		in = keelclient.AgentKeyInput{}
		w.post(func() {
			a.keyBusy = false
			if err != nil {
				a.keyErr = errText("save the key", err)
				return
			}
			k := true
			a.keyConfigured = &k
			w.say(short + " connected. The key is in the vault.")
			a.focusInput = true
			w.loadAgent()
		})
	})
}

// ---- cards ----

// editAgentProposal is a card's Save command: keel serve keeps the edit and answers with the proposal as it now holds it
// (the bytes, the kind and the ⇧ rule recomputed). What is typed afterwards is that, never the text of the field.
func (w *workspace) editAgentProposal(id string) {
	a := w.agent
	rec := a.props[id]
	if rec == nil || rec.saving || rec.p.Expired { // an expired proposal is never edited: keel serve no longer holds it
		return
	}
	command, conv := rec.edit, a.conv
	rec.saving = true
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		p, err := w.be.EditProposal(c, conv, id, command)
		w.post(func() {
			rec.saving = false
			if w.closed || a.conv != conv {
				return
			}
			if err != nil {
				w.warn(errText("edit the command", err))
				return
			}
			rec.p, rec.status, rec.editing = p, nil, false
		})
	})
}

// typeAgentProposal is a card's Type into pane (term.js typeAgentProposal, agentpanel.js typeProposal). It never types:
// it opens the Approve before type dialog, whose Type action is the only thing that does.
func (w *workspace) typeAgentProposal(id string, shift bool) {
	a := w.agent
	rec := a.props[id]
	if rec == nil || rec.p.Expired { // an expired card has no Type; this is the second lock (typeGate refuses it too)
		return
	}
	pane := w.agentPaneBySession(rec.p.SessionID)
	ok, reason := typeGate(rec.p, pane, shift)
	if !ok {
		if reason == gateShift { // plain Type is refused in the card; ⇧ + Type opens the dialog
			rec.status = &agentStatus{text: "Not typed. " + shiftReason(rec.p) + ": hold " + shiftWord() + " and press Type."}
		}
		return
	}
	switch {
	case pane.p == nil || pane.remote:
		rec.status = &agentStatus{text: msgAgentNotOpen}
		return
	case !pane.driving:
		rec.status = &agentStatus{text: msgAgentFollowing}
		return
	}
	host := orElse(rec.p.Host, pane.p.title())
	// A read-only one-liner keel serve agrees to is typed with no dialog and no Enter (rule 13); the card's refusals above
	// stand, and a destructive or state-changing proposal never reaches here without ⇧ and never goes direct.
	line := ""
	if proposalDirect(rec.p) {
		line = rec.p.Command
	}
	w.typeNow(pane.p, typeSource{
		line: line, conv: a.conv, proposal: id, shift: shift, target: host,
		dialog: func() {
			w.openApproveAgent(pane.p, &approveAgent{
				conv: a.conv, proposal: id, shift: shift,
				session: paneTarget(host, pane.sub) + " (agent proposal)",
				source: func() (string, bool) {
					r := a.props[id]
					if r == nil {
						return "", false
					}
					return r.p.Bytes, true // what keel serve holds for the proposal: the edit's answer, not a draft
				},
				done: func(err error) {
					rec.status = &agentStatus{ok: err == nil, text: "Typed into " + host + "."}
					if err != nil {
						rec.status.text = err.Error()
					} else {
						w.runStepTyped(rec)
					}
				},
				cancelled: func() { rec.status = nil },
			})
		},
		// Typed with no dialog: the card says so, and that Enter is the user's (the page's res.direct message).
		done: func(typed bool, message string) {
			rec.status = &agentStatus{ok: typed, text: message}
			if typed {
				w.runStepTyped(rec)
			}
		},
	})
}

// paneTarget names a pane with its tab ("prod-db-01 · deploy"); the tab is left out when it only repeats the name
// (lib/agent.js paneTarget).
func paneTarget(label, tab string) string {
	l, t := strings.TrimSpace(label), strings.TrimSpace(tab)
	switch {
	case t == "" || t == l:
		return l
	case l == "":
		return t
	}
	return l + " · " + t
}
