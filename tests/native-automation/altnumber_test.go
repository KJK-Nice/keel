//go:build nativeautomation

package nativeautomation

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// post sends a POST with the scratch token (never printed).
func (r *rig) post(path string, body any) int {
	r.t.Helper()
	tok, _ := os.ReadFile(filepath.Join(r.data, "keel-serve.token"))
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "http://"+r.addr+path, strings.NewReader(string(b)))
	req.Header.Set("X-Keel-Token", strings.TrimSpace(string(tok)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// optionDigit presses Option+<n> as a Mac keyboard does: the key with Alt, then the character the layout types for it
// (Option+2 types "™"), then the release.
func (r *rig) optionDigit(n, typed string) {
	r.t.Helper()
	r.input(map[string]any{"type": "keyDown", "key": "Alt+" + n}, map[string]any{"type": "text", "text": typed}, map[string]any{"type": "keyUp", "key": "Alt+" + n})
}

// newTabRig starts a scratch daemon with a second host and the window on the New tab page.
func newTabRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.startServe()
	if code := r.post("/api/hosts", map[string]any{"name": "Second shell", "authType": "local", "color": "#7aa2f7"}); code != 201 {
		t.Fatalf("POST /api/hosts: %d", code)
	}
	r.startApp()
	r.press("host Second shell") // now recent: the page then lists This machine first and Second shell second
	r.waitFor("its tab", has("tab Second shell"))
	r.press("New tab")
	r.waitFor("the cards", has("start card Second shell"))
	return r
}

func countLabel(ns []node, label string) (n int) {
	for _, o := range ns {
		if o.Label == label {
			n++
		}
	}
	return n
}

// ⌥1 to ⌥6 open the nth card of the New tab page, with the caret in "Where to?" and with nothing focused,
// and the input never takes the character the key types.
func TestOptionDigitOpensACardWithTheInputFocused(t *testing.T) {
	r := newTabRig(t)
	r.settle()
	if got := r.focusLabel(); !strings.Contains(got, "Host to connect to") {
		t.Fatalf("the keyboard is on %q, not the input", got)
	}
	r.optionDigit("2", "™")
	r.waitFor("the second card opened", func(ns []node) bool { return countLabel(ns, "tab Second shell") == 2 })
	r.settle()
	if n := countLabel(r.tree(), "tab Second shell"); n != 2 {
		t.Errorf("⌥2 opened %d tabs of Second shell, not one more", n-1)
	}
	r.quit()
}

// With no card behind the number nothing opens, and the character the Option key types stays out of the input.
func TestOptionDigitTypesNothingInTheInput(t *testing.T) {
	r := newTabRig(t)
	r.settle()
	r.typeText("ab")
	r.settle()
	r.optionDigit("6", "º")
	r.settle()
	if n := find(r.tree(), "Host to connect to"); n == nil || n.Value != "ab" {
		t.Fatalf("⌥6 left %+v in the input", n)
	}
	r.quit()
}

func TestOptionDigitOpensACardWithNothingFocused(t *testing.T) {
	r := newTabRig(t)
	r.settle()
	r.key("Tab") // the input gives the keyboard up
	r.key("Tab")
	r.optionDigit("1", "¡")
	r.waitFor("the first card opened", has("tab Local Shell"))
	r.quit()
}
