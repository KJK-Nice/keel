package native

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
	"keel/internal/native/sshparse"
)

// The New tab page (docs/ui-redesign.md §5.3): one input, the recent host cards, the Start actions and the
// recently closed rows. The rules are lib/newtab.js and term.js (paintStartPage, openSession, submitStartInput).
// Typing filters the saved hosts; a pasted ssh command, ssh:// URL or user@host:port is parsed by sshparse and
// offered, never run. A New tab is not written to the saved workspace.

const (
	recentCards = 6 // lib/newtab.js RECENT_CARDS
	closedShown = 3 // CLOSED_SHOWN
	maxInput    = sshparse.MaxInput
)

// startPage is the state of one New tab.
type startPage struct {
	query      string
	seen       string // the query pick and showError were last reset for
	pick       string // the host id of the highlighted card
	showError  bool   // a half-typed user@host is an error only after ↵
	connecting string // the host id being opened in this tab
	focus      bool   // give the input the keyboard
	input      ui.Handle
}

func newStartTab() *tab { return &tab{panes: map[string]*pane{}, page: &startPage{focus: true}} }

// addStartTab opens a New tab and shows it.
func (w *workspace) addStartTab() {
	w.tabs = append(w.tabs, newStartTab())
	w.selectTab(len(w.tabs) - 1)
}

// settle keeps a tab in the window: with none left it shows a New tab (term.js settleTabs). It does nothing
// before the first listing has been read, or while the window says why it cannot work.
func (w *workspace) settle() {
	if len(w.tabs) == 0 && w.live && !w.closed && w.blocked == "" {
		w.addStartTab()
	}
}

const newTabLock = 300 * time.Millisecond // holding ⌘T must not open a tab per key repeat (term.js newTab)

// newTab is the + button and the palette's New tab: a New tab.
func (w *workspace) newTab() { w.addStartTab() }

// newTabKey is ⌘T, which a held key repeats: one tab per newTabLock.
func (w *workspace) newTabKey() {
	now := time.Now()
	if now.Before(w.newTabLockUntil) {
		return
	}
	w.newTabLockUntil = now.Add(newTabLock)
	w.newTab()
}

func (w *workspace) localHost() (keelclient.Host, bool) {
	for _, h := range w.hosts {
		if h.AuthType == "local" {
			return h, true
		}
	}
	return keelclient.Host{}, false
}

// hostMatches is term.js hostMatches over the fields the native Host has (no tags or folder path yet).
func hostMatches(h keelclient.Host, q string) bool {
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(h.Name+" "+h.Group+" "+h.Address+" "+h.Username), needle)
}

// pickCards is lib/newtab.js recentCards: up to six hosts. With no query they are the recent hosts and the
// local shell first; with a query, every host that matches, recent ones first.
func pickCards(hosts []keelclient.Host, recent []string, query string, local *keelclient.Host) []keelclient.Host {
	byID := map[string]keelclient.Host{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	var out []keelclient.Host
	seen := map[string]bool{}
	add := func(h keelclient.Host, ok bool) {
		if ok && !seen[h.ID] {
			seen[h.ID] = true
			out = append(out, h)
		}
	}
	q := strings.TrimSpace(query)
	if q == "" {
		for _, id := range recent {
			h, ok := byID[id]
			add(h, ok)
		}
		if local != nil && !seen[local.ID] {
			out = append([]keelclient.Host{*local}, out...)
		}
		return out[:min(len(out), recentCards)]
	}
	for _, id := range recent {
		if h, ok := byID[id]; ok && hostMatches(h, q) {
			add(h, true)
		}
	}
	for _, h := range hosts {
		if hostMatches(h, q) {
			add(h, true)
		}
	}
	return out[:min(len(out), recentCards)]
}

// cards are the cards the page shows for its query.
func (w *workspace) cards(p *startPage) []keelclient.Host {
	var local *keelclient.Host
	if h, ok := w.localHost(); ok {
		local = &h
	}
	return pickCards(w.hosts, w.recentIDs, p.query, local)
}

// ageLabel is lib/newtab.js ageLabel: "just now", "12 min ago", "3 h ago", "yesterday", "5 days ago".
func ageLabel(at, now time.Time) string {
	s := max(0, int(now.Sub(at).Seconds()))
	if s < 60 {
		return "just now"
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%d min ago", m)
	}
	h := m / 60
	if h < 24 {
		return fmt.Sprintf("%d h ago", h)
	}
	if d := h / 24; d != 1 {
		return fmt.Sprintf("%d days ago", d)
	}
	return "yesterday"
}

// closedList is the recently closed entries whose host still exists, newest first (liveClosed).
func (w *workspace) closedList() []closedEntry {
	var out []closedEntry
	for _, e := range w.recent {
		if slices.ContainsFunc(w.hosts, func(h keelclient.Host) bool { return h.ID == e.hostID }) {
			out = append(out, e)
		}
	}
	return out
}

// closedRow is the label of a recently closed row: the host's current name, or the title it had, and
// "· split pane" for a pane.
func (w *workspace) closedRow(e closedEntry) string {
	name := e.title
	for _, h := range w.hosts {
		if h.ID == e.hostID {
			name = h.Name
		}
	}
	if name == "" {
		name = "Session"
	}
	if e.kind == "pane" {
		return name + " · split pane"
	}
	return name
}

// ---- the pasted command ----

// targetText is lib/newtab.js targetText: "deploy@10.0.4.21:2222" (IPv6 gets brackets).
func targetText(r *sshparse.Result) string {
	host := r.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := ""
	if r.Port != 0 && r.Port != 22 {
		port = fmt.Sprintf(":%d", r.Port)
	}
	user := ""
	if r.User != "" {
		user = r.User + "@"
	}
	return user + host + port
}

// offer is what the page shows under the input for the typed text: a read command (OK), an error, or nothing.
// A half-typed user@host is not an error yet; ↵ makes it one (term.js startOfferHTML).
func (p *startPage) offer() (r *sshparse.Result, errText string) {
	r = sshparse.ParseConnect(p.query)
	switch {
	case r == nil:
		return nil, ""
	case r.OK:
		return r, ""
	case r.Source == "target" && !p.showError:
		return nil, ""
	}
	return nil, r.Error
}

// sync resets what the typing resets (term.js: tab.pick = "", showError = false).
func (p *startPage) sync(cards []keelclient.Host) {
	if p.query != p.seen {
		p.seen, p.pick, p.showError = p.query, "", false
	}
	if !slices.ContainsFunc(cards, func(h keelclient.Host) bool { return h.ID == p.pick }) {
		p.pick = ""
		if len(cards) > 0 {
			p.pick = cards[0].ID
		}
	}
}

// ---- opening ----

// openFrom opens h in the page's tab (term.js openSession with the tab).
func (w *workspace) openFrom(t *tab, h keelclient.Host) { w.openHostIn(h, t) }

// openHost starts a session on h in a new tab, or in the New tab that is showing (term.js fillableStartTab).
func (w *workspace) openHost(h keelclient.Host) {
	fill := w.tab()
	if fill != nil && !fill.isNew() {
		fill = nil
	}
	w.openHostIn(h, fill)
}

// openHostIn starts a session on h. It fills the New tab fill when that is still one, else it adds a tab.
func (w *workspace) openHostIn(h keelclient.Host, fill *tab) {
	w.rememberRecent(h.ID)
	if fill != nil {
		if fill.page.connecting != "" {
			return
		}
		fill.page.connecting, fill.page.pick = h.ID, h.ID
	}
	w.work(func() {
		conn, err := w.be.Spawn(h.ID, startCols, startRows)
		w.post(func() {
			if fill != nil && fill.page != nil {
				fill.page.connecting = ""
			}
			switch {
			case err != nil:
				w.fail("open "+h.Name, err)
			case w.closed:
				conn.Close()
			default:
				w.attach(fill, w.newPane(h, conn))
			}
		})
	})
}

// attach puts p in the New tab fill, which becomes a session tab; without one, or when it is gone, in a new tab.
func (w *workspace) attach(fill *tab, p *pane) {
	i := slices.Index(w.tabs, fill)
	if fill == nil || !fill.isNew() || i < 0 {
		w.addTab(p)
		return
	}
	fill.root, fill.panes, fill.focused, fill.page = layout.Leaf(p.id), map[string]*pane{p.id: p}, p.id, nil
	w.selectTab(i)
}

// openCard is ⌥1 to ⌥6 and a click on a card.
func (w *workspace) openCard(t *tab, n int) {
	if cards := w.cards(t.page); n >= 0 && n < len(cards) {
		w.openFrom(t, cards[n])
	}
}

// submitStart is ↵ in the page's input (term.js submitStartInput): a read command opens New host filled from it
// (nothing is saved or connected until Save), a refused one shows its error, and otherwise the highlighted card, or
// the first, opens.
func (w *workspace) submitStart(t *tab) {
	p := t.page
	if r := sshparse.ParseConnect(p.query); r != nil {
		p.showError = !r.OK
		if r.OK {
			w.saveAsHost(r)
		}
		return
	}
	cards := w.cards(t.page)
	for _, h := range cards {
		if h.ID == p.pick {
			w.openFrom(t, h)
			return
		}
	}
	if len(cards) > 0 {
		w.openFrom(t, cards[0])
		return
	}
	w.warn("No matching host")
}

// movePick moves the highlighted card by delta (↑ ↓ in the input), stopping at the ends.
func (w *workspace) movePick(t *tab, delta int) {
	cards := w.cards(t.page)
	i := slices.IndexFunc(cards, func(h keelclient.Host) bool { return h.ID == t.page.pick })
	if len(cards) == 0 {
		return
	}
	t.page.pick = cards[min(max(max(i, 0)+delta, 0), len(cards)-1)].ID
}
