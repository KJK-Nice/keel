package native

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// Tab pages (docs/ui-redesign.md §4): Keychain, Logs, and later Forwards, Snippets and Settings each open as their
// own tab, one instance each. A page tab has no panes: it carries a tabPage, which holds the page's state in one
// type per page. The mechanism is term.js openPage; the page tabs are saved in the workspace file under "pages"
// and come back after the session tabs, in the order they had (lib/savedworkspace.js).
//
// To add a page: give it a pageDefs entry, a state type and a view, and set it in newTabPage, loadPage and
// pageView.

// pageDef is what the strip, the palette and the saved workspace know about a page.
type pageDef struct {
	title      string // the tab's name (term.js PAGES)
	tabIcon    string // the glyph in the tab (PAGES glyph)
	paletteTag string // the palette's "Open now" glyph (PALETTE_PAGE_ICONS); "" is the action glyph
}

var pageDefs = map[string]pageDef{
	"keychain": {title: "Keychain", tabIcon: "key-01", paletteTag: "key-01"},
	"logs":     {title: "Logs", tabIcon: "logs-tab"},
}

// pageNames is every page this client has, in the order the sidebar and the palette list them.
var pageNames = []string{"keychain", "logs"}

// tabPage is the state of a page tab: the name and the page's own state (only the one for that name is set).
type tabPage struct {
	name     string
	keychain *keychainPage
	logs     *logsPage
}

func newTabPage(name string) *tabPage {
	p := &tabPage{name: name}
	switch name {
	case "keychain":
		p.keychain = &keychainPage{}
	case "logs":
		p.logs = &logsPage{kind: "all"}
	}
	return p
}

// isPage reports a page tab (Keychain, Logs…), as opposed to a session tab or the New tab page.
func (t *tab) isPage() bool { return t.pg != nil }

// pageTab is the open tab for a page, or nil.
func (w *workspace) pageTab(name string) *tab {
	for _, t := range w.tabs {
		if t.pg != nil && t.pg.name == name {
			return t
		}
	}
	return nil
}

// addPageTab appends a page's tab without showing or reading it. It does nothing for a name this client does not
// have, or a page that is already open.
func (w *workspace) addPageTab(name string) *tab {
	if _, ok := pageDefs[name]; !ok || w.pageTab(name) != nil {
		return nil
	}
	t := &tab{panes: map[string]*pane{}, pg: newTabPage(name)}
	w.tabs = append(w.tabs, t)
	return t
}

// openPage opens a page's tab, or selects it when it is open already: one instance each.
func (w *workspace) openPage(name string) {
	if t := w.pageTab(name); t != nil {
		w.selectTab(slices.Index(w.tabs, t))
		return
	}
	if w.addPageTab(name) != nil {
		w.selectTab(len(w.tabs) - 1)
	}
}

// loadPage reads what the page shows.
func (w *workspace) loadPage(t *tab) {
	switch {
	case t.pg == nil:
	case t.pg.keychain != nil:
		w.loadIdentities()
	case t.pg.logs != nil:
		w.loadLogs()
	}
}

// refreshPages reads every open page again: the vault changed, or a page was selected.
func (w *workspace) refreshPages() {
	for _, t := range w.tabs {
		w.loadPage(t)
	}
}

// newItem is ⌘N: the page's own New on a tab page, New host elsewhere (term.js newItem).
func (w *workspace) newItem() {
	if t := w.tab(); t != nil && t.pg != nil && t.pg.keychain != nil {
		w.newIdentity()
		return
	}
	w.newHost()
}

// pageView is the stage for a page tab.
func (w *workspace) pageView(c *ui.Context, t *tab) {
	switch {
	case t.pg.keychain != nil:
		w.keychainView(c, t.pg.keychain)
	case t.pg.logs != nil:
		w.logsView(c, t.pg.logs)
	}
}

// sideEmpty is k-side-empty: a title and a line under it, centred.
func sideEmpty(c *ui.Context, title, desc string) {
	ui.Column(c).FillWidth().Padding(space6, space2).Gap(space1).AlignItems(ui.Center).Children(func() {
		bodyStrong(c, title, ink)
		ui.Text(c, desc).TextColor(inkMuted).FontSize(captionSize).Center()
	})
}
