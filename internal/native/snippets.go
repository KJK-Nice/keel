package native

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Snippets (docs/ui-redesign.md §5.12): saved commands. What the app has, and so what this has: a name and a command
// (one per line; each line ends with Enter when typed), the Snippets page with its list and editor, New and Edit
// snippet dialogs, the sidebar's Snippets section and the palette's Snippets group. The words are SnippetsPage and
// SnippetList (internal/ui/pages.templ, lists.templ), snippetDialog (dialogs.templ), the snippet menus
// (rowmenus.templ) and the handlers in cmd/keel/web/js/term.js.
//
// Not in the app and so not here: {{var}} fields, "Offer on hosts tagged", the Read-only / Changes state kind and the
// ⇧↵ rule. The editor has "Let the agent suggest this snippet" and the preview of what gets typed (plan 025): the page's
// Save sends the switch as the app's form does; the dialogs send no flag, so keel serve keeps it.
//
// Nothing here writes to a session. Every entry point (a row, the row menu, the editor's "Type into pane…", the palette)
// calls typeSnippet, which hands the snippet to typeNow (typenow.go): a read-only one-liner keel serve agrees to is typed
// without the dialog and without Enter, any other opens the Approve before type dialog (approve.go).

// snippetsPage is the state of the Snippets tab.
type snippetsPage struct {
	filter   string
	selected string // the snippet id; the first one while this is not in the list
	filterH  ui.Handle
	list     ui.ListState // the list's keyboard choice (Up, Down, Home, End), MyGo's ui.List
	choice   int          // the row of list.Selected
	ed       snippetEditor
}

func (p *snippetsPage) load(w *workspace)                { w.reloadSnippets() }
func (p *snippetsPage) view(w *workspace, c *ui.Context) { w.snippetsView(c, p) }
func (p *snippetsPage) newItem(w *workspace)             { w.newSnippet() }

// snippetEditor is the editor beside the list: the fields of the selected snippet.
type snippetEditor struct {
	id                    string // the snippet the fields were read from
	name, command         string
	origName, origCommand string // what they were read as: the editor is dirty when they differ
	agent, origAgent      bool   // "Let the agent suggest this snippet"
	err                   string
	saving                bool
	nameH, cmdH           ui.Handle
}

func (e *snippetEditor) dirty() bool {
	return e.name != e.origName || e.command != e.origCommand || e.agent != e.origAgent
}

// snippetPreview is lib/vaultview.js snippetPreview: the lines a snippet types, each with its Enter, and the count line.
func snippetPreview(text string) (lines []string, summary string) {
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" {
		lines = strings.Split(text, "\n")
		lines = lines[:len(lines)-1]
	}
	return lines, countWord(len(lines), "line", "lines") + " · " + countWord(len(text), "byte", "bytes") + ". Typing always goes through Approve before type."
}

// snippetFirstLine is lists.templ snippetFirstLine: the first line, with " …" when there are more.
func snippetFirstLine(s keelclient.Snippet) string {
	line := strings.TrimSpace(s.Command)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		return strings.TrimSpace(line[:i]) + " …"
	}
	return line
}

// paletteHint is term.js paletteCatalog's hint: the first line of the command.
func paletteHint(s keelclient.Snippet) string {
	return strings.Split(strings.TrimSpace(s.Command), "\n")[0]
}

// snippetName is menus.go sideSnippetName: a snippet with no name is "Snippet".
func snippetName(s keelclient.Snippet) string {
	if s.Name == "" {
		return "Snippet"
	}
	return s.Name
}

// snippetMatches is the filter of the page and of the sidebar: name and command, any case.
func snippetMatches(s keelclient.Snippet, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	return q == "" || strings.Contains(strings.ToLower(s.Name+" "+s.Command), q)
}

func (w *workspace) snippet(id string) (keelclient.Snippet, bool) {
	for _, s := range w.snippets {
		if s.ID == id {
			return s, true
		}
	}
	return keelclient.Snippet{}, false
}

// shownSnippets is the page's list under its filter.
func (w *workspace) shownSnippets(q string) (out []keelclient.Snippet) {
	for _, s := range w.snippets {
		if snippetMatches(s, q) {
			out = append(out, s)
		}
	}
	return out
}

// current is the selected snippet, else the first (term.js syncSnippetPage).
func (w *workspace) currentSnippet(p *snippetsPage) (keelclient.Snippet, bool) {
	if s, ok := w.snippet(p.selected); ok {
		return s, true
	}
	if len(w.snippets) > 0 {
		return w.snippets[0], true
	}
	return keelclient.Snippet{}, false
}

// sync reads the selected snippet into the editor, unless the user has edited this one (term.js syncSnippetPage:
// "form.dataset.snippetId !== sn.id || !form.dataset.dirty"). The command is shown without its last newline.
func (e *snippetEditor) sync(s keelclient.Snippet) {
	if e.id == s.ID && e.dirty() {
		return
	}
	if e.id != s.ID {
		e.err = ""
	}
	e.id, e.name, e.command = s.ID, s.Name, strings.TrimSuffix(s.Command, "\n")
	e.origName, e.origCommand = e.name, e.command
	e.agent, e.origAgent = s.AgentUsable, s.AgentUsable
}

// typeSnippet is "Type into pane…", a row, the palette (term.js paletteRunSnippet). A one-line, plainly read-only snippet
// keel serve agrees to is typed with no dialog and no Enter; every other one opens Approve before type for the snippet as
// it is saved. Both go through typeNow, which is the only thing here that types.
func (w *workspace) typeSnippet(id string) {
	sn, ok := w.snippet(id)
	if !ok {
		return
	}
	p, why := w.typeTarget()
	if p == nil {
		w.say(why)
		return
	}
	source := func() (string, bool) {
		s, ok := w.snippet(id)
		return s.Command, ok
	}
	w.typeNow(p, typeSource{
		line:      snippetLine(sn.Command),
		snippetID: id,
		dialog:    func() { w.openApprove(p, source) },
	})
}

// ---- saving ----

// saveEditor is Save (⌘S) on the page: term.js posts the editor to /ui/forms/snippet-editor, which trims the name
// and ends the command with a newline (httpapi forms.go).
func (w *workspace) saveEditor() {
	t := w.pageTab("snippets")
	if t == nil {
		return
	}
	p := pageAs[*snippetsPage](t)
	e := &p.ed
	if e.saving || e.id == "" {
		return
	}
	agent := e.agent // the app's form always posts the switch (pages.templ: name="agentUsable")
	in := keelclient.SnippetInput{ID: e.id, Name: strings.TrimSpace(e.name), Command: e.command, AgentUsable: &agent}
	if !strings.HasSuffix(in.Command, "\n") {
		in.Command += "\n"
	}
	e.saving, e.err = true, ""
	call(w, func(c context.Context) (keelclient.Snippet, error) { return w.be.SaveSnippet(c, in) }, func(saved keelclient.Snippet, err error) {
		e.saving = false
		if err != nil {
			e.err = hostError(err)
			return
		}
		if e.id == saved.ID { // what is saved is what the editor holds now
			e.name, e.command, e.agent = saved.Name, strings.TrimSuffix(saved.Command, "\n"), saved.AgentUsable
			e.origName, e.origCommand, e.origAgent = e.name, e.command, e.agent
		}
		w.say("Snippet saved")
		w.reloadSnippets()
	})
}

// snippetDialog is New snippet / Edit snippet (snippetDialog in dialogs.templ): a name and a command.
type snippetDialog struct {
	id            string // "" for New snippet
	name, command string
	err           string
	saving        bool
	focus         bool

	nameH, cmdH ui.Handle
}

func (w *workspace) newSnippet() { w.snipDlg = &snippetDialog{focus: true} }

// editSnippet is the row menu's Edit: the dialog holds the command as saved, newline and all.
func (w *workspace) editSnippet(s keelclient.Snippet) {
	w.snipDlg = &snippetDialog{id: s.ID, name: s.Name, command: s.Command, focus: true}
}

func (w *workspace) closeSnippetDialog() { w.snipDlg = nil }

// saveSnippetDialog is Save snippet: /ui/forms/snippet trims the name and ends a command with a newline.
func (w *workspace) saveSnippetDialog() {
	d := w.snipDlg
	if d == nil || d.saving {
		return
	}
	in := keelclient.SnippetInput{ID: d.id, Name: strings.TrimSpace(d.name), Command: d.command}
	if in.Command != "" && !strings.HasSuffix(in.Command, "\n") {
		in.Command += "\n"
	}
	if in.Name == "" || (d.id == "" && strings.TrimSpace(in.Command) == "") { // httpapi saveSnippet's own words
		d.err = "name and command are required"
		return
	}
	d.saving, d.err = true, ""
	call(w, func(c context.Context) (keelclient.Snippet, error) { return w.be.SaveSnippet(c, in) }, func(saved keelclient.Snippet, err error) {
		d.saving = false
		if err != nil {
			d.err = hostError(err)
			return
		}
		if w.snipDlg == d {
			w.closeSnippetDialog()
		}
		if t := w.pageTab("snippets"); t != nil && d.id == "" {
			pageAs[*snippetsPage](t).selected = saved.ID
		}
		w.say("Snippet saved")
		w.reloadSnippets()
	})
}

// ---- delete ----

// askDeleteSnippet is Delete: it asks first, with the webview's words (term.js: askConfirm `Delete snippet “name”?`).
func (w *workspace) askDeleteSnippet(s keelclient.Snippet) {
	w.askConfirm(&hostConfirm{title: "Confirm", message: "Delete snippet “" + s.Name + "”?", action: "Remove", run: func() { w.deleteSnippet(s) }})
}

func (w *workspace) deleteSnippet(s keelclient.Snippet) {
	callErr(w, func(c context.Context) error { return w.be.DeleteSnippet(c, s.ID) }, func(err error) {
		if err != nil {
			w.fail("delete snippet", err)
			return
		}
		w.reloadSnippets()
	})
}
