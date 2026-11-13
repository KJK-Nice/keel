package limits

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// limits is imported by the window and by keel serve: it may import no keel package but wire.
func TestStaysALeaf(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, "keel/") && p != "keel/internal/wire" {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
}

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
