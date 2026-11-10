package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// prefs are the window preferences, named and ranged as the webview's (cmd/keel/web/js/term.js). The
// file is only read here; the Settings page that writes it is phase N5.
type prefs struct {
	Opacity int `json:"opacity"` // 30–100; 100 is opaque
	Blur    int `json:"blur"`    // 0–64, macOS only, used below 100
}

const (
	opacityMin, opacityMax, opacityDefault = 30, 100, 66
	blurMin, blurMax, blurDefault          = 0, 64, 20
)

// loadPrefs reads <dataDir>/keel-native.json. A missing or unreadable file, or a value out of range,
// gives the default, field by field.
func loadPrefs(dataDir string) prefs {
	p := prefs{Opacity: opacityDefault, Blur: blurDefault}
	b, err := os.ReadFile(filepath.Join(dataDir, "keel-native.json"))
	if err != nil {
		return p
	}
	var f struct{ Opacity, Blur *int }
	if json.Unmarshal(b, &f) != nil {
		return p
	}
	if f.Opacity != nil && *f.Opacity >= opacityMin && *f.Opacity <= opacityMax {
		p.Opacity = *f.Opacity
	}
	if f.Blur != nil && *f.Blur >= blurMin && *f.Blur <= blurMax {
		p.Blur = *f.Blur
	}
	return p
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
