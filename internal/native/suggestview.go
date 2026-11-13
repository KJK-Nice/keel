package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The notice, the card and the inserted look drawn over a pane's terminal (cmd/keel/web/js/ui/suggest.js render, the
// k-sug-* rules of keel.css). The text on the line stays the terminal's: marks sit over its cells, the hint after the
// words, the card under the line (above it when it does not fit). Cell rects come from gridFor (suggestgeom.go).

const (
	sugCardWidth = 560 // min(43.077rem, ...)
	sugHintWidth = 330 // the notice's hint is placed by this estimate: the row below takes it when it will not fit after the words
)

func noticeHintText(mac bool) (lead, tail string) { return "Looks like a request", "suggest a command" }

// sugBadge is the card's risk badge.
func sugBadge(c *ui.Context, o keelclient.SuggestOption) {
	_, kind := riskBadge(o.Risk)
	fg, bg := tide, tideSoft
	switch kind {
	case "flare":
		fg, bg = flare, flareSoft
	case "signal":
		fg, bg = signal, signalSoft
	}
	ui.Row(c).Height(sizeControlSm-6).Padding(0, space1_5).Gap(space1).Radius(radiusFull).AlignItems(ui.Center).Shrink(0).Background(bg).Label("badge " + riskWords(o)).Children(func() {
		if o.Risk == keelclient.RiskDestructive {
			icon(c, "alert", fg)
		}
		label(c, riskWords(o), fg)
	})
}

func (w *workspace) suggestOverlay(c *ui.Context, p *pane) {
	v := p.sg.v
	if p.sg.m == nil || v.State == sugIdle || p.sg.line == nil || p.size == nil {
		return
	}
	cols, _ := p.size()
	if cols <= 0 {
		return
	}
	g := gridFor(p.fontFamily, p.fontSize, p.sg.scale)
	line := p.sg.line
	b := p.handle.Bounds(c)
	mac := true
	if p.sg.m != nil {
		mac = p.sg.m.d.mac
	}
	clip := func(start, n int) int { return max(min(n, cols-start), 0) }
	mark := func(start, n int, bg ui.Color, name string, underline bool) (ui.Rect, bool) {
		n = clip(start, n)
		if n == 0 {
			return ui.Rect{}, false
		}
		r := g.at(line.Row, start, n)
		e := ui.Box(c).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).Radius(radiusXs).Background(bg).Label(name).HitTest(func(float32, float32, ui.Rect) bool { return false })
		if underline {
			e.InsetShadow(0, -1, 0, 0, haze)
		}
		return r, true
	}
	hint := func(after ui.Rect, name string, build func()) {
		left, top := after.X+after.W+g.cw, after.Y
		if left+sugHintWidth > b.W-g.cw {
			left, top = g.ox+g.cw, after.Y+g.ch // no room after the words: the row below, from the line's start
		}
		ui.Row(c).Absolute().Left(left).Top(top).Height(g.ch).Padding(0, space1_5).Gap(space1_5).AlignItems(ui.Center).Background(stage).Label(name).
			HitTest(func(float32, float32, ui.Rect) bool { return false }).Children(build)
	}
	k := keyLabels(mac)
	switch v.State {
	case sugNotice:
		r, ok := mark(line.Start, len([]rune(line.Input)), hazeSoft, "suggestion notice", true)
		if ok {
			lead, tail := noticeHintText(mac)
			hint(r, "suggestion hint", func() {
				icon(c, "ai-magic", accentInk)
				caption(c, lead, inkFaint)
				if v.Hash { // a # line is a request: ⇥ asks. A line noticed on its own leaves ⇥ to the shell and names the key
					kbd(c, k.Tab)
				} else {
					for _, s := range k.Suggest {
						kbd(c, s)
					}
				}
				caption(c, tail, inkFaint)
			})
		}
	case sugInserted:
		if !v.Seen {
			return
		}
		if r, ok := mark(line.Start, len([]rune(v.Command)), accentSoft, "suggestion inserted", false); ok {
			hint(r, "suggestion hint", func() {
				caption(c, "Inserted, not run", inkFaint)
				kbd(c, k.Enter)
				caption(c, "run", inkFaint)
				for _, u := range k.Undo {
					kbd(c, u)
				}
				caption(c, "back to your words", inkFaint)
			})
		}
	default: // loading, error, card, inserting: the words greyed, the card below
		r, ok := mark(line.Start, max(len([]rune(line.Input)), 1), stage.Alpha(0.55), "suggestion words", false)
		if !ok {
			return
		}
		w.suggestCard(c, p, v, k, r, g, b)
	}
}

// suggestCard draws the pending, error and card states under (or over) the line.
func (w *workspace) suggestCard(c *ui.Context, p *pane, v sugView, k keyLabelSet, at ui.Rect, g cellGrid, b ui.Rect) {
	width := min(float32(sugCardWidth), b.W-2*space3)
	left, below := g.ox+space3, at.Y+g.ch+space1_5
	top := below
	if h := p.sg.cardH; h > 0 && below+h > b.H && at.Y-h-space1_5 >= 0 {
		top = at.Y - h - space1_5
	}
	var opt *keelclient.SuggestOption
	if v.Sug != nil && len(v.Sug.Options) > 0 {
		opt = &v.Sug.Options[min(max(v.Selected, 0), len(v.Sug.Options)-1)]
	}
	danger := opt != nil && opt.Risk == keelclient.RiskDestructive
	name := "suggestion card"
	switch {
	case v.State == sugLoading:
		name = "suggestion card pending"
	case v.State == sugError:
		name = "suggestion card error"
	case danger:
		name = "suggestion card destructive"
	}
	edge := line
	if danger {
		edge = flare
	}
	col := ui.Column(c).Absolute().Left(left).Top(top).Width(width).Background(raised).Border(1, edge).Radius(radiusMd).Clip().
		Shadow(0, 4, 16, 0, shadowPop).Label(name).Bind(&p.sg.card)
	col.Children(func() {
		head := func(src string, withBadge bool) {
			ui.Row(c).FillWidth().Padding(space2, space3, 0, space3).Gap(space2).AlignItems(ui.Center).Children(func() {
				icon(c, "ai-magic", accentInk)
				caption(c, src, inkFaint)
				if withBadge && opt != nil {
					ui.Spacer(c)
					sugBadge(c, *opt)
				}
			})
		}
		foot := func(items func(), rule string) { // the rule wraps to its own line, as the foot's flex-wrap puts it
			hairline(ui.Column(c).FillWidth().Padding(space1_5, space3).Gap(space1_5).Background(surface).Margin(space2, 0, 0, 0), 1, 0, 0, 0).Children(func() {
				ui.Row(c).FillWidth().Gap(space3).AlignItems(ui.Center).Children(items)
				if rule != "" {
					caption(c, rule, inkFaint)
				}
			})
		}
		keep := func() {
			ui.Row(c).Gap(space1).AlignItems(ui.Center).Children(func() { kbd(c, k.Esc); caption(c, "keep my words", inkFaint) })
		}
		switch {
		case v.State == sugLoading || v.State == sugError || opt == nil:
			head("Suggested from your words", false)
			msg := "Suggesting a command…"
			if v.State == sugError {
				msg = v.Message
			}
			ui.Row(c).FillWidth().Padding(space1_5, space3, 0, space3).Gap(space2).Children(func() {
				if v.State == sugLoading {
					workingBlocks(c, inkMuted, "Suggesting a command")
				}
				wrapText(c, msg, inkMuted).Shrink(1).MinWidth(0)
			})
			foot(keep, "")
		default:
			head(sourceLabel(opt.Source), true)
			ui.Box(c).FillWidth().Padding(space1_5, space3, 0, space3).Children(func() {
				ui.Box(c).FillWidth().Padding(space2, space3).Background(stage).Border(1, line).Radius(radiusSm).Label("suggested command").Children(func() {
					wrapText(c, opt.Command, ink).Font(monoStack)
				})
			})
			if opt.Why != "" {
				ui.Box(c).FillWidth().Padding(space1_5, space3, 0, space3).Children(func() { wrapText(c, opt.Why, inkMuted) })
			}
			if opt.Placeholders {
				ui.Box(c).FillWidth().Padding(space1_5, space3, 0, space3).Children(func() {
					wrapText(c, "Holds a redacted value such as [secret:1]. Replace it after inserting.", inkMuted)
				})
			}
			if v.Note != "" {
				ui.Box(c).FillWidth().Padding(space1_5, space3, 0, space3).Label("suggestion note").Children(func() { wrapText(c, v.Note, signal) })
			}
			if len(v.Sug.Options) > 1 {
				ui.Column(c).FillWidth().Padding(space1_5, space1_5, 0, space1_5).Children(func() {
					for i, o := range v.Sug.Options {
						if i == v.Selected {
							continue
						}
						row := ui.Row(c).FillWidth().Height(sizeControlSm).Padding(0, space1_5).Gap(space2).AlignItems(ui.Center).Radius(radiusXs).Cursor(ui.CursorPointer).Label("suggestion option " + string(rune('1'+i)))
						if row.Hovered() {
							row.Background(hoverGround)
						}
						if row.Clicked() {
							w.act(func() { p.sg.m.Select(i) })
						}
						row.Children(func() { kbd(c, string(rune('1'+i))); mono(c, o.Command, inkMuted).MinWidth(0) })
					}
				})
			}
			rule := ""
			if opt.NeedsShift {
				rule = "Production host: " + k.Tab + " alone does not insert a state change"
				if danger {
					rule = "Plain " + k.Tab + " never inserts a destructive command"
				}
			}
			foot(func() {
				if opt.NeedsShift {
					ui.Row(c).Gap(space1).AlignItems(ui.Center).Children(func() { kbd(c, k.Shift); kbd(c, k.Tab); caption(c, "insert anyway", inkFaint) })
				} else {
					ui.Row(c).Gap(space1).AlignItems(ui.Center).Children(func() { kbd(c, k.Tab); caption(c, "insert", inkFaint) })
				}
				if len(v.Sug.Options) > 1 {
					ui.Row(c).Gap(space1).AlignItems(ui.Center).Children(func() { kbd(c, k.Up); kbd(c, k.Down); caption(c, "other options", inkFaint) })
				}
				keep()
			}, rule)
		}
	})
	if h := p.sg.card.Bounds(c).H; h > 0 {
		p.sg.cardH = h
	}
}

// wrapText is body text that wraps in its box.
func wrapText(c *ui.Context, s string, col ui.Color) ui.Element {
	return ui.Text(c, s).TextColor(col).FontSize(uiFontSize).FontWeight(weightRegular)
}
