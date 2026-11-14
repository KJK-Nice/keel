package keelclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"keel/internal/authtoken"
)

// The client never follows a redirect, so the token header is never sent to another server: not by a plain
// request, not by Ask's client, which has no timeout of its own.
func TestClientDoesNotFollowRedirects(t *testing.T) {
	var (
		mu   sync.Mutex
		hits []string
	)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path+" token="+r.Header.Get(authtoken.Header))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+r.URL.RequestURI(), code)
		}))
		u, _ := url.Parse(srv.URL)
		c := NewAt(u.Host, "the-token")
		if _, err := c.Vault(context.Background()); err == nil {
			t.Errorf("%d: a redirected GET succeeded", code)
		}
		if err := c.Kill(context.Background(), "s1"); err == nil {
			t.Errorf("%d: a redirected DELETE succeeded", code)
		}
		if _, err := c.Ask(context.Background(), AskRequest{Question: "q"}); err == nil {
			t.Errorf("%d: a redirected Ask succeeded", code)
		}
		srv.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 0 {
		t.Fatalf("the other server was reached: %q", hits)
	}
}
