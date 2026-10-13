//go:build darwin

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildMacAppSignsCleanBundle(t *testing.T) {
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("codesign not available")
	}
	src, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	appDir := filepath.Join(cache, "keel", "Keel.app")
	// Stale file from older versions must be removed.
	if err := os.MkdirAll(filepath.Join(appDir, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(appDir, "Contents", "keel.entitlements")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // second pass = existing cached bundle
		if _, _, err := buildMacApp(src, cache); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(appDir, "Contents"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"Info.plist": true, "MacOS": true, "Resources": true, "_CodeSignature": true}
	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("unexpected %q in Contents", e.Name())
		}
	}
	out, err := exec.Command("codesign", "--verify", "--deep", "--strict", "--verbose=2", appDir).CombinedOutput()
	if err != nil {
		t.Fatalf("verify: %v: %s", err, out)
	}
	t.Logf("%s", out)
}
