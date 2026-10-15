package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"keel/internal/applog"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/session/sshtest"
	"keel/internal/store"
)

func hostCheckServer(t *testing.T) (*httptest.Server, *store.Store, string) {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "keel.json"))
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return newFakeBridge(), nil })
	logPath := filepath.Join(dir, "keel.log")
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).WithLogs(applog.New(logPath)).Handler())
	t.Cleanup(srv.Close)
	return srv, st, logPath
}

func doJSON(t *testing.T, method, url string, body any, out any) int {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func draftFor(s *sshtest.Server) map[string]any {
	return map[string]any{"name": "t", "address": s.Host, "port": s.Port, "username": s.User, "authType": "password", "password": s.Password}
}

func TestTestConnectionEndpoint(t *testing.T) {
	srv, st, logPath := hostCheckServer(t)
	jump := sshtest.New(t, sshtest.Options{User: "jumper", Password: "jump-secret"})
	target := sshtest.New(t, sshtest.Options{User: "deploy", Password: "typed-secret-xyz"})

	// Unknown key: reported with its fingerprint, never trusted.
	var res session.ProbeResult
	if code := doJSON(t, "POST", srv.URL+"/api/hosts/test", draftFor(target), &res); code != 200 {
		t.Fatalf("code %d", code)
	}
	if res.Kind != "unknown-host" || res.Fingerprint != target.Fingerprint() || res.KeyType != "ssh-ed25519" || res.Hostname != target.Addr {
		t.Fatalf("res = %+v", res)
	}
	if k, _ := st.KnownHost(target.Addr); k.Known {
		t.Fatal("Test connection trusted a key")
	}

	// The explicit Trust and save, then ok with latency.
	if code := doJSON(t, "POST", srv.URL+"/api/known-hosts", map[string]any{"hostname": res.Hostname, "fingerprint": res.Fingerprint, "keyType": res.KeyType}, nil); code != 200 {
		t.Fatalf("trust %d", code)
	}
	res = session.ProbeResult{}
	doJSON(t, "POST", srv.URL+"/api/hosts/test", draftFor(target), &res)
	if !res.OK || res.Kind != "ok" {
		t.Fatalf("res = %+v", res)
	}
	if target.Sessions() != 0 {
		t.Fatal("Test connection opened a session")
	}

	// Auth failed.
	bad := draftFor(target)
	bad["password"] = "nope"
	res = session.ProbeResult{}
	doJSON(t, "POST", srv.URL+"/api/hosts/test", bad, &res)
	if res.Kind != "auth" {
		t.Fatalf("res = %+v", res)
	}

	// Through a saved jump host whose secret is only in the vault, with a
	// saved target whose password is left blank in the dialog.
	if err := st.TrustHostKey(jump.Addr, jump.Fingerprint(), false); err != nil {
		t.Fatal(err)
	}
	bastion, err := st.UpsertHost(store.Host{Name: "bastion", Address: jump.Host, Port: jump.Port, Username: jump.User, AuthType: "password", Password: jump.Password})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := st.UpsertHost(store.Host{Name: "db", Address: "10.255.255.1", Port: 22, Username: target.User, AuthType: "password", Password: target.Password})
	if err != nil {
		t.Fatal(err)
	}
	via := draftFor(target)
	via["id"] = saved.ID
	via["password"] = "__saved__"
	via["jumpHost"] = bastion.ID
	res = session.ProbeResult{}
	doJSON(t, "POST", srv.URL+"/api/hosts/test", via, &res)
	if !res.OK || jump.Forwards() != 1 {
		t.Fatalf("res = %+v forwards=%d", res, jump.Forwards())
	}
	// Nothing was saved by the test.
	after, _ := st.GetHost(saved.ID)
	if after.JumpHost != "" || after.Address != "10.255.255.1" {
		t.Fatalf("test saved the draft: %+v", after)
	}

	// Refused.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	off := draftFor(target)
	off["port"], _ = strconv.Atoi(port)
	res = session.ProbeResult{}
	doJSON(t, "POST", srv.URL+"/api/hosts/test", off, &res)
	if res.Kind != "refused" || res.Message == "" {
		t.Fatalf("res = %+v", res)
	}

	// Bad requests.
	if code := doJSON(t, "POST", srv.URL+"/api/hosts/test", map[string]any{"authType": "local"}, nil); code != 400 {
		t.Fatalf("local %d", code)
	}
	self := draftFor(target)
	self["id"] = saved.ID
	self["jumpHost"] = saved.ID
	if code := doJSON(t, "POST", srv.URL+"/api/hosts/test", self, nil); code != 400 {
		t.Fatalf("self jump %d", code)
	}

	// The typed password is never logged or stored in keel.json.
	logs, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logs), "connection test t: ok") {
		t.Fatalf("log = %s", logs)
	}
	plain, _ := os.ReadFile(filepath.Join(filepath.Dir(logPath), "keel.json"))
	for _, secret := range []string{"typed-secret-xyz", "jump-secret", "nope"} {
		if strings.Contains(string(logs), secret) || strings.Contains(string(plain), secret) {
			t.Fatalf("%q leaked", secret)
		}
	}
}

func TestHostKeyReadAndForgetEndpoints(t *testing.T) {
	srv, st, logPath := hostCheckServer(t)
	h, err := st.UpsertHost(store.Host{Name: "box", Address: "10.0.0.7", Port: 2222, Username: "u", AuthType: "password", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var k store.KnownHostKey
	if code := doJSON(t, "GET", srv.URL+"/api/hosts/"+h.ID+"/host-key", nil, &k); code != 200 || k.Known || k.Hostname != "10.0.0.7:2222" {
		t.Fatalf("code %d key %+v", code, k)
	}
	if code := doJSON(t, "DELETE", srv.URL+"/api/hosts/"+h.ID+"/host-key", nil, nil); code != 404 {
		t.Fatalf("forget unknown: %d", code)
	}
	if err := st.TrustHostKeyType("10.0.0.7:2222", "SHA256:q3Vt9fKe", "ssh-ed25519", false); err != nil {
		t.Fatal(err)
	}
	k = store.KnownHostKey{}
	doJSON(t, "GET", srv.URL+"/api/hosts/"+h.ID+"/host-key", nil, &k)
	if !k.Known || k.Fingerprint != "SHA256:q3Vt9fKe" || k.KeyType != "ssh-ed25519" {
		t.Fatalf("key %+v", k)
	}
	if code := doJSON(t, "DELETE", srv.URL+"/api/hosts/"+h.ID+"/host-key", nil, nil); code != 200 {
		t.Fatalf("forget %d", code)
	}
	if k, _ := st.KnownHost("10.0.0.7:2222"); k.Known {
		t.Fatal("key still saved")
	}
	logs, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logs), "forgot host key for box (10.0.0.7:2222") {
		t.Fatalf("forget not logged: %s", logs)
	}
	if code := doJSON(t, "GET", srv.URL+"/api/hosts/nope/host-key", nil, nil); code != 404 {
		t.Fatalf("missing host %d", code)
	}
}
