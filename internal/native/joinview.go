package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
	"keel/internal/wire"
)

// joinDialogView is the Join a session dialog.
func (w *workspace) joinDialogView(c *ui.Context) {
	d := w.joinDlg
	if d == nil {
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		modalChrome(c, backdrop, panel)
		panel.Width(dialogWidth).Padding(space3).Gap(space2).Label("dialog " + joinTitle)
		bodyStrong(c, joinTitle, ink)
		ui.Text(c, joinDesc).TextColor(inkMuted).FontSize(captionSize)
		field(c, "Join link", &d.link, "https://…/join?room=…", false, &d.h, func(k ui.TextKey) bool {
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				w.act(w.submitJoin)
				return true
			}
			return false
		})
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			if textBtn(c, "Join", btnPrimary) {
				w.act(w.submitJoin)
			}
		})
	})
	if !d.focus && w.joinDlg == d {
		d.h.Focus(c)
		d.focus = true
	}
	if !open {
		w.act(w.closeJoin)
	}
}

// joinBanner is the haze banner over a joined tab (§5.7): whose session on which host, read-only
// or driving, Ask for control and Leave. Before the host has said who this client is it says it is waiting. A join that
// failed shows its error and Leave.
func (w *workspace) joinBanner(c *ui.Context, t *tab, p *pane, st keelclient.State) {
	leave := func() {
		if textBtn(c, "Leave", btnGhost) {
			w.act(func() { w.closeTab(t) })
		}
	}
	if st.Status == wire.StatusError {
		w.bannerRow(c, "banner failed", flareSoft, flare, st.Message, "", "", leave)
		return
	}
	if st.YouID == 0 {
		w.bannerRow(c, "banner joining", hazeSoft, haze, "Joining a shared session · waiting for the host", "", "", leave)
		return
	}
	owner, _, driving, host := followState(st)
	lead, tail := "Following", "read-only"
	if driving {
		lead, tail = "Driving", "you have control"
	}
	head := lead + " " + owner + "'s session on " + host + " · " + tail
	w.bannerRow(c, "banner "+head, hazeSoft, haze, head, "", "", func() {
		if !driving && textBtnIcon(c, "steering", "Ask for control", btnDefault) {
			w.act(func() { w.sendControl(p.conn.RequestControl, "ask for control") })
		}
		leave()
	})
}

// joinChips are the joined pane bar's chips: who drives, and "You follow".
func joinChips(c *ui.Context, st keelclient.State) {
	if st.YouID == 0 {
		return
	}
	_, driver, driving, _ := followState(st)
	text := "Nobody is driving"
	switch {
	case driving:
		text = "Driving"
	case driver != "":
		text = driver + " is driving"
	}
	chip(c, text, driving)
	if !driving {
		chipFollow(c, "You follow")
	}
}
