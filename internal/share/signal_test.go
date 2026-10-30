package share

import "testing"

func TestNormalizeSignal(t *testing.T) {
	ok := map[string]string{
		"":                                 "",
		"  ":                               "",
		"https://signal.example":           "https://signal.example",
		" https://signal.example/ ":        "https://signal.example",
		"http://127.0.0.1:47242":           "http://127.0.0.1:47242",
		"https://example.com/keel-signal/": "https://example.com/keel-signal",
	}
	for in, want := range ok {
		got, err := NormalizeSignal(in)
		if err != nil || got != want {
			t.Errorf("NormalizeSignal(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"signal.example",
		"ftp://signal.example",
		"https://",
		"javascript:alert(1)",
		"https://user:pw@signal.example",
		"https://signal.example/?room=x",
		"https://signal.example/#k=x",
		"https://signal.example/?",
	} {
		if got, err := NormalizeSignal(in); err == nil {
			t.Errorf("NormalizeSignal(%q) = %q, want an error", in, got)
		}
	}
}

// KEEL_SIGNAL beats the server saved in Settings, which beats the one built
// in.
func TestHubSignalSources(t *testing.T) {
	t.Setenv("KEEL_SIGNAL", "")
	old := DefaultSignal
	DefaultSignal = "https://baked.example/"
	t.Cleanup(func() { DefaultSignal = old })

	h := FromEnv(nil)
	if h.URL() != "https://baked.example" || h.Source() != SourceBuild {
		t.Fatalf("built in: %q (%s)", h.URL(), h.Source())
	}
	h.SetSaved("https://saved.example")
	if h.URL() != "https://saved.example" || h.Source() != SourceSettings {
		t.Fatalf("saved: %q (%s)", h.URL(), h.Source())
	}
	h.SetSaved("")
	if h.Source() != SourceBuild {
		t.Fatalf("after removing the saved server: %s", h.Source())
	}

	t.Setenv("KEEL_SIGNAL", "https://env.example")
	h = FromEnv(nil)
	h.SetSaved("https://saved.example")
	if h.URL() != "https://env.example" || h.Source() != SourceEnv {
		t.Fatalf("env: %q (%s)", h.URL(), h.Source())
	}

	none := New("", nil)
	if none.Enabled() || none.Source() != "" {
		t.Fatalf("no server: enabled=%v source=%q", none.Enabled(), none.Source())
	}
	none.SetSaved("https://saved.example")
	if !none.Enabled() {
		t.Fatal("a saved server should enable sharing")
	}
	var nilHub *Hub
	if nilHub.Enabled() || nilHub.URL() != "" || nilHub.Source() != "" {
		t.Fatal("a nil hub has no server")
	}
	nilHub.SetSaved("https://x.example")
}
