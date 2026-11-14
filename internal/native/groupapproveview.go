package native

import (
	"github.com/egoist/mygo/ui"
)

// The group approve dialog drawn as groupApproveDialog (internal/ui/groupspage.templ) and paintApprove (cmd/keel/web/js/ui/
// grouprun.js) with the .k-gx-approve rules of keel.css (docs/ui-redesign.md §17, §6 rule 10): the exact bytes once with their
// line and byte count, then one row per host with a tick, the pane's tab and Production and ⇧ badges. Nothing is ticked in
// advance unless there is only one host. The keyboard starts on Cancel, so a stray ↵ never types. Type is a click (⇧ held
// when a ticked host needs it), or Tab and ↵ on it.
//
// After a Type each row that was reached shows what happened to it: "Typed." or keel serve's refusal, in its words. A row
// that was reached is final: it cannot be ticked again and nothing is retried.

// The webview's words.
const (
	groupApproveTitle = "Type into these panes?"
	groupApproveHint  = "Tick each host to type into. Each one is checked and typed on its own. The current line in each pane is cleared first."
	groupNoneTypeable = "No host can be typed into now."
	groupValuesDiffer = "The filled-in values differ per host: each row shows the exact bytes for its pane."
	groupShiftNeeded  = "A ticked host is production (or the command is destructive): hold ⇧ and press Type."
	groupShiftRefused = "Hold ⇧ and press Type: a ticked host is production, or the command is destructive."
)

func (w *workspace) groupApproveView(c *ui.Context, d *approveDialog) {
	g := d.group()
	open := true
	var typeable []*groupRow // the rows keel serve made bytes for
	attempted := false
	for _, r := range g.rows {
		if r.err == "" {
			typeable = append(typeable, r)
		}
		attempted = attempted || r.attempted
	}
	same := rowGroups(typeable) <= 1
	sameUsed := sameSources(typeable)
	live := g.effective() // what a Type would type now: a ticked row whose pane is gone does not count
	n := len(live)
	shift := rowsNeedShift(live)
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth * 1.4).Padding(space4).Gap(space3).Label("dialog " + groupApproveTitle)
		text(c, groupApproveTitle, ink, titleSize, weightSemibold)
		ui.Text(c, groupApproveHint).TextColor(inkMuted).FontSize(uiFontSize)
		if same && len(typeable) > 0 {
			ui.Scroll(c).FillWidth().MaxHeight(approvePreviewMax).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("Exact bytes").Children(func() {
				bytesView(c, typeable[0].bytes)
			})
		}
		count := groupNoneTypeable
		switch {
		case len(typeable) > 0 && same:
			count = bytesLine(typeable[0].bytes) + " · the same bytes in every pane"
			if sameUsed && anyUsed(typeable) {
				count += " · filled in by Keel: " + usedLine(typeable[0].used)
			}
		case len(typeable) > 0:
			count = groupValuesDiffer
		}
		help(c, count)
		ui.Scroll(c).FillWidth().MaxHeight(approvePreviewMax * 2).Label("Hosts to type into").Children(func() {
			ui.Column(c).FillWidth().Gap(space1_5).Children(func() {
				for _, r := range g.rows {
					w.groupApproveRow(c, d, r, same, sameUsed)
				}
			})
		})
		switch {
		case g.shiftHint:
			ui.Row(c).FillWidth().Label("approve shift").Children(func() { caption(c, groupShiftRefused, flare) })
		case shift:
			ui.Row(c).FillWidth().Label("approve shift").Children(func() { caption(c, groupShiftNeeded, inkMuted) })
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			cancel := "Cancel"
			if attempted {
				cancel = "Close" // something was typed: the dialog no longer cancels anything
			}
			if dialogBtn(c, cancel, btnGhost, &d.cancelH, false) {
				open = false
			}
			shown := typeLabel(live)
			if shift {
				shown += " ⇧↵"
			}
			if click, mods := dialogBtnMods(c, "Type", shown, btnPrimary, &d.typeH, n == 0 || d.busy); click {
				w.pressType(mods&ui.Shift != 0)
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

func anyUsed(rows []*groupRow) bool {
	for _, r := range rows {
		if len(r.used) > 0 {
			return true
		}
	}
	return false
}

// groupApproveRow is one host: its tick, name, pane, badges and, as the app has them, its own bytes when they differ and
// where its values came from.
func (w *workspace) groupApproveRow(c *ui.Context, d *approveDialog, r *groupRow, same, sameUsed bool) {
	g := d.group()
	why := g.rowBlocked(r)
	off := why != "" || r.attempted || d.busy
	st := checkOff
	if r.checked && !r.attempted && why == "" {
		st = checkOn
	}
	ui.Column(c).FillWidth().Key(r.proposalID).Gap(space1).Padding(space1_5, space2).Radius(radiusSm).Border(1, line).Label("approve row " + r.host).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
			if checkBox(c, "Type into "+r.host, st, off) {
				w.act(func() {
					if g.rowBlocked(r) == "" && !r.attempted {
						r.checked = !r.checked
						g.shiftHint = false
					}
				})
			}
			body(c, r.host, ink).Shrink(1).MinWidth(0)
			if r.paneLabel != "" {
				caption(c, r.paneLabel, inkMuted).Shrink(1).MinWidth(0)
			}
			ui.Spacer(c)
			if r.production {
				pill(c, "Production", signalSoft, signal)
			}
			if r.needsShift {
				pill(c, "⇧", hoverGround, inkMuted)
			}
		})
		switch {
		case r.err == "" && !same:
			ui.Scroll(c).FillWidth().MaxHeight(approvePreviewMax).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("Exact bytes for " + r.host).Children(func() {
				bytesView(c, r.bytes)
			})
			meta := bytesLine(r.bytes)
			if len(r.used) > 0 {
				meta += " · " + usedLine(r.used)
			}
			help(c, meta)
		case r.err == "" && !sameUsed:
			if len(r.used) > 0 {
				help(c, "Filled in by Keel: "+usedLine(r.used))
			} else {
				help(c, "No variables")
			}
		}
		switch {
		case r.err != "":
			ui.Row(c).FillWidth().Label("approve row error").Children(func() { caption(c, r.err, flare).MaxLines(0) })
		case r.attempted && r.outcome != "":
			col := flare
			if r.typedOK {
				col = tide
			}
			ui.Row(c).FillWidth().Label("approve row result").Children(func() { caption(c, r.outcome, col).MaxLines(0) })
		case why != "":
			ui.Row(c).FillWidth().Label("approve row blocked").Children(func() { caption(c, why, flare) })
		}
	})
}
