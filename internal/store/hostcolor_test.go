package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyHexHostColorReadsAsToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keel.json")
	legacy := `{
  "hosts": [
    {"id":"h1","name":"a","address":"10.0.0.1","port":22,"username":"root","authType":"password","color":"var(--ansi-12)","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},
    {"id":"h2","name":"b","address":"10.0.0.2","port":22,"username":"root","authType":"password","color":"#123456","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}
  ],
  "snippets": [],
  "knownHosts": {}
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"h1": "var(--ansi-12)", "h2": "#123456"}
	for _, h := range hosts {
		if h.Color != want[h.ID] {
			t.Errorf("host %s color = %q, want %q", h.ID, h.Color, want[h.ID])
		}
	}
	full, err := st.GetHost("h1")
	if err != nil {
		t.Fatal(err)
	}
	if full.Color != "var(--ansi-12)" {
		t.Errorf("GetHost color = %q", full.Color)
	}
}

func TestHostColorsAreTokens(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range HostColors {
		if c.Name == "" || seen[c.Value] {
			t.Fatalf("bad swatch %+v", c)
		}
		seen[c.Value] = true
	}
	for hex, tok := range legacyHostColors {
		if !seen[tok] {
			t.Errorf("legacy %s maps to %s, not a swatch", hex, tok)
		}
	}
	if DefaultHostColor != HostColors[0].Value {
		t.Error("the default must be the first swatch: term.js picks the first radio for a new host")
	}
}
