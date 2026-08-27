package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := New(filepath.Join(t.TempDir(), "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Locked() {
		t.Fatal("expected unlocked store")
	}
	return st
}

func TestHostRoundTrip(t *testing.T) {
	st := testStore(t)
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected seeded local host, got %d", len(hosts))
	}
	saved, err := st.UpsertHost(Host{
		Name:     "api",
		Group:    "prod",
		Address:  "10.0.0.8",
		Port:     22,
		Username: "ubuntu",
		AuthType: "password",
		Password: "secret",
		Color:    "#6ea8ff",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "secret" || got.Name != "api" {
		t.Fatalf("unexpected host: %+v", got)
	}

	got.Password = ""
	got.Name = "api-1"
	updated, err := st.UpsertHost(got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Password != "secret" {
		t.Fatal("empty password should keep the saved secret")
	}
	if updated.Name != "api-1" {
		t.Fatal("name should update")
	}

	listed, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	var view Host
	for _, h := range listed {
		if h.ID == saved.ID {
			view = h
		}
	}
	if view.Password != "x" {
		t.Fatalf("list should not expose the real password, got %q", view.Password)
	}

	raw, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatal("keel.json still contains the password")
	}
	vault, err := os.ReadFile(st.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(vault), "secret") {
		t.Fatal("vault blob contains plaintext secret")
	}

	if err := st.DeleteHost(saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetHost(saved.ID); err != ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}

	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	hosts2, err := st2.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts2) != 1 {
		t.Fatalf("reloaded store should still have local host, got %d", len(hosts2))
	}
}

func TestKnownHostsTOFU(t *testing.T) {
	st := testStore(t)
	if err := st.CheckHostKey("example:22", "SHA256:abc"); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckHostKey("example:22", "SHA256:abc"); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckHostKey("example:22", "SHA256:other"); err == nil {
		t.Fatal("expected mismatch")
	}
}

func TestMigratePlaintextSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keel.json")
	legacy := `{
  "hosts": [{"id":"h1","name":"box","group":"","address":"10.0.0.1","port":22,"username":"root","authType":"password","password":"hunter2","color":"#5ee0b5","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}],
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
	got, err := st.GetHost("h1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "hunter2" {
		t.Fatalf("migrated password = %q", got.Password)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("plaintext password remained in keel.json")
	}
}

func TestLockedUntilSetup(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	_ = os.Unsetenv("KEEL_VAULT_PASSPHRASE")
	st, err := New(filepath.Join(t.TempDir(), "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Locked() && !st.NeedsSetup() {
		// keyring may work on this machine
		if _, err := st.ListHosts(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !st.Locked() {
		t.Fatal("expected locked without passphrase or keyring")
	}
	if _, err := st.ListHosts(); err != ErrLocked {
		t.Fatalf("list = %v", err)
	}
	if err := st.Setup("choose-a-phrase"); err != nil {
		t.Fatal(err)
	}
	if st.Locked() {
		t.Fatal("should unlock after setup")
	}
	hosts, err := st.ListHosts()
	if err != nil || len(hosts) != 1 {
		t.Fatalf("hosts %v %v", hosts, err)
	}
}

func TestIdentityConnectAndTunnelPersist(t *testing.T) {
	st := testStore(t)
	ident, err := st.UpsertIdentity(Identity{
		Name: "deploy", Kind: "password", Username: "ubuntu", Password: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := st.ListIdentities()
	if err != nil {
		t.Fatal(err)
	}
	if listed[0].Password != "x" {
		t.Fatalf("list leaked password %q", listed[0].Password)
	}
	host, err := st.UpsertHost(Host{
		Name: "api", Address: "10.0.0.3", Port: 22, Username: "",
		AuthType: "identity", IdentityID: ident.ID, Color: "#6ea8ff",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.HostForConnect(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "s3cret" || got.AuthType != "password" || got.Username != "ubuntu" {
		t.Fatalf("connect host %+v", got)
	}
	tn, err := st.UpsertTunnel(Tunnel{
		Name: "db", HostID: host.ID, Kind: "local",
		BindAddr: "127.0.0.1", BindPort: 15432,
		DestHost: "127.0.0.1", DestPort: 5432,
	})
	if err != nil {
		t.Fatal(err)
	}
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st2.GetTunnel(tn.ID)
	if err != nil || again.BindPort != 15432 {
		t.Fatalf("reload tunnel %v %v", again, err)
	}
	if err := st2.DeleteIdentity(ident.ID); err != nil {
		t.Fatal(err)
	}
	h2, err := st2.GetHost(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h2.IdentityID != "" {
		t.Fatalf("identity id should be cleared, got %q", h2.IdentityID)
	}
}
