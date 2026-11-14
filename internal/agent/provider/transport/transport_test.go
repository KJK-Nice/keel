package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// All tests talk to an httptest server on loopback; nothing leaves the
// machine.

const testKey = "transport-test-key-0123"

type reply struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func post(t *testing.T, errorText bool, status int, body string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != testKey || strings.Contains(r.URL.String(), testKey) {
			t.Errorf("key header %q, url %s", r.Header.Get("X-Key"), r.URL)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	c, err := New(Config{
		Name: "vendor", Timeout: 5 * time.Second, MaxBody: 1 << 20, ErrorText: errorText,
		Auth: func(h http.Header, key string) { h.Set("X-Key", key) },
	}, "  "+testKey+" ", WithBaseURL(srv.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	var out reply
	return c.Post(context.Background(), "/v1/x", map[string]string{"a": "b"}, &out, func() (string, bool) {
		if out.Error == nil {
			return "", false
		}
		return out.Error.Message, true
	})
}

func TestPostErrorMapping(t *testing.T) {
	long := strings.Repeat("x", 2*maxErrorText)
	for _, tc := range []struct {
		name      string
		errorText bool
		status    int
		body      string
		want      string // "" for no error
		not       string
	}{
		{"ok", true, 200, `{}`, "", ""},
		{"status and the vendor's message", true, 400, `{"error":{"message":"API key not valid.\nPass a valid key ` + testKey + `"}}`, "vendor: HTTP 400: API key not valid. Pass a valid key [key]", testKey},
		{"an error in a 200 reply", true, 200, `{"error":{"message":"quota"}}`, "vendor: HTTP 200: quota", ""},
		{"a long message is capped", true, 400, `{"error":{"message":"` + long + `"}}`, "vendor: HTTP 400: " + strings.Repeat("x", maxErrorText) + "…", long},
		{"not JSON on an error status: the status", true, 502, `<html>bad gateway</html>`, "vendor: HTTP 502: Bad Gateway", "html"},
		{"not JSON on 200", true, 200, `<html>`, "vendor: HTTP 200: unreadable response", ""},
		{"status only when the text is not wanted", false, 422, `{"error":{"message":"field prompt_line: secret words"}}`, "vendor: HTTP 422: Unprocessable Entity", "secret words"},
	} {
		err := post(t, tc.errorText, tc.status, tc.body)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.want != "" && (err == nil || err.Error() != tc.want):
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		case tc.not != "" && err != nil && strings.Contains(err.Error(), tc.not):
			t.Errorf("%s: error holds %q: %v", tc.name, tc.not, err)
		}
	}
}

func TestOutputLimitAndStop(t *testing.T) {
	if got := OutputLimit(0, 1000, 100000); got != DefaultAnswerTokens+1000 {
		t.Errorf("no limit asked: %d", got)
	}
	if got := OutputLimit(90000, 20000, 100000); got != 100000 {
		t.Errorf("over the cap: %d", got)
	}
	table := map[string]string{"": "", "done": "", "max": "length"}
	for reason, want := range map[string]string{"": "", "done": "", "max": "length", "new-reason": "other"} {
		if got := Stop(table, reason); got != want {
			t.Errorf("Stop(%q) = %q, want %q", reason, got, want)
		}
	}
}
