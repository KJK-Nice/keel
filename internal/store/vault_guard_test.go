package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The vault's test seams stay out of production code: no variable lowers
// the scrypt cost, the store does not ask whether it runs in a test, only
// tests pass TestVault, and every store a test opens takes it (or sets
// KEEL_KEYRING=off on the line above), so no test reaches the OS keychain.
func TestVaultSeamsAreForTestsOnly(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	ownNew := regexp.MustCompile(`(^|[^.\w])New\(`)
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".claude", ".scratch-native", "bin", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "internal/store/vault_guard_test.go" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		inStore := strings.HasPrefix(rel, "internal/store/")
		if strings.Contains(src, "KEEL_VAULT_"+"FAST") {
			t.Errorf("%s names the retired variable that lowered the scrypt cost", rel)
		}
		if !strings.HasSuffix(rel, "_test.go") {
			if strings.Contains(src, "TestVault(") && rel != "internal/store/vault.go" {
				t.Errorf("%s passes store.TestVault outside a test", rel)
			}
			if inStore && (strings.Contains(src, "\"testing\"") || strings.Contains(src, "testing.Testing")) {
				t.Errorf("%s imports testing", rel)
			}
			return nil
		}
		lines := strings.Split(src, "\n")
		for i, l := range lines {
			opens := strings.Contains(l, "store.New(") || (inStore && ownNew.MatchString(l) && !strings.HasPrefix(strings.TrimSpace(l), "//"))
			if !opens || strings.Contains(l, "TestVault()") {
				continue
			}
			if i > 0 && strings.TrimSpace(lines[i-1]) == `t.Setenv("KEEL_KEYRING", "off")` {
				continue
			}
			t.Errorf("%s:%d opens a store without TestVault(): %s", rel, i+1, strings.TrimSpace(l))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestVault's cheap work factor is in the file's header, so a vault it
// wrote opens with the same passphrase in a store without it, and a store
// without it writes age's own work factor.
func TestScryptWorkFactorIsAgesOwnOutsideTests(t *testing.T) {
	s := &Store{}
	if s.scryptWork != 0 {
		t.Fatal("a store built without TestVault lowers the scrypt work factor")
	}
	TestVault()(s)
	if s.scryptWork == 0 || !s.keyringOff {
		t.Fatalf("TestVault: scryptWork=%d keyringOff=%v", s.scryptWork, s.keyringOff)
	}
}
