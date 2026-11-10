package native

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
)

// The pane menu and the tab menu. Their items, order and words are the webview's: PaneMenu in
// internal/ui/rowmenus.templ with which items show from lib/termmenu.js paneMenuItems (for a terminal pane
// of this window, opened from its bar), and #tab-ctx in internal/ui/parts.templ with which are disabled
// from ui/tabmenu.js. Share waits for a later phase. A menu never types into a session.

// paneMenu fills the menu of pane p of tab t.
func (w *workspace) paneMenu(t *tab, p *pane) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		st := p.conn.Snapshot()
		if m.Item("Split right").Shortcut(ui.Cmd|ui.Shift, ui.KeyD).Chosen() {
			w.act(func() { w.split(t, p.id, layout.Row) })
		}
		if m.Item("Split down").Shortcut(ui.Cmd|ui.Shift, ui.KeyE).Chosen() {
			w.act(func() { w.split(t, p.id, layout.Col) })
		}
		if len(t.panes) > 1 || t.zoomed != "" {
			if m.Item("Focus view").Shortcut(ui.Cmd|ui.Shift, ui.KeyEnter).Chosen() {
				w.act(func() { w.toggleZoom(t, p.id) })
			}
		}
		if (st.Status == "error" || st.Status == "exited") && p.sessionID() != "" {
			if m.Item("Reconnect").Chosen() {
				w.act(func() { w.reconnect(p) })
			}
		}
		m.Separator()
		if m.Item("Close pane").Shortcut(ui.Cmd, ui.KeyW).Chosen() {
			w.act(func() { w.closePane(t, p.id) })
		}
	}
}

// tabMenu fills the menu of tab t.
func (w *workspace) tabMenu(t *tab) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		i := slices.Index(w.tabs, t)
		if m.Item("Rename tab").Disabled(t.isPage()).Chosen() {
			w.act(func() { w.beginRename(t) })
		}
		m.Separator()
		if m.Item("Close").Disabled(i < 0).Chosen() {
			w.act(func() { w.closeTab(t) })
		}
		if m.Item("Close others").Disabled(len(w.tabs) < 2).Chosen() {
			w.act(func() { w.closeTabs(w.tabsOthers(t)) })
		}
		if m.Item("Close to the right").Disabled(i < 0 || i == len(w.tabs)-1).Chosen() {
			w.act(func() { w.closeTabs(w.tabsRight(t)) })
		}
		if m.Item("Close all").Disabled(len(w.tabs) == 0).Chosen() {
			w.act(func() { w.closeTabs(slices.Clone(w.tabs)) })
		}
	}
}

// iconMenuBtn is iconBtn that opens a menu below it.
func iconMenuBtn(c *ui.Context, name, tip string, size float32, build func(m *ui.Menu)) {
	b := ui.Row(c).Size(size, size).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label(tip).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(hoverGround)
	}
	b.Children(func() { icon(c, name, inkMuted) })
	b.PopoverMenuButton(build)
}
