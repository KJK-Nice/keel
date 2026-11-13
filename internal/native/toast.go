package native

import (
	"errors"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Toasts: the results the webview shows with toast() (term.js, which calls tui.toast.add). The app's Toaster
// (components/toast/toast.templ) sits at the bottom right, shows the newest three, keeps each for 5 s, and draws a
// failure with a status icon. MyGo keeps the queue, the timers (they stop while the pointer rests on a toast) and
// the limit of three; this draws them with the Keel tokens. An action toast is the k-toast of actiontoast.js: a
// title and one button that runs and closes it.

const (
	toastTimeout = 5 * time.Second // Toaster's default timeout
	toastWidth   = 384             // max-w-sm of the toast viewport
	toastError   = "error"
)

// say shows a result (toast(message)).
func (w *workspace) say(msg string) {
	w.queued = append(w.queued, ui.Toast{ID: msg, Title: msg, Timeout: toastTimeout})
}

// warn shows a failure (toast(message, true)).
func (w *workspace) warn(msg string) {
	w.queued = append(w.queued, ui.Toast{ID: toastError + msg, Title: msg, Type: toastError, Timeout: toastTimeout})
}

// sayAction shows msg with one button that runs and closes the toast.
func (w *workspace) sayAction(msg, action string, run func()) {
	w.queued = append(w.queued, ui.Toast{ID: "action " + msg, Title: msg, Timeout: toastTimeout, Action: action, OnAction: run})
}

// errText is what a toast says for a failed call: the server's own reason, else what failed and why.
func errText(what string, err error) string {
	var se *keelclient.StatusError
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
		for _, t := range toasts {
			p := ui.ToastBase(c, t)
			p.Root.Row().Width(toastWidth).MaxWidth(toastWidth).Padding(space3).Gap(space2).AlignItems(ui.Start).
				Radius(radiusMd).Background(raised).Border(1, line).Shadow(0, 8, 24, 0, shadowPop).Label("toast " + t.Title)
			p.Root.Children(func() {
				if t.Type == toastError {
					ui.Row(c).Shrink(0).Children(func() { icon(c, "alert", flare) })
				}
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1_5).Children(func() {
					ui.Text(c, t.Title).TextColor(ink).FontSize(uiFontSize).FontWeight(weightSemibold)
					if t.Action != "" {
						b := p.ActionButton().Label(t.Action).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Background(raised).Border(1, lineStrong)
						b.Children(func() { label(c, t.Action, ink) })
					}
				})
				p.CloseButton().Label("Close notification").Size(sizeControlSm, sizeControlSm).Radius(radiusSm).Justify(ui.Center).Shrink(0).
					Children(func() { icon(c, "cancel-01", inkMuted) })
			})
		}
	})
}
