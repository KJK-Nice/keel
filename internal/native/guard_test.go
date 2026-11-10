package native

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The guards of docs/ui-redesign.md §6 rule 1 for keel-native: one writer, one caller. They read the source of
// every non-test .go file under internal/native, as cmd/keel/web/js/test/approve.test.mjs does for the page.

// sourceFiles are the non-test .go files under internal/native, by path relative to it ("/" separated).
func sourceFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(path)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 20 || out["keelclient/inject.go"] == "" {
		t.Fatalf("the guard sees %d files and not keelclient/inject.go: it would pass on nothing", len(out))
	}
	return out
}

func TestOnlyApprovedInjectSendsAnInjectFrame(t *testing.T) {
	frame := regexp.MustCompile(`"(inject|inject-challenge|type-check|direct)"|\\"(inject|inject-challenge|type-check|direct)\\"`)
	for name, src := range sourceFiles(t) {
		if name == "keelclient/inject.go" {
			continue
		}
		if m := frame.FindString(src); m != "" {
			t.Errorf("%s holds the frame type %s: only keelclient/inject.go may send an inject, and no file may send type-check or direct", name, m)
		}
	}
	// And it holds them: the guard is looking for the right strings.
	if src := sourceFiles(t)["keelclient/inject.go"]; !strings.Contains(src, `"inject-challenge"`) || !strings.Contains(src, `"inject"`) {
		t.Fatal("keelclient/inject.go no longer names the inject frames; update the guard")
	}
}

// calls lists every call of a method named name in file src as "file:line".
func calls(t *testing.T, file, src, name string) (out []string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
				out = append(out, file+":"+strconv.Itoa(fset.Position(c.Pos()).Line))
			}
		}
		return true
	})
	return out
}

func TestApprovedInjectHasOneCallerTheApproveDialog(t *testing.T) {
	var got []string
	for name, src := range sourceFiles(t) {
		got = append(got, calls(t, name, src, "ApprovedInject")...)
	}
	if len(got) > 1 || (len(got) == 1 && !strings.HasPrefix(got[0], "approve.go:")) {
		t.Fatalf("ApprovedInject is called from %v; it must be called once, from approve.go (the dialog's Type action)", got)
	}
}

func TestConnWriteIsOnlyTheTerminalViewsConnection(t *testing.T) {
	// Keystrokes reach a session only through the terminal view, which is given the connection
	// (terminal.Options.Conn in run.go) and writes the user's keys to it. No other file calls Write on a
	// session; prefs.go writes its own settings file.
	for name, src := range sourceFiles(t) {
		for _, c := range calls(t, name, src, "Write") {
			if name != "prefs.go" {
				t.Errorf("%s calls Write: bytes would reach a session or a file outside the terminal view", c)
			}
		}
	}
	if !strings.Contains(sourceFiles(t)["run.go"], "Conn: p.conn") {
		t.Fatal("the terminal view is no longer given the pane's connection in run.go; update the guard")
	}
}
