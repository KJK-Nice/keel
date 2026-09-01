package components

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScriptsHandlerServesEmbeddedBundle(t *testing.T) {
	t.Setenv("GO_ENV", "production")
	srv := httptest.NewServer(http.StripPrefix("/components/", ScriptsHandler()))
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/components/shadcn-templ.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if len(body) < 100 || !strings.Contains(string(body), "window.tui") {
		t.Fatalf("expected component JS bundle, got %d bytes", len(body))
	}
}

func TestScriptsHandlerFallsBackWhenComponentsDirMissing(t *testing.T) {
	t.Setenv("GO_ENV", "")
	t.Chdir(t.TempDir())
	srv := httptest.NewServer(http.StripPrefix("/components/", ScriptsHandler()))
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/components/shadcn-templ.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || len(body) < 100 {
		t.Fatalf("missing components dir should still serve embed: %d (%d bytes)", res.StatusCode, len(body))
	}
}
