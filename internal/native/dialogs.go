package native

import "github.com/egoist/mygo/ui"

// The dialog stack, in one table. A dialog's state is a pointer on the workspace (hostDlg, approve, confirm, …); its row
// here says when it is up, how it is drawn and which dialog stands in for it. The view draws the rows in order, back to
// front, and modalOpen is "some row is up", so adding a dialog is one row and nothing else keeps a list.

type dialogSlot struct {
	open func() bool // nil: drawn every frame but not a modal (the pane's host key prompt decides for itself)
	view func(*ui.Context)
	// covered reports a dialog that asks on this one's behalf (Delete? Trust this key?): this one stays up but is not
	// drawn meanwhile, so its fields come back as they were. nil: never.
	covered func() bool
}

// dialogSlots are the dialogs, back to front.
func (w *workspace) dialogSlots() []dialogSlot {
	confirm := func() bool { return w.confirm != nil }
	return []dialogSlot{
		{open: nil, view: w.hostKeyDialog},
		{open: func() bool { return w.quick != nil }, view: w.quickDialogView},
		{open: func() bool { return w.joinDlg != nil }, view: w.joinDialogView},
		{open: func() bool { return w.hostDlg != nil }, view: w.hostDialogView, covered: confirm},
		{open: func() bool { return w.identDlg != nil }, view: w.identityDialogView, covered: confirm},
		{open: func() bool { return w.snipDlg != nil }, view: w.snippetDialogView, covered: confirm},
		{open: func() bool { return w.nbDlg != nil }, view: w.notebookDialogView, covered: confirm},
		{open: func() bool { return w.folderDlg != nil }, view: w.folderDialogView, covered: confirm},
		{open: func() bool { return w.fwdDlg != nil }, view: w.forwardDialogView, covered: func() bool { return w.confirm != nil || w.fwdKey != nil }},
		{open: func() bool { return w.fwdKey != nil }, view: w.forwardKeyView},
		{open: func() bool { return w.approve != nil }, view: w.approveView},
		{open: func() bool { return w.confirm != nil }, view: w.confirmView},
		{open: func() bool { return w.pal != nil }, view: w.paletteView},
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
		if d.covered == nil || !d.covered() {
			d.view(c)
		}
	}
}

// confirmDialog asks before a destructive action. It stands over the dialog that asked (the covered rows above), which
// comes back with its values after.
type confirmDialog struct {
	title, message, action string
	run                    func()
	cancelH, actionH       ui.Handle
	focused                bool // the keyboard has been put on Cancel
}

func (w *workspace) askConfirm(c *confirmDialog) { w.confirm = c }

// confirmView is the confirm dialog (alertdialog): a title, the message, Cancel and the action.
func (w *workspace) confirmView(c *ui.Context) {
	cf := w.confirm
	if cf == nil {
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Label("alertdialog " + cf.title)
		bodyStrong(c, cf.title, ink)
		ui.Text(c, cf.message).TextColor(inkMuted).FontSize(uiFontSize)
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if dialogBtn(c, "Cancel", btnGhost, &cf.cancelH, false) {
				open = false
			}
			if dialogBtn(c, cf.action, btnDanger, &cf.actionH, false) {
				w.act(func() {
					w.endConfirm()
					cf.run()
				})
			}
		})
	})
	if !cf.focused { // the keyboard starts on Cancel, so a stray ↵ never destroys: the action is a click, or Tab and ↵
		cf.cancelH.Focus(c)
		cf.focused = true
	}
	if !open {
		w.act(w.endConfirm)
	}
}

// endConfirm closes the confirm dialog; a host dialog under it takes the keyboard back.
func (w *workspace) endConfirm() {
	w.confirm = nil
	if d := w.hostDlg; d != nil {
		d.focus = true
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
