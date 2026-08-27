package desktop

import (
	"strings"
	"testing"
)

func TestNavigateJS(t *testing.T) {
	js := navigateJS("http://127.0.0.1:47241/?name=ada")
	if !strings.Contains(js, `window.location.replace("http://127.0.0.1:47241/?name=ada")`) {
		t.Fatalf("js = %s", js)
	}
}
