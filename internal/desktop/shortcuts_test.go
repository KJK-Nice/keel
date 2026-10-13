package desktop

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	menuActionRe  = regexp.MustCompile(`SetAccelerator\("([^"]+)"\)\.OnClick\(w\.menuAction\("([^"]+)"\)\)`)
	menuHandlerRe = regexp.MustCompile(`SetAccelerator\("([^"]+)"\)\.OnClick\(func`)
)

// The native menu and term.js both react to the same chords: the menu
// accelerator calls window.__keelShortcut(name), and the page's keydown
// runs classifyShortcut. If they disagree, a key does one thing from the
// menu and another (or nothing) from the keyboard. This checks every menu
// accelerator against lib/shortcuts.js, on macOS (Cmd) and elsewhere (Ctrl).
func TestMenuAcceleratorsMatchShortcutTable(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src, err := os.ReadFile("desktop.go")
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		Accel string  `json:"accel"`
		Want  *string `json:"want"` // nil: the menu handles it natively, JS must not
	}
	var entries []entry
	for _, m := range menuActionRe.FindAllStringSubmatch(string(src), -1) {
		name := m[2]
		entries = append(entries, entry{Accel: m[1], Want: &name})
	}
	for _, m := range menuHandlerRe.FindAllStringSubmatch(string(src), -1) {
		entries = append(entries, entry{Accel: m[1]})
	}
	if len(entries) < 15 {
		t.Fatalf("found %d menu accelerators, expected at least 15 — did the menu code change shape?", len(entries))
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
	harness := `import { classifyShortcut } from ` + jsString(libURL) + `;
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
for (const { accel, want } of entries) {
  for (const mac of [true, false]) {
    const got = classifyShortcut(event(accel, mac), { mac, modalOpen: false, findOpen: () => false });
    const name = got ? got.name : null;
    if (name !== want) bad.push(accel + (mac ? " (mac)" : " (other)") + ": menu runs " + want + ", keyboard runs " + name);
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
