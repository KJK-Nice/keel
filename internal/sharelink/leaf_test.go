package sharelink

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sharelink is imported by the window and by keel serve: it may import only wire among keel's own packages.
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
