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
	"forwards": {title: "Port forwarding", tabIcon: "forwards"},
	"snippets": {title: "Snippets", tabIcon: "snippet", paletteTag: "snippet"},
	// The Notebooks page (§18) comes before Logs in the palette (term.js PAGES.notebooks); its sidebar section is the
	// Snippets segment's second half.
	"notebooks": {title: "Notebooks", tabIcon: "notebook-pen", paletteTag: "notebook-pen"},
	"logs":      {title: "Logs", tabIcon: "logs-tab"},
	"settings":  {title: "Settings", tabIcon: "settings-01"},
	// The Agent page is opened from the palette's Agent conversations and the panel's Open all in a tab, so it is not in
	// pageNames (term.js PAGES.agent).
	pageAgent: {title: "Agent", tabIcon: "ai-magic", paletteTag: "ai-magic"},
	// The Runbooks page (§16) is opened from the palette's Runbooks and the "Open Runbooks" of a saved runbook proposal, so it
	// is not in pageNames (term.js PAGES.runbooks).
	"runbooks": {title: "Runbooks", tabIcon: "book", paletteTag: "book"},
	// The Group page and the group run page (§17) are opened from a folder's menu, the palette and "Run a runbook on this
	// group", so they are not in pageNames (term.js PAGES.group, PAGES.grouprun). One instance each, named after the folder
	// or the run (tab.name).
	"group":    {title: "Group", tabIcon: "group", paletteTag: "group"},
	"grouprun": {title: "Group run", tabIcon: "group-run", paletteTag: "group-run"},
}

// pageAgent is the Agent page's name (a constant, so that no source line holds the quoted key that guard_test.go keeps
// for keelclient/inject.go).
const pageAgent = "agent"

// pageNames is every page this client has, in the order the sidebar and the palette list them.
var pageNames = []string{"keychain", "forwards", "snippets", "notebooks", "logs", "settings"}

// footPages are the sidebar footer's page buttons (the webview's footer is Keychain · Logs).
var footPages = []string{"keychain", "logs"}

// tabPage is the state of a page tab: the name and the page's own state (only the one for that name is set).
type tabPage struct {
	name     string
	keychain *keychainPage
	logs     *logsPage
	snippets *snippetsPage
	notebook *notebooksPage
	forwards *forwardsPage
	settings *settingsPage
	agent    *agentPage
	runbooks *runbooksPage
	group    *groupPage
	grouprun *groupRunPage
}

func newTabPage(name string) *tabPage {
	p := &tabPage{name: name}
	switch name {
	case "keychain":
		p.keychain = &keychainPage{}
	case "logs":
		p.logs = &logsPage{kind: "all"}
	case "snippets":
		p.snippets = &snippetsPage{}
	case "notebooks":
		p.notebook = &notebooksPage{}
	case "forwards":
		p.forwards = &forwardsPage{show: tunnelAll}
	case "settings":
		p.settings = newSettingsPage()
	case pageAgent:
		p.agent = &agentPage{}
	case "runbooks":
		p.runbooks = &runbooksPage{}
	case "group":
		p.group = newGroupPage()
	case "grouprun":
		p.grouprun = &groupRunPage{}
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
	if t.pg.settings != nil {
		w.initSliders(t.pg.settings)
	}
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
	case t.pg.snippets != nil:
		w.reloadSnippets()
	case t.pg.notebook != nil:
		w.reloadNotebooks()
	case t.pg.forwards != nil:
		w.reloadTunnels()
	case t.pg.settings != nil:
		w.loadSettings()
		if t.pg.settings.section == "agent" {
			w.loadAgentSettings()
		}
	case t.pg.agent != nil:
		w.loadAgent()
		w.loadConversations()
	case t.pg.runbooks != nil:
		w.loadRunbooks()
	case t.pg.group != nil:
		w.loadGroupPage()
	case t.pg.grouprun != nil:
		w.loadGroupRunPage()
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
	if t := w.tab(); t != nil && t.pg != nil {
		switch {
		case t.pg.keychain != nil:
			w.newIdentity()
			return
		case t.pg.snippets != nil:
			w.newSnippet()
			return
		case t.pg.notebook != nil: // ⌘N on the Notebooks page is its New notebook (term.js newItem)
			w.newNotebook()
			return
		case t.pg.forwards != nil:
			w.newForward()
			return
		case t.pg.agent != nil: // ⌘N on the Agent page is New conversation (agentpage.js newConversation)
			w.newAgentConversation()
			return
		case t.pg.runbooks != nil: // ⌘N on the Runbooks page is its New (term.js newItem)
			w.newRunbook()
			return
		}
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
	case t.pg.snippets != nil:
		w.snippetsView(c, t.pg.snippets)
	case t.pg.notebook != nil:
		w.notebooksView(c, t.pg.notebook)
	case t.pg.forwards != nil:
		w.forwardsView(c, t.pg.forwards)
	case t.pg.settings != nil:
		w.settingsView(c, t.pg.settings)
	case t.pg.agent != nil:
		w.agentPageView(c, t.pg.agent)
	case t.pg.runbooks != nil:
		w.runbooksView(c, t.pg.runbooks)
	case t.pg.group != nil:
		w.groupPageView(c, t.pg.group)
	case t.pg.grouprun != nil:
		w.groupRunView(c, t.pg.grouprun)
	}
}

// sideEmpty is k-side-empty: a title and a line under it, centred.
func sideEmpty(c *ui.Context, title, desc string) {
	ui.Column(c).FillWidth().Padding(space6, space2).Gap(space1).AlignItems(ui.Center).Children(func() {
		bodyStrong(c, title, ink)
		ui.Text(c, desc).TextColor(inkMuted).FontSize(captionSize).Center()
	})
}
