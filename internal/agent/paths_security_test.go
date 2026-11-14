package agent_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// valueUses lists every use in f of a name in names that is not the function of a call: a method value
// (f := s.noteContext), a method expression or a function passed as a value. The path rules of the *_invariants_test.go
// files count calls by name, so such a use would reach a gated path without being counted; they refuse it. A declaration
// of the name (a func, a method or an interface method) is not a use.
func valueUses(f *ast.File, names map[string]bool) (out []string) {
	called, declared := map[*ast.Ident]bool{}, map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			switch fun := ast.Unparen(x.Fun).(type) {
			case *ast.Ident:
				called[fun] = true
			case *ast.SelectorExpr:
				called[fun.Sel] = true
			case *ast.IndexExpr: // a generic function's call
				if id, ok := fun.X.(*ast.Ident); ok {
					called[id] = true
				}
			}
		case *ast.FuncDecl:
			declared[x.Name] = true
		case *ast.Field:
			for _, id := range x.Names {
				declared[id] = true
			}
		case *ast.KeyValueExpr: // a struct literal's field name
			if id, ok := x.Key.(*ast.Ident); ok {
				declared[id] = true
			}
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident) // a selector's Sel is visited as an Ident too
		if !ok {
			return true
		}
		if names[id.Name] && !called[id] && !declared[id] {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// noValueUses fails for every use of a key of allowed, in the files the rule reads, that is not a call.
func noValueUses(t *testing.T, allowed map[string][]string, read func(goFile) bool) {
	t.Helper()
	names := map[string]bool{}
	for n := range allowed {
		names[n] = true
	}
	for _, f := range parseModule(t) {
		if f.test || !read(f) {
			continue
		}
		for _, n := range valueUses(f.ast, names) {
			t.Errorf("%s uses %s as a value, not a call: the path rule counts calls, so this would reach it unseen", f.rel, n)
		}
	}
}

// The value check sees what a call count misses: a method value, a method expression and a function passed as a value.
func TestTheValueCheckSeesAMethodValue(t *testing.T) {
	src := `package x
type S struct{}
func (s *S) noteContext() {}
func noteContextFree() {}
func (s *S) Ask() { s.noteContext() }
func (s *S) viaValue() { f := s.noteContext; f() }
func viaExpr(s *S) { g := (*S).noteContext; g(s) }
func viaArg(run func()) { run() }
func passes() { viaArg(noteContextFree) }
type T interface{ noteContext() }
`
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := valueUses(f, map[string]bool{"noteContext": true, "noteContextFree": true})
	if len(got) != 3 {
		t.Fatalf("the value check saw %v, want noteContext twice and noteContextFree once", got)
	}
}
