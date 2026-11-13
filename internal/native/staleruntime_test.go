package native

import "testing"

func TestRuntimeIsStale(t *testing.T) {
	for _, c := range []struct {
		app, rt string
		want    bool
	}{
		{"", "abc", false}, // no build id: never warns
		{"", "", false},
		{"abc", "abc", false},
		{"abc", "xyz", true},
		{"abc", "", true}, // a runtime from before build ids is older
	} {
		if got := runtimeIsStale(c.app, c.rt); got != c.want {
			t.Errorf("runtimeIsStale(%q, %q) = %v", c.app, c.rt, got)
		}
	}
}
