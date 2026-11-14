package native

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The agent's notebooks drawn: the chips and the picker of the composer, and the card of what the agent did to a notebook. A card's buttons ask the workspace (agentnotebooks.go); none of them changes a
// notebook, types, or marks one reviewed.

// ---- the composer ----

// nbChips draws one chip per attached notebook, with "Stop sending <name>".
func (w *workspace) nbChips(c *ui.Context) {
	for _, nb := range w.attachedNotebooks() {
		id, name := nb.ID, notebookName(nb)
		ui.Row(c).Height(sizeControlSm).Padding(0, space1, 0, space2).Gap(space1).Radius(radiusFull).Background(hoverGround).Key("nb " + id).Label("attached " + name).Children(func() {
			iconSized(c, "notebook", inkMuted, iconSize)
			label(c, name, ink).Shrink(1).MinWidth(0).MaxLines(1)
			if iconBtn(c, "cancel-01", "Stop sending "+name, sizeControlSm-space1, false) {
				w.act(func() { w.detachNotebook(id) })
			}
		})
	}
}

// nbHint is the line under the chips: the attached notebooks go with every question until they are removed.
func (w *workspace) nbHint(c *ui.Context) {
	if h := attachHint(len(w.agent.attached)); h != "" {
		ui.Text(c, h).TextColor(inkFaint).FontSize(captionSize)
	}
}

// attachButton is "Attach notebook" in a composer's foot (where is "panel" or "page"), and the picker under it.
func (w *workspace) attachButton(c *ui.Context, where string) {
	a := w.agent
	on := a.nbPicker == where
	b := ui.Row(c).Size(sizeControlSm, sizeControlSm).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label("Attach notebook").Tooltip("Attach notebook").Cursor(ui.CursorPointer)
	col := inkMuted
	switch {
	case on:
		b.Background(accentSoft)
		col = accentInk
	case b.Hovered():
		b.Background(hoverGround)
	}
	b.Children(func() { icon(c, "notebook-pen", col) })
	if b.Clicked() {
		w.act(func() {
			if on {
				a.nbPicker = ""
				return
			}
			a.picker, a.sessOpen, a.nbPicker = false, false, where // one popover at a time
		})
	}
	if on {
		w.nbPickerView(c, b, where)
	}
}

// nbPickerNote is the picker's foot, with the numbers of the rule.
var nbPickerNote = fmt.Sprintf("At most %d, sent in full. The agent also sees the titles of your notebooks that are not private and may read them; it never sees a private one.", maxAttached)

// nbPickerView is "Notebooks to send with your question": every saved notebook, at most two ticked. A private notebook is
// listed and cannot be ticked. Nothing is read until the user asks.
func (w *workspace) nbPickerView(c *ui.Context, anchor ui.Element, where string) {
	a := w.agent
	open := true
	all := attachable(w.notebooks)
	full := len(a.attached) >= maxAttached
	ui.PopoverBase(c, anchor, &open, func(panel ui.Element) {
		panel.Width(sizeAgent-space6).Background(raised).Border(1, line).Radius(radiusMd).Shadow(0, 8, 24, 0, shadowPop).Padding(space1).Label("dialog Notebooks to send with your question")
		if len(all) == 0 {
			if emptySide(c, emptyNone("notebook-pen", "No notebooks yet", "Write one first; then attach it here.", "New notebook")) {
				w.act(func() { a.nbPicker = ""; w.newNotebook() })
			}
			return
		}
		ui.Row(c).Padding(space1_5, space2, space1).Children(func() { menuLabel(c, "Notebooks to send with your question") })
		if a.nbRowH == nil {
			a.nbRowH = map[string]*ui.Handle{}
		}
		// ↑ ↓ walk between the rows, wrapping at both ends: from the row that has the keyboard.
		panel.ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind != ui.InputKeyDown || (ev.Key != ui.KeyDown && ev.Key != ui.KeyUp) || ev.Mods != 0 {
				return
			}
			d := 1
			if ev.Key == ui.KeyUp {
				d = -1
			}
			for i, nb := range all {
				if h := a.nbRowH[nb.ID]; h != nil && h.FocusWithin(c) {
					next := all[(i+d+len(all))%len(all)].ID
					w.act(func() { a.nbWalk = next })
					return
				}
			}
		})
		for _, nb := range all {
			id, name := nb.ID, notebookName(nb)
			ticked := slices.Contains(a.attached, id)
			off := (!ticked && full) || nb.Private
			if a.nbRowH[id] == nil {
				a.nbRowH[id] = new(ui.Handle)
			}
			r := ui.Row(c).FillWidth().Padding(space1_5, space2).Gap(space2).Radius(radiusSm).Key(id).Label("pick notebook " + name).Focusable().Bind(a.nbRowH[id])
			if off {
				r.Opacity(0.6).Cursor(ui.CursorNotAllowed)
				if nb.Private {
					r.Tooltip("Private: the agent cannot read it")
				}
			} else {
				r.Cursor(ui.CursorPointer)
				if r.Hovered() {
					r.Background(hoverGround)
				}
			}
			r.Children(func() {
				box := ui.Row(c).Size(space4, space4).Radius(radiusXs).Justify(ui.Center).Shrink(0).Border(1, lineStrong).Background(stage)
				if ticked {
					box.Background(accentFill)
				}
				box.Children(func() {
					if ticked {
						iconSized(c, "tick", onAccent, iconSizeSm)
					}
				})
				body(c, name, ink).Grow(1).Shrink(1).MinWidth(0).MaxLines(1)
				switch {
				case nb.Private:
					outlineBadge(c, "Private")
				case nb.Source == byAgent:
					outlineBadge(c, "From agent")
				}
			})
			if r.Clicked() {
				w.act(func() { w.toggleAttachedNotebook(id) }) // a row that cannot be ticked says why
			}
		}
		if h := a.nbRowH[a.nbWalk]; a.nbWalk != "" && h != nil {
			h.Focus(c)
			a.nbWalk = ""
		}
		ui.Row(c).FillWidth().Padding(space1_5, space2, space1).Children(func() {
			ui.Text(c, nbPickerNote).TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
		})
	})
	if !open {
		w.act(func() {
			if a.nbPicker == where {
				a.nbPicker = ""
			}
		})
	}
}

// ---- the card ----

// nbBtn is a card's button: named with the notebook so cards tell apart. A card whose request is
// out ignores a press.
func nbBtn(c *ui.Context, act, title string, busy bool) bool {
	shown, kind := "", btnGhost
	switch act {
	case actOpen:
		shown = "Open"
	case actUndo:
		shown = "Undo"
	case actRestore:
		shown = "Restore"
	case actKeep:
		shown = "Keep"
	case actDelete:
		shown, kind = "Delete…", btnDanger
	}
	name := shown + " " + title
	if busy {
		ui.Row(c).Height(sizeControlSm).Padding(0, space2).Justify(ui.Center).Shrink(0).Opacity(0.45).Label(name).Children(func() { label(c, shown, inkMuted) })
		return false
	}
	return textBtnAs(c, name, shown, kind)
}

// nbCardLabels are the menu items of a card: its buttons, in order.
var nbCardLabels = map[string]string{actOpen: "Open", actUndo: "Undo", actRestore: "Restore", actKeep: "Keep", actDelete: "Delete…"}

// agentNbCard is what the agent did to a notebook, or a delete it proposes. Its buttons are
// requests to keel serve; Delete asks first, and nothing is deleted until the user confirms.
func (w *workspace) agentNbCard(c *ui.Context, id string) {
	rec := w.agent.nbProps[id]
	if rec == nil {
		return
	}
	p := rec.p
	live := p.Status == "created" || p.Status == "edited" || p.Status == "proposed"
	warn := warned(p) && live && !p.Expired
	border := line
	if warn || (p.Kind == "delete" && p.Status == "proposed" && !p.Expired) {
		border = signal
	}
	head := cardHead(p)
	title := orElse(p.Title, "Notebook")
	card := ui.Column(c).FillWidth().Key("nb "+id).Radius(radiusMd).Background(controlFill(raised)).Border(1, border).Clip().Label(head)
	if !live || p.Expired {
		card.Opacity(0.92)
	}
	card.PopoverMenu(func(m *ui.Menu) { w.agentNbCardMenu(m, id) })
	card.Children(func() {
		ui.Row(c).FillWidth().Padding(space3, space3, 0).Gap(space2).Wrap().Children(func() {
			glyph := "notebook"
			if p.Kind == "delete" {
				glyph = "agent-trash"
			}
			icon(c, glyph, inkMuted)
			label(c, head, ink)
			ui.Spacer(c)
			if p.Kind == "create" && p.Status == "created" {
				outlineBadge(c, "From agent")
			}
			if p.Kind == "edit" && p.Status == "edited" {
				outlineBadge(c, "Edited by agent")
			}
			if warn {
				if p.FromPane {
					pill(c, "From pane output", flareSoft, flare)
				} else {
					pill(c, "After reading panes", flareSoft, flare)
				}
			}
			if live && p.Placeholders {
				pill(c, "Placeholders", signalSoft, signal)
			}
		})
		ui.Column(c).FillWidth().Padding(space2, space3, 0).Gap(space2).Children(func() {
			bodyStrong(c, title, ink)
			status := cardStatus(p)
			switch {
			case p.Expired && live:
				cardNote(c, "agent-info", inkMuted, "Keel no longer holds this conversation's changes. Use History on the notebook to see or restore a version.")
			case status != "" && p.Status == "refused":
				cardNote(c, "alert", signal, status)
			case status != "":
				cardNote(c, "agent-info", inkMuted, status)
			}
			if p.Kind == "" && p.Body != "" { // a draft from an earlier Keel: its text as it was, never saved
				ui.Scroll(c).FillWidth().MaxHeight(sizeRow*5).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("notebook draft").Children(func() {
					ui.Text(c, p.Body).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0)
				})
			}
			if p.Summary != "" && p.Kind != "delete" {
				ui.Text(c, p.Summary).TextColor(inkMuted).FontSize(captionSize)
			}
			if p.Summary != "" && p.Kind == "delete" {
				ui.Text(c, "Why: "+p.Summary).TextColor(inkMuted).FontSize(captionSize)
			}
			if warn {
				warnBlock(c, paneWarning(p))
			}
			if live && p.Withheld {
				cardNote(c, "agent-info", signal, "It names values Keel keeps from the agent as {{name}}. Replace them before you use a command.")
			}
			if live && p.Placeholders {
				cardNote(c, "agent-info", signal, "It holds placeholders such as [secret:1]. Fill them in before you use a command: Keel will not type a block that holds one.")
			}
			if rec.err != "" {
				cardNote(c, "alert", flare, rec.err)
			}
		})
		hairline(ui.Row(c).FillWidth().Margin(space3, 0, 0).Padding(space2, space3).Gap(space1_5).Background(w.ground(surface)), 1, 0, 0, 0).Children(func() {
			if p.Kind != "delete" && p.Bytes > 0 {
				size := countWord(p.Bytes, "byte", "bytes")
				if p.Rev > 0 {
					size += " · rev " + strconv.Itoa(p.Rev)
				}
				text(c, size, inkFaint, captionSize, weightRegular)
			}
			ui.Spacer(c)
			for _, act := range cardActions(p) {
				if nbBtn(c, act, title, rec.busy) {
					w.act(func() { w.cardAction(p, act) })
				}
			}
		})
	})
}

// cardAction is a card button: Open shows the notebook, the others are requests (Delete asks first).
func (w *workspace) cardAction(p keelclient.NotebookProposal, act string) {
	if act == actOpen {
		w.openNotebook(p.NotebookID)
		return
	}
	w.askNbPropAction(p.ID, act)
}

// agentNbCardMenu is the card's right-click menu: its own buttons, in order, each doing what its button does.
func (w *workspace) agentNbCardMenu(m *ui.Menu, id string) {
	rec := w.agent.nbProps[id]
	if rec == nil || rec.busy {
		return
	}
	p := rec.p
	for _, act := range cardActions(p) {
		if menuItem(m, nbCardLabels[act]).Chosen() {
			w.act(func() { w.cardAction(p, act) })
		}
	}
}
