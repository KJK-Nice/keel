package keelclient

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// docs/ui-redesign.md §6 rule 1: the only grant to write a PTY for the user is the one-time nonce keel serve issues.
// A client flag "approved" in a frame is never a grant (keel serve ignores it: httpapi's TestInjectNonceOneShotRejectsClientApprovedReplay),
// and this client never sends one.
func TestNoFrameOfAnApprovedInjectCarriesAnApprovedFlag(t *testing.T) {
	for _, opts := range [][]InjectOption{nil, {FromNotebook}} {
		c, s := dialInject(t, "ready", "driver", nonceAnswer)
		if err := c.ApprovedInject(context.Background(), "ls\n", nil, opts...); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(s.all()) < 2 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if len(s.all()) != 2 {
			t.Fatalf("frames %v", s.all())
		}
		for _, m := range s.all() {
			for k := range m {
				if strings.EqualFold(k, "approved") {
					t.Errorf("a %v frame carries %q: only keel serve's nonce grants a write", m["type"], k)
				}
			}
		}
	}
}

// No source file of the native client, in any of its packages, or of internal/wire (the frames' shapes, which both ends
// decode) names an "approved" field of a frame.
func TestNoSourceFileNamesAnApprovedFrameField(t *testing.T) {
	for _, dir := range []string{"..", filepath.Join("..", "..", "wire")} {
		n := 0
		err := filepath.WalkDir(dir, func(f string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
				return err
			}
			n++
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			if strings.Contains(string(b), `"approved"`) || strings.Contains(string(b), `approved:`) {
				t.Errorf("%s names an approved frame field: only keel serve's nonce grants a write", f)
			}
			// A field named Approved with no tag is the JSON key "Approved", which a decoder matches to "approved".
			file, err := parser.ParseFile(token.NewFileSet(), f, b, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if fd, ok := n.(*ast.Field); ok {
					for _, id := range fd.Names {
						if strings.EqualFold(id.Name, "approved") {
							t.Errorf("%s declares a field %s: only keel serve's nonce grants a write", f, id.Name)
						}
					}
				}
				return true
			})
			return nil
		})
		if err != nil || n == 0 {
			t.Fatalf("no files in %s: %v", dir, err)
		}
	}
}
