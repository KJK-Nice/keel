package desktop

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// menuEntries is every Keel item in the File and View menus for goos.
func menuEntries(goos string) []menuEntry {
	var out []menuEntry
	for _, e := range append(slices.Clone(fileMenu), viewMenu(goos)...) {
		if e.label != "" {
			out = append(out, e)
		}
	}
	return out
}

// pinnedMenuActions is every name the native menu sends to
// window.__keelShortcut. Changing it means changing runShortcut in term.js
// too (TestMenuActionsAreHandledByThePage).
var pinnedMenuActions = []string{
	"close-tab", "find-term", "focus-pane", "join", "new-host", "new-tab",
	"quick-connect", "reopen-tab", "search", "split-down", "split-notebook",
	"split-right", "stop-runtime", "toggle-agent", "toggle-sidebar",
	"zoom-in", "zoom-out", "zoom-reset",
}

func TestMenuActionsArePinned(t *testing.T) {
	var got []string
	for _, e := range menuEntries("darwin") {
		if e.action != actionQuit {
			got = append(got, e.action)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, pinnedMenuActions) {
		t.Fatalf("menu actions = %v\nwant %v", got, pinnedMenuActions)
	}
}

// Every menu action must be a name runShortcut in term.js acts on;
// otherwise the item calls window.__keelShortcut and nothing happens.
func TestMenuActionsAreHandledByThePage(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "keel", "web", "js", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := strings.Index(s, "\n  function runShortcut(")
	if start < 0 {
		t.Fatal("runShortcut not found in term.js")
	}
	end := strings.Index(s[start+1:], "\n  function ")
	if end < 0 {
		t.Fatal("end of runShortcut not found in term.js")
	}
	body := s[start : start+1+end]
	handled := map[string]bool{}
	for _, m := range regexp.MustCompile(`name === "([a-z-]+)"`).FindAllStringSubmatch(body, -1) {
		handled[m[1]] = true
	}
	for _, name := range pinnedMenuActions {
		if !handled[name] {
			t.Errorf("menu action %q is not handled by runShortcut in term.js", name)
		}
	}
	if !strings.Contains(s, "window.__keelShortcut = ") {
		t.Error("term.js no longer defines window.__keelShortcut")
	}
}

// The native menu and term.js both react to the same chords: the menu
// accelerator calls window.__keelShortcut(name), and the page's keydown
// runs classifyShortcut. If they disagree, a key does one thing from the
// menu and another (or nothing) from the keyboard. This checks every menu
// accelerator against lib/shortcuts.js, on macOS (Cmd) and elsewhere
// (Ctrl). With a dialog open the menu item must run exactly when the key
// does (menuShortcutAllowed), and every chord the page runs must be one it
// swallows when it declines it (isChromeChord), so the menu never runs it
// in the page's place.
func TestMenuAcceleratorsMatchShortcutTable(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	type entry struct {
		Accel string  `json:"accel"`
		Want  *string `json:"want"`           // nil: the menu handles it natively, JS must not
		Only  string  `json:"only,omitempty"` // "mac" or "other": an OS-specific chord
	}
	var entries []entry
	add := func(goos, only string) {
		for _, e := range menuEntries(goos) {
			if e.accel == "" {
				continue
			}
			if only != "" && e.action != "toggle-agent" {
				continue // the agent chord is the only one that differs by OS
			}
			if only == "" && e.action == "toggle-agent" {
				continue
			}
			en := entry{Accel: e.accel, Only: only}
			if e.action != actionQuit {
				name := e.action
				en.Want = &name
			}
			entries = append(entries, en)
		}
	}
	add("darwin", "")
	add("darwin", "mac")
	add("linux", "other")
	entries = append(entries, entry{Accel: "CmdOrCtrl+i", Want: nil, Only: "other"}) // Ctrl+I is Tab: never the panel
	if len(entries) < 15 {
		t.Fatalf("found %d menu accelerators, expected at least 15", len(entries))
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}

	lib, err := filepath.Abs(filepath.Join("..", "..", "cmd", "keel", "web", "js", "lib", "shortcuts.js"))
	if err != nil {
		t.Fatal(err)
	}
	libPath := filepath.ToSlash(lib)
	if !strings.HasPrefix(libPath, "/") {
		libPath = "/" + libPath
	}
	libURL := (&url.URL{Scheme: "file", Path: libPath}).String()
	script := filepath.Join(t.TempDir(), "menu.mjs")
	harness := `import { classifyShortcut, isChromeChord, menuShortcutAllowed } from ` + jsString(libURL) + `;
const entries = ` + string(payload) + `;
const codes = { "=": "Equal", "-": "Minus", "0": "Digit0" };
function event(accel, mac) {
  const parts = accel.split("+");
  const raw = parts.pop();
  const shift = parts.includes("Shift");
  const key = raw.toLowerCase() === "enter" ? "Enter" : shift && raw.length === 1 ? raw.toUpperCase() : raw;
  return { key, code: codes[raw] || "", shiftKey: shift, metaKey: mac, ctrlKey: !mac, altKey: false, repeat: false };
}
const bad = [];
for (const { accel, want, only } of entries) {
  for (const mac of [true, false]) {
    if ((only === "mac" && !mac) || (only === "other" && mac)) continue;
    const where = accel + (mac ? " (mac)" : " (other)");
    const ev = event(accel, mac);
    const got = classifyShortcut(ev, { mac, modalOpen: false, findOpen: () => false });
    const name = got ? got.name : null;
    if (name !== want) bad.push(where + ": menu runs " + want + ", keyboard runs " + name);
    if (isChromeChord(ev, mac) !== (want !== null)) bad.push(where + ": isChromeChord disagrees with the menu");
    if (want === null) continue;
    const inDialog = classifyShortcut(ev, { mac, modalOpen: true, findOpen: () => false });
    const keyRuns = !!inDialog && inDialog.name === want;
    if (menuShortcutAllowed(want, { modalOpen: true }) !== keyRuns) {
      bad.push(where + ": with a dialog open the key " + (keyRuns ? "runs" : "does not run") + " " + want + " but the menu item " + (keyRuns ? "does not" : "does"));
    }
  }
}
if (bad.length) { console.log(bad.join("\n")); process.exit(1); }
console.log("ok", entries.length);
`
	if err := os.WriteFile(script, []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script).CombinedOutput()
	if err != nil {
		t.Fatalf("menu accelerators disagree with lib/shortcuts.js:\n%s", out)
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
