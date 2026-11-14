package native

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Settings on the fake backend: the nav, each section's controls, the sliders that repaint, the signal field and Stop.

func settingsBackend() *fakeBackend {
	fb := okBackend()
	fb.runtime = keelclient.Runtime{Build: "9d62912abcde-0123456789ab", PID: 4242, Addr: "127.0.0.1:50123", DataDir: "/scratch/data", UptimeSec: 3*3600 + 300, CanStop: true}
	fb.vault = keelclient.Vault{Mode: "passphrase", KeyringOff: true, Path: "/scratch/data/keel.vault"}
	return fb
}

func settingsRig(t *testing.T, fb *fakeBackend) *rig {
	t.Helper()
	dir := t.TempDir()
	r := newRigIn(t, fb, dir)
	r.w.showPage("settings")
	r.tt.Frame()
	return r
}

func (r *rig) section(t *testing.T, name string) {
	t.Helper()
	r.click(t, "settings nav "+name)
}

func (r *rig) prefsFile(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.w.dataDir, "keel-native.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSettingsNavIsTheApps(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.texts(t, "Settings", "Search settings", "Appearance", "Terminal", "Keyboard", "Agent", "Sharing", "Vault & security", "Runtime", "About Keel", "9d62912")
	if !r.has("settings nav Agent") {
		t.Error("no Agent section")
	}
	if _, ok := r.tt.Find("tab Settings"); !ok {
		t.Error("the tab is not named Settings")
	}
	r.texts(t, "Display", "Theme", "Light, dark, or follow the system.", "Window opacity")
	for _, s := range []string{"Font smoothing"} {
		if r.has("setting " + s) {
			t.Errorf("%q is drawn; this client has no counterpart", s)
		}
	}
}

func TestSettingsOpensFromTheKeyTheFooterAndThePalette(t *testing.T) {
	r := newRig(t, settingsBackend())
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.KeyComma)
	r.tt.Frame()
	if r.w.pageTab("settings") == nil {
		t.Fatal("⌘, did not open Settings")
	}
	r.w.closeFocused()
	r.tt.Frame()
	r.click(t, "Settings")
	if r.w.pageTab("settings") == nil {
		t.Fatal("the footer's Settings did not open it")
	}
	r.w.closeFocused()
	r.openPal(t)
	r.typePal("settings")
	rows := r.rows()
	i := slices.IndexFunc(rows, func(it palItem) bool { return it.title == "Settings" && it.keys == "⌘," })
	if i < 0 {
		t.Fatalf("the palette has no Settings with ⌘, : %+v", rows)
	}
	rows[i].run(false)
	r.w.closePalette()
	if r.w.pageTab("settings") == nil {
		t.Error("the palette did not open Settings")
	}
	r.w.showPage("settings") // one instance
	n := 0
	for _, tb := range r.w.allTabs() {
		if tb.isPage() && tb.pg.name == "settings" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d Settings tabs", n)
	}
}

func TestSettingsAppearanceSlidersWriteTheFileKeepUnknownFieldsAndRepaint(t *testing.T) {
	fb := settingsBackend()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(`{"opacity":66,"blur":20,"termSize":12,"recentHosts":["h-api"],"future":{"a":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &rig{fb: fb}
	r.w = newWorkspace(fb)
	r.w.useDataDir(t, dir)
	r.w.prefs = loadPrefs(dir)
	r.w.translucentWin, r.w.alpha = true, r.w.prefs.alpha() // the window started translucent
	r.w.work = func(fn func()) { fn() }
	r.w.post = func(fn func()) { fn() }
	r.w.mkBody = placeholder(&r.released)
	var blurs []int
	r.w.blurHook = func(n int) { blurs = append(blurs, n) }
	r.w.start()
	r.tt = ui.NewTester(r.w.view, 1280, 720)
	r.tt.Frame()
	r.click(t, "host api-1")
	wantPixel(t, "title bar at 66", r.tt.Image(), 900, 10, chrome, 0.66)
	r.w.showPage("settings")
	r.tt.Frame()
	p := r.w.settingsPage()
	if p.opacityV != 66 || p.blurV != 20 || p.sizeV != 12 {
		t.Fatalf("the sliders start at %v %v %v", p.opacityV, p.blurV, p.sizeV)
	}
	// Window opacity 80: the grounds repaint at 0.8 at once, the file has opacity 80 and every other field.
	p.opacityV = 80
	r.tt.Frame()
	wantPixel(t, "title bar at 80", r.tt.Image(), 900, 10, chrome, 0.8)
	f := r.prefsFile(t)
	if string(f["opacity"]) != "80" || string(f["blur"]) != "20" || string(f["termSize"]) != "12" || !strings.Contains(string(f["recentHosts"]), "h-api") || !strings.Contains(string(f["future"]), `"a"`) {
		t.Errorf("file %v", f)
	}
	if got := blurs[len(blurs)-1]; got != 20 {
		t.Errorf("blur calls %v: the blur is set again at the saved radius", blurs)
	}
	// Blur 40: saved, applied.
	if hasBlur() {
		p.blurV = 40
		r.tt.Frame()
		if string(r.prefsFile(t)["blur"]) != "40" || blurs[len(blurs)-1] != 40 {
			t.Errorf("blur %v %v", r.prefsFile(t)["blur"], blurs)
		}
	}
	// Crossing 100 paints the grounds opaque and clears the blur; going back brings both again.
	p.opacityV = 100
	r.tt.Frame()
	wantPixel(t, "title bar at 100", r.tt.Image(), 900, 10, chrome, 1)
	if blurs[len(blurs)-1] != 0 || r.w.alpha != 1 {
		t.Errorf("at 100: alpha %v blurs %v", r.w.alpha, blurs)
	}
	p.opacityV = 50
	r.tt.Frame()
	wantPixel(t, "title bar at 50", r.tt.Image(), 900, 10, chrome, 0.5)
	if r.w.alpha != 0.5 || blurs[len(blurs)-1] == 0 {
		t.Errorf("alpha %v blurs %v", r.w.alpha, blurs)
	}
	// Text size is shared with ⌘+ and ⌘-: the slider follows them and they follow it.
	r.tt.Key(ui.Cmd, ui.KeyEqual)
	r.tt.Frame()
	if p.sizeV != 13 || string(r.prefsFile(t)["termSize"]) != "13" {
		t.Errorf("after ⌘+: slider %v file %s", p.sizeV, r.prefsFile(t)["termSize"])
	}
	p.sizeV = 18
	r.tt.Frame()
	if r.w.prefs.TermSize != 18 || r.w.tabAt(0).panes["1"].fontSize != 18 {
		t.Errorf("the slider left the text size at %d (pane %v)", r.w.prefs.TermSize, r.w.tabAt(0).panes["1"].fontSize)
	}
}

// There is no Window shadow setting. An old file that has it still loads, the page has no such row, and the
// next save leaves the key out.
func TestAnOldWindowShadowKeyIsIgnoredAndDropped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(`{"windowShadow":false,"opacity":50}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := loadPrefs(dir); p.Opacity != 50 {
		t.Fatalf("prefs %+v", p)
	}
	if err := savePrefs(dir, map[string]any{"termSize": 14}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "keel-native.json"))
	if strings.Contains(string(b), "windowShadow") || !strings.Contains(string(b), `"opacity"`) {
		t.Fatalf("file after a save: %s", b)
	}
	r := settingsRig(t, settingsBackend())
	r.section(t, "Appearance")
	if !r.tt.HasText("Window opacity") || r.tt.HasText("Window shadow") {
		t.Fatal("the Window shadow row is still there")
	}
}

func TestSettingsSliderMovesWithTheKeyboard(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.section(t, "Terminal")
	r.click(t, "Text size slider")
	r.tt.Key(0, ui.KeyRight)
	r.tt.Frame()
	// The click set the middle of 10–22 (16) and → stepped to 17.
	if r.w.prefs.TermSize != 17 || string(r.prefsFile(t)["termSize"]) != "17" {
		t.Errorf("→ on the Text size slider left %d", r.w.prefs.TermSize)
	}
}

func TestSettingsOpaqueWindowSavesAndSaysItAppliesNextStart(t *testing.T) {
	r := settingsRig(t, settingsBackend()) // translucentWin is false: the window started opaque
	r.w.prefs.Opacity = 100
	p := r.w.settingsPage()
	p.opacityV = 70
	r.tt.Frame()
	if r.w.alpha != 1 {
		t.Errorf("an opaque window painted translucent grounds: alpha %v", r.w.alpha)
	}
	if string(r.prefsFile(t)["opacity"]) != "70" {
		t.Errorf("opacity not saved: %v", r.prefsFile(t))
	}
	r.texts(t, "The window turns see-through the next time Keel starts.")
}

func TestSettingsTerminalControlsAndTheirDefaults(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.section(t, "Terminal")
	r.texts(t, "Session", "Text size", "10–22px. ⌘+ / ⌘− / ⌘0 also change size.", "Input", "Copy on select", "Copy terminal selection to the clipboard.",
		"Right-click in the terminal", "Paste the clipboard, open the terminal menu, or do nothing. With Menu or Nothing, ⇧ right-click opens the system's menu.",
		"Paste", "Menu", "Nothing", "guest@keel:~ $ ssh prod-api")
	if !r.w.prefs.CopyOnSelect || r.w.prefs.RightClick != "menu" {
		t.Fatalf("defaults are the app's: copy on select on, right-click Menu; got %+v", r.w.prefs)
	}
	r.texts(t, "Font", "Monospace face used in every pane.", "JetBrains Mono") // the first of the five, chosen by default
	r.w.openHost(testHosts[1])                                                 // a pane to change live
	r.w.showPage("settings")
	r.tt.Frame()
	r.section(t, "Terminal")
	var offs []bool
	for _, tb := range r.w.allTabs() {
		for _, p := range tb.panes {
			p.setCopy = func(on bool) { offs = append(offs, on) }
		}
	}
	r.click(t, "Copy on select switch")
	if r.w.prefs.CopyOnSelect || !slices.Equal(offs, []bool{false}) || string(r.prefsFile(t)["copyOnSelect"]) != "false" {
		t.Errorf("copy on select: %+v calls %v file %v", r.w.prefs, offs, r.prefsFile(t))
	}
	r.click(t, "Paste")
	if r.w.prefs.RightClick != "paste" || string(r.prefsFile(t)["rightClick"]) != `"paste"` {
		t.Errorf("right click %q file %v", r.w.prefs.RightClick, r.prefsFile(t))
	}
	// A pane opened now takes it; the file is read back as written.
	r.w.openHost(testHosts[1])
	if got := r.w.focusedPane(); got.rightClick != "paste" || got.copyOnSelect {
		t.Errorf("a new pane has %q %v", got.rightClick, got.copyOnSelect)
	}
	if got := loadPrefs(r.w.dataDir); got.RightClick != "paste" || got.CopyOnSelect {
		t.Errorf("read back %+v", got)
	}
	r.w.showPage("settings")
	r.tt.Frame()
	r.click(t, "Nothing")
	if r.w.prefs.RightClick != "nothing" {
		t.Errorf("right click %q", r.w.prefs.RightClick)
	}
}

func TestLoadTermPrefs(t *testing.T) {
	dir := t.TempDir()
	if got := loadPrefs(dir); !got.CopyOnSelect || got.RightClick != "menu" || got.Name != "" {
		t.Errorf("missing file: %+v", got)
	}
	os.WriteFile(filepath.Join(dir, "keel-native.json"), []byte(`{"copyOnSelect":false,"rightClick":"sideways","name":"  `+strings.Repeat("é", 50)+` "}`), 0o600)
	got := loadPrefs(dir)
	if got.CopyOnSelect || got.RightClick != "menu" || len([]rune(got.Name)) != nameMax {
		t.Errorf("got %+v", got)
	}
}

func TestSettingsKeyboardListsTheKeysThisClientHas(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.section(t, "Keyboard")
	r.texts(t, "The keys Keel handles. They are fixed; the command palette shows the same keys beside each action.",
		"General", "Tabs", "Vault", "Panes", "Terminal", "New tab", "Command palette", "Settings", "Focus view (zoom pane)", "Reset text size")
	if !r.has("keys ⌘⇧T") || !r.has("keys ⌘,") {
		t.Errorf("key caps missing: %v", r.tt.Texts())
	}
	if !r.has("Join session") || !r.has("keys ⌘J") {
		t.Error("Join session ⌘J is not listed")
	}
	if !r.has("Toggle agent panel") || !r.has("keys ⌘I") {
		t.Error("Toggle agent panel ⌘I is not listed")
	}
	if !r.has("Ask the agent (from the palette)") || !r.has("keys ⌘↵") { // the palette's ⌘↵ row is built
		t.Error("Ask the agent (from the palette) ⌘↵ is not listed")
	}
	if !r.has("Put back your words after an inserted suggestion") || !r.has("keys ⌘Z") {
		t.Error("the put-back key of in-pane suggestions is not listed")
	}
}

func TestSettingsSharingNameAndSignalServerWithTheAppsRules(t *testing.T) {
	fb := settingsBackend()
	fb.runtime.SignalSource = ""
	r := settingsRig(t, fb)
	r.section(t, "Sharing")
	r.texts(t, "Share a session by link. Nobody joins without your say, and the keyboard stays with you until you pass it.",
		"You", "Display name", "Shown to people in sessions you share and join. Empty uses the default.",
		"Signal server", "Server for internet join links", "Share is off until you enter a keel-signal server: one you run yourself, or one someone runs for you.",
		"Access", "Joining needs your approval", "Control stays with you", "Always on")
	// The display name: trimmed, at most 40 characters, kept in keel-native.json, empty uses the default.
	r.fill(t, "Display name input", "  Ann  ")
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if r.w.prefs.Name != "Ann" {
		t.Errorf("the name is trimmed: %q", r.w.prefs.Name)
	}
	r.fill(t, "Display name input", strings.Repeat("n", 50))
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if got := r.w.prefs.Name; got != strings.Repeat("n", 40) || string(r.prefsFile(t)["name"]) != `"`+strings.Repeat("n", 40)+`"` {
		t.Errorf("name %q file %v", got, r.prefsFile(t)["name"])
	}
	// The signal server goes to keel serve as typed (trimmed); keel serve validates it.
	r.fill(t, "Signal server input", "  https://signal.example.com  ")
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if !slices.Equal(fb.signalSet, []string{"https://signal.example.com"}) {
		t.Fatalf("signal calls %v", fb.signalSet)
	}
	r.texts(t, "Signal server saved", "Join links go through this server. It never sees the terminal or the key in the link's #fragment.")
	// Unchanged: no call. Emptied: removed.
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if len(fb.signalSet) != 1 {
		t.Errorf("an unchanged field made a call: %v", fb.signalSet)
	}
	r.fill(t, "Signal server input", "")
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if !slices.Equal(fb.signalSet, []string{"https://signal.example.com", ""}) {
		t.Fatalf("signal calls %v", fb.signalSet)
	}
	r.texts(t, "Signal server removed")
	// keel serve's refusal is shown in its own words; the field keeps what was typed.
	fb.signalErr = &keelclient.StatusError{Code: 400, Msg: "signal server must be an https or wss URL"}
	r.fill(t, "Signal server input", "ftp://nope")
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	r.texts(t, "signal server must be an https or wss URL")
	// KEEL_SIGNAL wins for the run, and says so; the field still holds the saved server.
	fb.signalErr = nil
	fb.runtime = keelclient.Runtime{SignalURL: "https://env.example", SignalSource: "env", SignalSaved: "https://saved.example"}
	r.w.loadSettings()
	r.tt.Frame()
	r.texts(t, "KEEL_SIGNAL is set for this run, so Keel uses https://env.example. The server saved here is used when it is not set.")
}

func TestSettingsVaultAndSecurityShowsNoSecret(t *testing.T) {
	fb := settingsBackend()
	r := settingsRig(t, fb)
	r.section(t, "Vault & security")
	r.texts(t, "Passwords, key passphrases and the agent key are encrypted in the vault file. Nothing here shows a secret.",
		"Vault", "State", "Unlocked", "Unlocked with", "Passphrase", "The OS keychain is off for this run (KEEL_KEYRING=off), so Keel asks for the passphrase.",
		"Vault file", "/scratch/data/keel.vault", "Copy path", "Safety", "The agent never sees vault contents", "A lost passphrase cannot be recovered")
	r.click(t, "Copy path")
	if r.tt.Clipboard() != "/scratch/data/keel.vault" || !r.tt.HasText("Copied") {
		t.Errorf("clipboard %q", r.tt.Clipboard())
	}
	fb.vault = keelclient.Vault{Mode: "keychain", Locked: true}
	r.w.loadSettings()
	r.tt.Frame()
	r.texts(t, "Locked", "OS keychain", "The key that opens the vault is kept in your operating system's keychain. No passphrase to type.")
	for _, s := range r.tt.Texts() {
		if strings.Contains(strings.ToLower(s), "passphrase\x00") || s == "unlock-secret" {
			t.Errorf("text %q", s)
		}
	}
}

func TestSettingsRuntimeShowsStatusAndStopAsksFirst(t *testing.T) {
	fb := settingsBackend()
	r := settingsRig(t, fb)
	r.w.openHost(testHosts[1])
	r.w.showPage("settings")
	r.tt.Frame()
	r.section(t, "Runtime")
	r.texts(t, "keel serve owns your sessions and serves this window. It keeps running when the window closes.",
		"Status", "Build", "9d62912abcde-0123456789ab", "Address", "127.0.0.1:50123", "Data folder", "/scratch/data", "Process id", "4242",
		"Running sessions", "Uptime", "3 h 05 min", "Window", "Sessions keep running after the window closes", "Stop", "Stop the Keel runtime", "Stop runtime…")
	if !r.has("value 1") {
		t.Errorf("running sessions: %v", r.tt.Texts())
	}
	r.click(t, "Stop runtime…")
	r.texts(t, "Stop the Keel runtime?", "This ends the 1 running session and every port forward, then closes keel serve. Reopen Keel to start it again.", "Stop runtime")
	if fb.runtimeStops != 0 {
		t.Fatal("stopped before the confirm")
	}
	r.click(t, "Cancel")
	if fb.runtimeStops != 0 || r.w.confirmOpen() {
		t.Fatalf("Cancel stopped: %d", fb.runtimeStops)
	}
	r.click(t, "Stop runtime…")
	r.click(t, "alertdialog Stop the Keel runtime?")
	r.w.confirmDlg().run()
	r.tt.Frame()
	if fb.runtimeStops != 1 || !r.tt.HasText("Keel runtime stopped") {
		t.Errorf("stops %d", fb.runtimeStops)
	}
	// A refusal is a toast, not a silent stop.
	fb.stopErr = errors.New("nope")
	r.w.askStopRuntime()
	r.tt.Frame()
	r.w.confirmDlg().run()
	r.tt.Frame()
	r.texts(t, "stop runtime: nope")
	// A runtime that cannot be stopped says no.
	fb.runtime.CanStop = false
	r.w.loadSettings()
	r.tt.Frame()
	if err := r.tt.Click("Stop runtime…"); err == nil && r.w.confirmOpen() {
		t.Error("Stop opened its confirm for a runtime that cannot stop")
	}
}

func TestStopFromThePaletteAsksToo(t *testing.T) {
	fb := settingsBackend()
	r := newRig(t, fb)
	r.tt.Frame()
	r.openPal(t)
	r.typePal("stop keel runtime")
	rows := r.rows()
	if len(rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
	rows[0].run(false)
	r.w.closePalette()
	r.tt.Frame()
	if fb.runtimeStops != 0 || !r.w.confirmOpen() {
		t.Fatalf("the palette stopped the runtime without asking: stops %d", fb.runtimeStops)
	}
	r.texts(t, "Stop the Keel runtime?")
}

func TestSettingsAboutAndSearch(t *testing.T) {
	r := settingsRig(t, settingsBackend())
	r.section(t, "About Keel")
	r.texts(t, "Version", "9d62912", "Build", "Links", "Source", "https://github.com/KJK-Nice/keel", "README", "Copy link")
	r.click(t, "Copy link")
	if !strings.HasPrefix(r.tt.Clipboard(), "https://github.com/KJK-Nice/keel") {
		t.Errorf("clipboard %q", r.tt.Clipboard())
	}
	// Search reads every section's rows, titles, descriptions and key caps; with none the section comes back.
	r.fill(t, "Search settings", "clipboard")
	r.texts(t, "Copy on select", "Terminal")
	if r.has("setting Window opacity") {
		t.Error("a row that does not match shows")
	}
	r.fill(t, "Search settings", "⌘⇧T")
	r.texts(t, "Keyboard", "Reopen last tab")
	r.fill(t, "Search settings", "zzzz")
	r.texts(t, "Nothing matches")
	r.fill(t, "Search settings", "")
	r.texts(t, "About Keel", "Source")
}

func TestSettingsPageIsSavedWithTheTabs(t *testing.T) {
	fb := settingsBackend()
	dir := t.TempDir()
	r := newRigIn(t, fb, dir)
	r.w.showPage("settings")
	r.w.closeWindow()
	r2 := newRigIn(t, settingsBackend(), dir)
	if r2.w.pageTab("settings") == nil {
		t.Error("the Settings tab did not come back")
	}
}
