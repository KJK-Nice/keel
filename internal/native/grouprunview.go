package native

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The group run page drawn as GroupRunPage (internal/ui/groupspage.templ) and paint in cmd/keel/web/js/ui/grouprun.js with the
// k-gx-* rules of keel.css: on the left the runbook's steps with their marks and where each runs, the safety line and Stop
// runbook; on the right the current step (the mode, the proposal card with the saved command and its {{names}}, the halt
// banner, the hosts that need a pane), the captured values as a mask and a length, and the Results matrix. Every command is
// drawn with variable names; a value never reaches this page (keelclient/grouprun_types.go).

func (w *workspace) groupRunView(c *ui.Context, p *groupRunPage) {
	run := p.run
	if run == nil {
		ui.Column(c).Grow(1).FillWidth().FillHeight().Padding(space8).AlignItems(ui.Center).Background(w.ground(stage)).Children(func() {
			help(c, orElse(p.err, "Start a runbook from a group's page: Run a runbook on this group."))
		})
		return
	}
	// .k-gx-split: the steps (size-agentpage-context wide) beside the detail; under 760 px of page width (#page-grouprun's
	// container query) they go above it, at most 40% of the height, with their border below instead of at the right.
	narrow := p.width > 0 && p.width < groupRunNarrow
	var root ui.Element
	if narrow {
		root = ui.Column(c).Grow(1).FillWidth().FillHeight().Label("group run page")
	} else {
		root = ui.Row(c).Grow(1).FillWidth().FillHeight().Label("group run page")
	}
	p.width = root.Bounds().W
	root.Children(func() {
		steps := ui.Column(c).Shrink(0).Background(w.ground(surface)).Label("Steps")
		if narrow {
			steps.FillWidth().MaxHeight(max(root.Bounds().H, 0) * 0.4)
			hairline(steps, 0, 0, 1, 0)
		} else {
			steps.Width(sizeAgentPageContext).FillHeight()
			hairline(steps, 0, 1, 0, 0)
		}
		steps.Children(func() {
			ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Children(func() { w.runSteps(c, p, run) })
		})
		ui.Scroll(c).Grow(1).FillHeight().Shrink(1).MinWidth(0).Background(w.ground(stage)).Label("Group run").Children(func() {
			ui.Column(c).FillWidth().Padding(space6, space8).Gap(space4).Children(func() {
				w.runDetail(c, p, run)
				if p.err != "" {
					ui.Row(c).FillWidth().Label("group run error").Children(func() { caption(c, p.err, flare).MaxLines(0) })
				}
				for i := range run.Steps {
					w.runCaptures(c, run, i)
				}
				w.runMatrix(c, p, run)
			})
		})
	})
}

func markColor(state string) ui.Color {
	switch state {
	case "done":
		return tide
	case "failed":
		return flare
	case "now":
		return brass
	case "skipped":
		return inkFaint
	}
	return inkMuted
}

// runSteps is the steps column: the head (runbook, title, counts), the steps and the foot.
func (w *workspace) runSteps(c *ui.Context, p *groupRunPage, run *keelclient.GroupRun) {
	changes := 0
	for _, s := range run.Steps {
		if s.Risk != "" && s.Risk != "readonly" {
			changes++
		}
	}
	line := countWord(len(run.Steps), "step", "steps") + " · all read-only"
	if changes > 0 {
		verb := "change"
		if changes == 1 {
			verb = "changes"
		}
		line = countWord(len(run.Steps), "step", "steps") + " · " + fmt.Sprintf("%d %s state", changes, verb)
	}
	if !run.StartedAt.IsZero() {
		line += " · started " + run.StartedAt.Local().Format("15:04") + " by you"
	}
	ui.Column(c).FillWidth().Padding(space6, space4, space3).Gap(space1).Children(func() {
		menuLabel(c, "Runbook on "+run.Group)
		ui.Text(c, run.Name).TextColor(ink).FontSize(titleLg).FontWeight(weightSemibold).MaxLines(0)
		help(c, line)
	})
	for i, st := range run.Steps {
		mark, state := stepMark(run, i)
		sub := "No command · you check it"
		if st.Command != "" {
			sub = targetLabel(st)
			if i == run.Current {
				sub += " · " + modeShort(run.Mode)
			}
		}
		ui.Row(c).FillWidth().Padding(space2, space4).Gap(space3).AlignItems(ui.Start).Key(st.ID + strconv.Itoa(i)).Label("step " + strconv.Itoa(i+1) + " " + st.Title).Children(func() {
			ui.Row(c).Size(space6, space6).Radius(radiusFull).Border(1, markColor(state)).Justify(ui.Center).Shrink(0).Children(func() {
				text(c, mark, markColor(state), captionSize, weightSemibold)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
				body(c, st.Title, ink).MaxLines(0)
				caption(c, sub, inkMuted).MaxLines(0)
			})
		})
	}
	hairline(ui.Column(c).FillWidth().Padding(space3, space4).Gap(space3), 1, 0, 0, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Start).Children(func() {
			icon(c, "shield-01", inkMuted)
			help(c, runSafetyLine).MaxLines(0)
		})
		if run.Status == "active" {
			ui.Row(c).Children(func() {
				if textBtn(c, "Stop runbook", btnGhost) {
					w.act(func() { w.askStopRun(p) })
				}
			})
		}
	})
}

// runBanner is a k-review: a line with a glyph, and buttons after it.
func runBanner(c *ui.Context, glyph string, col ui.Color, label string, text func(), buttons func()) {
	ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(hoverGround).AlignItems(ui.Center).Wrap().Label(label).Children(func() {
		icon(c, glyph, col)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(text)
		if buttons != nil {
			buttons()
		}
	})
}

func (w *workspace) runDetail(c *ui.Context, p *groupRunPage, run *keelclient.GroupRun) {
	switch run.Status {
	case "finished":
		runBanner(c, "tick", tide, "run finished", func() {
			body(c, "All steps are done or skipped.", ink)
			caption(c, "Logs keep what was typed on each host.", inkMuted)
		}, nil)
		return
	case "stopped":
		runBanner(c, "agent-info", inkMuted, "run stopped", func() {
			body(c, fmt.Sprintf("Runbook stopped at step %d.", stoppedAt(run)), ink)
			caption(c, "Logs keep what was typed on each host. Nothing more is proposed.", inkMuted)
		}, nil)
		return
	}
	if run.Current < 0 || run.Current >= len(run.Steps) {
		return
	}
	i := run.Current
	st := run.Steps[i]
	ui.Row(c).FillWidth().Gap(space3).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, fmt.Sprintf("Step %d · %s", i+1, st.Title)).TextColor(ink).FontSize(titleSize).FontWeight(weightSemibold).Grow(1).MaxLines(0)
		if st.Command != "" {
			w.runModeSeg(c, p, run)
		}
	})
	if st.Command != "" {
		h := modeHelp(run.Mode)
		if run.ModeLocked {
			h += " " + modeLockedText
		}
		help(c, h).MaxLines(0)
	}
	if st.Description != "" {
		body(c, st.Description, inkMuted).MaxLines(0)
	}
	switch {
	case st.Command == "":
		runBanner(c, "agent-info", inkMuted, "step has no command", func() {
			msg := "This step has no command. Check it yourself, then mark it done."
			if st.Expect != "" {
				msg += " Expected: " + st.Expect + "."
			}
			body(c, msg, ink).MaxLines(0)
		}, func() {
			if textBtn(c, "Mark step done", btnDefault) {
				w.act(func() { w.markStep(p, "", "done") })
			}
			if textBtn(c, "Skip step", btnGhost) {
				w.act(func() { w.markStep(p, "", "skip") })
			}
		})
		return
	case len(st.Cells) == 0:
		runBanner(c, "alert", signal, "step has no host", func() {
			msg := "No host in this group is one the agent may read."
			if st.Role != "" {
				msg = "No host in this group that the agent may read has the role " + st.Role + "."
			}
			body(c, msg+" Skip the step to go on.", ink).MaxLines(0)
		}, func() {
			if textBtn(c, "Skip step", btnGhost) {
				w.act(func() { w.markStep(p, "", "skip") })
			}
		})
		return
	}
	w.runProposalCard(c, p, run, i)
	if h := haltLine(run); h != nil {
		runBanner(c, "alert", flare, "run halted", func() {
			body(c, h.title, ink).MaxLines(0)
			if h.sub != "" {
				caption(c, h.sub, inkMuted).MaxLines(0)
			}
		}, func() {
			if textBtn(c, "Retry host", btnDefault) {
				w.act(func() { w.markStep(p, h.hostID, "retry") })
			}
			if textBtn(c, "Skip host", btnGhost) {
				w.act(func() { w.markStep(p, h.hostID, "skip") })
			}
			if textBtn(c, stopAction, btnDanger) {
				w.act(func() { w.askStopRun(p) })
			}
		})
	}
	if need := run.NeedPane; len(need) > 0 {
		runBanner(c, "agent-info", inkMuted, "hosts need a pane", func() {
			head := need[0].Host + " has no pane open here."
			if len(need) > 1 {
				head = fmt.Sprintf("%d hosts have no pane open here.", len(need))
			}
			body(c, head, ink).MaxLines(0)
			caption(c, "Open a session on each host to go on. Opening a session is up to you; nothing is typed into it until you approve.", inkMuted).MaxLines(0)
		}, func() {
			for _, h := range need {
				if textBtnAs(c, "Open a pane on "+h.Host, "Open "+h.Host, btnDefault) {
					id := h.HostID
					w.act(func() { w.openRunHost(id) })
				}
			}
		})
	}
}

// runModeSeg is Rolling | Parallel; both are off while the mode may not change (keel serve refuses it too, with 409).
func (w *workspace) runModeSeg(c *ui.Context, p *groupRunPage, run *keelclient.GroupRun) {
	ui.Row(c).Padding(space0_5).Gap(space0_5).Radius(radiusSm).Background(stage).Border(1, line).Label("How to run on several hosts").Children(func() {
		for _, o := range [][2]string{{"rolling", "Rolling"}, {"parallel", "Parallel"}} {
			on := run.Mode == o[0]
			r := ui.Row(c).Height(sizeControlSm).Padding(0, space3).Justify(ui.Center).Radius(radiusXs).Label(o[1]).Cursor(ui.CursorPointer)
			col := inkMuted
			switch {
			case on:
				r.Background(raised).Shadow(0, 0, 0, 1, line)
				col = ink
			case r.Hovered() && !run.ModeLocked:
				r.Background(hoverGround)
			}
			if run.ModeLocked {
				r.Opacity(disabledAlpha).Disabled(true)
			}
			r.Children(func() { text(c, o[1], col, uiFontSize, weightMedium) })
			if r.Clicked() && !run.ModeLocked {
				mode := o[0]
				w.act(func() { w.setRunMode(p, mode) })
			}
		}
	})
}

// runProposalCard is the step's proposal: where it runs, its kind, the bytes with the variable names, and Type into N panes.
func (w *workspace) runProposalCard(c *ui.Context, p *groupRunPage, run *keelclient.GroupRun, i int) {
	st := run.Steps[i]
	ready := typeable(run, w.groupPaneOpen)
	var names []string
	for _, cell := range st.Cells {
		if cell.Host != "" {
			names = append(names, cell.Host)
		}
	}
	hosts := joinWords(names)
	if len(names) > 3 {
		hosts = names[0] + " … " + names[len(names)-1]
	}
	if len(names) == 0 {
		hosts = "one host"
	}
	if st.OneHost {
		hosts = "one of " + hosts
	}
	prod, shift := 0, false
	for _, cell := range ready {
		if cell.Proposal.Production {
			prod++
		}
		shift = shift || cell.Proposal.NeedsShift
	}
	bytes := "\x15" + st.Command + "\n"
	if len(ready) > 0 {
		bytes = ready[0].Proposal.Bytes
	}
	uses := usesLine(st, run.Steps, i)
	label := "Nothing to type now"
	if len(ready) > 0 {
		label = typeWord("Type into "+countWord(len(ready), "pane", "panes"), false) // the dialog always follows
	}
	ui.Column(c).FillWidth().Radius(radiusMd).Background(raised).Border(1, line).Clip().Label("Step " + strconv.Itoa(i+1) + " proposal").Children(func() {
		ui.Row(c).FillWidth().Padding(space3, space3, 0).Gap(space2).AlignItems(ui.Center).Wrap().Children(func() {
			icon(c, "agent-cmd", inkMuted)
			label := "every host"
			if st.Role != "" {
				label = st.Role
			}
			caption(c, "Run on", inkMuted)
			pill(c, label, hoverGround, inkMuted)
			body(c, hosts, ink)
			ui.Spacer(c)
			lbl, kind := riskBadge(st.Risk)
			kindPill(c, lbl, kind)
		})
		ui.Column(c).FillWidth().Padding(space2, space3, 0).Gap(space2).Children(func() {
			ui.Column(c).FillWidth().Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("Exact bytes").Children(func() {
				bytesNamesView(c, bytes)
			})
			if prod > 0 {
				noun := "host is"
				if prod > 1 {
					noun = "hosts are"
				}
				msg := fmt.Sprintf("%d of these %s production.", prod, noun)
				if shift {
					msg += " Typing there needs ⇧."
				}
				cardNote(c, "alert", signal, msg+" Tick each host in the next step only after you checked the command for it.")
			}
		})
		hairline(ui.Row(c).FillWidth().Margin(space3, 0, 0).Padding(space2, space3).Gap(space2).AlignItems(ui.Center).Background(w.ground(surface)), 1, 0, 0, 0).Children(func() {
			count := bytesLine(bytes) + " · same bytes in every pane"
			if uses != "" {
				count = uses
			}
			caption(c, count, inkFaint).Shrink(1).MinWidth(0)
			ui.Spacer(c)
			if uses != "" && textBtnAs(c, "Copy the command without values", "Copy", btnGhost) {
				w.act(func() { c.WriteClipboard(st.Command); w.say("Copied with the {{names}} in, not their values.") })
			}
			shown := label
			if shift && len(ready) > 0 {
				shown += " ⇧"
			}
			off := len(ready) == 0 || p.typing
			busyMark(c, p.typing, "Filling in the values")
			if textBtnOffAs(c, label, shown, btnPrimary, off) {
				w.act(w.typeGroupRun)
			}
		})
	})
	if uses != "" {
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Start).Children(func() {
			icon(c, "shield-01", inkMuted)
			help(c, fillNote).MaxLines(0)
		})
	}
}

// bytesNamesView is bytesView with each {{name}} drawn as a chip: the bytes as keel serve holds them, names and all.
func bytesNamesView(c *ui.Context, bytes string) {
	var spans []ui.Span
	for _, seg := range splitVars(bytes) {
		if seg.isName {
			spans = append(spans, ui.Span{Text: seg.text, Color: brassInk, Background: brassSoft, Weight: weightMedium, Radius: radiusXs, PaddingX: 3})
			continue
		}
		for _, r := range bytesRuns(seg.text) {
			sp := ui.Span{Text: r.text}
			if r.ctl {
				sp.Color, sp.Background, sp.Weight, sp.Radius, sp.PaddingX = brassInk, brassSoft, weightMedium, radiusXs, 3
			}
			spans = append(spans, sp)
		}
	}
	ui.RichText(c, spans...).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0).Label(bytesText(bytes))
}

// textBtnOffAs is textBtnAs that ignores a press and dims while off.
func textBtnOffAs(c *ui.Context, name, shown string, kind btnKind, off bool) bool {
	return ctlButton(c, name, shown, kind).Disabled(off).Clicked() && !off
}

// ---- the captured values ----

// runCaptures is a step's box of captured values (board CaptureVars): each name, its pattern and, per host, a mask and the
// length, or why there is none. keel serve never sends the value to this page.
func (w *workspace) runCaptures(c *ui.Context, run *keelclient.GroupRun, i int) {
	st := run.Steps[i]
	if len(st.Captures) == 0 {
		return
	}
	var cells []keelclient.GroupRunCell
	for _, cell := range st.Cells {
		if cell.HostID != "" {
			cells = append(cells, cell)
		}
	}
	ui.Column(c).FillWidth().Padding(space3).Gap(space2).Radius(radiusMd).Border(1, line).Background(w.ground(surface)).Label("Values captured from step " + strconv.Itoa(i+1)).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Wrap().Children(func() {
			icon(c, "shield-01", inkMuted)
			body(c, fmt.Sprintf("Captured from step %d · %s", i+1, st.Title), ink).Grow(1)
			pill(c, "Captured by Keel, never sent to the agent", brassSoft, brassInk)
		})
		for _, cap := range st.Captures {
			ui.Column(c).FillWidth().Gap(space1).Key(cap.Name).Label("capture " + cap.Name).Children(func() {
				ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
					caption(c, "Name", inkFaint).Width(space8 * 2)
					bytesNamesView(c, "{{"+cap.Name+"}}")
				})
				ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
					caption(c, "Pattern", inkFaint).Width(space8 * 2)
					mono(c, cap.Pattern, inkMuted).MaxLines(0)
				})
				if len(cells) == 0 {
					help(c, "Not captured yet.")
				}
				for _, cell := range cells {
					var cv *keelclient.CaptureView
					for k := range cell.Captures {
						if cell.Captures[k].Name == cap.Name {
							cv = &cell.Captures[k]
						}
					}
					mask, why, state := captureView(cv)
					ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Key(cell.HostID).Label("capture " + cap.Name + " on " + cell.Host + " " + state).Children(func() {
						body(c, cell.Host, ink)
						if mask != "" {
							mono(c, mask, inkMuted)
						}
						caption(c, why, inkMuted).MaxLines(0)
					})
				}
			})
		}
		help(c, expiryLine(st))
	})
}

// ---- the results matrix ----

var matrixCols = [3]float32{3, 2, 2} // Host, Role, and each step

func (w *workspace) runMatrix(c *ui.Context, p *groupRunPage, run *keelclient.GroupRun) {
	// Rows: the run's target hosts (any step), in member order.
	var targets []keelclient.GroupRunMember
	for _, m := range run.Members {
		if m.Excluded != "" {
			continue
		}
		for _, st := range run.Steps {
			if cellOf(st, m.HostID) != nil || candidateOf(st, m.HostID) {
				targets = append(targets, m)
				break
			}
		}
	}
	var cur *keelclient.GroupRunStep
	if run.Current >= 0 && run.Current < len(run.Steps) {
		cur = &run.Steps[run.Current]
	}
	head := runStatusLine(run)
	if cur != nil {
		head = fmt.Sprintf("Step %d: %s", run.Current+1, stepSummary(*cur))
	}
	ui.Column(c).FillWidth().Padding(space3).Gap(space2).Radius(radiusMd).Border(1, line).Background(w.ground(surface)).Label("Results").Children(func() {
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
			text(c, "Results", ink, uiFontSize, weightSemibold)
			ui.Spacer(c)
			help(c, head)
		})
		ui.Column(c).FillWidth().Label("Result per host and step").Children(func() {
			hairline(ui.Row(c).FillWidth().Height(sizeControl).Gap(space2).AlignItems(ui.Center), 0, 0, 1, 0).Children(func() {
				ui.Row(c).Grow(matrixCols[0]).Basis(0).MinWidth(0).Children(func() { label(c, "Host", inkMuted) })
				ui.Row(c).Grow(matrixCols[1]).Basis(0).MinWidth(0).Children(func() { label(c, "Role", inkMuted) })
				for i := range run.Steps {
					ui.Row(c).Grow(matrixCols[2]).Basis(0).MinWidth(0).Children(func() { label(c, "Step "+strconv.Itoa(i+1), inkMuted) })
				}
				ui.Row(c).Grow(5).Basis(0).MinWidth(0).Children(func() {})
			})
			if len(targets) == 0 {
				help(c, "No host in this group is a target: none has the roles these steps name, or the agent may read none of them.")
			}
			for _, m := range targets {
				w.runMatrixRow(c, p, run, m, cur)
			}
		})
		var left []string
		for _, m := range run.Members {
			if m.Excluded != "" {
				left = append(left, m.Host+" ("+m.Excluded+")")
			}
		}
		if len(left) > 0 {
			help(c, "Left out: "+joinWords(left)+". The run never types into them.").MaxLines(0)
		}
		help(c, runReportedLine).MaxLines(0)
	})
}

func cellOf(st keelclient.GroupRunStep, hostID string) *keelclient.GroupRunCell {
	for i := range st.Cells {
		if st.Cells[i].HostID == hostID {
			return &st.Cells[i]
		}
	}
	return nil
}

func candidateOf(st keelclient.GroupRunStep, hostID string) bool {
	for _, h := range st.Candidates {
		if h.HostID == hostID {
			return true
		}
	}
	return false
}

func (w *workspace) runMatrixRow(c *ui.Context, p *groupRunPage, run *keelclient.GroupRun, m keelclient.GroupRunMember, cur *keelclient.GroupRunStep) {
	ui.Row(c).FillWidth().MinHeight(sizeControl).Padding(space1, 0).Gap(space2).AlignItems(ui.Center).Key(m.HostID).Label("result row " + m.Host).Children(func() {
		ui.Row(c).Grow(matrixCols[0]).Basis(0).MinWidth(0).Gap(space1_5).AlignItems(ui.Center).Children(func() {
			body(c, m.Host, ink).Shrink(1).MinWidth(0)
			if m.Production {
				pill(c, "Production", signalSoft, signal)
			}
		})
		ui.Row(c).Grow(matrixCols[1]).Basis(0).MinWidth(0).Children(func() { mono(c, m.Role, inkMuted) })
		for _, st := range run.Steps {
			ui.Row(c).Grow(matrixCols[2]).Basis(0).MinWidth(0).Children(func() {
				cell := cellOf(st, m.HostID)
				if cell == nil {
					if candidateOf(st, m.HostID) {
						caption(c, "· maybe", inkMuted)
					}
					return
				}
				v := cellView(cell)
				col := inkMuted
				switch v.kind {
				case "ok":
					col = tide
				case "fail":
					col = flare
				case "run":
					col = brass
				}
				text(c, v.glyph+" "+v.text, col, captionSize, weightMedium)
			})
		}
		ui.Row(c).Grow(5).Basis(0).MinWidth(0).Gap(space1).Wrap().Justify(ui.End).Children(func() {
			if cur != nil && run.Status == "active" {
				if cell := cellOf(*cur, m.HostID); cell != nil {
					w.runCellActions(c, p, m.Host, cell)
				}
			}
		})
	})
}

// runCellActions are the current step's actions on one host (cellActions): Confirm, Mark done, Retry, Skip. Mark done waits
// while the shell may still report the exit status (keel serve refuses it too).
func (w *workspace) runCellActions(c *ui.Context, p *groupRunPage, host string, cell *keelclient.GroupRunCell) {
	btn := func(action, shown, name string, kind btnKind) {
		if textBtnAs(c, name+" "+host, shown, kind) {
			id := cell.HostID
			w.act(func() { w.markStep(p, id, action) })
		}
	}
	switch cell.State {
	case "typed":
		switch {
		case cell.Confirm:
			btn("confirm", "Confirm", "Confirm", btnDefault)
			btn("retry", "Retry", "Retry", btnGhost)
		case cell.Pending:
			btn("retry", "Retry", "Retry", btnGhost)
		default:
			btn("done", "Mark done", "Mark done", btnGhost)
			btn("retry", "Retry", "Retry", btnGhost)
		}
	case "failed":
		btn("retry", "Retry", "Retry", btnDefault)
		btn("skip", "Skip", "Skip", btnGhost)
		btn("done", "Mark done", "Mark done", btnGhost)
	case "proposed":
		btn("skip", "Skip", "Skip", btnGhost)
	case "waiting":
		if cell.HostID != "" {
			btn("skip", "Skip", "Skip", btnGhost)
		}
	}
}
