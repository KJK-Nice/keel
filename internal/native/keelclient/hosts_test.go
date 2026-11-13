package keelclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// rec is a server that records each request and answers with the next canned reply.
type rec struct {
	c       *Client
	mu      sync.Mutex
	got     []string // "METHOD path"
	bodies  []string
	queries []string // the raw query of each request
	status  int
	reply   string
	replies map[string]string // by "METHOD path"
}

func newRec(t *testing.T) *rec {
	t.Helper()
	r := &rec{status: 200}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		b, _ := io.ReadAll(q.Body)
		r.mu.Lock()
		key := q.Method + " " + q.URL.Path
		r.got, r.bodies, r.queries = append(r.got, key), append(r.bodies, string(b)), append(r.queries, q.URL.RawQuery)
		status, reply := r.status, r.reply
		if v, ok := r.replies[key]; ok {
			reply = v
		}
		r.mu.Unlock()
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	r.c = NewAt(strings.TrimPrefix(srv.URL, "http://"), tok)
	return r
}

func (r *rec) last() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.got[len(r.got)-1], r.bodies[len(r.bodies)-1]
}

const pw = "hunter2-do-not-leak"

func TestUnlockSendsThePassphraseOnceAndMapsForbidden(t *testing.T) {
	r := newRec(t)
	r.reply = `{"locked":false}`
	if err := r.c.Unlock(context.Background(), pw); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/vault/unlock" || body != `{"passphrase":"`+pw+`"}` {
		t.Fatalf("%s %s", got, body)
	}
	r.status, r.reply = 403, `{"error":"incorrect passphrase"}`
	err := r.c.Unlock(context.Background(), pw)
	if !errors.Is(err, ErrPassphrase) {
		t.Fatalf("403 gave %v", err)
	}
	r.status, r.reply = 400, `{"error":"bad"}`
	err = r.c.Unlock(context.Background(), pw)
	if err == nil || errors.Is(err, ErrPassphrase) || strings.Contains(err.Error(), pw) {
		t.Fatalf("400 gave %v", err)
	}
}

func TestVaultReadsTheFieldsTheUnlockScreenNeeds(t *testing.T) {
	r := newRec(t)
	r.reply = `{"locked":true,"needsSetup":false,"firstRun":false,"mode":"keychain","keyringOff":true,"path":"~/x/keel.vault","rev":"e.7","sessions":2}`
	v, err := r.c.Vault(context.Background())
	if err != nil || v != (Vault{Locked: true, Mode: "keychain", KeyringOff: true, Path: "~/x/keel.vault", Rev: "e.7", Sessions: 2}) {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestSaveHostCreatesAndUpdatesAndKeepsABlankSecretOut(t *testing.T) {
	r := newRec(t)
	r.reply = `{"id":"h9","name":"web","password":"__saved__"}`
	ctx := context.Background()
	h := HostDetail{Name: "web", Address: "a", Port: 22, Username: "u", AuthType: "password", Password: pw, Tags: []string{"x"}}
	out, err := r.c.SaveHost(ctx, h)
	if err != nil || out.ID != "h9" {
		t.Fatal(out, err)
	}
	got, body := r.last()
	if got != "POST /api/hosts" || !strings.Contains(body, `"password":"`+pw+`"`) || !strings.Contains(body, `"tags":["x"]`) {
		t.Fatalf("create: %s %s", got, body)
	}
	// An update with the password left blank sends no password at all: the server keeps the saved one.
	h.ID, h.Password, h.Name = "h9", "", "web2"
	if _, err := r.c.SaveHost(ctx, h); err != nil {
		t.Fatal(err)
	}
	got, body = r.last()
	if got != "PUT /api/hosts/h9" || strings.Contains(body, `"password":`) || !strings.Contains(body, `"name":"web2"`) {
		t.Fatalf("update: %s %s", got, body)
	}
	// A changed password is sent.
	h.Password = pw
	r.c.SaveHost(ctx, h)
	if _, body = r.last(); !strings.Contains(body, `"password":"`+pw+`"`) {
		t.Fatalf("changed password missing: %s", body)
	}
}

func TestServerValidationTextReachesTheCaller(t *testing.T) {
	r := newRec(t)
	r.status, r.reply = 400, `{"error":"address is required"}`
	_, err := r.c.SaveHost(context.Background(), HostDetail{Name: "x", AuthType: "password", Password: pw})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 400 || se.Msg != "address is required" {
		t.Fatalf("%v", err)
	}
	if strings.Contains(err.Error(), "address is required") || strings.Contains(err.Error(), pw) {
		t.Fatalf("Error() carries a body: %v", err)
	}
}

func TestHostCallsUseTheRightMethodAndPath(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{}`
	r.c.DeleteHost(ctx, "h 1")
	if got, _ := r.last(); got != "DELETE /api/hosts/h 1" { // the path as the server decodes it
		t.Fatal(got)
	}
	r.c.DuplicateHost(ctx, "h1")
	if got, _ := r.last(); got != "POST /api/hosts/h1/duplicate" {
		t.Fatal(got)
	}
	r.c.SetAgentReadable(ctx, "h1", false)
	if got, body := r.last(); got != "PUT /api/hosts/h1/agent-readable" || body != `{"readable":false}` {
		t.Fatal(got, body)
	}
	r.reply = `{"hostname":"10.0.0.1:22","fingerprint":"SHA256:abc","keyType":"ssh-ed25519","known":true}`
	k, err := r.c.HostKey(ctx, "h1")
	if got, _ := r.last(); got != "GET /api/hosts/h1/host-key" || err != nil || !k.Known || k.Fingerprint != "SHA256:abc" {
		t.Fatal(got, k, err)
	}
	r.c.ForgetHostKey(ctx, "h1")
	if got, _ := r.last(); got != "DELETE /api/hosts/h1/host-key" {
		t.Fatal(got)
	}
	r.reply = `[{"id":"i1","name":"deploy key","kind":"key","username":"u","privateKey":"__saved__"}]`
	ids, err := r.c.Identities(ctx)
	if got, _ := r.last(); got != "GET /api/identities" || err != nil || len(ids) != 1 || ids[0].Name != "deploy key" {
		t.Fatal(got, ids, err)
	}
	r.reply = `[{"id":"g1","name":"prod","parentId":"g0"}]`
	gs, err := r.c.Groups(ctx)
	if got, _ := r.last(); got != "GET /api/groups" || err != nil || len(gs) != 1 || gs[0].ParentID != "g0" {
		t.Fatal(got, gs, err)
	}
}

func TestTestHostSendsTheDraftAndReadsEveryOutcome(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	draft := HostDetail{Name: "web", Address: "a", Port: 22, Username: "u", AuthType: "password", Password: pw}
	r.reply = `{"ok":false,"kind":"unknown-host","message":"m","hostname":"a:22","fingerprint":"SHA256:f","keyType":"ssh-ed25519","trustToken":"tt-secret"}`
	res, err := r.c.TestHost(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/hosts/test" || !strings.Contains(body, `"password":"`+pw+`"`) {
		t.Fatal(got)
	}
	offer, ok := res.HostKeyOffer()
	if !ok || offer.Token != "tt-secret" || offer.Hostname != "a:22" || offer.Fingerprint != "SHA256:f" || offer.Kind != "unknown-host" {
		t.Fatalf("%+v %v", offer, ok)
	}
	for _, kind := range []string{"ok", "auth", "timeout", "dns", "refused", "other"} {
		r.reply = fmt.Sprintf(`{"ok":%v,"kind":%q,"latencyMs":12,"trustToken":"tt-secret","fingerprint":"F"}`, kind == "ok", kind)
		res, _ := r.c.TestHost(ctx, draft)
		if res.Kind != kind || res.LatencyMs != 12 {
			t.Errorf("%s: %+v", kind, res)
		}
		if _, ok := res.HostKeyOffer(); ok {
			t.Errorf("%s offers a key to trust", kind)
		}
	}
	// A key with no token is no offer (nothing to send back).
	if _, ok := (TestResult{Kind: "unknown-host", Fingerprint: "F"}).HostKeyOffer(); ok {
		t.Error("an offer without a token")
	}
}

func TestNoSecretInPrintedValues(t *testing.T) {
	h := HostDetail{Name: "web", Address: "a", Port: 22, Username: "u", AuthType: "key", Password: pw, PrivateKey: "PRIVATE-" + pw, Passphrase: "PASS-" + pw}
	res := TestResult{Kind: "unknown-host", TrustToken: "tok-" + pw}
	for _, v := range []any{h, &h, res, &res} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
			if out := fmt.Sprintf(f, v); strings.Contains(out, pw) {
				t.Errorf("%s of %T prints a secret: %s", f, v, out)
			}
		}
	}
	// The body still carries the secrets: that is the one request that needs them.
	b, _ := json.Marshal(h)
	if !strings.Contains(string(b), pw) {
		t.Error("the request body lost its secrets")
	}
}

func TestErrorsNameNoBodyAndNoSecret(t *testing.T) {
	r := newRec(t)
	r.status, r.reply = 500, `{"error":"boom `+pw+`"}`
	err := r.c.Unlock(context.Background(), pw)
	if err == nil || strings.Contains(err.Error(), pw) || strings.Contains(err.Error(), "boom") {
		t.Fatalf("%v", err)
	}
	if _, err = r.c.TestHost(context.Background(), HostDetail{Password: pw}); err == nil || strings.Contains(err.Error(), pw) {
		t.Fatalf("%v", err)
	}
}
