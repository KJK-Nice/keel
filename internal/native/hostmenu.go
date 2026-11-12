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
		if menuItem(m, "Connect").Chosen() {
			w.act(func() { w.openHost(h) })
		}
		if menuItem(m, "Open in split").Shortcut(ui.Alt, ui.KeyEnter).Chosen() {
			w.act(func() { w.openHostInSplit(h) })
		}
		m.Separator()
		if menuItem(m, "Edit").Chosen() {
			w.act(func() { w.editHost(h) })
		}
		if !local && menuItem(m, "Duplicate").Chosen() {
			w.act(func() { w.duplicateHost(h) })
		}
		if cmd := sshCommand(h, w.hosts); cmd != "" && menuItem(m, "Copy SSH command").Chosen() {
			w.act(func() { w.copyToClipboard(cmd, "SSH command") })
		}
		if menuItem(m, "Move to folder…").Chosen() {
			w.act(func() { w.moveHostToFolder(h) })
		}
		on := h.AgentReadable == nil || *h.AgentReadable
		if menuItem(m, "Agent can read").Checked(on).Chosen() {
			w.act(func() { w.setAgentReadable(h, !on) })
		}
		if !local {
			m.Separator()
			if menuItem(m, "Delete").Chosen() {
				w.act(func() { w.askDeleteHost(h) })
			}
		}
	}
}
