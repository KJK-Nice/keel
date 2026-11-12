package native

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Runbooks page, the card of a runbook in use and the card of a runbook the agent proposes, drawn as
// internal/ui/runbookspage.templ, cmd/keel/web/js/ui/runbooks.js and ui/agentthread.js (placeRun, paintRun, paintBookProp)
// do. Colours and sizes are tokens.go's.
//
// In the app and not here: the "," that ends a host chip (↵ ends it).
// A step's role, "One host only" and captures (group runs, §17) are edited in runbookStepTarget.

// badgeColors are a k-badge's ground and ink for a kind ("" is the plain one).
func badgeColors(kind string) (bg, fg ui.Color) {
	switch kind {
	case "tide":
		return tideSoft, tide
	case "flare":
		return flareSoft, flare
	case "signal":
		return signalSoft, signal
	case "haze":
		return hazeSoft, haze
	}
	return hoverGround, inkMuted
}

func kindPill(c *ui.Context, label, kind string) ui.Element {
	bg, fg := badgeColors(kind)
	return pill(c, label, bg, fg)
}

// ---- the page ----

func (w *workspace) runbooksView(c *ui.Context, p *runbooksPage) {
	ui.Row(c).Grow(1).FillWidth().FillHeight().Children(func() {
		hairline(ui.Column(c).Width(sizeList).Shrink(0).FillHeight().Background(w.ground(surface)), 0, 1, 0, 0).Children(func() {
			w.runbooksHead(c, p)
			ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Label("runbooks list").Children(func() { w.runbooksList(c, p) })
			hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space2, space4), 1, 0, 0, 0).Children(func() {
				if !p.unreadable && p.err == "" {
					caption(c, countWord(len(p.list), "runbook", "runbooks")+" · stored in the vault", inkFaint)
				}
			})
		})
		ui.Scroll(c).Grow(1).FillHeight().Shrink(1).MinWidth(0).Background(w.ground(stage)).Label("runbook editor").ObserveInput(w.rbKeys).Children(func() {
			ui.Column(c).FillWidth().Padding(space6, space8).Gap(space4).Children(func() {
				if p.ed == nil {
					ui.Column(c).FillWidth().Padding(space8, 0).AlignItems(ui.Center).Children(func() {
						help(c, "Select a runbook to edit it, or add one with New.")
					})
					return
				}
				w.runbookEditor(c, p, p.ed)
			})
		})
	})
}

// runbooksHead is the list's head: the title with New, "From a conversation…" and its menu, and the filter.
func (w *workspace) runbooksHead(c *ui.Context, p *runbooksPage) {
	hairline(ui.Column(c).FillWidth().Shrink(0).Padding(space6, space4, space3).Gap(space3), 0, 0, 1, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			ui.Text(c, "Runbooks").TextColor(ink).FontSize(titleLg).FontWeight(weightSemibold).Grow(1).MaxLines(1)
			if textBtnIcon(c, "add-01", "New", btnDefault) {
				w.act(w.newRunbook)
			}
		})
		ui.Row(c).FillWidth().Gap(space1_5).Children(func() {
			if textBtnIcon(c, "notebook-pen", "From a notebook…", btnGhost) {
				w.act(w.openNotebookMenu)
			}
			if textBtnIcon(c, "agent-history", "From a conversation…", btnGhost) {
				w.act(w.openConversationMenu)
			}
		})
		if p.menuNB {
			w.notebookSourceMenu(c)
		}
		if p.menu {
			w.conversationMenu(c, p)
		}
		ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Background(controlFill(stage)).Border(1, lineStrong).Children(func() {
			icon(c, "search-01", inkFaint)
			ph := "Filter " + countWord(len(p.list), "runbook", "runbooks")
			ui.TextInputBase(c, &p.filter).Label("Filter runbooks").Placeholder(ph).PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1).MinWidth(0).Bind(&p.filterH)
		})
	})
}

// notebookSourceMenu is the list under "From a notebook…": every notebook, private ones too (the draft says when its notebook
// is private). A click makes a draft, which is not kept until Save.
func (w *workspace) notebookSourceMenu(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(space1).Gap(space0_5).Radius(radiusSm).Background(raised).Border(1, line).Label("runbook sources").Children(func() {
		help(c, "Make a draft from a notebook: its headings become steps, its shell blocks their commands.")
		if len(w.notebooks) == 0 {
			help(c, "No notebooks. Write one on the Notebooks page.")
		}
		for _, nb := range sortedNotebooks(w.notebooks) {
			id, name := nb.ID, notebookName(nb)
			r := ui.Column(c).FillWidth().Key(id).Padding(space1_5, space2).Radius(radiusSm).Cursor(ui.CursorPointer).Label("draft from notebook " + name)
			if r.Hovered() {
				r.Background(hoverGround)
			}
			r.Children(func() { body(c, name, ink) })
			if r.Clicked() {
				w.act(func() { w.draftRunbookOfNotebook(id) })
			}
		}
	})
}

// conversationMenu is the list under "From a conversation…": a click makes a draft, which is not kept until Save.
func (w *workspace) conversationMenu(c *ui.Context, p *runbooksPage) {
	ui.Column(c).FillWidth().Padding(space1).Gap(space0_5).Radius(radiusSm).Background(raised).Border(1, line).Label("runbook sources").Children(func() {
		help(c, "Make a draft from a conversation: its proposals become steps.")
		switch {
		case p.menuLoad:
			help(c, "Loading…")
		case len(p.menuItems) == 0:
			help(c, p.menuEmpty)
		}
		now := time.Now()
		for _, s := range p.menuItems {
			r := ui.Column(c).FillWidth().Key(s.ID).Padding(space1_5, space2).Radius(radiusSm).Cursor(ui.CursorPointer).Label("draft from " + orElse(s.Title, "Conversation"))
			if r.Hovered() {
				r.Background(hoverGround)
			}
			r.Children(func() {
				body(c, orElse(s.Title, "Conversation"), ink)
				caption(c, sessionWhen(s.UpdatedAt, now)+" · "+strconv.Itoa(s.Proposals)+" proposals", inkFaint)
			})
			if r.Clicked() {
				id := s.ID
				w.act(func() { w.draftRunbook(id) })
			}
		}
	})
}

func (w *workspace) runbooksList(c *ui.Context, p *runbooksPage) {
	ui.Column(c).FillWidth().Children(func() {
		switch {
		case p.unreadable:
			if emptySide(c, emptyFailed("book", "runbooks", "keel.memory cannot be read. Delete all notes in Settings · Agent starts a fresh file.")) {
				w.act(w.loadRunbooks)
			}
			return
		case p.err != "":
			if emptySide(c, emptyFailed("book", "runbooks", p.err)) {
				w.act(w.loadRunbooks)
			}
			return
		case len(p.list) == 0:
			if p.loaded {
				if emptySide(c, emptyNone("book", "No runbooks yet", runbooksNone, "New runbook")) {
					w.act(w.newRunbook)
				}
			}
			return
		}
		shown := filterRunbooks(p.list, p.filter)
		if len(shown) == 0 {
			if emptySide(c, emptyNoMatch("search-01", p.filter)) {
				w.act(func() { p.filter = "" })
			}
			return
		}
		for _, rb := range shown {
			w.runbookRow(c, p, rb)
		}
	})
}

// runbookRow is a list row: its name, where it came from, Use when and its hosts, and the Agent may pick switch.
func (w *workspace) runbookRow(c *ui.Context, p *runbooksPage, rb keelclient.Runbook) {
	selected := rb.ID == p.selected
	row := hairline(ui.Row(c).FillWidth().Padding(space2, space4).Gap(space3).Key(rb.ID).Selected(selected), 0, 0, 1, 0)
	if selected {
		row.Background(hoverGround)
	}
	row.Children(func() {
		pick := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Gap(space3).Cursor(ui.CursorPointer).Label("Open " + rb.Name)
		pick.Children(func() {
			ui.Row(c).Size(sizeControl, sizeControl).Radius(radiusSm).Justify(ui.Center).Shrink(0).Background(inkMuted.Alpha(tileTintAlpha)).Children(func() {
				iconSized(c, "book", inkMuted, iconSize-space0_5)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
				ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
					bodyStrong(c, rb.Name, ink).Shrink(1).MinWidth(0)
					label, kind := runbookSourceBadge(rb)
					kindPill(c, label, kind)
				})
				if rb.When != "" {
					text(c, rb.When, inkMuted, itemSub, weightRegular).MinWidth(0)
				}
				if len(rb.Hosts) > 0 {
					ui.Row(c).FillWidth().Gap(space1).Wrap().Children(func() {
						for _, h := range rb.Hosts {
							pill(c, h, hoverGround, inkMuted)
						}
					})
				}
			})
		})
		if pick.Clicked() {
			id := rb.ID
			w.act(func() { w.selectRunbook(id) })
		}
		on := rb.AgentMayPick
		ui.Switch(c, &on).Label("Agent may pick " + rb.Name + " switch").Shrink(0)
		if on != rb.AgentMayPick {
			id := rb.ID
			w.act(func() { w.setRunbookPick(id, on) })
		}
	})
}

// ---- the editor ----

func (w *workspace) runbookEditor(c *ui.Context, p *runbooksPage, e *rbEditor) {
	saved := e.id != ""
	ui.Column(c).FillWidth().Gap(space4).Label("Runbook").Children(func() {
		ui.Row(c).FillWidth().Gap(space2).Wrap().AlignItems(ui.Center).Children(func() { // wraps when the panel narrows the page
			ui.Column(c).Grow(1).Basis(sizeRail).MinWidth(0).Children(func() {
				inputBox(c, "Runbook name", &e.name, "Name this runbook", &e.nameH, false, false, 120, nil)
			})
			if saved {
				if textBtn(c, "Use in agent", btnDefault) {
					w.act(w.useEditedRunbook)
				}
			} else {
				ui.Row(c).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).Shrink(0).Opacity(0.45).Border(1, lineStrong).Label("Use in agent").
					Children(func() { label(c, "Use in agent", ink) })
			}
			del := "Discard"
			if saved {
				del = "Delete"
			}
			if textBtn(c, del, btnGhost) {
				w.act(w.askDeleteRunbook)
			}
			if saveBtn(c, p.saving) {
				w.act(w.saveRunbook)
			}
		})
		rb := e.asRunbook()
		if saved && runbookNeedsReview(rb) {
			ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(signalSoft).AlignItems(ui.Start).Label("runbook review").Children(func() {
				icon(c, "alert", signal)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
					bodyStrong(c, "Not enabled yet. Review before enabling.", ink)
					ui.Text(c, runbookReviewText(rb)).TextColor(ink).FontSize(captionSize)
				})
				if textBtn(c, "Enable", btnDefault) {
					id := e.id
					w.act(func() { e.agentMayPick = true; w.setRunbookPick(id, true) })
				}
			})
		}
		if !saved && e.source != "user" {
			from := "the conversation"
			if e.source == "notebook" {
				from = "the notebook"
			}
			msg := "A draft from " + from
			if e.sourceRef != "" {
				msg += " “" + e.sourceRef + "”"
			}
			msg += ". Nothing is saved until you press Save, and the agent may not pick it until you turn that on."
			if e.fromPrivate {
				msg += " The notebook is private, but a runbook is not: once saved, the agent can be sent this runbook when you use it."
			}
			ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(hoverGround).AlignItems(ui.Start).Label("runbook draft").Children(func() {
				icon(c, "agent-info", inkMuted)
				ui.Text(c, msg).TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
			})
		}
		if e.err != "" {
			ui.Row(c).FillWidth().Label("runbook error").Children(func() { ui.Text(c, e.err).TextColor(flare).FontSize(captionSize) })
		}
		ui.Row(c).FillWidth().Gap(space6).Wrap().AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Basis(sizeList).MinWidth(0).Gap(space3).Children(func() {
				labeled(c, "Use when", &e.whenH, func() {
					inputBox(c, "Use when", &e.when, "A Patroni replica is behind or its WAL stream keeps restarting.", &e.whenH, false, false, 300, nil)
					help(c, "The agent compares this line with the question and the panes in scope to decide whether the runbook fits.")
				})
				menuLabel(c, "Steps")
				ui.Column(c).FillWidth().Gap(space3).Label("runbook steps").Children(func() {
					for i, s := range e.steps {
						w.runbookStep(c, e, i, s)
					}
				})
				if len(e.steps) >= maxRunbookSteps {
					ui.Row(c).Opacity(0.45).Label("Add step").Children(func() { label(c, "Add step", inkMuted) })
				} else if textBtnIcon(c, "add-01", "Add step", btnGhost) {
					w.act(func() {
						e.steps = append(e.steps, newRbStep(keelclient.RunbookStep{}))
						e.focusNew = len(e.steps)
					})
				}
			})
			w.runbookBox(c, e, rb)
		})
	})
	if e.focusName {
		e.nameH.Focus(c)
		e.focusName = false
	}
	if e.focusNew > 0 && e.focusNew <= len(e.steps) {
		e.steps[e.focusNew-1].titleH.Focus(c)
		e.focusNew = 0
	}
	if e.focusHost {
		e.hostH.Focus(c)
		e.focusHost = false
	}
}

func (w *workspace) runbookStep(c *ui.Context, e *rbEditor, i int, s *rbStepState) {
	n := strconv.Itoa(i + 1)
	ui.Row(c).FillWidth().Key(s).Gap(space3).AlignItems(ui.Start).Label("runbook step " + n).Children(func() {
		text(c, n, inkMuted, uiFontSize, weightSemibold).Shrink(0).Width(space6)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1_5).Children(func() {
			ui.Row(c).FillWidth().Gap(space2).Wrap().AlignItems(ui.Center).Children(func() { // wraps when the panel narrows the page
				ui.Row(c).Grow(1).Basis(sizeRail).MinWidth(0).Children(func() {
					inputBox(c, "Step "+n+" title", &s.title, "What this step does", &s.titleH, false, false, 200, nil)
				})
				if s.command != "" || s.risk != "" {
					label, kind := runbookStepKind(s.command, s.risk)
					kindPill(c, label, kind)
				}
				if iconBtnOff(c, "chevron-up", "Move step "+n+" up", sizeControlSm, false, i == 0) {
					w.act(func() { e.steps = moveItem(e.steps, i, -1) })
				}
				if iconBtnOff(c, "chevron-down", "Move step "+n+" down", sizeControlSm, false, i == len(e.steps)-1) {
					w.act(func() { e.steps = moveItem(e.steps, i, 1) })
				}
				if iconBtn(c, "agent-trash", "Remove step "+n, sizeControlSm, false) {
					w.act(func() { e.steps = append(e.steps[:i:i], e.steps[i+1:]...) })
				}
			})
			inputBox(c, "Step "+n+" description", &s.description, "Why, and what to look for", &s.descH, false, false, 1000, nil)
			ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
				ui.TextAreaBase(c, &s.command).Label("Step "+n+" command").Placeholder("Command (optional: a step without one is a check you mark done)").PlaceholderColor(inkFaint).
					TextColor(ink).FontSize(captionSize).Font(monoStack).Lines(2, 8).FillWidth().Bind(&s.cmdH)
			})
			inputBox(c, "Step "+n+" expected result", &s.expect, "Expected result (optional)", &s.expH, false, false, 300, nil)
			w.runbookStepTarget(c, s, n)
		})
	})
}

// runbookStepTarget is where a step runs in a group run (§17): a role, One host only and the values it captures
// (ui/runbooks.js step-target and capturesHTML). A single-host run ignores all of it.
func (w *workspace) runbookStepTarget(c *ui.Context, s *rbStepState, n string) {
	ui.Row(c).FillWidth().Gap(space3).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Row(c).Grow(1).Basis(sizeRail).MinWidth(0).Children(func() {
			inputBox(c, "Step "+n+" role", &s.role, "Role in a group run (empty: every host)", &s.roleH, false, false, 32, nil)
		})
		ui.Row(c).Gap(space2).AlignItems(ui.Center).Shrink(0).Children(func() {
			if checkBox(c, "Step "+n+" on one host only", boolCheck(s.oneHost), false) {
				w.act(func() { s.oneHost = !s.oneHost })
			}
			body(c, "One host only", ink)
		})
	})
	for len(s.capH) < len(s.captures) {
		s.capH = append(s.capH, [2]ui.Handle{})
	}
	for k := range s.captures {
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Key(k).Label("capture " + strconv.Itoa(k+1)).Children(func() {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				inputBox(c, "Step "+n+" capture "+strconv.Itoa(k+1)+" name", &s.captures[k].Name, "join_token", &s.capH[k][0], true, false, 32, nil)
			})
			ui.Row(c).Grow(2).Basis(0).MinWidth(0).Children(func() {
				inputBox(c, "Step "+n+" capture "+strconv.Itoa(k+1)+" pattern", &s.captures[k].Pattern, `--token\s+(\S+)`, &s.capH[k][1], true, false, maxPattern, nil)
			})
			if iconBtn(c, "cancel-01", "Remove capture "+orElse(s.captures[k].Name, strconv.Itoa(k+1)), sizeControlSm, false) {
				w.act(func() { s.captures = append(s.captures[:k:k], s.captures[k+1:]...); s.capH = nil })
			}
		})
	}
	if len(s.captures) > 0 {
		help(c, "In a group run Keel takes the pattern's group from this step's output on each host and fills it in where a later step says {{name}}. Never sent to the agent.")
	}
	if len(s.captures) < maxCaptures && textBtn(c, "Capture a value", btnGhost) {
		w.act(func() { s.captures = append(s.captures, keelclient.StepCapture{}) })
	}
}

// runbookBox is the box beside the steps: Agent may pick, the hosts, the meta lines and the rule for steps.
func (w *workspace) runbookBox(c *ui.Context, e *rbEditor, rb keelclient.Runbook) {
	ui.Column(c).Grow(1).Basis(sizeRail).MinWidth(0).Padding(space3).Gap(space3).Radius(radiusMd).Border(1, line).Label("Runbook settings").Children(func() {
		ui.Row(c).FillWidth().Gap(space3).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
				label(c, "Agent may pick this runbook", ink)
				help(c, "Off: only you can start it.")
			})
			ui.Switch(c, &e.agentMayPick).Label("Agent may pick this runbook switch").Shrink(0)
		})
		ui.Column(c).FillWidth().Gap(space1).Children(func() {
			label(c, "Hosts", inkMuted)
			ui.Row(c).FillWidth().Gap(space1).Wrap().Children(func() {
				for i, h := range e.hosts {
					ui.Row(c).Height(sizeControlSm).Padding(0, space0_5, 0, space1_5).Gap(space0_5).Radius(radiusXs).Background(hoverGround).AlignItems(ui.Center).Key(h).Children(func() {
						mono(c, h, inkMuted)
						if iconBtn(c, "cancel-01", "Remove "+h, sizeControlSm-space1, false) {
							w.act(func() { e.hosts = append(e.hosts[:i:i], e.hosts[i+1:]...); e.focusHost = true })
						}
					})
				}
			})
			inputBox(c, "Folder or tag", &e.hostInput, "Folder or tag", &e.hostH, false, false, 0, func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == 0:
					w.act(func() { e.addHostChip() })
					return true
				case k.Key == ui.KeyBackspace && k.Mods == 0 && e.hostInput == "" && len(e.hosts) > 0:
					w.act(func() { e.hosts = e.hosts[:len(e.hosts)-1] })
					return true
				}
				return false
			})
			help(c, "Offered only when a pane in scope is on one of these (its folder or a tag). Empty: any host.")
		})
		now := time.Now()
		src := "Draft, not saved"
		switch {
		case e.id != "":
			src = runbookSourceLine(rb)
		case e.source == "user":
			src = "New, not saved"
		}
		for _, row := range [][2]string{{"Source", src}, {"Last used", runbookUsedLabel(rb, now)}, {"Steps", runbookStepsLine(rb)}} {
			ui.Column(c).FillWidth().Gap(space0_5).Children(func() {
				caption(c, row[0], inkFaint)
				ui.Text(c, row[1]).TextColor(ink).FontSize(captionSize)
			})
		}
		help(c, "Steps become proposals. Each one waits for you to press Type, with the usual rating and ⇧ rule on production.")
	})
}

// addHostChip ends the host being typed as a chip: "," ends it too in the app, ↵ here.
func (e *rbEditor) addHostChip() {
	e.hosts, e.hostInput, e.focusHost = addHost(e.hosts, e.hostInput), "", true
}

// ---- the card of a runbook in use ----

// stepMarkColor is a progress row's mark.
func stepMarkColor(state string) ui.Color {
	switch state {
	case "done":
		return tide
	case "failed":
		return flare
	case "now":
		return brassInk
	}
	return inkFaint
}

// runCard is the runbook in use, under "Agent" of the turn that started it (agentthread.js paintRun): the runbook, why it was
// picked, every step's progress and what the user can do now. Every step is still a proposal card of its own, typed through
// Approve before type; the card only asks (Propose step N), marks (done, skip, retry), moves or stops.
func (w *workspace) runCard(c *ui.Context, runID string) {
	a := w.agent
	u := &a.runs
	var run *keelclient.ChatRunbook
	if u.run != nil && u.run.ID == runID {
		run = u.run
	}
	name := "a runbook"
	switch {
	case run != nil:
		name = run.Name
	case u.names[runID] != "":
		name = u.names[runID]
	}
	card := ui.Column(c).FillWidth().Key("run "+runID).Padding(space3).Gap(space2).Radius(radiusMd).Background(controlFill(raised)).Border(1, line).Label("Runbook " + name)
	card.Children(func() {
		nameRow := func(badge string) {
			ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
				icon(c, "book", inkMuted)
				bodyStrong(c, name, ink).Shrink(1).MinWidth(0)
				ui.Spacer(c)
				kindPill(c, badge, "")
			})
		}
		switch {
		case run == nil || (run.Status == "stopped" && !u.none):
			nameRow("Stopped")
			return
		case u.none:
			ui.Text(c, "No runbook for this conversation. The agent answers from the panes in scope and your notes only.").TextColor(inkMuted).FontSize(captionSize)
			if textBtnOff(c, "Use "+name, btnGhost, u.busy) {
				id, nm := run.RunbookID, name
				w.act(func() { w.useRunbook(id, nm) })
			}
			return
		}
		nameRow("Runbook")
		if run.PinHost != "" { // where the steps go: the host of the first step, pinned, with its tab
			target := "Run on " + run.PinHost
			if pn := w.agentPaneBySession(run.PinSessionID); pn != nil && pn.sub != "" {
				target += " · " + pn.sub
			}
			ui.Row(c).FillWidth().Gap(space1_5).AlignItems(ui.Center).Label("run target").Children(func() {
				icon(c, "agent-cmd", inkMuted)
				text(c, target, ink, captionSize, weightMedium)
			})
		}
		ui.Text(c, runWhyLine(run)).TextColor(inkMuted).FontSize(captionSize)
		ui.Column(c).FillWidth().Gap(space1).Label("Steps").Children(func() {
			now := time.Now()
			for i, st := range run.Steps {
				r := stepRow(run, i, now)
				ui.Row(c).FillWidth().Key(st.ID).Gap(space2).AlignItems(ui.Start).Label("run step " + strconv.Itoa(i+1) + " " + r.state).Children(func() {
					text(c, r.mark, stepMarkColor(r.state), uiFontSize, weightSemibold).Shrink(0).Width(space4)
					text(c, st.Title, ink, uiFontSize, weightRegular).Shrink(1).MinWidth(0)
					if r.note != "" {
						ui.Spacer(c)
						text(c, r.note, inkFaint, captionSize, weightRegular).Shrink(0)
					}
				})
			}
		})
		if run.Blocked != "" {
			cardNote(c, "alert", signal, run.Blocked)
			if run.PinHost != "" && textBtnOff(c, "Move the run to another host", btnGhost, u.busy) {
				w.act(w.moveRun)
			}
		}
		w.runActions(c, run, nextAction(run, func(pid string) bool { r := a.props[pid]; return r != nil && !r.p.Expired }))
		if u.err != "" {
			cardNote(c, "alert", flare, u.err)
		}
		ui.Row(c).FillWidth().Gap(space1_5).Children(func() {
			if textBtn(c, "Use a different runbook", btnGhost) {
				w.act(w.toggleChoose)
			}
			if textBtnOff(c, "Don't use", btnGhost, u.busy) {
				w.act(w.stopRun)
			}
		})
		if u.choosing {
			w.runOthers(c, u)
		}
		cardNote(c, "shield-01", inkMuted, "Runbook steps are proposals. Each one waits for you. The agent cannot skip ahead or type for you.")
	})
}

// runOthers is the list "Use a different runbook" opens: a runbook that does not fit the panes in scope says why.
func (w *workspace) runOthers(c *ui.Context, u *runUse) {
	ui.Column(c).FillWidth().Gap(space0_5).Label("Other runbooks").Children(func() {
		switch {
		case !u.listed:
			help(c, "Loading…")
		case len(u.others) == 0:
			help(c, "No other runbooks. Add one on the Runbooks page.")
		}
		for _, o := range u.others {
			off := !o.ok || u.busy
			r := ui.Column(c).FillWidth().Key(o.id).Padding(space1_5, space2).Radius(radiusSm).Border(1, line).Label("Use " + o.name)
			if off {
				r.Opacity(0.6).Cursor(ui.CursorNotAllowed)
			} else {
				r.Cursor(ui.CursorPointer)
				if r.Hovered() {
					r.Background(hoverGround)
				}
			}
			r.Children(func() {
				body(c, o.name, ink)
				sub := o.when
				if !o.ok {
					sub = o.reason
				}
				if sub != "" {
					caption(c, sub, inkFaint)
				}
			})
			if r.Clicked() && !off {
				id, nm := o.id, o.name
				w.act(func() { w.useRunbook(id, nm) })
			}
		}
	})
}

// runActions is what the run card offers for the current step (agentthread.js runActionsHTML).
func (w *workspace) runActions(c *ui.Context, run *keelclient.ChatRunbook, act runAction) {
	busy := w.agent.runs.busy
	btn := func(name string, primary bool, do func()) {
		kind := btnGhost
		if primary {
			kind = btnPrimary
		}
		if textBtnOff(c, name, kind, busy) {
			w.act(do)
		}
	}
	step := func(action string) func() { return func() { w.runStepAction(act.step.id, action) } }
	row := func(msg string, flareText bool, buttons func()) {
		col := inkMuted
		if flareText {
			col = flare
		}
		ui.Text(c, msg).TextColor(col).FontSize(captionSize)
		ui.Row(c).FillWidth().Gap(space1_5).Wrap().Children(buttons)
	}
	n := strconv.Itoa(act.step.n)
	switch act.kind {
	case "propose":
		row("Next: step "+n+" · "+act.step.title, false, func() {
			btn("Propose step "+n, true, w.proposeRunStep)
			btn("Skip", false, step("skip"))
		})
	case "proposed":
		row("Step "+n+" is proposed below. Nothing is typed until you press Type.", false, func() {
			btn("Mark done", false, step("done"))
			btn("Skip", false, step("skip"))
		})
	case "confirm":
		row("Step "+n+" · exit 0, reported by the shell. "+orElse(run.PinHost, "This host")+" is a production host: confirm the step before going on.", false, func() {
			btn("Confirm done", true, step("done"))
			btn("Retry", false, step("retry"))
			btn("Skip", false, step("skip"))
		})
	case "waiting":
		row("Step "+n+" was typed. Its exit status shows here when the pane's shell reports it; otherwise mark it yourself.", false, func() {
			btn("Mark done", true, step("done"))
			btn("Retry", false, step("retry"))
			btn("Skip", false, step("skip"))
		})
	case "noexit":
		row("Step "+n+" ended with no exit status reported. Check the pane, then mark it.", false, func() {
			btn("Mark done", true, step("done"))
			btn("Retry", false, step("retry"))
			btn("Skip", false, step("skip"))
		})
	case "failed":
		exit := ""
		if act.exit != nil {
			exit = " · exit " + strconv.Itoa(*act.exit) + ", reported by the shell"
		}
		row("Step "+n+" failed"+exit+". The runbook waits for you.", true, func() {
			btn("Retry step "+n, true, step("retry"))
			btn("Mark done", false, step("done"))
			btn("Skip", false, step("skip"))
		})
	case "check":
		row("Step "+n+" has no command. Check it yourself, then mark it done.", false, func() {
			btn("Mark done", true, step("done"))
			btn("Skip", false, step("skip"))
		})
	case "finished":
		ui.Text(c, "All steps are done or skipped.").TextColor(inkMuted).FontSize(captionSize)
	}
}

// ---- the card of a runbook the agent proposes ----

// agentBookCard is "Save as a runbook?" (agentthread.js paintBookProp): nothing is saved until Save is pressed, and what is
// saved starts with Agent may pick off.
func (w *workspace) agentBookCard(c *ui.Context, id string) {
	rec := w.agent.books[id]
	if rec == nil {
		return
	}
	b := rec.b
	warn := runbookProposalWarn(b)
	border := line
	if warn && b.Status == "open" && !b.Expired {
		border = signal
	}
	card := ui.Column(c).FillWidth().Key("book "+id).Radius(radiusMd).Background(controlFill(raised)).Border(1, border).Clip().Label("Proposed runbook")
	card.Children(func() {
		if b.Status != "open" || b.Expired {
			msg := runbookProposalDone(b)
			if b.Status == "open" {
				msg = "Not saved. This proposal expired; add the runbook yourself on the Runbooks page."
			}
			w.doneRow(c, b.Status == "saved", msg, func() {
				if b.Status == "saved" && textBtn(c, "Open Runbooks", btnGhost) {
					w.act(w.openRunbooksPage)
				}
				if !b.Expired && textBtnOff(c, "Undo", btnGhost, rec.busy) {
					w.act(func() { w.undoBookProposal(id) })
				}
			})
			return
		}
		ui.Row(c).FillWidth().Padding(space3, space3, 0).Gap(space2).Wrap().Children(func() {
			icon(c, "book", inkMuted)
			label(c, "Save as a runbook?", ink)
			ui.Spacer(c)
			switch {
			case b.FromPane:
				pill(c, "From pane output", flareSoft, flare)
			case warn:
				pill(c, "After reading panes", flareSoft, flare)
			}
			pill(c, countWord(len(b.Steps), "step", "steps"), hoverGround, inkMuted)
		})
		ui.Column(c).FillWidth().Padding(space2, space3, 0).Gap(space2).Children(func() {
			bodyStrong(c, b.Name, ink)
			if b.When != "" {
				text(c, "Use when: "+b.When, inkFaint, captionSize, weightRegular)
			}
			for i, st := range b.Steps {
				ui.Row(c).FillWidth().Key(i).Gap(space2).AlignItems(ui.Start).Children(func() {
					text(c, strconv.Itoa(i+1), inkMuted, uiFontSize, weightSemibold).Shrink(0).Width(space4)
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1).Children(func() {
						text(c, st.Title, ink, uiFontSize, weightRegular)
						if st.Command != "" {
							ui.Scroll(c).FillWidth().MaxHeight(sizeRow*3).Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, line).Label("Command").Children(func() {
								ui.Text(c, st.Command).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0)
							})
						}
					})
				})
			}
			if warn {
				warnBlock(c, runbookProposalWarning(b))
			}
			if rec.err != "" {
				cardNote(c, "alert", flare, rec.err)
			}
			cardNote(c, "shield-01", inkMuted, "Nothing is saved until you press Save. A saved runbook starts with Agent may pick off; review it on the Runbooks page.")
		})
		hairline(ui.Row(c).FillWidth().Margin(space3, 0, 0).Padding(space2, space3).Gap(space1_5).Background(w.ground(surface)), 1, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			if textBtnOff(c, "Dismiss", btnGhost, rec.busy) {
				w.act(func() { w.dismissBookProposal(id) })
			}
			name, kind := "Save", btnPrimary
			if warn {
				name, kind = "Save anyway", btnDefault
			}
			if textBtnOff(c, name, kind, rec.busy) {
				w.act(func() { w.saveBookProposal(id) })
			}
		})
	})
}
