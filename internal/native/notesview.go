package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/wire"
)

// Notes for the agent drawn: the list in Edit host and in Settings · Agent
// and the card for a note the agent proposes. Colours and sizes are tokens.go's.

// inlineText draws a line with `code` in mono and **bold**.
func inlineText(c *ui.Context, s string, col ui.Color) ui.Element {
	var spans []ui.Span
	for _, m := range mdInline(s) {
		sp := ui.Span{Text: m.text}
		switch {
		case m.code:
			sp.Font, sp.Size, sp.Background, sp.Radius, sp.PaddingX = monoStack, captionSize, hoverGround, radiusXs, space1
		case m.bold:
			sp.Weight = weightSemibold
		}
		spans = append(spans, sp)
	}
	return ui.RichText(c, spans...).TextColor(col).FontSize(uiFontSize).MaxLines(0)
}

// warnBlock is the warning on a card whose text may have come from terminal output.
func warnBlock(c *ui.Context, s string) {
	ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(signalSoft).AlignItems(ui.Start).Label("note warning").Children(func() {
		icon(c, "alert", signal)
		ui.Text(c, s).TextColor(ink).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
	})
}

// notesBlockOpts are the words and rules of one notes list.
type notesBlockOpts struct {
	title, placeholder, help string
	host                     bool // Edit host's list: it says when the host is not saved yet, and when Agent can read is off
	readable                 bool // the dialog's Agent can read switch
}

// notesBlock is the notes list: its head (title and count), the list with Edit and Delete on each note, the field that adds
// one, an error and the help.
func (w *workspace) notesBlock(c *ui.Context, e *notesEditor, o notesBlockOpts) {
	unsaved := !e.saved()
	ui.Column(c).FillWidth().Gap(space2).Label(o.title).Children(func() {
		ui.Row(c).FillWidth().Gap(space1_5).AlignItems(ui.Center).Children(func() {
			icon(c, "note", inkMuted)
			menuLabel(c, o.title)
			ui.Spacer(c)
			if !unsaved {
				caption(c, notesCountLabel(e.notes), inkFaint)
			}
		})
		if o.host && !o.readable {
			ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(hoverGround).AlignItems(ui.Start).Label("notes not sent").Children(func() {
				icon(c, "shield-01", inkMuted)
				ui.Text(c, "Agent can read is off, so these notes are not sent. They stay in the vault.").TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
			})
		}
		if unsaved {
			help(c, "Save the host first, then add notes for the agent.")
			return
		}
		ui.Column(c).FillWidth().Gap(space1_5).Label("notes list").Children(func() {
			if len(e.notes) == 0 {
				if e.loaded {
					help(c, emptyNotesText(e, o.host))
				} else {
					waitHelp(c, "Loading notes…")
				}
			}
			for _, n := range e.notes {
				w.noteRow(c, e, n)
			}
		})
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				inputBox(c, "Add a note", &e.input, o.placeholder, &e.inputH, false, false, 0, func(k ui.TextKey) bool {
					if k.Key == ui.KeyEnter && k.Mods == 0 { // ↵ adds the note; it never submits the host form around it
						w.act(func() { w.addNote(e) })
						return true
					}
					return false
				})
			})
			if textBtn(c, "Add", btnDefault) {
				w.act(func() { w.addNote(e) })
			}
		})
		if e.err != "" {
			ui.Row(c).FillWidth().Label("notes error").Children(func() { ui.Text(c, e.err).TextColor(flare).FontSize(captionSize) })
		}
		help(c, o.help)
	})
	if e.focusInput && e.saved() {
		e.inputH.Focus(c)
		e.focusInput = false
	}
}

// emptyNotesText is the list's empty-state sentence.
func emptyNotesText(e *notesEditor, host bool) string {
	switch {
	case e.unreadable:
		return "Saved notes cannot be read. Delete all notes in Settings · Agent starts a fresh file."
	case e.user:
		return "No notes. Add how you like to work, e.g. Prefer journalctl over tail -f."
	case e.group:
		return "No notes. Add what is true of every host in this group."
	}
	return "No notes. Add what the agent should know about this host."
}

// noteRow is one note: its text, where it came from, and Edit and Delete; or the field it is edited in.
func (w *workspace) noteRow(c *ui.Context, e *notesEditor, n keelclient.Note) {
	ui.Column(c).FillWidth().Key(n.ID).Padding(space2, space3).Gap(space1_5).Radius(radiusSm).Background(stage).Border(1, line).Label("note").Children(func() {
		if e.editing == n.ID {
			ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
				ui.TextAreaBase(c, &e.editText).Label("Edit note").TextColor(ink).FontSize(uiFontSize).Lines(2, 6).FillWidth().Bind(&e.editH).
					OnTextKey(func(k ui.TextKey) bool {
						switch {
						case k.Key == ui.KeyEnter && k.Mods == 0: // ↵ saves, ⇧↵ is a newline
							w.act(func() { w.saveNoteEdit(e) })
							return true
						case k.Key == ui.KeyEscape && k.Mods == 0:
							w.act(func() { w.cancelEditNote(e) })
							return true
						}
						return false
					})
			})
			ui.Row(c).FillWidth().Gap(space1_5).Justify(ui.End).Children(func() {
				if textBtn(c, "Cancel", btnGhost) {
					w.act(func() { w.cancelEditNote(e) })
				}
				if textBtn(c, "Save", btnPrimary) {
					w.act(func() { w.saveNoteEdit(e) })
				}
			})
			if e.focusEdit {
				e.editH.Focus(c)
				e.focusEdit = false
			}
			return
		}
		inlineText(c, n.Text, ink)
		ui.Row(c).FillWidth().Gap(space1).AlignItems(ui.Center).Children(func() {
			text(c, noteSourceLabel(n), inkFaint, captionSize, weightRegular).Shrink(1).MinWidth(0)
			ui.Spacer(c)
			if iconBtn(c, "agent-pencil", "Edit note", sizeControlSm, false) {
				w.act(func() { w.startEditNote(e, n.ID) })
			}
			if iconBtn(c, "agent-trash", "Delete note", sizeControlSm, false) {
				w.act(func() { w.deleteNote(e, n.ID) })
			}
		})
	})
}

// ---- the card for a note the agent proposes ----

// doneRow is a closed card: a line, and the buttons after it.
func (w *workspace) doneRow(c *ui.Context, ok bool, msg string, buttons func()) {
	ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).AlignItems(ui.Center).Label("proposal done").Children(func() {
		if ok {
			icon(c, "tick", tide)
		} else {
			icon(c, "agent-info", inkMuted)
		}
		ui.Text(c, msg).TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
		buttons()
	})
}

// agentNoteCard is "Remember this?": nothing is saved until Save is pressed.
func (w *workspace) agentNoteCard(c *ui.Context, id string, onPage bool) {
	rec := w.agent.notes[id]
	if rec == nil {
		return
	}
	n := rec.n
	warn := noteProposalWarn(n)
	editH := &rec.editH
	if onPage {
		editH = &rec.editHP
	}
	border := line
	if warn && n.Status == wire.CardOpen && !n.Expired {
		border = signal
	}
	card := ui.Column(c).FillWidth().Key("note "+id).Radius(radiusMd).Background(controlFill(raised)).Border(1, border).Clip().Label("Proposed note")
	card.Children(func() {
		if n.Status != wire.CardOpen || n.Expired {
			msg := noteProposalDone(n)
			if n.Status == wire.CardOpen {
				msg = "Not saved. This proposal expired; add the note yourself in Edit host or Settings · Agent."
			}
			w.doneRow(c, n.Status == wire.CardSaved, msg, func() {
				if !n.Expired && textBtnOff(c, "Undo", btnGhost, rec.busy) {
					w.act(func() { w.undoNoteProposal(id) })
				}
			})
			return
		}
		ui.Row(c).FillWidth().Padding(space3, space3, 0).Gap(space2).Wrap().Children(func() {
			icon(c, "note", inkMuted)
			label(c, "Remember this?", ink)
			ui.Spacer(c)
			switch {
			case n.FromPane:
				pill(c, "From pane output", flareSoft, flare)
			case warn:
				pill(c, "After reading panes", flareSoft, flare)
			}
			pill(c, noteProposalTarget(n), hoverGround, inkMuted).Label("note target")
		})
		ui.Column(c).FillWidth().Padding(space2, space3, 0).Gap(space2).Children(func() {
			if rec.editing {
				ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
					ui.TextAreaBase(c, &rec.edit).Label("Edit note").TextColor(ink).FontSize(uiFontSize).Lines(2, 8).FillWidth().Bind(editH)
				})
			} else {
				inlineText(c, n.Text, ink)
			}
			text(c, noteProposalSource(n), inkFaint, captionSize, weightRegular)
			if o := noteProposalOrigin(n); o != "" {
				cardNote(c, "agent-info", signal, o)
			}
			if warn {
				warnBlock(c, noteProposalWarning(n))
			}
			if rec.err != "" {
				cardNote(c, "alert", flare, rec.err)
			}
			cardNote(c, "shield-01", inkMuted, "Nothing is saved until you press Save. Notes live in the vault; edit them in Edit host or Settings · Agent.")
		})
		hairline(ui.Row(c).FillWidth().Margin(space3, 0, 0).Padding(space2, space3).Gap(space1_5).Background(w.ground(surface)), 1, 0, 0, 0).Children(func() {
			save := func() {
				name, kind := "Save", btnPrimary
				if warn { // Save says "Save anyway" on a note that may come from a pane
					name, kind = "Save anyway", btnDefault
				}
				if textBtnOff(c, name, kind, rec.busy) {
					w.act(func() { w.saveNoteProposal(id) })
				}
			}
			if rec.editing {
				ui.Spacer(c)
				if textBtn(c, "Cancel", btnGhost) {
					w.act(func() { rec.editing = false })
				}
				save()
				return
			}
			text(c, noteProposalScope(n), inkFaint, captionSize, weightRegular).Shrink(1).MinWidth(0)
			ui.Spacer(c)
			if textBtnOff(c, "Dismiss", btnGhost, rec.busy) {
				w.act(func() { w.dismissNoteProposal(id) })
			}
			if iconBtn(c, "agent-pencil", "Edit note", sizeControlSm, false) {
				w.act(func() { w.startEditNoteProposal(rec) })
			}
			save()
		})
	})
	if rec.editing && rec.focusEdit && onPage == w.agentPageShown() {
		editH.Focus(c)
		rec.focusEdit = false
	}
}

// textBtnOff is textBtn that ignores a press and dims while off (a card whose request is out).
func textBtnOff(c *ui.Context, name string, kind btnKind, off bool) bool {
	return textBtnOffAs(c, name, name, kind, off)
}
