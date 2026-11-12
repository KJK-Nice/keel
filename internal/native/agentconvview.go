package native

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Recent conversations drawn (internal/ui/agent.templ #agent-sessions, agentthread.js sessionRowHTML and
// sessionGroupsHTML; the k-agent-sessions* and k-agent-sess-* rules of keel.css): a popover under the header with a
// search, the Today / Yesterday / Older groups with counts, rows with title, host badges, time and "N proposals", Rename
// and Delete on each row, and a footer with Open all in a tab, the retention note and Delete all.

// rowOpts is how a list row is drawn: the panel's rows have Rename and Delete, the page's do not (its header has them).
type rowOpts struct {
	current, selected, actions bool
}

// agentSessions is the popover. anchor is the title button.
func (w *workspace) agentSessions(c *ui.Context, anchor ui.Element) {
	a := w.agent
	open := true
	_, winH := c.Size()
	ui.PopoverBase(c, anchor, &open, func(panel ui.Element) {
		panel.Width(sizeAgent-space4).MaxHeight(winH*0.7).Background(raised).Border(1, line).Radius(radiusMd).Clip().
			Shadow(0, 8, 24, 0, shadowPop).Label("dialog Recent conversations")
		groups := w.listGroups(a.sess.filter, "")
		rows := flatRows(groups)
		a.sess.sel = min(a.sess.sel, len(rows)-1)
		ui.Row(c).FillWidth().Shrink(0).Padding(space2, space2, space1).Children(func() {
			ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Background(controlFill(stage)).Border(1, lineStrong).Children(func() {
				icon(c, "search-01", inkFaint)
				in := ui.TextInputBase(c, &a.sess.filter).Label("Search conversations").Placeholder("Search conversations").PlaceholderColor(inkFaint).
					TextColor(ink).FontSize(uiFontSize).Grow(1).MinWidth(0).Bind(&a.sess.filterH)
				in.OnTextKey(func(k ui.TextKey) bool { return w.sessionKey(k, rows) })
			})
		})
		ui.Scroll(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).Label("conversations").Children(func() {
			ui.Column(c).FillWidth().Padding(space1, space1_5, space1_5).Children(func() {
				w.sessionList(c, groups, "", true)
			})
		})
		hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space1_5).Gap(space1_5).AlignItems(ui.Center).Background(w.ground(surface)), 1, 0, 0, 0).Children(func() {
			if textBtn(c, "Open all in a tab", btnGhost) {
				w.act(func() { w.closeSessions(); w.openPage(pageAgent) })
			}
			if d := a.historyDays(); d != nil {
				caption(c, retentionNote(*d), inkFaint)
			}
			ui.Spacer(c)
			off := len(a.list.sessions) == 0 && !a.list.unreadable
			b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label("Delete all").Cursor(ui.CursorPointer)
			if b.Hovered() && !off {
				b.Background(hoverGround)
			}
			if off {
				b.Opacity(dimAlpha).Disabled(true)
			}
			b.Children(func() { label(c, "Delete all", flare) })
			if b.Clicked() && !off {
				w.act(w.askDeleteAll)
			}
		})
	})
	if a.sess.focus && a.renameID == "" {
		a.sess.filterH.Focus(c)
		a.sess.focus = false
	}
	if !open {
		w.act(w.closeSessions)
	}
}

// listGroups is the saved list, searched by the popover's field (or the page's) and, on the page, by host.
func (w *workspace) listGroups(query, host string) []sessionGroup {
	a := w.agent
	if a.list.unreadable || a.list.err != "" {
		return nil
	}
	return sessionGroups(a.list.sessions, query, time.Now(), host)
}

func flatRows(groups []sessionGroup) (out []keelclient.ConversationSummary) {
	for _, g := range groups {
		out = append(out, g.rows...)
	}
	return out
}

// sessionKey handles ↑ ↓ and ↵ typed in the search field: ↑↓ move between the search field and the rows, ↵ resumes the
// row. A key typed while a row is being renamed is the rename field's.
func (w *workspace) sessionKey(k ui.TextKey, rows []keelclient.ConversationSummary) bool {
	a := w.agent
	if k.Mods != 0 || a.renameID != "" || len(rows) == 0 {
		return false
	}
	switch k.Key {
	case ui.KeyDown:
		w.act(func() { a.sess.sel = (a.sess.sel+2)%(len(rows)+1) - 1 }) // -1 is the field
		return true
	case ui.KeyUp:
		w.act(func() { a.sess.sel = (a.sess.sel+len(rows)+1)%(len(rows)+1) - 1 })
		return true
	case ui.KeyEnter:
		if a.sess.sel >= 0 && a.sess.sel < len(rows) {
			id := rows[a.sess.sel].ID
			w.act(func() { w.resumeConversation(id) })
			return true
		}
	}
	return false
}

// sessionList draws the groups (or why there are none).
func (w *workspace) sessionList(c *ui.Context, groups []sessionGroup, host string, actions bool) {
	a := w.agent
	if empty := sessionsEmptyText(a.list, len(groups)); empty != "" {
		if host != "" && len(a.list.sessions) > 0 && a.list.err == "" && !a.list.unreadable {
			empty = "No conversations on this host."
		}
		ui.Row(c).FillWidth().Padding(space3, space2).Children(func() {
			ui.Text(c, empty).TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
		})
		return
	}
	i := 0
	for _, g := range groups {
		ui.Row(c).FillWidth().Height(sizeControlSm+space0_5).Padding(0, space2).Margin(space1_5, 0, 0).AlignItems(ui.Center).Key("group " + g.label).Children(func() {
			text(c, g.label, inkFaint, captionSize, weightSemibold)
			ui.Spacer(c)
			text(c, strconv.Itoa(len(g.rows)), inkFaint, captionSize, weightSemibold)
		})
		for _, s := range g.rows {
			w.sessionRow(c, s, rowOpts{current: s.ID == a.conv, selected: actions && i == a.sess.sel, actions: actions})
			i++
		}
	}
}

// sessionRow is one saved conversation.
func (w *workspace) sessionRow(c *ui.Context, s keelclient.ConversationSummary, o rowOpts) {
	a := w.agent
	title := orElse(s.Title, titleNewConv)
	if o.actions && a.renameID == s.ID { // the inline title field (↵ saves, Esc leaves the title as it was)
		w.renameField(c, s.ID, "Conversation title", true)
		return
	}
	row := ui.Row(c).FillWidth().Padding(space1, space2).Gap(space1).Radius(radiusSm).AlignItems(ui.Center).Key(s.ID)
	if o.current || o.selected || row.Hovered() {
		row.Background(hoverGround)
	}
	if o.selected {
		row.ScrollIntoView()
	}
	row.Children(func() {
		pick := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Label("conversation " + title).Cursor(ui.CursorPointer).Focusable()
		pick.Children(func() {
			wt := weightRegular
			if o.current {
				wt = weightMedium
			}
			ui.Text(c, title).TextColor(ink).FontSize(uiFontSize).FontWeight(wt).MaxLines(1)
			ui.Row(c).FillWidth().Gap(space1_5).Wrap().AlignItems(ui.Center).Children(func() {
				for i, h := range s.Hosts {
					if i == 3 {
						break
					}
					pill(c, h, hoverGround, inkMuted)
				}
				caption(c, sessionWhen(s.UpdatedAt, time.Now()), inkFaint)
				caption(c, "· "+proposalsLabel(s.Proposals), inkFaint)
			})
		})
		if pick.Clicked() {
			id := s.ID
			w.act(func() { w.resumeConversation(id) })
		}
		if o.actions {
			if iconBtn(c, "agent-pencil", "Rename "+title, sizeControlSm, false) {
				id, t := s.ID, s.Title
				w.act(func() { w.beginConvRename(id, t) })
			}
			if iconBtn(c, "agent-trash", "Delete "+title, sizeControlSm, false) {
				id := s.ID
				w.act(func() { w.deleteConversation(id) })
			}
		}
	})
}

// renameField is the title field of a row being renamed, or of the page's heading: ↵ and Save send the new title, Esc
// leaves it as it was.
func (w *workspace) renameField(c *ui.Context, id, name string, withSave bool) {
	a := w.agent
	ui.Row(c).FillWidth().Padding(space1, space2).Gap(space1_5).AlignItems(ui.Center).Key("rename " + id).Children(func() {
		ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			inputBox(c, name, &a.renameText, "", &a.renameH, false, false, 120, func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == 0:
					t := k.Text()
					w.act(func() { w.renameConversation(id, t) })
					return true
				case k.Key == ui.KeyEscape && k.Mods == 0:
					w.act(w.endConvRename)
					return true
				}
				return false
			})
		})
		if withSave && textBtn(c, "Save", btnPrimary) {
			t := a.renameText
			w.act(func() { w.renameConversation(id, t) })
		}
	})
	if a.renameFocus {
		a.renameH.Focus(c)
		a.renameFocus = false
	}
}
