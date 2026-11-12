package native

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Settings · Agent drawn (internal/ui/pages.templ agentSettingsPanel, cmd/keel/web/js/lib/agentsettings.js and the
// k-agent-card* rules of keel.css) as prefRows: the groups, controls, labels and help texts are the app's, in its order.
//
// "Notes for the agent" (Your notes, Delete all notes) is notes.go's: the list saves each change at once, and Delete all notes
// asks first.

func (w *workspace) agentSection(p *settingsPage) settingsSection {
	s := w.agentVal()
	v := s.Settings
	str := func(x string) *string { return &x }
	save := func(patch keelclient.AgentSettingsPatch) { w.act(func() { w.putAgent(patch, nil) }) }
	jevOn := s.Keys["jev"].Configured
	suggestOff := v.Suggestions == "off"

	provider := prefGroup{title: "Provider", rows: []prefRow{
		{preview: true, ctl: func(c *ui.Context) { w.providerCards(c, p, s) }},
		{title: "Model", desc: "Used by the agent panel. Suggestions in panes use the provider's small, fast model.", ctl: func(c *ui.Context) {
			items := modelItems(w.agent.provs, v.Provider, v.Model)
			entries := make([]pickEntry, len(items))
			for i, it := range items {
				entries[i] = pickEntry{value: it.value, label: it.label}
			}
			ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
				picker(c, "Model", v.Model, entries, func(m string) {
					if m == v.Model {
						return
					}
					patch := keelclient.AgentSettingsPatch{Model: str(m)} // a model names its provider: picking one switches to it
					for _, it := range items {
						if it.value == m && it.provider != "" && it.provider != v.Provider {
							patch.Provider = str(it.provider)
						}
					}
					save(patch)
				})
			})
		}},
	}}

	suggest := prefGroup{title: "Suggestions in panes", rows: []prefRow{
		{title: "Suggest a command when I type plain language", desc: "Press ⇥ to see it. Nothing is inserted until you accept, and nothing runs until you press ↵.",
			ctl: w.switchCtl("Suggest commands", v.Suggestions != "off", func(on bool) { save(keelclient.AgentSettingsPatch{Suggestions: str(onOff(on))}) })},
		{title: "Suggest from this pane's scrollback", desc: "Offers a command already shown at the pane's prompt. Up to 40 recent commands from the pane are sent to TypeSafe Jev, redacted, each time you press ⇥.",
			ctl: w.switchCtl("Suggest from this pane's scrollback", v.HistorySuggestions != "off", func(on bool) { save(keelclient.AgentSettingsPatch{HistorySuggestions: str(onOff(on))}) })},
		{title: "How to notice plain language", stack: true, ctl: func(c *ui.Context) {
			radioGroup(c, "How to notice plain language", [][2]string{{detectionAuto, "Notice automatically"}, {detectionHashOnly, "Only lines starting with #"}},
				v.Detection, suggestOff, map[string]string{detectionAuto: "Needs a Jev key"}, func(d string) { save(keelclient.AgentSettingsPatch{Detection: str(d)}) })
			if n := detectionNote(v.Suggestions, v.Detection, jevOn); n != "" {
				ui.Text(c, n).TextColor(inkMuted).FontSize(captionSize)
			}
		}},
	}}

	panel := prefGroup{title: "Agent panel", rows: []prefRow{
		{title: "Default panes per conversation", desc: "Which panes the agent may read when you start a conversation.", stack: true, ctl: func(c *ui.Context) {
			radioGroup(c, "Default panes per conversation", agentScopes, v.DefaultScope, false, nil, func(sc string) { save(keelclient.AgentSettingsPatch{DefaultScope: str(sc)}) })
		}},
		{title: "Scrollback lines per pane", desc: "50–1000. How much of each pane the agent reads. Default 200.", ctl: func(c *ui.Context) {
			sliderCtlStep(c, "Scrollback lines", &p.linesV, linesMin, linesMax, linesStep, clampLines(int(p.linesV+0.5)))
		}},
	}}

	claude := prefGroup{title: "Claude Code", rows: []prefRow{
		{title: "Allow Claude Code (keel mcp)",
			desc: "Lets Claude Code list and read panes on hosts with Agent can read, each read logged, and propose commands that wait in the agent panel. It never types.",
			ctl:  w.switchCtl("Allow Claude Code (keel mcp)", v.ClaudeCode, func(on bool) { save(keelclient.AgentSettingsPatch{ClaudeCode: &on}) })},
	}}

	notes := prefGroup{title: "Notes for the agent", rows: []prefRow{
		{title: "Your notes", desc: "How you like to work, for every host. Sent with questions that read a pane on a host the agent may read, redacted like pane text. A host's own notes are in Edit host.", stack: true,
			extra: "Notes about you", ctl: func(c *ui.Context) {
				w.notesBlock(c, &p.notes, notesBlockOpts{title: "Notes about you", placeholder: "Add a note, e.g. Prefer journalctl over tail -f",
					help: "The agent may propose a note; nothing is saved until you press Save. Stored encrypted in the vault."})
			}},
		{title: "Delete all notes", desc: "Forget every host note and every note about you. Conversations stay.", ctl: func(c *ui.Context) {
			if textBtnAs(c, "Delete all notes…", "Delete all notes", btnDanger) { // it asks first
				w.act(w.askDeleteAllNotes)
			}
		}},
	}}

	redact := func(ips, hosts bool) keelclient.AgentSettingsPatch {
		return keelclient.AgentSettingsPatch{Redact: &keelclient.AgentRedact{Secrets: true, IPs: ips, Hostnames: hosts}}
	}
	safety := prefGroup{title: "Safety", rows: []prefRow{
		{title: "The agent never types into a pane", desc: "Only you do, with Type, ⇥ or ⇧⇥. Nothing runs until you press ↵.", ctl: alwaysOn},
		{title: "Secrets are always redacted", desc: "Values that look like tokens, keys and passwords are replaced before anything is sent. The vault is never sent.", ctl: alwaysOn},
		{title: "Risky commands need ⇧⇥", desc: "Plain ⇥ never inserts a destructive command. On production hosts a command that changes state needs ⇧⇥ too. Read-only commands insert with ⇥ everywhere.", ctl: alwaysOn},
		{title: "Redact IP addresses", desc: "Replace IPv4 and IPv6 addresses before sending.",
			ctl: w.switchCtl("Redact IP addresses", v.Redact.IPs, func(on bool) { save(redact(on, v.Redact.Hostnames)) })},
		{title: "Redact hostnames", desc: "Replace host names and domains before sending.",
			ctl: w.switchCtl("Redact hostnames", v.Redact.Hostnames, func(on bool) { save(redact(v.Redact.IPs, on)) })},
	}}

	history := prefGroup{title: "History", rows: []prefRow{
		{title: "Keep conversations", desc: "Saved encrypted in the vault, redacted as sent, and deleted when older than this. Only while open never writes them to disk.",
			ctl: func(c *ui.Context) {
				entries := make([]pickEntry, len(historyChoices))
				for i, ch := range historyChoices {
					entries[i] = pickEntry{value: ch[0], label: ch[1]}
				}
				ui.Row(c).Width(sizeRail).Shrink(0).Children(func() {
					picker(c, "Keep conversations", historyValue(v.HistoryDays), entries, func(d string) {
						if n, err := strconv.Atoi(d); err == nil && d != historyValue(v.HistoryDays) {
							save(keelclient.AgentSettingsPatch{HistoryDays: &n})
						}
					})
				})
			}},
		{title: "Delete all history", desc: "Forget every conversation now. The Agent log keeps what was read.", ctl: func(c *ui.Context) {
			if textBtnAs(c, "Delete all history…", "Delete all history", btnDanger) { // the dialog follows
				w.act(w.askDeleteHistory)
			}
		}},
	}}

	return settingsSection{id: "agent", title: "Agent",
		lead:   "The agent reads only the panes you allow and suggests commands. It never types into a pane on its own.",
		groups: []prefGroup{provider, suggest, panel, claude, notes, safety, history}}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// switchCtl is a k-switch row control: the switch shows the saved value, and a press sends the new one.
func (w *workspace) switchCtl(name string, value bool, set func(bool)) func(*ui.Context) {
	return func(c *ui.Context) {
		on := value
		ui.Switch(c, &on).Label(name + " switch").Shrink(0)
		if on != value {
			set(on)
		}
	}
}

// sliderCtlStep is sliderCtl with a step and a value in its own output (Scrollback lines is 50–1000 in steps of 10).
func sliderCtlStep(c *ui.Context, name string, v *float64, lo, hi, step float64, shown int) {
	ui.Row(c).Gap(space3).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.StepSlider(c, v, lo, hi, step).Label(name + " slider").Width(rem(sliderPx))
		ui.Row(c).MinWidth(space6 + space3).Justify(ui.Center).Children(func() { body(c, strconv.Itoa(shown), ink).Label(name + " value") })
	})
}

// radioGroup is a k-radios group: one choice is on; a badge sits beside a choice's words; off greys the group.
func radioGroup(c *ui.Context, name string, opts [][2]string, current string, off bool, badges map[string]string, pick func(string)) {
	sel := current
	ui.RadioGroup(c, func() {
		for _, o := range opts {
			ui.Row(c).FillWidth().Gap(space2).Key(o[0]).Children(func() {
				r := ui.Radio(c, &sel, o[0], o[1]).Label(name + " " + o[1])
				if off {
					r.Disabled(true)
				}
				if b := badges[o[0]]; b != "" {
					outlineBadge(c, b)
				}
			})
		}
	}).Label(name)
	if sel != current && !off {
		pick(sel)
	}
}

// ---- the provider cards ----

func (w *workspace) providerCards(c *ui.Context, p *settingsPage, s keelclient.AgentSettings) {
	ui.Column(c).FillWidth().Gap(space3).Label("agent providers").Children(func() {
		if len(w.agent.provs) == 0 {
			help(c, "Reading the providers…")
		}
		for _, d := range w.agent.provs {
			w.providerCard(c, p, d, s.Keys[d.ID].Configured)
		}
	})
}

// providerCard is one provider as a card (agentsettings.js providerCardHTML): its name and role, what it is for, whether
// it is connected, Change key and Disconnect when it is, and the masked key field when it is not (or when Change key opens it).
func (w *workspace) providerCard(c *ui.Context, p *settingsPage, d keelclient.AgentProvider, configured bool) {
	card := p.card(d.ID)
	role, glyph := "Model", "ai-magic"
	if d.Kind == "detector" {
		role, glyph = "Plain-language detection", "lock"
	}
	ui.Column(c).FillWidth().Padding(space3).Gap(space3).Radius(radiusMd).Background(controlFill(raised)).Border(1, line).Key(d.ID).Label("agent card " + d.Name).Children(func() {
		ui.Row(c).FillWidth().Gap(space3).AlignItems(ui.Start).Children(func() {
			ui.Row(c).Size(sizeControl, sizeControl).Radius(radiusSm).Background(hoverGround).Justify(ui.Center).Shrink(0).Children(func() { icon(c, glyph, brassInk) })
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space0_5).Children(func() {
				ui.Row(c).Gap(space2).AlignItems(ui.Center).Children(func() {
					body(c, d.Name, ink)
					caption(c, role, inkFaint)
				})
				help(c, providerPurpose(d.ID))
			})
			if configured {
				ui.Row(c).Height(space4).Padding(0, space1_5).Gap(space1).Radius(radiusXs).Justify(ui.Center).Shrink(0).Background(tideSoft).Label("badge Connected " + d.Name).Children(func() {
					dot(c, tide)
					text(c, "Connected", tide, kbdSize, weightSemibold)
				})
			} else {
				ui.Row(c).Shrink(0).Label("badge Not connected " + d.Name).Children(func() { outlineBadge(c, "Not connected") })
			}
		})
		if configured {
			ui.Row(c).Gap(space2).Children(func() {
				if textBtnAs(c, "Change key "+d.Name, "Change key", btnDefault) {
					w.act(func() { card.changing, card.err, card.focus = !card.changing, "", true })
				}
				if textBtnAs(c, "Disconnect "+d.Name, "Disconnect", btnDanger) {
					id := d.ID
					w.act(func() { w.askDisconnect(id) })
				}
			})
		}
		if !configured || card.changing {
			w.keyForm(c, p, d, card, configured)
		}
	})
}

// keyForm is the key field of a provider card: shown when the provider is not connected, and when Change key opens it. The
// field is masked, its key goes out once and the field is emptied (saveCardKey).
func (w *workspace) keyForm(c *ui.Context, p *settingsPage, d keelclient.AgentProvider, card *keyCard, changing bool) {
	name := orElse(d.KeyLabel, "API key")
	ui.Column(c).FillWidth().Gap(space2).Label("agent key form " + d.Name).Children(func() {
		if d.Warning != "" {
			ui.Row(c).FillWidth().Padding(space2, space3).Gap(space2).Radius(radiusSm).Background(signalSoft).AlignItems(ui.Start).Label("agent warning " + d.Name).Children(func() {
				icon(c, "alert", signal)
				ui.Text(c, d.Warning).TextColor(ink).FontSize(captionSize).Grow(1).Shrink(1).MinWidth(0)
			})
		}
		placeholder := "Paste the key"
		btn := "Connect"
		if changing {
			placeholder, btn = "Paste the new key", "Save key"
		}
		id := d.ID
		labeled(c, name, &card.h, func() {
			inputBox(c, name+" input", &card.field, placeholder, &card.h, true, true, 0, func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 {
					w.act(func() { w.saveCardKey(p, id) })
					return true
				}
				return false
			})
		})
		if card.err != "" {
			ui.Text(c, card.err).TextColor(flare).FontSize(captionSize)
		}
		ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
			if !card.busy && textBtnAs(c, btn+" "+d.Name, btn, btnPrimary) {
				w.act(func() { w.saveCardKey(p, id) })
			}
			if changing && textBtnAs(c, "Cancel "+d.Name, "Cancel", btnGhost) {
				w.act(func() { *card = keyCard{} })
			}
			ui.Spacer(c)
			if u := safeURL(d.KeyHelpURL); u != "" {
				ui.Link(c, "Get a key", u).TextColor(brassInk).FontSize(captionSize)
			}
			if u := safeURL(d.TermsURL); u != "" {
				ui.Link(c, "Terms", u).TextColor(brassInk).FontSize(captionSize)
			}
		})
	})
	if card.focus {
		card.h.Focus(c)
		card.focus = false
	}
}
