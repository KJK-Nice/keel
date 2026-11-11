package native

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Saved conversations (docs/ui-redesign.md §5.4.1, the panel's title button) and the Agent tab page's use of the same
// one conversation (§5.4.2). The feature is cmd/keel/web/js/ui/agentpanel.js (openSessions, resume, renameSession,
// deleteSession, deleteAllSessions, proposeAgain) and agentthread.js (renderTranscript, paintExpired).
//
// What this file does and does not do:
//   - keel serve saves the conversation in the vault, redacted as sent. This client reads the list and a transcript into
//     memory when the user asks to see them and writes none of it anywhere: not to keel-native.json, not to the saved
//     workspace (which may hold the page tab's name and nothing of what was said).
//   - A resumed conversation carries on under its own id; the ask sends exactly what any other ask sends.
//   - A proposal keel serve no longer holds comes back Expired. Its card has Copy and Propose again and no Type and no Edit,
//     and nothing in this file sends either request for it: Propose again asks keel serve for a new proposal, which is
//     handled like any other (typeAgentProposal, the Approve before type dialog).
//   - Delete and Delete all ask first.

const (
	msgListLocked   = "Unlock the vault to see saved conversations."
	msgConvGone     = "That conversation is gone."
	deleteAllTitle  = "Delete all conversations?"
	deleteAllAction = "Delete all"
	deleteAllText   = "Every saved agent conversation is deleted from the vault, in every window. The log entries that say what the agent read stay in Logs."
	deleteOneTitle  = "Delete this conversation?"
	deleteOneAction = "Delete"
	deleteOneText   = "This conversation is deleted from the vault, in every window. The log entries that say what the agent read stay in Logs."
)

// sessUI is the Recent conversations popover's own state.
type sessUI struct {
	filter  string
	filterH ui.Handle
	focus   bool // give the search field the keyboard
	sel     int  // the row the arrow keys are on; -1 is the search field
}

// historyDays is how long keel serve keeps conversations as far as this window knows: the last list, else the settings.
// nil until one of them was read.
func (a *agentPanel) historyDays() *int {
	if a.list.historyDays != nil {
		return a.list.historyDays
	}
	if a.settings != nil {
		d := a.settings.Settings.HistoryDays
		return &d
	}
	return nil
}

// savedNote is the sub row's note on where the conversation lives.
func (a *agentPanel) savedNote() string {
	if d := a.historyDays(); d != nil {
		return savedLabel(*d, a.list.saveError)
	}
	return savedInVault
}

// convTurns is the conversation as the page's context column reads it.
func (a *agentPanel) convTurns() []convTurn {
	out := make([]convTurn, 0, len(a.turns))
	for _, t := range a.turns {
		ct := convTurn{trace: t.trace, again: t.again}
		for _, id := range t.props {
			if r := a.props[id]; r != nil {
				ct.proposals = append(ct.proposals, r.p)
			}
		}
		out = append(out, ct)
	}
	return out
}

// questions is how many questions the conversation holds (a Propose again turn is not one).
func (a *agentPanel) questions() (n int) {
	for _, t := range a.turns {
		if !t.again {
			n++
		}
	}
	return n
}

// known is a conversation keel serve knows: one that was asked or resumed. Rename and Delete need one.
func (a *agentPanel) known() bool { return len(a.turns) > 0 }

// agentPageShown is whether the Agent page is the tab on screen.
func (w *workspace) agentPageShown() bool {
	t := w.tab()
	return t != nil && t.pg != nil && t.pg.agent != nil
}

// ---- the list ----

// loadConversations reads the saved conversations (GET /api/agent/conversations).
func (w *workspace) loadConversations() {
	a := w.agent
	if a.listLoading {
		return
	}
	a.listLoading = true
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		l, err := w.be.Conversations(c)
		w.post(func() {
			a.listLoading = false
			if w.closed {
				return
			}
			if err != nil {
				a.list.sessions = nil
				var se *keelclient.StatusError
				if errors.As(err, &se) && se.Code == 423 {
					a.list.err = msgListLocked
				} else {
					a.list.err = errText("read the conversations", err)
				}
				return
			}
			d := l.HistoryDays
			a.list = convListState{sessions: l.Sessions, historyDays: &d, saveError: l.SaveError, unreadable: l.Unreadable}
			a.sess.sel = min(a.sess.sel, len(l.Sessions)-1)
		})
	})
}

// openSessions is a press on the title button.
func (w *workspace) openSessions() {
	a := w.agent
	a.picker, a.sessOpen = false, true
	a.sess.filter, a.sess.focus, a.sess.sel, a.renameID = "", true, -1, ""
	w.loadConversations()
}

func (w *workspace) closeSessions() {
	a := w.agent
	a.sessOpen, a.renameID = false, ""
}

// ---- resume ----

// resumeConversation opens a saved conversation in the window's one conversation, which the panel and the page both show.
// Nothing is read or sent: the transcript comes from keel serve, and the next ask reads the panes in scope as they are
// then (agentpanel.js resume).
func (w *workspace) resumeConversation(id string) {
	a := w.agent
	if id == "" {
		return
	}
	if id == a.conv && len(a.turns) > 0 {
		w.closeSessions()
		a.focusInput = true
		return
	}
	a.resumedSeq++
	seq := a.resumedSeq
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		t, err := w.be.Conversation(c, id)
		w.post(func() {
			if w.closed || a.resumedSeq != seq { // another resume, or a new conversation, began meanwhile
				return
			}
			if err != nil {
				var se *keelclient.StatusError
				if errors.As(err, &se) && se.Code == 404 {
					w.warn(msgConvGone)
				} else {
					w.warn(errText("open the conversation", err))
				}
				w.loadConversations()
				return
			}
			w.showTranscript(t)
		})
	})
}

// showTranscript makes the transcript the window's conversation: in memory only.
func (w *workspace) showTranscript(t keelclient.Transcript) {
	a := w.agent
	w.closeSessions()
	if a.busy != nil {
		a.busy()
		a.busy = nil
	}
	a.conv, a.title = t.ID, t.Title
	a.turns, a.props = nil, map[string]*agentProp{}
	a.createdAt, a.resumedAt = t.CreatedAt, t.UpdatedAt
	a.renameID = ""
	for _, st := range t.Turns {
		turn := &agentTurn{question: st.Question, at: st.At, done: true, saved: true, again: st.Again, trace: st.Trace, answer: st.Answer, cut: st.Cut}
		for _, p := range st.Proposals {
			a.props[p.ID] = &agentProp{p: p, status: typedStatus(p)}
			turn.props = append(turn.props, p.ID)
		}
		a.turns = append(a.turns, turn)
	}
	if len(a.turns) == 0 { // an empty transcript is still a conversation (an empty Propose again turn draws nothing)
		a.turns = append(a.turns, &agentTurn{done: true, saved: true, again: true})
	}
	a.stick = true
	a.focusInput = true
}

// resumedAfter is the index of the last turn read from keel serve, which the "Resumed" divider and its note follow, or -1
// when the conversation was not resumed. A later ask appends below it, as agentthread.js does.
func (a *agentPanel) resumedAfter() int {
	if a.resumedAt.IsZero() {
		return -1
	}
	last := -1
	for i, t := range a.turns {
		if t.saved {
			last = i
		}
	}
	return last
}

// ---- rename, delete ----

// beginConvRename starts the inline rename of a row in the list, or of the page's heading.
func (w *workspace) beginConvRename(id, title string) {
	a := w.agent
	a.renameID, a.renameText, a.renameFocus = id, title, true
}

func (w *workspace) endConvRename() { w.agent.renameID = "" }

// renameConversation is PATCH /api/agent/conversations/{id}, on the id keel serve gave. An empty title is not sent.
func (w *workspace) renameConversation(id, title string) {
	a := w.agent
	title = strings.TrimSpace(title)
	a.renameID = ""
	if title == "" || id == "" {
		return
	}
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		kept, err := w.be.RenameConversation(c, id, title)
		w.post(func() {
			if w.closed {
				return
			}
			if err != nil {
				w.warn(errText("rename the conversation", err))
				return
			}
			for i := range a.list.sessions {
				if a.list.sessions[i].ID == id {
					a.list.sessions[i].Title = kept
				}
			}
			if id == a.conv {
				a.title = kept
			}
		})
	})
}

// askDeleteConversation is Delete on a row or on the page: it asks first.
func (w *workspace) askDeleteConversation(id string) {
	if id == "" {
		return
	}
	w.closeSessions() // the dialog is on top: a press on it must not first dismiss the list
	w.askConfirm(&hostConfirm{title: deleteOneTitle, message: deleteOneText, action: deleteOneAction, run: func() { w.deleteConversation(id) }})
}

func (w *workspace) deleteConversation(id string) {
	a := w.agent
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.DeleteConversation(c, id)
		w.post(func() {
			if w.closed {
				return
			}
			if err != nil {
				w.warn(errText("delete the conversation", err))
				return
			}
			a.list.sessions = slices.DeleteFunc(slices.Clone(a.list.sessions), func(s keelclient.ConversationSummary) bool { return s.ID == id })
			if id == a.conv {
				w.newAgentConversation()
				a.focusInput = false
			}
			w.say("Conversation deleted")
		})
	})
}

// askDeleteAll is Delete all (Recent conversations, Settings · Agent's Delete all history): it asks first, with the app's words.
func (w *workspace) askDeleteAll() {
	w.closeSessions()
	w.askConfirm(&hostConfirm{title: deleteAllTitle, message: deleteAllText, action: deleteAllAction, run: w.deleteAllConversations})
}

func (w *workspace) deleteAllConversations() {
	a := w.agent
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.DeleteAllConversations(c)
		w.post(func() {
			if w.closed {
				return
			}
			if err != nil {
				w.warn(errText("delete the history", err))
				return
			}
			a.list.sessions, a.list.saveError, a.list.unreadable, a.list.err = nil, "", false, ""
			w.newAgentConversation()
			a.focusInput = false
			w.say("Agent history deleted")
		})
	})
}

// ---- Propose again ----

// proposeAgain is an expired card's Propose again: keel serve rates the saved command again and makes a new proposal,
// which comes back as a live card and is typed, if at all, like any other. The answer is dropped when the conversation
// changed while it was asked.
func (w *workspace) proposeAgain(id string) {
	a := w.agent
	rec := a.props[id]
	if rec == nil || !rec.p.Expired { // only a saved proposal is proposed again
		return
	}
	conv := a.conv
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		p, err := w.be.ProposeAgain(c, conv, id)
		w.post(func() {
			if w.closed || a.conv != conv {
				return
			}
			if err != nil {
				w.warn(errText("propose again", err))
				return
			}
			a.props[p.ID] = &agentProp{p: p, status: typedStatus(p)}
			a.turns = append(a.turns, &agentTurn{done: true, again: true, at: time.Now(), props: []string{p.ID}})
			a.stick = true
		})
	})
}
