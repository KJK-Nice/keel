package native

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The keys of keel-native.json. A key this version does not know is kept as it is on every save.
const (
	keyOpacity       = "opacity"
	keyBlur          = "blur"
	keyTermSize      = "termSize"
	keyUISize        = "uiSize"
	keyTheme         = "theme"
	keyTermFont      = "termFont"
	keyTermWeight    = "termWeight"
	keyLineHeight    = "termLineHeight"
	keyLigatures     = "termLigatures"
	keyFeatures      = "termFeatures"
	keyCellWidth     = "termCellWidth"
	keyThicken       = "termThicken"
	keyContrast      = "termContrast"
	keySelectionBg   = "selectionBg"
	keySelectionText = "selectionText"
	keyCursorStyle   = "cursorStyle"
	keyCursorBlink   = "cursorBlink"
	keyOptionKey     = "optionKey"
	keyCopyOnSelect  = "copyOnSelect"
	keyRightClick    = "rightClick"
	keyDisplayName   = "name"
	keySidebarWidth  = "sidebarWidth"
	keyAgentWidth    = "agentWidth"
	keyAgentPanel    = "agentPanel"
	keyCollapsed     = "groupCollapsed"
	keyRecentHosts   = "recentHosts"
	keyClosedTabs    = "closedTabs"
)

// prefsDoc is the whole of keel-native.json, loaded once when the window starts. The workspace owns it as w.prefs and
// changes a field with the setter for it, which writes the key through setPref.
type prefsDoc struct {
	Opacity int // 30–100; 100 is opaque
	Blur    int // 0–64, macOS only, used below 100

	TermSize int // terminal text size, 10–22, set by ⌘+ ⌘- ⌘0

	Theme  string // system | light | dark: Settings · Appearance · Theme
	UISize int    // 11–16: Settings · Appearance · UI size (the terminal is separate)

	TermFont   string // Settings · Terminal · Font: jetbrains | ibmplex | menlo | consolas | system | family:<name>
	TermWeight int    // 100–900 in steps of 100: Settings · Terminal · Weight
	LineHeight int    // 100–200, the row height in % of the font's own: Settings · Terminal · Line height
	Ligatures  bool   // Settings · Terminal · Advanced: on by default
	Features   string // OpenType features as cleanFeatures keeps them: "ss01, cv02, -calt"
	CellWidth  int    // 80–150, the column width in % of the font's advance
	Thicken    bool   // macOS only

	Contrast      string // Settings · Terminal · Colors: off | some | high
	SelectionBg   string // theme | red | green | yellow | blue | magenta | cyan
	SelectionText string // the same

	CursorStyle string // block | bar | underline: panes opened afterwards
	CursorBlink bool   // on by default
	OptionKey   string // special | alt (macOS): what Option is to programs

	CopyOnSelect bool
	RightClick   string // paste | menu | nothing
	Name         string // Settings · Sharing · Display name; "" is the default

	SidebarWidth float32 // the sidebar's width as kept; 0 is the default
	AgentWidth   float32 // the agent panel's width as kept; 0 is the default
	AgentPanel   bool

	Collapsed   map[string]bool // the folders the tree shows closed (ids and flat group names)
	RecentHosts []string        // host ids, newest first, at most recentMax
	ClosedTabs  []closedEntry   // recently closed tabs and panes, newest first, at most closedMax
}

const (
	opacityMin, opacityMax, opacityDefault = 30, 100, 66
	blurMin, blurMax, blurDefault          = 0, 64, 20
)

const (
	rightPaste, rightMenu, rightNothing = "paste", "menu", "nothing"
)

var rightClickModes = [][2]string{{rightPaste, "Paste"}, {rightMenu, "Menu"}, {rightNothing, "Nothing"}}

// intPrefs are the integer keys with their range and default; a value outside the range loads as the default.
var intPrefs = []struct {
	key           string
	min, max, def int
	field         func(*prefsDoc) *int
}{
	{keyOpacity, opacityMin, opacityMax, opacityDefault, func(p *prefsDoc) *int { return &p.Opacity }},
	{keyBlur, blurMin, blurMax, blurDefault, func(p *prefsDoc) *int { return &p.Blur }},
	{keyTermSize, termSizeMin, termSizeMax, termSizeDefault, func(p *prefsDoc) *int { return &p.TermSize }},
	{keyUISize, uiSizeMin, uiSizeMax, uiSizeDefault, func(p *prefsDoc) *int { return &p.UISize }},
	{keyTermWeight, termWeightMin, termWeightMax, termWeightDefault, func(p *prefsDoc) *int { return &p.TermWeight }},
	{keyLineHeight, lineHeightMin, lineHeightMax, lineHeightDefault, func(p *prefsDoc) *int { return &p.LineHeight }},
	{keyCellWidth, cellWidthMin, cellWidthMax, cellWidthDefault, func(p *prefsDoc) *int { return &p.CellWidth }},
}

func defaultPrefs() prefsDoc {
	p := prefsDoc{Theme: themeSystem, TermFont: fontJetBrains, Ligatures: true, Contrast: contrastOff, SelectionBg: colorTheme, SelectionText: colorTheme, CursorStyle: cursorBlock, CursorBlink: true, OptionKey: optionSpecial, CopyOnSelect: true, RightClick: rightMenu, Collapsed: map[string]bool{}}
	for _, e := range intPrefs {
		*e.field(&p) = e.def
	}
	return p
}

// loadPrefs reads <dataDir>/keel-native.json. A missing or unreadable file, or a value that is missing, of the wrong
// type or out of range, gives the default, field by field.
func loadPrefs(dataDir string) prefsDoc {
	p := defaultPrefs()
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	if err != nil {
		return p
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return p
	}
	get := func(key string, dst any) bool { return raw[key] != nil && json.Unmarshal(raw[key], dst) == nil }
	for _, e := range intPrefs {
		var n int
		if get(e.key, &n) && n >= e.min && n <= e.max {
			*e.field(&p) = n
		}
	}
	var s string
	get(keyTheme, &s)
	p.Theme = validTheme(s)
	s = ""
	get(keyTermFont, &s)
	p.TermFont = validTermFont(s)
	get(keyLigatures, &p.Ligatures)
	get(keyThicken, &p.Thicken)
	s = ""
	get(keyFeatures, &s)
	p.Features = cleanFeatures(s)
	s = ""
	get(keyContrast, &s)
	p.Contrast = validContrast(s)
	s = ""
	get(keySelectionBg, &s)
	p.SelectionBg = validSelColor(s)
	s = ""
	get(keySelectionText, &s)
	p.SelectionText = validSelColor(s)
	s = ""
	get(keyCursorStyle, &s)
	p.CursorStyle = validChoice(s, cursorStyles)
	get(keyCursorBlink, &p.CursorBlink)
	s = ""
	get(keyOptionKey, &s)
	p.OptionKey = validChoice(s, optionModes)
	get(keyCopyOnSelect, &p.CopyOnSelect)
	s = ""
	get(keyRightClick, &s)
	for _, m := range rightClickModes {
		if s == m[0] {
			p.RightClick = s
		}
	}
	s = ""
	get(keyDisplayName, &s)
	p.Name = cutName(s)
	if get(keySidebarWidth, &p.SidebarWidth) && p.SidebarWidth <= 0 {
		p.SidebarWidth = 0
	}
	if get(keyAgentWidth, &p.AgentWidth) && p.AgentWidth < 0 {
		p.AgentWidth = 0
	}
	get(keyAgentPanel, &p.AgentPanel)
	var ids []string
	get(keyCollapsed, &ids)
	for _, id := range ids {
		p.Collapsed[id] = true
	}
	var list []any
	get(keyRecentHosts, &list)
	for _, v := range list { // strings only, whatever else the file holds
		if id, ok := v.(string); ok && id != "" && len(p.RecentHosts) < recentMax {
			p.RecentHosts = append(p.RecentHosts, id)
		}
	}
	list = nil
	get(keyClosedTabs, &list)
	for _, v := range list { // only entries naming a host, the title cut at 120 characters, "pane" or else "tab"
		e, _ := v.(map[string]any)
		id, _ := e["hostId"].(string)
		if id == "" {
			continue
		}
		title, _ := e["title"].(string)
		if r := []rune(title); len(r) > 120 {
			title = string(r[:120])
		}
		kind := "tab"
		if e["kind"] == "pane" {
			kind = "pane"
		}
		ms, _ := e["at"].(float64)
		p.ClosedTabs = append(p.ClosedTabs, closedEntry{hostID: id, title: title, kind: kind, at: time.UnixMilli(int64(ms))})
		if len(p.ClosedTabs) >= closedMax {
			break
		}
	}
	return p
}

func cutName(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > nameMax {
		s = string(r[:nameMax])
	}
	return s
}

func closedJSON(list []closedEntry) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, map[string]any{"hostId": e.hostID, "title": e.title, "kind": e.kind, "at": e.at.UnixMilli()})
	}
	return out
}

// collapsedList is the closed folders as the file keeps them, sorted so the file is stable.
func (p prefsDoc) collapsedList() []string {
	ids := []string{}
	for k := range p.Collapsed {
		ids = append(ids, k)
	}
	slices.Sort(ids)
	return ids
}

func (p prefsDoc) alpha() float32 { return float32(p.Opacity) / 100 }

// prefKeys is what each key writes, read from w.prefs, and the setting it names in the toast shown when the write fails
// ("save <title>").
var prefKeys = map[string]struct {
	title string
	value func(*prefsDoc) any
}{
	keyOpacity:       {"the window opacity", func(p *prefsDoc) any { return p.Opacity }},
	keyBlur:          {"the blur", func(p *prefsDoc) any { return p.Blur }},
	keyTermSize:      {"the text size", func(p *prefsDoc) any { return p.TermSize }},
	keyUISize:        {"the UI size", func(p *prefsDoc) any { return p.UISize }},
	keyTheme:         {"the theme", func(p *prefsDoc) any { return p.Theme }},
	keyTermFont:      {"the terminal font", func(p *prefsDoc) any { return p.TermFont }},
	keyTermWeight:    {"the font weight", func(p *prefsDoc) any { return p.TermWeight }},
	keyLineHeight:    {"the line height", func(p *prefsDoc) any { return p.LineHeight }},
	keyLigatures:     {"ligatures", func(p *prefsDoc) any { return p.Ligatures }},
	keyFeatures:      {"the font features", func(p *prefsDoc) any { return p.Features }},
	keyCellWidth:     {"the cell width", func(p *prefsDoc) any { return p.CellWidth }},
	keyThicken:       {"thicken", func(p *prefsDoc) any { return p.Thicken }},
	keyContrast:      {"the text contrast", func(p *prefsDoc) any { return p.Contrast }},
	keySelectionBg:   {"the selection background", func(p *prefsDoc) any { return p.SelectionBg }},
	keySelectionText: {"the selection text", func(p *prefsDoc) any { return p.SelectionText }},
	keyCursorStyle:   {"the cursor style", func(p *prefsDoc) any { return p.CursorStyle }},
	keyCursorBlink:   {"the cursor blink", func(p *prefsDoc) any { return p.CursorBlink }},
	keyOptionKey:     {"the Option key", func(p *prefsDoc) any { return p.OptionKey }},
	keyCopyOnSelect:  {"copy on select", func(p *prefsDoc) any { return p.CopyOnSelect }},
	keyRightClick:    {"the right-click setting", func(p *prefsDoc) any { return p.RightClick }},
	keyDisplayName:   {"the display name", func(p *prefsDoc) any { return p.Name }},
	keySidebarWidth:  {"the sidebar width", func(p *prefsDoc) any { return p.SidebarWidth }},
	keyAgentWidth:    {"the agent panel width", func(p *prefsDoc) any { return p.AgentWidth }},
	keyAgentPanel:    {"the agent panel", func(p *prefsDoc) any { return p.AgentPanel }},
	keyCollapsed:     {"the closed folders", func(p *prefsDoc) any { return p.collapsedList() }},
	keyRecentHosts:   {"recent hosts", func(p *prefsDoc) any { return slices.Clone(p.RecentHosts) }},
	keyClosedTabs:    {"recently closed", func(p *prefsDoc) any { return closedJSON(p.ClosedTabs) }},
}

// prefWrites holds the keys set since the last write. One write at a time takes all of them, so the file always ends
// with the value set last, and a burst of sets (a slider, a sash drag) is one write.
type prefWrites struct {
	write   sync.Mutex // held from taking the keys to the end of their write
	mu      sync.Mutex
	pending map[string]any
}

// setPref writes key of keel-native.json, as w.prefs holds it now, off the UI thread, leaving every other key as it
// is. Nothing is written without a data directory.
func (w *workspace) setPref(key string) {
	if w.dataDir == "" {
		return
	}
	v := prefKeys[key].value(&w.prefs)
	s := &w.prefWrites
	s.mu.Lock()
	queued := s.pending != nil
	if !queued {
		s.pending = map[string]any{}
	}
	s.pending[key] = v
	s.mu.Unlock()
	if !queued {
		w.work(func() {
			if what, err := w.flushPrefs(); err != nil {
				w.post(func() { w.prefsFailed(what, err) })
			}
		})
	}
}

// prefsFailed tells the user a settings write did not work, or that a bad file was put aside.
func (w *workspace) prefsFailed(what string, err error) {
	if errors.Is(err, errPrefsRepaired) {
		w.say(err.Error())
		return
	}
	w.fail(what, err)
}

// flushPrefs writes the keys set since the last write, now. what names them for a failed write.
func (w *workspace) flushPrefs() (what string, err error) {
	s := &w.prefWrites
	s.write.Lock()
	defer s.write.Unlock()
	s.mu.Lock()
	set := s.pending
	s.pending = nil
	s.mu.Unlock()
	if len(set) == 0 {
		return "", nil
	}
	titles := []string{}
	for k := range set {
		titles = append(titles, prefKeys[k].title)
	}
	slices.Sort(titles)
	return "save " + strings.Join(titles, ", "), savePrefs(w.dataDir, set)
}

var prefsMu sync.Mutex

// savePrefs sets these fields in <dataDir>/keel-native.json and leaves every other field as it is: the
// ones this version does not know too. A file that is not JSON is left alone and reported.
func savePrefs(dataDir string, set map[string]any) error {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	fields := map[string]json.RawMessage{}
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	repaired := false
	switch {
	case err == nil:
		if json.Unmarshal(b, &fields) != nil {
			if err := os.Rename(filepath.Join(dataDir, "keel-native.json"), filepath.Join(dataDir, "keel-native.json.bad")); err != nil {
				return err
			}
			fields, repaired = map[string]json.RawMessage{}, true
		}
	case !os.IsNotExist(err):
		return err
	}
	delete(fields, "windowShadow") // an old file's value is dropped
	for k, v := range set {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		fields[k] = raw
	}
	out, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(dataDir, "keel-native.json", out); err != nil {
		return err
	}
	if repaired {
		return errPrefsRepaired
	}
	return nil
}

// errPrefsRepaired is what savePrefs answers when the write worked but the old file was not JSON: it is kept as
// keel-native.json.bad and a new one was written.
//
//lint:ignore ST1005 shown to the user in the app's words
var errPrefsRepaired = errors.New("The settings file was not valid. It is kept as keel-native.json.bad and a new one was written")

// writeFileAtomic writes dir/name through a temp file in dir and a rename, with mode 0600.
func writeFileAtomic(dir, name string, data []byte) error {
	f, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, name))
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
