//go:build !keelfake

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakePkg = "keel/internal/agent/provider/providertest"

func TestFakeProviderIsRefusedWithoutItsTag(t *testing.T) {
	if _, _, _, err := agentProviders("fake"); err == nil || !strings.Contains(err.Error(), "keelfake") {
		t.Fatalf("KEEL_AGENT_PROVIDER=fake in a build without the tag: err = %v, want a refusal naming the tag", err)
	}
	for _, v := range []string{"", "gemini", "FAKE", "fake ", "http://example.com"} {
		f, j, fake, err := agentProviders(v)
		if err != nil || fake || f == nil || j == nil {
			t.Errorf("KEEL_AGENT_PROVIDER=%q: fake=%v err=%v, want the real clients", v, fake, err)
		}
	}
}

// keel serve stops before it opens the store or a port.
func TestRunServeRefusesTheFakeWithoutItsTag(t *testing.T) {
	t.Setenv(agentProviderEnv, "fake")
	dir := filepath.Join(t.TempDir(), "data")
	if err := RunServe("127.0.0.1:0", dir); err == nil || !strings.Contains(err.Error(), "keelfake") {
		t.Fatalf("RunServe = %v, want a refusal", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("a refused keel serve made its data directory: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

func linksFake(t *testing.T, pkg, tags string) bool {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-tags="+tags, pkg)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", pkg, err, out)
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) == fakePkg {
			return true
		}
	}
	return false
}

func TestFakeProviderIsLinkedOnlyWithItsTag(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	for _, pkg := range []string{"./cmd/keel", "./cmd/keel-native"} {
		if linksFake(t, pkg, "") {
			t.Errorf("%s without keelfake links %s", pkg, fakePkg)
		}
	}
	if !linksFake(t, "./cmd/keel", "keelfake") {
		t.Errorf("./cmd/keel with keelfake does not link %s", fakePkg)
	}
}

// The tag is passed where a test binary is built and nowhere else.
func TestNoReleasePathPassesTheFakeTag(t *testing.T) {
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"native-e2e": true, "native-automation-build": true}
	seen := map[string]bool{}
	target := ""
	for _, l := range strings.Split(string(mk), "\n") {
		if l != "" && l[0] != '\t' && l[0] != ' ' && l[0] != '#' {
			if i := strings.Index(l, ":"); i > 0 {
				target = strings.TrimSpace(l[:i])
			}
		}
		if strings.Contains(l, "keelfake") && l[0] != '#' {
			seen[target] = true
			if !allowed[target] {
				t.Errorf("Makefile target %q passes the tag keelfake: %s", target, l)
			}
		}
	}
	for name := range allowed {
		if !seen[name] {
			t.Errorf("Makefile target %q does not build with the tag keelfake", name)
		}
	}
	scripts, _ := filepath.Glob(filepath.Join(root, "scripts", "*"))
	flows, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*"))
	for _, p := range append(scripts, flows...) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "keelfake") {
			t.Errorf("%s names the tag keelfake", p)
		}
	}
}
