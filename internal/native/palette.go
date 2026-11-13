package native

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"unicode"

	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

// The command palette (docs/ui-redesign.md §5.2). The model here follows cmd/keel/web/js/lib/palette.js
// (groups, matching, ordering, cursor, highlight) and the item list of term.js paletteCatalog: titles,
// hints and key caps are the webview's. Choosing an item never types into a session: a snippet opens Approve before type
// (approve.go). The last row, "Ask the agent" (⌘↵), opens the agent panel with the query typed and sends nothing.

// groupOrder is lib/palette.js GROUP_ORDER.
var groupOrder = []string{"Open now", "Hosts", "Actions", "Snippets"}

// palItem is one row. run gets split = true for ⌥↵ (open in a split); rows with no split form ignore it.
type palItem struct {
	group, title, hint, keys string
	icon                     string // a key of iconPaths; "" is the action glyph
	state                    string // "live" | "connecting" | "dead" | "idle": a state dot (open tabs and panes)
	badge                    string // an accent label before the keys ("Approve first" on snippets)
	bare                     bool   // no group heading above it (lib/palette.js askRow)
	run                      func(split bool)
}

// searchHosts is term.js revealVault: the sidebar shows its Hosts section and its filter has the keyboard.
func (w *workspace) searchHosts() {
	w.sidebar, w.sideSeg, w.focusHosts, w.wantFocus = true, "hosts", true, ""
}

// askKeys is the Ask the agent row's key cap: ⌘↵, Ctrl+↵ off macOS (lib/shortcutmap.js ask-agent).
func askKeys() string {
	if runtime.GOOS == "darwin" {
		return "⌘↵"
	}
	return "Ctrl+↵"
}

// askRow is lib/palette.js askRow: the palette's last row, there whatever the query matches; ⌘↵ runs it from any row.
func (w *workspace) askRow(query string) palItem {
	q := strings.TrimSpace(query)
	title := "Ask the agent"
	if q != "" {
		title = "Ask the agent about “" + q + "”"
	}
	return palItem{group: "Agent", title: title, icon: "ai-magic", keys: askKeys(), bare: true, run: func(bool) { w.askFromPalette(q) }}
}

// askFromPalette is term.js askAgentFromPalette: the panel opens with the query in the composer. Nothing is sent; the user
// presses ↵ (§5.2). A joined tab has no panel.
func (w *workspace) askFromPalette(query string) {
	if w.joinedActive() {
		w.say(msgAgentJoined)
		return
	}
	w.setAgentOpen(true)
	w.agent.input = query
	w.agent.focusInput = true
}

// palette is the open palette: the items as they were when it opened, what is typed and the selection.
type palette struct {
	items  []palItem
	query  string
	seen   string // the query the cursor was last reset for
	cursor int
	reveal bool // scroll the selected row into view in the next frame
	focus  bool // the input has had the keyboard
	ask    func(query string) palItem
}

func palWords(q string) []string { return strings.Fields(strings.ToLower(q)) }

// paletteMatches reports whether every word of q appears in the item's title, hint, group or keys.
func paletteMatches(it palItem, q string) bool {
	hay := strings.ToLower(it.title + " " + it.hint + " " + it.group + " " + it.keys)
	for _, w := range palWords(q) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// orderGroups sorts items by groupOrder, keeping the given order inside a group. Groups not in groupOrder
// follow, in order of first appearance.
func orderGroups(items []palItem) []palItem {
	rank := map[string]int{}
	for i, g := range groupOrder {
		rank[g] = i
	}
	next := len(groupOrder)
	for _, it := range items {
		if _, ok := rank[it.group]; !ok {
			rank[it.group] = next
			next++
		}
	}
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(a, b palItem) int { return rank[a.group] - rank[b.group] })
	return out
}

func filterPalette(items []palItem, q string) []palItem {
	var keep []palItem
	for _, it := range items {
		if paletteMatches(it, q) {
			keep = append(keep, it)
		}
	}
	return orderGroups(keep)
}

// moveCursor steps the selection by delta, wrapping at both ends.
func moveCursor(cursor, delta, count int) int {
	if count <= 0 {
		return 0
	}
	return ((cursor+delta)%count + count) % count
}

// clampCursor keeps a cursor inside a list that may have shrunk.
func clampCursor(cursor, count int) int { return max(0, min(cursor, count-1)) }

// span is a run of a title, marked when it matches a query word.
type span struct {
	text string
	mark bool
}

// highlight splits text into runs, marking every part that matches a word of q (overlaps merge).
func highlight(text, q string) []span {
	rs := []rune(text)
	low := make([]rune, len(rs))
	for i, r := range rs {
		low[i] = unicode.ToLower(r) // one rune for one, so the indexes agree
	}
	var ranges [][2]int
	for _, w := range palWords(q) {
		wr := []rune(w)
		for from := 0; from+len(wr) <= len(low); {
			at := indexRunes(low[from:], wr)
			if at < 0 {
				break
			}
			ranges = append(ranges, [2]int{from + at, from + at + len(wr)})
			from += at + len(wr)
		}
	}
	if len(ranges) == 0 {
		return []span{{text: text}}
	}
	slices.SortFunc(ranges, func(a, b [2]int) int { return a[0] - b[0] })
	merged := [][2]int{ranges[0]}
	for _, r := range ranges[1:] {
		if last := &merged[len(merged)-1]; r[0] <= last[1] {
			last[1] = max(last[1], r[1])
		} else {
			merged = append(merged, r)
		}
	}
	var out []span
	at := 0
	for _, m := range merged {
		if m[0] > at {
			out = append(out, span{text: string(rs[at:m[0]])})
		}
		out = append(out, span{text: string(rs[m[0]:m[1]]), mark: true})
		at = m[1]
	}
	if at < len(rs) {
		out = append(out, span{text: string(rs[at:])})
	}
	return out
}

func indexRunes(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

// ---- the items ----

// tabDot is term.js paletteDot(tabStatus(tab)): live when every pane is ready, connecting while one is,
// dead when one failed, else idle.
func paneDot(status string) string {
	switch status {
	case "ready":
		return "live"
	case "connecting":
		return "connecting"
	case "error", "exited":
		return "dead"
	}
	return "idle"
}

func (t *tab) dot() string {
	n, ready, connecting := 0, 0, 0
	for _, p := range t.panes {
		n++
		switch p.status() {
		case "error", "exited":
			return "dead"
		case "connecting":
			connecting++
		case "ready":
			ready++
		}
	}
	switch {
	case connecting > 0:
		return "connecting"
	case n > 0 && ready == n:
		return "live"
	}
	return "idle"
}

// hostSubtitle is lib/groups.js hostSubtitle: a host that signs in with a saved identity says so.
func hostSubtitle(h keelclient.Host) string {
	if h.AuthType == "local" {
		return "this machine"
	}
	port := ""
	if h.Port != 0 && h.Port != 22 {
		port = fmt.Sprintf(":%d", h.Port)
	}
	ident := ""
	if h.IdentityID != "" {
		ident = " · identity"
	}
	return h.Username + "@" + h.Address + port + ident
}

// paletteItems is the palette's list as the workspace is now: Open now, Hosts and Actions.
func (w *workspace) paletteItems() []palItem {
	var items []palItem
	add := func(section, title, keys, icon string, run func()) {
		items = append(items, palItem{group: "Actions", title: title, hint: section, keys: keys, icon: icon, run: func(bool) { run() }})
	}
	add("Tabs", "New tab", "⌘T", "add-01", w.newTab)
	add("Tabs", "New Local Shell", "", "", w.newLocalTab)
	add("Tabs", "Close pane or tab", "⌘W", "", w.closeFocused)
	add("Tabs", "Reopen last tab", "⌘⇧T", "reopen", w.reopenClosed)
	add("Tabs", "Previous tab", "⌘<", "", func() { w.cycleTab(-1) })
	add("Tabs", "Next tab", "⌘>", "", func() { w.cycleTab(1) })
	add("Tabs", "First tab", "⌘1", "", func() { w.selectTab(0) })
	add("Tabs", "Last tab", "⌘9", "", func() { w.selectTab(len(w.tabs) - 1) })
	add("Vault", "Toggle vault sidebar", "⌘B", "panel-left", func() { w.sidebar = !w.sidebar })
	add("Agent", "Toggle agent panel", "⌘I", "ai-magic", w.toggleAgent)
	add("Agent", "Agent conversations", "", "ai-magic", w.openAgentPage)
	add("Agent", "Runbooks", "", "ai-magic", w.openRunbooksPage)
	// A folder's roles, notes and runs (§17).
	for _, g := range w.folders {
		id := g.ID
		add("Vault", "Group "+g.Name+" · roles and runbooks", "", "", func() { w.openGroupPage(id, false) })
	}
	add("Vault", "Search hosts", "", "", w.searchHosts)
	add("Vault", "New host", "⌘N", "add-01", w.newHost)
	add("Vault", "Quick connect", "⌘⇧O", "", func() { w.openQuick("", nil) })
	add("Vault", "Join session", "⌘J", "link", w.openJoin)
	add("Vault", "Import hosts from ~/.ssh/config", "", "", w.openImport)
	for _, name := range pageNames { // Keychain, Port forwarding, Snippets, Logs, Settings (Notebooks sit before Logs in the webview)
		keys := ""
		if name == "settings" {
			keys = "⌘,"
		}
		add("Vault", pageDefs[name].title, keys, "", func() { w.openPage(name) })
	}
	add("Panes", "Split right", "⌘⇧D", "split-h", func() { w.splitFocused(layout.Row) })
	add("Panes", "Split down", "⌘⇧E", "split-v", func() { w.splitFocused(layout.Col) })
	add("Panes", "Split notebook", "", "", w.splitNotebook)
	add("Panes", "Focus view (zoom pane)", "⌘⇧↵", "", w.zoomFocused)
	add("Panes", "Find in terminal", "⌘F", "", func() { w.openFind(w.focusedPane()) })
	if w.tab().canSync() { // not in a joined tab, nor in a tab with no terminal
		add("Panes", "Toggle sync input", "", "", func() { w.toggleSync(w.tab()) })
	}
	add("Terminal", "Larger text", "⌘+", "", func() { w.setTermSize(w.termSize + 1) })
	add("Terminal", "Smaller text", "⌘-", "", func() { w.setTermSize(w.termSize - 1) })
	add("Terminal", "Reset text size", "⌘0", "", func() { w.setTermSize(termSizeDefault) })
	add("Terminal", "Share session (join link)", "", "", w.openShare)
	add("Terminal", "Stop sharing this session", "", "", w.stopShare)
	// The webview stops the runtime from here at once; this client asks first, as Settings · Runtime does.
	add("Terminal", "Stop Keel runtime", "", "", w.askStopRuntime)

	// Open now: every tab, and the panes of tabs with more than one. Picking one focuses it; it never starts
	// a second session.
	for i, t := range w.tabs {
		n := i + 1
		keys := ""
		switch {
		case i < 8:
			keys = fmt.Sprintf("⌘%d", n)
		case i == len(w.tabs)-1:
			keys = "⌘9"
		}
		focusTab := func() { w.selectTab(i) }
		if t.isNew() {
			items = append(items, palItem{group: "Open now", title: "New tab", hint: fmt.Sprintf("tab %d", n), keys: keys, icon: "add-01", run: func(bool) { focusTab() }})
			continue
		}
		if t.isPage() {
			items = append(items, palItem{group: "Open now", title: t.name(), hint: fmt.Sprintf("tab %d", n), keys: keys, icon: pageDefs[t.pg.name].paletteTag, run: func(bool) { focusTab() }})
			continue
		}
		leaves := t.root.Leaves()
		hint := fmt.Sprintf("tab %d", n)
		if len(leaves) > 1 {
			hint += fmt.Sprintf(" · %d panes", len(leaves))
		}
		if p := t.panes[leaves[0]]; p != nil {
			if p.conn.Snapshot().Role == "driver" {
				hint += " · driving"
			} else {
				hint += " · following"
			}
		}
		items = append(items, palItem{group: "Open now", title: t.name(), hint: hint, keys: keys, icon: "computer-terminal-01", state: t.dot(), run: func(bool) { focusTab() }})
		if len(leaves) > 1 {
			for _, id := range leaves {
				p := t.panes[id]
				if p == nil {
					continue
				}
				sub := fmt.Sprintf("pane in tab %d", n)
				if s := p.conn.Snapshot().Subtitle; s != "" {
					sub += " · " + s
				}
				items = append(items, palItem{group: "Open now", title: p.host.Name, hint: sub, icon: "computer-terminal-01", state: paneDot(p.status()), run: func(bool) {
					focusTab()
					w.pickPane(t, id)
					w.revealPanes(t)
				}})
			}
		}
	}

	// Hosts open a new session; ⌥↵ opens it in a split of the focused pane.
	for _, h := range w.hosts {
		ic := "server"
		if h.AuthType == "local" {
			ic = "computer-terminal-01"
		}
		items = append(items, palItem{group: "Hosts", title: "New session on " + h.Name, hint: hostSubtitle(h), icon: ic, run: func(split bool) {
			if split {
				w.openHostInSplit(h)
			} else {
				w.openHost(h)
			}
		}})
	}
	// Snippets only ever go through typeSnippet. "Approve first" only on a snippet that will certainly ask (rule 13: more
	// than one line, or outside the direct grammar); the others may type with no dialog (term.js snippetBadge).
	for _, sn := range w.snippets {
		items = append(items, palItem{group: "Snippets", title: "Type snippet: " + sn.Name, hint: paletteHint(sn), icon: "snippet", badge: snippetBadge(sn.Command),
			run: func(bool) { w.typeSnippet(sn.ID) }})
	}
	// Notebooks open without a session; nothing here types (term.js paletteItems: notebook-new, notebook:<id>).
	items = append(items, palItem{group: "Notebooks", title: "New notebook", run: func(bool) { w.newNotebook() }})
	for _, nb := range w.notebooks {
		id, hint := nb.ID, "Open without a session"
		if nb.Pinned {
			hint = "Pinned"
		}
		items = append(items, palItem{group: "Notebooks", title: "Open notebook " + notebookName(nb), hint: hint, run: func(bool) { w.openNotebook(id) }})
	}
	return items
}

// openHostInSplit is ⌥↵ on a host: a session on h in a split right of the focused pane, or in a new tab when
// no session tab is showing (term.js openHostInSplit).
func (w *workspace) openHostInSplit(h keelclient.Host) {
	t := w.tab()
	if t == nil || t.isNew() || t.panes[t.focused] == nil {
		w.openHost(h)
		return
	}
	w.rememberRecent(h.ID)
	w.splitOn(t, t.focused, h, layout.Row)
}

// ---- opening and closing ----

func (w *workspace) openPalette() {
	if w.pal != nil {
		return
	}
	w.pal = &palette{items: w.paletteItems(), ask: w.askRow}
}

// closePalette closes it and gives the keyboard back to the focused pane.
func (w *workspace) closePalette() {
	if w.pal == nil {
		return
	}
	w.pal = nil
	if t := w.tab(); t != nil && t.panes[t.focused] != nil {
		w.wantFocus = t.focused
	}
}

func (w *workspace) togglePalette() {
	if w.pal != nil {
		w.closePalette()
		return
	}
	w.openPalette()
}

// visible is the rows for the typed query; the cursor is kept inside them.
func (p *palette) visible() []palItem {
	rows := filterPalette(p.items, p.query)
	if p.ask != nil { // the last row, whatever the query matches
		rows = append(rows, p.ask(p.query))
	}
	if p.query != p.seen {
		p.seen, p.cursor = p.query, 0
	}
	p.cursor = clampCursor(p.cursor, len(rows))
	return rows
}

// choose closes the palette and runs the selected row (nothing when no row matches).
func (w *workspace) choose(split bool) {
	p := w.pal
	if p == nil {
		return
	}
	rows := p.visible()
	i := p.cursor
	w.closePalette()
	if i >= 0 && i < len(rows) {
		rows[i].run(split)
	}
}
