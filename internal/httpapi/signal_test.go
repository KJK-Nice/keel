package httpapi

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"keel/internal/runtime"
	"keel/internal/share"
	"keel/internal/store"
)

func signalServer(t *testing.T) (*httptest.Server, *store.Store, *share.Hub) {
	t.Helper()
	t.Setenv("KEEL_VAULT_PASSPHRASE", "test-vault-pass")
	t.Setenv("KEEL_VAULT_FAST", "1")
	t.Setenv("KEEL_SIGNAL", "")
	st, err := store.New(t.TempDir() + "/keel.json")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(st, nil)
	hub := share.FromEnv(rt)
	web := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	srv := httptest.NewServer(New(st, rt, fs.FS(web), hub).Handler())
	t.Cleanup(srv.Close)
	return srv, st, hub
}

func putSignalURL(t *testing.T, srv *httptest.Server, url string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"url": url})
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/share/signal", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// Settings · Sharing saves the signal server in keel.json and the hub uses
// it for the next share; /api/runtime reports it and where it comes from.
func TestPutSignalSavesAndEnablesSharing(t *testing.T) {
	srv, st, hub := signalServer(t)
	if hub.Enabled() {
		t.Fatal("no server yet")
	}
	code, out := putSignalURL(t, srv, " https://signal.example/ ")
	if code != http.StatusOK {
		t.Fatalf("PUT = %d %v", code, out)
	}
	if out["signalURL"] != "https://signal.example" || out["signalSource"] != share.SourceSettings || out["signalSaved"] != "https://signal.example" {
		t.Fatalf("PUT body = %v", out)
	}
	if st.SignalURL() != "https://signal.example" || hub.URL() != "https://signal.example" {
		t.Fatalf("saved %q, hub %q", st.SignalURL(), hub.URL())
	}

	res, err := srv.Client().Get(srv.URL + "/api/runtime")
	if err != nil {
		t.Fatal(err)
	}
	var rt map[string]any
	_ = json.NewDecoder(res.Body).Decode(&rt)
	res.Body.Close()
	if rt["signalURL"] != "https://signal.example" || rt["signalSource"] != share.SourceSettings {
		t.Fatalf("/api/runtime = %v", rt)
	}

	code, out = putSignalURL(t, srv, "")
	if code != http.StatusOK || out["signalURL"] != "" || out["signalSource"] != "" {
		t.Fatalf("removing: %d %v", code, out)
	}
	if hub.Enabled() || st.SignalURL() != "" {
		t.Fatal("removing the server should turn Share off")
	}
}

func TestPutSignalRejectsBadURLs(t *testing.T) {
	srv, st, _ := signalServer(t)
	for _, bad := range []string{"signal.example", "ftp://x.example", "https://x.example/#k=1"} {
		if code, out := putSignalURL(t, srv, bad); code != http.StatusBadRequest {
			t.Errorf("PUT %q = %d %v, want 400", bad, code, out)
		}
	}
	if st.SignalURL() != "" {
		t.Fatalf("a rejected URL was saved: %q", st.SignalURL())
	}
}
