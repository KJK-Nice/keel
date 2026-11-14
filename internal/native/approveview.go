package native

import "github.com/egoist/mygo/ui"

// The Approve before type dialog drawn as ptyInjectDialog (internal/ui/dialogs.templ) with .modal and
// .pty-inject-preview (keel.css): the title and its sentence, the session, the exact text in the mono font, an error
// line, Cancel and Type. As in the webview the keyboard starts on Cancel, so a stray ↵ never types; Type is a click, or
// Tab and ↵. Esc, Cancel and a click outside close it and send nothing.

// dialogBtn is textBtn that takes the keyboard (↵ presses it) and can be switched off.
func dialogBtn(c *ui.Context, name string, kind btnKind, h *ui.Handle, off bool) bool {
	clicked, _ := dialogBtnMods(c, name, name, kind, h, off)
	return clicked
}

// dialogBtnMods is dialogBtn that also says which modifiers were held with the press (⇧ for a group's Type). shown is the
// text on the button; name is what a test or assistive technology finds it by.
func dialogBtnMods(c *ui.Context, name, shown string, kind btnKind, h *ui.Handle, off bool) (bool, ui.Modifiers) {
	b := ctlButton(c, name, shown, kind).Focusable().Bind(h)
	if off {
		b.Disabled(true)
	}
	if b.Clicked() {
		return true, b.ClickModifiers()
	}
	return false, 0
}

// pressType is the Type button's action in both dialogs, the one place that names approveType (guard_test.go): the one-pane
// dialog's Type calls it with false, the group dialog's with ⇧ as it was held when the button was pressed.
func (w *workspace) pressType(shift bool) { w.act(func() { w.approveType(shift) }) }

func (w *workspace) approveView(c *ui.Context) {
	d := w.approve
	if d == nil {
		return
	}
	hint, note, glyphs := approveHint, "", false
	switch s := d.src.(type) {
	case *approveGroup:
		w.groupApproveView(c, d)
		return
	case approveNotebook:
		hint, note = approveHintNotebook, s.note
	case *approveAgent: // the bytes as the proposal card draws them: ^U, the command, ↵
		glyphs = true
	}
	open := true
	why := d.blocked() // driver only, and the session ready: else Type is off and this says why
	shown := d.shown() // what is typed after Ctrl-U: the same call as Type makes
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Label("dialog " + approveTitle)
		text(c, approveTitle, ink, titleSize, weightSemibold)
		ui.Text(c, hint).TextColor(inkMuted).FontSize(uiFontSize)
		ui.Row(c).FillWidth().Gap(space1).Children(func() {
			caption(c, "Session:", inkMuted)
			caption(c, d.session(), inkMuted).MinWidth(0)
		})
		ui.Scroll(c).FillWidth().MaxHeight(approvePreviewMax).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("approve preview").Children(func() {
			if glyphs {
				bytesView(c, shown)
			} else {
				ui.Text(c, shown).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0)
			}
		})
		if note != "" { // .k-review.pty-inject-agent, after the preview as the app has it
			ui.Row(c).FillWidth().Padding(space2, space3).Radius(radiusMd).Background(hoverGround).Label("approve note").Children(func() {
				ui.Text(c, note).TextColor(inkMuted).FontSize(captionSize).MaxLines(0)
			})
		}
		switch {
		case d.err != "":
			ui.Row(c).FillWidth().Label("approve error").Children(func() { caption(c, d.err, flare) })
		case why != "":
			ui.Row(c).FillWidth().Label("approve blocked").Children(func() { caption(c, why, flare) })
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if dialogBtn(c, "Cancel", btnGhost, &d.cancelH, false) {
				open = false
			}
			if dialogBtn(c, "Type", btnPrimary, &d.typeH, why != "" || d.busy) {
				w.pressType(false)
			}
		})
	})
	if d.focus && w.approve == d {
		d.cancelH.Focus(c)
		d.focus = false
	}
	if !open {
		w.act(w.cancelApprove)
	}
}
