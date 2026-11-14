package native

import (
	"context"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/wire"
)

// Notes for the agent (docs/ui-redesign.md §15). The feature is the list in Edit host and in
// Settings · Agent, the note cards of the agent panel, and internal/httpapi/agentnotes.go; its words
// and rules are in noteslogic.go.
//
// What this file does and does not do:
//   - Notes live in keel serve (keel.memory, in the vault). This client holds them in memory only and writes none of them to
//     disk (TestNotesAndRunbooksAreNeverWrittenToDisk).
//   - Every change the user makes in a list is saved at once with POST / PUT / DELETE /api/agent/notes: keel serve cleans and
//     redacts the text and the list shows what it kept, with a toast when it replaced values.
//   - The agent never writes a note. A note it proposes is a card; the card's Save (saveNoteProposal) is the only thing that
//     keeps it, Dismiss keeps nothing, and nothing saves, retries or pre-ticks anything on its own.
//   - Delete all notes asks first.

// notesEditor is one notes list: a host's in Edit host, or the user's own in Settings · Agent.
type notesEditor struct {
	user    bool   // the user's own notes (Settings · Agent), else the notes of hostID or, with group set, of groupID
	hostID  string // "" for a host that is not saved yet
	group   bool   // the notes of a folder (§17): about every host in it
	groupID string

	notes      []keelclient.Note
	loaded     bool
	unreadable bool // keel.memory cannot be read: Delete all notes starts a fresh file
	busy       bool
	gen        int // a read that answers after another began, or after the list was dropped, is ignored
	err        string

	input      string
	inputH     ui.Handle
	focusInput bool

	editing   string // the note being edited
	editText  string
	editH     ui.Handle
	focusEdit bool
}

// mine picks this list's notes from all of them.
func (e *notesEditor) mine(all []keelclient.Note) []keelclient.Note {
	switch {
	case e.user:
		return userNotesOf(all)
	case e.group:
		return groupNotesOf(all, e.groupID)
	}
	return hostNotesOf(all, e.hostID)
}

func (e *notesEditor) saved() bool { return e.user || e.hostID != "" || (e.group && e.groupID != "") }

// loadNotes reads the notes (GET /api/agent/notes) and keeps this list's. It forgets what was being typed.
func (w *workspace) loadNotes(e *notesEditor) {
	e.gen++
	gen := e.gen
	e.editing, e.input, e.err, e.loaded, e.notes = "", "", "", false, nil
	if !e.saved() {
		return
	}
	callIf(w, func() bool { return e.gen == gen }, w.be.Notes, func(l keelclient.NoteList, err error) {
		if err != nil {
			e.err = hostError(err)
		} else {
			e.unreadable, e.notes = l.Unreadable, e.mine(l.Notes)
		}
		e.loaded = true
	})
}

func (e *notesEditor) find(id string) (keelclient.Note, bool) {
	for _, n := range e.notes {
		if n.ID == id {
			return n, true
		}
	}
	return keelclient.Note{}, false
}

// noteSavedToast is the toast after an add or a save that replaced values: "Note added. 1 secret-looking value was replaced
// before saving." Nothing is said when nothing was replaced.
func noteSavedToast(verb string, redacted int) string {
	if extra := redactedToast(redacted); extra != "" {
		return "Note " + verb + ". " + extra
	}
	return ""
}

// addNote is Add (and ↵ in the field): the text as typed, cleaned only to decide whether a request is worth sending.
func (w *workspace) addNote(e *notesEditor) {
	if e.busy || !e.saved() {
		return
	}
	text, ok, tooLong := cleanNote(e.input)
	if !ok {
		e.err = ""
		if tooLong {
			e.err = noteTooLong
		}
		e.focusInput = true
		return
	}
	e.busy, e.err = true, ""
	gen := e.gen
	in := keelclient.NoteInput{Text: text}
	switch {
	case e.group:
		in.GroupID = e.groupID // a note has a host or a group, never both
	case !e.user:
		in.HostID = e.hostID
	}
	call(w, func(c context.Context) (keelclient.SavedNote, error) { return w.be.SaveNote(c, in) }, func(res keelclient.SavedNote, err error) {
		e.busy = false
		if e.gen != gen {
			return
		}
		e.focusInput = true
		if err != nil {
			e.err = hostError(err)
			return
		}
		e.notes = append(e.notes, res.Note) // what keel serve kept
		e.input = ""
		if t := noteSavedToast("added", res.Redacted); t != "" {
			w.say(t)
		}
	})
}

func (w *workspace) startEditNote(e *notesEditor, id string) {
	if n, ok := e.find(id); ok {
		e.editing, e.editText, e.focusEdit, e.err = id, n.Text, true, ""
	}
}

func (w *workspace) cancelEditNote(e *notesEditor) { e.editing = "" }

// saveNoteEdit is Save (and ↵) on an edited note.
func (w *workspace) saveNoteEdit(e *notesEditor) {
	id := e.editing
	if id == "" || e.busy {
		return
	}
	text, ok, tooLong := cleanNote(e.editText)
	if !ok {
		e.err = "A note needs some text. Delete it instead."
		if tooLong {
			e.err = noteTooLong
		}
		return
	}
	e.busy = true
	gen := e.gen
	call(w, func(c context.Context) (keelclient.SavedNote, error) {
		return w.be.SaveNote(c, keelclient.NoteInput{ID: id, Text: text})
	}, func(res keelclient.SavedNote, err error) {
		e.busy = false
		if e.gen != gen {
			return
		}
		if err != nil {
			e.err = hostError(err)
			return
		}
		for i := range e.notes {
			if e.notes[i].ID == id {
				e.notes[i] = res.Note
			}
		}
		e.editing, e.err = "", ""
		if t := noteSavedToast("saved", res.Redacted); t != "" {
			w.say(t)
		}
	})
}

// deleteNote is the row's Delete: at once (only Delete all notes asks).
func (w *workspace) deleteNote(e *notesEditor, id string) {
	if e.busy {
		return
	}
	e.busy = true
	gen := e.gen
	callErr(w, func(c context.Context) error { return w.be.DeleteNote(c, id) }, func(err error) {
		e.busy = false
		if e.gen != gen {
			return
		}
		if err != nil {
			e.err = hostError(err)
			return
		}
		var keep []keelclient.Note
		for _, n := range e.notes {
			if n.ID != id {
				keep = append(keep, n)
			}
		}
		e.notes, e.err = keep, ""
		if e.editing == id {
			e.editing = ""
		}
		w.say("Note deleted.")
		e.focusInput = true
	})
}

// askDeleteAllNotes is Delete all notes: it asks first.
func (w *workspace) askDeleteAllNotes() {
	w.askConfirm(&confirmDialog{
		title:   "Delete all notes?",
		message: "Every host note and every note about you is deleted from the vault. Conversations and the log stay.",
		action:  "Delete all notes",
		run:     w.deleteAllNotes,
	})
}

func (w *workspace) deleteAllNotes() {
	callErr(w, func(c context.Context) error { _, err := w.be.DeleteAllNotes(c); return err }, func(err error) {
		if err != nil {
			msg := hostError(err)
			if msg == "" {
				msg = "Could not delete the notes"
			}
			w.warn(msg)
			return
		}
		w.say("Notes deleted")
		if p := w.settingsPage(); p != nil {
			w.loadNotes(&p.notes)
		}
		if d := w.hostDlg; d != nil && d.nb != nil {
			w.loadNotes(d.nb)
		}
	})
}

// ---- the notes the agent proposes ----

// agentNote is a note proposal on the thread: what keel serve holds, and what the card is doing.
type agentNote struct {
	n    keelclient.NoteProposal
	busy bool
	err  string

	editing   bool
	edit      string    // the field; sent to keel serve only by Save
	editH     ui.Handle // the field in the panel
	editHP    ui.Handle // the same field on the Agent page
	focusEdit bool
}

// registerNote adds a proposal to the conversation's cards.
func (a *agentPanel) registerNote(n keelclient.NoteProposal) *agentNote {
	rec := &agentNote{n: n}
	a.notes[n.ID] = rec
	return rec
}

func (w *workspace) startEditNoteProposal(rec *agentNote) {
	if rec.n.Expired || rec.n.Status != wire.CardOpen {
		return
	}
	rec.editing, rec.edit, rec.focusEdit = true, rec.n.Text, true
}

// saveNoteProposal is the card's Save (or Save anyway): the one thing that keeps a note the agent proposed. It sends the
// user's edit if there is one, else "" (keep it as shown). It runs once per press: a card that is busy ignores a second.
func (w *workspace) saveNoteProposal(id string) {
	a := w.agent
	rec := a.notes[id]
	if rec == nil || rec.busy || rec.n.Expired || rec.n.Status != wire.CardOpen {
		return
	}
	text := ""
	if rec.editing {
		text = rec.edit
	}
	conv := a.conv
	rec.busy, rec.err = true, ""
	call(w, func(c context.Context) (keelclient.SavedNote, error) { return w.be.SaveProposedNote(c, conv, id, text) }, func(res keelclient.SavedNote, err error) {
		rec.busy = false
		if a.conv != conv {
			return
		}
		if err != nil {
			rec.err = hostError(err)
			return
		}
		if res.Proposal != nil {
			rec.n = *res.Proposal
		} else {
			rec.n.Status, rec.n.NoteID = "saved", res.Note.ID
		}
		rec.editing = false
		if t := savedRedactedToast(res.Redacted); t != "" {
			w.say(t)
		}
	})
}

// dismissNoteProposal is Dismiss: the proposal is closed and nothing is kept.
func (w *workspace) dismissNoteProposal(id string) { w.noteProposalAction(id, false) }

// undoNoteProposal is Undo on a saved note: keel serve deletes the note and reopens the proposal.
func (w *workspace) undoNoteProposal(id string) { w.noteProposalAction(id, true) }

func (w *workspace) noteProposalAction(id string, undo bool) {
	a := w.agent
	rec := a.notes[id]
	if rec == nil || rec.busy || rec.n.Expired {
		return
	}
	conv := a.conv
	rec.busy, rec.err = true, ""
	call(w, func(c context.Context) (keelclient.NoteProposal, error) {
		if undo {
			return w.be.UndoProposedNote(c, conv, id)
		}
		return w.be.DismissProposedNote(c, conv, id)
	}, func(n keelclient.NoteProposal, err error) {
		rec.busy = false
		if a.conv != conv {
			return
		}
		if err != nil {
			rec.err = hostError(err)
			return
		}
		rec.n = n
		rec.editing = false
	})
}
