package httpapi

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
Host via-nouser
  HostName 10.0.4.26
  ProxyJump gate.example.net:2222
Host via-wronguser
  HostName 10.0.4.27
  ProxyJump admin@gate.example.net:2222
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

type importOut struct {
	Hosts  []store.Host `json:"hosts"`
	Failed []struct {
		Alias string `json:"alias"`
		Error string `json:"error"`
	} `json:"failed"`
}

func TestSSHImportMapsProxyJump(t *testing.T) {
	f := jumpImportFixture(t, jumpConfig)
	gate, err := f.st.UpsertHost(store.Host{Name: "gate", Address: "gate.example.net", Port: 2222, Username: "gw", AuthType: "password", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Same address and port, another user: never the jump for gw@.
	if _, err := f.st.UpsertHost(store.Host{Name: "gate-admin", Address: "gate.example.net", Port: 2222, Username: "root", AuthType: "password", Password: "y"}); err != nil {
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
	if plan["db"].JumpAlias != "bastion" || plan["db"].JumpVia != "ops@10.0.0.1:22" {
		t.Fatalf("db = %+v", plan["db"])
	}
	if plan["via-saved"].JumpSaved != "gate" || plan["via-saved"].JumpVia != "gw@gate.example.net:2222" {
		t.Fatalf("via-saved = %+v", plan["via-saved"])
	}
	// No user in the ProxyJump: the local user (tester), which no saved host has.
	if plan["via-nouser"].JumpSaved != "" || !strings.Contains(plan["via-nouser"].JumpProblem, "tester@gate.example.net:2222") {
		t.Fatalf("via-nouser = %+v", plan["via-nouser"])
	}
	if plan["via-wronguser"].JumpSaved != "" || plan["via-wronguser"].JumpProblem == "" {
		t.Fatalf("via-wronguser = %+v", plan["via-wronguser"])
	}
	for _, a := range []string{"via-nothing", "two-hops", "via-proxied"} {
		if plan[a].JumpAlias != "" || plan[a].JumpSaved != "" || plan[a].JumpProblem == "" || plan[a].Proxy != "ProxyJump" {
			t.Fatalf("%s = %+v", a, plan[a])
		}
	}

	all := []string{"db", "bastion", "via-saved", "via-nouser", "via-wronguser", "via-nothing", "two-hops", "via-proxied"}
	// Unlinkable ProxyJump hosts are refused unless ticked Without the jump,
	// and nothing is saved.
	var bad map[string]any
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": all}, &bad); code != http.StatusBadRequest || bad["errorKind"] != "jump" {
		t.Fatalf("import = %d %v", code, bad)
	}
	for _, a := range []string{"via-nouser", "via-wronguser", "via-nothing", "two-hops", "via-proxied"} {
		if !strings.Contains(bad["error"].(string), a) {
			t.Fatalf("%s not named: %v", a, bad["error"])
		}
	}
	if hosts, _ := f.st.ListHosts(); len(hosts) != 3 {
		t.Fatalf("a refused import saved hosts: %d", len(hosts))
	}

	var out importOut
	body := map[string]any{"hosts": all, "withoutJump": []string{"via-nouser", "via-wronguser", "via-nothing", "two-hops", "via-proxied"}}
	if code := f.post(t, "/api/ssh-config/import", body, &out); code != http.StatusOK {
		t.Fatalf("import %d", code)
	}
	byName := map[string]store.Host{}
	for _, h := range out.Hosts {
		byName[h.Name] = h
	}
	if len(byName) != len(all) || len(out.Failed) != 0 {
		t.Fatalf("hosts = %d failed = %+v", len(byName), out.Failed)
	}
	if byName["db"].JumpHost == "" || byName["db"].JumpHost != byName["bastion"].ID {
		t.Fatalf("db jump = %q, bastion = %q", byName["db"].JumpHost, byName["bastion"].ID)
	}
	if byName["via-saved"].JumpHost != gate.ID {
		t.Fatalf("via-saved jump = %q", byName["via-saved"].JumpHost)
	}
	for _, a := range []string{"bastion", "via-nouser", "via-wronguser", "via-nothing", "two-hops", "via-proxied"} {
		if byName[a].JumpHost != "" {
			t.Fatalf("%s got jump %q", a, byName[a].JumpHost)
		}
	}
}

func TestSSHImportProxyJumpNeedsItsJumpOrAnExplicitTick(t *testing.T) {
	f := jumpImportFixture(t, jumpConfig)
	var bad map[string]any
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": []string{"db"}}, &bad); code != http.StatusBadRequest || bad["errorKind"] != "jump" {
		t.Fatalf("db alone = %d %v", code, bad)
	}
	var out importOut
	if code := f.post(t, "/api/ssh-config/import", map[string]any{"hosts": []string{"db"}, "withoutJump": []string{"db"}}, &out); code != http.StatusOK {
		t.Fatalf("import %d", code)
	}
	if len(out.Hosts) != 1 || out.Hosts[0].JumpHost != "" {
		t.Fatalf("hosts = %+v", out.Hosts)
	}
}

func TestSSHImportAmbiguousSavedJumpIsNotLinked(t *testing.T) {
	f := jumpImportFixture(t, jumpConfig)
	for _, n := range []string{"gate-1", "gate-2"} {
		if _, err := f.st.UpsertHost(store.Host{Name: n, Address: "gate.example.net", Port: 2222, Username: "gw", AuthType: "password", Password: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	var p importPreview
	f.get(t, "/api/ssh-config/preview", &p)
	for _, h := range p.Hosts {
		if h.Alias == "via-saved" && (h.JumpSaved != "" || !strings.Contains(h.JumpProblem, "several saved hosts")) {
			t.Fatalf("via-saved = %+v", h)
		}
	}
}
