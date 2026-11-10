package native

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

type tokList struct {
	Tokens []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	} `json:"tokens"`
}

func TestTokensMatchJSON(t *testing.T) {
	raw, err := os.ReadFile("../../docs/design/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Color, Spacing, Radius, Size tokList
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, th := range []struct {
		id string
		p  *hexPalette
	}{{"dark", &darkHex}, {"light", &lightHex}} {
		vals := map[string]string{}
		for _, tk := range doc.Color.Tokens {
			var v map[string]string
			if err := json.Unmarshal(tk.Value, &v); err != nil {
				t.Fatal(err)
			}
			vals[tk.Name] = v[th.id]
		}
		resolve := func(name string) string {
			v := vals[name]
			for strings.HasPrefix(v, "{") {
				v = vals[strings.Trim(v, "{}")]
			}
			return v
		}
		p := th.p
		colors := map[string]string{
			"chrome": p.chrome, "stage": p.stage, "surface": p.surface, "raised": p.raised,
			"hover": p.hover, "line": p.line, "line-strong": p.lineStrong, "ink": p.ink,
			"ink-muted": p.inkMuted, "ink-faint": p.inkFaint, "brass": p.brass,
			"brass-ink": p.brassInk, "brass-soft": p.brassSoft, "tide": p.tide, "signal": p.signal, "flare": p.flare, "term-selection": p.termSelection,
			"brass-strong": p.brassStrong, "on-brass": p.onBrass, "signal-soft": p.signalSoft, "flare-soft": p.flareSoft,
			"on-flare": p.onFlare, "haze": p.haze, "haze-soft": p.hazeSoft, "scrim": p.scrim, "tide-soft": p.tideSoft,
			"focus": p.focus, "term-fg": p.termFg, "term-bg": p.termBg, "term-cursor": p.termCursor,
		}
		for i, h := range p.ansi {
			colors[fmt.Sprintf("ansi-%d", i)] = h
		}
		for name, got := range colors {
			if want := resolve(name); got != want {
				t.Errorf("%s color %s = %q, tokens.json = %q", th.id, name, got, want)
			}
		}
		if len(colors) != len(doc.Color.Tokens) {
			t.Errorf("%s: %d colours checked, tokens.json has %d", th.id, len(colors), len(doc.Color.Tokens))
		}
	}
	px := map[string]float64{}
	for _, l := range []tokList{doc.Spacing, doc.Radius, doc.Size} {
		for _, tk := range l.Tokens {
			var s string
			if json.Unmarshal(tk.Value, &s) != nil || !strings.HasSuffix(s, "px") {
				continue
			}
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64)
			if err != nil {
				t.Fatalf("%s: %v", tk.Name, err)
			}
			px[tk.Name] = n
		}
	}
	nums := map[string]float64{
		"size-titlebar": sizeTitlebar, "size-tab": sizeTab, "size-panebar": sizePanebar,
		"size-control": sizeControl, "size-control-sm": sizeControlSm, "size-row": sizeRow,
		"size-sidebar": sizeSidebar, "size-rail": sizeRail, "space-0.5": space0_5, "space-1": space1, "space-1.5": space1_5, "space-2": space2,
		"space-3": space3, "space-4": space4, "space-6": space6, "space-8": space8, "radius-xs": radiusXs, "radius-sm": radiusSm,
		"radius-md": radiusMd, "radius-lg": radiusLg, "radius-full": radiusFull, "size-control-lg": sizeControlLg,
	}
	for name, got := range nums {
		want, ok := px[name]
		if !ok {
			t.Errorf("%s not in tokens.json", name)
		} else if got != want {
			t.Errorf("%s = %v, tokens.json = %v", name, got, want)
		}
	}

	// The type sizes of the Display, Interface and Mono groups, and the modal shadow's colours.
	var typ struct {
		Type struct {
			Groups []struct {
				Styles []struct {
					Name     string
					FontSize string
				}
			}
		}
		Shadow struct {
			Tokens []struct {
				Name  string
				Value struct{ Dark, Light string }
			}
		}
		Vibrancy struct {
			Tokens []struct {
				Name  string
				Value json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(raw, &typ); err != nil {
		t.Fatal(err)
	}
	sizes := map[string]float64{"display": displaySize, "title": titleSize, "body": uiFontSize, "caption": captionSize, "kbd": kbdSize}
	for _, g := range typ.Type.Groups {
		for _, st := range g.Styles {
			if want, ok := sizes[st.Name]; ok {
				if st.FontSize != fmt.Sprintf("%gpx", want) {
					t.Errorf("type %s is %s in tokens.json, %g here", st.Name, st.FontSize, want)
				}
				delete(sizes, st.Name)
			}
		}
	}
	if len(sizes) != 0 {
		t.Errorf("type styles not found in tokens.json: %v", sizes)
	}
	for _, tk := range typ.Shadow.Tokens {
		for _, th := range []struct {
			id, got string
			p       *hexPalette
		}{{"dark", tk.Value.Dark, &darkHex}, {"light", tk.Value.Light, &lightHex}} {
			if tk.Name == "shadow-pop" && !strings.Contains(th.got, " "+th.p.shadowPop+",") {
				t.Errorf("shadow-pop %s = %q, want the drop shadow %s", th.id, th.got, th.p.shadowPop)
			}
			if tk.Name == "shadow-modal" && !strings.Contains(th.got, " "+th.p.shadowModal+",") {
				t.Errorf("shadow-modal %s = %q, want the drop shadow %s", th.id, th.got, th.p.shadowModal)
			}
		}
	}
	for _, tk := range typ.Vibrancy.Tokens {
		var v struct{ Dark, Light string }
		if tk.Name == "vibrancy-term-share" && (json.Unmarshal(tk.Value, &v) != nil || v.Dark != "1" || v.Light != fmt.Sprint(termShareLight)) {
			t.Errorf("vibrancy-term-share = %s, want dark 1 and light %v", tk.Value, termShareLight)
		}
	}
}
