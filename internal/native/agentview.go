package native

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The agent panel drawn (internal/ui/agent.templ, cmd/keel/web/js/ui/agentpanel.js and agentthread.js; the k-agent* and
// k-prop* rules of keel.css): a right-hand panel of size-agent, a header, the scope chip, the thread (your question, the
// trace, the answer, proposal cards), and the composer. Colours and sizes are tokens.go's.
//
// A card's buttons ask the workspace (agent.go); none of them types. Type into pane opens Approve before type.

// pill is a k-badge with a text.
func pill(c *ui.Context, s string, bg, fg ui.Color) ui.Element {
	return ui.Row(c).Height(space4).Padding(0, space1_5).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(bg).Children(func() {
		text(c, s, fg, kbdSize, weightSemibold)
	})
}

// bytesView draws the exact bytes Type sends, the control characters as glyphs (k-ctl).
func bytesView(c *ui.Context, bytes string) {
	var spans []ui.Span
	for _, r := range bytesRuns(bytes) {
		sp := ui.Span{Text: r.text}
		if r.ctl {
			sp.Color, sp.Background, sp.Weight, sp.Radius, sp.PaddingX = brassInk, brassSoft, weightMedium, radiusXs, 3
		}
		spans = append(spans, sp)
	}
	// Named by the bytes as drawn, glyphs and all: what assistive technology and a test read is what is on screen.
	ui.RichText(c, spans...).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0).Label(bytesText(bytes))
}

// ---- the panel ----

// agentShown is whether the panel is on screen: open, and not in a joined tab (§5.7: no panel there).
func (w *workspace) agentShown() bool { return w.agent.open && !w.joinedActive() }

func (w *workspace) agentView(c *ui.Context) {
	a := w.agent
	if atTrigger(a.input) { // "@" at the start of a word opens the pane picker and is not typed (agentpanel.js)
		a.input, a.picker = strings.TrimSuffix(a.input, "@"), true
	}
	if a.settings == nil && !a.loading && !a.loadTried { // open at start: read the settings once the window is shown
		a.loadTried = true
		w.act(w.loadAgent)
	}
	w.agentSync()
	if w.copyOut != "" { // a Copy from a card's menu, which has no context to write with
		c.WriteClipboard(w.copyOut)
		w.copyOut = ""
		w.say("Copied")
	}
	winW, _ := c.Size()
	hairline(ui.Column(c).Width(clampAgentWidth(a.width, winW)).Shrink(0).FillHeight().Background(w.ground(surface)).Label("agent panel"), 0, 0, 0, 1).Children(func() {
		w.agentSash(c, winW)
		w.agentHead(c)
		w.agentSub(c)
		if a.stick {
			a.scroll.Y, a.stick = math.MaxFloat32, false
		}
		ui.Scroll(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).TrackScroll(&a.scroll).Label("agent thread").Children(func() {
			ui.Column(c).FillWidth().Padding(space4, space3).Gap(space4).Children(func() { w.agentThread(c, false) })
		})
		ui.Row(c).FillWidth().Shrink(0).Padding(space2, space3).Gap(space2).AlignItems(ui.Start).Children(func() {
			icon(c, "shield-01", inkFaint)
			ui.Text(c, agentFootNote).TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
		})
		w.agentComposer(c)
	})
}

func (w *workspace) agentHead(c *ui.Context) {
	a := w.agent
	hairline(ui.Row(c).FillWidth().Height(sizeControlLg+space2).Shrink(0).Padding(0, space1_5, 0, space3).Gap(space2), 0, 0, 1, 0).Children(func() {
		icon(c, "ai-magic", brassInk)
		// The title is the button that opens Recent conversations (agent.templ #agent-sessions-btn).
		t := ui.Row(c).Height(sizeControlSm).Padding(0, space1_5).Gap(space1).Radius(radiusSm).Shrink(1).MinWidth(0).
			Label("Recent conversations").Cursor(ui.CursorPointer).Focusable().Selected(a.sessOpen)
		if t.Hovered() || a.sessOpen {
			t.Background(hoverGround)
		}
		t.Children(func() {
			bodyStrong(c, orElse(a.title, titleNewConv), ink).Shrink(1).MinWidth(0).MaxLines(1)
			iconSized(c, "chevron-down", inkMuted, iconSizeSm)
		})
		if t.Clicked() {
			w.act(func() {
				if a.sessOpen {
					w.closeSessions()
				} else {
					w.openSessions()
				}
			})
		}
		if a.sessOpen {
			w.agentSessions(c, t)
		}
		ui.Spacer(c)
		if iconBtn(c, "add-01", "New conversation", sizeControlSm, false) {
			w.act(w.newAgentConversation)
		}
		if iconBtn(c, "cancel-01", "Close agent", sizeControlSm, false) {
			w.act(func() { w.setAgentOpen(false) })
		}
	})
}

// agentSub is the scope chip, which opens the pane picker, and where conversations are kept.
func (w *workspace) agentSub(c *ui.Context) {
	a := w.agent
	m := len(candidatePanes(w.agentPanes()))
	hairline(ui.Row(c).FillWidth().Height(sizeControlLg+space1).Shrink(0).Padding(0, space3).Gap(space2), 0, 0, 1, 0).Children(func() {
		chip := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1_5).Radius(radiusFull).Border(1, line).Shrink(1).MinWidth(0).
			Label("Choose what the agent can read").Cursor(ui.CursorPointer)
		col := inkMuted
		if a.picker || chip.Hovered() {
			chip.Border(1, lineStrong)
			col = ink
		}
		chip.Children(func() {
			iconSized(c, "eye", col, iconSizeSm)
			text(c, scopeLabel(len(a.scope), m), col, kbdSize+1, weightMedium)
			iconSized(c, "chevron-down", col, iconSizeSm)
		})
		if chip.Clicked() {
			w.act(func() { a.picker = !a.picker })
		}
		if a.picker {
			w.agentPicker(c, chip)
		}
		ui.Spacer(c)
		text(c, a.savedNote(), inkFaint, captionSize, weightRegular)
	})
}

// pickLabel names a picker row: the host and its tab, with the session when two rows would read the same.
func pickLabel(p agentPane, all []agentPane) string {
	l := p.label
	if p.sub != "" {
		l += " · " + p.sub
	}
	for _, o := range all {
		if o.sessionID != p.sessionID && o.label == p.label && o.sub == p.sub {
			return l + " · " + p.sessionID
		}
	}
	return l
}

// agentPicker is the pane picker, a popover under the scope chip: every pane the panel counts, a host with reading off
// marked "Agent can't read" and not pickable, and the presets (agentpanel.js paintPicker).
func (w *workspace) agentPicker(c *ui.Context, anchor ui.Element) {
	a := w.agent
	open := true
	all := candidatePanes(w.agentPanes())
	ui.PopoverBase(c, anchor, &open, func(panel ui.Element) {
		panel.Width(sizeAgent-space6).Background(raised).Border(1, line).Radius(radiusMd).Shadow(0, 8, 24, 0, shadowPop).Padding(space1).Label("dialog Panes the agent reads")
		if len(all) == 0 {
			ui.Row(c).Padding(space2).Children(func() { help(c, "Open a terminal to let the agent read it.") })
			return
		}
		ui.Row(c).Padding(space1_5, space2, space1).Children(func() { menuLabel(c, "Panes the agent reads") })
		for _, p := range all {
			on := contains(a.scope, p.sessionID)
			r := ui.Row(c).FillWidth().Padding(space1_5, space2).Gap(space2).Radius(radiusSm).Key(p.sessionID).Label("pick " + pickLabel(p, all)).Focusable()
			if p.readable {
				r.Cursor(ui.CursorPointer)
				if r.Hovered() {
					r.Background(hoverGround)
				}
			} else {
				r.Opacity(0.6).Cursor(ui.CursorNotAllowed)
			}
			r.Children(func() {
				box := ui.Row(c).Size(space4, space4).Radius(radiusXs).Justify(ui.Center).Shrink(0).Border(1, lineStrong).Background(stage)
				if on {
					box.Background(brass)
				}
				box.Children(func() {
					if on {
						iconSized(c, "tick", onBrass, iconSizeSm)
					}
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
					body(c, p.label, ink)
					sub := p.sub
					if p.focused {
						sub = strings.TrimPrefix(sub+" · focused", " · ")
					}
					if sub != "" {
						caption(c, sub, inkFaint)
					}
				})
				switch {
				case !p.readable:
					pill(c, "Agent can't read", hoverGround, inkMuted)
				case p.production:
					pill(c, "Production", signalSoft, signal)
				}
			})
			if r.Clicked() && p.readable {
				w.act(func() { w.setAgentScope(toggleScope(a.scope, &p)) })
			}
		}
		hairline(ui.Row(c).FillWidth().Gap(space1).Padding(space1, space1, 0).Margin(space1, 0, 0), 1, 0, 0, 0).Children(func() {
			for _, pr := range [][2]string{{"focused", "Focused pane"}, {"tab", "Every pane in tab"}, {"none", "None"}} {
				if textBtn(c, pr[1], btnGhost) {
					w.act(func() {
						if pr[0] == "none" {
							w.setAgentScope(nil)
						} else {
							w.setAgentScope(defaultScope(pr[0], w.agentPanes()))
						}
						a.picker = false
					})
				}
			}
		})
	})
	if !open {
		w.act(func() { a.picker = false })
	}
}

// ---- the thread ----

func (w *workspace) agentThread(c *ui.Context, onPage bool) {
	a := w.agent
	if len(a.turns) == 0 {
		w.agentEmpty(c, onPage)
		return
	}
	resumed := a.resumedAfter()
	for i, t := range a.turns {
		ui.Column(c).FillWidth().Gap(space4).Key(i).Children(func() {
			if !t.again {
				w.agentTurn(c, t)
			}
			for _, id := range t.props {
				w.agentCard(c, id, onPage)
			}
			for _, id := range t.notes {
				w.agentNoteCard(c, id, onPage)
			}
			for _, id := range t.books {
				w.agentBookCard(c, id)
			}
			for _, id := range t.nbs {
				w.agentNbCard(c, id)
			}
			if i == resumed {
				w.agentResumed(c)
			}
		})
	}
}

// agentResumed is the "Resumed · …" divider and the note under the saved turns of a resumed conversation.
func (w *workspace) agentResumed(c *ui.Context) {
	a := w.agent
	hairlineRow := func() { ui.Box(c).Grow(1).Height(1).Background(line) }
	ui.Row(c).FillWidth().Gap(space3).AlignItems(ui.Center).Label("agent resumed").Children(func() {
		hairlineRow()
		text(c, "Resumed · "+agoLabel(a.resumedAt, time.Now()), inkFaint, captionSize, weightMedium)
		hairlineRow()
	})
	ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Start).Label("agent resumed note").Children(func() {
		icon(c, "shield-01", inkFaint)
		ui.Text(c, "Earlier reads stay in this conversation as text. New reads use only the panes in scope now, on hosts that still allow the agent.").
			TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
	})
}

func (w *workspace) agentEmpty(c *ui.Context, onPage bool) {
	a := w.agent
	if a.keyConfigured != nil && !*a.keyConfigured {
		if !onPage {
			w.agentConnect(c)
			return
		}
		name := "a model"
		if a.provider != nil {
			name = orElse(a.provider.Name, name)
		}
		ui.Column(c).FillWidth().Padding(space6, space2).Gap(space2).AlignItems(ui.Center).Children(func() {
			icon(c, "ai-magic", inkFaint)
			text(c, "Connect "+name+" first", ink, titleSize, weightSemibold)
			ui.Text(c, "Add a key in the agent panel or in Settings · Agent. Nothing is sent until then.").TextColor(inkMuted).FontSize(captionSize).TextAlign(ui.Center)
			if textBtn(c, "Open the agent panel", btnDefault) {
				w.act(func() { w.setAgentOpen(true) })
			}
		})
		return
	}
	n := 200
	if a.settings != nil && a.settings.Settings.ScrollbackLines > 0 {
		n = a.settings.Settings.ScrollbackLines
	}
	msg := "When you send, the agent reads the last " + strconv.Itoa(n) + " lines of each pane in scope, redacts secrets, and answers. Every read is written to the log."
	if onPage {
		msg = "Pick a conversation on the left, or ask a new question. The agent reads the last " + strconv.Itoa(n) + " lines of each pane in scope, redacts secrets, and answers. Every read is written to the log."
	}
	ui.Column(c).FillWidth().Padding(space6, space2).Gap(space2).AlignItems(ui.Center).Children(func() {
		icon(c, "ai-magic", inkFaint)
		text(c, agentEmptyTitle, ink, titleSize, weightSemibold)
		ui.Text(c, msg).TextColor(inkMuted).FontSize(captionSize).TextAlign(ui.Center)
	})
}

// agentConnect is "Connect Gemini": the provider's warning and the masked key field (PUT /api/agent/keys/{provider}).
func (w *workspace) agentConnect(c *ui.Context) {
	a := w.agent
	d := keelclient.AgentProvider{Name: "Google Gemini", ShortName: "Gemini", KeyLabel: "Gemini API key"}
	if a.provider != nil {
		d = *a.provider
	}
	short := orElse(d.ShortName, orElse(d.Name, "the model"))
	ui.Column(c).FillWidth().Padding(space3).Gap(space3).Radius(radiusMd).Background(raised).Border(1, line).Label("agent connect").Children(func() {
		text(c, "Connect "+short, ink, titleSize, weightSemibold)
		help(c, "When you ask, the agent sends the panes in scope, redacted, and your question to "+orElse(d.Name, "Google Gemini")+". The key is kept in the vault.")
		if d.Warning != "" {
			ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(signalSoft).AlignItems(ui.Start).Label("agent warning").Children(func() {
				icon(c, "alert", signal)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1).Children(func() {
					ui.Text(c, d.Warning).TextColor(ink).FontSize(captionSize)
					if strings.HasPrefix(d.TermsURL, "https://") {
						ui.Link(c, short+" API terms", d.TermsURL).TextColor(brassInk).FontSize(captionSize)
					}
				})
			})
		}
		labeled(c, orElse(d.KeyLabel, "API key"), &a.keyH, func() {
			ui.Row(c).FillWidth().Gap(space2).Children(func() {
				// Masked: the key is never drawn, and the field is emptied the moment it is sent (saveAgentKey).
				ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					inputBox(c, orElse(d.KeyLabel, "API key")+" input", &a.keyField, "", &a.keyH, true, true, 0, func(k ui.TextKey) bool {
						if k.Key == ui.KeyEnter && k.Mods == 0 {
							w.act(w.saveAgentKey)
							return true
						}
						return false
					})
				})
				if textBtn(c, "Connect", btnPrimary) {
					w.act(w.saveAgentKey)
				}
			})
		})
		if a.keyErr != "" {
			ui.Row(c).FillWidth().Label("agent key error").Children(func() { ui.Text(c, a.keyErr).TextColor(flare).FontSize(captionSize) })
		}
		if strings.HasPrefix(d.KeyHelpURL, "https://") {
			ui.Link(c, "Get a key", d.KeyHelpURL).TextColor(brassInk).FontSize(captionSize)
		}
	})
	if a.focusKey {
		a.keyH.Focus(c)
		a.focusKey = false
	}
}

func (w *workspace) agentWho(c *ui.Context, who string, ai bool) {
	ui.Row(c).Gap(space1_5).Children(func() {
		if ai {
			iconSized(c, "ai-magic", brassInk, iconSizeSm)
		}
		label(c, who, inkMuted)
	})
}

func traceRow(c *ui.Context, glyph string, col ui.Color, s string) {
	ui.Row(c).FillWidth().Gap(space2).Children(func() {
		icon(c, glyph, col)
		text(c, s, inkMuted, captionSize, weightRegular).Font(monoStack).Shrink(1).MinWidth(0).MaxLines(0) // a long line wraps, as the app's does
	})
}

func (w *workspace) agentTurn(c *ui.Context, t *agentTurn) {
	ui.Column(c).FillWidth().Gap(space1_5).Children(func() {
		who := "You"
		if t.saved && !t.at.IsZero() { // a saved turn says when it was asked (savedTurnHTML)
			who += " · " + stampLabel(t.at, time.Now())
		}
		w.agentWho(c, who, false)
		ui.Text(c, t.question).TextColor(ink).FontSize(uiFontSize)
	})
	ui.Column(c).FillWidth().Gap(space1_5).Children(func() {
		w.agentWho(c, "Agent", true)
		if t.runID != "" {
			w.runCard(c, t.runID)
		}
		ui.Column(c).FillWidth().Gap(space0_5).Padding(space1, 0).Children(func() {
			switch {
			case t.pending:
				msg := "Asking without reading a pane"
				if t.scoped > 0 {
					noun := "panes"
					if t.scoped == 1 {
						noun = "pane"
					}
					msg = "Reading " + strconv.Itoa(t.scoped) + " " + noun + " and asking"
				}
				traceRow(c, "agent-busy", brassInk, msg)
			case t.stopped:
				traceRow(c, "agent-skip", inkFaint, "Stopped")
			case t.done:
				for _, s := range t.trace {
					if s.Status == "read" {
						traceRow(c, "tick", tide, traceText(s))
					} else if s.Status == "notebook" || s.Status == "notebook-index" {
						traceRow(c, "notebook", inkFaint, traceText(s)) // agentthread.js stepHTML: the notebook glyph
					} else if s.Status == "notes" || s.Status == "runbook" {
						traceRow(c, "agent-info", inkFaint, traceText(s))
					} else {
						traceRow(c, "agent-skip", inkFaint, traceText(s))
					}
				}
				if len(t.trace) == 0 && !t.saved { // a saved turn with no trace shows none (savedTurnHTML)
					traceRow(c, "agent-skip", inkFaint, "Read no pane")
				}
				if t.testProvider {
					traceRow(c, "agent-info", inkFaint, "Test provider · nothing left this machine")
				}
			}
		})
		if t.err != nil {
			w.agentError(c, t.err)
		}
		if t.done {
			if t.answer != "" || !t.saved {
				w.agentAnswer(c, t.answer)
			}
			if n := cutNotice(t.cut); n != "" {
				ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(signalSoft).AlignItems(ui.Start).Label("agent cut").Children(func() {
					icon(c, "alert", signal)
					ui.Text(c, n).TextColor(ink).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
				})
			}
		}
	})
}

// agentError is the app's error block (agentthread.js errorHTML).
func (w *workspace) agentError(c *ui.Context, err error) {
	title, detail := "The agent could not answer.", err.Error()
	var se *keelclient.StatusError
	if errors.As(err, &se) {
		switch {
		case se.Code == 412:
			title, detail = agentNoModelTitle, agentNoModelDetail
		case se.Code == 423:
			title, detail = "The vault is locked.", "Unlock it, then ask again."
		case se.Msg != "":
			detail = se.Msg
		}
	}
	ui.Column(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(flareSoft).Label("agent error").Children(func() {
		text(c, title, flare, captionSize, weightMedium)
		ui.Text(c, detail).TextColor(ink).FontSize(captionSize)
	})
}

// agentAnswer draws the answer as paragraphs and code blocks. A code block has Copy and never Type: only a proposal card
// types, through Approve before type (§6.1).
func (w *workspace) agentAnswer(c *ui.Context, answer string) {
	ui.Column(c).FillWidth().Gap(space2).Label("agent answer").Children(func() {
		for i, b := range mdBlocks(answer) {
			if !b.code {
				var spans []ui.Span
				for _, s := range mdInline(b.text) {
					sp := ui.Span{Text: s.text}
					switch {
					case s.code:
						sp.Font, sp.Size, sp.Background, sp.Radius, sp.PaddingX = monoStack, captionSize, hoverGround, radiusXs, space1
					case s.bold:
						sp.Weight = weightSemibold
					}
					spans = append(spans, sp)
				}
				ui.RichText(c, spans...).TextColor(ink).FontSize(uiFontSize).MaxLines(0)
				continue
			}
			ui.Column(c).FillWidth().Key(i).Padding(space2, space3).Gap(space1_5).Radius(radiusSm).Background(stage).Border(1, line).Children(func() {
				ui.Text(c, b.text).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0)
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					if iconBtn(c, "agent-copy", "Copy code", sizeControlSm, false) {
						w.act(func() { c.WriteClipboard(b.text); w.say("Copied") })
					}
				})
			})
		}
	})
}

// ---- a proposal card ----

// agentCard draws a proposal. onPage is the Agent page's thread, which is away from the panes: Type names the pane it types
// into (agentpage.js typeLabel).
func (w *workspace) agentCard(c *ui.Context, id string, onPage bool) {
	a := w.agent
	rec := a.props[id]
	if rec == nil {
		return
	}
	p := rec.p
	if p.Expired {
		w.agentExpiredCard(c, id)
		return
	}
	editH := &rec.editH
	if onPage {
		editH = &rec.editHP
	}
	pane := w.agentPaneBySession(p.SessionID)
	offer := typeOffer(p, pane)
	kind, kindCol := riskBadge(p.Risk)
	bg, fg := signalSoft, signal
	switch kindCol {
	case "tide":
		bg, fg = tideSoft, tide
	case "flare":
		bg, fg = flareSoft, flare
	}
	border := line
	if p.Risk == "destructive" {
		border = flare
	}
	card := ui.Column(c).FillWidth().Key(id).Radius(radiusMd).Background(controlFill(raised)).Border(1, border).Clip().Label("Proposal")
	card.PopoverMenu(func(m *ui.Menu) { w.agentCardMenu(m, id, offer) })
	card.Children(func() {
		ui.Row(c).FillWidth().Padding(space3, space3, 0).Gap(space2).Wrap().Children(func() {
			icon(c, "agent-cmd", inkMuted)
			label(c, runOnLabel(p), ink)
			mono(c, orElse(p.Host, paneLabel(pane, "no pane")), inkMuted).Shrink(1).MinWidth(0)
			if p.Production {
				pill(c, "Production", signalSoft, signal)
			}
			ui.Spacer(c)
			pill(c, kind, bg, fg).Label("proposal kind")
		})
		ui.Column(c).FillWidth().Padding(space2, space3, 0).Gap(space2).Children(func() {
			if rec.editing {
				ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
					ui.TextAreaBase(c, &rec.edit).Label("Edit command").TextColor(ink).FontSize(captionSize).Font(monoStack).Lines(2, 8).FillWidth().Bind(editH)
				})
			} else {
				ui.Scroll(c).FillWidth().MaxHeight(sizeRow*5).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("agent bytes").Children(func() {
					bytesView(c, p.Bytes)
				})
			}
			if p.Why != "" {
				ui.Text(c, p.Why).TextColor(inkMuted).FontSize(captionSize)
			}
			if offer == offerShift && rec.status == nil {
				cardNote(c, "alert", flare, shiftReason(p)+". Hold "+shiftWord()+" when you press Type.")
			}
			switch offer {
			case offerFollowing:
				cardNote(c, "eye", haze, msgAgentFollowing)
			case offerClosed:
				cardNote(c, "agent-info", haze, "That pane is not open in this window. Copy the command instead.")
			case offerNone:
				cardNote(c, "agent-info", haze, "No pane named for this command. Copy it instead.")
			case offerPlaceholder:
				cardNote(c, "alert", signal, "It holds a redacted placeholder such as [secret:1], which a shell would read as a pattern. Keel will not type it: edit it to the real value or an environment variable first.")
			}
			if st := rec.status; st != nil {
				col, glyph := flare, "alert"
				if st.ok {
					col, glyph = tide, "tick"
				}
				cardNote(c, glyph, col, st.text).Label("proposal status")
			}
		})
		hairline(ui.Row(c).FillWidth().Margin(space3, 0, 0).Padding(space2, space3).Gap(space1_5).Background(w.ground(surface)), 1, 0, 0, 0).Children(func() {
			if rec.editing {
				ui.Spacer(c)
				if textBtn(c, "Cancel", btnGhost) {
					w.act(func() { rec.editing = false })
				}
				if textBtn(c, "Save command", btnPrimary) {
					w.act(func() { w.editAgentProposal(id) })
				}
				return
			}
			text(c, countLabel(p.Bytes), inkFaint, captionSize, weightRegular)
			ui.Spacer(c)
			if iconBtn(c, "agent-edit", "Edit command", sizeControlSm, false) {
				w.act(func() { w.startEdit(rec) })
			}
			if iconBtn(c, "agent-copy", "Copy command", sizeControlSm, false) {
				w.act(func() { c.WriteClipboard(rec.p.Command); w.say("Copied") }) // the command only, never the bytes
			}
			if offer == offerType || offer == offerShift {
				w.typeButton(c, id, offer, onPage)
			}
		})
	})
	if rec.editing && rec.focusEdit && onPage == w.agentPageShown() { // the field on screen takes the keyboard
		editH.Focus(c)
		rec.focusEdit = false
	}
}

// agentExpiredCard is a proposal saved from an earlier run (agentthread.js paintExpired). keel serve no longer holds it,
// so it cannot be typed and cannot be edited: the card has Copy and Propose again and nothing else. Propose again asks
// keel serve for a new proposal, which is a live card like any other.
func (w *workspace) agentExpiredCard(c *ui.Context, id string) {
	rec := w.agent.props[id]
	p := rec.p
	kind, kindCol := riskBadge(p.Risk)
	bg, fg := signalSoft, signal
	switch kindCol {
	case "tide":
		bg, fg = tideSoft, tide
	case "flare":
		bg, fg = flareSoft, flare
	}
	now := time.Now()
	meta := "not typed"
	if p.TypedAt != nil {
		meta = "typed " + stampLabel(*p.TypedAt, now)
	}
	if p.ProposedAt != nil {
		meta = "Proposed " + stampLabel(*p.ProposedAt, now) + " · " + meta
	}
	ui.Column(c).FillWidth().Key(id).Radius(radiusMd).Background(controlFill(raised)).Border(1, line).Clip().Opacity(0.92).Label("Proposal expired").Children(func() {
		ui.Row(c).FillWidth().Padding(space3, space3, 0).Gap(space2).Wrap().Children(func() {
			icon(c, "agent-cmd", inkMuted)
			label(c, runOnLabel(p), ink)
			mono(c, orElse(p.Host, "no pane"), inkMuted).Shrink(1).MinWidth(0)
			if p.Production {
				pill(c, "Production", signalSoft, signal)
			}
			ui.Spacer(c)
			pill(c, kind, bg, fg).Label("proposal kind")
			pill(c, "Expired", hoverGround, inkMuted).Label("proposal expired badge")
		})
		ui.Column(c).FillWidth().Padding(space2, space3, 0).Gap(space2).Children(func() {
			ui.Scroll(c).FillWidth().MaxHeight(sizeRow*5).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("agent bytes").Children(func() {
				bytesView(c, p.Bytes)
			})
			if p.Why != "" {
				ui.Text(c, p.Why).TextColor(inkMuted).FontSize(captionSize)
			}
			cardNote(c, "agent-history", inkFaint, "Panes may have changed since this was proposed.")
		})
		hairline(ui.Row(c).FillWidth().Margin(space3, 0, 0).Padding(space2, space3).Gap(space1_5).Background(w.ground(surface)), 1, 0, 0, 0).Children(func() {
			text(c, meta, inkFaint, captionSize, weightRegular)
			ui.Spacer(c)
			if textBtn(c, "Copy", btnGhost) {
				w.act(func() { w.copyOut = p.Command }) // the command only, never the bytes; the next frame writes it
			}
			if textBtn(c, "Propose again", btnDefault) {
				w.act(func() { w.proposeAgain(id) })
			}
		})
	})
}

func paneLabel(p *agentPane, fallback string) string {
	if p != nil {
		return p.label
	}
	return fallback
}

// cardNote is a k-prop-note: the icon carries the colour, the words are muted, except a status, which is its colour's.
func cardNote(c *ui.Context, glyph string, col ui.Color, s string) ui.Element {
	words := inkMuted
	if col == flare || col == tide {
		words = col
	}
	r := ui.Row(c).FillWidth().Gap(space1_5).AlignItems(ui.Start)
	r.Children(func() {
		icon(c, glyph, col)
		ui.Text(c, s).TextColor(words).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
	})
	return r
}

func (w *workspace) startEdit(rec *agentProp) {
	if rec.p.Expired {
		return
	}
	rec.editing, rec.edit, rec.focusEdit = true, rec.p.Command, true
}

// typeButton is Type into pane, with its key: ↵, or ⇧↵ when the proposal needs ⇧. It ends in "…" when a dialog will
// follow and not when the proposal looks typeable with one click (agentpanel.js typeLabel). A press asks the workspace,
// which types it directly or opens Approve before type; the card never types. ⇧ held on the click is the ⇧ of the ⇧ rule.
func (w *workspace) typeButton(c *ui.Context, id, offer string, onPage bool) {
	rec := w.agent.props[id]
	name := "Type into pane"
	if onPage { // the page is away from the panes, so the button names the one it types into
		name = "Type into " + orElse(rec.p.Host, "pane")
		if pn := w.agentPaneBySession(rec.p.SessionID); pn != nil {
			name = "Type into " + paneTarget(pn.label, pn.sub)
		}
	}
	word := typeWord(name, proposalDirect(rec.p))
	b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Justify(ui.Center).Shrink(0).
		Label(word).Cursor(ui.CursorPointer).Focusable()
	bg := brass
	if b.Hovered() {
		bg = brassStrong
	}
	b.Background(bg)
	// The app's key cap is ↵, or ⇧↵ for a proposal that needs ⇧. MyGo gives a button pressed from the keyboard no modifier, so
	// ⇧↵ cannot be offered: a ⇧ proposal says ⇧ (held with a click) instead, and ↵ is a plain Type, which keel serve and
	// the card refuse for it (docs/native.md).
	cap := "↵"
	if offer == offerShift {
		cap = shiftWord()
	}
	b.Children(func() {
		label(c, word, onBrass)
		text(c, cap, onBrass, kbdSize, weightMedium).Font(monoStack)
	})
	if b.Clicked() {
		shift := b.ClickModifiers()&ui.Shift != 0
		w.act(func() { w.typeAgentProposal(id, shift) })
	}
}

// agentCardMenu is the card menu (agentcardmenu.js): the card's own buttons, each doing what its button does.
func (w *workspace) agentCardMenu(m *ui.Menu, id, offer string) {
	rec := w.agent.props[id]
	if rec == nil {
		return
	}
	if rec.editing {
		if m.Item("Cancel").Chosen() {
			w.act(func() { rec.editing = false })
		}
		if m.Item("Save command").Chosen() {
			w.act(func() { w.editAgentProposal(id) })
		}
		return
	}
	if m.Item("Edit command").Chosen() {
		w.act(func() { w.startEdit(rec) })
	}
	if m.Item("Copy command").Chosen() {
		w.act(func() { w.copyOut = rec.p.Command }) // the command only, never the bytes; the next frame writes it
	}
	if (offer == offerType || offer == offerShift) && m.Item("Type into pane…").Chosen() {
		w.act(func() { w.typeAgentProposal(id, false) }) // from a menu ⇧ is not held: a ⇧ proposal says so on its card
	}
}

// ---- the composer ----

func (w *workspace) agentComposer(c *ui.Context) {
	a := w.agent
	hairline(ui.Column(c).FillWidth().Shrink(0).Padding(space2, space3, space3).Gap(space2), 1, 0, 0, 0).Children(func() {
		if len(a.scope) > 0 || len(a.attached) > 0 {
			ui.Row(c).FillWidth().Wrap().Gap(space1_5).Children(func() {
				all := w.agentPanes()
				defer w.nbChips(c) // the attached notebooks go after the panes
				for _, id := range a.scope {
					var p *agentPane
					for i := range all {
						if all[i].sessionID == id {
							p = &all[i]
						}
					}
					if p == nil {
						continue
					}
					ui.Row(c).Height(sizeControlSm).Padding(0, space1, 0, space2).Gap(space1).Radius(radiusFull).Background(hoverGround).Key(id).Label("scope " + pickLabel(*p, all)).Children(func() {
						icon(c, "agent-at", inkMuted)
						mono(c, p.label, ink)
						if iconBtn(c, "cancel-01", "Stop reading "+p.label, sizeControlSm-space1, false) {
							w.act(func() { w.setAgentScope(removeID(a.scope, id)) })
						}
					})
				}
			})
		}
		w.nbHint(c)
		ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(controlFill(stage)).Border(1, lineStrong).Children(func() {
			ui.TextAreaBase(c, &a.input).Label("Message the agent").Placeholder(agentInputHint).PlaceholderColor(inkFaint).TextColor(ink).
				FontSize(uiFontSize).Lines(2, 8).FillWidth().Bind(&a.inputH).OnTextKey(func(k ui.TextKey) bool {
				if k.Key != ui.KeyEnter || k.Mods != 0 { // ⇧↵ is a newline
					return false
				}
				q := k.Text()
				k.SetText("")
				w.act(func() {
					if !w.agentAsk(q) && a.input == "" { // nothing sent (busy, or no key yet): give the words back
						a.input = q
					}
				})
				return true
			})
		})
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
			w.attachButton(c, "panel")
			text(c, agentComposerHint, inkFaint, captionSize, weightRegular).Shrink(1).MinWidth(0)
			ui.Spacer(c)
			if a.busy != nil && textBtn(c, "Stop", btnGhost) {
				w.act(w.agentStop)
			}
			if iconBtnOff(c, "arrow-up-02", "Send", sizeControlSm, false, a.busy != nil) {
				w.act(func() {
					q := a.input
					a.input = ""
					if !w.agentAsk(q) && a.input == "" {
						a.input = q
					}
				})
			}
		})
	})
	if a.focusInput && !w.modalOpen() {
		a.inputH.Focus(c)
		a.focusInput = false
	}
}

// agentSash is .k-agent-sash: a strip on the panel's left edge (8 px wide, a brass line while hovered, focused or dragged).
// Dragging it left widens the panel; ← and → step it by size-agent-step (⇧ four times that), Home and End go to the narrowest
// and widest, a double click puts the default back. The width is kept when the pointer lets go and after each key.
func (w *workspace) agentSash(c *ui.Context, winW float32) {
	a := w.agent
	s := ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Width(space2).Cursor(ui.CursorResizeColumn).Focusable().Bind(&a.sashH).Label("Resize agent panel")
	cur := clampAgentWidth(a.width, winW)
	if dx, _, down := s.Dragged(); down {
		a.sashDrag = true
		if dx != 0 {
			a.width = clampAgentWidth(cur-dx, winW)
		}
	} else if a.sashDrag { // let go: keep the width
		a.sashDrag = false
		w.act(func() { w.setAgentWidth(a.width) })
	}
	if s.DoubleClicked() {
		w.act(func() { w.setAgentWidth(0) })
	}
	s.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind != ui.InputKeyDown {
			return
		}
		step := float32(sizeAgentStep)
		if ev.Mods&ui.Shift != 0 {
			step *= 4
		}
		switch ev.Key { // the panel is on the right: ← widens it
		case ui.KeyLeft:
			w.act(func() { w.setAgentWidth(clampAgentWidth(cur+step, winW)) })
		case ui.KeyRight:
			w.act(func() { w.setAgentWidth(clampAgentWidth(cur-step, winW)) })
		case ui.KeyHome:
			w.act(func() { w.setAgentWidth(0) })
		case ui.KeyEnd:
			w.act(func() { w.setAgentWidth(winW) }) // clamped to half the window
		}
	})
	if a.sashDrag || s.Hovered() || a.sashH.FocusWithin(c) {
		s.Children(func() { ui.Box(c).Absolute().Top(0).Bottom(0).Left(space1).Width(1).Background(brassInk) })
	}
}

func removeID(ids []string, id string) (out []string) {
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
