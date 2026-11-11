package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The host row menu: right-click on a row, or its "…" control. The items, order and words are hostRowMenu in
// internal/ui/rowmenus.templ: Connect, Open in split, Edit, Duplicate (not the local shell), Agent can read,
// Move to folder…, Delete (not the local shell). "Move to folder…" opens Edit host on its Folder field (term.js
// moveHostToFolder). "Copy SSH command" waits for a later phase (docs/native.md).
// A menu never types into a session.

// hostMenu fills the menu of a host row.
func (w *workspace) hostMenu(h keelclient.Host) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		local := h.AuthType == "local"
		if m.Item("Connect").Chosen() {
			w.act(func() { w.openHost(h) })
		}
		if m.Item("Open in split").Shortcut(ui.Alt, ui.KeyEnter).Chosen() {
			w.act(func() { w.openHostInSplit(h) })
		}
		m.Separator()
		if m.Item("Edit").Chosen() {
			w.act(func() { w.editHost(h) })
		}
		if !local && m.Item("Duplicate").Chosen() {
			w.act(func() { w.duplicateHost(h) })
		}
		if m.Item("Move to folder…").Chosen() {
			w.act(func() { w.moveHostToFolder(h) })
		}
		on := h.AgentReadable == nil || *h.AgentReadable
		if m.Item("Agent can read").Checked(on).Chosen() {
			w.act(func() { w.setAgentReadable(h, !on) })
		}
		if !local {
			m.Separator()
			if m.Item("Delete").Chosen() {
				w.act(func() { w.askDeleteHost(h) })
			}
		}
	}
}
