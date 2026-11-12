package native

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
)

// The pane menu and the tab menu. Their items, order and words are the webview's: PaneMenu in
// internal/ui/rowmenus.templ with which items show from lib/termmenu.js paneMenuItems (for a terminal pane
// of this window, opened from its bar), and #tab-ctx in internal/ui/parts.templ with which are disabled
// from ui/tabmenu.js. A menu never types into a session.

// paneMenu fills the menu of pane p of tab t, opened from its bar.
func (w *workspace) paneMenu(t *tab, p *pane) func(m *ui.Menu) { return w.paneMenuFor(t, p, false) }

// paneMenuFor is paneMenu for a bar (row false) or for a row of the sidebar's Panes section (row true): a row has
// Focus and no Focus view (lib/termmenu.js paneMenuItems).
func (w *workspace) paneMenuFor(t *tab, p *pane, row bool) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		st := p.conn.Snapshot()
		if row && menuItem(m, "Focus").Chosen() {
			w.act(func() { w.pickPane(t, p.id) })
		}
		if !p.remote { // splits need a pane of this window, not a joined one (lib/termmenu.js paneMenuItems)
			if menuItem(m, "Split right").Shortcut(ui.Cmd|ui.Shift, ui.KeyD).Chosen() {
				w.act(func() { w.split(t, p.id, layout.Row) })
			}
			if menuItem(m, "Split down").Shortcut(ui.Cmd|ui.Shift, ui.KeyE).Chosen() {
				w.act(func() { w.split(t, p.id, layout.Col) })
			}
		}
		if !row && (len(t.panes) > 1 || t.zoomed != "") {
			if menuItem(m, "Focus view").Shortcut(ui.Cmd|ui.Shift, ui.KeyEnter).Chosen() {
				w.act(func() { w.toggleZoom(t, p.id) })
			}
		}
		if !p.remote && p.sessionID() != "" && menuItem(m, "Share").Chosen() { // a live terminal of this window
			w.act(func() { w.pickPane(t, p.id); w.openShare() })
		}
		if (st.Status == "error" || st.Status == "exited") && p.sessionID() != "" {
			if menuItem(m, "Reconnect").Chosen() {
				w.act(func() { w.reconnect(p) })
			}
		}
		m.Separator()
		if menuItem(m, "Close pane").Shortcut(ui.Cmd, ui.KeyW).Chosen() {
			w.act(func() { w.closePane(t, p.id) })
		}
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

// menuIconFor names the icon of each menu entry, by its label; the webview's menus had a leading icon on every entry.
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
