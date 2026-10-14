package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestHostTagsPersistAndNormalize(t *testing.T) {
	st := testStore(t)
	saved, err := st.UpsertHost(Host{
		Name:     "gpu-box",
		Address:  "10.0.0.9",
		Port:     22,
		Username: "ubuntu",
		AuthType: "password",
		Password: "x",
		Color:    "#6ea8ff",
		Tags:     []string{" prod ", "#GPU", "prod", "gpu", "", strings.Repeat("z", 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tags) != 3 || saved.Tags[0] != "prod" || saved.Tags[1] != "GPU" {
		t.Fatalf("normalized tags = %#v", saved.Tags)
	}
	if len([]rune(saved.Tags[2])) != 32 {
		t.Fatalf("long tag should truncate, got %q", saved.Tags[2])
	}

	got, err := st.GetHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Tags, ",") != strings.Join(saved.Tags, ",") {
		t.Fatalf("get tags = %#v", got.Tags)
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
	if strings.Join(view.Tags, ",") != strings.Join(saved.Tags, ",") {
		t.Fatalf("list tags = %#v", view.Tags)
	}

	raw, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"tags"`) || !strings.Contains(string(raw), `"prod"`) {
		t.Fatalf("keel.json missing tags: %s", raw)
	}

	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st2.GetHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(again.Tags, ",") != strings.Join(saved.Tags, ",") {
		t.Fatalf("reloaded tags = %#v", again.Tags)
	}

	cleared, err := st2.UpsertHost(Host{
		ID: saved.ID, Name: saved.Name, Address: saved.Address, Port: saved.Port,
		Username: saved.Username, AuthType: saved.AuthType, Color: saved.Color,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Tags) != 0 {
		t.Fatalf("empty tags should clear, got %#v", cleared.Tags)
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags([]string{" #prod ", "PROD", "staging", "  "})
	if len(got) != 2 || got[0] != "prod" || got[1] != "staging" {
		t.Fatalf("got %#v", got)
	}
	if NormalizeTags(nil) != nil || NormalizeTags([]string{"", "#"}) != nil {
		t.Fatal("empty input should be nil")
	}
	long := NormalizeTags([]string{strings.Repeat("a", 8), strings.Repeat("b", 8), strings.Repeat("c", 8), strings.Repeat("d", 8), strings.Repeat("e", 8), strings.Repeat("f", 8), strings.Repeat("g", 8), strings.Repeat("h", 8), "overflow"})
	if len(long) != 8 {
		t.Fatalf("cap = %d", len(long))
	}
}

func TestKnownHostsRequireTrust(t *testing.T) {
	st := testStore(t)
	err := st.CheckHostKey("example:22", "SHA256:abc")
	var unk *UnknownHostError
	if !errors.As(err, &unk) {
		t.Fatalf("first key should be unknown, got %v", err)
	}
	if unk.Host != "example:22" || unk.Fingerprint != "SHA256:abc" {
		t.Fatalf("unknown = %+v", unk)
	}
	if err := st.TrustHostKey("example:22", "SHA256:abc", false); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckHostKey("example:22", "SHA256:abc"); err != nil {
		t.Fatal(err)
	}
	err = st.CheckHostKey("example:22", "SHA256:other")
	var mm *HostKeyMismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("expected mismatch, got %v", err)
	}
	if err := st.TrustHostKey("example:22", "SHA256:other", false); err == nil {
		t.Fatal("replace required to overwrite")
	}
	if err := st.TrustHostKey("example:22", "SHA256:other", true); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckHostKey("example:22", "SHA256:other"); err != nil {
		t.Fatal(err)
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
	// Never touch the real "keel"/"vault" keychain item from a test.
	t.Setenv("KEEL_KEYRING", "off")
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

func TestHostKeepAliveAndReconnectDefaults(t *testing.T) {
	local := Host{AuthType: "local"}
	if local.KeepAlive() != 0 {
		t.Fatalf("local keepalive %s", local.KeepAlive())
	}
	if local.AutoReconnect() {
		t.Fatal("local should not auto-reconnect")
	}
	ssh := Host{AuthType: "password"}
	if ssh.KeepAlive() != 30*time.Second {
		t.Fatalf("ssh default keepalive %s", ssh.KeepAlive())
	}
	if !ssh.AutoReconnect() {
		t.Fatal("ssh should auto-reconnect")
	}
	off := Host{AuthType: "key", KeepAliveSec: -1, NoAutoReconnect: true}
	if off.KeepAlive() != 0 {
		t.Fatalf("disabled keepalive %s", off.KeepAlive())
	}
	if off.AutoReconnect() {
		t.Fatal("noAutoReconnect should stick")
	}
	custom := Host{AuthType: "identity", KeepAliveSec: 15}
	if custom.KeepAlive() != 15*time.Second {
		t.Fatalf("custom keepalive %s", custom.KeepAlive())
	}
}

func TestKeepAliveSecRoundTrip(t *testing.T) {
	st := testStore(t)
	saved, err := st.UpsertHost(Host{
		Name: "box", Address: "10.0.0.8", Username: "ubuntu", AuthType: "password",
		Password: "x", Color: "#6ea8ff", KeepAliveSec: -1, NoAutoReconnect: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.KeepAliveSec != -1 || !got.NoAutoReconnect {
		t.Fatalf("got keepalive=%d noReconnect=%v", got.KeepAliveSec, got.NoAutoReconnect)
	}
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st2.GetHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.KeepAliveSec != -1 || !again.NoAutoReconnect {
		t.Fatalf("reload keepalive=%d noReconnect=%v", again.KeepAliveSec, again.NoAutoReconnect)
	}
}

func TestDuplicateHost(t *testing.T) {
	st := testStore(t)
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	var local Host
	for _, h := range hosts {
		if h.AuthType == "local" {
			local = h
		}
	}
	if local.ID == "" {
		t.Fatal("expected seeded local host")
	}
	if _, err := st.DuplicateHost(local.ID); err == nil {
		t.Fatal("local shell should not duplicate")
	}

	saved, err := st.UpsertHost(Host{
		Name:         "api",
		Group:        "prod",
		Address:      "10.0.0.8",
		Port:         2222,
		Username:     "ubuntu",
		AuthType:     "password",
		Password:     "secret",
		Color:        "#6ea8ff",
		KeepAliveSec: 45,
	})
	if err != nil {
		t.Fatal(err)
	}
	copy1, err := st.DuplicateHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if copy1.ID == "" || copy1.ID == saved.ID {
		t.Fatalf("copy should be a new host, got %q", copy1.ID)
	}
	if copy1.Name != "api (copy)" {
		t.Fatalf("name = %q", copy1.Name)
	}
	if copy1.Address != "10.0.0.8" || copy1.Port != 2222 || copy1.Username != "ubuntu" || copy1.Group != "prod" {
		t.Fatalf("settings not copied: %+v", copy1)
	}
	if copy1.Password != "secret" {
		t.Fatal("duplicate should copy the saved secret")
	}
	if copy1.KeepAliveSec != 45 || copy1.Color != "#6ea8ff" {
		t.Fatalf("hygiene fields not copied: %+v", copy1)
	}

	copy2, err := st.DuplicateHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if copy2.Name != "api (copy 2)" {
		t.Fatalf("second copy name = %q", copy2.Name)
	}

	if _, err := st.DuplicateHost("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestNotebookRoundTripAndPin(t *testing.T) {
	st := testStore(t)
	saved, err := st.UpsertNotebook(Notebook{
		Name:  "  ",
		Cells: []NotebookCell{{Body: "# hello\nls"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || saved.Name != "Notebook" {
		t.Fatalf("normalized = %+v", saved)
	}
	if len(saved.Cells) != 1 || saved.Cells[0].ID == "" || saved.Cells[0].Body != "# hello\nls" {
		t.Fatalf("cells = %+v", saved.Cells)
	}

	saved.Name = "ops"
	saved.Pinned = true
	saved.Cells = append(saved.Cells, NotebookCell{Body: "uptime"})
	updated, err := st.UpsertNotebook(saved)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "ops" || !updated.Pinned || len(updated.Cells) != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.CreatedAt != saved.CreatedAt {
		t.Fatal("createdAt should stick")
	}

	listed, err := st.ListNotebooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != updated.ID || !listed[0].Pinned {
		t.Fatalf("list = %+v", listed)
	}

	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st2.GetNotebook(updated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "ops" || again.Cells[0].Body != "# hello\nls" || !again.Pinned {
		t.Fatalf("reloaded = %+v", again)
	}

	if err := st2.DeleteNotebook(updated.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.GetNotebook(updated.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted err = %v", err)
	}
}

func TestNotebookStoresNoOutputs(t *testing.T) {
	st := testStore(t)
	var nb Notebook
	if err := json.Unmarshal([]byte(`{"name":"ops","cells":[{"id":"c1","body":"ls","output":"PTY_OUTPUT_LEAK","stdout":"PTY_OUTPUT_LEAK"}]}`), &nb); err != nil {
		t.Fatal(err)
	}
	saved, err := st.UpsertNotebook(nb)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PTY_OUTPUT_LEAK") {
		t.Fatalf("persisted outputs: %s", raw)
	}
	onDisk, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), "PTY_OUTPUT_LEAK") {
		t.Fatal("keel.json must not store notebook cell outputs")
	}
}

func TestSetupRefusesExistingVault(t *testing.T) {
	st := testStore(t)
	saved, err := st.UpsertHost(Host{
		Name: "box", Address: "10.0.0.1", Port: 22, Username: "root",
		AuthType: "password", Password: "keep-me",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Setup("other-phrase"); !errors.Is(err, ErrVaultExists) {
		t.Fatalf("setup on unlocked vault = %v, want ErrVaultExists", err)
	}

	vault := st.vaultPath
	before, err := os.ReadFile(vault)
	if err != nil {
		t.Fatal(err)
	}

	// Reopen without the passphrase: the existing scrypt vault is locked but
	// does not need setup. Setup must not overwrite it.
	_ = os.Unsetenv("KEEL_VAULT_PASSPHRASE")
	// Never touch the real "keel"/"vault" keychain item from a test.
	t.Setenv("KEEL_KEYRING", "off")
	reopened, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Locked() || reopened.NeedsSetup() {
		t.Fatalf("locked=%v needsSetup=%v, want locked existing vault", reopened.Locked(), reopened.NeedsSetup())
	}
	if err := reopened.Setup("attacker-phrase"); !errors.Is(err, ErrVaultExists) {
		t.Fatalf("setup on locked vault = %v, want ErrVaultExists", err)
	}
	after, err := os.ReadFile(vault)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Setup rewrote the existing vault")
	}
	if err := reopened.Unlock("test-vault-pass"); err != nil {
		t.Fatalf("unlock with original passphrase: %v", err)
	}
	got, err := reopened.GetHost(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "keep-me" {
		t.Fatalf("password = %q after refused setup", got.Password)
	}
}
