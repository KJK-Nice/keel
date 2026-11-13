package keelclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// secret stands in for a passphrase, a key or a password. It opens nothing.
const secret = "scratch-secret-for-tests"

func TestSetupVaultSendsThePassphraseOnceAndMapsConflict(t *testing.T) {
	r := newRec(t)
	r.reply = `{"locked":false}`
	if err := r.c.SetupVault(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/vault/setup" || body != `{"passphrase":"`+secret+`"}` {
		t.Fatalf("%s %s", got, body)
	}
	r.status, r.reply = 409, `{"error":"vault already exists"}`
	if err := r.c.SetupVault(context.Background(), secret); !errors.Is(err, ErrVaultExists) {
		t.Fatalf("409 gave %v", err)
	}
	r.status, r.reply = 400, `{"error":"passphrase is required"}`
	err := r.c.SetupVault(context.Background(), secret)
	var se *StatusError
	if !errors.As(err, &se) || se.Msg != "passphrase is required" || strings.Contains(err.Error(), secret) {
		t.Fatalf("400 gave %v", err)
	}
}

func TestFirstRunDoneAndTheVaultFieldsItNeeds(t *testing.T) {
	r := newRec(t)
	r.status = 204
	if err := r.c.FirstRunDone(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "POST /api/first-run/done" || body != "" {
		t.Fatalf("%s %q", got, body)
	}
	r.status, r.reply = 200, `{"locked":false,"needsSetup":false,"firstRun":true,"sshConfig":"~/.ssh/config"}`
	v, err := r.c.Vault(context.Background())
	if err != nil || !v.FirstRun || v.SSHConfig != "~/.ssh/config" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestSSHConfigPreviewReadsEveryFieldTheImportTableShows(t *testing.T) {
	r := newRec(t)
	r.reply = `{"path":"~/.ssh/config","found":true,
	 "hosts":[{"alias":"web","hostName":"w.example","user":"deploy","port":2222,"identityFiles":["/h/.ssh/id_web"],"saved":true,
	   "proxy":"ProxyJump","proxyJump":"gate","jumpAlias":"gate","jumpSaved":"g","jumpVia":"u@g:22","jumpProblem":"p","source":"x:1","notes":["n"]}],
	 "patterns":[{"pattern":"*.prod","source":"x:9"}],
	 "keys":[{"path":"/h/.ssh/id_web","display":"~/.ssh/id_web","name":"id_web","exists":true,"encrypted":true,"problem":"","usedBy":["web"]}],
	 "notes":[{"source":"x:14","text":"Match block skipped"}]}`
	p, err := r.c.SSHConfigPreview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "GET /api/ssh-config/preview" || body != "" {
		t.Fatalf("%s %q", got, body)
	}
	h := p.Hosts[0]
	if !p.Found || h.Alias != "web" || h.Port != 2222 || !h.Saved || h.Proxy != "ProxyJump" || h.JumpAlias != "gate" || h.JumpSaved != "g" ||
		h.JumpVia != "u@g:22" || h.JumpProblem != "p" || h.IdentityFiles[0] != "/h/.ssh/id_web" || h.Notes[0] != "n" {
		t.Fatalf("host %+v", h)
	}
	k := p.Keys[0]
	if !k.Encrypted || !k.Exists || k.UsedBy[0] != "web" || p.Patterns[0].Pattern != "*.prod" || p.Notes[0].Text != "Match block skipped" {
		t.Fatalf("%+v", p)
	}
}

func TestSSHConfigImportSendsOnlyTheTicksAndKeepsPassphrasesOut(t *testing.T) {
	r := newRec(t)
	r.reply = `{"hosts":[{"id":"h1","name":"web"}],"identities":[{"id":"i1","name":"id_web","kind":"key","privateKey":"__saved__"}],"failed":[{"alias":"db","error":"no jump"}]}`
	req := ImportRequest{Hosts: []string{"web"}, Keys: []ImportKey{{Path: "/h/.ssh/id_web", Passphrase: secret}}, WithoutJump: []string{"db"}}
	out, err := r.c.SSHConfigImport(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	got, body := r.last()
	if got != "POST /api/ssh-config/import" || body != `{"hosts":["web"],"keys":[{"path":"/h/.ssh/id_web","passphrase":"`+secret+`"}],"withoutJump":["db"]}` {
		t.Fatalf("%s %s", got, body)
	}
	if len(out.Hosts) != 1 || out.Identities[0].Name != "id_web" || !out.Identities[0].HasPrivateKey || out.Failed[0].Alias != "db" {
		t.Fatalf("%+v", out)
	}
	for _, f := range []string{fmt.Sprintf("%v", req), fmt.Sprintf("%+v", req), fmt.Sprintf("%#v", req), fmt.Sprintf("%+v", req.Keys), fmt.Sprintf("%#v", req.Keys[0])} {
		if strings.Contains(f, secret) {
			t.Fatalf("a formatted request shows the passphrase: %s", f)
		}
	}
	// No ticks still sends empty lists, as the webview does.
	if _, err := r.c.SSHConfigImport(context.Background(), ImportRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, body := r.last(); body != `{"hosts":[],"keys":[]}` {
		t.Fatalf("empty import: %s", body)
	}
}

func TestSSHConfigImportRefusalCarriesTheKeyAndKindButNoPassphrase(t *testing.T) {
	r := newRec(t)
	r.status, r.reply = 400, `{"error":"That passphrase doesn't open id_web","errorKind":"wrongPassphrase","key":"/h/.ssh/id_web"}`
	_, err := r.c.SSHConfigImport(context.Background(), ImportRequest{Hosts: []string{"web"}, Keys: []ImportKey{{Path: "/h/.ssh/id_web", Passphrase: secret}}})
	var se *StatusError
	if !errors.As(err, &se) || se.ErrorKind != "wrongPassphrase" || se.Key != "/h/.ssh/id_web" || se.Code != 400 {
		t.Fatalf("%v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprintf("%+v", se), secret) {
		t.Fatalf("the error shows the passphrase: %v", err)
	}
}

func TestIdentitiesKeepOnlyWhetherASecretIsStored(t *testing.T) {
	r := newRec(t)
	r.reply = `[{"id":"i1","name":"deploy","kind":"key","username":"ubuntu","createdAt":"2026-03-04T05:06:07Z","privateKey":"__saved__","passphrase":"__saved__"},
	 {"id":"i2","name":"db","kind":"password","password":"__saved__"},{"id":"i3","name":"empty","kind":"key"}]`
	ids, err := r.c.Identities(context.Background())
	if err != nil || len(ids) != 3 {
		t.Fatal(ids, err)
	}
	a, b, c := ids[0], ids[1], ids[2]
	if a.Username != "ubuntu" || a.CreatedAt.Year() != 2026 || !a.HasPrivateKey || !a.HasPassphrase || a.HasPassword {
		t.Errorf("key identity %+v", a)
	}
	if !b.HasPassword || b.HasPrivateKey || c.HasPrivateKey || c.HasPassphrase {
		t.Errorf("%+v %+v", b, c)
	}
	if s := fmt.Sprintf("%+v", a); strings.Contains(s, "__saved__") {
		t.Errorf("the placeholder is held: %s", s)
	}
}

func TestSaveIdentityCreatesUpdatesAndKeepsABlankSecretOut(t *testing.T) {
	r := newRec(t)
	r.reply = `{"id":"i9","name":"deploy","kind":"password","password":"__saved__"}`
	ctx := context.Background()
	in := IdentityInput{Name: "deploy", Username: "ubuntu", Kind: "password", Password: secret}
	out, err := r.c.SaveIdentity(ctx, in)
	if err != nil || out.ID != "i9" || !out.HasPassword {
		t.Fatal(out, err)
	}
	got, body := r.last()
	if got != "POST /api/identities" || body != `{"name":"deploy","username":"ubuntu","kind":"password","password":"`+secret+`"}` {
		t.Fatalf("create: %s %s", got, body)
	}
	in.ID, in.Password = "i9", ""
	if _, err := r.c.SaveIdentity(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, body = r.last()
	if got != "PUT /api/identities/i9" || strings.Contains(body, `"password":`) || strings.Contains(body, `"id"`) {
		t.Fatalf("update: %s %s", got, body)
	}
	in.Kind, in.PrivateKey, in.Passphrase = "key", "-----BEGIN-----"+secret, secret+"-phrase"
	for _, f := range []string{fmt.Sprintf("%v", in), fmt.Sprintf("%+v", in), fmt.Sprintf("%#v", in)} {
		if strings.Contains(f, secret) {
			t.Fatalf("a formatted identity input shows a secret: %s", f)
		}
	}
	r.status, r.reply = 400, `{"error":"name is required"}`
	_, err = r.c.SaveIdentity(ctx, IdentityInput{Kind: "key", PrivateKey: secret})
	var se *StatusError
	if !errors.As(err, &se) || se.Msg != "name is required" || strings.Contains(err.Error(), secret) {
		t.Fatalf("%v", err)
	}
}

func TestDeleteIdentityMethodAndPath(t *testing.T) {
	r := newRec(t)
	r.status = 204
	if err := r.c.DeleteIdentity(context.Background(), "i 1"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "DELETE /api/identities/i 1" || body != "" {
		t.Fatalf("%s %q", got, body)
	}
}

func TestLogsAsksForTheLimitAndReadsEveryField(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		gotQuery = q.Method + " " + q.URL.Path + "?" + q.URL.RawQuery
		fmt.Fprint(w, `[{"time":"2026-03-04T05:06:07Z","kind":"agent.read","message":"read 12 lines","hostId":"h1","tunnelId":"t1","lines":12,"bytes":300,"redactions":{"token":2},"source":"jev","count":3,"modelMs":40,"jevMs":9}]`)
	}))
	defer srv.Close()
	c := NewAt(strings.TrimPrefix(srv.URL, "http://"), tok)
	out, err := c.Logs(context.Background(), 150)
	if err != nil || gotQuery != "GET /api/logs?limit=150" {
		t.Fatalf("%q %v", gotQuery, err)
	}
	e := out[0]
	if e.Kind != "agent.read" || e.HostID != "h1" || e.TunnelID != "t1" || e.Lines != 12 || e.Bytes != 300 || e.Redactions["token"] != 2 || e.Source != "jev" || e.Count != 3 || e.ModelMs != 40 || e.JevMs != 9 {
		t.Fatalf("%+v", e)
	}
	if _, err := c.Logs(context.Background(), 0); err != nil || gotQuery != "GET /api/logs?" {
		t.Fatalf("%q %v", gotQuery, err)
	}
}

func TestAgentProvidersAreDisplayText(t *testing.T) {
	r := newRec(t)
	r.reply = `[{"id":"gemini","name":"Google Gemini","keyLabel":"Gemini API key","keyHelpUrl":"https://k","warning":"w","termsUrl":"https://t","kind":"model"}]`
	ps, err := r.c.AgentProviders(context.Background())
	if err != nil || len(ps) != 1 || ps[0].KeyLabel != "Gemini API key" || ps[0].Warning != "w" {
		t.Fatal(ps, err)
	}
	if got, _ := r.last(); got != "GET /api/agent/providers" {
		t.Fatal(got)
	}
}
