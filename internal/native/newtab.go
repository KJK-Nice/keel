package native

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
	"keel/internal/native/sshparse"
)

// The New tab page (docs/ui-redesign.md §5.3): one input, the recent host cards, the Start actions and the
// recently closed rows.
// Typing filters the saved hosts; a pasted ssh command, ssh:// URL or user@host:port is parsed by sshparse and
// offered, never run. A New tab is not written to the saved workspace.

const (
	recentCards = 6
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
	alt        altPress
}

// altPress is the page as ⌥1 to ⌥6 found it: on a Mac the key's character ("™" for ⌥2) reaches the input before the
// shortcut runs, and would filter the cards the number stands for.
type altPress struct {
	cards []keelclient.Host
	query string
	at    time.Time
	done  bool // a card was opened for this press
}

const altPressFor = 500 * time.Millisecond

func (a altPress) fresh() bool { return !a.at.IsZero() && time.Since(a.at) < altPressFor }

func newStartTab() *tab { return &tab{panes: map[string]*pane{}, start: &startPage{focus: true}} }

// addStartTab opens a New tab and shows it.
func (w *workspace) addStartTab() {
	w.tabs = append(w.tabs, newStartTab())
	w.selectTab(len(w.tabs) - 1)
}

// settle keeps a tab in the window: with none left it shows a New tab. It does nothing
// before the first listing has been read, or while the window says why it cannot work.
func (w *workspace) settle() {
	if len(w.tabs) == 0 && w.live && !w.closed && w.blocked == "" {
		w.addStartTab()
	}
}

const newTabLock = 300 * time.Millisecond // holding ⌘T must not open a tab per key repeat

// newTabKey is ⌘T, which a held key repeats: one tab per newTabLock.
func (w *workspace) newTabKey() {
	now := time.Now()
	if now.Before(w.newTabLockUntil) {
		return
	}
	w.newTabLockUntil = now.Add(newTabLock)
	w.addStartTab()
}

func (w *workspace) localHost() (keelclient.Host, bool) {
	for _, h := range w.hosts {
		if h.IsLocal() {
			return h, true
		}
	}
	return keelclient.Host{}, false
}

// hostMatches: the query is one needle, looked for in the host's name, group, the names of every
// folder above it, address, user and tags.
func hostMatches(groups []keelclient.Group, h keelclient.Host, q string) bool {
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return true
	}
	return strings.Contains(hostSearchHay(groups, h), needle)
}

// pickCards is up to six hosts. With no query they are the recent hosts and the
// local shell first; with a query, every host that matches, recent ones first.
func pickCards(groups []keelclient.Group, hosts []keelclient.Host, recent []string, query string, local *keelclient.Host) []keelclient.Host {
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
		if h, ok := byID[id]; ok && hostMatches(groups, h, q) {
			add(h, true)
		}
	}
	for _, h := range hosts {
		if hostMatches(groups, h, q) {
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
	return pickCards(w.folders, w.hosts, w.prefs.RecentHosts, p.query, local)
}

// ageLabel is "just now", "12 min ago", "3 h ago", "yesterday", "5 days ago".
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
	for _, e := range w.prefs.ClosedTabs {
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
	if h, ok := hostByID(w.hosts, e.hostID); ok {
		name = h.Name
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

// targetText is "deploy@10.0.4.21:2222" (IPv6 gets brackets).
func targetText(r *sshparse.Result) string {
	host := keelclient.BracketIPv6(r.Host)
	port := ""
	if r.Port != 0 && r.Port != limits.DefaultPort {
		port = fmt.Sprintf(":%d", r.Port)
	}
	user := ""
	if r.User != "" {
		user = r.User + "@"
	}
	return user + host + port
}

// offer is what the page shows under the input for the typed text: a read command (OK), an error, or nothing.
// A half-typed user@host is not an error yet; ↵ makes it one.
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

// sync resets the pick and the error when the typing changes.
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

// openHost starts a session on h in a new tab, or in the New tab that is showing.
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
		if fill.start.connecting != "" {
			return
		}
		fill.start.connecting, fill.start.pick = h.ID, h.ID
	}
	w.work(func() {
		conn, err := w.be.Spawn(h.ID, startCols, startRows)
		w.post(func() {
			if fill != nil && fill.isNew() {
				fill.start.connecting = ""
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
	fill.root, fill.panes, fill.focused, fill.start = layout.Leaf(p.id), map[string]*pane{p.id: p}, p.id, nil
	w.selectTab(i)
}

// openCard is ⌥1 to ⌥6 and a click on a card.
func (w *workspace) openCard(t *tab, n int) {
	if !t.isNew() { // the shortcut was registered while t was the page; an earlier key turned t into a session tab
		return
	}
	cards := w.cards(t.start)
	if a := &t.start.alt; a.fresh() { // the page saw this key too: one press opens one card, whichever path comes second
		if a.done {
			return
		}
		a.done, cards = true, a.cards
	}
	if n >= 0 && n < len(cards) {
		w.openHostIn(cards[n], t)
	}
}

// submitStart is ↵ in the page's input: a read command opens New host filled from it
// (nothing is saved or connected until Save), a refused one shows its error, and otherwise the highlighted card, or
// the first, opens.
func (w *workspace) submitStart(t *tab) {
	p := t.start
	if r := sshparse.ParseConnect(p.query); r != nil {
		p.showError = !r.OK
		if r.OK {
			w.saveAsHost(r)
		}
		return
	}
	cards := w.cards(t.start)
	for _, h := range cards {
		if h.ID == p.pick {
			w.openHostIn(h, t)
			return
		}
	}
	if len(cards) > 0 {
		w.openHostIn(cards[0], t)
		return
	}
	w.warn("No matching host")
}

// movePick moves the highlighted card by delta (↑ ↓ in the input), stopping at the ends.
func (w *workspace) movePick(t *tab, delta int) {
	cards := w.cards(t.start)
	i := slices.IndexFunc(cards, func(h keelclient.Host) bool { return h.ID == t.start.pick })
	if len(cards) == 0 {
		return
	}
	t.start.pick = cards[min(max(max(i, 0)+delta, 0), len(cards)-1)].ID
}
