package native

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Approve before type (docs/ui-redesign.md §6): keel-native never types anything on the user's behalf until the user
// has seen the exact bytes and pressed Type. This file is the only caller of keelclient.ApprovedInject, and Type is
// its only trigger (guard_test.go keeps both true). Words, bytes and rules are the webview's: ptyInjectDialog in
// internal/ui/dialogs.templ and approveBeforeType, settlePtyInject and paletteRunSnippet in cmd/keel/web/js/term.js,
// and sendApprovedInject in pty.js.
//
// The bytes: the command gets a trailing newline if it has none (term.js paletteRunSnippet), the dialog shows that
// text, and what is sent is Ctrl-U (it clears the line first, as the dialog says) followed by exactly that text.
// keel serve writes the bytes it is given and adds nothing (runtime.Session.InjectTicket). Nothing is kept: the
// dialog asks its source for the command every time it draws and every time Type is pressed.
//
// There is no production or ⇧ rule for snippets: the webview has none (the rule lives in the agent's proposals, which
// this client does not have).

// clearLine is Ctrl-U: the shell clears what is typed on the line before the snippet.
const clearLine = "\x15"

const approvePreviewMax = 130 // not a token: .pty-inject-preview max-height, 10rem

// The webview's words for the dialog (ptyInjectDialog) and for a refusal (pty.js).
const (
	approveTitle    = "Type into the terminal?"
	approveHint     = "This sends keystrokes to the focused session. The current line is cleared first. Nothing is typed until you approve."
	msgNoSession    = "Open a session first"
	msgNotConnected = "Session is not connected"
	msgFollowing    = "You are following — take or request control to type"
	msgGone         = "It was deleted elsewhere; close this dialog." // hostError for a 404
)

// approveDialog is the open Approve before type dialog: the pane it types into and where the command comes from.
type approveDialog struct {
	p      *pane
	source func() (string, bool) // the command as it is now; false when it is gone (deleted elsewhere)
	err    string                // why the last Type did not type
	busy   bool                  // a Type is waiting for keel serve
	focus  bool                  // the dialog has been given the keyboard

	cancelH, typeH ui.Handle
}

// shown is the text the dialog previews: the command, with a newline if it has none (term.js paletteRunSnippet).
func (d *approveDialog) shown() string {
	cmd, _ := d.source()
	if !strings.HasSuffix(cmd, "\n") {
		cmd += "\n"
	}
	return cmd
}

func (d *approveDialog) has() bool {
	_, ok := d.source()
	return ok
}

// session is the line after "Session:" (term.js: the pane's subtitle, else its title).
func (d *approveDialog) session() string {
	st := d.p.conn.Snapshot()
	switch {
	case st.Subtitle != "":
		return st.Subtitle
	case st.Title != "":
		return st.Title
	}
	return "session"
}

// blocked is why Type cannot type into the pane now, in the webview's words, or "" (rule: driver only).
func (d *approveDialog) blocked() string {
	st := d.p.conn.Snapshot()
	switch {
	case !d.has():
		return msgGone
	case st.Status != "ready":
		return msgNotConnected
	case st.Role != "driver":
		return msgFollowing
	}
	return ""
}

// openApprove opens the dialog for pane p. It sends nothing. Opening it again replaces it.
func (w *workspace) openApprove(p *pane, source func() (string, bool)) {
	w.approve = &approveDialog{p: p, source: source, focus: true}
}

// typeTarget is the pane a snippet types into: the focused pane of the tab shown, else of the session tab shown
// last (term.js paletteRunSnippet selects that tab first). It says why there is none.
func (w *workspace) typeTarget() (*pane, string) {
	t := w.tab()
	if t == nil || len(t.panes) == 0 {
		t = w.lastSession
		if t == nil || !w.hasTab(t) || len(t.panes) == 0 {
			return nil, msgNoSession
		}
	}
	p := t.panes[t.focused]
	if p == nil {
		return nil, msgNoSession
	}
	return p, ""
}

// askType is every entry point's way to type a snippet: it opens the Approve dialog for the target pane, or says
// why it cannot. It types nothing and never calls the writer.
func (w *workspace) askType(source func() (string, bool)) {
	p, why := w.typeTarget()
	if p == nil {
		w.err = why
		return
	}
	if t := w.tabOf(p); t != nil && t != w.tab() {
		w.selectTab(w.tabIndex(t)) // the approval names the pane you can see
	}
	w.openApprove(p, source)
}

// closeApprove closes the dialog without typing and gives the keyboard back to the pane.
func (w *workspace) closeApprove() {
	d := w.approve
	if d == nil {
		return
	}
	w.approve = nil
	if t := w.tabOf(d.p); t != nil && t == w.tab() {
		w.wantFocus = d.p.id
	}
}

// approveType is the Type button (and its key): the one place keel-native asks to type. It sends the bytes the
// dialog shows now, behind Ctrl-U, through the one writer; keel serve decides, and a refusal stays in the dialog.
func (w *workspace) approveType() {
	d := w.approve
	if d == nil || d.busy {
		return
	}
	if msg := d.blocked(); msg != "" {
		d.err = msg
		return
	}
	if w.tabOf(d.p) == nil {
		d.err = msgNoSession
		return
	}
	command := clearLine + d.shown()
	conn := d.p.conn
	d.busy, d.err = true, ""
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := conn.ApprovedInject(c, command)
		w.post(func() {
			d.busy = false
			switch {
			case err != nil:
				d.err = err.Error()
			case w.approve == d:
				w.closeApprove()
			}
		})
	})
}

// hasTab, tabOf and tabIndex find where a pane or tab is.
func (w *workspace) hasTab(t *tab) bool { return w.tabIndex(t) >= 0 }

func (w *workspace) tabIndex(t *tab) int {
	for i, o := range w.tabs {
		if o == t {
			return i
		}
	}
	return -1
}

func (w *workspace) tabOf(p *pane) *tab {
	for _, t := range w.tabs {
		if t.panes[p.id] == p {
			return t
		}
	}
	return nil
}
