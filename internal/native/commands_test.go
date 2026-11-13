package native

import (
	"strings"
	"testing"
)

// What the menu bar, the palette's actions and the Keyboard page show on macOS. These rows were written down from the three
// lists the command table replaced, before they were deleted: the table must generate the same rows in the same order.
const wantMenuMac = `New Tab|CmdOrCtrl+t
Close Tab|CmdOrCtrl+w
Reopen Tab|CmdOrCtrl+Shift+t
|
New Host|CmdOrCtrl+n
Quick Connect|CmdOrCtrl+Shift+o
Join Session|CmdOrCtrl+j
|
Split Right|CmdOrCtrl+Shift+d
Split Down|CmdOrCtrl+Shift+e
Split Notebook|
Find in Terminal|CmdOrCtrl+f
Toggle Vault Sidebar|CmdOrCtrl+b
Toggle Agent Panel|CmdOrCtrl+i
|
Focus View|CmdOrCtrl+Shift+enter
|
Zoom In|CmdOrCtrl+=
Zoom Out|CmdOrCtrl+-
Actual Size|CmdOrCtrl+0
Stop Keel runtime|`

const wantPaletteMac = `New tab|Tabs|⌘T|add-01
New Local Shell|Tabs||
Close pane or tab|Tabs|⌘W|
Reopen last tab|Tabs|⌘⇧T|reopen
Previous tab|Tabs|⌘<|
Next tab|Tabs|⌘>|
First tab|Tabs|⌘1|
Last tab|Tabs|⌘9|
Toggle vault sidebar|Vault|⌘B|panel-left
Toggle agent panel|Agent|⌘I|ai-magic
Suggest a command for this line|Agent|⌘Y|ai-magic
Agent conversations|Agent||ai-magic
Runbooks|Agent||ai-magic
Search hosts|Vault||
New host|Vault|⌘N|add-01
Quick connect|Vault|⌘⇧O|
Join session|Vault|⌘J|link
Import hosts from ~/.ssh/config|Vault||
Keychain|Vault||
Port forwarding|Vault||
Snippets|Vault||
Notebooks|Vault||
Logs|Vault||
Settings|Vault|⌘,|
Split right|Panes|⌘⇧D|split-h
Split down|Panes|⌘⇧E|split-v
Split notebook|Panes||
Focus view (zoom pane)|Panes|⌘⇧↵|
Find in terminal|Panes|⌘F|
Toggle sync input|Panes||
Larger text|Terminal|⌘+|
Smaller text|Terminal|⌘-|
Reset text size|Terminal|⌘0|
Share session (join link)|Terminal||
Stop sharing this session|Terminal||
Stop Keel runtime|Terminal||`

const wantKeyboardMac = `General|Command palette|⌘K
Tabs|New tab|⌘T
Tabs|Close pane or tab|⌘W
Tabs|Reopen last tab|⌘⇧T
Tabs|Previous tab|⌘<
Tabs|Next tab|⌘>
Tabs|First tab|⌘1
Tabs|Last tab|⌘9
Vault|Toggle vault sidebar|⌘B
Vault|New host|⌘N
Vault|Quick connect|⌘⇧O
Vault|Join session|⌘J
Vault|Settings|⌘,
Panes|Split right|⌘⇧D
Panes|Split down|⌘⇧E
Panes|Focus view (zoom pane)|⌘⇧↵
Panes|Find in terminal|⌘F
Terminal|Larger text|⌘+
Terminal|Smaller text|⌘-
Terminal|Reset text size|⌘0
Agent|Toggle agent panel|⌘I
Agent|Ask the agent (from the palette)|⌘↵
Agent|Suggest a command for the prompt line|⌘Y
Agent|Put back your words after an inserted suggestion|⌘Z`

// menuRowsNow is the menu bar's File, View and Stop rows as label|accelerator, a separator as "|".
func menuRowsNow() string {
	var rows []string
	for _, e := range allMenuEntries() {
		rows = append(rows, e.label+"|"+e.accel)
	}
	return strings.Join(rows, "\n")
}

func paletteRowsNow(t *testing.T) string {
	r := newRig(t, okBackend())
	r.click(t, "host api-1")
	var rows []string
	for _, it := range r.w.paletteItems() {
		if it.group == "Actions" {
			rows = append(rows, it.title+"|"+it.hint+"|"+it.keys+"|"+it.icon)
		}
	}
	return strings.Join(rows, "\n")
}

func keyboardRowsNow() string {
	var rows []string
	for _, g := range shortcutGroups() {
		for _, s := range g.items {
			rows = append(rows, g.name+"|"+s.title+"|"+s.keys)
		}
	}
	return strings.Join(rows, "\n")
}

// allMenuEntries are File, View and Stop in the order the menu bar shows them (Close Window sits before Stop).
func allMenuEntries() []menuEntry {
	return append(append(menuEntries("File"), menuEntries("View")...), menuEntries("Stop")...)
}

// usePlatform makes the window run on another OS for the test: the keys and their labels follow.
func usePlatform(t *testing.T, goos string) {
	t.Helper()
	was := platform
	platform = goos
	t.Cleanup(func() { platform = was })
}

// Off macOS the palette and the Keyboard page write the keys as Ctrl+Shift+T, as the menu bar and the Agent button do, not
// with macOS's glyphs (they showed ⌘ on every OS before the table).
func TestKeyLabelsOffMacAreWords(t *testing.T) {
	usePlatform(t, "linux")
	for _, rows := range []string{paletteRowsNow(t), keyboardRowsNow()} {
		if strings.ContainsAny(rows, "⌘⇧⌥") {
			t.Errorf("macOS glyphs on Linux:\n%s", rows)
		}
	}
	want := map[string]string{"New tab": "Ctrl+T", "Reopen last tab": "Ctrl+Shift+T", "Toggle agent panel": "Ctrl+Shift+I",
		"Previous tab": "Ctrl+<", "Larger text": "Ctrl++", "Focus view (zoom pane)": "Ctrl+Shift+↵", "Settings": "Ctrl+,"}
	for _, g := range shortcutGroups() {
		for _, s := range g.items {
			if k, ok := want[s.title]; ok && s.keys != k {
				t.Errorf("%s is %q, want %q", s.title, s.keys, k)
			}
		}
	}
}

// The palette's Toggle vault sidebar does what ⌘B does, which is nothing in a joined tab.
func TestToggleSidebarFromThePaletteDoesNothingInAJoinedTab(t *testing.T) {
	jr := newJoinRig(t)
	jr.join(t, joinTestLink)
	if !jr.w.joinedActive() {
		t.Fatal("setup: no joined tab")
	}
	was := jr.w.sidebar
	for _, it := range jr.w.paletteItems() {
		if it.title == "Toggle vault sidebar" {
			it.run(false)
		}
	}
	if jr.w.sidebar != was {
		t.Error("the palette toggled the sidebar in a joined tab")
	}
}

func TestMenuPaletteAndKeyboardRowsOnMac(t *testing.T) {
	usePlatform(t, "darwin")
	for _, c := range []struct{ name, got, want string }{
		{"menu bar", menuRowsNow(), wantMenuMac},
		{"palette", paletteRowsNow(t), wantPaletteMac},
		{"keyboard page", keyboardRowsNow(), wantKeyboardMac},
	} {
		if c.got != c.want {
			t.Errorf("%s changed:\n got:\n%s\nwant:\n%s", c.name, c.got, c.want)
		}
	}
}
