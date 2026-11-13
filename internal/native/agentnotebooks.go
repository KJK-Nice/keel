package native

import (
	"context"
	"errors"
	"slices"

	"keel/internal/native/keelclient"
)

// Notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12; cmd/keel/web/js/ui/agentpanel.js, ui/agentthread.js and
// term.js askAboutNotebook). keel serve decides everything:
//   - The user attaches up to two notebooks to a question. An ask names them by id and never sends text; keel serve reads
//     and redacts them, and refuses a private one. It also names the notebooks open here with typing not saved yet
//     (Editing), so the agent's edit to one is refused and no keystroke is lost.
//   - The agent creates and edits notebooks that are not private, and proposes deletes. Each is a card. A card's buttons
//     are requests to keel serve (Undo, Delete after the user confirmed, Keep, Restore); the client changes no notebook
//     itself and shows the notebook the answer returns.
//   - Nothing here types, reads a pane, or marks a notebook reviewed.

// agentNbProp is a notebook card on the thread: what keel serve holds, and what the card is doing.
type agentNbProp struct {
	p    keelclient.NotebookProposal
	busy bool
	err  string
}

// registerNbProp adds a card to the conversation. The notebook it carries (Record) is not kept with the card.
func (a *agentPanel) registerNbProp(p keelclient.NotebookProposal) {
	p.Record = nil
	a.nbProps[p.ID] = &agentNbProp{p: p}
}

// notebookChanged shows a notebook as keel serve has it: the list takes it, an open pane with no typing loads it, and one
// with typing keeps it and says so once when the agent made the change (term.js notebookChanged).
func (w *workspace) notebookChanged(rec keelclient.Notebook) {
	w.rememberNotebook(rec)
	w.followNotebooks()
}

// ---- attaching ----

// attachedNotebooks are the attached notebooks that still exist, in the order they were attached.
func (w *workspace) attachedNotebooks() (out []keelclient.Notebook) {
	for _, id := range w.agent.attached {
		if nb, ok := w.notebook(id); ok {
			out = append(out, nb)
		}
	}
	return out
}

// attachNotebook attaches one notebook (agentpanel.js attachNotebook): a private one is refused, and so is a third.
func (w *workspace) attachNotebook(id string) {
	a := w.agent
	if id == "" || slices.Contains(a.attached, id) {
		return
	}
	if nb, ok := w.notebook(id); ok && nb.Private {
		w.warn(privateRefused)
		return
	}
	if len(a.attached) >= maxAttached {
		w.warn(attachLimit)
		return
	}
	a.attached = reconcileAttached(append(slices.Clone(a.attached), id), w.notebooks)
}

// toggleAttachedNotebook is a picker row: it ticks or unticks. A row that cannot be ticked says why instead of acting.
func (w *workspace) toggleAttachedNotebook(id string) {
	a := w.agent
	if nb, ok := w.notebook(id); ok && nb.Private {
		w.warn(privateRefused)
		return
	}
	if !slices.Contains(a.attached, id) && len(a.attached) >= maxAttached {
		w.warn(attachLimit)
		return
	}
	a.attached = reconcileAttached(toggleAttached(a.attached, id, w.notebooks), w.notebooks)
}

func (w *workspace) detachNotebook(id string) { w.agent.attached = removeID(w.agent.attached, id) }

// askAboutNotebook is "Ask the agent about this notebook": the panel opens with the notebook attached. Nothing is sent
// until the user asks. A private notebook does not exist for the agent (§6 rule 12), and a joined tab has no agent.
func (w *workspace) askAboutNotebook(id string) {
	if w.joinedActive() {
		w.say(msgAgentJoined)
		return
	}
	if nb, ok := w.notebook(id); ok && nb.Private {
		w.warn(privateRefused)
		return
	}
	w.setAgentOpen(true)
	w.attachNotebook(id)
}

// askAboutNotebookPane is the notebook pane bar's button: a notebook not saved yet has no id for the agent to be sent.
func (w *workspace) askAboutNotebookPane(p *pane) {
	if p == nil || p.nb == nil {
		return
	}
	if p.nb.id == "" {
		w.say(saveFirst)
		return
	}
	w.askAboutNotebook(p.nb.id)
}

// ---- before an ask ----

// editingNotebooks are the ids of the open notebooks with typing keel serve does not have (a save pending, running or
// failed): the agent's edits to them are refused (term.js editingNotebooks).
func (w *workspace) editingNotebooks() (ids []string) {
	for _, p := range w.notebookPanes() {
		if n := p.nb; n.id != "" && !n.gone && n.dirty() && !slices.Contains(ids, n.id) {
			ids = append(ids, n.id)
		}
	}
	return ids
}

// nbFlushAll saves what was typed in every open notebook, then calls done on the UI thread (at once when nothing is
// pending). Typing saves first, so the agent reads what the user typed.
func (w *workspace) nbFlushAll(done func()) {
	var open []*pane
	for _, p := range w.notebookPanes() {
		if !p.nb.gone && (p.nb.auto.pending() || p.nb.saving) {
			open = append(open, p)
		}
	}
	left := len(open)
	if left == 0 {
		done()
		return
	}
	for _, p := range open {
		w.nbFlush(p, func(bool) {
			if left--; left == 0 {
				done()
			}
		})
	}
}

// ---- the card's buttons ----

// nbCardMessage is what a card says when keel serve refuses its action.
func nbCardMessage(err error) string {
	var se *keelclient.StatusError
	if errors.As(err, &se) && se.Code == 410 {
		return "Keel no longer holds this conversation's changes. Use History on the notebook, or Notebooks · Recently deleted."
	}
	return nbErr(err, err.Error())
}

// askNbPropAction is a card's button. Delete asks first, with the app's question; the others act at once. Each is a request
// to keel serve and the card then shows what it answered.
func (w *workspace) askNbPropAction(id, action string) {
	rec := w.agent.nbProps[id]
	if rec == nil || rec.busy {
		return
	}
	if action == actDelete {
		w.askConfirm(&hostConfirm{title: "Delete notebook", message: deleteConfirmText(rec.p), action: "Delete", run: func() { w.nbPropAction(id, action) }})
		return
	}
	w.nbPropAction(id, action)
}

// nbPropAction posts a card's route: Undo, Delete (only after the user confirmed), Keep or Restore. Typing is saved first,
// so the action acts on what the user has and a pending save cannot write the old text back afterwards.
func (w *workspace) nbPropAction(id, action string) {
	a := w.agent
	rec := a.nbProps[id]
	if rec == nil || rec.busy {
		return
	}
	conv, was := a.conv, rec.p
	rec.busy, rec.err = true, ""
	w.nbFlushAll(func() {
		call(w, func(c context.Context) (res keelclient.NotebookAction, err error) {
			switch action {
			case actUndo:
				return w.be.UndoNotebookChange(c, conv, id)
			case actDelete:
				return w.be.PostNotebookDeleteConfirm(c, conv, id)
			case actKeep:
				return w.be.DismissNotebookProposal(c, conv, id)
			case actRestore:
				return w.be.RestoreNotebookChange(c, conv, id)
			}
			return res, errors.New("unknown card action")
		}, func(res keelclient.NotebookAction, err error) {
			rec.busy = false
			if a.conv != conv {
				return
			}
			if err != nil {
				rec.err = nbCardMessage(err)
				return
			}
			gone := (action == actDelete || (action == actUndo && was.Kind == "create")) && was.NotebookID != ""
			if res.Proposal.ID != "" {
				rec.p = res.Proposal
				rec.p.Record = nil
			}
			if res.Notebook != nil {
				w.applyNotebook(*res.Notebook)
			}
			if gone {
				w.removeNotebook(was.NotebookID)
			}
			w.reloadNotebooks()
		})
	})
}

// ---- the review banner's actions (term.js undoAgentEdit) ----

// undoAgentEdit is the banner's Undo: it restores the version kept before the newest agent edit. Typing since then is
// kept in History too (keel serve keeps the version a restore replaces), but it asks first.
func (w *workspace) undoAgentEdit(id string) {
	w.nbFlushID(id, func() {
		rec, ok := w.notebook(id)
		if !ok {
			return
		}
		call(w, func(c context.Context) ([]keelclient.NotebookVersion, error) { return w.be.NotebookHistory(c, id) }, func(vs []keelclient.NotebookVersion, err error) {
			if err != nil {
				w.warn(nbErr(err, "Could not undo"))
				return
			}
			i := slices.IndexFunc(vs, func(v keelclient.NotebookVersion) bool { return v.Event == "before-agent-edit" })
			if i < 0 {
				w.warn("Keel has no version from before the agent's edit. Open History to choose one.")
				return
			}
			vid := vs[i].ID
			if rec.LastBy != byAgent {
				w.askConfirm(&hostConfirm{title: "Undo the agent's edit", action: "Undo",
					message: "You changed this notebook after the agent's edit. Undo puts back the version from before the agent's edit; your later changes stay in History.",
					run:     func() { w.restoreBeforeAgentEdit(id, vid, rec.Rev) }})
				return
			}
			w.restoreBeforeAgentEdit(id, vid, rec.Rev)
		})
	})
}

func (w *workspace) restoreBeforeAgentEdit(id, vid string, rev int) {
	call(w, func(c context.Context) (keelclient.Notebook, error) {
		return w.be.RestoreNotebookVersion(c, id, vid, rev)
	}, func(saved keelclient.Notebook, err error) {
		if err != nil {
			w.warn(nbErr(err, "Could not undo"))
			return
		}
		w.applyNotebook(saved)
		w.say("Undone. The agent's version is in History.")
	})
}
