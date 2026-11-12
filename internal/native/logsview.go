package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Logs page drawn as LogsPage and logRow (internal/ui) with .k-page-head, .k-toolbar, .k-chip and
// .k-table--log of keel.css.

const (
	logTimeCol = 68  // the Time column: the reference puts Kind 80 px after Time, Target 108 after Kind and Event 116 after Target; the row's gap is 12
	logKindCol = 96  // the Kind column
	logTargCol = 104 // the Target column
)

// pageHead is k-page-head: the title and its line, and the page's actions on the right.
func pageHead(c *ui.Context, title, desc string, actions func()) {
	ui.Row(c).FillWidth().Padding(space6, space8, space4).Gap(space3).AlignItems(ui.End).Shrink(0).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1).Children(func() {
			ui.Text(c, title).TextColor(ink).FontSize(displaySize).FontWeight(weightSemibold)
			ui.Text(c, desc).TextColor(inkMuted).FontSize(uiFontSize).MaxWidth(70 * 7)
		})
		actions()
	})
}

func (w *workspace) logsView(c *ui.Context, p *logsPage) {
	ui.Column(c).Grow(1).FillWidth().FillHeight().Background(w.ground(surface)).Children(func() {
		pageHead(c, "Logs", "What Keel did on this machine, newest first. Secrets are never written here.", func() {
			if textBtn(c, "Refresh", btnDefault) {
				w.act(w.loadLogs)
			}
		})
		ui.Row(c).FillWidth().Padding(0, space8, space3).Gap(space2).Wrap().Shrink(0).Children(func() {
			searchField(c, &p.filter, "Search events", &p.filterH).Width(sizeSidebar)
			ui.Row(c).Gap(space1_5).Label("Show events").Children(func() {
				for _, ch := range logChips {
					on := p.kind == ch[0]
					r := ui.Row(c).Height(sizeControlSm).Padding(0, space3).Radius(radiusFull).Justify(ui.Center).Shrink(0).Label(ch[1]).Cursor(ui.CursorPointer).Checked(on)
					col := inkMuted
					switch {
					case on:
						r.Background(hoverGround).Border(1, lineStrong)
						col = ink
					default:
						r.Border(1, line)
						if r.Hovered() {
							col = ink
						}
					}
					r.Children(func() { label(c, ch[1], col) })
					if r.Clicked() {
						k := ch[0]
						w.act(func() { p.kind = k })
					}
				}
			})
		})
		hairline(ui.Row(c).FillWidth().Height(sizePanebar).Padding(0, space6, 0, space8).Gap(space3).Shrink(0), 0, 0, 1, 0).Children(func() {
			ui.Row(c).Width(logTimeCol).Shrink(0).Children(func() { label(c, "Time", inkFaint) })
			ui.Row(c).Width(logKindCol).Shrink(0).Children(func() { label(c, "Kind", inkFaint) })
			ui.Row(c).Width(logTargCol).Shrink(0).Children(func() { label(c, "Target", inkFaint) })
			ui.Row(c).Grow(1).Children(func() { label(c, "Event", inkFaint) })
		})
		rows := p.rows()
		switch {
		case p.err != "":
			if pr, _ := emptyPage(c, emptyFailed("logs-tab", "the log", p.err)); pr {
				w.act(func() { w.loadPage(w.tab()) })
			}
		case len(rows) == 0 && p.loaded && len(p.entries) == 0:
			emptyPage(c, emptyNone("logs-tab", "No log entries", logsNone, ""))
		case len(rows) == 0 && p.loaded:
			if pr, _ := emptyPage(c, emptyNoMatch("search-01", orElse(p.filter, p.kind))); pr {
				w.act(func() { p.filter, p.kind = "", "all" })
			}
		default:
			ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Label("log list").Children(func() {
				for i, e := range rows {
					w.logRow(c, i, e)
				}
			})
		}
	})
}

// logRow is one row: time, kind, target and event (with the counts of an agent entry).
func (w *workspace) logRow(c *ui.Context, i int, e keelclient.LogEntry) {
	r := hairline(ui.Row(c).FillWidth().MinHeight(sizeControlLg).Padding(space1, space6, space1, space8).Gap(space3).Key(i).Label("log "+e.Kind), 0, 0, 1, 0)
	if r.Hovered() {
		r.Background(hoverGround)
	}
	r.Children(func() {
		ui.Row(c).Width(logTimeCol).Shrink(0).Children(func() { mono(c, logTimeLabel(e), inkMuted) })
		ui.Row(c).Width(logKindCol).Shrink(0).Children(func() {
			if logBadgeFailed(e.Kind) {
				tag(c, e.Kind, flareSoft, flare, false)
			} else {
				tag(c, e.Kind, hoverGround, inkMuted, false)
			}
		})
		ui.Row(c).Width(logTargCol).Shrink(0).Children(func() { mono(c, logTarget(w.hosts, e), inkMuted).MinWidth(0) })
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1).Children(func() {
			ui.Text(c, e.Message).TextColor(ink).FontSize(uiFontSize).MaxLines(1)
			if facts := logFacts(e); len(facts) > 0 {
				ui.Row(c).Wrap().Gap(space1).Tooltip(logFactsTitle(e)).Children(func() {
					for _, f := range facts {
						ui.Row(c).Height(space4).Padding(0, space1_5).Radius(radiusXs).Border(1, lineStrong).Justify(ui.Center).Children(func() {
							text(c, f, inkMuted, kbdSize, weightRegular).Font(monoStack)
						})
					}
				})
			}
		})
	})
}
