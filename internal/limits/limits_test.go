package limits

import (
	"strings"
	"testing"
)

func TestStartupCommandProblem(t *testing.T) {
	for _, ok := range []string{"", "tmux attach", "cd /srv && ls"} {
		if p := StartupCommandProblem(ok); p != "" {
			t.Errorf("%q: %s", ok, p)
		}
	}
	for cmd, want := range map[string]string{
		strings.Repeat("a", MaxStartupCommand+1): "longer than 1024 bytes",
		"a\xff":                                  "not valid text",
		"a\nb":                                   "one line",
		"a\tb":                                   "one line",
		"a\xe2\x80\xa8b":                         "one line",
	} {
		if p := StartupCommandProblem(cmd); !strings.Contains(p, want) {
			t.Errorf("%q: %q, want %q", cmd, p, want)
		}
	}
}

func TestCleanRoleAndClamp(t *testing.T) {
	for in, want := range map[string]bool{"": true, " Worker ": true, "a.b-c_d": true, "-x": false, "two words": false, strings.Repeat("a", 33): false} {
		if _, ok := CleanRole(in); ok != want {
			t.Errorf("CleanRole(%q) ok = %v", in, ok)
		}
	}
	if r, _ := CleanRole(" Worker "); r != "worker" {
		t.Errorf("role %q", r)
	}
	for in, want := range map[int]int{0: 200, 1: 50, 50: 50, 999: 999, 5000: 1000, -4: 50} {
		if got := ClampScrollbackLines(in); got != want {
			t.Errorf("ClampScrollbackLines(%d) = %d, want %d", in, got, want)
		}
	}
}
