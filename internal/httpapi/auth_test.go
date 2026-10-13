package httpapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"keel/internal/authtoken"
	"keel/internal/runtime"
	"keel/internal/session"
	"keel/internal/store"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func authServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	web := fstest.MapFS{
		"index.html": {Data: []byte("ok")},
		"app.js":     {Data: []byte("console.log(1)")},
	}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), nil).WithAuth(testToken).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// noRedirect keeps the client from following the token redirect, so the
// test can inspect it.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func get(t *testing.T, client *http.Client, rawURL string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestAuthRequiredOnPrivilegedRoutes(t *testing.T) {
	srv := authServer(t)
	c := srv.Client()
	for _, p := range []string{"/", "/api/hosts", "/api/sessions", "/ui/hosts", "/ws/herd"} {
		if res := get(t, c, srv.URL+p, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without token = %d, want 401", p, res.StatusCode)
		}
	}
	res, err := c.Post(srv.URL+"/api/runtime/stop", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /api/runtime/stop without token = %d, want 401", res.StatusCode)
	}
	bad := http.Header{authtoken.Header: {strings.Repeat("0", len(testToken))}}
	if res := get(t, c, srv.URL+"/api/hosts", bad); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", res.StatusCode)
	}
}

func TestAuthOpenRoutes(t *testing.T) {
	srv := authServer(t)
	c := srv.Client()
	for _, p := range []string{"/api/health", "/app.js"} {
		if res := get(t, c, srv.URL+p, nil); res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 without token", p, res.StatusCode)
		}
	}
}

func TestAuthHeader(t *testing.T) {
	srv := authServer(t)
	h := http.Header{authtoken.Header: {testToken}}
	if res := get(t, srv.Client(), srv.URL+"/api/hosts", h); res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/hosts with token header = %d", res.StatusCode)
	}
}

func TestAuthTokenParamSetsCookieAndRedirects(t *testing.T) {
	srv := authServer(t)
	c := srv.Client()
	c.CheckRedirect = noRedirect
	res := get(t, c, srv.URL+"/?native=1&"+tokenParam+"="+testToken+"&name=ada", nil)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("token load = %d, want 303", res.StatusCode)
	}
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Path != "/" || loc.Query().Get(tokenParam) != "" || loc.Query().Get("native") != "1" || loc.Query().Get("name") != "ada" {
		t.Fatalf("redirect to %q: want / with the token removed and other params kept", loc)
	}
	var cookie *http.Cookie
	for _, ck := range res.Cookies() {
		if strings.HasPrefix(ck.Name, "keel_auth_") {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatal("no auth cookie set")
	}
	u, _ := url.Parse(srv.URL)
	if cookie.Name != "keel_auth_"+u.Port() {
		t.Fatalf("cookie name %q, want per-port keel_auth_%s", cookie.Name, u.Port())
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("cookie %+v: want HttpOnly, SameSite=Strict, Path=/", cookie)
	}
	h := http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}
	for _, p := range []string{loc.RequestURI(), "/api/hosts", "/ui/hosts"} {
		if res := get(t, c, srv.URL+p, h); res.StatusCode != http.StatusOK {
			t.Errorf("GET %s with cookie = %d, want 200", p, res.StatusCode)
		}
	}
}

func TestAuthWrongTokenParam(t *testing.T) {
	srv := authServer(t)
	c := srv.Client()
	c.CheckRedirect = noRedirect
	res := get(t, c, srv.URL+"/?"+tokenParam+"=nope", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token param = %d, want 401", res.StatusCode)
	}
	if len(res.Cookies()) != 0 {
		t.Fatal("wrong token param set a cookie")
	}
}

func TestHealthProof(t *testing.T) {
	srv := authServer(t)
	res := get(t, srv.Client(), srv.URL+"/api/health?nonce=abc123", nil)
	var body struct {
		Proof string `json:"proof"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !authtoken.CheckProof(testToken, "abc123", body.Proof) {
		t.Fatalf("proof %q does not answer the nonce", body.Proof)
	}
	if strings.Contains(body.Proof, testToken) {
		t.Fatal("health leaked the token")
	}
}

// A DNS-rebound page reaches 127.0.0.1 with its own hostname in Host.
func TestLoopbackHostRequired(t *testing.T) {
	srv := authServer(t)
	u, _ := url.Parse(srv.URL)
	for _, host := range []string{"attacker.example:" + u.Port(), "attacker.example", "192.168.1.5:" + u.Port()} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/health", nil)
		req.Host = host
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, res.StatusCode)
		}
	}
	for _, host := range []string{"127.0.0.1:" + u.Port(), "localhost:" + u.Port(), "[::1]:" + u.Port()} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/health", nil)
		req.Host = host
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", host, res.StatusCode)
		}
	}
}

func TestHealthReportsBuild(t *testing.T) {
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, func(store.Host, int, int) (session.Bridge, error) { return newFakeBridge(), nil })
	srv := httptest.NewServer(New(st, rt, fs.FS(fstest.MapFS{}), nil).WithBuild("abc123-def456").Handler())
	t.Cleanup(srv.Close)
	var body struct {
		Build string `json:"build"`
	}
	if err := json.NewDecoder(get(t, srv.Client(), srv.URL+"/api/health", nil).Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Build != "abc123-def456" {
		t.Fatalf("health build %q", body.Build)
	}
}
