package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func staticGet(t *testing.T, h http.Handler, path string, hdr map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func TestStaticRevalidatesWithETag(t *testing.T) {
	js := strings.Repeat("export const x = 1;\n", 200)
	fsys := fstest.MapFS{"js/term.js": {Data: []byte(js)}}
	h := newStaticHandler(fsys)

	res := staticGet(t, h, "/js/term.js", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control %q, want no-cache (revalidate, never store-and-forget)", cc)
	}
	if strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
		t.Fatal("static files must be cacheable")
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type %q", ct)
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != js {
		t.Fatal("identity body differs")
	}

	res = staticGet(t, h, "/js/term.js", map[string]string{"If-None-Match": etag})
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation status %d, want 304", res.StatusCode)
	}
}

func TestStaticGzipsTextOnce(t *testing.T) {
	css := strings.Repeat(".k-pane { color: var(--k-fg); }\n", 300)
	fsys := fstest.MapFS{
		"css/keel.css":      {Data: []byte(css)},
		"fonts/inter.woff2": {Data: bytes.Repeat([]byte{0x77, 0x4f, 0x46, 0x32}, 1000)},
		"js/tiny.js":        {Data: []byte("1")},
	}
	h := newStaticHandler(fsys)

	res := staticGet(t, h, "/css/keel.css", map[string]string{"Accept-Encoding": "br, gzip"})
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("css not gzipped")
	}
	if !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatal("gzipped response must Vary on Accept-Encoding")
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != css {
		t.Fatal("gzip body differs")
	}

	res = staticGet(t, h, "/css/keel.css", map[string]string{"Accept-Encoding": "gzip;q=0"})
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatal("gzip;q=0 must get identity")
	}
	res = staticGet(t, h, "/fonts/inter.woff2", map[string]string{"Accept-Encoding": "gzip"})
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatal("woff2 is already compressed")
	}
	res = staticGet(t, h, "/js/tiny.js", map[string]string{"Accept-Encoding": "gzip"})
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatal("tiny files are not worth gzip")
	}
}

// In development the UI is served from disk: an edited file must get a new
// ETag on the next request, not the cached one.
func TestStaticPicksUpEditedFiles(t *testing.T) {
	fsys := fstest.MapFS{"js/term.js": {Data: []byte("one"), ModTime: time.Unix(1, 0)}}
	h := newStaticHandler(fsys)
	first := staticGet(t, h, "/js/term.js", nil).Header.Get("ETag")

	fsys["js/term.js"] = &fstest.MapFile{Data: []byte("two"), ModTime: time.Unix(2, 0)}
	res := staticGet(t, h, "/js/term.js", map[string]string{"If-None-Match": first})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("edited file answered %d to the old ETag", res.StatusCode)
	}
	if body, _ := io.ReadAll(res.Body); string(body) != "two" {
		t.Fatalf("body %q, want two", body)
	}
	if res.Header.Get("ETag") == first {
		t.Fatal("ETag did not change with the content")
	}
}

func TestStaticFallsBackForDirsAndMissing(t *testing.T) {
	fsys := fstest.MapFS{"js/term.js": {Data: []byte("x")}}
	h := newStaticHandler(fsys)
	if res := staticGet(t, h, "/js/missing.js", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file status %d", res.StatusCode)
	}
	if res := staticGet(t, h, "/../js/term.js", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("cleaned path status %d", res.StatusCode)
	}
}
