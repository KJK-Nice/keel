package native

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"slime/internal/native/slimeclient"
)

// The Settings page. Each section is data (groups of rows), so the search can read every row's text as filterSettings does.

const (
	settingsMaxWidth = 667 // 616 px at the default UI size, plus margin
	sliderPx         = 117 // 9 UI-size px
	prefInputWidth   = sizeRail
	leadWidth        = 496 // the lead wraps its lines at 458 px in the design (between 452 and 465)
)

type prefRow struct {
	title, desc string
	extra       string // more text the search reads
	stack       bool
	preview     bool // not searched
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
	w.syncAgentLines(p)
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
			ui.Column(c).FillWidth().MaxWidth(rem(settingsMaxWidth) + 2*space8).Padding(space8).Gap(space6).Children(func() {
				switch {
				case q != "" && len(shown) == 0:
					if emptySide(c, emptyNoMatch("search-01", strings.TrimSpace(p.search))) {
						w.act(func() { p.search = "" })
					}
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

func (w *workspace) settingsRail(c *ui.Context, p *settingsPage) {
	hairline(ui.Column(c).Width(sizeRail).Shrink(0).FillHeight().Padding(space3, space2).Gap(space0_5).Background(w.ground(chrome)), 0, 1, 0, 0).Children(func() {
		ui.Row(c).Padding(space1, space2).Children(func() { menuLabel(c, "Settings") })
		searchField(c, &p.search, "Search settings", &p.searchH).FillWidth()
		ui.Box(c).Height(space2).Shrink(0)
		// The sections are MyGo's ui.Sidebar: its item look, the accent while it has the keyboard, grey otherwise, and
		// ↑ ↓ Home End and typing a name to choose. About Slime is a second one under the first, so ↑ ↓ stop at the seam.
		chosen := p.section
		side := func(items func()) ui.Element {
			return ui.Sidebar(c, &chosen, items).Padding(0).Background(ui.Transparent)
		}
		a := side(func() {
			for _, n := range settingsNav {
				ui.SidebarItem(c, n[0], nil, n[1]).Label("settings nav " + n[1])
			}
		}).Grow(1)
		version := ""
		if p.rt != nil {
			version = versionOf(p.rt.Build)
		}
		b := side(func() {
			ui.SidebarItem(c, "about", nil, "About Slime").Label("settings nav About Slime").Children(func() {
				caption(c, version, inkFaint).Shrink(0)
			})
		})
		if (a.Changed() || b.Changed()) && chosen != p.section {
			id := chosen
			w.act(func() {
				p.showSection(id)
				if id == "sharing" || id == "vault" || id == "runtime" || id == "about" {
					w.loadSettings()
				}
				if id == "agent" {
					w.loadAgentSettings()
				}
			})
		}
	})
}

func (w *workspace) settingsPanel(c *ui.Context, s settingsSection, p *settingsPage) {
	ui.Column(c).FillWidth().Gap(space6).Key(s.id).Label("settings " + s.title).Children(func() {
		ui.Column(c).FillWidth().Gap(space3).Children(func() { // the lead sits half a gap under the title
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

// prefRowView draws a row: the rows of a group have a hairline under them but the last.
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

func alwaysOn(c *ui.Context) {
	ui.Row(c).Height(space4).Padding(0, space1_5).Gap(space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).Border(1, lineStrong).Label("badge Always on").Children(func() {
		iconSized(c, "lock", inkMuted, iconSizeSm)
		text(c, "Always on", inkMuted, kbdSize, weightSemibold)
	})
}

func outlineBadge(c *ui.Context, s string) { neutralBadge(c, s) }

func statValue(c *ui.Context, s string) {
	ui.Text(c, s).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxWidth(60 * 7).Label("value " + s).Shrink(1).MinWidth(0)
}

func sliderCtl(c *ui.Context, name string, v *float64, lo, hi float64, shown int, over ...func()) func(*ui.Context) {
	return func(c *ui.Context) {
		ui.Row(c).Gap(space3).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.StepSlider(c, v, lo, hi, 1).Label(name + " slider").Width(rem(sliderPx))
			ui.Row(c).MinWidth(space6).Justify(ui.Center).Children(func() { // over: a key cap that stands in for the value while ⌘ is held
				body(c, fmt.Sprint(shown), ink).Label(name + " value")
				for _, f := range over {
					f()
				}
			})
		})
	}
}

// numberCtl is a number field of an integer pref, bound as a slider is (sliderSpecs applies what is typed or stepped);
// over draws over it (a key cap while ⌘ is held).
func numberCtl(c *ui.Context, name string, v *float64, lo, hi, step float64, over ...func()) {
	ui.Row(c).Width(sizeNumber).Shrink(0).Children(func() {
		ui.NumberInput(c, v, lo, hi, step).Label(name + " field").FillWidth()
		for _, f := range over {
			f()
		}
	})
}

func (w *workspace) copyBtn(c *ui.Context, name, value string) {
	if value == "" {
		return
	}
	if textBtn(c, name, btnDefault) {
		w.act(func() { w.copy(value, "") })
	}
}

// ---- the sections ----

func (w *workspace) settingsSections(p *settingsPage) []settingsSection {
	return []settingsSection{
		w.appearanceSection(p), w.terminalSection(p), w.keyboardSection(), w.agentSection(p), w.sharingSection(p), w.vaultSection(p), w.runtimeSection(p), w.aboutSection(p),
	}
}

func (w *workspace) appearanceSection(p *settingsPage) settingsSection {
	opacity := prefRow{title: "Window opacity", desc: "How much of the desktop the window covers. 30–100; 100 is opaque.",
		ctl: func(c *ui.Context) { w.prefSlider(c, p, keyOpacity, "Window opacity") }}
	if !w.translucentWin && w.prefs.Opacity < opacityMax {
		opacity.desc += " The window turns see-through the next time Slime starts."
	}
	if !w.themeNative { // where the window takes the theme as its appearance there is no system theme to differ from
		opacity.desc += " When the theme differs from the system's, the window stays opaque."
	}
	theme := prefRow{title: "Theme", desc: "Light, dark, or follow the system.", ctl: func(c *ui.Context) {
		ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
			segControl(c, "Color theme", validTheme(w.prefs.Theme), themeChoices, func(v string) { w.act(func() { w.setTheme(v) }) })
		})
	}}
	uiSizeRow := prefRow{title: "UI size", desc: "Tabs, settings, and chrome. 11–16px. Terminal size is separate.",
		ctl: func(c *ui.Context) { w.prefSlider(c, p, keyUISize, "UI size") }}
	rows := []prefRow{theme, opacity}
	if hasBlur() {
		rows = append(rows, prefRow{title: "Window blur", desc: "How much the desktop behind the window is blurred. 0–64; 0 is clear.",
			ctl: func(c *ui.Context) { w.prefSlider(c, p, keyBlur, "Window blur") }})
	}
	rows = append(rows, uiSizeRow)
	return settingsSection{id: "appearance", title: "Appearance", groups: []prefGroup{{title: "Display", rows: rows}}}
}

func (w *workspace) terminalSection(p *settingsPage) settingsSection {
	session := []prefGroup{{title: "Typography", rows: []prefRow{
		{title: "Font", desc: "Monospace face used in every pane: Slime's own, or any installed on this machine.", ctl: func(c *ui.Context) {
			cur := validTermFont(w.prefs.TermFont)
			entries := fontEntries(installedFonts())
			if !slices.ContainsFunc(entries, func(e pickEntry) bool { return e.value == cur }) { // a family since removed
				entries = append(entries, pickEntry{value: cur, label: termFontLabel(cur), desc: "Not installed"})
			}
			ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
				picker(c, "Terminal font", cur, entries, func(v string) { w.act(func() { w.setTermFont(v) }) })
			})
		}},
		{title: "Text size", desc: "10–22px. ⌘+ / ⌘− / ⌘0 also change size.", ctl: func(c *ui.Context) {
			numberCtl(c, "Text size", &p.sliders[keyTermSize].v, termSizeMin, termSizeMax, 1, func() { w.keyCap(c, "+ − 0") }) // the hint shows only while ⌘ is held
		}},
		{title: "Weight", desc: "100–900. Bold text is 300 heavier. A font without the weight draws its nearest.", ctl: func(c *ui.Context) {
			numberCtl(c, "Weight", &p.sliders[keyTermWeight].v, termWeightMin, termWeightMax, termWeightStep)
		}},
		{title: "Line height", desc: "100–200% of the font's own. Text is centred in its row.", ctl: func(c *ui.Context) {
			w.prefSlider(c, p, keyLineHeight, "Line height")
		}},
		{preview: true, ctl: func(c *ui.Context) { w.termPreview(c) }},
	}}}
	advanced := prefGroup{title: "Advanced font", rows: []prefRow{
		{title: "Ligatures", desc: "Join pairs such as -> and != into one sign, where the font draws them.", ctl: func(c *ui.Context) {
			on := w.prefs.Ligatures
			ui.Switch(c, &on).Label("Ligatures switch").Shrink(0)
			if on != w.prefs.Ligatures {
				w.act(func() { w.setLigatures(on) })
			}
		}},
		{title: "Font features", desc: "OpenType features, as ss01, cv02 or -calt (off). What the font lacks does nothing.", ctl: func(c *ui.Context) {
			if !p.featuresWas { // the field shows the saved features until the user is in it
				p.features = w.prefs.Features
			}
			ui.Row(c).Width(prefInputWidth).Shrink(1).Children(func() {
				inputBox(c, "Font features input", &p.features, "ss01, cv02", &p.featuresH, true, false, featuresLen, func(k ui.TextKey) bool {
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						p.features = k.Text()
						w.act(func() { w.setFeatures(p.features) })
						return true
					}
					return false
				})
			})
			if p.featuresH.FocusWithin(c) {
				p.featuresWas = true
			} else if p.featuresWas { // leaving the field applies it
				p.featuresWas = false
				w.act(func() { w.setFeatures(p.features) })
			}
		}},
		{title: "Cell width", desc: "80–150% of the font's width. Glyphs keep their size, centred in wider columns.", ctl: func(c *ui.Context) {
			numberCtl(c, "Cell width", &p.sliders[keyCellWidth].v, cellWidthMin, cellWidthMax, cellWidthStep)
		}},
	}}
	if isMac() {
		advanced.rows = append(advanced.rows, prefRow{title: "Thicken", desc: "Draw text with a heavier stroke, as on a low-density display.", ctl: func(c *ui.Context) {
			on := w.prefs.Thicken
			ui.Switch(c, &on).Label("Thicken switch").Shrink(0)
			if on != w.prefs.Thicken {
				w.act(func() { w.setThicken(on) })
			}
		}})
	}
	colors := prefGroup{title: "Colors", rows: []prefRow{
		{title: "Improve text contrast", desc: "Lighten or darken text a program draws too faint for its background, keeping its hue. Some is 3:1, High 4.5:1.", ctl: func(c *ui.Context) {
			ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
				segControl(c, "Improve text contrast", w.prefs.Contrast, contrastModes, func(v string) { w.act(func() { w.setContrast(v) }) })
			})
		}},
		{title: "Selection background", desc: "The system accent, or a colour of the terminal palette.", ctl: func(c *ui.Context) {
			ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
				picker(c, "Selection background", w.prefs.SelectionBg, selEntries("System accent"), func(v string) { w.act(func() { w.setSelectionBg(v) }) })
			})
		}},
		{title: "Selection text", desc: "Selected text keeps its own colour, or takes one of the palette.", ctl: func(c *ui.Context) {
			ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
				picker(c, "Selection text", w.prefs.SelectionText, selEntries("Its own colour"), func(v string) { w.act(func() { w.setSelectionText(v) }) })
			})
		}},
	}}
	cursor := prefGroup{title: "Cursor", rows: []prefRow{
		{title: "Cursor style", desc: "Applies to panes opened from now on. Programs such as vim may change it.", ctl: func(c *ui.Context) {
			ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
				segControl(c, "Cursor style", w.prefs.CursorStyle, cursorStyles, func(v string) { w.act(func() { w.setCursorStyle(v) }) })
			})
		}},
		{title: "Blink", desc: "Applies to panes opened from now on.", ctl: func(c *ui.Context) {
			on := w.prefs.CursorBlink
			ui.Switch(c, &on).Label("Blink switch").Shrink(0)
			if on != w.prefs.CursorBlink {
				w.act(func() { w.setCursorBlink(on) })
			}
		}},
	}}
	input := prefGroup{title: "Input", rows: []prefRow{
		{title: "Copy on select", desc: "Copy terminal selection to the clipboard.", ctl: func(c *ui.Context) {
			on := w.prefs.CopyOnSelect
			ui.Switch(c, &on).Label("Copy on select switch").Shrink(0)
			if on != w.prefs.CopyOnSelect {
				w.act(func() { w.setCopyOnSelect(on) })
			}
		}},
		{title: "Play animation on copy", desc: "Copied text flashes in the selection's colour. Off with reduced motion.", ctl: func(c *ui.Context) {
			on := w.prefs.CopyFlash
			ui.Switch(c, &on).Label("Play animation on copy switch").Shrink(0)
			if on != w.prefs.CopyFlash {
				w.act(func() { w.setCopyFlash(on) })
			}
		}},
		{title: "Right-click in the terminal",
			desc: "Paste the clipboard, open the terminal menu, or do nothing. With Menu or Nothing, ⇧ right-click opens the system's menu.",
			ctl: func(c *ui.Context) {
				ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
					segControl(c, "Right-click in the terminal", w.prefs.RightClick, rightClickModes, func(v string) { w.act(func() { w.setRightClick(v) }) })
				})
			}},
	}}
	if isMac() {
		input.rows = append(input.rows, prefRow{title: "Option key",
			desc: "Type special characters (⌥E é), or send Alt to programs, as Meta in Emacs and readline. Applies to panes opened from now on.",
			ctl: func(c *ui.Context) {
				ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
					segControl(c, "Option key", w.prefs.OptionKey, optionModes, func(v string) { w.act(func() { w.setOptionKey(v) }) })
				})
			}})
	}
	return settingsSection{id: "terminal", title: "Terminal", groups: append(session, advanced, colors, cursor, input)}
}

func (w *workspace) keyboardSection() settingsSection {
	s := settingsSection{id: "keyboard", title: "Keyboard",
		lead: "The keys Slime handles. They are fixed; the command palette shows the same keys beside each action."}
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
				p.name, p.nameSeen = w.prefs.Name, true
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
				case k.Key == ui.KeyEscape && k.Mods == 0:
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
		{title: "A lost passphrase cannot be recovered", desc: "Slime never re-keys or overwrites an existing vault from the unlock screen."},
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
		desc: "Closing the window detaches; reopen Slime to attach. Closing a session tab ends that session.", ctl: alwaysOn}}}
	canStop := p.rt == nil || rt.CanStop // enabled until slime serve says it cannot stop
	stop := prefGroup{title: "Stop", rows: []prefRow{{title: "Stop the Slime runtime",
		desc: "Ends every running session and port forward, then closes slime serve. Reopen Slime to start it again.",
		ctl: func(c *ui.Context) {
			if textBtnOffAs(c, "Stop runtime…", "Stop runtime…", btnDanger, !canStop) {
				w.act(w.askStopRuntime)
			}
		}}}}
	return settingsSection{id: "runtime", title: "Runtime",
		lead: "slime serve owns your sessions and serves this window. It keeps running when the window closes.", groups: []prefGroup{status, window, stop}}
}

func (w *workspace) aboutSection(p *settingsPage) settingsSection {
	rt := keelRuntime(p)
	stat := func(title, value string) prefRow {
		return prefRow{title: title, extra: value, ctl: func(c *ui.Context) { statValue(c, value) }}
	}
	version, build := "unknown", "unknown"
	// The release of the packaged app (its Info.plist); bin/slime has none and shows the commit instead.
	if v := mygo.App.Version(); v != "" {
		version = v
	} else if v := versionOf(rt.Build); v != "" {
		version = v
	}
	if rt.Build != "" {
		build = rt.Build
	}
	link := func(title, url, name string) prefRow {
		return prefRow{title: title, desc: url, ctl: func(c *ui.Context) { w.copyBtn(c, name, url) }}
	}
	return settingsSection{id: "about", title: "About Slime",
		lead: "A local SSH workspace. Go owns SSH and the terminals; this window is a native client of slime serve.",
		groups: []prefGroup{
			{title: "Version", rows: []prefRow{stat("Version", version), stat("Build", build)}},
			{title: "Links", rows: []prefRow{
				link("Source", "https://github.com/KJK-Nice/slime", "Copy link"),
				link("README", "https://github.com/KJK-Nice/slime#readme", "Copy link"),
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
func keelRuntime(p *settingsPage) slimeclient.Runtime {
	if p.rt == nil {
		return slimeclient.Runtime{}
	}
	return *p.rt
}

// termPreview is Typography's sample: a prompt, a warning and a bold line, in the terminal's font, weight, size and line
// height, on the terminal's ground.
func (w *workspace) termPreview(c *ui.Context) {
	tf := w.prefs.termFont()
	size, wt := float32(w.prefs.TermSize), tf.Weight
	m := ui.Font{Family: tf.Family, Size: size, Weight: wt}.Metrics()
	row := (m.Ascent + m.Descent + m.LineGap) * tf.LineHeight // as the terminal sizes a row
	var features []string                                     // as the plugin gives them to the font: -tag is tag=0
	for _, f := range tf.Features {
		if tag, ok := strings.CutPrefix(f, "-"); ok {
			f = tag + "=0"
		}
		features = append(features, strings.TrimPrefix(f, "+"))
	}
	ln := func(s string, col ui.Color, weight int) {
		ui.Text(c, s).TextColor(col).FontSize(size).FontWeight(weight).Font(tf.Family).FontFeatures(features...).Height(row).SingleLine()
	}
	ui.Column(c).FillWidth().Padding(space3).Radius(radiusMd).Background(stage).Border(1, line).Label("terminal preview").Children(func() {
		ln("guest@slime:~ $ make test && make lint", ink, wt)
		ui.Row(c).Height(row).AlignItems(ui.Center).Children(func() { // a prompt and the cursor of new panes
			ui.Text(c, "guest@slime:~ $ ").TextColor(ink).FontSize(size).FontWeight(wt).Font(tf.Family).SingleLine()
			tf.Size = size
			cw := gridFor(tf, 1).cw // the terminal's cell
			cur := ui.Box(c).Background(ink).Label("cursor preview " + w.prefs.CursorStyle)
			switch w.prefs.CursorStyle {
			case cursorBar:
				cur.Width(space0_5).Height(row)
			case cursorUnderline:
				cur.Width(cw).Height(space0_5).AlignSelf(ui.End)
			default:
				cur.Width(cw).Height(row)
			}
		})
		ln("view.go:272: warning: label shrinks", ansi[3], wt)
		ln("if err != nil && n >= 0 { return x => y } // ligatures", inkMuted, wt)
		ln("ok  slime/internal/native  17.6s", ink, min(max(wt+300, 700), 900)) // bold as the terminal draws it
	})
}
