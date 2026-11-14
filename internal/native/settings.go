package native

import (
	"context"
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Settings (docs/ui-redesign.md §5.9). The helpers are in settingshelpers.go and the agent's own settings in agentsettings.go.
//
// There is no Appearance · Font smoothing row: MyGo draws its own text. Sharing · Display name is stored in
// keel-native.json ("name").
//
// Window opacity and Window blur apply live while the window was created translucent (Transparent cannot change on an open
// MyGo window): the grounds repaint at the new alpha, crossing 100 paints them opaque, and the blur is set again (0 at
// 100). A window that started opaque saves the value and says it applies from the next start. Right-click in the terminal
// has no setter in the terminal plugin: it applies to panes opened from then on; Copy on select and Text size apply to
// every pane at once.

// settingsPage is the state of the Settings tab.
type settingsPage struct {
	section string // appearance | terminal | keyboard | agent | sharing | vault | runtime | about
	search  string
	searchH ui.Handle

	rt       *keelclient.Runtime
	vault    *keelclient.Vault
	loaded   bool
	loadFail string // the read failed: the sections show what they have

	// The sliders are bound to floats; a change is told from the value last applied.
	opacityV, blurV, sizeV, uiV                         float64
	opacityApplied, blurApplied, sizeApplied, uiApplied int

	name, signal         string // the two inputs while they are edited
	nameH, signalH       ui.Handle
	nameWas, signalWas   bool // the input had the keyboard in the last frame: leaving it commits
	nameSeen, signalSeen bool // the field has been given its text
	signalSaving         bool

	// Settings · Agent (agentsettings.go): a provider card's state, and the scrollback slider. The slider is bound to a float;
	// a move is sent one request at a time and the value keel serve holds is shown again.
	cards        map[string]*keyCard
	notes        notesEditor // Notes for the agent: the user's own (notes.go)
	linesV       float64
	linesApplied int
	linesBusy    bool
	linesInit    bool
}

var settingsNav = [][2]string{
	{"appearance", "Appearance"}, {"terminal", "Terminal"}, {"keyboard", "Keyboard"}, {"agent", "Agent"},
	{"sharing", "Sharing"}, {"vault", "Vault & security"}, {"runtime", "Runtime"},
}

func newSettingsPage() *settingsPage {
	return &settingsPage{section: "appearance", notes: notesEditor{user: true}}
}

func (p *settingsPage) load(w *workspace) {
	w.loadSettings()
	if p.section == "agent" {
		w.loadAgentSettings()
	}
}

func (p *settingsPage) view(w *workspace, c *ui.Context) { w.settingsView(c, p) }

// ---- reading ----

// loadSettings reads what Sharing, Vault & security, Runtime and About show (settingspages.js refresh). Neither call
// holds a secret.
func (w *workspace) loadSettings() {
	type read struct {
		rt         keelclient.Runtime
		v          keelclient.Vault
		rerr, verr error
	}
	call(w, func(c context.Context) (r read, _ error) {
		r.rt, r.rerr = w.be.Runtime(c)
		r.v, r.verr = w.be.Vault(c)
		return r, nil
	}, func(r read, _ error) {
		t := w.pageTab("settings")
		if t == nil {
			return
		}
		p := pageAs[*settingsPage](t)
		if r.rerr == nil {
			p.rt = &r.rt
		}
		if r.verr == nil {
			p.vault = &r.v
		}
		p.loaded = true
		if r.rerr != nil || r.verr != nil {
			p.loadFail = "Could not read keel serve"
		} else {
			p.loadFail = ""
		}
	})
}

// ---- the window and terminal preferences ----

// alphaFor is the ground alpha of an opacity: 1 at 100.
func alphaFor(opacity int) float32 {
	if opacity >= opacityMax {
		return 1
	}
	return float32(opacity) / 100
}

// setOpacity keeps the value and, in a window that started translucent, repaints the grounds at
// the new alpha and sets the blur again.
func (w *workspace) setOpacity(n int) {
	n = min(max(n, opacityMin), opacityMax)
	if n == w.prefs.Opacity {
		return
	}
	w.prefs.Opacity = n
	w.setPref(keyOpacity)
	if w.translucentWin {
		w.alpha = alphaFor(n)
	}
	w.applyBlur()
}

// setUIPref keeps the size; every view draws at it from the next frame (setUISize).
func (w *workspace) setUIPref(n int) {
	n = min(max(n, uiSizeMin), uiSizeMax)
	if n == w.prefs.UISize {
		return
	}
	w.prefs.UISize = n
	w.setPref(keyUISize)
}

func (w *workspace) setBlur(n int) {
	n = min(max(n, blurMin), blurMax)
	if n == w.prefs.Blur {
		return
	}
	w.prefs.Blur = n
	w.setPref(keyBlur)
	w.applyBlur()
}

// applyBlur sets the window's blur: the saved radius below 100, clear at 100, and none in a window that is not translucent.
func (w *workspace) applyBlur() {
	if w.blurHook == nil {
		return
	}
	r := w.prefs.Blur
	if !w.translucentWin || w.prefs.Opacity >= opacityMax {
		r = 0
	}
	w.blurHook(r)
}

// setCopyOnSelect is the Copy on select switch: every terminal takes it at once.
func (w *workspace) setCopyOnSelect(on bool) {
	if on == w.prefs.CopyOnSelect {
		return
	}
	w.prefs.CopyOnSelect = on
	w.setPref(keyCopyOnSelect)
	for _, t := range w.tabs {
		for _, p := range t.panes {
			p.copyOnSelect = on
			if p.setCopy != nil {
				p.setCopy(on)
			}
		}
	}
}

// setRightClick is the Right-click in the terminal row. The plugin takes it when a terminal is made.
func (w *workspace) setRightClick(mode string) {
	if mode == w.prefs.RightClick {
		return
	}
	w.prefs.RightClick = mode
	w.setPref(keyRightClick)
}

// setDisplayName: trimmed and cut to 40 characters; empty uses the default.
func (w *workspace) setDisplayName(v string) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > nameMax {
		v = string([]rune(v)[:nameMax])
	}
	if v == w.prefs.Name {
		return
	}
	w.prefs.Name = v
	w.setPref(keyDisplayName)
}

// defaultName is the name used when no custom one is set: the one keel app was started with (Options.Name), else
// KEEL_NAME, else the user name (cmd/keel parseName).
func defaultName() string {
	if launchName != "" {
		return launchName
	}
	for _, k := range []string{"KEEL_NAME", "USER"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "guest"
}

// ---- the signal server and Stop ----

// signalSaved is what the field was last given (the saved server).
func (p *settingsPage) signalSavedValue() string {
	if p.rt == nil {
		return ""
	}
	v, _, _ := signalFields(*p.rt)
	return v
}

// commitSignal is settingspages.js commitSignal: PUT /api/share/signal when the field differs from the saved server.
// keel serve validates the address; its words show in a toast.
func (w *workspace) commitSignal(p *settingsPage) {
	url := strings.TrimSpace(p.signal)
	if p.signalSaving || p.rt == nil || url == p.signalSavedValue() {
		return
	}
	p.signalSaving = true
	call(w, func(c context.Context) (keelclient.Runtime, error) { return w.be.SetSignal(c, url) }, func(info keelclient.Runtime, err error) {
		p.signalSaving = false
		if err != nil {
			w.warn(errText("Could not save the signal server", err))
			return
		}
		if p.rt != nil {
			p.rt.SignalURL, p.rt.SignalSource, p.rt.SignalSaved = info.SignalURL, info.SignalSource, info.SignalSaved
		}
		p.signalSeen = false // the field takes the saved value again
		if url == "" {
			w.say("Signal server removed")
		} else {
			w.say("Signal server saved")
		}
	})
}

// askStopRuntime is Stop runtime…: it asks first, with the session count it knows.
func (w *workspace) askStopRuntime() {
	n := w.paneCount()
	if p := pageAs[*settingsPage](w.pageTab("settings")); p != nil && p.rt != nil {
		n = p.rt.Sessions
	}
	w.askConfirm(&confirmDialog{title: "Stop the Keel runtime?", message: stopMessage(n), action: "Stop runtime", run: w.stopRuntime})
}

// staleRuntimeNotice is the warning shown when keel serve is from another build.
const staleRuntimeNotice = "The Keel runtime that's running is from a different build than this app, so you're seeing its version of Keel. " +
	"Stop the runtime and reopen Keel to update. Open sessions will end."

// runtimeIsStale: a window opened without a build id never warns; a runtime that reports none is older.
func runtimeIsStale(appBuild, runtimeBuild string) bool {
	return appBuild != "" && appBuild != runtimeBuild
}

// checkRuntimeBuild warns once, when the window opens, if keel serve is from another build than the window. It
// is a toast whose button asks the Stop runtime question.
func (w *workspace) checkRuntimeBuild() {
	if w.appBuild == "" {
		return
	}
	call(w, w.be.Runtime, func(rt keelclient.Runtime, err error) {
		if err == nil && runtimeIsStale(w.appBuild, rt.Build) {
			w.sayAction(staleRuntimeNotice, "Stop runtime", w.askStopRuntime)
		}
	})
}

// stopRuntime is the confirm's action: POST /api/runtime/stop. keel serve ends every session and exits.
func (w *workspace) stopRuntime() {
	callErr(w, w.be.StopRuntime, func(err error) {
		if err != nil {
			w.warn(errText("stop runtime", err))
			return
		}
		w.say("Keel runtime stopped")
	})
}

// ---- the sliders ----

// syncSliders tells a slider the user moved from a value changed elsewhere (⌘+ for the text size) and applies the first.
func (w *workspace) syncSliders(p *settingsPage) {
	step := func(v *float64, applied *int, current int, apply func(int)) {
		if n := int(math.Round(*v)); n != *applied { // the user moved it: applied when the frame is built, so the next one paints it
			*applied = n
			w.act(func() { apply(n) })
			return
		}
		if current != *applied { // changed elsewhere
			*applied, *v = current, float64(current)
		}
	}
	step(&p.opacityV, &p.opacityApplied, w.prefs.Opacity, w.setOpacity)
	step(&p.blurV, &p.blurApplied, w.prefs.Blur, w.setBlur)
	step(&p.sizeV, &p.sizeApplied, w.prefs.TermSize, w.setTermSize)
	step(&p.uiV, &p.uiApplied, w.prefs.UISize, w.setUIPref)
}

// hasBlur reports a window that has the blur row (macOS only, as data-blur-only).
func hasBlur() bool { return isMac() }

// initSliders gives the sliders the current values when the page is made.
func (w *workspace) initSliders(p *settingsPage) {
	p.opacityV, p.opacityApplied = float64(w.prefs.Opacity), w.prefs.Opacity
	p.blurV, p.blurApplied = float64(w.prefs.Blur), w.prefs.Blur
	p.sizeV, p.sizeApplied = float64(w.prefs.TermSize), w.prefs.TermSize
	p.uiV, p.uiApplied = float64(w.prefs.UISize), w.prefs.UISize
}
