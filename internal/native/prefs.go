package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// prefs are the window preferences, named and ranged as the webview's (cmd/keel/web/js/term.js). The
// file is only read here; the Settings page that writes it is phase N5.
type prefs struct {
	Opacity int `json:"opacity"` // 30–100; 100 is opaque
	Blur    int `json:"blur"`    // 0–64, macOS only, used below 100

	TermSize int `json:"termSize"` // terminal text size, 10–22, set by ⌘+ ⌘- ⌘0

	Theme  string `json:"theme"`  // system | light | dark: Settings · Appearance · Theme
	UISize int    `json:"uiSize"` // 11–16: Settings · Appearance · UI size (text and rem sizes; the terminal is separate)

	TermFont string `json:"termFont"` // Settings · Terminal · Font: jetbrains | ibmplex | menlo | consolas | system
}

const (
	opacityMin, opacityMax, opacityDefault = 30, 100, 66
	blurMin, blurMax, blurDefault          = 0, 64, 20
)

// loadPrefs reads <dataDir>/keel-native.json. A missing or unreadable file, or a value out of range,
// gives the default, field by field.
func loadPrefs(dataDir string) prefs {
	p := prefs{Opacity: opacityDefault, Blur: blurDefault, TermSize: termSizeDefault, Theme: themeSystem, UISize: uiSizeDefault, TermFont: fontJetBrains}
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	if err != nil {
		return p
	}
	var f struct {
		Opacity, Blur, TermSize, UISize *int
		Theme, TermFont                 string
	}
	if json.Unmarshal(b, &f) != nil {
		return p
	}
	p.Theme, p.TermFont = validTheme(f.Theme), validTermFont(f.TermFont)
	if f.UISize != nil && *f.UISize >= uiSizeMin && *f.UISize <= uiSizeMax {
		p.UISize = *f.UISize
	}
	if f.TermSize != nil && *f.TermSize >= termSizeMin && *f.TermSize <= termSizeMax {
		p.TermSize = *f.TermSize
	}
	if f.Opacity != nil && *f.Opacity >= opacityMin && *f.Opacity <= opacityMax {
		p.Opacity = *f.Opacity
	}
	if f.Blur != nil && *f.Blur >= blurMin && *f.Blur <= blurMax {
		p.Blur = *f.Blur
	}
	return p
}

// loadRecentHosts reads the recent host ids (newest first, at most recentMax) from keel-native.json:
// strings only, whatever else the file holds.
func loadRecentHosts(dataDir string) (ids []string) {
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	if err != nil {
		return nil
	}
	var f struct{ RecentHosts []any }
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	for _, v := range f.RecentHosts {
		if id, ok := v.(string); ok && id != "" && len(ids) < recentMax {
			ids = append(ids, id)
		}
	}
	return ids
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

// withEnv applies KEEL_NATIVE_OPACITY and KEEL_NATIVE_BLUR (for development and tests) over p.
func (p prefs) withEnv() (prefs, error) {
	for _, e := range []struct {
		name     string
		dst      *int
		min, max int
	}{
		{"KEEL_NATIVE_OPACITY", &p.Opacity, opacityMin, opacityMax},
		{"KEEL_NATIVE_BLUR", &p.Blur, blurMin, blurMax},
	} {
		v := os.Getenv(e.name)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < e.min || n > e.max {
			return p, fmt.Errorf("%s must be an integer from %d to %d, got %q", e.name, e.min, e.max, v)
		}
		*e.dst = n
	}
	return p, nil
}

func (p prefs) alpha() float32 { return float32(p.Opacity) / 100 }

// termPrefs are Settings · Terminal · Copy on select and Right-click in the terminal, and the Sharing display name
// (term.js keel-term and keel-name-custom, kept in the webview's localStorage; here in keel-native.json).
type termPrefs struct {
	CopyOnSelect bool   // copy the terminal selection to the clipboard (the app's default is on)
	RightClick   string // paste | menu | nothing (the app's default is menu)
	Name         string // the custom display name, "" for the default
}

const (
	rightPaste, rightMenu, rightNothing = "paste", "menu", "nothing"
	nameKey                             = "name"
)

var rightClickModes = [][2]string{{rightPaste, "Paste"}, {rightMenu, "Menu"}, {rightNothing, "Nothing"}}

// loadTermPrefs reads them, field by field: a missing or invalid value is the default.
func loadTermPrefs(dataDir string) termPrefs {
	p := termPrefs{CopyOnSelect: true, RightClick: rightMenu}
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	if err != nil {
		return p
	}
	var f struct {
		CopyOnSelect *bool
		RightClick   string
		Name         string
	}
	if json.Unmarshal(b, &f) != nil {
		return p
	}
	if f.CopyOnSelect != nil {
		p.CopyOnSelect = *f.CopyOnSelect
	}
	for _, m := range rightClickModes {
		if f.RightClick == m[0] {
			p.RightClick = f.RightClick
		}
	}
	p.Name = strings.TrimSpace(f.Name)
	if r := []rune(p.Name); len(r) > nameMax {
		p.Name = string(r[:nameMax])
	}
	return p
}
