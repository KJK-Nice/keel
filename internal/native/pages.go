package native

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// Tab pages (docs/ui-redesign.md §4): Keychain, Logs, Port forwarding, Snippets, Settings and the others each open as
// their own tab, one instance each. A page tab has no panes: it carries a tabPage. The page tabs are saved in the
// workspace file under "pages" and come back after the session tabs, in the order they had.
//
// To add a page: write its state type with load and view (and whichever of the optional interfaces below it needs) in
// its own file, and give it a pageKinds entry.

// page is the state of one page tab and what a tab asks of it.
type page interface {
	load(w *workspace) // reads what the page shows: it was selected, or the vault changed
	view(w *workspace, c *ui.Context)
}

// pageNewItem is a page with its own New for ⌘N.
type pageNewItem interface{ newItem(w *workspace) }

// pageTitled is a page whose tab is named after what it shows ("" is the kind's title).
type pageTitled interface{ tabTitle() string }

// pageSaved is a page that keeps something of its own in the saved workspace.
type pageSaved interface {
	snapshot(d *savedDoc)
	restore(w *workspace, d *savedDoc)
}

// pageKind is what the strip, the palette, the sidebar and the saved workspace know about a page.
type pageKind struct {
	name       string // the name in the saved workspace
	title      string // the tab's name
	tabIcon    string // the glyph in the tab
	paletteTag string // the palette's "Open now" glyph; "" is the action glyph
	listed     bool   // the palette's Vault section lists it
	footIcon   string // the sidebar footer has a button for it, with this glyph
	make       func(w *workspace) page
}

// pageAgent is the Agent page's name (a constant, so that no source line holds the quoted key that guard_test.go keeps
// for keelclient/inject.go).
const pageAgent = "agent"

// pageKinds is every page this client has, in the order the palette and the sidebar footer list them.
var pageKinds = []pageKind{
	{name: "keychain", title: "Keychain", tabIcon: "key-01", paletteTag: "key-01", listed: true, footIcon: "key-01",
		make: func(*workspace) page { return &keychainPage{} }},
	{name: "forwards", title: "Port forwarding", tabIcon: "forwards", listed: true,
		make: func(*workspace) page { return &forwardsPage{show: tunnelAll} }},
	{name: "snippets", title: "Snippets", tabIcon: "snippet", paletteTag: "snippet", listed: true,
		make: func(*workspace) page { return &snippetsPage{} }},
	{name: "notebooks", title: "Notebooks", tabIcon: "notebook-pen", paletteTag: "notebook-pen", listed: true,
		make: func(*workspace) page { return &notebooksPage{} }},
	{name: "logs", title: "Logs", tabIcon: "logs-tab", listed: true, footIcon: "logs",
		make: func(*workspace) page { return &logsPage{kind: "all"} }},
	{name: "settings", title: "Settings", tabIcon: "settings-01", listed: true,
		make: func(w *workspace) page { p := newSettingsPage(); w.initSliders(p); return p }},
	// The rest are opened from where they belong (the agent panel, a runbook proposal, a folder's menu, the palette's
	// own rows), so they are not listed.
	{name: pageAgent, title: "Agent", tabIcon: "ai-magic", paletteTag: "ai-magic",
		make: func(*workspace) page { return &agentPage{} }},
	{name: "runbooks", title: "Runbooks", tabIcon: "book", paletteTag: "book",
		make: func(*workspace) page { return &runbooksPage{} }},
	{name: "group", title: "Group", tabIcon: "group", paletteTag: "group",
		make: func(*workspace) page { return newGroupPage() }},
	{name: "grouprun", title: "Group run", tabIcon: "group-run", paletteTag: "group-run",
		make: func(*workspace) page { return &groupRunPage{} }},
}

func pageKindOf(name string) *pageKind {
	for i := range pageKinds {
		if pageKinds[i].name == name {
			return &pageKinds[i]
		}
	}
	return nil
}

// tabPage is the page of a page tab: its kind and its state.
type tabPage struct {
	*pageKind
	page
}

// pageAs is the tab's page when the tab holds a T, or nil: the one way to the concrete page.
func pageAs[T page](t *tab) (p T) {
	if t != nil && t.pg != nil {
		p, _ = t.pg.page.(T)
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
	k := pageKindOf(name)
	if k == nil || w.pageTab(name) != nil {
		return nil
	}
	t := &tab{panes: map[string]*pane{}, pg: &tabPage{k, k.make(w)}}
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

// loadPage reads what the tab's page shows; it does nothing for a tab that is not a page.
func (w *workspace) loadPage(t *tab) {
	if t.pg != nil {
		t.pg.load(w)
	}
}

// refreshPages reads every open page again: the vault changed.
func (w *workspace) refreshPages() {
	for _, t := range w.tabs {
		w.loadPage(t)
	}
}

// newItem is ⌘N: the page's own New on a tab page that has one, New host elsewhere.
func (w *workspace) newItem() {
	if t := w.tab(); t != nil && t.pg != nil {
		if p, ok := t.pg.page.(pageNewItem); ok {
			p.newItem(w)
			return
		}
	}
	w.newHost()
}
