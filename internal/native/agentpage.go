package native

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Agent tab page (docs/ui-redesign.md §5.4.2). The feature is cmd/keel/web/js/ui/agentpage.js, internal/ui/agentpage.templ
// and the k-agentpage* rules of keel.css; its words and rules are lib/agent.js (agentconvlogic.go).
//
// The page has no conversation of its own. It shows the window's one conversation (agent.go) in a second thread, so every
// change (resume, ask, rename, delete, Propose again, Type) goes through that conversation and the panel shows the same
// thing. Like the panel it never writes to a pane: a card's Type goes to typeAgentProposal, as in the panel.
//
// Not built, as the app does not build them (§5.4.2 "Not built"): a run mode, pinning, sharing a conversation, exporting
// one, and search inside a conversation. The composer has the notebook chips and the Attach notebook picker of the panel
// (agentnotebooksview.go), and the thread the notebook cards.

const (
	pageWideMin = 960 // size-agentpage-list + size-agentpage-context + size-agent: below it the context goes under the list
	pageMidMin  = 660 // size-agentpage-list + size-agent: below it the two columns share the width
	hostChipMax = 6   // the host chips: All and up to six hosts
)

// agentPage is the state of the Agent tab: only what the page itself holds. No conversation text is kept here: the
// conversation is the window's (agentPanel), read from keel serve into memory.
type agentPage struct {
	filter  string
	filterH ui.Handle
	host    string // the host chip that is on; "" is All

	input      string
	inputH     ui.Handle
	focusInput bool

	renaming   bool // the heading is a field
	renameText string
	renameH    ui.Handle
	renameWas  bool

	scroll, listScroll, ctxScroll ui.ScrollState
	seen                          int     // turns and cards drawn last frame: the thread follows when it grew
	width                         float32 // the tab's width last frame, for the narrow layouts
}

func (p *agentPage) load(w *workspace) {
	w.loadAgent()
	w.loadConversations()
}

func (p *agentPage) view(w *workspace, c *ui.Context) { w.agentPageView(c, p) }
func (p *agentPage) newItem(w *workspace)             { w.newAgentConversation() }

// openAgentPage is the palette's Agent conversations and the panel's Open all in a tab.
func (w *workspace) openAgentPage() { w.openPage(pageAgent) }

// agentLayout is how the three columns sit for a tab of this width (the app's container queries).
type agentLayout int

const (
	layoutWide   agentLayout = iota // list, thread, context
	layoutMid                       // the context under the list, the thread beside both
	layoutNarrow                    // the same, the two columns sharing the width 2:3
)

func pageLayout(width float32) agentLayout {
	switch {
	case width <= 0 || width > pageWideMin:
		return layoutWide
	case width > pageMidMin:
		return layoutMid
	}
	return layoutNarrow
}

// pageRetention is the list's footnote: where the conversations are and for how long.
func (a *agentPanel) pageRetention() string {
	d := a.historyDays()
	switch {
	case d == nil:
		return ""
	case *d <= 0:
		return "Not saved · kept while open"
	case a.list.saveError != "":
		return "Not saved: " + a.list.saveError
	}
	return "Saved encrypted in the vault · " + strings.ToLower(retentionNote(*d))
}

// openRetentionSettings is Retention settings: Settings · Agent.
func (w *workspace) openRetentionSettings() {
	w.openPage("settings")
	if p := pageAs[*settingsPage](w.pageTab("settings")); p != nil {
		p.section, p.search = "agent", ""
		w.loadAgentSettings()
	}
}

// pageNewConversation is New conversation on the page: ⌘N while it is the tab, and its + button.
func (w *workspace) pageNewConversation(p *agentPage) {
	w.newAgentConversation()
	w.agent.focusInput = false
	p.focusInput, p.renaming = true, false
}

// pageContextPane is a pane in scope as the context column names it.
func (w *workspace) pageScopePanes() (out []agentPane) {
	for _, id := range w.agent.scope {
		if p := w.agentPaneBySession(id); p != nil {
			out = append(out, *p)
		}
	}
	return out
}

// pageProposals are the conversation's proposals once each, in order, with their state.
type pageProp struct {
	p     keelclient.Proposal
	typed bool
}

func (a *agentPanel) pageProposals() (out []pageProp) {
	seen := map[string]bool{}
	for _, t := range a.turns {
		for _, id := range t.props {
			r := a.props[id]
			if r == nil || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, pageProp{p: r.p, typed: r.status != nil && r.status.ok})
		}
	}
	return out
}

// pageHeadMeta is the meta line under the heading: up to four hosts and when it started.
func (a *agentPanel) pageHeadMeta() (hosts []string, when string) {
	hosts = conversationHosts(a.convTurns())
	if len(hosts) > 4 {
		hosts = hosts[:4]
	}
	q := a.questions()
	if q == 0 {
		return hosts, "No questions yet"
	}
	at := a.createdAt
	if at.IsZero() && len(a.turns) > 0 {
		at = a.turns[0].at
	}
	return hosts, startedLabel(at, q, time.Now())
}
