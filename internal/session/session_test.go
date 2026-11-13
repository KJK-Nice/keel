package session

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"keel/internal/store"
)

func TestParseSize(t *testing.T) {
	cols, rows := ParseSize("120", "40")
	if cols != 120 || rows != 40 {
		t.Fatalf("got %d x %d", cols, rows)
	}
	cols, rows = ParseSize("", "")
	if cols != 80 || rows != 24 {
		t.Fatalf("defaults: %d x %d", cols, rows)
	}
}

func TestLocalWorkingDirIsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	if got := localWorkingDir(); got != home {
		t.Fatalf("local shell dir %q, want home %q", got, home)
	}
}

// Every KEEL_* variable the code names is either a secret (store.SecretEnv,
// which a local shell never inherits) or listed here as holding none. A
// new variable fails this test until someone decides which it is.
func TestEverySecretVariableIsKeptFromLocalShells(t *testing.T) {
	plain := map[string]bool{
		"KEEL_ADDR": true, "KEEL_DATA": true, "KEEL_SSH_DIR": true, "KEEL_NAME": true, "KEEL_KEYRING": true,
		"KEEL_SIGNAL": true, "KEEL_SIGNAL_ADDR": true, "KEEL_SIGNAL_CLIENT_IP": true, "KEEL_AGENT_PROVIDER": true,
		"KEEL_NATIVE_CAPTURE": true, "KEEL_NATIVE_OPACITY": true, "KEEL_NATIVE_BLUR": true,
	}
	secret := map[string]bool{}
	for _, name := range store.SecretEnv {
		secret[name] = true
		if plain[name] {
			t.Errorf("%s is listed as both secret and plain", name)
		}
		in := []string{"A=1", name + "=hunter2", "B=2"}
		if got := shellEnv(in); len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
			t.Errorf("shellEnv kept %s: %q", name, got)
		}
	}
	if !secret["KEEL_VAULT_PASSPHRASE"] {
		t.Error("the vault passphrase variable is not in store.SecretEnv")
	}
	named := regexp.MustCompile(`"(KEEL_[A-Z0-9_]+)["=]`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
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
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range named.FindAllSubmatch(b, -1) {
			if name := string(m[1]); !secret[name] && !plain[name] {
				t.Errorf("%s names %s: add it to store.SecretEnv if it holds a secret, or to this test's plain list", p, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestShellEnvDropsVaultPassphrase(t *testing.T) {
	in := []string{
		"HOME=/home/u",
		"KEEL_VAULT_PASSPHRASE=hunter2",
		"KEEL_VAULT_PASSPHRASE_HINT=kept",
		"KEEL_ADDR=127.0.0.1:47241",
		"PATH=/usr/bin",
	}
	got := shellEnv(in)
	want := []string{
		"HOME=/home/u",
		"KEEL_VAULT_PASSPHRASE_HINT=kept",
		"KEEL_ADDR=127.0.0.1:47241",
		"PATH=/usr/bin",
	}
	if len(got) != len(want) {
		t.Fatalf("shellEnv = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shellEnv = %q, want %q", got, want)
		}
	}
}
