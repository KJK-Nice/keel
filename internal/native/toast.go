package native

import (
	"errors"
	"time"

	"github.com/egoist/mygo/ui"

	"slime/internal/native/slimeclient"
)

// Toasts sit at the bottom right, show the newest three and keep each for 5 s. MyGo keeps the queue, the timers (they
// stop while the pointer rests on a toast) and the limit of three; this draws them with the Slime tokens, as Rex's: a
// pill with a status icon (a tide check for a result, a flare alert for a failure), the title, an optional muted
// second line, an optional action button that runs and closes the toast, and a round close button. A result's
// title that does not fit scrolls sideways on one line, as Rex's does (marquee); with reduced motion, and on a
// failure, it wraps instead, so it can be read at a glance.

const (
	toastTimeout = 5 * time.Second // Toaster's default timeout
	toastWidth   = 420             // the widest a toast grows; a short one is as wide as its text
	toastError   = "error"
	toastSuccess = "success"
	toastIcon    = 20 // the status icon's round

	marqueeWait  = 1500 * time.Millisecond // still at each end before it moves on
	marqueeSpeed = 40                      // DIPs a second
)

// marquee is the state of one scrolling title: how long it has moved, which stops while the pointer rests on
// the toast, as its timer does.
type marquee struct {
	elapsed time.Duration
	last    time.Time
}

// marqueeOffset is how far left a title overflow DIPs too wide is drawn after elapsed: still for marqueeWait,
// across at marqueeSpeed, still at the end for marqueeWait, then back to the start and round again.
func marqueeOffset(elapsed time.Duration, overflow float32) float32 {
	if overflow <= 0 {
		return 0
	}
	move := time.Duration(float64(overflow) / marqueeSpeed * float64(time.Second))
	t := elapsed % (2*marqueeWait + move)
	switch {
	case t < marqueeWait:
		return 0
	case t < marqueeWait+move:
		return -overflow * float32(t-marqueeWait) / float32(move)
	}
	return -overflow
}

// say shows a result (toast(message)).
func (w *workspace) say(msg string) {
	w.queued = append(w.queued, ui.Toast{ID: msg, Title: msg, Type: toastSuccess, Timeout: toastTimeout})
}

// warn shows a failure (toast(message, true)).
func (w *workspace) warn(msg string) {
	w.queued = append(w.queued, ui.Toast{ID: toastError + msg, Title: msg, Type: toastError, Timeout: toastTimeout})
}

// sayAction shows msg with one button that runs and closes the toast.
func (w *workspace) sayAction(msg, action string, run func()) {
	w.sayDetail(msg, "", action, run, toastTimeout)
}

// sayDetail shows a result with a second line (detail, may be empty) and one button (action, may be empty) for timeout;
// a negative timeout keeps it until it is closed.
func (w *workspace) sayDetail(title, detail, action string, run func(), timeout time.Duration) {
	w.queued = append(w.queued, ui.Toast{ID: toastActionID(title), Title: title, Description: detail, Type: toastSuccess,
		Timeout: timeout, Action: action, OnAction: run})
}

func toastActionID(title string) string { return "action " + title }

// errText is what a toast says for a failed call: the server's own reason, else what failed and why.
func errText(what string, err error) string {
	var se *slimeclient.StatusError
	if errors.As(err, &se) && se.Msg != "" {
		return se.Msg
	}
	return what + ": " + err.Error()
}

// toastsView hands the queued toasts to MyGo and draws the viewport, once per frame.
func (w *workspace) toastsView(c *ui.Context) {
	for _, t := range w.queued {
		c.AddToast(t)
	}
	w.queued = nil
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(space4).AlignItems(ui.End).Gap(space2)
		showing := map[string]bool{}
		for _, t := range toasts {
			showing[t.ID] = true
		}
		for id := range w.marquees {
			if !showing[id] {
				delete(w.marquees, id)
			}
		}
		for _, t := range toasts {
			ok := t.Type == toastSuccess
			titleCol := ink
			if ok {
				titleCol = tide
			}
			p := ui.ToastBase(c, t)
			p.Root.Row().MaxWidth(toastWidth).Padding(space2, space2, space2, space3).Gap(space2).AlignItems(ui.Center).
				Radius(radiusFull).Background(raised).Border(1, line).Shadow(0, 8, 24, 0, shadowPop).Label("toast " + t.Title)
			p.Root.Children(func() {
				switch t.Type {
				case toastSuccess:
					ui.Row(c).Shrink(0).Size(toastIcon, toastIcon).Radius(radiusFull).Background(tide).Justify(ui.Center).
						AlignItems(ui.Center).Children(func() { iconSized(c, "tick", raised, iconSize-4) })
				case toastError:
					ui.Row(c).Shrink(0).Children(func() { icon(c, "alert", flare) })
				}
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					if ok && !c.Preferences().ReduceMotion {
						w.marqueeTitle(c, t, titleCol, p.Root.Hovered())
					} else {
						ui.Text(c, t.Title).TextColor(titleCol).FontSize(uiFontSize).FontWeight(weightSemibold)
					}
					if t.Description != "" {
						caption(c, t.Description, inkMuted)
					}
				})
				if t.Action != "" {
					b := p.ActionButton().Label(t.Action).Shrink(0).Height(sizeControlSm).Padding(0, space3).Radius(radiusFull)
					if ok {
						b.Background(tideSoft).Children(func() { label(c, t.Action, tide) })
					} else {
						b.Background(raised).Border(1, lineStrong).Children(func() { label(c, t.Action, ink) })
					}
				}
				p.CloseButton().Label("Close notification").Size(sizeControlSm, sizeControlSm).Radius(radiusFull).Justify(ui.Center).
					AlignItems(ui.Center).Shrink(0).Children(func() { icon(c, "cancel-01", inkMuted) })
			})
		}
	})
}

// marqueeTitle draws t's title on one line in a clipped box and, when it is wider than the box, scrolls it
// (marqueeOffset), asking for frames while it does; hovered holds it where it is.
func (w *workspace) marqueeTitle(c *ui.Context, t ui.Toast, col ui.Color, hovered bool) {
	box := ui.Row(c).ClipX().Grow(1).Shrink(1).MinWidth(0)
	textW, _ := c.MeasureText(0, ui.Span{Text: t.Title, Size: uiFontSize, Weight: weightSemibold})
	overflow := textW - box.Bounds().W // the box's width from the previous frame; none yet on the first
	var off float32
	if box.Bounds().W > 0 && overflow > 0 {
		if w.marquees == nil {
			w.marquees = map[string]*marquee{}
		}
		m := w.marquees[t.ID]
		now := c.Now()
		if m == nil {
			m = &marquee{last: now}
			w.marquees[t.ID] = m
		}
		if !hovered {
			m.elapsed += now.Sub(m.last)
		}
		m.last = now
		off = marqueeOffset(m.elapsed, overflow)
		c.AnimationFrame()
	}
	box.Children(func() {
		ui.Text(c, t.Title).NoWrap().Shrink(0).Translate(off, 0).TextColor(col).FontSize(uiFontSize).FontWeight(weightSemibold)
	})
}
