package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The keys of keel-native.json. A key this version does not know is kept as it is on every save; windowShadow (retired in
// v0.2.1: the window has no shadow) is ignored on load and dropped by the next save.
const (
	keyOpacity      = "opacity"
	keyBlur         = "blur"
	keyTermSize     = "termSize"
	keyUISize       = "uiSize"
	keyTheme        = "theme"
	keyTermFont     = "termFont"
	keyCopyOnSelect = "copyOnSelect"
	keyRightClick   = "rightClick"
	keyDisplayName  = "name"
	keySidebarWidth = "sidebarWidth"
	keyAgentWidth   = "agentWidth"
	keyAgentPanel   = "agentPanel"
	keyCollapsed    = "groupCollapsed"
	keyRecentHosts  = "recentHosts"
	keyClosedTabs   = "closedTabs"
)

// prefsDoc is the whole of keel-native.json, loaded once when the window starts. The workspace owns it as w.prefs and
// changes a field with the setter for it, which writes the key through savePref.
type prefsDoc struct {
	Opacity int // 30–100; 100 is opaque
	Blur    int // 0–64, macOS only, used below 100

	TermSize int // terminal text size, 10–22, set by ⌘+ ⌘- ⌘0

	Theme  string // system | light | dark: Settings · Appearance · Theme
	UISize int    // 11–16: Settings · Appearance · UI size (the terminal is separate)

	TermFont string // Settings · Terminal · Font: jetbrains | ibmplex | menlo | consolas | system

	CopyOnSelect bool   // copy the terminal selection to the clipboard
	RightClick   string // paste | menu | nothing
	Name         string // Settings · Sharing · Display name; "" is the default

	SidebarWidth float32 // the sidebar's width as kept; 0 is the default
	AgentWidth   float32 // the agent panel's width as kept; 0 is the default
	AgentPanel   bool    // the agent panel was left open

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
}

// defaultPrefs is what a window starts with when there is no file: the defaults of the app.
func defaultPrefs() prefsDoc {
	p := prefsDoc{Theme: themeSystem, TermFont: fontJetBrains, CopyOnSelect: true, RightClick: rightMenu, Collapsed: map[string]bool{}}
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

// cutName trims a display name and cuts it to nameMax characters.
func cutName(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > nameMax {
		s = string(r[:nameMax])
	}
	return s
}

// closedJSON is the closed list as loadPrefs reads it.
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

// savePref writes one key of keel-native.json off the UI thread, leaving every other key as it is; what names the
// setting in the toast shown when the write fails ("save <what>"). Nothing is written without a data directory.
func (w *workspace) savePref(what, key string, v any) {
	dir := w.dataDir
	if dir == "" {
		return
	}
	w.work(func() {
		if err := savePrefs(dir, map[string]any{key: v}); err != nil {
			w.post(func() { w.fail("save "+what, err) })
		}
	})
}

var prefsMu sync.Mutex

// savePrefs sets these fields in <dataDir>/keel-native.json and leaves every other field as it is: the
// ones this version does not know too. A file that is not JSON is left alone and reported.
func savePrefs(dataDir string, set map[string]any) error {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	fields := map[string]json.RawMessage{}
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &fields); err != nil {
			return fmt.Errorf("keel-native.json is not JSON: %w", err)
		}
	case !os.IsNotExist(err):
		return err
	}
	delete(fields, "windowShadow") // retired in v0.2.1: the window has no shadow, and an old file's value is dropped
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
	return writeFileAtomic(dataDir, "keel-native.json", out)
}

// writeFileAtomic writes dir/name through a temp file in dir and a rename, with mode 0600.
func writeFileAtomic(dir, name string, data []byte) error {
	f, err := os.CreateTemp(dir, name+".*.tmp") // 0600
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
