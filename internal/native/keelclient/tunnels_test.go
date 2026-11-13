package keelclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTunnelCallsUseTheForwardAPI(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `[{"id":"t1","name":"db","hostId":"h1","kind":"local","bindAddr":"127.0.0.1","bindPort":5433,"destHost":"127.0.0.1","destPort":5432,"status":"ready","listenAddr":"127.0.0.1:5433"},{"id":"t2","status":"error","error":"refused"}]`
	ts, err := r.c.Tunnels(ctx)
	if err != nil || len(ts) != 2 || ts[0].ListenAddr != "127.0.0.1:5433" || ts[0].DestPort != 5432 || ts[1].Error != "refused" {
		t.Fatalf("tunnels %+v %v", ts, err)
	}
	if got, _ := r.last(); got != "GET /api/tunnels" {
		t.Errorf("got %s", got)
	}
	r.reply = `{"id":"t3"}`
	if _, err := r.c.SaveTunnel(ctx, TunnelInput{Name: "x", HostID: "h", Kind: "dynamic", BindAddr: "127.0.0.1", BindPort: 1080}); err != nil {
		t.Fatal(err)
	}
	got, body := r.last()
	if got != "POST /api/tunnels" || strings.Contains(body, "destHost") || !strings.Contains(body, `"kind":"dynamic"`) {
		t.Errorf("create %s %s", got, body)
	}
	if _, err := r.c.SaveTunnel(ctx, TunnelInput{ID: "t3", Name: "x", HostID: "h", Kind: "local", BindAddr: "127.0.0.1", DestHost: "d", DestPort: 1}); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/tunnels/t3" || strings.Contains(body, `"id"`) {
		t.Errorf("update %s %s", got, body)
	}
	for _, c := range []struct {
		call func() error
		want string
	}{
		{func() error { _, err := r.c.StartTunnel(ctx, "t3"); return err }, "POST /api/tunnels/t3/start"},
		{func() error { _, err := r.c.StopTunnel(ctx, "t3"); return err }, "POST /api/tunnels/t3/stop"},
		{func() error { return r.c.DeleteTunnel(ctx, "t3") }, "DELETE /api/tunnels/t3"},
	} {
		if err := c.call(); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.last(); got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
}

func TestStartTunnelRefusedForAnUnknownKeyCarriesTheOfferWithoutPrintingTheToken(t *testing.T) {
	r := newRec(t)
	r.status = 409
	r.reply = `{"error":"unknown host","errorKind":"unknown-host","hostname":"h:22","fingerprint":"SHA256:abc","keyType":"ssh-ed25519","trustToken":"one-time-secret"}`
	_, err := r.c.StartTunnel(context.Background(), "t1")
	var se *StatusError
	if !errors.As(err, &se) || se.Offer.Kind != "unknown-host" || se.Offer.Fingerprint != "SHA256:abc" || se.Offer.Token != "one-time-secret" {
		t.Fatalf("err %v", err)
	}
	if s := fmt.Sprintf("%v %+v %s", err, se.Offer, se.Offer); strings.Contains(s, "one-time-secret") {
		t.Errorf("the token is printed: %s", s)
	}
}

func TestRuntimeCallsAndTheAcceptedStop(t *testing.T) {
	r := newRec(t)
	ctx := context.Background()
	r.reply = `{"build":"9d62912abcde-0123","pid":42,"addr":"127.0.0.1:1","dataDir":"/d","sessions":2,"uptimeSec":125,"canStop":true,"signalURL":"https://s","signalSource":"settings","signalSaved":"https://s"}`
	rt, err := r.c.Runtime(ctx)
	if err != nil || rt.Build != "9d62912abcde-0123" || rt.Sessions != 2 || rt.UptimeSec != 125 || !rt.CanStop || rt.SignalSaved != "https://s" {
		t.Fatalf("runtime %+v %v", rt, err)
	}
	if got, _ := r.last(); got != "GET /api/runtime" {
		t.Errorf("got %s", got)
	}
	if _, err := r.c.SetSignal(ctx, "https://x.example"); err != nil {
		t.Fatal(err)
	}
	if got, body := r.last(); got != "PUT /api/share/signal" || body != `{"url":"https://x.example"}` {
		t.Errorf("signal %s %s", got, body)
	}
	r.status, r.reply = 202, ""
	if err := r.c.StopRuntime(ctx); err != nil {
		t.Fatalf("a 202 is the answer to a stop: %v", err)
	}
	if got, _ := r.last(); got != "POST /api/runtime/stop" {
		t.Errorf("got %s", got)
	}
	r.status, r.reply = 400, `{"error":"not a valid signal server"}`
	var se *StatusError
	if _, err := r.c.SetSignal(ctx, "nope"); !errors.As(err, &se) || se.Msg != "not a valid signal server" {
		t.Errorf("refusal %v", err)
	}
}
