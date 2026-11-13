package native

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Group page drawn as GroupPage (internal/ui/groupspage.templ) with the k-gx-* rules of keel.css and groups.js: the head
// (the folder's name, "Folder · N hosts · production", Run a runbook on this group, Export to Ansible), the role chips, the
// hosts table (host, address, role, tags, agent), the group notes and the variables, and the export drawer beside the page.

// memberCols are the table's columns as weights: Host, Address, Role, Tags, Agent.
var memberCols = [5]float32{3, 3, 2.4, 2.4, 2.6}

func (w *workspace) groupPageView(c *ui.Context, p *groupPage) {
	ui.Row(c).Grow(1).FillWidth().FillHeight().Children(func() {
		ui.Scroll(c).Grow(1).FillHeight().Shrink(1).MinWidth(0).Background(w.ground(stage)).Label("Group").Children(func() {
			ui.Column(c).FillWidth().Padding(space6, space8).Gap(space4).Children(func() {
				w.groupHead(c, p)
				w.groupRoleChips(c, p)
				if p.err != "" {
					ui.Row(c).FillWidth().Label("group error").Children(func() { caption(c, p.err, flare) })
				}
				w.groupHostsTable(c, p)
				ui.Row(c).FillWidth().Gap(space4).Wrap().AlignItems(ui.Start).Children(func() {
					ui.Column(c).Grow(1).Basis(sizeRail).MinWidth(0).Padding(space3).Radius(radiusMd).Border(1, line).Background(w.ground(surface)).Label("group notes").Children(func() {
						w.notesBlock(c, &p.notes, notesBlockOpts{title: groupNotesTitle, placeholder: groupNotesHint, help: groupNotesHelp})
					})
					w.groupVarsBox(c, p)
				})
			})
		})
		if p.ex.open {
			w.exportDrawerView(c, p)
		}
	})
}

func (w *workspace) groupHead(c *ui.Context, p *groupPage) {
	name := orElse(p.title, "Group")
	ui.Row(c).FillWidth().Gap(space3).AlignItems(ui.Start).Wrap().Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space1).Children(func() {
			ui.Text(c, name).TextColor(ink).FontSize(titleLg).FontWeight(weightSemibold).MaxLines(1).Label("group title")
			if p.loaded {
				help(c, groupLine(p.members, w.hosts)+".")
			}
		})
		if textBtnIcon(c, "group-run", "Run a runbook on this group", btnDefault) {
			w.act(func() { w.toggleRunMenu(p) })
		}
		if textBtnIcon(c, "book", "Export to Ansible", btnPrimary) {
			w.act(func() { w.toggleExport(p) })
		}
	})
	if p.run.open {
		w.groupRunMenu(c, p)
	}
}

// groupRunMenu is the list under "Run a runbook on this group": each runbook with its steps and roles.
func (w *workspace) groupRunMenu(c *ui.Context, p *groupPage) {
	ui.Column(c).Width(sizeRail*1.6).Padding(space1).Gap(space0_5).Radius(radiusSm).Background(raised).Border(1, line).Label("group runbooks").Children(func() {
		switch {
		case p.run.err != "":
			help(c, p.run.err)
			return
		case p.run.loading && len(p.run.books) == 0:
			waitHelp(c, "Loading runbooks…")
			return
		case len(p.run.books) == 0:
			if emptySide(c, emptySentences("book", groupNoBooks, "Open Runbooks")) {
				w.act(func() { w.openPage("runbooks") })
			}
			return
		}
		menuLabel(c, "Run on "+orElse(p.title, "this group"))
		for _, b := range p.run.books {
			r := ui.Column(c).FillWidth().Key(b.ID).Padding(space1_5, space2).Radius(radiusSm).Cursor(ui.CursorPointer).Label("run runbook " + b.Name)
			if r.Hovered() {
				r.Background(hoverGround)
			}
			r.Children(func() {
				body(c, b.Name, ink)
				caption(c, runbookGroupSub(b), inkFaint)
			})
			if r.Clicked() {
				id := b.ID
				w.act(func() { w.startGroupRun(p, id) })
			}
		}
	})
}

// runbookGroupSub is "3 steps · Roles worker, control-plane" or "2 steps · Every host" (groups.js openMenu).
func runbookGroupSub(b keelclient.Runbook) string {
	var roles []string
	seen := map[string]bool{}
	for _, s := range b.Steps {
		if s.Role != "" && !seen[s.Role] {
			seen[s.Role] = true
			roles = append(roles, s.Role)
		}
	}
	sub := "Every host"
	if len(roles) > 0 {
		sub = "Roles " + joinWords(roles)
	}
	return countWord(len(b.Steps), "step", "steps") + " · " + sub
}

func (w *workspace) groupRoleChips(c *ui.Context, p *groupPage) {
	ui.Row(c).FillWidth().Gap(space3).Wrap().AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(space1_5).Wrap().Label("Hosts per role").Children(func() {
			for _, ch := range roleCounts(p.members) {
				fg := ink
				if ch.none {
					fg = inkMuted
				}
				ui.Row(c).Height(sizeControlSm).Padding(0, space2).Gap(space1).Radius(radiusFull).Background(hoverGround).AlignItems(ui.Center).
					Label(fmt.Sprintf("%d %s", ch.count, ch.role)).Key(ch.role).Children(func() {
					text(c, fmt.Sprint(ch.count), fg, roleChipSize, weightSemibold)
					text(c, ch.role, fg, roleChipSize, weightRegular)
				})
			}
		})
		help(c, groupRoleHelp)
	})
}

func (w *workspace) groupHostsTable(c *ui.Context, p *groupPage) {
	byID := map[string]keelclient.Host{}
	for _, h := range w.hosts {
		byID[h.ID] = h
	}
	roles := roleNames(p.members)
	ui.Column(c).FillWidth().Radius(radiusMd).Background(w.ground(surface)).Border(1, line).Clip().Label("Hosts in this group").Children(func() {
		cell := func(i int) ui.Element {
			return ui.Row(c).Grow(memberCols[i]).Basis(0).MinWidth(0).Gap(space2).AlignItems(ui.Center)
		}
		hairline(ui.Row(c).FillWidth().Height(sizeRow).Padding(0, space3).Gap(space3).Shrink(0), 0, 0, 1, 0).Children(func() {
			for i, n := range []string{"Host", "Address", "Role", "Tags", "Agent"} {
				cell(i).Children(func() { label(c, n, inkMuted) })
			}
		})
		if len(p.members) == 0 {
			if p.loaded {
				emptySide(c, emptySentences("server", groupNoHosts, ""))
			}
			return
		}
		for _, m := range p.members {
			h := byID[m.HostID]
			ui.Row(c).FillWidth().MinHeight(sizeRow).Padding(space1_5, space3).Gap(space3).Key(m.HostID).AlignItems(ui.Center).Label("member " + m.Name).Children(func() {
				cell(0).Children(func() {
					ui.Row(c).Size(sizeTileSm, sizeTileSm).Radius(radiusXs).Justify(ui.Center).Shrink(0).
						Background(hostTint(h.Color).Alpha(tileTintAlpha)).Children(func() { iconSized(c, "server", hostTint(h.Color), iconSizeSm) })
					body(c, m.Name, ink).Shrink(1).MinWidth(0)
					if m.Folder != "" && m.Folder != p.group.Name {
						caption(c, m.Folder, inkFaint).Shrink(1).MinWidth(0)
					}
				})
				cell(1).Children(func() { mono(c, memberAddr(h), inkMuted) })
				cell(2).Children(func() { w.rolePicker(c, p, m, roles) })
				cell(3).Children(func() {
					for _, t := range h.Tags {
						pill(c, t, hoverGround, inkMuted)
					}
				})
				cell(4).Children(func() {
					if !h.AgentMayRead() {
						pill(c, "Agent cannot read", hoverGround, inkMuted)
					} else {
						pill(c, "Can read", hazeSoft, haze)
					}
					if h.IsProduction() {
						pill(c, "Production", signalSoft, signal)
					}
				})
			})
		}
	})
}

// memberAddr is the address column: user@address, "this machine" for a local shell.
func memberAddr(h keelclient.Host) string {
	switch {
	case h.Address != "":
		if h.Username != "" {
			return h.Username + "@" + h.Address
		}
		return h.Address
	case h.IsLocal():
		return "this machine"
	}
	return ""
}

// rolePicker is a host's Role: the group's roles, No role and New role… (which turns into a field).
func (w *workspace) rolePicker(c *ui.Context, p *groupPage, m keelclient.GroupMember, roles []string) {
	if p.roleFor == m.HostID {
		in := ui.TextInputBase(c, &p.roleText).Label("New role for " + m.Name).Placeholder("e.g. worker").PlaceholderColor(inkFaint).TextColor(ink).
			FontSize(uiFontSize).Font(monoStack).MaxLength(32).Grow(1).MinWidth(0).Bind(&p.roleH)
		in.OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEnter && k.Mods == 0:
				p.roleText = k.Text()
				w.act(func() { w.endNewRole(p, true) })
			case k.Key == ui.KeyEscape && k.Mods == 0:
				w.act(func() { w.endNewRole(p, false) })
			default:
				return false
			}
			return true
		})
		if !p.roleSeen { // the field takes the keyboard once; losing it keeps what was typed
			p.roleH.Focus(c)
		}
		if p.roleH.FocusWithin(c) {
			p.roleSeen = true
		} else if p.roleSeen {
			w.act(func() { w.endNewRole(p, true) })
		}
		return
	}
	entries := []pickEntry{{"", "No role", ""}}
	for _, r := range roles {
		entries = append(entries, pickEntry{r, r, ""})
	}
	entries = append(entries, pickEntry{groupNewRole, "New role…", ""})
	picker(c, "Role of "+m.Name, m.Role, entries, func(v string) { w.act(func() { w.pickRole(p, m.HostID, v) }) })
}

// ---- variables ----

func (w *workspace) groupVarsBox(c *ui.Context, p *groupPage) {
	ui.Column(c).Grow(1).Basis(sizeRail).MinWidth(0).Padding(space3).Gap(space2).Radius(radiusMd).Border(1, line).Background(w.ground(surface)).Label("Group variables").Children(func() {
		ui.Row(c).FillWidth().Gap(space1_5).AlignItems(ui.Center).Children(func() {
			text(c, "Group variables", ink, uiFontSize, weightSemibold)
			pill(c, fmt.Sprint(len(p.group.Vars)), hoverGround, inkMuted).Label("group variables count")
		})
		ui.Column(c).FillWidth().Gap(space1_5).Label("group variables list").Children(func() {
			if len(p.group.Vars) == 0 {
				emptySide(c, emptySentences("group", groupVarsEmpty, "")) // the Add row below is the action
			}
			for _, v := range p.group.Vars {
				w.varRow(c, p, v)
			}
		})
		ui.Row(c).FillWidth().Gap(space2).Children(func() {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				inputBox(c, "Variable name", &p.varName, "api_vip", &p.varNameH, true, false, 32, func(k ui.TextKey) bool {
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						w.act(func() { w.addVar(p) })
						return true
					}
					return false
				})
			})
			ui.Row(c).Grow(2).Basis(0).MinWidth(0).Children(func() {
				inputBox(c, "Variable value", &p.varValue, "10.20.0.10", &p.varValueH, true, false, maxVarValue, func(k ui.TextKey) bool {
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						w.act(func() { w.addVar(p) })
						return true
					}
					return false
				})
			})
			if textBtn(c, "Add variable", btnDefault) {
				w.act(func() { w.addVar(p) })
			}
		})
		if p.varNameFocus {
			p.varNameH.Focus(c)
			p.varNameFocus = false
		}
		if p.varsErr != "" {
			ui.Row(c).FillWidth().Label("group variables error").Children(func() { caption(c, p.varsErr, flare) })
		}
		help(c, groupVarsHelp)
	})
}

// varRow is one variable: its name and value, Edit and Remove; or the field the value is edited in.
func (w *workspace) varRow(c *ui.Context, p *groupPage, v keelclient.GroupVar) {
	ui.Row(c).FillWidth().Gap(space2).Key(v.Name).AlignItems(ui.Center).Label("variable " + v.Name).Children(func() {
		mono(c, v.Name, ink).Width(sizeRail / 2).Shrink(0)
		if p.varEdit == v.Name {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				inputBox(c, "Value of "+v.Name, &p.varEditTxt, "", &p.varEditH, true, false, maxVarValue, func(k ui.TextKey) bool {
					switch {
					case k.Key == ui.KeyEnter && k.Mods == 0:
						p.varEditTxt = k.Text()
						w.act(func() { w.saveEditVar(p) })
					case k.Key == ui.KeyEscape && k.Mods == 0:
						w.act(func() { p.varEdit = "" })
					default:
						return false
					}
					return true
				})
			})
			if p.varFocusEdit {
				p.varEditH.Focus(c)
				p.varFocusEdit = false
			}
			return
		}
		mono(c, v.Value, inkMuted).Grow(1).Shrink(1).MinWidth(0)
		if textBtn(c, "Edit "+v.Name, btnGhost) {
			w.act(func() { w.startEditVar(p, v) })
		}
		if textBtn(c, "Remove "+v.Name, btnGhost) {
			w.act(func() { w.removeVar(p, v.Name) })
		}
	})
}

// ---- Export to Ansible ----

func (w *workspace) exportDrawerView(c *ui.Context, p *groupPage) {
	ex := &p.ex
	hairline(ui.Column(c).Width(sizeAgent+40).Shrink(0).FillHeight().Background(w.ground(surface)), 0, 0, 0, 1).Label("Export to Ansible").Children(func() {
		hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space3, space4).Gap(space2).AlignItems(ui.Center), 0, 0, 1, 0).Children(func() {
			icon(c, "book", inkMuted)
			text(c, "Export to Ansible", ink, uiFontSize, weightSemibold).Grow(1)
			if iconBtn(c, "cancel-01", "Close export", sizeControlSm, false) {
				w.act(func() { w.toggleExport(p) })
			}
		})
		ui.Scroll(c).FillWidth().Grow(1).Shrink(1).Children(func() {
			ui.Column(c).FillWidth().Padding(space4).Gap(space3).Children(func() {
				help(c, groupExportHelp)
				segControl(c, "Inventory format", ex.format, [][2]string{{"ini", "INI"}, {"yaml", "YAML"}}, func(v string) {
					w.act(func() { ex.format, ex.file = v, 0; w.exportRefresh(p) })
				})
				entries := []pickEntry{{"", "Inventory only", ""}}
				for _, b := range p.run.books {
					entries = append(entries, pickEntry{b.ID, "Playbook: " + b.Name, countWord(len(b.Steps), "step", "steps")})
				}
				picker(c, "Playbook from a runbook", ex.runbook, entries, func(v string) {
					w.act(func() {
						ex.runbook = v
						ex.file = 0
						if v != "" {
							ex.file = 1 // the playbook, as the app shows it
						}
						w.exportRefresh(p)
					})
				})
				if ex.runbook != "" {
					segControl(c, "Playbook mode", ex.mode, [][2]string{{"rolling", "Rolling"}, {"parallel", "Parallel"}}, func(v string) {
						w.act(func() { ex.mode = v; w.exportRefresh(p) })
					})
				}
				ui.Row(c).FillWidth().Gap(space1_5).Wrap().Label("Files").Children(func() {
					for i, f := range ex.files {
						on := i == ex.file
						r := ui.Row(c).Height(sizeControlSm).Padding(0, space2).Radius(radiusSm).Justify(ui.Center).Label("file " + f.Name).Cursor(ui.CursorPointer).Key(f.Name)
						bg, fg := hoverGround, inkMuted
						if on {
							bg, fg = raised, ink
							r.Shadow(0, 0, 0, 1, line)
						}
						r.Background(bg).Children(func() { mono(c, f.Name, fg) })
						if r.Clicked() {
							idx := i
							w.act(func() { ex.file = idx })
						}
					}
				})
				code := ""
				if ex.file < len(ex.files) {
					code = ex.files[ex.file].Content
				}
				ui.Scroll(c).FillWidth().MaxHeight(sizeAgent).Padding(space2, space3).Radius(radiusSm).Background(stage).Border(1, line).Label("File contents").Children(func() {
					ui.Text(c, code).TextColor(ink).FontSize(captionSize).Font(monoStack).MaxLines(0)
				})
				if ex.file < len(ex.files) {
					help(c, fileMeta(ex.files[ex.file]))
				}
				ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
					if checkBox(c, "Use ansible_user from each host's saved user", boolCheck(ex.user), false) {
						w.act(func() { ex.user = !ex.user; w.exportRefresh(p) })
					}
					help(c, "Use ansible_user from each host's saved user")
				})
				ui.Row(c).FillWidth().Gap(space2).AlignItems(ui.Center).Children(func() {
					if checkBox(c, "Include the jump host as ansible_ssh_common_args", boolCheck(ex.jump), false) {
						w.act(func() { ex.jump = !ex.jump; w.exportRefresh(p) })
					}
					help(c, "Include the jump host as ansible_ssh_common_args")
				})
				if ex.err != "" {
					ui.Row(c).FillWidth().Label("export error").Children(func() { caption(c, ex.err, flare) })
				}
				help(c, groupExportSafe)
				if d := dryRun(ex.files); d != "" {
					mono(c, "Try it first: "+d, inkMuted).MaxLines(0)
				}
			})
		})
		hairline(ui.Row(c).FillWidth().Shrink(0).Padding(space3, space4).Gap(space2).Justify(ui.End), 1, 0, 0, 0).Children(func() {
			if textBtnOff(c, "Copy", btnDefault, ex.file >= len(ex.files)) {
				w.act(func() {
					if ex.file < len(ex.files) {
						f := ex.files[ex.file]
						c.WriteClipboard(f.Content)
						w.say("Copied " + f.Name + ".")
					}
				})
			}
			// Download (groups.js): the shown file's text, saved where the system's save dialog says. Nothing is written by
			// keel serve and nothing runs.
			if textBtnOff(c, "Download", btnPrimary, ex.file >= len(ex.files)) {
				w.act(func() {
					if ex.file < len(ex.files) {
						w.downloadFile(ex.files[ex.file].Name, ex.files[ex.file].Content)
					}
				})
			}
		})
	})
}

func boolCheck(on bool) checkState {
	if on {
		return checkOn
	}
	return checkOff
}
