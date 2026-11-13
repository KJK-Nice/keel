package native

import "github.com/egoist/mygo/ui"

// The dialogs, in one table. A dialog's state is a pointer on the workspace (hostDlg, approve, confirm, …); its row here
// says when it is up and how it is drawn. The view draws the rows in order, back to front, and modalOpen is "some row
// is up", so adding a dialog is one row and nothing else keeps a list.

type dialogSlot struct {
	open func() bool // nil: drawn every frame but not a modal (the pane's host key prompt decides for itself)
	view func(*ui.Context)
}

// dialogSlots are the dialogs, back to front.
func (w *workspace) dialogSlots() []dialogSlot {
	return []dialogSlot{
		{nil, w.hostKeyDialog},
		{func() bool { return w.quick != nil }, w.quickDialogView},
		{func() bool { return w.joinDlg != nil }, w.joinDialogView},
		{func() bool { return w.hostDlg != nil }, w.hostDialogView},
		{func() bool { return w.identDlg != nil }, w.identityDialogView},
		{func() bool { return w.snipDlg != nil }, w.snippetDialogView},
		{func() bool { return w.nbDlg != nil }, w.notebookDialogView},
		{func() bool { return w.folderDlg != nil }, w.folderDialogView},
		{func() bool { return w.fwdDlg != nil }, w.forwardDialogView},
		{func() bool { return w.fwdKey != nil }, w.forwardKeyView},
		{func() bool { return w.approve != nil }, w.approveView},
		{func() bool { return w.confirm != nil }, w.confirmView},
		{func() bool { return w.pal != nil }, w.paletteView},
	}
}

// modalOpen reports a dialog that has the keyboard: no terminal or page input takes focus while one shows.
func (w *workspace) modalOpen() bool {
	for _, d := range w.dialogSlots() {
		if d.open != nil && d.open() {
			return true
		}
	}
	return false
}

// dialogsView draws the dialogs.
func (w *workspace) dialogsView(c *ui.Context) {
	for _, d := range w.dialogSlots() {
		d.view(c)
	}
}

// returnKeyboard gives the keyboard back to the focused pane after a dialog closes, or, with toPage, to the page of a tab
// that has no pane.
func (w *workspace) returnKeyboard(toPage bool) {
	t := w.tab()
	if t == nil {
		return
	}
	if t.panes[t.focused] != nil {
		w.wantFocus = t.focused
	} else if toPage && t.page != nil {
		t.page.focus = true
	}
}
