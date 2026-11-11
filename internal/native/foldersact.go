package native

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// What the folder menus, the folder dialog and a drop do (term.js createGroup, editGroup, renameGroup, deleteGroup,
// assignHostsGroup, moveHostToFolder, newHostIn and the dragstart / drop handlers; the words are dialogs.templ groupDialog
// and rowmenus.templ groupRowMenu). keel serve keeps the folders; this client reads them again after every write and keeps
// none on disk. Deleting a folder moves its hosts and subfolders up one level (store.DeleteGroup) and asks first.

// The webview's words.
const (
	folderNewTitle   = "New group"
	folderEditTitle  = "Edit group"
	folderDlgHint    = "Label plus an optional parent. Hosts in this group and its subgroups show on the Hosts screen."
	folderNeedsLabel = "name is required" // httpapi saveGroup's own word
	folderSavedToast = "Group saved"
	folderGoneToast  = "Group removed"
)

// folderDialog is New group / Edit group (groupDialog in dialogs.templ): a label and an optional parent.
type folderDialog struct {
	id           string // "" for New group
	name, parent string
	err          string
	saving       bool
	focus        bool

	nameH ui.Handle
}

// folderRename is the inline field a folder's Rename turns its name into (term.js renameGroup): ↵ keeps the name, Esc and
// an empty or unchanged name leave it.
type folderRename struct {
	id   string
	text string
	seen bool // the field has had the keyboard; losing it keeps the name
	h    ui.Handle
}

// reloadFolders reads the folders again.
func (w *workspace) reloadFolders() {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		groups, err := w.be.Groups(c)
		w.post(func() {
			if w.closed || err != nil { // a failed list leaves the tree as it was
				return
			}
			w.folders = groups
		})
	})
}

// afterFolderWrite reads the folders and the hosts again: a host's Group (its folder's name) is keel serve's.
func (w *workspace) afterFolderWrite() {
	w.reloadFolders()
	w.reloadHosts()
}

// ---- New, Edit, Move into ----

// newFolder is "New group" (the sidebar's menu) and "New subgroup": parent may be "".
func (w *workspace) newFolder(parent string) {
	if why := folderCreateRefusal(w.folders, parent); why != "" {
		w.warn(why)
		return
	}
	w.folderDlg = &folderDialog{parent: parent, focus: true}
}

// editFolder is Edit and Move into…: the same dialog, on the saved folder.
func (w *workspace) editFolder(id string) {
	if g, ok := groupByID(w.folders, id); ok {
		w.folderDlg = &folderDialog{id: g.ID, name: g.Name, parent: g.ParentID, focus: true}
	}
}

func (w *workspace) closeFolderDialog() { w.folderDlg = nil }

// saveFolderDialog is Save group.
func (w *workspace) saveFolderDialog() {
	d := w.folderDlg
	if d == nil || d.saving {
		return
	}
	g := keelclient.Group{ID: d.id, Name: strings.TrimSpace(d.name), ParentID: d.parent}
	if g.Name == "" {
		d.err = folderNeedsLabel
		return
	}
	d.saving, d.err = true, ""
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		_, err := w.be.SaveGroup(c, g)
		w.post(func() {
			d.saving = false
			if err != nil {
				d.err = hostError(err)
				return
			}
			if w.folderDlg == d {
				w.closeFolderDialog()
			}
			w.say(folderSavedToast)
			w.afterFolderWrite()
		})
	})
}

// ---- Rename ----

// renameFolder turns the folder's name in the tree into a field.
func (w *workspace) renameFolder(id string) {
	g, ok := groupByID(w.folders, id)
	if !ok || w.folderRen != nil {
		return
	}
	w.folderRen = &folderRename{id: id, text: g.Name}
}

// endFolderRename keeps the name typed (ok) or leaves the old one; an empty or unchanged name is left, as the app does.
func (w *workspace) endFolderRename(ok bool) {
	r := w.folderRen
	if r == nil {
		return
	}
	w.folderRen = nil
	g, found := groupByID(w.folders, r.id)
	next := strings.TrimSpace(r.text)
	if !ok || !found || next == "" || next == g.Name {
		return
	}
	g.Name = next
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		_, err := w.be.SaveGroup(c, g)
		w.post(func() {
			if err != nil {
				w.warn(hostError(err))
				return
			}
			w.afterFolderWrite()
		})
	})
}

// ---- Delete ----

// askDeleteFolder is the folder menu's Delete: it asks first, with the webview's words (term.js deleteGroup).
func (w *workspace) askDeleteFolder(id string) {
	g, ok := groupByID(w.folders, id)
	if !ok {
		return
	}
	w.askConfirm(&hostConfirm{
		title:   "Confirm",
		message: "Remove group " + g.Name + "? Hosts and subgroups move up one level.",
		action:  "Remove",
		run:     func() { w.deleteFolder(id) },
	})
}

func (w *workspace) deleteFolder(id string) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.DeleteGroup(c, id)
		w.post(func() {
			if err != nil {
				w.warn(hostError(err))
				return
			}
			w.say(folderGoneToast)
			delete(w.collapsed, id)
			w.afterFolderWrite()
		})
	})
}

// ---- host and folder drops ----

// moveHostsTo is assignHostsGroup: the hosts go into the folder dest ("" takes them out of any folder).
func (w *workspace) moveHostsTo(ids []string, dest string) {
	if len(ids) == 0 {
		return
	}
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.MoveHosts(c, ids, dest)
		w.post(func() {
			if err != nil {
				w.warn(hostError(err))
				return
			}
			w.afterFolderWrite()
		})
	})
}

// moveFolderTo is a folder dropped on another folder or on the root: the app's two refusals first, then a save with the
// new parent (the folder's roles and variables stay: only the name and the parent are sent).
func (w *workspace) moveFolderTo(id, dest string) {
	if why := folderMoveRefusal(w.folders, id, dest); why != "" {
		w.warn(why)
		return
	}
	g, ok := groupByID(w.folders, id)
	if !ok {
		return
	}
	g.ParentID = dest
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		_, err := w.be.SaveGroup(c, g)
		w.post(func() {
			if err != nil {
				w.warn(hostError(err))
				return
			}
			w.afterFolderWrite()
		})
	})
}

// dropOnFolder handles whatever was dropped on the folder dest ("" is the root of the tree).
func (w *workspace) dropOnFolder(dest string, d *treeDrag) {
	switch {
	case d == nil:
	case len(d.hosts) > 0:
		w.moveHostsTo(d.hosts, dest)
	case d.folder != "":
		w.moveFolderTo(d.folder, dest)
	}
}

// moveHostToFolder is the host menu's Move to folder…: Edit host, whose Folder field is the same field and save
// (term.js moveHostToFolder).
func (w *workspace) moveHostToFolder(h keelclient.Host) { w.editHost(h) }

// newHostIn is the folder menu's New host: New host with that folder picked (term.js newHostIn).
func (w *workspace) newHostIn(groupID string) {
	h := keelclient.Host{GroupID: groupID}
	w.openHostDialog(&h, nil, "")
}
