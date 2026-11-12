package native

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The Agent page drawn (internal/ui/agentpage.templ and the k-agentpage* rules of keel.css): the conversation list on the
// left, the conversation in the middle (the panel's thread and cards), and what it may read, has read and proposed on the
// right. Colours and sizes are tokens.

func (w *workspace) agentPageView(c *ui.Context, p *agentPage) {
	a := w.agent
	w.agentSync()
	if a.settings == nil && !a.loading && !a.loadTried {
		a.loadTried = true
		w.act(w.loadAgent)
	}
	if w.copyOut != "" { // a Copy from a card, which has no context to write with
		c.WriteClipboard(w.copyOut)
		w.copyOut = ""
		w.say("Copied")
	}
	layout := pageLayout(p.width)
	root := ui.Row(c).Grow(1).FillWidth().FillHeight().Background(w.ground(surface)).Label("agent page")
	p.width = root.Bounds().W
	root.Children(func() {
		switch layout {
		case layoutWide:
			w.pageListCol(c, p, ui.Column(c).Width(sizeAgentPageList).Shrink(0).FillHeight())
			w.pageMain(c, p, 1)
			w.pageContextCol(c, p, ui.Column(c).Width(sizeAgentPageContext).Shrink(0).FillHeight(), true)
		default:
			// The context column goes under the list; the thread sits beside both (3fr over 2fr).
			left := ui.Column(c).Shrink(0).FillHeight().MinWidth(0)
			main := float32(1)
			if layout == layoutMid {
				left.Width(sizeAgentPageList)
			} else {
				left.Grow(2).Basis(0).Shrink(1) // below 660 px the two columns share the width 2:3
				main = 3
			}
			left.Children(func() {
				w.pageListCol(c, p, ui.Column(c).FillWidth().Grow(3).Shrink(1).MinHeight(0))
				w.pageContextCol(c, p, ui.Column(c).FillWidth().Grow(2).Shrink(1).MinHeight(0), false)
			})
			w.pageMain(c, p, main)
		}
	})
	if p.focusInput && !w.modalOpen() && !a.sessOpen {
		p.inputH.Focus(c)
		p.focusInput = false
	}
}

// ---- left: the saved conversations ----

func (w *workspace) pageListCol(c *ui.Context, p *agentPage, col ui.Element) {
	a := w.agent
	hairline(col.Background(w.ground(chrome)).Label("agent conversations"), 0, 1, 0, 0).Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Padding(space3, space2+space0_5, space2).Gap(space2).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space2).Children(func() {
				ui.Text(c, "Conversations").TextColor(ink).FontSize(uiFontSize).FontWeight(weightSemibold).Grow(1)
				if iconBtn(c, "add-01", "New conversation", sizeControlSm, false) {
					w.act(func() { w.pageNewConversation(p) })
				}
			})
			ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Background(controlFill(stage)).Border(1, lineStrong).Children(func() {
				icon(c, "search-01", inkFaint)
				ui.TextInputBase(c, &p.filter).Label("Search conversations").Placeholder("Search conversations").PlaceholderColor(inkFaint).
					TextColor(ink).FontSize(uiFontSize).Grow(1).MinWidth(0).Bind(&p.filterH)
			})
			w.pageHostChips(c, p)
		})
		groups := w.listGroups(p.filter, p.host)
		ui.Scroll(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).TrackScroll(&p.listScroll).Label("conversations").Children(func() {
			ui.Column(c).FillWidth().Padding(0, space1_5, space2).Children(func() {
				w.sessionList(c, groups, p.host, false)
			})
		})
		hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space2, space3).Gap(space2).AlignItems(ui.Center), 1, 0, 0, 0).Children(func() {
			ui.Text(c, a.pageRetention()).TextColor(inkMuted).FontSize(captionSize - 1).Grow(1).Shrink(1).MinWidth(0)
			r := ui.Row(c).Shrink(0).Label("Retention settings").Cursor(ui.CursorPointer)
			r.Children(func() { text(c, "Retention settings", brassInk, captionSize-1, weightMedium) })
			if r.Clicked() {
				w.act(w.openRetentionSettings)
			}
		})
	})
}

// pageHostChips are the host filter: All and up to six hosts, the most recent conversations' first.
func (w *workspace) pageHostChips(c *ui.Context, p *agentPage) {
	a := w.agent
	var hosts []string
	if !a.list.unreadable && a.list.err == "" {
		hosts = hostFilters(a.list.sessions, hostChipMax)
	}
	if p.host != "" && !contains(hosts, p.host) {
		p.host = ""
	}
	if len(hosts) == 0 {
		return
	}
	ui.Row(c).FillWidth().Wrap().Gap(space1_5).Label("Filter by host").Children(func() {
		for _, h := range append([]string{""}, hosts...) {
			name := orElse(h, "All")
			on := p.host == h
			ch := ui.Row(c).Height(sizeControlSm).Padding(0, space2+space0_5).Radius(radiusFull).Justify(ui.Center).Key("chip " + h).
				Label("host filter " + name).Cursor(ui.CursorPointer).Selected(on)
			col := inkMuted
			switch {
			case on:
				ch.Background(brassSoft)
				col = brassInk
			default:
				ch.Border(1, line)
				if ch.Hovered() {
					ch.Background(hoverGround)
				}
			}
			ch.Children(func() { text(c, name, col, uiFontSize, weightMedium).MaxLines(1) })
			if ch.Clicked() {
				host := h
				w.act(func() { p.host = host })
			}
		}
	})
}

// ---- middle: the conversation ----

func (w *workspace) pageMain(c *ui.Context, p *agentPage, grow float32) {
	a := w.agent
	ui.Column(c).Grow(grow).Basis(0).Shrink(1).MinWidth(0).FillHeight().Background(w.ground(surface)).Label("agent conversation").Children(func() {
		w.pageHead(c, p)
		n := len(a.turns)
		for _, t := range a.turns {
			n += len(t.props)
			if t.pending {
				n++
			}
		}
		if n != p.seen {
			p.seen = n
			p.scroll.Y = math.MaxFloat32
		}
		ui.Scroll(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).TrackScroll(&p.scroll).Label("agent page thread").Children(func() {
			ui.Column(c).FillWidth().MaxWidth(sizeAgentPageThread).Padding(space4+space1, space6).Gap(space4).Children(func() {
				w.agentThread(c, true)
			})
		})
		w.pageComposer(c, p)
	})
}

func (w *workspace) pageHead(c *ui.Context, p *agentPage) {
	a := w.agent
	known := a.known()
	hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space4, space6, space3+space0_5).Gap(space2+space0_5).AlignItems(ui.Center), 0, 0, 1, 0).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1_5).Children(func() {
			if p.renaming {
				w.pageRenameField(c, p)
			} else {
				ui.Text(c, orElse(a.title, titleNewConv)).TextColor(ink).FontSize(rem(18)).FontWeight(weightSemibold).MaxLines(1).Label("agent page title")
			}
			hosts, when := a.pageHeadMeta()
			ui.Row(c).FillWidth().Wrap().Gap(space1_5).AlignItems(ui.Center).Children(func() {
				for _, h := range hosts {
					pill(c, h, hoverGround, inkMuted)
				}
				caption(c, when, inkFaint)
			})
		})
		b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Border(1, lineStrong).Background(raised).Shrink(0).
			Label("Continue in panel").Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(hoverGround)
		}
		b.Children(func() {
			label(c, "Continue in panel", ink)
			kbd(c, agentKeys())
		})
		if b.Clicked() {
			w.act(func() { w.setAgentOpen(true) })
		}
		if iconBtnOff(c, "agent-pencil", "Rename this conversation", sizeControlSm, false, !known) {
			w.act(func() { p.renaming, p.renameText, p.renameWas = true, a.title, false })
		}
		if iconBtnOff(c, "agent-trash", "Delete this conversation", sizeControlSm, false, !known) {
			conv := a.conv
			w.act(func() { w.deleteConversation(conv) })
		}
	})
}

// pageRenameField is the heading as a field: ↵ and Save rename the conversation on the id keel serve gave, Esc leaves it.
func (w *workspace) pageRenameField(c *ui.Context, p *agentPage) {
	a := w.agent
	conv := a.conv
	save := func(t string) {
		w.act(func() {
			p.renaming = false
			if t = strings.TrimSpace(t); t != "" && t != a.title {
				w.renameConversation(conv, t)
			}
		})
	}
	ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			inputBox(c, "Conversation title", &p.renameText, "", &p.renameH, false, false, 120, func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == 0:
					save(k.Text())
					return true
				case k.Key == ui.KeyEscape && k.Mods == 0:
					w.act(func() { p.renaming = false })
					return true
				}
				return false
			})
		})
		if textBtn(c, "Save", btnPrimary) {
			save(p.renameText)
		}
	})
	if !p.renameWas {
		p.renameH.Focus(c)
		p.renameWas = true
	}
}

func (w *workspace) pageComposer(c *ui.Context, p *agentPage) {
	a := w.agent
	ui.Column(c).FillWidth().Shrink(0).Padding(0, space6, space4).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(sizeAgentPageThread-2*space6).Padding(space2, space3, space3).Gap(space2).Radius(radiusMd).Background(controlFill(raised)).Border(1, line).Children(func() {
			if len(a.attached) > 0 { // the page's chips are the attached notebooks (agentpage.js paintChips)
				ui.Row(c).FillWidth().Wrap().Gap(space1_5).Children(func() { w.nbChips(c) })
			}
			w.nbHint(c)
			ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
				ui.TextAreaBase(c, &p.input).Label("Message the agent on the page").Placeholder(w.pagePlaceholder()).PlaceholderColor(inkFaint).TextColor(ink).
					FontSize(uiFontSize).Lines(2, 8).FillWidth().Bind(&p.inputH).OnTextKey(func(k ui.TextKey) bool {
					if k.Key != ui.KeyEnter || k.Mods != 0 { // ⇧↵ is a newline
						return false
					}
					q := k.Text()
					k.SetText("")
					w.act(func() { w.pageAsk(p, q) })
					return true
				})
			})
			ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
				w.attachButton(c, "page")
				text(c, "Reads only panes in scope now · ↵ send", inkFaint, captionSize, weightRegular).Shrink(1).MinWidth(0)
				ui.Spacer(c)
				if a.busy != nil && textBtn(c, "Stop", btnGhost) {
					w.act(w.agentStop)
				}
				if iconBtnOff(c, "arrow-up-02", "Send", sizeControlSm, false, a.busy != nil) {
					w.act(func() {
						q := p.input
						p.input = ""
						w.pageAsk(p, q)
					})
				}
			})
		})
	})
}

func (w *workspace) pagePlaceholder() string {
	if w.agent.questions() > 0 {
		return "Continue this conversation…"
	}
	return "Ask about your sessions…"
}

// pageAsk sends a question from the page's composer: the window's one conversation answers it, and the panel shows it too.
// Nothing sent (busy, or no key yet): the words come back.
func (w *workspace) pageAsk(p *agentPage, q string) {
	if !w.agentAsk(q) && p.input == "" {
		p.input = q
	}
}

// ---- right: in scope, read so far, proposals ----

func (w *workspace) pageContextCol(c *ui.Context, p *agentPage, col ui.Element, wide bool) {
	a := w.agent
	col.Background(w.ground(chrome)).Label("agent conversation context")
	if wide {
		hairline(col, 0, 0, 0, 1)
	} else {
		hairline(col, 1, 0, 0, 0)
	}
	col.Children(func() {
		ui.Scroll(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).TrackScroll(&p.ctxScroll).Children(func() {
			ui.Column(c).FillWidth().Padding(space4, space3+space0_5).Gap(space4 + space1).Children(func() {
				scope := w.pageScopePanes()
				w.ctxSection(c, "In scope", len(scope), func() {
					if len(scope) == 0 {
						ctxEmpty(c, "No pane in scope. Choose panes in the agent panel.")
					}
					for _, sp := range scope {
						ui.Row(c).FillWidth().MinHeight(sizeControlSm).Gap(space2).AlignItems(ui.Center).Key(sp.sessionID).Children(func() {
							dot(c, tide)
							mono(c, paneTarget(sp.label, sp.sub), ink).Grow(1).Shrink(1).MinWidth(0).MaxLines(1)
							if sp.production {
								pill(c, "Production", signalSoft, signal)
							}
							pill(c, "Agent reads", hazeSoft, haze)
						})
					}
				})
				reads := readSummary(a.convTurns())
				w.ctxSection(c, "Read so far", -1, func() {
					if len(reads) == 0 {
						ctxEmpty(c, "Nothing read yet.")
					}
					for _, r := range reads {
						ui.Row(c).FillWidth().MinHeight(sizeControlSm).Gap(space2).AlignItems(ui.Center).Key(r.host).Children(func() {
							mono(c, r.host, ink).Grow(1).Shrink(1).MinWidth(0).MaxLines(1)
							mono(c, r.linesLabel, inkMuted)
							mono(c, r.redactLabel, inkFaint)
						})
					}
				})
				props := a.pageProposals()
				w.ctxSection(c, "Proposals", len(props), func() {
					if len(props) == 0 {
						ctxEmpty(c, "No proposals yet.")
					}
					for _, pr := range props {
						lbl, kind := proposalState(pr.p, pr.typed)
						ui.Row(c).FillWidth().MinHeight(sizeControlSm).Gap(space2).AlignItems(ui.Center).Key(pr.p.ID).Children(func() {
							mono(c, strings.Join(strings.Fields(pr.p.Command), " "), ink).Grow(1).Shrink(1).MinWidth(0).MaxLines(1)
							switch kind {
							case "brass":
								pill(c, lbl, brassSoft, brassInk)
							case "outline":
								outlineBadge(c, lbl)
							default:
								pill(c, lbl, hoverGround, inkMuted)
							}
						})
					}
				})
				ui.Text(c, "Every read and every typed proposal is also in Logs, under Agent.").TextColor(inkMuted).FontSize(captionSize - 1)
			})
		})
	})
}

// ctxSection is a heading with its count (the count is left out for a negative one) and its rows.
func (w *workspace) ctxSection(c *ui.Context, title string, n int, rows func()) {
	ui.Column(c).FillWidth().Gap(space1_5).Key(title).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			text(c, title, inkFaint, captionSize, weightSemibold)
			ui.Spacer(c)
			if n >= 0 {
				text(c, strconv.Itoa(n), inkFaint, captionSize, weightSemibold)
			}
		})
		ui.Column(c).FillWidth().Gap(space0_5).Children(rows)
	})
}

func ctxEmpty(c *ui.Context, s string) {
	ui.Text(c, s).TextColor(inkMuted).FontSize(captionSize - 1)
}
