package native

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The First run screens, drawn as firstRunScreen and agentStep in internal/ui/gate.templ with the .k-onb-*,
// .k-choice-card, .k-imp-* and .k-promise rules of keel.css. The import table is lib/onboard.js's markup.

const (
	onbWidth     = sizeSidebar * 5 / 2 // .k-onb-col: size-sidebar · 2.5, inside its padding
	onbWideWidth = sizeSidebar * 3     // data-step="hosts": size-sidebar · 3
	stepDotSize  = 20
	kvKeyWidth   = space8*3 + space4 // .k-onb .k-kv first column
	impCheckCol  = space6 + space4   // the table's first column
	impRowHeight = sizeControlLg + space1_5
	impTableRows = 8
	maxPassLen   = 1024
)

// firstRunView is the window while the first run shows: the title bar's drag strip, the column and the status line.
func (w *workspace) firstRunView(c *ui.Context) {
	f := w.first
	hairline(ui.Row(c).FillWidth().Height(sizeTitlebar).Shrink(0).Background(w.ground(chrome)), 0, 0, 1, 0).DragWindow()
	width := float32(onbWidth)
	if f.step == stepHosts {
		width = onbWideWidth
	}
	ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Background(w.ground(stage)).Label("first run").Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(width + space8*2).Padding(space8).Gap(space6).Children(func() {
				if f.flow == flowFirstRun {
					w.stepper(c, f)
				}
				switch f.step {
				case stepVault:
					w.vaultStep(c, f)
				case stepHosts:
					w.hostsStep(c, f)
				default:
					w.agentStep(c, f)
				}
			})
		})
	})
	if f.footFn != nil {
		ui.Row(c).FillWidth().Shrink(0).Justify(ui.Center).Background(w.ground(stage)).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(width+space8*2).Padding(0, space8, space4).Children(func() {
				hairline(ui.Row(c).FillWidth().Padding(space2, 0, 0).Gap(space2), 1, 0, 0, 0).Children(f.footFn)
			})
		})
	}
	hairline(ui.Row(c).FillWidth().Height(statusbarSize).Shrink(0).Padding(0, space3).Gap(space4).Background(w.ground(chrome)), 1, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(space1_5).Children(func() {
			dot(c, tide)
			caption(c, gateSessions(f.vault.Sessions), inkMuted)
		})
	})
	w.firstRunFocus(c, f)
}

// firstRunFocus gives the keyboard to the field the flow asked for. A field that has just appeared cannot take
// it in the frame it is built in, so the request is repeated for a few frames until the field has it.
func (w *workspace) firstRunFocus(c *ui.Context, f *firstRun) {
	var h *ui.Handle
	switch want := f.want; {
	case want == "pass":
		h = &f.passH
	case want == "pass2":
		h = &f.pass2H
	case want == "agentkey":
		h = &f.agentKeyH
	case strings.HasPrefix(want, "key:"):
		h = f.handleFor(strings.TrimPrefix(want, "key:"))
	default:
		return
	}
	if h.FocusWithin(c) || f.wantTries >= 5 {
		f.want, f.wantTries = "", 0
		return
	}
	f.wantTries++
	h.Focus(c)
	c.Invalidate()
}

// stepper is the progress row: Vault, Hosts, Agent (k-steps).
func (w *workspace) stepper(c *ui.Context, f *firstRun) {
	ui.Row(c).Gap(space2 + space0_5).Label("Setup progress").Children(func() {
		for i, name := range []string{"Vault", "Hosts", "Agent"} {
			if i > 0 {
				ui.Box(c).Width(space8).Height(1).Shrink(0).Background(line)
			}
			col := inkFaint
			switch {
			case i == f.step:
				col = ink
			case i < f.step:
				col = inkMuted
			}
			ui.Row(c).Gap(space2).Label("step " + name).Children(func() {
				n := ui.Row(c).Size(stepDotSize, stepDotSize).Radius(radiusFull).Justify(ui.Center).Shrink(0)
				switch {
				case i == f.step:
					n.Border(1, brassInk)
					n.Children(func() { text(c, fmt.Sprint(i+1), brassInk, captionSize, weightMedium) })
				case i < f.step:
					n.Background(tideSoft)
					n.Children(func() { iconSized(c, "tick", tide, iconSizeSm+space1) })
				default:
					n.Border(1, line)
					n.Children(func() { text(c, fmt.Sprint(i+1), col, captionSize, weightMedium) })
				}
				text(c, name, col, captionSize, weightMedium)
			})
		}
	})
}

// head is k-gate-head: an optional mark, the title and the lead.
func head(c *ui.Context, withMark bool, title, lead string) {
	ui.Column(c).FillWidth().Gap(space1_5).Children(func() {
		if withMark {
			mark(c)
		}
		ui.Text(c, title).TextColor(ink).FontSize(displaySize).FontWeight(weightSemibold)
		if lead != "" {
			ui.Text(c, lead).TextColor(inkMuted).FontSize(uiFontSize)
		}
	})
}

// box is k-box: a surface with a hairline and a radius.
func box(c *ui.Context) ui.Element {
	return ui.Column(c).FillWidth().Padding(space4).Gap(space3).Radius(radiusMd).Background(surface).Border(1, line)
}

// kv is one row of a k-kv list.
func kv(c *ui.Context, key, value string, monospace bool) {
	ui.Row(c).FillWidth().Gap(space4).AlignItems(ui.Start).Children(func() {
		body(c, key, inkMuted).Width(kvKeyWidth).Shrink(0)
		v := ui.Text(c, value).TextColor(ink).FontSize(uiFontSize).Grow(1).Shrink(1).MinWidth(0)
		if monospace {
			v.Font(monoStack).FontSize(captionSize)
		}
	})
}

// tag is a k-badge: a small label on a soft ground, or outlined.
func tag(c *ui.Context, s string, bg, fg ui.Color, outline bool) {
	r := ui.Row(c).Height(space4).Padding(0, space1_5).Radius(radiusXs).Justify(ui.Center).Shrink(0).Label("badge " + s)
	if outline {
		r.Border(1, lineStrong)
	} else {
		r.Background(bg)
	}
	r.Children(func() { text(c, s, fg, kbdSize, weightSemibold) })
}

// foot is k-onb-foot: a hairline above the buttons. It stays at the bottom of the window while the step scrolls
// (the webview scrolls it with the step; a long import table would push the buttons out of sight).
func (f *firstRun) foot(kids func()) { f.footFn = kids }

// bigPrimary is k-btn--primary k-btn--lg with the ↵ key cap. It reports a click and ignores one while off.
func bigPrimary(c *ui.Context, name string, enabled bool) bool {
	b := ui.Row(c).Height(sizeControlLg).Padding(0, space3).Gap(space2).Radius(radiusSm).Justify(ui.Center).Shrink(0).
		Label(name).Cursor(ui.CursorPointer).Background(brass)
	if !enabled {
		b.Opacity(dimAlpha).Disabled(true)
	} else if b.Hovered() {
		b.Background(brassStrong)
	}
	b.Children(func() {
		bodyStrong(c, name, onBrass)
		text(c, "↵", onBrass, kbdSize, weightMedium).Font(monoStack)
	})
	return enabled && b.Clicked()
}

// radio is k-radio.
func radio(c *ui.Context, on bool) {
	r := ui.Row(c).Size(radioSize, radioSize).Radius(radiusFull).Justify(ui.Center).Shrink(0).Background(stage)
	if on {
		r.Border(1, brassInk)
		r.Children(func() { ui.Box(c).Size(radioDot, radioDot).Radius(radiusFull).Background(brassInk) })
		return
	}
	r.Border(1, lineStrong)
}

// choiceCard is button.k-choice-card: a radio, a title with an optional badge, and the description. It reports a click.
func choiceCard(c *ui.Context, name string, on, disabled bool, title, badgeText, desc string) bool {
	selected := on
	r := ui.RadioBase(c, &selected, true).Row().FillWidth().Padding(space3, space4).Gap(space3).AlignItems(ui.Start).Radius(radiusMd).Background(surface).Label(name)
	switch {
	case on:
		r.Border(1, focus).Shadow(0, 0, 0, 1, focus)
	case r.Hovered() && !disabled:
		r.Border(1, lineStrong)
	default:
		r.Border(1, line)
	}
	if disabled {
		r.Opacity(0.6)
	}
	r.Children(func() {
		ui.Column(c).Padding(space0_5, 0, 0).Children(func() { radio(c, on) })
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1).Children(func() {
			ui.Row(c).Gap(space2).Children(func() {
				bodyStrong(c, title, ink)
				if badgeText != "" {
					tag(c, badgeText, brassSoft, brassInk, false)
				}
			})
			ui.Text(c, desc).TextColor(inkMuted).FontSize(captionSize)
		})
	})
	return r.Clicked()
}

// ---- step 1 ----

func (w *workspace) vaultStep(c *ui.Context, f *firstRun) {
	kc := firstRunKeychain()
	setup := f.vault.NeedsSetup
	head(c, true, "Welcome to Keel", "Keel keeps your SSH sessions running after you close the window. First, a vault for host passwords and keys. It is encrypted on this machine and never leaves it.")
	ui.Column(c).FillWidth().Gap(space2 + space0_5).Label("How to unlock the vault").Children(func() {
		choiceCard(c, "Unlock with "+kc, f.usesKeychain(), !f.usesKeychain(), "Unlock with "+kc, "Recommended", keychainDesc(f, kc))
		if choiceCard(c, "Use a passphrase", !f.usesKeychain(), f.usesKeychain(), "Use a passphrase", "", passDesc(f, kc)) && setup {
			w.act(func() { f.want = "pass" })
		}
	})
	if setup {
		ui.Column(c).FillWidth().Gap(space3).Children(func() {
			ui.Row(c).FillWidth().Gap(space4).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
					labeled(c, "Passphrase", &f.passH, func() {
						inputBox(c, "Passphrase", &f.pass, "", &f.passH, false, true, maxPassLen, w.vaultKey(f, &f.pass))
					})
				})
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
					labeled(c, "Repeat passphrase", &f.pass2H, func() {
						inputBox(c, "Repeat passphrase", &f.pass2, "", &f.pass2H, false, true, maxPassLen, w.vaultKey(f, &f.pass2))
					})
				})
			})
			if f.passErr != "" {
				ui.Row(c).Label("passphrase error").Children(func() { caption(c, f.passErr, flare) })
			}
			help(c, "Keel can't recover the vault if you lose the passphrase.")
		})
	}
	box(c).Children(func() {
		path := f.vault.Path
		if path == "" {
			path = "keel.vault"
		}
		kv(c, "Vault file", path, true)
		kv(c, "Encryption", vaultEnc(f), false)
		kv(c, "Holds", "Host passwords, private keys and key passphrases. Host names, addresses and snippets stay in keel.json.", false)
	})
	if setup && f.existing {
		box(c).Label("existing vault").Children(func() {
			help(c, "To use a keel.vault you already have: quit Keel, run keel serve stop, put that keel.vault and its keel.json in "+
				dataFolder(f.vault.Path)+", then open Keel. It opens with the keychain or passphrase that made it. Keel does not change a vault it cannot open.")
		})
	}
	f.foot(func() {
		if setup && textBtn(c, "I already have a keel.vault…", btnGhost) {
			w.act(func() { f.existing = !f.existing })
		}
		ui.Spacer(c)
		if bigPrimary(c, nextLabel(f), !f.busy) {
			w.act(w.submitVault)
		}
	})
}

// vaultKey makes ↵ in a passphrase field create the vault.
func (w *workspace) vaultKey(f *firstRun, field *string) func(ui.TextKey) bool {
	return func(k ui.TextKey) bool {
		if k.Key == ui.KeyEnter && k.Mods == 0 {
			*field = k.Text()
			w.act(w.submitVault)
			return true
		}
		return false
	}
}

// ---- step 2 ----

func (w *workspace) hostsStep(c *ui.Context, f *firstRun) {
	title := "Bring your hosts"
	if f.flow == flowImport {
		title = "Import hosts"
	}
	head(c, false, title, f.lead)
	if f.preview != nil && f.preview.Found {
		ui.Column(c).FillWidth().Gap(space4).Children(func() { w.importBody(c, f) })
	}
	if f.notice != "" {
		col := tide
		if f.noticeBad {
			col = flare
		}
		ui.Row(c).FillWidth().Label("import notice").Children(func() { ui.Text(c, f.notice).TextColor(col).FontSize(uiFontSize) })
	}
	f.foot(func() {
		if f.flow == flowFirstRun && textBtn(c, "Back", btnGhost) {
			w.act(func() { f.step, f.want = stepVault, ""; f.notice = "" })
		}
		ui.Spacer(c)
		skip := "Cancel"
		if f.flow == flowFirstRun {
			skip = "Skip, add hosts later"
		}
		if textBtn(c, skip, btnDefault) {
			w.act(func() { f.notice = ""; w.afterHosts(f) })
		}
		n := f.addCount()
		if bigPrimary(c, addLabel(n), n > 0 && !f.busy) {
			w.act(w.runImport)
		}
	})
}

// checkBox is MyGo's check box that can be mixed (a dash), named by name. The caller owns the state: this reports a click and
// the caller sets the state it shows next frame.
func checkBox(c *ui.Context, name string, st checkState, disabled bool) bool {
	s := ui.CheckOff
	switch st {
	case checkOn:
		s = ui.CheckOn
	case checkMixed:
		s = ui.CheckMixed
	}
	b := ui.CheckboxState(c, &s, "").Label(name)
	if disabled {
		b.Disabled(true)
	}
	return !disabled && b.Changed()
}

// importCols are the table's columns as weights: Host, Address, User, Key, Via.
var importCols = [5]float32{3, 3, 2, 3, 4}

func (w *workspace) importBody(c *ui.Context, f *firstRun) {
	p, sel := *f.preview, f.sel
	w.importTable(c, f, p, sel)
	if len(p.Patterns) > 0 {
		ui.Column(c).FillWidth().Gap(space2).Label("Patterns").Children(func() {
			n := len(p.Patterns)
			msg := "This pattern sets options for the hosts above. It is not a host to add."
			if n > 1 {
				msg = fmt.Sprintf("These %d patterns set options for the hosts above. They are not hosts to add.", n)
			}
			help(c, msg)
			ui.Row(c).FillWidth().Wrap().Gap(space1_5).Children(func() {
				for _, pt := range p.Patterns {
					ui.Row(c).Height(space4).Padding(0, space1_5).Radius(radiusXs).Border(1, lineStrong).Justify(ui.Center).Tooltip(pt.Source).Label("pattern " + pt.Pattern).Children(func() {
						text(c, pt.Pattern, inkMuted, kbdSize, weightRegular).Font(monoStack)
					})
				}
			})
		})
	}
	if notes := notesList(p); len(notes) > 0 {
		ui.Column(c).FillWidth().Gap(space2).Children(func() {
			label := countWord(len(notes), "note", "notes") + " about this config"
			r := ui.Row(c).Gap(space1_5).Cursor(ui.CursorPointer).Label(label).Expanded(f.notesOpen)
			r.Children(func() {
				ch := "chevron-right"
				if f.notesOpen {
					ch = "chevron-down"
				}
				icon(c, ch, inkMuted)
				caption(c, label, inkMuted)
			})
			if r.Clicked() {
				w.act(func() { f.notesOpen = !f.notesOpen })
			}
			if f.notesOpen {
				ui.Column(c).FillWidth().Gap(space1).Children(func() {
					for _, n := range notes {
						ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Start).Children(func() {
							mono(c, n.source, inkMuted).Shrink(0)
							ui.Text(c, n.text).TextColor(inkMuted).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
						})
					}
				})
			}
		})
	}
	if len(p.Keys) > 0 {
		w.importKeys(c, f, p, sel)
	}
}

func (w *workspace) importTable(c *ui.Context, f *firstRun, p keelclient.SSHPreview, sel *importSel) {
	if len(p.Hosts) == 0 {
		help(c, "No hosts to add: every Host line in the config is a pattern.")
		return
	}
	ui.Column(c).FillWidth().Radius(radiusMd).Background(surface).Border(1, line).Clip().Label("Hosts in " + p.Path).Children(func() {
		cell := func(i int) ui.Element { return ui.Row(c).Grow(importCols[i]).Basis(0).MinWidth(0).Gap(space2) }
		hairline(ui.Row(c).FillWidth().Height(impRowHeight).Padding(0, space3).Gap(space3).Shrink(0), 0, 0, 1, 0).Children(func() {
			ui.Row(c).Width(impCheckCol - space3).Shrink(0).Children(func() {
				if checkBox(c, "Select all hosts", allState(p, sel), false) {
					w.act(f.toggleAll)
				}
			})
			for i, name := range []string{"Host", "Address", "User", "Key", "Via"} {
				cell(i).Children(func() { label(c, name, inkMuted) })
			}
		})
		ui.Scroll(c).FillWidth().MaxHeight(impRowHeight * impTableRows).Children(func() {
			for _, h := range p.Hosts {
				hairline(ui.Row(c).FillWidth().MinHeight(impRowHeight).Padding(space1, space3).Gap(space3).Key(h.Alias).Label("row "+h.Alias), 0, 0, 1, 0).Children(func() {
					ui.Row(c).Width(impCheckCol - space3).Shrink(0).Children(func() {
						if checkBox(c, "Add "+h.Alias, boolState(sel.hosts[h.Alias]), false) {
							w.act(func() { f.toggleHost(h.Alias) })
						}
					})
					cell(0).Children(func() {
						body(c, h.Alias, ink).Shrink(1).MinWidth(0)
						if h.Saved {
							tag(c, "Already saved", hazeSoft, haze, false)
						}
					})
					cell(1).Children(func() { mono(c, hostAddress(h), inkMuted).MinWidth(0) })
					cell(2).Children(func() { mono(c, h.User, inkMuted).MinWidth(0) })
					cell(3).Children(func() {
						if k := keyNames(h); k != "" {
							body(c, k, inkMuted).MinWidth(0)
						} else {
							body(c, "—", inkFaint)
						}
					})
					cell(4).Wrap().Children(func() { w.viaCell(c, f, h, sel) })
				})
			}
		})
		hairline(ui.Row(c).FillWidth().Padding(space2, space3), 1, 0, 0, 0).Children(func() {
			caption(c, summaryText(p, sel), inkMuted)
		})
	})
}

func boolState(b bool) checkState {
	if b {
		return checkOn
	}
	return checkOff
}

// viaCell is the Via column: the proxy's flag and, for a ProxyJump that is not kept, the Without the jump box.
func (w *workspace) viaCell(c *ui.Context, f *firstRun, h keelclient.ConfigHost, sel *importSel) {
	v := viaOf(h)
	if v.kind == "" {
		body(c, "—", inkFaint)
		return
	}
	bg, fg := tideSoft, tide
	switch v.kind {
	case "if-added":
		bg, fg = hazeSoft, haze
	case "unused":
		bg, fg = signalSoft, signal
	}
	// The flag wraps onto a second line when the column is narrow, so the row stays inside the table.
	r := ui.Row(c).MinHeight(space4).Padding(0, space1_5).Gap(space1).Radius(radiusXs).Justify(ui.Center).Shrink(1).MinWidth(0).Background(bg).Tooltip(v.tip).Label("via " + h.Alias)
	r.Children(func() {
		if v.kind == "unused" {
			iconSized(c, "alert", fg, iconSizeSm)
		}
		ui.Text(c, v.text).TextColor(fg).FontSize(kbdSize).FontWeight(weightSemibold).Shrink(1).MinWidth(0)
	})
	if v.noJump {
		ui.Row(c).Gap(space2).Children(func() {
			if checkBox(c, "Add "+h.Alias+" without the jump", boolState(sel.noJump[h.Alias]), false) {
				w.act(func() { f.toggleNoJump(h.Alias) })
			}
			help(c, "Without the jump")
		})
	}
}

func (w *workspace) importKeys(c *ui.Context, f *firstRun, p keelclient.SSHPreview, sel *importSel) {
	box(c).Label("Private keys").Children(func() {
		ui.Row(c).Gap(space2 + space0_5).Children(func() {
			icon(c, "key-01", inkMuted)
			bodyStrong(c, "Copy private keys into the vault", ink)
		})
		for i, k := range p.Keys {
			st := sel.keyOf(k.Path)
			ok := copyable(k)
			on := st.on && ok
			col := ui.Column(c).FillWidth().Gap(space2).Key(k.Path)
			if i > 0 {
				hairline(col, 1, 0, 0, 0).Padding(space2, 0, 0)
			}
			col.Children(func() {
				ui.Row(c).FillWidth().Gap(space3).Children(func() {
					ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Wrap().Gap(space2).Children(func() {
						name := k.Display
						if name == "" {
							name = k.Path
						}
						mono(c, name, ink)
						if k.Encrypted {
							tag(c, "Passphrase", brassSoft, inkMuted, true)
						}
						if len(k.UsedBy) > 0 {
							help(c, "Used by "+joinWords(k.UsedBy))
						}
						if !ok {
							prob := k.Problem
							if prob == "" {
								prob = "cannot be read"
							}
							caption(c, prob, flare)
						}
					})
					sw := on
					s := ui.Switch(c, &sw).Label("Copy " + keyLabel(k) + " into the vault").Shrink(0)
					if !ok {
						s.Disabled(true)
					}
					if sw != on {
						w.act(func() { f.toggleKey(k) })
					}
				})
				if on && k.Encrypted {
					labeled(c, "Passphrase for "+k.Name, f.handleFor(k.Path), func() {
						inputBox(c, "Passphrase for "+k.Name, &st.pass, "", f.handleFor(k.Path), false, true, maxPassLen, w.keyPassKey(st))
					})
				}
				if msg := f.keyErrs[k.Path]; msg != "" {
					ui.Row(c).Label("key error " + k.Name).Children(func() { caption(c, msg, flare) })
				}
			})
		}
		help(c, "Each key is off until you turn it on. The files in ~/.ssh stay where they are; Keel only reads them. A copied key becomes a Keychain identity, encrypted in the vault with its passphrase. Hosts whose key stays off are added with password sign-in; edit them to pick a key.")
	})
}

func keyLabel(k keelclient.ConfigKey) string {
	if k.Display != "" {
		return k.Display
	}
	return k.Name
}

func joinWords(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

// keyPassKey makes ↵ in a key's passphrase field run the import.
func (w *workspace) keyPassKey(st *keyState) func(ui.TextKey) bool {
	return func(k ui.TextKey) bool {
		if k.Key == ui.KeyEnter && k.Mods == 0 {
			st.pass = k.Text()
			w.act(w.runImport)
			return true
		}
		return false
	}
}

// ---- step 3 ----

func (w *workspace) agentStep(c *ui.Context, f *firstRun) {
	head(c, false, "An agent that suggests, never types", "Ask it about a session, or type plain language at a prompt and press ⇥. It is optional, and you can set it up later in Settings.")
	ui.Column(c).FillWidth().Gap(space3).Label("What the agent promises").Children(func() {
		for _, p := range []struct {
			glyph  string
			col    ui.Color
			title  string
			detail string
		}{
			{"eye", haze, "Reads only the panes you pick", `Every pane it can read carries an "Agent reads" mark. Hosts can opt out.`},
			{"shield-01", brassInk, "Suggests commands; you press Type", "The agent never types into a pane. Nothing reaches one until you accept it. Destructive commands need ⇧⇥."},
			{"lock", tide, "Secrets are redacted, the vault stays closed", "No passwords, keys or snippets leave it. Secret-looking values in a pane are redacted before sending."},
		} {
			ui.Row(c).FillWidth().Gap(space3).AlignItems(ui.Start).Label("promise " + p.title).Children(func() {
				ui.Row(c).Size(sizeControl, sizeControl).Radius(radiusSm).Justify(ui.Center).Shrink(0).Background(p.col.Alpha(tileTintAlpha)).Children(func() {
					iconSized(c, p.glyph, p.col, iconSize-space0_5)
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
					bodyStrong(c, p.title, ink)
					ui.Text(c, p.detail).TextColor(inkMuted).FontSize(captionSize)
				})
			})
		}
	})
	w.agentChoice(c, f)
	if f.notice != "" {
		col := tide
		if f.noticeBad {
			col = flare
		}
		ui.Row(c).FillWidth().Label("import notice").Children(func() { ui.Text(c, f.notice).TextColor(col).FontSize(uiFontSize) })
	}
	f.foot(func() {
		if textBtn(c, "Back", btnGhost) {
			w.act(func() { w.showHostsStep(f) })
		}
		ui.Spacer(c)
		if textBtn(c, "Set up later", btnDefault) {
			w.act(func() { w.finishFirstRun(f) })
		}
		if bigPrimary(c, "Open Keel", !f.busy) {
			w.act(func() { w.openKeel(f) })
		}
	})
}

// agentChoice is the k-box of step 3: the provider (Google Gemini or Not now), the key field and the provider's words
// when Gemini is chosen, the switch, and the line under them (gate.templ agentStep).
func (w *workspace) agentChoice(c *ui.Context, f *firstRun) {
	d := keelclient.AgentProvider{Name: "Google Gemini", KeyLabel: "Gemini API key"}
	if f.agentProv != nil {
		d = *f.agentProv
	}
	keyLabel := orElse(d.KeyLabel, "Gemini API key")
	ui.Column(c).FillWidth().Padding(space4).Gap(space3).Radius(radiusMd).Background(surface).Border(1, line).Label("agent choice").Children(func() {
		ui.Column(c).FillWidth().Gap(space1_5).Children(func() {
			label(c, "Provider", inkMuted)
			ui.Column(c).FillWidth().Gap(space2).Label("Provider").Children(func() {
				if choiceCard(c, "Google Gemini", f.agentGemini, false, "Google Gemini", "", "Answers questions and writes suggestions. Needs an API key.") && !f.agentGemini {
					w.act(func() { f.pickAgentProvider(true) })
				}
				if choiceCard(c, "Not now", !f.agentGemini, false, "Not now", "", "Connect a provider later in Settings, Agent.") && f.agentGemini {
					w.act(func() { f.pickAgentProvider(false) })
				}
			})
		})
		if f.agentGemini {
			if d.Warning != "" {
				ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(signalSoft).AlignItems(ui.Start).Label("agent warning").Children(func() {
					icon(c, "alert", signal)
					ui.Text(c, d.Warning).TextColor(ink).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
				})
			}
			labeled(c, keyLabel, &f.agentKeyH, func() {
				// Masked: the key is never drawn, and the field is emptied the moment it is sent (openKeel).
				inputBox(c, keyLabel+" input", &f.agentKey, "", &f.agentKeyH, true, true, 0, func(k ui.TextKey) bool {
					f.agentErr = ""
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						f.agentKey = k.Text()
						w.act(func() { w.openKeel(f) })
						return true
					}
					return false
				})
			})
			if f.agentErr != "" {
				ui.Row(c).FillWidth().Label("agent key error").Children(func() { ui.Text(c, f.agentErr).TextColor(flare).FontSize(captionSize) })
			}
			ui.Row(c).Gap(space1_5).Children(func() {
				if strings.HasPrefix(d.KeyHelpURL, "https://") {
					ui.Link(c, "Get a key", d.KeyHelpURL).TextColor(brassInk).FontSize(captionSize)
				}
				if strings.HasPrefix(d.TermsURL, "https://") {
					help(c, "·")
					ui.Link(c, "Gemini API terms", d.TermsURL).TextColor(brassInk).FontSize(captionSize)
				}
			})
		}
		ui.Row(c).FillWidth().Gap(space3).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() { body(c, "Suggest commands when I type plain language", ink).MaxLines(0) })
			on := !f.agentOff
			ui.Switch(c, &on).Label("Suggest commands when I type plain language switch").Shrink(0)
			f.agentOff = !on
		})
		help(c, "Nothing is sent anywhere while you set up. A key is stored in the vault and used only when you ask the agent or press ⇥.")
	})
}
