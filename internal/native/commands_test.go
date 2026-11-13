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
	for _, e := range append(append(fileMenu(), viewMenu("darwin")...), stopRuntimeEntry) {
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

func TestMenuPaletteAndKeyboardRowsOnMac(t *testing.T) {
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
