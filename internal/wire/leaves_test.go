package wire_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The leaf packages are imported by the window and by keel serve, so each may import only the keel packages listed here.
func TestLeavesImportOnlyTheirListedKeelPackages(t *testing.T) {
	for dir, allowed := range map[string][]string{
		"wire":      nil,
		"cmdrule":   {"keel/internal/wire"},
		"limits":    nil,
		"sharelink": nil,
		"paths":     nil,
		"authtoken": {"keel/internal/paths"},
		"randid":    nil,
	} {
		t.Run(dir, func(t *testing.T) {
			files, err := filepath.Glob(filepath.Join("..", dir, "*.go"))
			if err != nil || len(files) == 0 {
				t.Fatalf("no files in %s: %v", dir, err)
			}
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
					if !strings.HasPrefix(p, "keel/") || p == "keel/internal/"+dir {
						continue
					}
					ok := false
					for _, a := range allowed {
						ok = ok || p == a
					}
					if !ok {
						t.Errorf("%s imports %s", name, p)
					}
				}
			}
		})
	}
}
