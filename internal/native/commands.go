package native

import (
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/layout"
)

// platform is the OS the window runs on, decided once. Every choice of a key or a label by platform reads it through isMac;
// a test that needs another platform sets it (usePlatform in commands_test.go). Real capabilities of the OS (the blur, the
// native appearance, kerning) read it too; the font registration in run.go asks the OS itself.
var platform = runtime.GOOS

// isMac is whether this is macOS: ⌘ is the command key, and keys are written as glyphs.
func isMac() bool { return platform == "darwin" }

// digitKeys are the keys 1 to 9: the tab keys (⌘1 to ⌘9), the New tab page's cards (⌥1 to ⌥6) and a suggestion's options.
var digitKeys = [9]ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// chord is a key with its modifiers. mods say ui.Cmd for ⌘, which is Ctrl off macOS.
type chord struct {
	mods  ui.Modifiers
	key   ui.Key
	glyph string // the key on a key cap: "T", "↵", "<"
	accel string // the key in a menu accelerator: "t", "enter", "="; "" is the glyph in lower case
	// shiftImplied: the cap does not show ⇧ because the glyph needs it (⌘< is ⌘⇧,).
	shiftImplied bool
}

// cmdKey is ⌘ and a key; cmdShiftKey is ⌘⇧ and a key.
func cmdKey(key ui.Key, glyph string) *chord { return &chord{mods: ui.Cmd, key: key, glyph: glyph} }
func cmdShiftKey(key ui.Key, glyph string) *chord {
	return &chord{mods: ui.Cmd | ui.Shift, key: key, glyph: glyph}
}

// label is the key cap text: ⌘⇧T on macOS, Ctrl+Shift+T elsewhere.
func (k chord) label() string {
	shift := k.mods&ui.Shift != 0 && !k.shiftImplied
	if isMac() {
		s := ""
		if k.mods&ui.Cmd != 0 {
			s += "⌘"
		}
		if shift {
			s += "⇧"
		}
		if k.mods&ui.Alt != 0 {
			s += "⌥"
		}
		return s + k.glyph
	}
	var parts []string
	if k.mods&ui.Cmd != 0 {
		parts = append(parts, "Ctrl")
	}
	if shift {
		parts = append(parts, "Shift")
	}
	if k.mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	return strings.Join(append(parts, k.glyph), "+")
}

// accelerator is the key as a menu bar accelerator: CmdOrCtrl+Shift+t.
func (k chord) accelerator() string {
	s := ""
	if k.mods&ui.Cmd != 0 {
		s += "CmdOrCtrl+"
	}
	if k.mods&ui.Shift != 0 {
		s += "Shift+"
	}
	if k.mods&ui.Alt != 0 {
		s += "Alt+"
	}
	if k.accel != "" {
		return s + k.accel
	}
	return s + strings.ToLower(k.glyph)
}

// command is one thing the window can do, with every surface that shows it: the shortcut, the menu bar, the palette and the
// Keyboard page are all made from this table. A surface a command is not on has a nil spec. Nothing here types into a session.
type command struct {
	id string
	// key is the shortcut on macOS and elsewhere unless other is set; also are more chords that run it and are not shown.
	key, other *chord
	also       []chord
	run        func(w *workspace) // nil: the key is written down but handled elsewhere (⌘↵ in the palette, ⌘Z in the card)
	enabled    func(w *workspace) bool
	menuSep    bool // a separator before its menu item
	menu       *menuSpec
	pal        *palSpec
	help       *helpSpec
}

type menuSpec struct {
	section, title string
	pos            int // the order in the section
}

type palSpec struct {
	section, title, icon string
	hide                 bool                         // the row is left out while the command is not enabled
	run                  func(w *workspace)           // when the palette does what the key does not (New host always makes a host)
	many                 func(w *workspace) []palItem // rows made from the workspace, in this command's place
}

type helpSpec struct{ group, title string }

// chordHere is the key of this platform, nil when the command has none.
func (c *command) chordHere() *chord {
	if !isMac() && c.other != nil {
		return c.other
	}
	return c.key
}

// keyLabel is the key cap text of this platform; "" when there is no key.
func (c *command) keyLabel() string {
	if k := c.chordHere(); k != nil {
		return k.label()
	}
	return ""
}

// chords are the keys that run the command on this platform.
func (c *command) chords() []chord {
	var out []chord
	if k := c.chordHere(); k != nil {
		out = append(out, *k)
	}
	return append(out, c.also...)
}

// invoke runs the command if it is available now.
func (c *command) invoke(w *workspace) {
	if c.run != nil && (c.enabled == nil || c.enabled(w)) {
		c.run(w)
	}
}

// commands is the table. Its order is the palette's and the Keyboard page's; the menu bar sorts by menuSpec.pos. It is made
// in init because the commands reach the palette, which reads the table.
var commands []command

func notJoined(w *workspace) bool { return !w.joinedActive() }

func init() {
	commands = []command{
		{id: "palette", key: cmdKey(ui.KeyK, "K"), also: []chord{*cmdShiftKey(ui.KeyP, "P")}, run: (*workspace).togglePalette,
			help: &helpSpec{"General", "Command palette"}},
		{id: "tab.new", key: cmdKey(ui.KeyT, "T"), run: (*workspace).newTabKey,
			menu: &menuSpec{"File", "New Tab", 1}, pal: &palSpec{section: "Tabs", title: "New tab", icon: "add-01"}, help: &helpSpec{"Tabs", "New tab"}},
		{id: "tab.local", run: (*workspace).newLocalTab, pal: &palSpec{section: "Tabs", title: "New Local Shell"}},
		{id: "tab.close", key: cmdKey(ui.KeyW, "W"), run: (*workspace).closeFocused,
			menu: &menuSpec{"File", "Close Tab", 2}, pal: &palSpec{section: "Tabs", title: "Close pane or tab"}, help: &helpSpec{"Tabs", "Close pane or tab"}},
		{id: "tab.reopen", key: cmdShiftKey(ui.KeyT, "T"), run: (*workspace).reopenClosed,
			menu: &menuSpec{"File", "Reopen Tab", 3}, pal: &palSpec{section: "Tabs", title: "Reopen last tab", icon: "reopen"}, help: &helpSpec{"Tabs", "Reopen last tab"}},
		// ⌘< and ⌘> are ⌘⇧, and ⌘⇧. on a US keyboard: both spellings are taken.
		{id: "tab.prev", key: &chord{mods: ui.Cmd | ui.Shift, key: ui.KeyBracketLeft, glyph: "<", shiftImplied: true}, also: []chord{{mods: ui.Cmd | ui.Shift, key: ui.KeyComma}},
			run: func(w *workspace) { w.cycleTab(-1) }, pal: &palSpec{section: "Tabs", title: "Previous tab"}, help: &helpSpec{"Tabs", "Previous tab"}},
		{id: "tab.next", key: &chord{mods: ui.Cmd | ui.Shift, key: ui.KeyBracketRight, glyph: ">", shiftImplied: true}, also: []chord{{mods: ui.Cmd | ui.Shift, key: ui.KeyPeriod}},
			run: func(w *workspace) { w.cycleTab(1) }, pal: &palSpec{section: "Tabs", title: "Next tab"}, help: &helpSpec{"Tabs", "Next tab"}},
		// ⌘2 to ⌘8 select the tab of that number (tabDigit in shortcuts); ⌘1 and ⌘9 are here for their rows.
		{id: "tab.first", key: cmdKey(digitKeys[0], "1"), run: func(w *workspace) { w.selectTab(0) },
			pal: &palSpec{section: "Tabs", title: "First tab"}, help: &helpSpec{"Tabs", "First tab"}},
		{id: "tab.last", key: cmdKey(digitKeys[8], "9"), run: func(w *workspace) { w.selectTab(len(w.tabs) - 1) },
			pal: &palSpec{section: "Tabs", title: "Last tab"}, help: &helpSpec{"Tabs", "Last tab"}},
		{id: "sidebar", key: cmdKey(ui.KeyB, "B"), run: func(w *workspace) { w.sidebar = !w.sidebar }, enabled: notJoined, // no toggle in a joined tab
			menu: &menuSpec{"View", "Toggle Vault Sidebar", 2}, pal: &palSpec{section: "Vault", title: "Toggle vault sidebar", icon: "panel-left"},
			help: &helpSpec{"Vault", "Toggle vault sidebar"}},
		// Ctrl+I is Tab in a terminal: Ctrl+⇧I off macOS.
		{id: "agent.toggle", key: cmdKey(ui.KeyI, "I"), other: cmdShiftKey(ui.KeyI, "I"), run: (*workspace).toggleAgent,
			menu: &menuSpec{"View", "Toggle Agent Panel", 3}, pal: &palSpec{section: "Agent", title: "Toggle agent panel", icon: "ai-magic"},
			help: &helpSpec{"Agent", "Toggle agent panel"}},
		{id: "agent.ask", key: &chord{mods: ui.Cmd, key: ui.KeyEnter, glyph: "↵"}, help: &helpSpec{"Agent", "Ask the agent (from the palette)"}},
		// Ctrl+Y is the shell's yank, Ctrl+Z suspends the job: Ctrl+⇧ off macOS. It types nothing.
		{id: "agent.suggest", key: cmdKey(ui.KeyY, "Y"), other: cmdShiftKey(ui.KeyY, "Y"), run: (*workspace).suggestRequest,
			pal: &palSpec{section: "Agent", title: "Suggest a command for this line", icon: "ai-magic"}, help: &helpSpec{"Agent", "Suggest a command for the prompt line"}},
		{id: "agent.undo", key: cmdKey(ui.KeyZ, "Z"), other: cmdShiftKey(ui.KeyZ, "Z"), help: &helpSpec{"Agent", "Put back your words after an inserted suggestion"}},
		{id: "agent.conversations", run: (*workspace).openAgentPage, pal: &palSpec{section: "Agent", title: "Agent conversations", icon: "ai-magic"}},
		{id: "agent.runbooks", run: (*workspace).openRunbooksPage, pal: &palSpec{section: "Agent", title: "Runbooks", icon: "ai-magic"}},
		{id: "folders", pal: &palSpec{many: func(w *workspace) (rows []palItem) { // a folder's roles, notes and runs
			for _, g := range w.folders {
				id := g.ID
				rows = append(rows, vaultRow("Group "+g.Name+" · roles and runbooks", "", "", func(w *workspace) { w.openGroupPage(id, false) }, w))
			}
			return rows
		}}},
		{id: "hosts.search", run: (*workspace).searchHosts, pal: &palSpec{section: "Vault", title: "Search hosts"}},
		// ⌘N is the page's own New where a page has one; the palette's New host is always a host.
		{id: "host.new", key: cmdKey(ui.KeyN, "N"), run: (*workspace).newItem,
			menuSep: true, menu: &menuSpec{"File", "New Host", 4}, pal: &palSpec{section: "Vault", title: "New host", icon: "add-01", run: (*workspace).newHost}, help: &helpSpec{"Vault", "New host"}},
		{id: "host.quick", key: cmdShiftKey(ui.KeyO, "O"), run: func(w *workspace) { w.openQuick("", nil) },
			menu: &menuSpec{"File", "Quick Connect", 5}, pal: &palSpec{section: "Vault", title: "Quick connect"}, help: &helpSpec{"Vault", "Quick connect"}},
		{id: "host.join", key: cmdKey(ui.KeyJ, "J"), run: (*workspace).openJoin,
			menu: &menuSpec{"File", "Join Session", 6}, pal: &palSpec{section: "Vault", title: "Join session", icon: "link"}, help: &helpSpec{"Vault", "Join session"}},
		{id: "hosts.import", run: (*workspace).openImport, pal: &palSpec{section: "Vault", title: "Import hosts from ~/.ssh/config"}},
		{id: "pages", pal: &palSpec{many: func(w *workspace) (rows []palItem) { // Keychain, Port forwarding, Snippets, Notebooks, Logs, Settings
			for _, k := range pageKinds {
				if !k.listed {
					continue
				}
				keys := ""
				if k.name == "settings" {
					keys = commandByID("settings").keyLabel()
				}
				rows = append(rows, vaultRow(k.title, keys, "", func(w *workspace) { w.openPage(k.name) }, w))
			}
			return rows
		}}},
		{id: "settings", key: &chord{mods: ui.Cmd, key: ui.KeyComma, glyph: ","}, run: func(w *workspace) { w.openPage("settings") }, help: &helpSpec{"Vault", "Settings"}},
		{id: "pane.splitr", key: cmdShiftKey(ui.KeyD, "D"), run: func(w *workspace) { w.splitFocused(layout.Row) },
			menuSep: true, menu: &menuSpec{"File", "Split Right", 7}, pal: &palSpec{section: "Panes", title: "Split right", icon: "split-h"}, help: &helpSpec{"Panes", "Split right"}},
		{id: "pane.splitd", key: cmdShiftKey(ui.KeyE, "E"), run: func(w *workspace) { w.splitFocused(layout.Col) },
			menu: &menuSpec{"File", "Split Down", 8}, pal: &palSpec{section: "Panes", title: "Split down", icon: "split-v"}, help: &helpSpec{"Panes", "Split down"}},
		{id: "pane.splitnb", run: (*workspace).splitNotebook, menu: &menuSpec{"File", "Split Notebook", 9}, pal: &palSpec{section: "Panes", title: "Split notebook"}},
		{id: "pane.zoom", key: &chord{mods: ui.Cmd | ui.Shift, key: ui.KeyEnter, glyph: "↵", accel: "enter"}, run: (*workspace).zoomFocused,
			menuSep: true, menu: &menuSpec{"View", "Focus View", 4}, pal: &palSpec{section: "Panes", title: "Focus view (zoom pane)"}, help: &helpSpec{"Panes", "Focus view (zoom pane)"}},
		{id: "pane.find", key: cmdKey(ui.KeyF, "F"), run: func(w *workspace) { w.openFind(w.focusedPane()) },
			menu: &menuSpec{"View", "Find in Terminal", 1}, pal: &palSpec{section: "Panes", title: "Find in terminal"}, help: &helpSpec{"Panes", "Find in terminal"}},
		{id: "pane.sync", run: func(w *workspace) { w.toggleSync(w.tab()) }, enabled: func(w *workspace) bool { return w.tab().canSync() }, // not in a joined tab, nor in a tab with no terminal
			pal: &palSpec{section: "Panes", title: "Toggle sync input", hide: true}},
		// ⌘+ is ⌘= with or without ⇧.
		{id: "text.larger", key: &chord{mods: ui.Cmd, key: ui.KeyEqual, glyph: "+", accel: "="}, also: []chord{{mods: ui.Cmd | ui.Shift, key: ui.KeyEqual}},
			run:     func(w *workspace) { w.setTermSize(w.prefs.TermSize + 1) },
			menuSep: true, menu: &menuSpec{"View", "Zoom In", 5}, pal: &palSpec{section: "Terminal", title: "Larger text"}, help: &helpSpec{"Terminal", "Larger text"}},
		{id: "text.smaller", key: &chord{mods: ui.Cmd, key: ui.KeyMinus, glyph: "-"}, run: func(w *workspace) { w.setTermSize(w.prefs.TermSize - 1) },
			menu: &menuSpec{"View", "Zoom Out", 6}, pal: &palSpec{section: "Terminal", title: "Smaller text"}, help: &helpSpec{"Terminal", "Smaller text"}},
		{id: "text.reset", key: cmdKey(ui.Key0, "0"), run: func(w *workspace) { w.setTermSize(termSizeDefault) },
			menu: &menuSpec{"View", "Actual Size", 7}, pal: &palSpec{section: "Terminal", title: "Reset text size"}, help: &helpSpec{"Terminal", "Reset text size"}},
		{id: "share", run: (*workspace).openShare, pal: &palSpec{section: "Terminal", title: "Share session (join link)"}},
		{id: "share.stop", run: (*workspace).stopShare, pal: &palSpec{section: "Terminal", title: "Stop sharing this session"}},
		// The webview stops the runtime from here at once; this client asks first, as Settings · Runtime does.
		{id: "runtime.stop", run: (*workspace).askStopRuntime, menu: &menuSpec{"Stop", "Stop Keel runtime", 1}, pal: &palSpec{section: "Terminal", title: "Stop Keel runtime"}},
	}
}

// vaultRow is a palette row of the Actions group under Vault.
func vaultRow(title, keys, icon string, run func(w *workspace), w *workspace) palItem {
	return palItem{group: "Actions", title: title, hint: "Vault", keys: keys, icon: icon, run: func(bool) { run(w) }}
}

// commandByID is the command with that id; the id is always one of the table's.
func commandByID(id string) *command {
	for i := range commands {
		if commands[i].id == id {
			return &commands[i]
		}
	}
	panic("no command " + id)
}

// commandKeyLabel is the key cap text of a command on this platform.
func commandKeyLabel(id string) string { return commandByID(id).keyLabel() }

// shortcutOf is the key of a command, for a menu item that shows it.
func shortcutOf(id string) (ui.Modifiers, ui.Key) {
	k := commandByID(id).chordHere()
	return k.mods, k.key
}

// runCommand runs a command by id if it is available now (the title bar's button).
func (w *workspace) runCommand(id string) { commandByID(id).invoke(w) }

// commandShortcuts declares the window shortcuts of every command in the table, and ⌘2 to ⌘8 for the tabs. A window shortcut
// is taken before the terminal sees the key.
func (w *workspace) commandShortcuts(c *ui.Context) {
	for i := range commands {
		cmd := &commands[i]
		if cmd.run == nil {
			continue
		}
		for _, k := range cmd.chords() {
			c.OnWindowShortcut(k.mods, k.key, func() { w.act(func() { cmd.invoke(w) }) })
		}
	}
	for i, k := range digitKeys[1:8] {
		c.OnWindowShortcut(ui.Cmd, k, func() { w.act(func() { w.selectTab(i + 1) }) })
	}
}

// menuEntries are the items of a menu bar section, as the table has them.
func menuEntries(section string) []menuEntry {
	var specs []*command
	for i := range commands {
		if m := commands[i].menu; m != nil && m.section == section {
			specs = append(specs, &commands[i])
		}
	}
	slices.SortStableFunc(specs, func(a, b *command) int { return a.menu.pos - b.menu.pos })
	var out []menuEntry
	for _, c := range specs {
		if c.menuSep {
			out = append(out, menuEntry{})
		}
		out = append(out, menuEntry{label: c.menu.title, accel: c.accel(), run: c.invoke})
	}
	return out
}

// accel is the accelerator of the menu item: "" when the command has no key.
func (c *command) accel() string {
	if k := c.chordHere(); k != nil {
		return k.accelerator()
	}
	return ""
}

// tabKeyLabel is the cap of the key that selects tab number n (1 to 9).
func tabKeyLabel(n int) string { return (&chord{mods: ui.Cmd, glyph: strconv.Itoa(n)}).label() }
