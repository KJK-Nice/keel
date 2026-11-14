package native

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
	"keel/internal/wire"
)

// The pane menu and the tab menu. A menu never types into a session.

// paneMenu fills the menu of pane p of tab t, opened from its bar.
func (w *workspace) paneMenu(t *tab, p *pane) func(m *ui.Menu) { return w.paneMenuFor(t, p, false) }

// paneMenuFor is paneMenu for a bar (row false) or for a row of the sidebar's Panes section (row true): a row has
// Focus and no Focus view.
func (w *workspace) paneMenuFor(t *tab, p *pane, row bool) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		st := p.conn.Snapshot()
		if row && menuItem(m, "Focus").Chosen() {
			w.act(func() { w.pickPane(t, p.id) })
		}
		if !p.remote { // splits need a pane of this window, not a joined one
			for _, s := range paneSplits {
				if menuItem(m, s.label).Shortcut(shortcutOf(s.id)).Chosen() {
					w.act(func() { w.split(t, p.id, s.dir) })
				}
			}
		}
		if !row && (len(t.panes) > 1 || t.zoomed != "") {
			if menuItem(m, labelFocusView).Shortcut(shortcutOf("pane.zoom")).Chosen() {
				w.act(func() { w.toggleZoom(t, p.id) })
			}
		}
		if !p.remote && p.sessionID() != "" && menuItem(m, "Share").Chosen() { // a live terminal of this window
			w.act(func() { w.pickPane(t, p.id); w.openShare() })
		}
		if (st.Status == wire.StatusError || st.Status == wire.StatusExited) && p.sessionID() != "" {
			if menuItem(m, "Reconnect").Chosen() {
				w.act(func() { w.reconnect(p) })
			}
		}
		m.Separator()
		if menuItem(m, labelClosePane).Shortcut(shortcutOf("tab.close")).Chosen() {
			w.act(func() { w.closePane(t, p.id) })
		}
	}
}

// The pane actions as a pane's bar and its menu name them.
const (
	labelFocusView = "Focus view"
	labelClosePane = "Close pane"
)

// paneSplits are a pane's two splits: the command, the label (the tool's tip and the menu item) and the tool's glyph.
var paneSplits = []struct {
	id, label, glyph string
	dir              layout.Dir
}{
	{"pane.splitr", "Split right", "split-h", layout.Row},
	{"pane.splitd", "Split down", "split-v", layout.Col},
}

// paneTools are the tools at the end of a pane's bar, a terminal's or a notebook's: the splits, focus view, the menu and
// close. Only the focused pane's tools show caps.
func (w *workspace) paneTools(c *ui.Context, t *tab, p *pane, focused bool) {
	for _, s := range paneSplits {
		if w.paneCapBtn(c, focused, capLabel(s.id), s.glyph, s.label, false, false) {
			w.act(func() { w.split(t, p.id, s.dir) })
		}
	}
	zoomed := t.zoomed == p.id
	zoomTip, zoomGlyph := labelFocusView, "zoom"
	if zoomed {
		zoomTip, zoomGlyph = "Exit focus view", "unzoom"
	}
	if w.paneCapBtn(c, focused, capLabel("pane.zoom"), zoomGlyph, zoomTip, zoomed, len(t.panes) < 2 && !zoomed) { // needs another pane to zoom out of
		w.act(func() { w.toggleZoom(t, p.id) })
	}
	iconMenuBtn(c, "more", "More pane actions", sizeControlSm, w.paneMenu(t, p))
	if iconBtn(c, "cancel-01", labelClosePane, sizeControlSm, false) {
		w.act(func() { w.closePane(t, p.id) })
	}
}

// tabMenu fills the menu of tab t.
func (w *workspace) tabMenu(t *tab) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		i := slices.Index(w.tabs, t)
		if menuItem(m, "Rename tab").Disabled(t.isPage()).Chosen() {
			w.act(func() { w.beginRename(t) })
		}
		m.Separator()
		if menuItem(m, "Close").Disabled(i < 0).Chosen() {
			w.act(func() { w.closeTab(t) })
		}
		if menuItem(m, "Close others").Disabled(len(w.tabs) < 2).Chosen() {
			w.act(func() { w.closeTabs(w.tabsOthers(t)) })
		}
		if menuItem(m, "Close to the right").Disabled(i < 0 || i == len(w.tabs)-1).Chosen() {
			w.act(func() { w.closeTabs(w.tabsRight(t)) })
		}
		if menuItem(m, "Close all").Disabled(len(w.tabs) == 0).Chosen() {
			w.act(func() { w.closeTabs(slices.Clone(w.tabs)) })
		}
	}
}

// menuIconFor names the icon of each menu entry, by its label; every entry has a leading icon.
var menuIconFor = map[string]string{
	"Roles and runbooks": "group", "Run a runbook…": "group-run", "New host": "add-01", "New subgroup": "add-01",
	"Edit": "agent-pencil", "Rename": "agent-pencil", "Rename tab": "agent-pencil", "Edit command": "agent-pencil",
	"Move into…": "group", "Move to folder…": "group", "New group": "group",
	"Delete": "agent-trash", "Delete…": "agent-trash",
	"Start": "forwards", "Stop": "cancel-01", "Cancel": "cancel-01",
	"Copy local address": "agent-copy", "Copy": "agent-copy", "Copy command": "agent-copy", "Copy SSH command": "agent-copy", "Duplicate": "agent-copy",
	"Open": "notebook", "Pin": "pin", "Unpin": "pin", "Make private": "lock", "Let the agent read it": "eye", "Agent can read": "eye",
	"History": "agent-history", "Ask the agent about this notebook": "ai-magic",
	"Type into pane…": "agent-cmd", "Save command": "tick", "Undo": "agent-redo", "Restore": "restore-bin", "Keep": "tick",
	"Connect": "computer-terminal-01", "Local terminal": "computer-terminal-01", "Open in split": "split-h",
	"Quick connect": "action", "Join session": "link",
	"Focus": "radio", "Split right": "split-h", "Split down": "split-v", "Focus view": "zoom", "Share": "share-08",
	"Reconnect": "refresh-cw", "Close pane": "cancel-01", "Close": "cancel-01", "Close others": "cancel-01",
	"Close to the right": "cancel-01", "Close all": "cancel-01",
}

var menuIcons = map[string]ui.MenuIcon{}

// menuItem is m.Item with the entry's icon (menuIconFor) and, for a Delete, the destructive colour.
func menuItem(m *ui.Menu, label string) *ui.MenuItem {
	it := m.Item(label)
	name := menuIconFor[label]
	if menuItemSeen != nil {
		menuItemSeen(label, name)
	}
	if name != "" {
		ic, ok := menuIcons[name]
		if !ok {
			if svg, err := ui.ParseSVG([]byte(svgHead + iconPaths[name] + "</svg>")); err == nil {
				if mi, err := mygo.NewMenuIconSVG(svg, true); err == nil {
					ic = mi
				}
			}
			menuIcons[name] = ic
		}
		it.Icon(ic)
	}
	if strings.HasPrefix(label, "Delete") {
		it.Danger()
	}
	return it
}

// menuItemSeen is set by tests to see each entry built and the icon name it got.
var menuItemSeen func(label, icon string)

// iconMenuBtn is iconBtn that opens a menu below it.
// A row has no such button: it was built only while the row was hovered, so moving the pointer onto the opened menu
// un-hovered the row, dropped the button and closed the menu. Rows use the right-click menu and the menu key.
func iconMenuBtn(c *ui.Context, name, tip string, size float32, build func(m *ui.Menu)) {
	b := ui.Row(c).Size(size, size).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label(tip).Tooltip(tip).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(hoverGround)
	}
	b.Children(func() { icon(c, name, inkMuted) })
	b.PopoverMenuButton(build)
}
