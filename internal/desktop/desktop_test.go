package desktop

import (
	"strings"
	"testing"
)

func TestChromeArgsUseAppMode(t *testing.T) {
	args := chromeArgs("http://127.0.0.1:47241/?name=ada", "/tmp/keel-profile")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--app=http://127.0.0.1:47241/?name=ada") {
		t.Fatalf("missing --app: %v", args)
	}
	if !strings.Contains(joined, "--user-data-dir=/tmp/keel-profile") {
		t.Fatalf("missing isolated profile: %v", args)
	}
}

func TestLookPathOverrideMissing(t *testing.T) {
	t.Setenv("KEEL_BROWSER", "/definitely/not/a/browser")
	if _, err := LookPath(); err == nil {
		t.Fatal("expected error for missing KEEL_BROWSER")
	}
}
