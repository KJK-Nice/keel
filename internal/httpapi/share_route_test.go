package httpapi

import (
	"net/http"
	"testing"
)

func TestStopShareRoute(t *testing.T) {
	srv, _, _, _ := testServer(t)
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/share/abc123", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /api/share/{id} = %d, want 204", res.StatusCode)
	}
}
