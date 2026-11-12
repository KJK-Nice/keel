package native

import (
	"fmt"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The sidebar's folder tree and the folder dialog, drawn as HostList and treeGroup (internal/ui/lists.templ) and groupDialog
// (dialogs.templ): hosts in no folder first, then each folder (collapsible, with its host count and its menu) holding its
// hosts and subfolders by name. A host row or a folder row drags onto a folder (or onto the tree's root, which takes it out
// of any folder); the rules of a drop are folders.go's. Without folders the sidebar keeps the flat list of host.Group
// (view.go group).

// hasFolders reports whether keel serve has folders: only then is the tree the nested one.
func (w *workspace) hasFolders() bool { return len(w.folders) > 0 }

// folderTreeView is the Hosts section's body when there are folders.
func (w *workspace) folderTreeView(c *ui.Context) {
	tree := nestFolderTree(w.folders, w.hosts, w.filter)
	loose := looseHosts(w.folders, w.hosts, w.filter)
	root := ui.Column(c).FillWidth().Label("host tree")
	root.Children(func() {
		if folderTreeEmpty(tree, loose) {
			w.hostsEmpty(c)
			return
		}
		for _, h := range loose {
			w.hostRow(c, h)
		}
		for _, n := range tree {
			w.folderNodeView(c, n)
		}
	})
	if d, ok := ui.Drop[*treeDrag](root); ok {
		w.act(func() { w.dropOnFolder("", d) })
	}
}

// folderMenu is groupRowMenu.
func (w *workspace) folderMenu(g keelclient.Group) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("Roles and runbooks").Chosen() {
			w.act(func() { w.openGroupPage(g.ID, false) })
		}
		if m.Item("Run a runbook…").Chosen() {
			w.act(func() { w.openGroupPage(g.ID, true) })
		}
		m.Separator()
		if m.Item("New host").Chosen() {
			w.act(func() { w.newHostIn(g.ID) })
		}
		if m.Item("New subgroup").Chosen() {
			w.act(func() { w.newFolder(g.ID) })
		}
		if m.Item("Edit").Chosen() {
			w.act(func() { w.editFolder(g.ID) })
		}
		if m.Item("Rename").Chosen() {
			w.act(func() { w.renameFolder(g.ID) })
		}
		if m.Item("Move into…").Chosen() {
			w.act(func() { w.editFolder(g.ID) })
		}
		m.Separator()
		if m.Item("Delete").Chosen() {
			w.act(func() { w.askDeleteFolder(g.ID) })
		}
	}
}

// folderNodeView is treeGroup: the heading, and while it is open the folder's hosts and then its subfolders.
func (w *workspace) folderNodeView(c *ui.Context, n folderNode) {
	open := !w.collapsed[n.g.ID]
	node := ui.Column(c).FillWidth().Key(n.g.ID).Label("folder node " + n.g.Name)
	node.Children(func() {
		indent := float32(n.depth-1) * space3
		h := ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2, 0, space2+indent).Gap(space1_5).Radius(radiusSm).
			Label("folder " + n.g.Name).Cursor(ui.CursorPointer).Drag(&treeDrag{folder: n.g.ID})
		over, isOver := ui.DragOver[*treeDrag](node)
		switch {
		case isOver && over != nil && over.folder != n.g.ID: // is-drop-target
			h.Background(hoverGround).Border(1, brass)
		case h.Hovered():
			h.Background(hoverGround)
		}
		h.PopoverMenu(w.folderMenu(n.g))
		renaming := w.folderRen != nil && w.folderRen.id == n.g.ID
		h.Children(func() {
			chev := "chevron-down"
			if !open {
				chev = "chevron-right"
			}
			icon(c, chev, inkMuted)
			if renaming {
				w.folderRenameField(c)
			} else {
				label(c, n.g.Name, inkMuted).Grow(1).Shrink(1).MinWidth(0)
			}
			if !renaming {
				if h.Hovered() || h.FocusWithin() {
					iconMenuBtn(c, "more-horizontal", "Actions for "+n.g.Name, sizeControlSm, w.folderMenu(n.g))
				} else {
					caption(c, fmt.Sprint(n.count), inkMuted)
				}
			}
		})
		if h.Clicked() && !renaming {
			id := n.g.ID
			w.act(func() { w.setCollapsed(id, open) })
		}
		if !open {
			return
		}
		for _, host := range n.hosts {
			w.hostRow(c, host)
		}
		for _, k := range n.kids {
			w.folderNodeView(c, k)
		}
	})
	if d, ok := ui.Drop[*treeDrag](node); ok {
		id := n.g.ID
		w.act(func() { w.dropOnFolder(id, d) })
	}
}

// folderRenameField is the field a folder's name turns into: ↵ keeps the name, Esc leaves the old one.
func (w *workspace) folderRenameField(c *ui.Context) {
	r := w.folderRen
	in := ui.TextInputBase(c, &r.text).Label("Rename folder").TextColor(ink).FontSize(uiFontSize).FontWeight(weightMedium).
		Grow(1).Shrink(1).MinWidth(0).Bind(&r.h)
	if !r.seen { // the old name is selected, so typing replaces it
		in.SetTextSelection(0, utf8.RuneCountInString(r.text))
	}
	in.OnTextKey(func(k ui.TextKey) bool {
		switch {
		case k.Key == ui.KeyEnter && k.Mods == 0:
			r.text = k.Text()
			w.act(func() { w.endFolderRename(true) })
		case k.Key == ui.KeyEscape && k.Mods == 0:
			w.act(func() { w.endFolderRename(false) })
		default:
			return false
		}
		return true
	})
}

// folderRenameFocus gives the field the keyboard, and keeps the name when it loses it (the app's blur), once per frame.
func (w *workspace) folderRenameFocus(c *ui.Context) {
	r := w.folderRen
	if r == nil {
		return
	}
	switch {
	case r.h.FocusWithin(c):
		r.seen = true
	case r.seen:
		w.act(func() { w.endFolderRename(true) })
	default:
		r.h.Focus(c)
	}
}

// ---- the folder dialog ----

func (w *workspace) folderDialogView(c *ui.Context) {
	d := w.folderDlg
	if d == nil || w.confirm != nil {
		return
	}
	title := folderNewTitle
	if d.id != "" {
		title = folderEditTitle
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Label("dialog " + title)
		panel.ObserveInput(func(ev ui.InputEvent) { // ⌘↵ saves from any field
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == ui.Cmd {
				w.act(w.saveFolderDialog)
			}
		})
		text(c, title, ink, titleSize, weightSemibold)
		help(c, folderDlgHint)
		labeled(c, "Label", &d.nameH, func() {
			inputBox(c, "Group label", &d.name, "Production", &d.nameH, false, false, 0, func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 { // ↵ in the field submits the form
					w.act(w.saveFolderDialog)
					return true
				}
				return false
			})
		})
		labeled(c, "Parent Group", nil, func() {
			entries := append([]pickEntry{{"", "No parent", ""}}, groupOptionEntries(w.folders, d.id)...)
			picker(c, "Parent Group", d.parent, entries, func(v string) { d.parent = v })
		})
		if d.err != "" {
			ui.Row(c).FillWidth().Label("group dialog error").Children(func() { caption(c, d.err, flare) })
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			if textBtnOff(c, "Save group", btnPrimary, d.saving) {
				w.act(w.saveFolderDialog)
			}
		})
	})
	if d.focus && w.folderDlg == d {
		d.nameH.Focus(c)
		d.focus = false
	}
	if !open {
		w.act(w.closeFolderDialog)
	}
}
