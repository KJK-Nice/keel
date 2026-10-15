package httpapi

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"keel/internal/runtime"
	"keel/internal/store"
)

func jumpImportFixture(t *testing.T, cfg string) importFixture {
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
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(root, "data", "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, runtime.New(st, nil), fs.FS(web), nil).WithSSHDir(sshDir).Handler())
	t.Cleanup(srv.Close)
	return importFixture{srv: srv, st: st, sshDir: sshDir}
}

const jumpConfig = `Host bastion
  HostName 10.0.0.1
  User ops
Host db
  HostName 10.0.4.21
  User deploy
  ProxyJump bastion
Host via-saved
  HostName 10.0.4.22
  ProxyJump gw@gate.example.net:2222
Host via-nothing
  HostName 10.0.4.23
  ProxyJump nowhere.example.net
Host two-hops
  HostName 10.0.4.24
  ProxyJump bastion,db
Host via-proxied
  HostName 10.0.4.25
  ProxyJump db
`

func TestSSHImportMapsProxyJump(t *testing.T) {
	f := jumpImportFixture(t, jumpConfig)
	gate, err := f.st.UpsertHost(store.Host{Name: "gate", Address: "gate.example.net", Port: 2222, Username: "gw", AuthType: "password", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var p importPreview
	if code := f.get(t, "/api/ssh-config/preview", &p); code != http.StatusOK {
		t.Fatalf("preview %d", code)
	}
	plan := map[string]importHost{}
	for _, h := range p.Hosts {
		plan[h.Alias] = h
	}
	if plan["db"].JumpAlias != "bastion" || plan["via-saved"].JumpSaved != "gate" {
		t.Fatalf("plans: db=%+v via-saved=%+v", plan["db"], plan["via-saved"])
	}
	for _, a := range []string{"via-nothing", "two-hops", "via-proxied"} {
		if plan[a].JumpAlias != "" || plan[a].JumpSaved != "" || plan[a].Proxy != "ProxyJump" {
			t.Fatalf("%s = %+v", a, plan[a])
		}
	}

	var out struct {
		Hosts []store.Host `json:"hosts"`
	}
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": []string{"db", "bastion", "via-saved", "via-nothing", "two-hops", "via-proxied"}}, &out); code != http.StatusOK {
		t.Fatalf("import %d", code)
	}
	byName := map[string]store.Host{}
	for _, h := range out.Hosts {
		byName[h.Name] = h
	}
	if byName["db"].JumpHost == "" || byName["db"].JumpHost != byName["bastion"].ID {
		t.Fatalf("db jump = %q, bastion = %q", byName["db"].JumpHost, byName["bastion"].ID)
	}
	if byName["via-saved"].JumpHost != gate.ID {
		t.Fatalf("via-saved jump = %q", byName["via-saved"].JumpHost)
	}
	for _, a := range []string{"bastion", "via-nothing", "two-hops", "via-proxied"} {
		if byName[a].JumpHost != "" {
			t.Fatalf("%s got jump %q", a, byName[a].JumpHost)
		}
	}
}

func TestSSHImportKeepsFlagWhenJumpNotImported(t *testing.T) {
	f := jumpImportFixture(t, jumpConfig)
	var out struct {
		Hosts []store.Host `json:"hosts"`
	}
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": []string{"db"}}, &out); code != http.StatusOK {
		t.Fatalf("import %d", code)
	}
	if len(out.Hosts) != 1 || out.Hosts[0].JumpHost != "" {
		t.Fatalf("hosts = %+v", out.Hosts)
	}
}
