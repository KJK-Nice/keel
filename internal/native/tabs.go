package native

import (
	"slices"
	"strings"
	"time"
)

// Tab and pane handling: focus view, closing tabs, the tab order, the recently closed
// list and tab names.

// tabOf is the tab that holds p, or nil.
func (w *workspace) tabOf(p *pane) *tab {
	for _, t := range w.tabs {
		if t.panes[p.id] == p {
			return t
		}
	}
	return nil
}

// showPane selects the tab that holds p when another is shown.
func (w *workspace) showPane(p *pane) {
	if t := w.tabOf(p); t != nil && t != w.tab() {
		w.selectTab(slices.Index(w.tabs, t))
	}
}

// dropPane ends p's session (Kill) and frees its terminal and connection. The caller removes it from its tab.
func (w *workspace) dropPane(p *pane) {
	if w.findPane == p {
		w.endFind()
	}
	w.forgetPrompts(p)
	if w.share != nil && w.share.p == p {
		w.share = nil
	}
	if p.nb != nil { // a notebook owns no session: what was typed is saved, and nothing is killed
		w.nbClosed(p)
		return
	}
	if p.remote { // a joined pane owns no session: leaving closes the peer and kills nothing
		w.release(p)
		return
	}
	if sid := p.sessionID(); sid == "" {
		if p.release != nil {
			p.release()
		}
		w.killWhenNamed(p.conn)
	} else {
		w.release(p)
		w.kill(sid)
	}
}

// removeTab takes t out of the strip. The tab before it is shown when the active one went and it was last.
func (w *workspace) removeTab(t *tab) {
	i := slices.Index(w.tabs, t)
	if i < 0 {
		return
	}
	if w.renaming == t {
		w.renaming = nil
	}
	w.tabs = slices.Delete(w.tabs, i, i+1)
	if w.active > i || w.active >= len(w.tabs) {
		w.active--
	}
	w.active = max(w.active, 0)
	if len(w.tabs) > 0 {
		w.selectTab(w.active)
	}
	w.settle()
}

// closeTab kills every pane's session and removes the tab. It is recorded once, as the tab.
func (w *workspace) closeTab(t *tab) {
	if !slices.Contains(w.tabs, t) {
		return
	}
	leaves := t.root.Leaves()
	if len(leaves) > 0 {
		w.recordClosed(t.panes[leaves[0]], "tab")
	}
	for _, id := range leaves {
		if p := t.panes[id]; p != nil {
			w.dropPane(p)
		}
	}
	t.panes = map[string]*pane{}
	w.removeTab(t)
}

// closeTabs closes these tabs (the tab menu's Close, Close others, Close to the right and Close all).
func (w *workspace) closeTabs(ts []*tab) {
	for _, t := range ts {
		w.closeTab(t)
	}
}

// Which tabs each item of the tab menu closes for tab i.
func (w *workspace) tabsOthers(t *tab) []*tab {
	return slices.DeleteFunc(slices.Clone(w.tabs), func(o *tab) bool { return o == t })
}

func (w *workspace) tabsRight(t *tab) []*tab {
	i := slices.Index(w.tabs, t)
	if i < 0 {
		return nil
	}
	return slices.Clone(w.tabs[i+1:])
}

// cycleTab shows the tab delta steps from the active one, wrapping at both ends.
func (w *workspace) cycleTab(delta int) {
	if n := len(w.tabs); n > 0 {
		w.selectTab((w.active + delta%n + n) % n)
	}
}

// moveTab puts t at the index it has among the other tabs when it lands at slot (0 to len(tabs)), counted
// in the strip as it is now. The same tab stays active.
func (w *workspace) moveTab(t *tab, slot int) {
	from := slices.Index(w.tabs, t)
	if from < 0 {
		return
	}
	cur := w.tab()
	slot = min(max(slot, 0), len(w.tabs))
	if slot > from {
		slot--
	}
	w.tabs = slices.Insert(slices.Delete(w.tabs, from, from+1), slot, t)
	w.active = max(slices.Index(w.tabs, cur), 0)
}

// toggleZoom is focus view for pane id: it fills the tab, or the tab shows all its panes again. A tab of
// one pane does not zoom. The zoomed pane has focus.
func (w *workspace) toggleZoom(t *tab, id string) {
	if t.zoomed != "" {
		t.zoomed = ""
		if t == w.tab() {
			w.wantFocus = t.focused
		}
		return
	}
	if t.panes[id] == nil || len(t.panes) < 2 {
		return
	}
	t.zoomed = id
	t.focused = id
	if t == w.tab() {
		w.wantFocus = id
	}
}

func (w *workspace) zoomFocused() {
	if t := w.tab(); t != nil {
		w.toggleZoom(t, t.focused)
	}
}

// ---- the tab's name ----

func (w *workspace) beginRename(t *tab) {
	if w.renaming != nil || t.isPage() || !slices.Contains(w.tabs, t) {
		return
	}
	w.renaming, w.renameText, w.renameSeen = t, t.name(), false
}

// endRename keeps the name typed (ok) or leaves the old one; an empty name clears the custom title.
func (w *workspace) endRename(ok bool) {
	t := w.renaming
	if t == nil {
		return
	}
	w.renaming = nil
	if ok {
		t.title = strings.TrimSpace(w.renameText)
	}
	if tt := w.tab(); tt != nil {
		w.wantFocus = tt.focused
	}
}

// ---- recently closed ----

const closedMax = 10

// closedEntry is a closed tab or pane: the host, the name and the time, nothing else.
type closedEntry struct {
	hostID, title, kind string // kind is "tab" or "pane"
	at                  time.Time
}

// recordClosed puts p first. An older entry for the same host and kind goes.
func (w *workspace) recordClosed(p *pane, kind string) {
	if p == nil || p.host.ID == "" {
		return
	}
	e := closedEntry{p.host.ID, p.host.Name, kind, time.Now()}
	rest := slices.DeleteFunc(slices.Clone(w.prefs.ClosedTabs), func(o closedEntry) bool { return o.hostID == e.hostID && o.kind == e.kind })
	w.prefs.ClosedTabs = append([]closedEntry{e}, rest...)
	if len(w.prefs.ClosedTabs) > closedMax {
		w.prefs.ClosedTabs = w.prefs.ClosedTabs[:closedMax]
	}
	w.keepClosed()
}

// keepClosed saves the recently closed list in keel-native.json (so the New tab page and
// ⌘⇧T know it after the window was closed): host ids, titles and times, off the UI thread.
func (w *workspace) keepClosed() {
	w.setPref(keyClosedTabs)
}

// reopenClosed opens the newest closed entry whose host still exists as a new session (⌘⇧T).
func (w *workspace) reopenClosed() { w.reopenClosedAt(w.tab(), 0) }

// reopenClosedAt opens entry i of the closed list (the live ones, newest first): in the New tab t when that
// is one, else in a new tab. The entry leaves the list.
func (w *workspace) reopenClosedAt(t *tab, i int) {
	list := w.closedList()
	if i < 0 || i >= len(list) {
		w.say("Nothing to reopen")
		return
	}
	e := list[i]
	w.prefs.ClosedTabs = slices.DeleteFunc(slices.Clone(w.prefs.ClosedTabs), func(o closedEntry) bool { return o == e })
	w.keepClosed()
	for _, h := range w.hosts {
		if h.ID == e.hostID {
			if t != nil && t.isNew() {
				w.openHostIn(h, t)
			} else {
				w.openHost(h)
			}
			return
		}
	}
}

// ---- recent hosts ----

const recentMax = 12

// rememberRecent puts a host first in the recent hosts and keeps the list in keel-native.json: ids only.
func (w *workspace) rememberRecent(id string) {
	if id == "" {
		return
	}
	ids := append([]string{id}, slices.DeleteFunc(slices.Clone(w.prefs.RecentHosts), func(o string) bool { return o == id })...)
	w.prefs.RecentHosts = ids[:min(len(ids), recentMax)]
	w.setPref(keyRecentHosts)
}

// ---- terminal text size ----

const (
	termSizeMin, termSizeMax, termSizeDefault = 10, 22, 12
)

// setTermSize sets the terminal font size of every pane, clamped, and keeps it in keel-native.json.
func (w *workspace) setTermSize(n int) {
	n = min(max(n, termSizeMin), termSizeMax)
	if n == w.prefs.TermSize {
		return
	}
	w.prefs.TermSize = n
	for _, t := range w.tabs {
		for _, p := range t.panes {
			p.fontSize = float32(n)
			if p.setFont != nil {
				p.setFont(float32(n))
			}
		}
	}
	w.setPref(keyTermSize)
}
