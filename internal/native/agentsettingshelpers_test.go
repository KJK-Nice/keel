package native

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"keel/internal/native/keelclient"
)

var testGemini = keelclient.AgentProvider{ID: "gemini", Name: "Google Gemini", Kind: "model", KeyLabel: "Gemini API key", Models: []string{"a", "b"}}

func TestLinksMustBeHttps(t *testing.T) {
	for in, want := range map[string]string{"javascript:alert(1)": "", "http://x.test": "", "https://x.test/a": "https://x.test/a", "": ""} {
		if got := safeURL(in); got != want {
			t.Errorf("safeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProvidersSayWhatTheyAreFor(t *testing.T) {
	if !strings.Contains(providerPurpose("jev"), "Plain-language detection") || providerPurpose("gemini") == "" || providerPurpose("anthropic") != providerPurpose("gemini") || providerPurpose("nope") != "" {
		t.Error("the purposes are not the app's")
	}
}

func TestDetectionNoteAutoNeedsJevHashLinesNever(t *testing.T) {
	if n := detectionNote("on", "auto", false); !strings.Contains(n, "Jev is not connected") || !strings.Contains(n, "only lines that start with #") {
		t.Errorf("auto without Jev: %q", n)
	}
	if n := detectionNote("on", "auto", true); !strings.Contains(n, "⇥ stays the shell's") || !strings.Contains(n, suggestKeyHere()) {
		t.Errorf("auto with Jev: %q", n)
	}
	if n := detectionNote("on", "hash-only", false); !strings.Contains(n, "comment") || !strings.Contains(n, "Nothing you type is sent") {
		t.Errorf("hash-only: %q", n)
	}
	if n := detectionNote("off", "auto", false); n != "" {
		t.Errorf("suggestions off: %q", n)
	}
}

func TestHistoryLinesAndModels(t *testing.T) {
	for in, want := range map[int]string{0: "0", -3: "0", 30: "30", 45: "30", 300: "365", 7: "7", 100: "90"} {
		if got := historyValue(in); got != want {
			t.Errorf("historyValue(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int]int{10: 50, 5000: 1000, 0: 200, 50: 50, 1000: 1000, 333: 333} {
		if got := clampLines(in); got != want {
			t.Errorf("clampLines(%d) = %d, want %d", in, got, want)
		}
	}
	if len(historyChoices) != 5 || historyChoices[0][1] != "Only while the panel is open" || historyChoices[4][1] != "1 year" {
		t.Errorf("history choices %v", historyChoices)
	}
	if len(agentScopes) != 3 || agentScopes[0] != [2]string{"focused", "Focused pane only"} || agentScopes[1][1] != "Panes I pick each time" || agentScopes[2][1] != "Every pane in the tab" {
		t.Errorf("scopes %v", agentScopes)
	}
}

func TestModelsOfEveryModelProviderLabelledWithTheProviderAndTheProviderEachPicks(t *testing.T) {
	claude := keelclient.AgentProvider{ID: "anthropic", Name: "Anthropic Claude", ShortName: "Claude", Kind: "model", Models: []string{"c1", "c2"}}
	jev := keelclient.AgentProvider{ID: "jev", Name: "TypeSafe Jev", Kind: "detector", KeyLabel: "Jev API key"}
	items := modelItems([]keelclient.AgentProvider{testGemini, claude, jev}, "gemini", "a")
	var shape [][2]string
	for _, it := range items {
		shape = append(shape, [2]string{it.value, it.provider})
	}
	if want := [][2]string{{"a", "gemini"}, {"b", "gemini"}, {"c1", "anthropic"}, {"c2", "anthropic"}}; !reflect.DeepEqual(shape, want) {
		t.Errorf("items %v", shape)
	}
	if items[0].label != "a · Google Gemini" || items[2].label != "c1 · Claude" {
		t.Errorf("labels %q %q", items[0].label, items[2].label)
	}
	// A custom model stays first, under the saved provider.
	custom := modelItems([]keelclient.AgentProvider{testGemini, claude}, "anthropic", "x")
	if custom[0] != (modelItem{"x", "x · Claude", "anthropic"}) {
		t.Errorf("custom %+v", custom[0])
	}
	// One model provider: bare model names.
	var labels []string
	for _, it := range modelItems([]keelclient.AgentProvider{testGemini, jev}, "gemini", "a") {
		labels = append(labels, it.label)
	}
	if !reflect.DeepEqual(labels, []string{"a", "b"}) {
		t.Errorf("labels %v", labels)
	}
}

func TestAKeyErrorNeverCarriesTheRequest(t *testing.T) {
	if got := keyError(&keelclient.StatusError{Code: 423}); got != "The vault is locked. Unlock it, then try again." {
		t.Error(got)
	}
	if got := keyError(&keelclient.StatusError{Code: 400, Msg: "the key is empty, too long, or has spaces"}); got != "the key is empty, too long, or has spaces" {
		t.Error(got)
	}
	if got := keyError(errors.New("PUT /api/agent/keys/gemini: connection refused")); got != "Could not save the key." {
		t.Error(got)
	}
	if !strings.Contains(disconnectMessage("jev", "TypeSafe Jev"), "stop noticing plain language") || !strings.Contains(disconnectMessage("gemini", "Google Gemini"), "will stop working until you connect a key again") {
		t.Error("the disconnect words are not the app's")
	}
}
