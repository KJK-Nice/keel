package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"keel/internal/authtoken"
)

func TestAdmitShareRoute(t *testing.T) {
	srv := authServer(t)
	post := func(body string, token bool) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/share/s1/admit", strings.NewReader(body))
		if token {
			req.Header.Set(authtoken.Header, testToken)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := post(`{"peerId":"p1","allow":true}`, false); code != http.StatusUnauthorized {
		t.Errorf("admit without token = %d, want 401", code)
	}
	if code := post(`{"allow":true}`, true); code != http.StatusBadRequest {
		t.Errorf("admit without peerId = %d, want 400", code)
	}
	if code := post(`{"peerId":"p1","allow":true}`, true); code != http.StatusNotFound {
		t.Errorf("admit on an unshared session = %d, want 404", code)
	}
}
