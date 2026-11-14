package native

import (
	"strings"

	"keel/internal/cmdrule"
)

// Typing a notebook's fence (docs/ui-redesign.md §6 rules 12 and 13; term.js runNotebookFence, insertNotebookIntoInput,
// confirmTermInput). A notebook is one more source for the two ways a snippet is typed, and adds no writer and no call
// site: this file calls neither ApprovedInject nor DirectType (guard_test.go keeps that true).
//
//   - Direct: a shell fence of exactly one line, with no placeholder, goes through typeNow. keel serve compares the line with
//     the one line of a shell fence of the stored notebook (so the notebook is saved first), refuses an unreviewed agent edit
//     with a dialog, and types the line with no Enter and no Ctrl-U.
//   - The dialog: every other fence, and whenever keel serve says a dialog is needed, opens Approve before type with the
//     fence's exact bytes: the trimmed code and a newline, no Ctrl-U (confirmTermInput sends line + "\n"), and the inject
//     carries notebook:true (pty.js sendApprovedInject), which keel serve refuses a placeholder for.
//
// The target is the terminal beside the notebook in its tab. Opening, rendering, editing, saving, restoring and deleting
// a notebook type nothing; only the Type button of a fence does, and only for a shell fence.

// unreviewedNote is the line under the bytes when the agent edited the notebook and the user has not reviewed that edit
// (term.js insertNotebookIntoInput: _inputAgentNote).
const unreviewedNote = "The agent edited this notebook and you have not reviewed that edit. Read this command before you approve it: it may be the agent's."

// typeNotebookFence is a fence's Type button.
func (w *workspace) typeNotebookFence(np *pane, f fence) {
	t := w.tabOf(np)
	if t == nil || np.nb == nil || !f.Shell {
		return
	}
	term := t.firstTerm()
	if term == nil {
		w.say(msgNoTermBeside)
		return
	}
	line := strings.TrimSpace(f.Code)
	if line == "" {
		w.say("Write a command first")
		return
	}
	if cmdrule.HasPlaceholder(line) { // a shell glob, never the value: refused before anything is sent
		w.warn(placeholderRefused)
		return
	}
	src := typeSource{notebookID: np.nb.id, dialog: func() { w.askNotebookType(term, np, line) }}
	if directable(f) && np.nb.id != "" {
		src.line = line
	}
	// keel serve compares the line with the stored text: what was typed is saved first.
	w.nbFlush(np, func(bool) { w.typeNow(term, src) })
}

// askNotebookType opens Approve before type for a fence's line. It sends nothing.
func (w *workspace) askNotebookType(term, np *pane, line string) {
	note := ""
	if rec, ok := w.notebook(np.nb.id); ok && rec.AgentUnreviewed {
		note = unreviewedNote
	}
	w.openApprove(term, func() (string, bool) { return line, true })
	w.approve.fromNotebook(note)
}
