package runvars

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runvars is imported by the window and by keel serve: it may import no keel package but wire.
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

func TestOneNameSyntaxAndTheDeliberateDifference(t *testing.T) {
	// A name can be declared only in lower case; a reference may be in upper case so that {{FOO}} is refused as
	// missing rather than typed as text; DeclaredRefRe is the lower-case-only form.
	if NameRe.MatchString("FOO") || !refRe.MatchString("{{FOO}}") || DeclaredRefRe.MatchString("{{FOO}}") {
		t.Fatal("the name and reference syntaxes drifted")
	}
	if !DeclaredRefRe.MatchString("{{a_1|q}}") || !NameRe.MatchString("a_1") {
		t.Fatal("a lower-case name is both")
	}
}
