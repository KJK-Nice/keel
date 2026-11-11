package native

import "github.com/egoist/mygo/ui"

// The Approve before type dialog drawn as ptyInjectDialog (internal/ui/dialogs.templ) with .modal and
// .pty-inject-preview (keel.css): the title and its sentence, the session, the exact text in the mono font, an error
// line, Cancel and Type. As in the webview the keyboard starts on Cancel, so a stray ↵ never types; Type is a click, or
// Tab and ↵. Esc, Cancel and a click outside close it and send nothing.

// dialogBtn is textBtn that takes the keyboard (↵ presses it) and can be switched off.
func dialogBtn(c *ui.Context, name string, kind btnKind, h *ui.Handle, off bool) bool {
	b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).Shrink(0).
		Label(name).Cursor(ui.CursorPointer).Focusable().Bind(h)
	bg, fg := raised, ink
	switch kind {
	case btnGhost:
		bg, fg = ui.Transparent, inkMuted
		if b.Hovered() && !off {
			bg, fg = hoverGround, ink
		}
	case btnPrimary:
		bg, fg = brass, onBrass
		if b.Hovered() && !off {
			bg = brassStrong
		}
	}
	b.Background(bg)
	if off {
		b.Opacity(0.45).Disabled(true)
	}
	b.Children(func() { label(c, name, fg) })
	return b.Clicked()
}

func (w *workspace) approveView(c *ui.Context) {
	d := w.approve
	if d == nil {
		return
	}
	open := true
	why := d.blocked() // driver only, and the session ready: else Type is off and this says why
	shown := d.shown() // what is typed after Ctrl-U: the same call as Type makes
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Background(raised).Border(1, line).Radius(radiusMd).Label("dialog " + approveTitle)
		text(c, approveTitle, ink, titleSize, weightSemibold)
		ui.Text(c, approveHint).TextColor(inkMuted).FontSize(uiFontSize)
		ui.Row(c).FillWidth().Gap(space1).Children(func() {
			caption(c, "Session:", inkMuted)
			caption(c, d.session(), inkMuted).MinWidth(0)
		})
		ui.Scroll(c).FillWidth().MaxHeight(approvePreviewMax).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("approve preview").Children(func() {
			if d.agent != nil { // the bytes as the proposal card draws them: ^U, the command, ↵
				bytesView(c, shown)
			} else {
				ui.Text(c, shown).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0)
			}
		})
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
				w.act(w.approveType)
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
