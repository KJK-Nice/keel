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
	dark := map[string]string{}
	for _, tk := range doc.Color.Tokens {
		var v struct{ Dark string }
		if err := json.Unmarshal(tk.Value, &v); err != nil {
			t.Fatal(err)
		}
		dark[tk.Name] = v.Dark
	}
	resolve := func(name string) string {
		v := dark[name]
		for strings.HasPrefix(v, "{") {
			v = dark[strings.Trim(v, "{}")]
		}
		return v
	}
	colors := map[string]string{
		"chrome": hexChrome, "stage": hexStage, "surface": hexSurface, "raised": hexRaised,
		"hover": hexHover, "line": hexLine, "line-strong": hexLineStrong, "ink": hexInk,
		"ink-muted": hexInkMuted, "ink-faint": hexInkFaint, "brass": hexBrass,
		"brass-ink": hexBrassInk, "brass-soft": hexBrassSoft, "tide": hexTide, "signal": hexSignal, "flare": hexFlare, "term-selection": hexTermSelection,
		"brass-strong": hexBrassStrong, "on-brass": hexOnBrass, "signal-soft": hexSignalSoft, "flare-soft": hexFlareSoft,
		"on-flare": hexOnFlare, "haze": hexHaze, "haze-soft": hexHazeSoft, "scrim": hexScrim, "tide-soft": hexTideSoft,
		"focus": hexFocus, "term-fg": hexTermFg, "term-bg": hexTermBg, "term-cursor": hexTermCursor,
	}
	for i, h := range hexAnsi {
		colors[fmt.Sprintf("ansi-%d", i)] = h
	}
	for name, got := range colors {
		if want := resolve(name); got != want {
			t.Errorf("color %s = %q, tokens.json dark = %q", name, got, want)
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
		"size-sidebar": sizeSidebar, "space-0.5": space0_5, "space-1": space1, "space-1.5": space1_5, "space-2": space2,
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
				Value struct{ Dark string }
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
		if tk.Name == "shadow-modal" && !strings.HasPrefix(tk.Value.Dark, "0 24px 64px "+hexShadowModal+",") {
			t.Errorf("shadow-modal = %q, want a 0 24px 64px %s drop shadow", tk.Value.Dark, hexShadowModal)
		}
	}
}
