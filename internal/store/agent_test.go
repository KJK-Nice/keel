package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func agentStore(t *testing.T) (*Store, string) {
	t.Helper()
	t.Setenv("KEEL_KEYRING", "off")
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	path := filepath.Join(t.TempDir(), "keel.json")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	return st, path
}

func reopen(t *testing.T, path string) *Store {
	t.Helper()
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func hostByID(t *testing.T, st *Store, id string) Host {
	t.Helper()
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hosts {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("host %s not listed", id)
	return Host{}
}

func TestAgentFlagsDefaultsAndReadsAlwaysSet(t *testing.T) {
	st, _ := agentStore(t)
	h, err := st.UpsertHost(Host{Name: "api", Address: "10.0.0.8", Username: "u", AuthType: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if h.AgentReadable == nil || !*h.AgentReadable || h.Production == nil || *h.Production {
		t.Fatalf("new host flags = %v/%v, want readable, not production", h.AgentReadable, h.Production)
	}
	got := hostByID(t, st, h.ID)
	if !got.ReadableByAgent() || got.IsProduction() {
		t.Fatalf("listed flags wrong: %+v", got)
	}
	b, _ := json.Marshal(got)
	if !bytes.Contains(b, []byte(`"agentReadable":true`)) || !bytes.Contains(b, []byte(`"production":false`)) {
		t.Fatalf("API shape must always carry both flags: %s", b)
	}
}

func TestAgentFlagsUpdateKeepsWhenNotSent(t *testing.T) {
	st, path := agentStore(t)
	h, err := st.UpsertHost(Host{Name: "db", Address: "db", Username: "u", AuthType: "password",
		AgentReadable: boolPtr(false), Production: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	// An older client sends the host without the two fields.
	upd := Host{ID: h.ID, Name: "db-1", Address: "db", Username: "u", AuthType: "password"}
	if _, err := st.UpsertHost(upd); err != nil {
		t.Fatal(err)
	}
	got := hostByID(t, st, h.ID)
	if got.ReadableByAgent() || !got.IsProduction() || got.Name != "db-1" {
		t.Fatalf("update without flags changed them: %+v", got)
	}
	// Explicit values win.
	upd.AgentReadable, upd.Production = boolPtr(true), boolPtr(false)
	if _, err := st.UpsertHost(upd); err != nil {
		t.Fatal(err)
	}
	st2 := reopen(t, path)
	got = hostByID(t, st2, h.ID)
	if !got.ReadableByAgent() || got.IsProduction() {
		t.Fatalf("explicit flags not saved: %+v", got)
	}
}

func TestAgentFlagsMigrationOfOldHosts(t *testing.T) {
	st, path := agentStore(t)
	g, err := st.UpsertGroup(Group{Name: "Production"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := st.UpsertHost(Host{Name: "old", Address: "old", Username: "u", AuthType: "password", GroupID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite keel.json as a build without the fields would have: no
	// agentOff, no production keys on any host.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, rh := range raw["hosts"].([]any) {
		m := rh.(map[string]any)
		delete(m, "agentOff")
		delete(m, "production")
	}
	out, _ := json.Marshal(raw)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	st2 := reopen(t, path)
	got := hostByID(t, st2, h.ID)
	// Loading never turns production on, even in a production group.
	if !got.ReadableByAgent() || got.IsProduction() {
		t.Fatalf("old host must load readable and not production: %+v", got)
	}
	// Saving it unchanged (an older client) keeps it so.
	got.AgentReadable, got.Production = nil, nil
	if _, err := st2.UpsertHost(got); err != nil {
		t.Fatal(err)
	}
	got = hostByID(t, st2, h.ID)
	if got.IsProduction() {
		t.Fatal("saving an unmoved host must not turn production on")
	}
}

func TestProductionAutoOnCreateInGroup(t *testing.T) {
	st, _ := agentStore(t)
	g, err := st.UpsertGroup(Group{Name: " PRODUCTION "})
	if err != nil {
		t.Fatal(err)
	}
	h, err := st.UpsertHost(Host{Name: "a", Address: "a", Username: "u", AuthType: "password", GroupID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !h.IsProduction() {
		t.Fatal("host created in production group must be production")
	}
	// By group name (resolveGroupRef matches the existing group).
	h2, err := st.UpsertHost(Host{Name: "b", Address: "b", Username: "u", AuthType: "password", Group: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if !h2.IsProduction() {
		t.Fatal("host created by group name production must be production")
	}
	// The user can create it with the switch off.
	h3, err := st.UpsertHost(Host{Name: "c", Address: "c", Username: "u", AuthType: "password", GroupID: g.ID, Production: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if h3.IsProduction() {
		t.Fatal("explicit production=false on create must win")
	}
	// Only the direct parent counts.
	child, err := st.UpsertGroup(Group{Name: "db", ParentID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	h4, err := st.UpsertHost(Host{Name: "d", Address: "d", Username: "u", AuthType: "password", GroupID: child.ID})
	if err != nil {
		t.Fatal(err)
	}
	if h4.IsProduction() {
		t.Fatal("a host in production/db is not auto production (direct parent only)")
	}
	// Other names do not count.
	h5, err := st.UpsertHost(Host{Name: "e", Address: "e", Username: "u", AuthType: "password", Group: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if h5.IsProduction() {
		t.Fatal("group prod is not production")
	}
}

func TestProductionAutoOnMove(t *testing.T) {
	st, _ := agentStore(t)
	prod, _ := st.UpsertGroup(Group{Name: "production"})
	other, _ := st.UpsertGroup(Group{Name: "staging"})
	h, err := st.UpsertHost(Host{Name: "a", Address: "a", Username: "u", AuthType: "password", GroupID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if h.IsProduction() {
		t.Fatal("staging host is not production")
	}
	if err := st.AssignHostsGroup([]string{h.ID}, prod.ID); err != nil {
		t.Fatal(err)
	}
	if !hostByID(t, st, h.ID).IsProduction() {
		t.Fatal("move into production must turn production on")
	}
	// The user turns it off; it stays off while the host stays there.
	cur := hostByID(t, st, h.ID)
	cur.Production = boolPtr(false)
	if _, err := st.UpsertHost(cur); err != nil {
		t.Fatal(err)
	}
	cur = hostByID(t, st, h.ID)
	cur.Production = nil
	if _, err := st.UpsertHost(cur); err != nil {
		t.Fatal(err)
	}
	if hostByID(t, st, h.ID).IsProduction() {
		t.Fatal("saving without a move must not turn production back on")
	}
	if err := st.AssignHostsGroup([]string{h.ID}, prod.ID); err != nil {
		t.Fatal(err)
	}
	if hostByID(t, st, h.ID).IsProduction() {
		t.Fatal("re-assigning to the same group is not a move")
	}
	// Moving out never turns it off.
	if err := st.AssignHostsGroup([]string{h.ID}, other.ID); err != nil {
		t.Fatal(err)
	}
	cur = hostByID(t, st, h.ID)
	cur.Production = boolPtr(true)
	if _, err := st.UpsertHost(cur); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignHostsGroup([]string{h.ID}, ""); err != nil {
		t.Fatal(err)
	}
	if !hostByID(t, st, h.ID).IsProduction() {
		t.Fatal("moving out of production must not turn production off")
	}
	// An edit that changes the group into production also counts.
	h2, _ := st.UpsertHost(Host{Name: "b", Address: "b", Username: "u", AuthType: "password", GroupID: other.ID})
	h2.Production = nil
	h2.GroupID = prod.ID
	h2.Group = ""
	if _, err := st.UpsertHost(h2); err != nil {
		t.Fatal(err)
	}
	if !hostByID(t, st, h2.ID).IsProduction() {
		t.Fatal("edit moving host into production must turn production on")
	}
	// Renaming a group to production changes nothing.
	h3, _ := st.UpsertHost(Host{Name: "c", Address: "c", Username: "u", AuthType: "password", GroupID: other.ID})
	if _, err := st.UpsertGroup(Group{ID: other.ID, Name: "Production"}); err != nil {
		t.Fatal(err)
	}
	if hostByID(t, st, h3.ID).IsProduction() {
		t.Fatal("renaming a group must not change production")
	}
}

func TestDeleteGroupMovingHostsIntoProduction(t *testing.T) {
	st, _ := agentStore(t)
	prod, _ := st.UpsertGroup(Group{Name: "Production"})
	child, _ := st.UpsertGroup(Group{Name: "db", ParentID: prod.ID})
	other, _ := st.UpsertGroup(Group{Name: "staging"})
	inner, _ := st.UpsertGroup(Group{Name: "web", ParentID: other.ID})
	h, _ := st.UpsertHost(Host{Name: "a", Address: "a", Username: "u", AuthType: "password", GroupID: child.ID})
	h2, _ := st.UpsertHost(Host{Name: "b", Address: "b", Username: "u", AuthType: "password", GroupID: inner.ID})
	if h.IsProduction() {
		t.Fatal("production/db host is not production (direct parent only)")
	}
	if err := st.DeleteGroup(child.ID); err != nil {
		t.Fatal(err)
	}
	if !hostByID(t, st, h.ID).IsProduction() {
		t.Fatal("deleting a group moves its hosts into production: auto-on must fire")
	}
	if err := st.DeleteGroup(inner.ID); err != nil {
		t.Fatal(err)
	}
	if hostByID(t, st, h2.ID).IsProduction() {
		t.Fatal("moving up into staging must not turn production on")
	}
}

func TestDuplicateKeepsAgentFlags(t *testing.T) {
	st, _ := agentStore(t)
	h, _ := st.UpsertHost(Host{Name: "a", Address: "a", Username: "u", AuthType: "password",
		AgentReadable: boolPtr(false), Production: boolPtr(true)})
	d, err := st.DuplicateHost(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.ReadableByAgent() || !d.IsProduction() {
		t.Fatalf("duplicate lost flags: %+v", d)
	}
}

func TestAgentSettingsDefaultsNormalizeAndPersist(t *testing.T) {
	st, path := agentStore(t)
	got, err := st.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultAgentSettings() {
		t.Fatalf("defaults = %+v", got)
	}
	saved, err := st.SetAgentSettings(AgentSettings{
		Provider: "Gemini", Model: "gemini-3.6-flash", Detection: DetectionHashOnly,
		DefaultScope: ScopeTab, ScrollbackLines: 5000,
		Redact:      AgentRedaction{Secrets: false, IPs: true, Hostnames: true},
		HistoryDays: 9999,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Provider != "gemini" || saved.ScrollbackLines != MaxScrollbackLines || !saved.Redact.Secrets || saved.HistoryDays != MaxHistoryDays {
		t.Fatalf("normalize = %+v", saved)
	}
	st2 := reopen(t, path)
	again, err := st2.AgentSettings()
	if err != nil || again != saved {
		t.Fatalf("reload = %+v, %v", again, err)
	}
	for _, n := range []struct{ in, want int }{{1, MinScrollbackLines}, {0, DefaultScrollbackLines}, {-3, MinScrollbackLines}, {333, 333}} {
		out, err := AgentSettings{ScrollbackLines: n.in}.Normalize()
		if err != nil || out.ScrollbackLines != n.want {
			t.Fatalf("lines %d → %d (%v), want %d", n.in, out.ScrollbackLines, err, n.want)
		}
	}
	for _, bad := range []AgentSettings{
		{Detection: "sometimes"}, {DefaultScope: "all"}, {Provider: "Bad Provider!"}, {Model: "../x"},
	} {
		if _, err := st.SetAgentSettings(bad); !errors.Is(err, ErrAgentSettings) {
			t.Fatalf("%+v accepted (%v)", bad, err)
		}
	}
}

func TestAgentKeysLiveInVaultOnly(t *testing.T) {
	st, path := agentStore(t)
	const gem = "AIzaSyTESTONLY-0123456789abcdefghijklmnop"
	const jev = "jev_live_TESTONLY_9f8e7d6c5b4a"
	if ok, _ := st.AgentKeyConfigured("gemini"); ok {
		t.Fatal("no key yet")
	}
	if err := st.SetAgentKey("gemini", "  "+gem+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentKey("jev", jev); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "has space", "x\x00y", strings.Repeat("k", 2000)} {
		if err := st.SetAgentKey("gemini", bad); !errors.Is(err, ErrAgentKey) {
			t.Fatalf("key %q accepted", bad)
		}
	}
	if err := st.SetAgentKey("../vault", "abc"); !errors.Is(err, ErrAgentKey) {
		t.Fatal("bad provider id accepted")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(gem)) || bytes.Contains(b, []byte(jev)) {
		t.Fatal("agent key written to keel.json")
	}
	vb, err := os.ReadFile(filepath.Join(filepath.Dir(path), "keel.vault"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(vb, []byte(gem)) {
		t.Fatal("keel.vault must be encrypted")
	}
	st2 := reopen(t, path)
	if ok, _ := st2.AgentKeyConfigured("gemini"); !ok {
		t.Fatal("key lost on reopen")
	}
	if k, err := st2.AgentKey("gemini"); err != nil || k != gem {
		t.Fatalf("AgentKey = %q, %v", k, err)
	}
	known, err := st2.KnownSecretValues()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(known, gem) || !contains(known, jev) {
		t.Fatal("agent keys missing from the redaction list")
	}
	if err := st2.ClearAgentKey("gemini"); err != nil {
		t.Fatal(err)
	}
	if err := st2.ClearAgentKey("gemini"); err != nil {
		t.Fatal("clearing twice must be fine")
	}
	if _, err := st2.AgentKey("gemini"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cleared key still readable")
	}
	if ok, _ := st2.AgentKeyConfigured("jev"); !ok {
		t.Fatal("clearing gemini cleared jev")
	}
}

func TestKnownSecretValuesCoversHostsAndIdentities(t *testing.T) {
	st, _ := agentStore(t)
	if _, err := st.UpsertHost(Host{Name: "a", Address: "a", Username: "u", AuthType: "password", Password: "hostpass-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertIdentity(Identity{Name: "k", Kind: "key", PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----", Passphrase: "kp-2"}); err != nil {
		t.Fatal(err)
	}
	known, err := st.KnownSecretValues()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hostpass-1", "kp-2"} {
		if !contains(known, want) {
			t.Fatalf("%q missing from %v", want, known)
		}
	}
}

func TestAgentAccessorsLocked(t *testing.T) {
	st, _ := agentStore(t)
	st.mu.Lock()
	st.locked = true
	st.mu.Unlock()
	if _, err := st.AgentSettings(); !errors.Is(err, ErrLocked) {
		t.Fatal("settings readable while locked")
	}
	if err := st.SetAgentKey("gemini", "k"); !errors.Is(err, ErrLocked) {
		t.Fatal("key settable while locked")
	}
	if _, err := st.KnownSecretValues(); !errors.Is(err, ErrLocked) {
		t.Fatal("secrets listable while locked")
	}
	if _, err := st.AgentKey("gemini"); !errors.Is(err, ErrLocked) {
		t.Fatal("key readable while locked")
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Google closed gemini-2.5-flash to new keys, so settings saved with the
// old default read back as the new one.
func TestAgentSettingsRetiredDefaultModel(t *testing.T) {
	st, path := agentStore(t)
	old := DefaultAgentSettings()
	old.Model = retiredDefaultModel
	if _, err := st.SetAgentSettings(old); err != nil {
		t.Fatal(err)
	}
	got, err := reopen(t, path).AgentSettings()
	if err != nil || got.Model != DefaultAgentModel {
		t.Fatalf("model = %q (%v), want %q", got.Model, err, DefaultAgentModel)
	}
}
