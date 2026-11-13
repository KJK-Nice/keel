package hostimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"keel/internal/sshconfig"
	"keel/internal/store"
)

// The import saves through the store, so a host the API would refuse is
// refused here too: with no local user to fall back on, a Host without User
// has nobody to sign in as. It is reported and the other host is added.
func TestImportRunsTheStoresHostChecks(t *testing.T) {
	t.Setenv(store.EnvPassphrase, "test-vault-pass")
	st, err := store.New(filepath.Join(t.TempDir(), "keel.json"), store.TestVault())
	if err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(t.TempDir(), ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "Host nouser\n  HostName 10.0.0.9\n\nHost named\n  HostName 10.0.0.10\n  User deploy\n"
	if err := os.WriteFile(filepath.Join(ssh, "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Import(st, sshconfig.Options{SSHDir: ssh, LocalUser: ""}, Request{Hosts: []string{"nouser", "named"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hosts) != 1 || res.Hosts[0].Name != "named" {
		t.Fatalf("added %+v, want only named", res.Hosts)
	}
	if len(res.Failed) != 1 || res.Failed[0].Alias != "nouser" || !strings.Contains(res.Failed[0].Error, "username is required") {
		t.Fatalf("failed = %+v, want nouser refused for its missing user", res.Failed)
	}
	saved, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range saved {
		if h.Name == "nouser" {
			t.Fatal("a host with no user was saved")
		}
	}
}
