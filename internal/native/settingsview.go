package native

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Settings page drawn as SettingsPage (internal/ui/pages.templ) with .k-rail, .k-settings-main, .settings-group and
// .pref-row of keel.css. Each section is data (groups of rows), so the search can read every row's text as filterSettings does.

const (
	settingsMaxWidth = 90 * 7 // .k-settings-form: max-width 90ch
	sliderWidth      = 144    // w-36
	prefInputWidth   = sizeRail
	leadWidth        = 60 * 7 // .k-settings-lead: 60ch
)

// prefRow is a .pref-row: a title and a line under it on the left, the control on the right.
type prefRow struct {
	title, desc string
	extra       string // more text the search reads (data-search)
	stack       bool   // .pref-row-stack: the control under the text
	preview     bool   // .pref-row-preview: not searched
	ctl         func(c *ui.Context)
}

type prefGroup struct {
	title string
	rows  []prefRow
}

type settingsSection struct {
	id, title, lead string
	groups          []prefGroup
}

func (r prefRow) text() string { return strings.ToLower(r.title + " " + r.desc + " " + r.extra) }

// ---- the page ----

func (w *workspace) settingsView(c *ui.Context, p *settingsPage) {
	w.syncSliders(p)
	secs := w.settingsSections(p)
	q := strings.ToLower(strings.TrimSpace(p.search))
	shown := secs
	if q != "" { // filterSettings: every section with a matching row shows, with its matching rows
		shown = nil
		for _, s := range secs {
			var gs []prefGroup
			for _, g := range s.groups {
				var rows []prefRow
				for _, r := range g.rows {
					if !r.preview && strings.Contains(r.text(), q) {
						rows = append(rows, r)
					}
				}
				if len(rows) > 0 {
					gs = append(gs, prefGroup{g.title, rows})
				}
			}
			if len(gs) > 0 {
				s.groups = gs
				shown = append(shown, s)
			}
		}
	}
	ui.Row(c).Grow(1).FillWidth().FillHeight().Children(func() {
		w.settingsRail(c, p)
		ui.Scroll(c).Grow(1).FillHeight().Shrink(1).MinWidth(0).Background(w.ground(surface)).Label("settings").Children(func() {
			ui.Column(c).FillWidth().MaxWidth(rem(settingsMaxWidth)).Padding(space8).Gap(space6).Children(func() {
				switch {
				case q != "" && len(shown) == 0:
					help(c, "No settings match.")
				case q != "":
					for _, s := range shown {
						w.settingsPanel(c, s, p)
					}
				default:
					for _, s := range secs {
						if s.id == p.section {
							w.settingsPanel(c, s, p)
						}
					}
				}
			})
		})
	})
}

// settingsRail is the section list: the label, the search, the sections, and About Keel under a spacer.
func (w *workspace) settingsRail(c *ui.Context, p *settingsPage) {
	hairline(ui.Column(c).Width(sizeRail).Shrink(0).FillHeight().Padding(space3, space2).Gap(space0_5).Background(w.ground(chrome)), 0, 1, 0, 0).Children(func() {
		ui.Row(c).Padding(space1, space2).Children(func() { menuLabel(c, "Settings") })
		ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space1_5).Radius(radiusSm).Background(stage).Border(1, lineStrong).Children(func() {
			icon(c, "search-01", inkFaint)
			ui.TextInputBase(c, &p.search).Label("Search settings").Placeholder("Search settings").PlaceholderColor(inkFaint).TextColor(ink).
				FontSize(uiFontSize).Grow(1).MinWidth(0).Bind(&p.searchH)
		})
		ui.Box(c).Height(space2).Shrink(0)
		for _, n := range settingsNav {
			w.railItem(c, p, n[0], n[1], "")
		}
		ui.Spacer(c)
		version := ""
		if p.rt != nil {
			version = versionOf(p.rt.Build)
		}
		w.railItem(c, p, "about", "About Keel", version)
	})
}

// railItem is button.k-rail-item: selected has the hover ground and a heavier name; a count sits on the right.
func (w *workspace) railItem(c *ui.Context, p *settingsPage, id, name, count string) {
	on := p.section == id
	r := ui.Row(c).FillWidth().Height(sizeControl).Padding(0, space2).Gap(space2).Radius(radiusSm).Label("settings nav " + name).
		Cursor(ui.CursorPointer).Selected(on)
	if on || r.Hovered() {
		r.Background(hoverGround)
	}
	r.Children(func() {
		if on {
			bodyStrong(c, name, ink)
		} else {
			body(c, name, inkMuted)
		}
		if count != "" {
			ui.Spacer(c)
			caption(c, count, inkFaint)
		}
	})
	if r.Clicked() {
		w.act(func() {
			p.section, p.search = id, ""
			if id == "sharing" || id == "vault" || id == "runtime" || id == "about" {
				w.loadSettings()
			}
		})
	}
}

func (w *workspace) settingsPanel(c *ui.Context, s settingsSection, p *settingsPage) {
	ui.Column(c).FillWidth().Gap(space6).Key(s.id).Label("settings " + s.title).Children(func() {
		ui.Column(c).FillWidth().Gap(space3).Children(func() { // .k-settings-lead sits half a gap under the title
			ui.Text(c, s.title).TextColor(ink).FontSize(displaySize).FontWeight(weightSemibold)
			if s.lead != "" {
				ui.Text(c, s.lead).TextColor(inkMuted).FontSize(uiFontSize).MaxWidth(rem(leadWidth))
			}
		})
		for _, g := range s.groups {
			ui.Column(c).FillWidth().Gap(space1).Children(func() {
				if g.title != "" {
					menuLabel(c, g.title)
				}
				for i, r := range g.rows {
					w.prefRowView(c, r, i == len(g.rows)-1)
				}
			})
		}
	})
}

// prefRowView is .pref-row: the rows of a group have a hairline under them but the last.
func (w *workspace) prefRowView(c *ui.Context, r prefRow, last bool) {
	bottom := float32(1)
	if last {
		bottom = 0
	}
	var box ui.Element
	switch {
	case r.preview:
		box = ui.Column(c).FillWidth().Padding(space3, 0).Label("setting preview")
	case r.stack:
		box = ui.Column(c).FillWidth().Padding(space3, 0).Gap(space2).Label("setting " + r.title)
	default:
		box = ui.Row(c).FillWidth().Padding(space3, 0).Gap(space4 + space1).AlignItems(ui.Center).Label("setting " + r.title)
	}
	hairline(box, 0, 0, bottom, 0).Children(func() {
		if r.preview {
			r.ctl(c)
			return
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
			if r.title != "" {
				ui.Text(c, r.title).TextColor(ink).FontSize(uiFontSize).FontWeight(weightMedium)
			}
			if r.desc != "" {
				help(c, r.desc)
			}
		})
		if r.ctl != nil {
			r.ctl(c)
		}
	})
}

// ---- controls ----

// alwaysOn is the outline badge with a lock.
func alwaysOn(c *ui.Context) {
	ui.Row(c).Height(space4).Padding(0, space1_5).Gap(space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).Border(1, lineStrong).Label("badge Always on").Children(func() {
		iconSized(c, "lock", inkMuted, iconSizeSm)
		text(c, "Always on", inkMuted, kbdSize, weightSemibold)
	})
}

// outlineBadge is .k-badge--outline with a text.
func outlineBadge(c *ui.Context, s string) { tag(c, s, ui.Transparent, ink, true) }

// statValue is .k-mono.k-pref-value: a value on the right, in the mono face, wrapping.
func statValue(c *ui.Context, s string) {
	ui.Text(c, s).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxWidth(60 * 7).Label("value " + s).Shrink(1).MinWidth(0)
}

// sliderCtl is .pref-slider: the slider and its value.
func sliderCtl(c *ui.Context, name string, v *float64, lo, hi float64, shown int) func(*ui.Context) {
	return func(c *ui.Context) {
		ui.Row(c).Gap(space3).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.StepSlider(c, v, lo, hi, 1).Label(name + " slider").Width(sliderWidth)
			ui.Row(c).MinWidth(space6).Justify(ui.Center).Children(func() { body(c, fmt.Sprint(shown), ink).Label(name + " value") })
		})
	}
}

// copyBtn is a small k-btn that copies a text and says so.
func (w *workspace) copyBtn(c *ui.Context, name, value string) {
	if value == "" {
		return
	}
	if textBtn(c, name, btnDefault) {
		w.act(func() { c.WriteClipboard(value); w.say("Copied") })
	}
}

// ---- the sections ----

func (w *workspace) settingsSections(p *settingsPage) []settingsSection {
	return []settingsSection{
		w.appearanceSection(p), w.terminalSection(p), w.keyboardSection(), w.sharingSection(p), w.vaultSection(p), w.runtimeSection(p), w.aboutSection(p),
	}
}

func (w *workspace) appearanceSection(p *settingsPage) settingsSection {
	opacity := prefRow{title: "Window opacity", desc: "How much of the desktop the window covers. 30–100; 100 is opaque.",
		ctl: func(c *ui.Context) {
			sliderCtl(c, "Window opacity", &p.opacityV, opacityMin, opacityMax, w.win.Opacity)(c)
		}}
	if !w.translucentWin && w.win.Opacity < opacityMax {
		opacity.desc += " The window turns see-through the next time Keel starts."
	}
	if !w.themeNative { // .pref-desc-os is hidden where the window takes the theme as its appearance
		opacity.desc += " When the theme differs from the system's, the window stays opaque."
	}
	theme := prefRow{title: "Theme", desc: "Light, dark, or follow the system.", ctl: func(c *ui.Context) {
		ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
			segControl(c, "Color theme", validTheme(w.win.Theme), themeChoices, func(v string) { w.act(func() { w.setTheme(v) }) })
		})
	}}
	uiSizeRow := prefRow{title: "UI size", desc: "Tabs, settings, and chrome. 11–16px. Terminal size is separate.",
		ctl: func(c *ui.Context) { sliderCtl(c, "UI size", &p.uiV, uiSizeMin, uiSizeMax, w.win.UISize)(c) }}
	rows := []prefRow{theme, opacity}
	if hasBlur() {
		rows = append(rows, prefRow{title: "Window blur", desc: "How much the desktop behind the window is blurred. 0–64; 0 is clear.",
			ctl: func(c *ui.Context) { sliderCtl(c, "Window blur", &p.blurV, blurMin, blurMax, w.win.Blur)(c) }})
	}
	rows = append(rows, uiSizeRow) // Theme, Window opacity, Window blur, UI size, as the app orders them
	return settingsSection{id: "appearance", title: "Appearance", groups: []prefGroup{{title: "Display", rows: rows}}}
}

func (w *workspace) terminalSection(p *settingsPage) settingsSection {
	session := []prefGroup{{title: "Session", rows: []prefRow{
		{title: "Font", desc: "Monospace face used in every pane.", ctl: func(c *ui.Context) {
			entries := make([]pickEntry, len(termFonts))
			for i, f := range termFonts {
				entries[i] = pickEntry{value: f.value, label: f.label, desc: f.desc}
			}
			ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
				picker(c, "Terminal font", validTermFont(w.win.TermFont), entries, func(v string) { w.act(func() { w.setTermFont(v) }) })
			})
		}},
		{title: "Text size", desc: "10–22px. ⌘+ / ⌘− / ⌘0 also change size.", ctl: func(c *ui.Context) {
			sliderCtl(c, "Text size", &p.sizeV, termSizeMin, termSizeMax, w.termSize)(c)
			kbd(c, "⌘+").Label("shortcut hint")
		}},
		{preview: true, ctl: func(c *ui.Context) { // the .pref-preview box: a prompt in the terminal's face at the text size
			ui.Row(c).FillWidth().Height(3*24).Padding(0, space3).AlignItems(ui.Center).Radius(radiusSm).Background(stage).Border(1, line).Label("terminal preview").Children(func() {
				ui.Text(c, "guest@keel:~ $ ssh prod-api").TextColor(ink).FontSize(float32(w.termSize)).Font(termStack(w.win.TermFont))
			})
		}},
	}}}
	input := prefGroup{title: "Input", rows: []prefRow{
		{title: "Copy on select", desc: "Copy terminal selection to the clipboard.", ctl: func(c *ui.Context) {
			on := w.term.CopyOnSelect
			ui.Switch(c, &on).Label("Copy on select switch").Shrink(0)
			if on != w.term.CopyOnSelect {
				w.act(func() { w.setCopyOnSelect(on) })
			}
		}},
		{title: "Right-click in the terminal",
			desc: "Paste the clipboard, open the terminal menu, or do nothing. With Menu or Nothing, ⇧ right-click opens the system's menu.",
			ctl: func(c *ui.Context) {
				ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
					segControl(c, "Right-click in the terminal", w.term.RightClick, rightClickModes, func(v string) { w.act(func() { w.setRightClick(v) }) })
				})
			}},
	}}
	return settingsSection{id: "terminal", title: "Terminal", groups: append(session, input)}
}

func (w *workspace) keyboardSection() settingsSection {
	s := settingsSection{id: "keyboard", title: "Keyboard",
		lead: "The keys Keel handles. They are fixed; the command palette shows the same keys beside each action."}
	for _, g := range shortcutGroups() {
		grp := prefGroup{title: g.name}
		for _, it := range g.items {
			grp.rows = append(grp.rows, prefRow{title: it.title, extra: it.keys, ctl: func(c *ui.Context) {
				ui.Row(c).Gap(space1).Shrink(0).Label("keys " + it.keys).Children(func() {
					for _, cap := range splitKeyCaps(it.keys) {
						kbd(c, cap)
					}
				})
			}})
		}
		s.groups = append(s.groups, grp)
	}
	return s
}

func (w *workspace) sharingSection(p *settingsPage) settingsSection {
	_, placeholder, signalHelp := signalFields(keelRuntime(p))
	fallback := defaultName()
	you := prefGroup{title: "You", rows: []prefRow{{title: "Display name",
		desc: "Shown to people in sessions you share and join. Empty uses the default.",
		ctl: func(c *ui.Context) {
			if !p.nameSeen { // the field holds the saved name until the user is in it
				p.name, p.nameSeen = w.term.Name, true
			}
			ui.Row(c).Width(prefInputWidth).Shrink(1).Children(func() {
				inputBox(c, "Display name input", &p.name, fallback, &p.nameH, false, false, nameMax, func(k ui.TextKey) bool {
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						p.name = k.Text()
						w.act(func() { w.setDisplayName(p.name); p.nameSeen = false })
						return true
					}
					return false
				})
			})
			if p.nameH.FocusWithin(c) {
				p.nameWas = true
			} else if p.nameWas { // change: leaving the field keeps the name
				p.nameWas = false
				w.act(func() { w.setDisplayName(p.name); p.nameSeen = false })
			}
		}}}}
	signal := prefGroup{title: "Signal server", rows: []prefRow{{title: "Server for internet join links", desc: signalHelp, stack: true,
		ctl: func(c *ui.Context) {
			if !p.signalSeen && p.rt != nil {
				p.signal, p.signalSeen = p.signalSavedValue(), true
			}
			inputBox(c, "Signal server input", &p.signal, placeholder, &p.signalH, true, false, 2048, func(k ui.TextKey) bool {
				switch {
				case k.Key == ui.KeyEnter && k.Mods == 0:
					p.signal = k.Text()
					w.act(func() { w.commitSignal(p) })
					return true
				case k.Key == ui.KeyEscape && k.Mods == 0: // the saved server comes back
					w.act(func() { p.signalSeen = false })
					return true
				}
				return false
			})
			if p.signalH.FocusWithin(c) {
				p.signalWas = true
			} else if p.signalWas {
				p.signalWas = false
				w.act(func() { w.commitSignal(p) })
			}
		}}}}
	access := prefGroup{title: "Access", rows: []prefRow{
		{title: "Joining needs your approval", desc: "Anyone with the link can ask to watch. You see the name they typed and choose Allow or Deny.", ctl: alwaysOn},
		{title: "Control stays with you", desc: "Viewers can ask for control. You pass it to one person and can take it back.", ctl: alwaysOn},
	}}
	return settingsSection{id: "sharing", title: "Sharing", lead: "Share a session by link. Nobody joins without your say, and the keyboard stays with you until you pass it.",
		groups: []prefGroup{you, signal, access}}
}

func (w *workspace) vaultSection(p *settingsPage) settingsSection {
	state, label, helpText, path := "", "", "", ""
	if p.vault != nil {
		v := *p.vault
		state = "Unlocked"
		switch {
		case v.NeedsSetup:
			state = "Not set up"
		case v.Locked:
			state = "Locked"
		}
		label, helpText = vaultMode(v)
		path = v.Path
	}
	vault := prefGroup{title: "Vault", rows: []prefRow{
		{title: "State", ctl: func(c *ui.Context) { outlineBadge(c, state) }},
		{title: "Unlocked with", desc: helpText, ctl: func(c *ui.Context) { outlineBadge(c, label) }},
		{title: "Vault file", desc: path, ctl: func(c *ui.Context) { w.copyBtn(c, "Copy path", path) }},
	}}
	safety := prefGroup{title: "Safety", rows: []prefRow{
		{title: "The agent never sees vault contents", desc: "Passwords, keys and passphrases are never sent to a model. Every agent read and insert is in Logs.", ctl: alwaysOn},
		{title: "A lost passphrase cannot be recovered", desc: "Keel never re-keys or overwrites an existing vault from the unlock screen."},
	}}
	return settingsSection{id: "vault", title: "Vault & security",
		lead: "Passwords, key passphrases and the agent key are encrypted in the vault file. Nothing here shows a secret.", groups: []prefGroup{vault, safety}}
}

func (w *workspace) runtimeSection(p *settingsPage) settingsSection {
	rt := keelRuntime(p)
	stat := func(title, value string) prefRow {
		return prefRow{title: title, extra: value, ctl: func(c *ui.Context) { statValue(c, value) }}
	}
	build := rt.Build
	if build == "" && p.rt != nil {
		build = "unknown"
	}
	status := prefGroup{title: "Status", rows: []prefRow{
		stat("Build", build), stat("Address", rt.Addr), stat("Data folder", rt.DataDir), stat("Process id", pidText(p)),
		stat("Running sessions", fmt.Sprint(rt.Sessions)), stat("Uptime", uptimeText(rt.UptimeSec)),
	}}
	window := prefGroup{title: "Window", rows: []prefRow{{title: "Sessions keep running after the window closes",
		desc: "Closing the window detaches; reopen Keel to attach. Closing a session tab ends that session.", ctl: alwaysOn}}}
	canStop := rt.CanStop == nil || *rt.CanStop
	stop := prefGroup{title: "Stop", rows: []prefRow{{title: "Stop the Keel runtime",
		desc: "Ends every running session and port forward, then closes keel serve. Reopen Keel to start it again.",
		ctl: func(c *ui.Context) {
			b := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).Shrink(0).Label("Stop runtime…").Cursor(ui.CursorPointer)
			fg := flare
			switch {
			case !canStop:
				b.Background(flareSoft).Opacity(dimAlpha).Disabled(true)
			case b.Hovered():
				b.Background(flare)
				fg = onFlare
			default:
				b.Background(flareSoft)
			}
			b.Children(func() { label(c, "Stop runtime…", fg) })
			if b.Clicked() && canStop {
				w.act(w.askStopRuntime)
			}
		}}}}
	return settingsSection{id: "runtime", title: "Runtime",
		lead: "keel serve owns your sessions and serves this window. It keeps running when the window closes.", groups: []prefGroup{status, window, stop}}
}

func (w *workspace) aboutSection(p *settingsPage) settingsSection {
	rt := keelRuntime(p)
	stat := func(title, value string) prefRow {
		return prefRow{title: title, extra: value, ctl: func(c *ui.Context) { statValue(c, value) }}
	}
	version, build := "unknown", "unknown"
	if v := versionOf(rt.Build); v != "" {
		version = v
	}
	if rt.Build != "" {
		build = rt.Build
	}
	link := func(title, url, name string) prefRow {
		return prefRow{title: title, desc: url, ctl: func(c *ui.Context) { w.copyBtn(c, name, url) }}
	}
	return settingsSection{id: "about", title: "About Keel",
		lead: "A local SSH workspace. Go owns SSH and the terminals; this window is a native client of keel serve.",
		groups: []prefGroup{
			{title: "Version", rows: []prefRow{stat("Version", version), stat("Build", build)}},
			{title: "Links", rows: []prefRow{
				link("Source", "https://github.com/KJK-Nice/keel", "Copy link"),
				link("README", "https://github.com/KJK-Nice/keel#readme", "Copy link"),
			}},
		}}
}

func pidText(p *settingsPage) string {
	if p.rt == nil {
		return ""
	}
	return fmt.Sprint(p.rt.PID)
}

// keelRuntime is what GET /api/runtime said, or nothing yet.
func keelRuntime(p *settingsPage) keelclient.Runtime {
	if p.rt == nil {
		return keelclient.Runtime{}
	}
	return *p.rt
}
