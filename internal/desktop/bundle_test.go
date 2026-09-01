package desktop

import (
	"os"
	"strings"
	"testing"
)

func TestLooksLikeAppBundle(t *testing.T) {
	if !looksLikeAppBundle("/Users/me/Library/Caches/keel/Keel.app/Contents/MacOS/keel") {
		t.Fatal("expected bundle path")
	}
	if looksLikeAppBundle("/Users/me/dev/gosh/bin/keel") {
		t.Fatal("plain binary is not a bundle")
	}
}

func TestWithBundleEnv(t *testing.T) {
	got := withBundleEnv([]string{"PATH=/usr/bin", "KEEL_MAC_BUNDLE=0", "HOME=/tmp"})
	joined := strings.Join(got, "\n")
	if strings.Count(joined, "KEEL_MAC_BUNDLE=") != 1 || !strings.Contains(joined, "KEEL_MAC_BUNDLE=1") {
		t.Fatalf("env = %q", got)
	}
}

func TestMacOSBundleFiles(t *testing.T) {
	info, err := os.ReadFile("macos/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	s := string(info)
	for _, want := range []string{"CFBundleIdentifier", "app.keel.desktop", "CFBundleExecutable", "NSAllowsLocalNetworking", "CFBundleIconFile", "CFBundleIconName", "ApplePressAndHoldEnabled"} {
		if !strings.Contains(s, want) {
			t.Errorf("Info.plist missing %q", want)
		}
	}
	if !strings.Contains(s, "<key>CFBundleIconFile</key>") || !strings.Contains(s, "<string>Keel</string>") {
		t.Error("Info.plist missing CFBundleIconFile Keel")
	}
	if !strings.Contains(s, "<key>CFBundleIconName</key>") || !strings.Contains(s, "<string>Keel</string>") {
		t.Error("Info.plist missing CFBundleIconName Keel")
	}
	if !strings.Contains(s, "<key>ApplePressAndHoldEnabled</key>") || !strings.Contains(s, "<false/>") {
		t.Error("Info.plist must disable Apple press-and-hold so terminal keys repeat")
	}
	ent, err := os.ReadFile("macos/entitlements.plist")
	if err != nil {
		t.Fatal(err)
	}
	e := string(ent)
	for _, want := range []string{
		"com.apple.security.cs.allow-jit",
		"com.apple.security.cs.allow-unsigned-executable-memory",
		"com.apple.security.cs.disable-library-validation",
	} {
		if !strings.Contains(e, want) {
			t.Errorf("entitlements.plist missing %q", want)
		}
	}
}
