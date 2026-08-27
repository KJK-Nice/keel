package desktop

import (
	"os"
	"strings"
	"testing"
)

func TestNavigateJS(t *testing.T) {
	js := navigateJS("http://127.0.0.1:47241/?name=ada")
	if !strings.Contains(js, `window.location.replace("http://127.0.0.1:47241/?name=ada")`) {
		t.Fatalf("js = %s", js)
	}
}

func TestSplashHasMark(t *testing.T) {
	b, err := os.ReadFile("shell/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`M7 22 L16 8 L25 22`, `Starting Keel`} {
		if !strings.Contains(s, want) {
			t.Errorf("shell/index.html missing %q", want)
		}
	}
}
