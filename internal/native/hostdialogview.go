package native

import (
	"github.com/egoist/mygo/ui"
)

// The host dialog, drawn as hostDialog in internal/ui/dialogs.templ (760px, two columns: General and Connection;
// Authentication, Session and Agent; the footer) with the .k-host-* rules of keel.css. "Notes for the agent"
// (§15) waits for a later phase (docs/native.md).

// modalOpen reports a dialog that has the keyboard: no terminal or page input takes focus while one shows.
func (w *workspace) modalOpen() bool {
	return w.pal != nil || w.quick != nil || w.joinDlg != nil || w.hostDlg != nil || w.identDlg != nil || w.confirm != nil || w.approve != nil || w.snipDlg != nil || w.nbDlg != nil || w.folderDlg != nil || w.fwdDlg != nil || w.fwdKey != nil
}

// inputBox is a single-line input in a bordered box (k-input).
func inputBox(c *ui.Context, name string, value *string, placeholder string, h *ui.Handle, mono, masked bool, maxLen int, onKey func(ui.TextKey) bool) {
	in := ui.TextInput(c, value).Label(name).Placeholder(placeholder).FillWidth().MinWidth(0).Bind(h)
	if mono {
		in.Font(monoStack)
	}
	if masked {
		in.Password()
	}
	if maxLen > 0 {
		in.MaxLength(maxLen)
	}
	if onKey != nil {
		in.OnTextKey(onKey)
	}
}

// labeled is a k-field: the label above, the control below. Clicking the label gives h (when there is one) the
// keyboard, as a <label for> does.
func labeled(c *ui.Context, name string, h *ui.Handle, control func()) {
	ui.Column(c).FillWidth().MinWidth(0).Gap(space1).Children(func() {
		r := ui.Row(c).Cursor(ui.CursorPointer)
		r.Children(func() { label(c, name, inkMuted) })
		if h != nil && r.Clicked() {
			h.Focus(c)
		}
		control()
	})
}

func help(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(inkMuted).FontSize(captionSize)
}

// menuLabel is a k-menu-label: a group heading.
func menuLabel(c *ui.Context, s string) { text(c, s, inkFaint, captionSize, weightSemibold) }

// picker is a select: the chosen label and a chevron; the menu lists the entries (their second line is muted).
func picker(c *ui.Context, name, current string, entries []pickEntry, onPick func(string)) {
	shown := ""
	for _, e := range entries {
		if e.value == current {
			shown = e.label
		}
	}
	b := ui.Button(c, "").FillWidth().Justify(ui.Start).
		Label(name + " picker") // not the label's own name: a click by name finds the label first
	b.Children(func() {
		ui.Text(c, shown).SingleLine().Grow(1).MinWidth(0)
		icon(c, "chevron-down", inkMuted)
	})
	b.PopoverMenuButton(func(m *ui.Menu) {
		for _, e := range entries {
			it := m.Item(e.label).Radio(e.value == current)
			if e.desc != "" {
				it.Subtitle(e.desc)
			}
			if it.Chosen() {
				v := e.value
				onPick(v)
			}
		}
	})
}

// segControl is k-seg: one option is on.
func segControl(c *ui.Context, name string, current string, opts [][2]string, onPick func(string)) {
	labels := make([]string, len(opts))
	at := 0
	for i, o := range opts {
		labels[i] = o[1]
		if o[0] == current {
			at = i
		}
	}
	idx := at
	// ui.SegmentedBase: MyGo's segmented control with a tighter segment, so four labels fit the sidebar at 13 pt.
	seg := ui.SegmentedBase(c, &idx, len(opts))
	t := c.Theme()
	seg.Track.FillWidth().AlignItems(ui.Stretch).Padding(2).Gap(2).Radius(t.Radius).Border(1, t.Border).Label(name).Children(func() {
		for i, l := range labels {
			s := seg.Segment(i).Grow(1).Shrink(1).MinWidth(0).Padding(space1, space1_5).Radius(max(t.Radius-2, 0)).Justify(ui.Center).Label(l).KeepFocus()
			if i == idx {
				s.Background(hoverGround).Border(1, t.Border)
			}
			s.Children(func() { ui.Text(c, l).SingleLine().TextColor(map[bool]ui.Color{true: ink, false: inkMuted}[i == idx]) })
		}
	})
	if idx != at {
		onPick(opts[idx][0])
	}
}

// switchRow is a k-field--row: the text on the left, the switch on the right.
func switchRow(c *ui.Context, name, note string, on *bool) {
	ui.Row(c).FillWidth().Gap(space3).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			body(c, name, ink).MaxLines(0)
			help(c, note)
		})
		ui.Switch(c, on).Label(name).Shrink(0)
	})
}

func (w *workspace) hostDialogView(c *ui.Context) {
	d := w.hostDlg
	if d == nil || d.confirm != nil { // the confirm dialog replaces it while it asks (term.js closes the modal first)
		return
	}
	d.dropStaleTest()
	title := "New host"
	if d.id != "" {
		title = "Edit host"
	}
	local := d.f.auth == "local"
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Width(hostDialogWidth).Background(raised).Border(1, line).Radius(radiusMd).Clip().Label("dialog " + title)
		// ⌘↵ saves from any field (§5.8, §8).
		panel.ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == ui.Cmd {
				w.act(w.saveHost)
			}
		})
		ui.Row(c).FillWidth().Padding(space4, space4, space2).Children(func() { text(c, title, ink, titleSize, weightSemibold) })
		if d.notes != nil {
			ui.Column(c).FillWidth().Padding(0, space4, space2).Children(func() { w.notesView(c, d.notes) })
		}
		ui.Scroll(c).FillWidth().MaxHeight(hostColsMax).Shrink(1).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Children(func() {
				hairline(ui.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(space4).Gap(space3), 0, 1, 0, 0).Children(func() { w.hostLeft(c, d, local) })
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(space4).Gap(space3).Children(func() { w.hostRight(c, d, local) })
			})
		})
		w.hostTestView(c, d)
		if d.err != "" {
			ui.Row(c).FillWidth().Padding(0, space4, space3).Label("host form error").Children(func() { caption(c, d.err, flare) })
		}
		hairline(ui.Row(c).FillWidth().Padding(space3, space4).Gap(space2), 1, 0, 0, 0).Children(func() { w.hostFooter(c, d, local) })
	})
	if d.focus && w.hostDlg == d {
		d.nameH.Focus(c)
		d.focus = false
	}
	if !open {
		w.act(w.closeHostDialog)
	}
}

func (w *workspace) hostLeft(c *ui.Context, d *hostDialog, local bool) {
	f := &d.f
	menuLabel(c, "General")
	labeled(c, "Label", &d.nameH, func() { inputBox(c, "Label", &f.name, "prod-api", &d.nameH, false, false, 0, nil) })
	labeled(c, "Folder", nil, func() {
		picker(c, "Folder", f.groupID, folderEntries(d.groups), d.setFolder)
	})
	labeled(c, "Color", nil, func() {
		ui.Row(c).Gap(space1_5).Wrap().Label("color").Children(func() {
			for _, sw := range hostColors {
				r := ui.Box(c).Size(14, 14).Radius(radiusFull).Background(hostTint(sw.value)).Label("color " + sw.name).Tooltip(sw.name).Cursor(ui.CursorPointer)
				if f.color == sw.value {
					r.Shadow(0, 0, 0, 2, ink).Checked(true)
				} else {
					r.Border(1, lineStrong)
				}
				if r.Clicked() {
					v := sw.value
					w.act(func() { f.color = v })
				}
			}
		})
	})
	labeled(c, "Tags", &d.tagsH, func() { inputBox(c, "Tags", &f.tags, "prod, gpu", &d.tagsH, false, false, 0, nil) })
	menuLabel(c, "Connection")
	ui.Row(c).FillWidth().Gap(space2).Children(func() {
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Children(func() {
			labeled(c, "Hostname / IP", &d.addrH, func() { inputBox(c, "Hostname / IP", &f.address, "10.0.0.12", &d.addrH, false, false, 0, nil) })
		})
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
			labeled(c, "Port", &d.portH, func() { inputBox(c, "Port", &f.port, "22", &d.portH, false, false, 5, nil) })
		})
	})
	labeled(c, "Username", &d.userH, func() { inputBox(c, "Username", &f.username, "ubuntu", &d.userH, false, false, 0, nil) })
	if !local {
		ui.Column(c).FillWidth().Gap(space1).Children(func() {
			label(c, "Jump host", inkMuted)
			picker(c, "Jump host", f.jump, jumpEntries(w.hosts, d.id, f.jump), func(v string) { f.jump = v })
			help(c, "Connects to this saved host first, with its own key or password from the vault. One hop.")
		})
	}
}

func (w *workspace) hostRight(c *ui.Context, d *hostDialog, local bool) {
	f := &d.f
	menuLabel(c, "Authentication")
	labeled(c, "Authentication", nil, func() {
		opts := make([][2]string, len(authKinds))
		for i, a := range authKinds {
			opts[i] = [2]string{a.value, a.label}
		}
		segControl(c, "Authentication", f.auth, opts, d.setAuth)
	})
	switch f.auth {
	case "identity":
		labeled(c, "Identity", nil, func() {
			entries := identityEntries(d.idents, f.identityID)
			if len(entries) == 0 {
				ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Label("Identity").Children(func() {
					body(c, "No identities — add in Keychain", inkFaint)
				})
				return
			}
			picker(c, "Identity", f.identityID, entries, func(v string) { f.identityID = v })
		})
	case "password":
		labeled(c, "Password", &d.passH, func() {
			inputBox(c, "Host password", &f.password, "Leave blank to keep saved password", &d.passH, false, true, 0, nil)
		})
	case "key":
		// A text area has no mask (Password is ignored on it), and the webview's shows the key too: it is cleared
		// when the dialog closes or the request is sent.
		labeled(c, "Private key", &d.keyH, func() {
			ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
				ui.TextAreaBase(c, &f.privateKey).Label("Private key").Placeholder("-----BEGIN OPENSSH PRIVATE KEY-----").PlaceholderColor(inkFaint).
					TextColor(ink).FontSize(captionSize).Font(monoStack).Lines(6, 6).FillWidth().Bind(&d.keyH)
			})
		})
		labeled(c, "Key passphrase", &d.phraseH, func() { inputBox(c, "Key passphrase", &f.passphrase, "Optional", &d.phraseH, false, true, 0, nil) })
	}
	if !local {
		w.hostKeyBlock(c, d)
		menuLabel(c, "Session")
		ui.Column(c).FillWidth().Gap(space1).Children(func() {
			labeled(c, "Startup command", &d.startH, func() {})
			inputBox(c, "Startup command", &f.startup, "tmux new -A -s main", &d.startH, true, false, maxStartup, nil)
			help(c, "Typed once with Enter when a session opens. Not typed again on reconnect, nor for people who join. Do not put secrets here.")
		})
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Justify(ui.End).Children(func() {
				ui.Checkbox(c, &f.keepAlive, "Keepalive")
			})
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				labeled(c, "Keepalive interval (sec)", &d.keepH, func() {
					if f.keepAlive {
						inputBox(c, "Keepalive interval (sec)", &f.keepSec, "30", &d.keepH, false, false, 3, nil)
					} else {
						ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Radius(radiusSm).Border(1, line).Label("Keepalive interval (sec)").Opacity(dimAlpha).Children(func() {
							body(c, f.keepSec, inkFaint)
						})
					}
				})
			})
		})
		ui.Checkbox(c, &f.autoReconnect, "Auto reconnect")
	}
	menuLabel(c, "Agent")
	switchRow(c, "Agent can read this host's panes", "You still choose panes per conversation.", &f.agentReadable)
	switchRow(c, "Treat as production", "Suggestions that change state need ⇧⇥ and show a red edge. On by default in a group named production.", &f.production)
	switchRow(c, "Let the agent propose notes", "It asks first. Nothing is saved until you press Save.", &f.agentNotes)
	if d.nb != nil {
		w.notesBlock(c, d.nb, notesBlockOpts{title: "Notes for the agent", placeholder: "Add a note, e.g. Backups run at 02:00 from pgbackrest", host: true, readable: f.agentReadable,
			help: "Sent to the agent only when this host is in scope and Agent can read is on. Redacted like pane text. Stored encrypted in the vault."})
	}
}

// hostKeyBlock is the host key line: the saved key for the host's address:port, and Forget host key.
func (w *workspace) hostKeyBlock(c *ui.Context, d *hostDialog) {
	known := d.key != nil && d.key.Known
	ui.Column(c).FillWidth().Gap(space1).Label("host key").Children(func() {
		label(c, "Host key", inkMuted)
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			badge(c, known)
			if known && d.key.KeyType != "" {
				ui.Row(c).Height(space4).Padding(0, space1).Radius(radiusXs).Border(1, lineStrong).Children(func() {
					mono(c, keyTypeLabel(d.key.KeyType), inkMuted)
				})
			}
			if known && textBtn(c, "Forget host key", btnGhost) {
				w.act(w.askForgetKey)
			}
		})
		if known {
			ui.Text(c, d.key.Fingerprint).Font(monoStack).TextColor(inkMuted).FontSize(captionSize) // wraps: it is long
			help(c, "Saved for "+d.key.Hostname+". Keel refuses a different key until you replace it.")
		} else {
			help(c, "Trusted on first connect; Keel asks again if it changes.")
		}
	})
}

// badge is the host key badge: Trusted (tide) or Not saved (outline).
func badge(c *ui.Context, trusted bool) {
	r := ui.Row(c).Height(space4).Padding(0, space1_5).Radius(radiusXs).Justify(ui.Center).Shrink(0)
	s, fg := "Not saved", inkMuted
	if trusted {
		s, fg = "Trusted", tide
		r.Background(tideSoft)
	} else {
		r.Border(1, lineStrong)
	}
	r.Label("host key " + s).Children(func() { text(c, s, fg, kbdSize, weightSemibold) })
}

// hostTestView is the result under the columns: its explanation, an offered key and the trust button.
func (w *workspace) hostTestView(c *ui.Context, d *hostDialog) {
	t := &d.test
	if t.detail == "" {
		return
	}
	ui.Column(c).Padding(space3).Gap(space2).Margin(0, space4, space3).Radius(radiusMd).Background(surface).Border(1, line).Label("test result").Children(func() {
		ui.Text(c, t.detail).TextColor(inkMuted).FontSize(uiFontSize)
		if t.res == nil {
			return
		}
		if fp := fingerprintText(*t.res); fp != "" {
			ui.Text(c, fp).Font(monoStack).TextColor(inkMuted).FontSize(captionSize)
		}
		if _, ok := t.res.HostKeyOffer(); ok {
			name := "Trust and save"
			if t.res.Kind == "host-key-mismatch" {
				name = "Replace saved key"
			}
			ui.Row(c).Children(func() {
				if textBtn(c, name, btnDefault) {
					w.act(w.trustTest)
				}
			})
		}
	})
}

func (w *workspace) hostFooter(c *ui.Context, d *hostDialog, local bool) {
	if d.id != "" && !local && d.saved != nil {
		if textBtn(c, "Delete host", btnDanger) {
			h := *d.saved
			w.act(func() { w.askDeleteHost(h) })
		}
	}
	ui.Spacer(c)
	if t := &d.test; t.short != "" {
		ui.Row(c).Gap(space1_5).Label("test state").Children(func() {
			stateDot(c, t.state)
			caption(c, t.short, inkMuted)
		})
	}
	if !local {
		name := "Test connection"
		if d.test.running {
			name = "Cancel test"
		}
		if textBtn(c, name, btnDefault) {
			w.act(w.runHostTest)
		}
	}
	if textBtn(c, "Cancel", btnGhost) {
		w.act(w.closeHostDialog)
	}
	r := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Justify(ui.Center).Shrink(0).Background(brass).Label("Save host").Cursor(ui.CursorPointer)
	if r.Hovered() {
		r.Background(brassStrong)
	}
	r.Children(func() {
		label(c, "Save host", onBrass)
		text(c, "⌘↵", onBrass, kbdSize, weightMedium).Font(monoStack)
	})
	if r.Clicked() {
		w.act(w.saveHost)
	}
}

// confirmView is the confirm dialog (alertdialog): a title, the message, Cancel and the action. It stands in for
// the host dialog while that asks, and by itself for the host menu's Delete.
func (w *workspace) confirmView(c *ui.Context) {
	var cf *hostConfirm
	standalone := false
	switch {
	case w.hostDlg != nil && w.hostDlg.confirm != nil:
		cf = w.hostDlg.confirm
	case w.confirm != nil:
		cf, standalone = w.confirm, true
	default:
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Background(raised).Border(1, line).Radius(radiusMd).Label("alertdialog " + cf.title)
		bodyStrong(c, cf.title, ink)
		ui.Text(c, cf.message).TextColor(inkMuted).FontSize(uiFontSize)
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			if textBtn(c, cf.action, btnDanger) {
				w.act(func() {
					w.endConfirm(standalone)
					cf.run()
				})
			}
		})
	})
	if !open {
		w.act(func() { w.endConfirm(standalone) })
	}
}

// endConfirm closes the confirm dialog; the host dialog under it comes back with its values.
func (w *workspace) endConfirm(standalone bool) {
	if standalone {
		w.confirm = nil
		return
	}
	if d := w.hostDlg; d != nil {
		d.confirm = nil
		d.focus = true
	}
}
