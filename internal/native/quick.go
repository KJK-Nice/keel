package native

import (
	"context"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/native/sshparse"
)

// Quick connect, and "Connect once" on the New tab page, which is the same dialog (term.js openQuick,
// saveQuick; quickDialog in internal/ui/dialogs.templ). It saves a password host under the group "Quick
// connect" with POST /api/hosts, as the webview does, and opens a session on it; the host key dialog and the
// failure banner then work as for any host. The password is typed here, never read from the pasted command.

type quickDialog struct {
	target, password string
	err              string
	notes            *sshparse.Result // the pasted command it was opened from: its Left out and key-file notes
	focus            bool
	targetH, passH   ui.Handle
}

// openQuick opens the dialog with a target, and notes of the pasted command when it came from one.
func (w *workspace) openQuick(target string, notes *sshparse.Result) {
	w.quick = &quickDialog{target: target, notes: notes}
}

func (w *workspace) closeQuick() {
	w.quick = nil
	w.returnKeyboard(true)
}

// quickColor is the colour the webview gives a quick host.
const quickColor = "var(--ansi-12)"

// submitQuick saves the host and opens it. The target must read as user@host (or user@host:port): the same
// parser as the New tab page, which never runs or resolves anything.
func (w *workspace) submitQuick() {
	q := w.quick
	if q == nil {
		return
	}
	r := sshparse.ParseConnect(q.target)
	// A typed ssh command with options Keel would drop is not a target: nothing is connected on a guess.
	if r == nil || !r.OK || r.User == "" || len(r.Dropped) > 0 {
		q.err = "Could not parse user@host"
		return
	}
	h := keelclient.NewHost{
		Name: r.User + "@" + r.Host, Group: "Quick connect", Address: r.Host, Port: r.Port, Username: r.User,
		AuthType: "password", Password: q.password, Color: quickColor,
	}
	w.quick = nil // the password leaves the dialog with the request
	call(w, func(c context.Context) (keelclient.Host, error) { return w.be.CreateHost(c, h) }, func(saved keelclient.Host, err error) {
		if err != nil {
			w.fail("quick connect", err)
			return
		}
		w.hosts = append(w.hosts, saved)
		w.openHost(saved)
	})
}

// quickDialogView is the dialog.
func (w *workspace) quickDialogView(c *ui.Context) {
	q := w.quick
	if q == nil {
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space3).Gap(space2).Label("dialog Quick connect")
		bodyStrong(c, "Quick connect", ink)
		ui.Text(c, "Paste user@host or user@host:2222. Saved under Quick connect.").TextColor(inkMuted).FontSize(captionSize)
		if q.notes != nil {
			w.notesView(c, q.notes)
		}
		submit := func(k ui.TextKey) bool {
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				w.act(w.submitQuick)
				return true
			}
			return false
		}
		field(c, "Target", &q.target, "ubuntu@192.168.1.20:22", false, &q.targetH, submit)
		field(c, "Password", &q.password, "", true, &q.passH, submit)
		if q.err != "" {
			caption(c, q.err, flare)
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			if textBtn(c, "Connect", btnPrimary) {
				w.act(w.submitQuick)
			}
		})
	})
	if !q.focus && w.quick == q {
		if q.target == "" {
			q.targetH.Focus(c)
		} else {
			q.passH.Focus(c)
		}
		q.focus = true
	}
	if !open {
		w.act(w.closeQuick)
	}
}

// field is a labelled single-line input in a dialog: the label above, the box below (as the sidebar's filter).
func field(c *ui.Context, name string, value *string, placeholder string, password bool, h *ui.Handle, onKey func(ui.TextKey) bool) {
	ui.Column(c).FillWidth().Gap(space1).Children(func() {
		label(c, name, inkMuted)
		ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
			in := ui.TextInputBase(c, value).Label(name).Placeholder(placeholder).PlaceholderColor(inkFaint).TextColor(ink).
				FontSize(uiFontSize).Grow(1).MinWidth(0).Bind(h)
			if password {
				in.Password()
			}
			in.OnTextKey(onKey)
		})
	})
}
