package app

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestWantWebview(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		env  string
		want bool
		err  string
	}{
		{"default", nil, "", false, ""},
		{"env native", nil, "native", false, ""},
		{"env webview", nil, "webview", true, ""},
		{"flag", []string{"--webview"}, "", true, ""},
		{"flag beats env native", []string{"--name", "x", "--webview"}, "native", true, ""},
		{"flag beats a bad env", []string{"--webview"}, "gtk", true, ""},
		{"unknown env", nil, "gtk", false, "native or webview"},
		{"case matters", nil, "Webview", false, "native or webview"},
	} {
		got, err := WantWebview(c.args, c.env)
		if got != c.want || (err == nil) != (c.err == "") || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: got %v, %v", c.name, got, err)
		}
	}
}

// The calls before a window run in this order, with a 0700 data directory, and stop at the first failure.
func TestPrepareOrder(t *testing.T) {
	var calls []string
	boom := errors.New("boom")
	steps := func(failAt string) serveSteps {
		step := func(name string) error {
			calls = append(calls, name)
			if name == failAt {
				return boom
			}
			return nil
		}
		return serveSteps{
			mkdir: func(dir string, perm os.FileMode) error {
				if dir != "/d" || perm != 0o700 {
					t.Errorf("mkdir %q %v", dir, perm)
				}
				return step("mkdir")
			},
			token:  func(string) (string, error) { return "tok", step("token") },
			ensure: func(a, d string) error { return step("ensure") },
			verify: func(a, tok string) error {
				if a != "127.0.0.1:1" || tok != "tok" {
					t.Errorf("verify %q", a)
				}
				return step("verify")
			},
		}
	}
	order := []string{"mkdir", "token", "ensure", "verify"}
	if _, err := steps("").prepare("127.0.0.1:1", "/d"); err != nil || strings.Join(calls, " ") != strings.Join(order, " ") {
		t.Fatalf("calls %v, err %v", calls, err)
	}
	for i, fail := range order {
		calls = nil
		if _, err := steps(fail).prepare("127.0.0.1:1", "/d"); !errors.Is(err, boom) || len(calls) != i+1 {
			t.Errorf("fail at %s: calls %v, err %v", fail, calls, err)
		}
	}
}
