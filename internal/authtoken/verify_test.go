package authtoken

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVerifyServer(t *testing.T) {
	const token = "our-token"
	for _, ch := range []Challenge{WindowChallenge, MCPChallenge} {
		good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.String(), token) || r.Header.Get(Header) != "" || r.Header.Get(MCPHeader) != "" {
				t.Errorf("VerifyServer sent the token: %s", r.URL)
			}
			_, _ = w.Write([]byte(`{"` + ch.Field + `":"` + Proof(token, r.URL.Query().Get(ch.Param)) + `"}`))
		}))
		u, _ := url.Parse(good.URL)
		c := &http.Client{CheckRedirect: NoRedirect}
		if err := VerifyServer(context.Background(), c, u.Host, ch, token); err != nil {
			t.Errorf("%s: %v", ch.Param, err)
		}
		if err := VerifyServer(context.Background(), c, u.Host, ch, "another-token"); !errors.Is(err, ErrUnproven) {
			t.Errorf("%s: a proof for another token = %v, want ErrUnproven", ch.Param, err)
		}
		// A server that sends the health check elsewhere has proven nothing, even when the other end would.
		away := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, good.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
		}))
		u2, _ := url.Parse(away.URL)
		if err := VerifyServer(context.Background(), c, u2.Host, ch, token); !errors.Is(err, ErrUnproven) {
			t.Errorf("%s: a redirected health check = %v, want ErrUnproven", ch.Param, err)
		}
		away.Close()
		good.Close()
		if err := VerifyServer(context.Background(), c, u.Host, ch, token); err == nil || errors.Is(err, ErrUnproven) {
			t.Errorf("%s: a server that is down = %v, want the transport's error", ch.Param, err)
		}
	}
	// The other challenge's proof does not stand in.
	cross := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"` + MCPChallenge.Field + `":"` + Proof(token, r.URL.Query().Get(WindowChallenge.Param)) + `"}`))
	}))
	defer cross.Close()
	u, _ := url.Parse(cross.URL)
	if err := VerifyServer(context.Background(), http.DefaultClient, u.Host, WindowChallenge, token); !errors.Is(err, ErrUnproven) {
		t.Errorf("a proof in the other field = %v, want ErrUnproven", err)
	}
}
