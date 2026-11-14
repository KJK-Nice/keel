package native

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
	"keel/internal/wire"
)

// The command palette (docs/ui-redesign.md §5.2): groups, matching, ordering, cursor, highlight, and the item list.
// Choosing an item never types into a session: a snippet opens Approve before type
// (approve.go). The last row, "Ask the agent" (⌘↵), opens the agent panel with the query typed and sends nothing.

var groupOrder = []string{"Open now", "Hosts", "Actions", "Snippets"}

// palItem is one row. run gets split = true for ⌥↵ (open in a split); rows with no split form ignore it.
type palItem struct {
	group, title, hint, keys string
	icon                     string // a key of iconPaths; "" is the action glyph
	state                    string // "live" | "connecting" | "dead" | "idle": a state dot (open tabs and panes)
	badge                    string // an accent label before the keys ("Approve first" on snippets)
	bare                     bool   // no group heading above it
	run                      func(split bool)
}

// searchHosts: the sidebar shows its Hosts section and its filter has the keyboard.
func (w *workspace) searchHosts() {
	w.sidebar, w.sideSeg, w.focusHosts, w.wantFocus = true, "hosts", true, ""
}

// askKeys is the Ask the agent row's key cap: ⌘↵, Ctrl+↵ off macOS.
func askKeys() string { return commandKeyLabel("agent.ask") }

// askRow is the palette's last row, there whatever the query matches; ⌘↵ runs it from any row.
func (w *workspace) askRow(query string) palItem {
	q := strings.TrimSpace(query)
	title := "Ask the agent"
	if q != "" {
		title = "Ask the agent about “" + q + "”"
	}
	return palItem{group: "Agent", title: title, icon: "ai-magic", keys: askKeys(), bare: true, run: func(bool) { w.askFromPalette(q) }}
}

// askFromPalette: the panel opens with the query in the composer. Nothing is sent; the user
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

// paneDot is live when every pane is ready, connecting while one is,
// dead when one failed, else idle.
func paneDot(status wire.Status) string {
	switch status {
	case wire.StatusReady:
		return "live"
	case wire.StatusConnecting:
		return "connecting"
	case wire.StatusError, wire.StatusExited:
		return "dead"
	}
	return "idle"
}

func (t *tab) dot() string {
	n, ready, connecting := 0, 0, 0
	for _, p := range t.panes {
		n++
		switch p.status() {
		case wire.StatusError, wire.StatusExited:
			return "dead"
		case wire.StatusConnecting:
			connecting++
		case wire.StatusReady:
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

// hostSubtitle: a host that signs in with a saved identity says so.
func hostSubtitle(h keelclient.Host) string {
	if h.IsLocal() {
		return "this machine"
	}
	port := ""
	if h.SSHPort() != limits.DefaultPort {
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
	for i := range commands { // the actions are the table's, in its order
		c, p := &commands[i], commands[i].pal
		switch {
		case p == nil:
		case p.many != nil:
			items = append(items, p.many(w)...)
		case p.hide && c.enabled != nil && !c.enabled(w):
		default:
			run := c.invoke
			if p.run != nil {
				run = p.run
			}
			items = append(items, palItem{group: "Actions", title: p.title, hint: p.section, keys: c.keyLabel(), icon: p.icon, run: func(bool) { run(w) }})
		}
	}

	// Open now: every tab, and the panes of tabs with more than one. Picking one focuses it; it never starts
	// a second session.
	for i, t := range w.tabs {
		n := i + 1
		keys := ""
		switch {
		case i < 8:
			keys = tabKeyLabel(n)
		case i == len(w.tabs)-1:
			keys = tabKeyLabel(9)
		}
		focusTab := func() { w.selectTab(i) }
		switch t.mode() {
		case modeNew:
			items = append(items, palItem{group: "Open now", title: "New tab", hint: fmt.Sprintf("tab %d", n), keys: keys, icon: "add-01", run: func(bool) { focusTab() }})
			continue
		case modePage:
			items = append(items, palItem{group: "Open now", title: t.name(), hint: fmt.Sprintf("tab %d", n), keys: keys, icon: t.pg.paletteTag, run: func(bool) { focusTab() }})
			continue
		}
		leaves := t.root.Leaves()
		hint := fmt.Sprintf("tab %d", n)
		if len(leaves) > 1 {
			hint += fmt.Sprintf(" · %d panes", len(leaves))
		}
		if p := t.panes[leaves[0]]; p != nil {
			if p.conn.Snapshot().Driving() {
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
		if h.IsLocal() {
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
	// than one line, or outside the direct grammar); the others may type with no dialog.
	for _, sn := range w.snippets {
		items = append(items, palItem{group: "Snippets", title: "Type snippet: " + sn.Name, hint: paletteHint(sn), icon: "snippet", badge: snippetBadge(sn.Command),
			run: func(bool) { w.typeSnippet(sn.ID) }})
	}
	// Notebooks open without a session; nothing here types.
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
// no session tab is showing.
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
	w.returnKeyboard(false)
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
