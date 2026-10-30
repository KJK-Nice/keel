package desktop

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestMacAppearanceFollowsThemeChoice(t *testing.T) {
	for choice, want := range map[string]application.MacAppearanceType{
		"light":  application.NSAppearanceNameAqua,
		"dark":   application.NSAppearanceNameDarkAqua,
		"system": application.DefaultAppearance, // cleared: the OS decides
	} {
		if got := macAppearance(choice); got != want {
			t.Errorf("macAppearance(%q) = %q, want %q", choice, got, want)
		}
	}
}

func TestParseAppearanceTakesOnlyKnownChoices(t *testing.T) {
	for msg, want := range map[string]string{
		"keel:appearance:light":  "light",
		"keel:appearance:dark":   "dark",
		"keel:appearance:system": "system",
	} {
		got, ok := parseAppearance(msg)
		if !ok || got != want {
			t.Errorf("parseAppearance(%q) = %q, %v", msg, got, ok)
		}
	}
	for _, msg := range []string{
		"",
		"keel:appearance:",
		"keel:appearance:Light",
		"keel:appearance:dark ",
		"keel:appearance:NSAppearanceNameVibrantDark",
		"appearance:dark",
		"wails:drag",
		"dark",
	} {
		if got, ok := parseAppearance(msg); ok {
			t.Errorf("parseAppearance(%q) accepted %q", msg, got)
		}
	}
}

func TestSameOriginIsTheKeelPage(t *testing.T) {
	page := "http://127.0.0.1:47241/?native=1&chrome=titlebar&appearance=native"
	for _, origin := range []string{
		"http://127.0.0.1:47241/?native=1&chrome=titlebar&appearance=native",
		"http://127.0.0.1:47241/",
	} {
		if !sameOrigin(page, origin) {
			t.Errorf("sameOrigin(%q) = false", origin)
		}
	}
	for _, origin := range []string{
		"",
		"http://127.0.0.1:47242/",
		"https://127.0.0.1:47241/",
		"http://localhost:47241/",
		"https://example.com/",
		"wails://wails/",
	} {
		if sameOrigin(page, origin) {
			t.Errorf("sameOrigin(%q) = true", origin)
		}
	}
	if sameOrigin("", "") {
		t.Error("an empty page URL matches nothing")
	}
}

func TestFollowsThemeNativelyOnlyOnMac(t *testing.T) {
	for goos, want := range map[string]bool{"darwin": true, "windows": false, "linux": false} {
		if got := followsThemeNatively(goos); got != want {
			t.Errorf("followsThemeNatively(%q) = %v", goos, got)
		}
	}
}

func TestRawMessageWithoutWindowIsIgnored(t *testing.T) {
	w := &nativeWindow{url: "http://127.0.0.1:47241/"}
	// No window yet: nothing to set, and no panic.
	w.rawMessage(nil, "keel:appearance:light", &application.OriginInfo{Origin: "http://127.0.0.1:47241/", IsMainFrame: true})
}

func TestParseBlurTakesOnlyARadiusInRange(t *testing.T) {
	for msg, want := range map[string]int{
		"keel:blur:0":  0,
		"keel:blur:7":  7,
		"keel:blur:64": 64,
	} {
		got, ok := parseBlur(msg)
		if !ok || got != want {
			t.Errorf("parseBlur(%q) = %d, %v", msg, got, ok)
		}
	}
	for _, msg := range []string{
		"",
		"keel:blur:",
		"keel:blur:65",
		"keel:blur:-1",
		"keel:blur:07",
		"keel:blur:+7",
		"keel:blur:7 ",
		"keel:blur:7.5",
		"keel:blur:100",
		"blur:7",
		"keel:appearance:dark",
	} {
		if got, ok := parseBlur(msg); ok {
			t.Errorf("parseBlur(%q) = %d, want rejected", msg, got)
		}
	}
}
