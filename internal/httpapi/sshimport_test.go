package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/ssh"

	"keel/internal/runtime"
	"keel/internal/store"
)

type fileSnap struct {
	data []byte
	mode os.FileMode
	mod  time.Time
}

func snap(t *testing.T, path string) fileSnap {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileSnap{b, fi.Mode(), fi.ModTime()}
}

func writeKey(t *testing.T, path, pass string, mode os.FileMode) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var blk *pem.Block
	if pass == "" {
		blk, err = ssh.MarshalPrivateKey(priv, "fixture")
	} else {
		blk, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "fixture", []byte(pass))
	}
	if err != nil {
		t.Fatal(err)
	}
	b := pem.EncodeToMemory(blk)
	if err := os.WriteFile(path, b, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type importFixture struct {
	srv     *httptest.Server
	st      *store.Store
	dataDir string
	sshDir  string
	plain   string
	locked  string
	other   string
	pems    []string
}

func newImportFixture(t *testing.T) importFixture {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	t.Setenv("KEEL_KEYRING", "off")
	t.Setenv("USER", "tester")
	root := t.TempDir()
	sshDir := filepath.Join(root, "home", ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := importFixture{dataDir: filepath.Join(root, "data"), sshDir: sshDir}
	f.plain = filepath.Join(sshDir, "id_plain")
	f.locked = filepath.Join(sshDir, "id_locked")
	f.other = filepath.Join(sshDir, "id_other")
	f.pems = append(f.pems, writeKey(t, f.plain, "", 0o600))
	f.pems = append(f.pems, writeKey(t, f.locked, "key-pass", 0o400))
	f.pems = append(f.pems, writeKey(t, f.other, "", 0o640))
	cfg := `Host web db
  HostName %h.example.net
  User deploy
  IdentityFile ~/.ssh/id_plain
Host vault-box
  HostName 10.0.0.9
  Port 2222
  IdentityFile ~/.ssh/id_locked
Host jumpy
  ProxyJump bastion
  IdentityFile ~/.ssh/id_other
Host *.internal
  User wild
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(f.dataDir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.st = st
	rt := runtime.New(st, nil)
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	f.srv = httptest.NewServer(New(st, rt, fs.FS(web), nil).WithSSHDir(sshDir).Handler())
	t.Cleanup(f.srv.Close)
	return f
}

func (f importFixture) get(t *testing.T, path string, v any) int {
	t.Helper()
	res, err := http.Get(f.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if v != nil {
		if err := json.NewDecoder(res.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (f importFixture) post(t *testing.T, path string, body any, v any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(f.srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if v != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatalf("%s: %s", err, raw)
		}
	}
	return res.StatusCode
}

func TestSSHConfigPreview(t *testing.T) {
	f := newImportFixture(t)
	if _, err := f.st.UpsertHost(store.Host{Name: "already", Address: "db.example.net", Port: 22, Username: "deploy", AuthType: "password", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	var p importPreview
	if code := f.get(t, "/api/ssh-config/preview", &p); code != http.StatusOK {
		t.Fatalf("preview = %d", code)
	}
	if !p.Found || p.Path != "~/.ssh/config" {
		t.Fatalf("preview = %+v", p)
	}
	got := map[string]importHost{}
	for _, h := range p.Hosts {
		got[h.Alias] = h
	}
	if len(got) != 4 {
		t.Fatalf("hosts = %+v", p.Hosts)
	}
	if got["web"].HostName != "web.example.net" || got["web"].Saved {
		t.Fatalf("web = %+v", got["web"])
	}
	if !got["db"].Saved {
		t.Fatal("db matches a saved host (address, user, port) and must be marked")
	}
	if got["vault-box"].User != "tester" || got["vault-box"].Port != 2222 {
		t.Fatalf("vault-box = %+v", got["vault-box"])
	}
	if got["jumpy"].Proxy != "ProxyJump" {
		t.Fatalf("jumpy = %+v", got["jumpy"])
	}
	if len(p.Patterns) != 1 || p.Patterns[0].Pattern != "*.internal" {
		t.Fatalf("patterns = %+v", p.Patterns)
	}
	if len(p.Keys) != 3 {
		t.Fatalf("keys = %+v", p.Keys)
	}
	for _, k := range p.Keys {
		if (k.Name == "id_locked") != k.Encrypted || k.Problem != "" {
			t.Fatalf("key = %+v", k)
		}
	}
	raw, _ := json.Marshal(p)
	for _, pemText := range f.pems {
		if bytes.Contains(raw, []byte(strings.Split(pemText, "\n")[1])) {
			t.Fatal("the preview must not carry key material")
		}
	}
}

func TestSSHConfigImportCopiesOnlyChosenKeysAndLeavesOriginals(t *testing.T) {
	f := newImportFixture(t)
	files := []string{f.plain, f.locked, f.other, filepath.Join(f.sshDir, "config")}
	before := map[string]fileSnap{}
	for _, p := range files {
		before[p] = snap(t, p)
	}
	time.Sleep(20 * time.Millisecond)

	// A key with a passphrase and none given: nothing is saved.
	var errBody map[string]string
	code := f.post(t, "/api/ssh-config/import", map[string]any{
		"hosts": []string{"web", "vault-box"},
		"keys":  []map[string]string{{"path": f.locked}},
	}, &errBody)
	if code != http.StatusBadRequest || errBody["errorKind"] != "passphrase" || errBody["key"] != f.locked {
		t.Fatalf("missing passphrase = %d %+v", code, errBody)
	}
	code = f.post(t, "/api/ssh-config/import", map[string]any{
		"hosts": []string{"web"},
		"keys":  []map[string]string{{"path": f.locked, "passphrase": "nope"}},
	}, &errBody)
	if code != http.StatusBadRequest || errBody["errorKind"] != "wrongPassphrase" {
		t.Fatalf("wrong passphrase = %d %+v", code, errBody)
	}
	if hosts, _ := f.st.ListHosts(); len(hosts) != 1 {
		t.Fatalf("a failed import saved hosts: %d", len(hosts))
	}
	if ids, _ := f.st.ListIdentities(); len(ids) != 0 {
		t.Fatalf("a failed import saved identities: %d", len(ids))
	}

	// Copy id_plain and id_locked; id_other (used by jumpy) stays off.
	var out struct {
		Hosts      []store.Host     `json:"hosts"`
		Identities []store.Identity `json:"identities"`
	}
	code = f.post(t, "/api/ssh-config/import", map[string]any{
		"hosts": []string{"web", "vault-box", "jumpy", "web"},
		"keys": []map[string]string{
			{"path": f.plain},
			{"path": f.locked, "passphrase": "key-pass"},
		},
	}, &out)
	if code != http.StatusOK {
		t.Fatalf("import = %d", code)
	}
	if len(out.Hosts) != 3 || len(out.Identities) != 2 {
		t.Fatalf("import = %+v", out)
	}
	byName := map[string]store.Host{}
	for _, h := range out.Hosts {
		byName[h.Name] = h
		if h.PrivateKey != "" && h.PrivateKey != "__saved__" {
			t.Fatal("import response carries key material")
		}
	}
	if byName["web"].AuthType != "identity" || byName["vault-box"].AuthType != "identity" {
		t.Fatalf("hosts with a copied key use it: %+v", out.Hosts)
	}
	if byName["jumpy"].AuthType != "password" || byName["jumpy"].IdentityID != "" {
		t.Fatalf("jumpy's key was not copied: %+v", byName["jumpy"])
	}
	for _, id := range out.Identities {
		if id.PrivateKey != "__saved__" {
			t.Fatalf("identity response must be redacted: %+v", id)
		}
	}
	locked, err := f.st.GetIdentity(byName["vault-box"].IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Passphrase != "key-pass" || locked.PrivateKey != f.pems[1] || locked.Kind != "key" || locked.Name != "id_locked" {
		t.Fatalf("locked identity = %+v", locked.Name)
	}
	host, err := f.st.HostForConnect(byName["web"].ID)
	if err != nil || host.PrivateKey != f.pems[0] || host.AuthType != "key" {
		t.Fatalf("web connects with id_plain: %v %s", err, host.AuthType)
	}

	// keel.json never holds key material or passphrases.
	raw, err := os.ReadFile(filepath.Join(f.dataDir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"PRIVATE KEY", "key-pass", strings.Split(f.pems[0], "\n")[1], strings.Split(f.pems[1], "\n")[1]} {
		if bytes.Contains(raw, []byte(needle)) {
			t.Fatalf("keel.json contains %q", needle)
		}
	}
	vault, err := os.ReadFile(filepath.Join(f.dataDir, "keel.vault"))
	if err != nil || bytes.Contains(vault, []byte("PRIVATE KEY")) {
		t.Fatal("keel.vault must be encrypted")
	}

	// The originals: same bytes, same mode, same mtime.
	for p, b := range before {
		a := snap(t, p)
		if !bytes.Equal(a.data, b.data) || a.mode != b.mode || !a.mod.Equal(b.mod) {
			t.Fatalf("%s changed (mode %v -> %v)", p, b.mode, a.mode)
		}
	}
	entries, _ := os.ReadDir(f.sshDir)
	if len(entries) != 4 {
		t.Fatalf("~/.ssh gained or lost files: %d", len(entries))
	}

	// A second preview marks the imported hosts as saved.
	var p importPreview
	f.get(t, "/api/ssh-config/preview", &p)
	for _, h := range p.Hosts {
		if want := h.Alias != "db"; h.Saved != want {
			t.Fatalf("%s saved = %v", h.Alias, h.Saved)
		}
	}
}

func TestSSHConfigImportRefusesWhatTheConfigDoesNotName(t *testing.T) {
	f := newImportFixture(t)
	outside := filepath.Join(t.TempDir(), "stolen")
	writeKey(t, outside, "", 0o600)
	var body map[string]string
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": []string{"web"}, "keys": []map[string]string{{"path": outside}}}, &body); code != http.StatusBadRequest {
		t.Fatalf("key outside the config = %d", code)
	}
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": []string{"*.internal"}}, &body); code != http.StatusBadRequest {
		t.Fatalf("pattern import = %d", code)
	}
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": []string{"nope"}}, &body); code != http.StatusBadRequest {
		t.Fatalf("unknown alias = %d", code)
	}
	if hosts, _ := f.st.ListHosts(); len(hosts) != 1 {
		t.Fatalf("refused imports saved hosts: %d", len(hosts))
	}
}

func TestSSHConfigMissing(t *testing.T) {
	f := newImportFixture(t)
	if err := os.Remove(filepath.Join(f.sshDir, "config")); err != nil {
		t.Fatal(err)
	}
	var p importPreview
	if code := f.get(t, "/api/ssh-config/preview", &p); code != http.StatusOK || p.Found || len(p.Hosts) != 0 {
		t.Fatalf("missing = %d %+v", code, p)
	}
}

// KEEL_KEYRING=off without a passphrase: the vault needs setup, /api/vault
// says so while every other route is locked, and import is locked too.
func TestVaultStateAndFirstRun(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "")
	_ = os.Unsetenv("KEEL_VAULT_PASSPHRASE")
	t.Setenv("KEEL_VAULT_FAST", "1")
	t.Setenv("KEEL_KEYRING", "off")
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, nil)
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).WithSSHDir(t.TempDir()).Handler())
	t.Cleanup(srv.Close)
	f := importFixture{srv: srv, st: st}

	var vs map[string]any
	if code := f.get(t, "/api/vault", &vs); code != http.StatusOK {
		t.Fatalf("vault state = %d", code)
	}
	if vs["locked"] != true || vs["needsSetup"] != true || vs["firstRun"] != true || vs["keyringOff"] != true || vs["mode"] != "" {
		t.Fatalf("vault state = %+v", vs)
	}
	if code := f.get(t, "/api/ssh-config/preview", nil); code != http.StatusLocked {
		t.Fatalf("preview while locked = %d", code)
	}
	if code := f.post(t, "/api/first-run/done", nil, nil); code != http.StatusLocked {
		t.Fatalf("done while locked = %d", code)
	}
	if code := f.post(t, "/api/vault/setup", map[string]string{"passphrase": "first-run-pass"}, nil); code != http.StatusOK {
		t.Fatalf("setup = %d", code)
	}
	// Setup still refuses a second time (no re-key, no overwrite).
	if code := f.post(t, "/api/vault/setup", map[string]string{"passphrase": "other"}, nil); code != http.StatusConflict {
		t.Fatalf("second setup = %d", code)
	}
	f.get(t, "/api/vault", &vs)
	if vs["locked"] != false || vs["mode"] != "passphrase" || vs["firstRun"] != true {
		t.Fatalf("after setup = %+v", vs)
	}
	if code := f.post(t, "/api/first-run/done", nil, nil); code != http.StatusNoContent {
		t.Fatalf("done = %d", code)
	}
	f.get(t, "/api/vault", &vs)
	if vs["firstRun"] != false {
		t.Fatalf("after done = %+v", vs)
	}

	// Restart: the passphrase vault is locked, not first run, and unlocks.
	st2, err := store.New(filepath.Join(dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	vs2 := st2.VaultState()
	if !vs2.Locked || vs2.NeedsSetup || vs2.Mode != "passphrase" || vs2.FirstRun {
		t.Fatalf("restart = %+v", vs2)
	}
	if err := st2.Setup("again"); err != store.ErrVaultExists {
		t.Fatalf("setup while locked = %v", err)
	}
	if err := st2.Unlock("wrong"); err != store.ErrPassphrase {
		t.Fatalf("wrong unlock = %v", err)
	}
	if err := st2.Unlock("first-run-pass"); err != nil {
		t.Fatal(err)
	}
}
