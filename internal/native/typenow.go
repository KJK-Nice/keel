package native

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/cmdrule"
	"keel/internal/native/keelclient"
)

// Type without the dialog (docs/ui-redesign.md §6 rule 13): the one function that asks keelclient.DirectType to type, and
// the only caller of it. Every entry point of a snippet (the palette row, the sidebar row, the page's button and its row
// menu) and of an agent proposal's card goes through typeNow; guard_test.go keeps that true.
//
// keel serve decides, every time. The hint (typedirect.go) only skips the round trip when the answer is certainly
// "dialog". The bytes are the command alone: no Ctrl-U, no newline, so the command is appended to the prompt line and
// nothing runs until the user presses Enter in that pane. This client never sends an Enter.

// typeSource is what an entry point hands typeNow.
type typeSource struct {
	line      string // the command a direct type sends, as it is saved: "" when the dialog is certain
	snippetID string // a snippet's id: keel serve compares its stored command with the line
	// A notebook fence instead: the notebook's id, whose stored text keel serve searches for the line as the one line of a
	// shell fence (notebooktype.go). It must have been saved before the request.
	notebookID string
	// An agent proposal instead: the conversation, the proposal and ⇧ as the card was pressed. keel serve checks the
	// proposal, the pane and the ⇧ rule itself. Only typeNow builds the reference, so no other file may.
	conv, proposal string
	shift          bool
	target         string                           // what the note names; the pane's title when empty
	dialog         func()                           // opens Approve before type: the path that was there before
	done           func(typed bool, message string) // the card's status for a proposal; nil for a snippet, which a toast tells
}

// reviewHold is how long the keyboard stays off the terminal after a direct type while a key is still down
// (1.5 s): a held ↵ that pressed the button must not repeat into the pane and run the command.
const reviewHold = 1500 * time.Millisecond

// typeNow types s.line into p without the dialog when keel serve allows it, and opens the dialog otherwise.
func (w *workspace) typeNow(p *pane, s typeSource) {
	if t := w.tabOf(p); t != nil && t != w.tab() {
		w.selectTab(w.tabIndex(t)) // the pane you can see
	}
	alt, promptLine := true, "" // a pane whose screen is unknown is read as the alternate screen, never as a prompt
	if p.screen != nil {
		alt, promptLine = p.screen()
	}
	switch {
	case !p.canType(): // the dialog's own refusals say why
		s.dialog()
		return
	case s.line == "", !cmdrule.DirectSafe(s.line), alt: // the dialog is certain: keel serve is not asked
		s.dialog()
		return
	case p.typing:
		w.warn(keelclient.ErrDirectBusy.Error())
		return
	}
	ref := keelclient.DirectRef{Kind: "snippet", SnippetID: s.snippetID, AltScreen: alt, PromptLine: promptLine}
	var agent *keelclient.AgentRef
	if s.notebookID != "" {
		ref.Kind, ref.SnippetID, ref.NotebookID = "notebook", "", s.notebookID
	}
	if s.proposal != "" {
		ref.Kind, ref.SnippetID = "agent", ""
		agent = &keelclient.AgentRef{ConversationID: s.conv, ProposalID: s.proposal, Shift: s.shift}
	}
	target := orElse(s.target, orElse(p.title(), p.conn.Snapshot().Subtitle))
	conn, line := p.conn, s.line
	p.typing = true
	call(w, func(c context.Context) (keelclient.DirectResult, error) { return conn.DirectType(c, line, ref, agent) }, func(res keelclient.DirectResult, err error) {
		p.typing = false
		msg := ""
		switch {
		case err != nil: // includes no answer: it may have been typed, so no dialog and no retry
			msg = err.Error()
		case res.Typed:
			note := typedNote(target)
			w.say(note)
			if t := w.tabOf(p); t != nil && t == w.tab() {
				w.reviewing, w.reviewAt = p.id, time.Now()
			}
			if s.done != nil {
				s.done(true, note)
			}
			return
		case res.Dialog:
			s.dialog()
			return
		default:
			msg = res.Message
		}
		if s.done != nil {
			s.done(false, msg)
			return
		}
		w.warn(msg)
	})
}

// reviewFocus gives the terminal the keyboard after a direct type, once no key is held (or after reviewHold).
func (w *workspace) reviewFocus(c *ui.Context) {
	if w.reviewing == "" {
		return
	}
	if len(c.KeysDown()) == 0 || time.Since(w.reviewAt) > reviewHold {
		w.wantFocus, w.reviewing = w.reviewing, ""
		return
	}
	c.Invalidate()
}
