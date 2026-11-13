package cmdrule

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdrule is imported by the window, which cannot link the server's
// packages: it may import only wire among keel's own.
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
