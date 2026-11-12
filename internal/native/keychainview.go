package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Keychain page drawn as KeychainPage and IdentityList (internal/ui) with .k-split, .k-item and .k-detail-* of
// keel.css, and the detail of lib/vaultview.js identityDetailHTML.

const ()

// splitHead is the list column's head: the title with its New button, and the filter.
func (w *workspace) splitHead(c *ui.Context, title, newLabel string, filter *string, filterLabel string, h *ui.Handle, onNew func()) {
	hairline(ui.Column(c).FillWidth().Shrink(0).Padding(space6, space4, space3).Gap(space3), 0, 0, 1, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			ui.Text(c, title).TextColor(ink).FontSize(titleLg).FontWeight(weightSemibold).Grow(1).MaxLines(1)
			if textBtnIcon(c, "add-01", newLabel, btnDefault) {
				w.act(onNew)
			}
		})
		ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Background(controlFill(stage)).Border(1, lineStrong).Children(func() {
			icon(c, "search-01", inkFaint)
			ui.TextInputBase(c, filter).Label(filterLabel).Placeholder(filterLabel).PlaceholderColor(inkFaint).TextColor(ink).FontSize(uiFontSize).Grow(1).MinWidth(0).Bind(h)
		})
	})
}

// textBtnIcon is a small k-btn with an icon before its name.
func textBtnIcon(c *ui.Context, glyph, name string, kind btnKind) bool {
	b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label(name).Cursor(ui.CursorPointer)
	bg, fg := raised, ink
	if b.Hovered() {
		bg = hoverGround
	}
	if kind == btnGhost {
		bg, fg = ui.Transparent, inkMuted
		if b.Hovered() {
			bg, fg = hoverGround, ink
		}
	}
	b.Background(bg)
	if kind == btnDefault {
		b.Border(1, lineStrong)
	}
	b.Children(func() {
		icon(c, glyph, fg)
		label(c, name, fg)
	})
	return b.Clicked()
}

func (w *workspace) keychainView(c *ui.Context, p *keychainPage) {
	cur, hasCur := p.current()
	ui.Row(c).Grow(1).FillWidth().FillHeight().Children(func() {
		hairline(ui.Column(c).Width(sizeList).Shrink(0).FillHeight().Background(w.ground(surface)), 0, 1, 0, 0).Children(func() {
			w.splitHead(c, "Keychain", "New identity", &p.filter, "Filter identities", &p.filterH, w.newIdentity)
			ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Label("identity list").Children(func() {
				w.identityList(c, p, cur.ID)
			})
		})
		ui.Scroll(c).Grow(1).FillHeight().Shrink(1).MinWidth(0).Background(w.ground(stage)).Label("identity detail").Children(func() {
			ui.Column(c).FillWidth().Padding(space6, space8).Gap(space4).Children(func() {
				switch {
				case p.err != "":
					ui.Text(c, p.err).TextColor(flare).FontSize(uiFontSize)
				case hasCur:
					w.identityDetail(c, cur)
				default:
					ui.Column(c).FillWidth().Padding(space8, 0).AlignItems(ui.Center).Children(func() {
						help(c, "Select an identity, or add one with New identity.")
					})
				}
			})
		})
	})
}

// identityList is the list column: SSH keys, then passwords.
func (w *workspace) identityList(c *ui.Context, p *keychainPage, current string) {
	ui.Column(c).FillWidth().Children(func() {
		if len(p.idents) == 0 {
			if p.loaded {
				sideEmpty(c, "No identities yet", "Add an SSH key or a password and attach it to hosts.")
			}
			return
		}
		shown := p.shown()
		if len(shown) == 0 {
			sideEmpty(c, "No identities match", "Clear the filter.")
			return
		}
		for _, g := range []struct{ kind, name string }{{"key", "SSH keys"}, {"password", "Passwords"}} {
			ids := identitiesOfKind(shown, g.kind)
			if len(ids) == 0 {
				continue
			}
			ui.Row(c).FillWidth().Padding(space3, space4, space1).Children(func() { menuLabel(c, g.name) })
			for _, id := range ids {
				w.identityRow(c, p, id, id.ID == current)
			}
		}
	})
}

func (w *workspace) identityRow(c *ui.Context, p *keychainPage, id keelclient.Identity, selected bool) {
	r := hairline(ui.Row(c).FillWidth().Padding(space2, space4).Gap(space3).Cursor(ui.CursorPointer).Key(id.ID).
		Label("identity "+id.Name).Selected(selected), 0, 0, 1, 0)
	hover := r.Hovered()
	if selected || hover {
		r.Background(hoverGround)
	}
	r.PopoverMenu(w.identityMenu(id))
	r.Children(func() {
		tileIcon := "key-01"
		if identityKind(id) != "key" {
			tileIcon = "square-lock-password"
		}
		ui.Row(c).Size(sizeControl, sizeControl).Radius(radiusSm).Justify(ui.Center).Shrink(0).Background(inkMuted.Alpha(tileTintAlpha)).Children(func() {
			iconSized(c, tileIcon, inkMuted, iconSize-space0_5)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			bodyStrong(c, id.Name, ink).MinWidth(0)
			text(c, identitySubtitle(w.hosts, id), inkMuted, itemSub, weightRegular).Font(monoStack).MinWidth(0)
		})
		switch {
		case hover:
			iconMenuBtn(c, "more-horizontal", "Actions for "+id.Name, sizeControlSm, w.identityMenu(id))
		case identityKind(id) == "key" && id.HasPassphrase:
			tag(c, "passphrase", hoverGround, inkMuted, false)
		}
	})
	if r.Clicked() {
		w.act(func() { p.selected = id.ID })
	}
}

// identityMenu is the row menu (rowmenus.templ identityRowMenu): Edit and Delete. Nothing in it shows or copies a
// secret, and an identity a host still uses cannot be deleted.
func (w *workspace) identityMenu(id keelclient.Identity) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("Edit").Chosen() {
			w.act(func() { w.editIdentity(id) })
		}
		m.Separator()
		inUse := len(identityHostsOf(w.hosts, id.ID)) > 0
		if m.Item("Delete").Disabled(inUse).Chosen() {
			w.act(func() { w.askDeleteIdentity(id) })
		}
	}
}

// identityDetail is the selected identity: what is stored, never the secret.
func (w *workspace) identityDetail(c *ui.Context, id keelclient.Identity) {
	kind := identityKind(id)
	used := identityHostsOf(w.hosts, id.ID)
	sub := identityKindLabel(id)
	if d := dateLabel(id); d != "" {
		sub += " · added " + d
	}
	tileIcon := "key-01"
	if kind != "key" {
		tileIcon = "square-lock-password"
	}
	ui.Row(c).FillWidth().Gap(space3).Children(func() {
		ui.Row(c).Size(tileLgSize, tileLgSize).Radius(radiusMd).Justify(ui.Center).Shrink(0).Background(brassSoft).Children(func() {
			iconSized(c, tileIcon, brassInk, tileLgIcon)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			ui.Text(c, id.Name).TextColor(ink).FontSize(titleLg).FontWeight(weightSemibold)
			text(c, sub, inkMuted, itemSub, weightRegular).Font(monoStack)
		})
		if textBtn(c, "Edit", btnDefault) {
			w.act(func() { w.editIdentity(id) })
		}
	})
	box(c).Children(func() {
		user := id.Username
		if user == "" {
			user = "—"
		}
		kv(c, "Username", user, true)
		if kind == "key" {
			kv(c, "Private key", stored(id.HasPrivateKey, "Encrypted in the vault", "Not stored"), false)
			kv(c, "Passphrase", stored(id.HasPassphrase, "Stored in the vault", "None"), false)
		} else {
			kv(c, "Password", stored(id.HasPassword, "Encrypted in the vault", "Not stored"), false)
		}
	})
	ui.Column(c).FillWidth().Gap(space1).Children(func() {
		label(c, "Used by "+hostsWord(len(used)), inkMuted)
		if len(used) == 0 {
			help(c, "No host uses it yet. Pick it in a host's Authentication.")
			return
		}
		ui.Row(c).FillWidth().Wrap().Gap(space1_5).Children(func() {
			for _, h := range used {
				ui.Row(c).Height(space4).Padding(0, space1_5).Radius(radiusXs).Background(hoverGround).Justify(ui.Center).Label("used by " + h.Name).Children(func() {
					text(c, h.Name, inkMuted, kbdSize, weightRegular).Font(monoStack)
				})
			}
		})
	})
	inUse := len(used) > 0
	hairline(ui.Row(c).FillWidth().Padding(space2, 0, 0).Gap(space2), 1, 0, 0, 0).Children(func() {
		ui.Spacer(c)
		b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label("Delete identity").Cursor(ui.CursorPointer).
			Background(flareSoft)
		if inUse {
			b.Opacity(0.45).Disabled(true).Tooltip("Remove it from " + hostsWord(len(used)) + " first")
		} else if b.Hovered() {
			b.Background(flare)
		}
		fg := flare
		if b.Hovered() && !inUse {
			fg = onFlare
		}
		b.Children(func() { label(c, "Delete identity", fg) })
		if !inUse && b.Clicked() {
			w.act(func() { w.askDeleteIdentity(id) })
		}
	})
	if inUse {
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			help(c, "Remove it from "+hostsWord(len(used))+" before you can delete it.")
		})
	}
}

func stored(have bool, yes, no string) string {
	if have {
		return yes
	}
	return no
}

// ---- the identity dialog ----

func (w *workspace) identityDialogView(c *ui.Context) {
	d := w.identDlg
	if d == nil || w.confirm != nil {
		return
	}
	title := "New identity"
	if d.id != "" {
		title = "Edit identity"
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Width(dialogWidth).Padding(space4).Gap(space3).Background(raised).Border(1, line).Radius(radiusMd).Label("dialog " + title)
		panel.ObserveInput(func(ev ui.InputEvent) { // ⌘↵ saves from any field
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == ui.Cmd {
				w.act(w.saveIdentity)
			}
		})
		text(c, title, ink, titleSize, weightSemibold)
		f := &d.f
		labeled(c, "Name", &d.nameH, func() { inputBox(c, "Identity name", &f.name, "deploy key", &d.nameH, false, false, 0, nil) })
		labeled(c, "Username", &d.userH, func() { inputBox(c, "Identity username", &f.username, "ubuntu", &d.userH, false, false, 0, nil) })
		labeled(c, "Kind", nil, func() {
			segControl(c, "Kind", f.kind, [][2]string{{"password", "Password"}, {"key", "Private key"}}, func(v string) { f.kind = v })
		})
		if f.kind == "key" {
			// A text area has no mask, and the webview's shows the key too: it is cleared when the dialog closes or
			// the request is sent.
			labeled(c, "Private key", &d.keyH, func() {
				ui.Column(c).FillWidth().Padding(space1_5, space2).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
					ui.TextAreaBase(c, &f.privateKey).Label("Identity private key").Placeholder("-----BEGIN OPENSSH PRIVATE KEY-----").PlaceholderColor(inkFaint).
						TextColor(ink).FontSize(captionSize).Font(monoStack).Lines(6, 6).FillWidth().Bind(&d.keyH)
				})
			})
			labeled(c, "Key passphrase", &d.phraseH, func() {
				inputBox(c, "Identity key passphrase", &f.passphrase, "Optional", &d.phraseH, false, true, 0, nil)
			})
		} else {
			labeled(c, "Password", &d.passH, func() {
				inputBox(c, "Identity password", &f.password, "Leave blank to keep saved password", &d.passH, false, true, 0, nil)
			})
		}
		if d.err != "" {
			ui.Row(c).FillWidth().Label("identity form error").Children(func() { caption(c, d.err, flare) })
		}
		ui.Row(c).FillWidth().Gap(space2).Justify(ui.End).Children(func() {
			if textBtn(c, "Cancel", btnGhost) {
				open = false
			}
			if textBtn(c, "Save identity", btnPrimary) {
				w.act(w.saveIdentity)
			}
		})
	})
	if d.focus && w.identDlg == d {
		d.nameH.Focus(c)
		d.focus = false
	}
	if !open {
		w.act(w.closeIdentityDialog)
	}
}
