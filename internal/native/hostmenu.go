package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The host row menu: right-click on a row, or its "…" control. The items, order and words are hostRowMenu in
// internal/ui/rowmenus.templ: Connect, Open in split, Edit, Duplicate (not the local shell), Agent can read,
// Delete (not the local shell). "Copy SSH command" and "Move to folder…" wait for later phases (docs/native.md).
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
