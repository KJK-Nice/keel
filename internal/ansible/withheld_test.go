package ansible

import (
	"strings"
	"testing"
)

// A variable the caller withheld (its value is in the vault) is named in a
// comment, in both formats, and never written.
func TestWithheldVars(t *testing.T) {
	inv := Inventory{Group: "g", Vars: []Var{{Name: "ok", Value: "1"}}, Withheld: []string{"old_pw"}}
	for _, out := range []string{INI(inv), YAML(inv)} {
		if !strings.Contains(out, "# old_pw: left out: its value is also in Keel's vault") || !strings.Contains(out, "ok") {
			t.Fatalf("withheld:\n%s", out)
		}
	}
	// Only withheld names: the INI vars section still says so.
	if out := INI(Inventory{Group: "g", Withheld: []string{"x"}}); !strings.Contains(out, "g:vars]\n# x: left out") {
		t.Fatalf("only withheld:\n%s", out)
	}
}
