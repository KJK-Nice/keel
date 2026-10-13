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
