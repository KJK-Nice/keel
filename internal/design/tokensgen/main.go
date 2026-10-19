// Command tokensgen writes tokens.css from docs/design/tokens.json, the
// single source of Keel's design tokens. `make ui` runs it for the design
// docs copy and the copy the app serves; `make check-generated` fails when
// either differs from what it writes.
//
//	go run ./internal/design/tokensgen docs/design/tokens.json out.css...
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type themed struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type group struct {
	Tokens []themed `json:"tokens"`
}

type tokens struct {
	Color struct {
		Themes []struct {
			ID string `json:"id"`
		} `json:"themes"`
		Tokens []themed `json:"tokens"`
	} `json:"color"`
	Type struct {
		Families struct {
			Sans    string `json:"sans"`
			Display string `json:"display"`
			Mono    string `json:"mono"`
		} `json:"families"`
		Groups []struct {
			Family string      `json:"family"`
			Styles []typeStyle `json:"styles"`
		} `json:"groups"`
	} `json:"type"`
	Spacing  group `json:"spacing"`
	Radius   group `json:"radius"`
	Size     group `json:"size"`
	Shadow   group `json:"shadow"`
	Duration group `json:"duration"`
	Vibrancy group `json:"vibrancy"`
}

type typeStyle struct {
	Name       string `json:"name"`
	FontSize   string `json:"fontSize"`
	LineHeight string `json:"lineHeight"`
	FontWeight int    `json:"fontWeight"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: tokensgen tokens.json out.css...")
		os.Exit(2)
	}
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	css, err := generate(src)
	if err != nil {
		fail(err)
	}
	for _, out := range os.Args[2:] {
		if err := os.WriteFile(out, []byte(css), 0o644); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tokensgen:", err)
	os.Exit(1)
}

// generate renders the token file. Dark is the default theme, so its
// values also sit on :root; light overrides them under [data-theme=light].
func generate(src []byte) (string, error) {
	var t tokens
	if err := json.Unmarshal(src, &t); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("/* Keel — generated from tokens.json */\n")
	for _, th := range t.Color.Themes {
		b.WriteString(selector(th.ID) + "{\n")
		for _, tok := range t.Color.Tokens {
			v, err := themeValue(tok, th.ID)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "--%s:%s;\n", tok.Name, v)
		}
		b.WriteString("}\n")
	}
	b.WriteString(":root{")
	fmt.Fprintf(&b, "--font-sans:%s;--font-display:%s;--font-mono:%s;", t.Type.Families.Sans, t.Type.Families.Display, t.Type.Families.Mono)
	for _, g := range []group{t.Spacing, t.Radius, t.Size, t.Duration, t.Vibrancy} {
		for _, tok := range g.Tokens {
			var v string
			if err := json.Unmarshal(tok.Value, &v); err != nil {
				return "", fmt.Errorf("%s: %w", tok.Name, err)
			}
			fmt.Fprintf(&b, "--%s:%s;", strings.ReplaceAll(tok.Name, ".", `\.`), v)
		}
	}
	for _, g := range t.Type.Groups {
		for _, st := range g.Styles {
			v, err := typeFont(st, g.Family)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "--type-%s:%s;", st.Name, v)
		}
	}
	b.WriteString("}\n")
	for _, th := range t.Color.Themes {
		b.WriteString(selector(th.ID) + "{")
		for _, tok := range t.Shadow.Tokens {
			v, err := themeValue(tok, th.ID)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "--%s:%s;", tok.Name, v)
		}
		b.WriteString("}")
	}
	return b.String(), nil
}

func selector(theme string) string {
	if theme == "dark" {
		return ":root,[data-theme=dark]"
	}
	return "[data-theme=" + theme + "]"
}

func themeValue(tok themed, theme string) (string, error) {
	var m map[string]string
	if err := json.Unmarshal(tok.Value, &m); err != nil {
		return "", fmt.Errorf("%s: %w", tok.Name, err)
	}
	v, ok := m[theme]
	if !ok {
		return "", fmt.Errorf("%s: no %s value", tok.Name, theme)
	}
	// {name} references another token.
	if strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
		return "var(--" + v[1:len(v)-1] + ")", nil
	}
	return v, nil
}

// typeFont renders a type style as a font shorthand for --type-<name>.
// Sizes are rem (px ÷ 13, the default UI size) so they follow the user's UI
// size, as docs/design/README.md asks; letter spacing and case stay with the
// rule that uses the style.
func typeFont(st typeStyle, family string) (string, error) {
	size, err := pxToRem(st.FontSize)
	if err != nil {
		return "", fmt.Errorf("type %s: %w", st.Name, err)
	}
	lh, err := pxToRem(st.LineHeight)
	if err != nil {
		return "", fmt.Errorf("type %s: %w", st.Name, err)
	}
	if family == "" {
		family = "sans"
	}
	return fmt.Sprintf("%d %s/%s var(--font-%s)", st.FontWeight, size, lh, family), nil
}

func pxToRem(px string) (string, error) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(px, "px"), 64)
	if err != nil || !strings.HasSuffix(px, "px") {
		return "", fmt.Errorf("%q is not a px value", px)
	}
	rem := strconv.FormatFloat(n/13, 'f', 3, 64)
	rem = strings.TrimRight(strings.TrimRight(rem, "0"), ".")
	return rem + "rem", nil
}
