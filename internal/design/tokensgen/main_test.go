package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The committed tokens.css files must be exactly what tokens.json renders,
// so tokens.json stays the single source.
func TestGenerateMatchesCommitted(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	src, err := os.ReadFile(filepath.Join(root, "docs", "design", "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := generate(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/design/tokens.css", "cmd/keel/web/css/tokens.css"} {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s is stale: run make ui", rel)
		}
	}
}

func TestGenerateResolvesReferences(t *testing.T) {
	src := []byte(`{"color":{"themes":[{"id":"dark"},{"id":"light"}],"tokens":[
		{"name":"brass","value":{"dark":"#e8b960","light":"#e7b551"}},
		{"name":"focus","value":{"dark":"{brass}","light":"{brass}"}}]},
		"spacing":{"tokens":[{"name":"space-0.5","value":"2px"}]}}`)
	got, err := generate(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{":root,[data-theme=dark]{\n--brass:#e8b960;\n--focus:var(--brass);\n}", "[data-theme=light]{\n--brass:#e7b551;", `--space-0\.5:2px;`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Type styles become --type-<name> font shorthands in rem (px / 13).
func TestGenerateTypeStyles(t *testing.T) {
	src := []byte(`{"type":{"families":{"sans":"S","display":"D","mono":"M"},"groups":[
		{"family":"display","styles":[{"name":"display","fontSize":"28px","lineHeight":"32px","fontWeight":600}]},
		{"family":"sans","styles":[{"name":"reading","fontSize":"14px","lineHeight":"22px","fontWeight":400},
			{"name":"body","fontSize":"13px","lineHeight":"18px","fontWeight":400}]}]}}`)
	got, err := generate(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--type-display:600 2.154rem/2.462rem var(--font-display);",
		"--type-reading:400 1.077rem/1.692rem var(--font-sans);",
		"--type-body:400 1rem/1.385rem var(--font-sans);",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if _, err := generate([]byte(`{"type":{"groups":[{"styles":[{"name":"x","fontSize":"1em","lineHeight":"2px"}]}]}}`)); err == nil {
		t.Error("a non-px size must fail")
	}
}

// The window translucency steps (Settings · Appearance) are tokens too.
func TestGenerateVibrancyTints(t *testing.T) {
	got, err := generate([]byte(`{"vibrancy":{"tokens":[{"name":"vibrancy-tint-medium","value":"66%"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "--vibrancy-tint-medium:66%;") {
		t.Errorf("missing vibrancy tint in:\n%s", got)
	}
}
