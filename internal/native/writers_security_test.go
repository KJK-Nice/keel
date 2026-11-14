package native

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The writers of docs/ui-redesign.md §6 rule 1, checked on what each name refers to rather than how it is spelled. The
// rules in guard_test.go read call text; these type-check the window's packages, so a writer reached as a method value
// (f := conn.ApprovedInject; f(…)), a method expression, an interface of another name or a session handed to io.Writer
// (io.WriteString, fmt.Fprint, io.Copy) is seen too.

// typedPkg is one type-checked package under internal/native: its non-test files by path relative to internal/native.
type typedPkg struct {
	path  string
	fset  *token.FileSet
	files map[string]*ast.File
	info  *types.Info
	pkg   *types.Package
}

var typedCache struct {
	pkgs []*typedPkg
	err  error
	done bool
}

// typedNative type-checks internal/native and every package under it, importing everything else from the export data the
// go command builds. It never skips: a rule that could not look proves nothing.
func typedNative(t *testing.T) []*typedPkg {
	t.Helper()
	if !typedCache.done {
		typedCache.pkgs, typedCache.err = loadTyped()
		typedCache.done = true
	}
	if typedCache.err != nil {
		t.Fatalf("the writer rules cannot type-check the window: %v", typedCache.err)
	}
	return typedCache.pkgs
}

func loadTyped() ([]*typedPkg, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(goBin, "list", "-export", "-deps", "-json=ImportPath,Export,Dir,GoFiles", "./...")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, errors.New(string(ee.Stderr))
		}
		return nil, err
	}
	type listed struct{ ImportPath, Export, Dir string; GoFiles []string }
	exports := map[string]string{}
	var own []listed
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p listed
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		exports[p.ImportPath] = p.Export
		if p.ImportPath == "keel/internal/native" || strings.HasPrefix(p.ImportPath, "keel/internal/native/") {
			own = append(own, p)
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		if exports[path] == "" {
			return nil, errors.New("no export data for " + path)
		}
		return os.Open(exports[path])
	})
	var pkgs []*typedPkg
	for _, p := range own {
		tp := &typedPkg{path: p.ImportPath, fset: fset, files: map[string]*ast.File{}, info: &types.Info{
			Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
		}}
		var files []*ast.File
		for _, name := range p.GoFiles {
			full := filepath.Join(p.Dir, name)
			f, err := parser.ParseFile(fset, full, nil, 0)
			if err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(wd, full)
			tp.files[filepath.ToSlash(rel)] = f
			files = append(files, f)
		}
		tp.pkg, err = (&types.Config{Importer: imp}).Check(p.ImportPath, fset, files, tp.info)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, tp)
	}
	if len(pkgs) < 3 {
		return nil, errors.New("fewer than three packages under internal/native")
	}
	return pkgs, nil
}

// funcAt is the function declaration that holds pos in f, by name ("" outside one).
func funcAt(f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Pos() <= pos && pos < fn.End() {
			return fn.Name.Name
		}
	}
	return ""
}

// calledIdents are the identifiers that name the function of a call: f in f(…) and Sel in x.Sel(…).
func calledIdents(f *ast.File) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			switch fun := ast.Unparen(c.Fun).(type) {
			case *ast.Ident:
				out[fun] = true
			case *ast.SelectorExpr:
				out[fun.Sel] = true
			}
		}
		return true
	})
	return out
}

// writerSites are the places each writer, and each step of the window's paths to one, may be used, as "file:function",
// once each. Every use of a function or method of that name declared under internal/native, on any type (keelclient's
// Conn, the window's session interface, or an interface declared anywhere else), counts.
var writerSites = map[string][]string{
	"ApprovedInject": {"approve.go:approveType"},
	"DirectType":     {"typenow.go:typeNow"},
	"SuggestInsert":  {"suggestpane.go:insertSuggestion"},
	"RestoreWords":   {"suggestpane.go:restoreWords"},
	// The paths to them: the Type buttons, typeNow's three entry points, the card's keys, sync input's wrapper.
	"approveType":      {"approveview.go:pressType"},
	"pressType":        {"approveview.go:approveView", "groupapproveview.go:groupApproveView"},
	"typeNow":          {"agent.go:typeAgentProposal", "notebooktype.go:typeNotebookFence", "snippets.go:typeSnippet"},
	"insertSuggestion": {"suggestpane.go:suggestInsert"},
	"restoreWords":     {"suggestpane.go:suggestMachine"},
	"newSyncConn":      {"run.go:liveBodyT"},
	"fanOut":           {"syncinput.go:Write"},
}

// usesOfWriters lists, for each name, every use of a function or method of that name declared under internal/native
// (or, outside it, in package x: the self-check) in pkgs as "file:function", sorted, with " (not a call)" after a use
// that is not the function of a call (a method value or a method expression).
func usesOfWriters(pkgs []*typedPkg, names map[string][]string) map[string][]string {
	got := map[string][]string{}
	for _, p := range pkgs {
		for name, f := range p.files {
			called := calledIdents(f)
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				fn, ok := p.info.Uses[id].(*types.Func)
				if !ok || fn.Pkg() == nil {
					return true
				}
				if _, tracked := names[fn.Name()]; !tracked {
					return true
				}
				if path := fn.Pkg().Path(); path != "x" && path != "keel/internal/native" && !strings.HasPrefix(path, "keel/internal/native/") {
					return true // a name of the same spelling from elsewhere
				}
				site := name + ":" + funcAt(f, id.Pos())
				if !called[id] {
					site += " (not a call)"
				}
				got[fn.Name()] = append(got[fn.Name()], site)
				return true
			})
		}
	}
	for _, s := range got {
		slices.Sort(s)
	}
	return got
}

// Each writer, and each step of the paths to one, is used once per site, as a call, at its sites only: a method value, a
// method expression or a call through an interface declared anywhere fails here, whatever the receiver's name.
func TestEachWriterIsUsedOnceAsACallAtItsSite(t *testing.T) {
	got := usesOfWriters(typedNative(t), writerSites)
	for name, sites := range writerSites {
		if !slices.Equal(got[name], sites) {
			t.Errorf("%s is used at %v; it must be used once, called, at each of %v", name, got[name], sites)
		}
	}
}

// The checker above finds a writer taken as a value and a call through an interface declared elsewhere: it is run here on
// a package that does both, so it cannot pass by finding nothing.
func TestTheWriterCheckSeesAMethodValueAndAForeignInterface(t *testing.T) {
	src := `package x
type conn struct{}
func (conn) ApprovedInject(string) error { return nil }
type typer interface{ ApprovedInject(string) error }
func approveType(c conn) { _ = c.ApprovedInject("ls") }
func viaValue(c conn) { f := c.ApprovedInject; _ = f("rm") }
func viaExpr(c conn) { g := conn.ApprovedInject; _ = g(c, "rm") }
func viaInterface(c conn) { var t typer = c; _ = t.ApprovedInject("rm") }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &typedPkg{fset: fset, files: map[string]*ast.File{"x.go": f}, info: &types.Info{Uses: map[*ast.Ident]types.Object{}}}
	if _, err := (&types.Config{}).Check("x", fset, []*ast.File{f}, p.info); err != nil {
		t.Fatal(err)
	}
	got := usesOfWriters([]*typedPkg{p}, map[string][]string{"ApprovedInject": nil})["ApprovedInject"]
	want := []string{"x.go:approveType", "x.go:viaExpr (not a call)", "x.go:viaInterface", "x.go:viaValue (not a call)"}
	if !slices.Equal(got, want) {
		t.Fatalf("the check saw %v, want %v", got, want)
	}
}

// conversions lists every place in f where an expression of a type for which from reports true is given to a slot of
// an interface type for which to reports true: a call's argument, an assignment, a variable's value, a composite
// literal's element, a return value, a channel send or an explicit conversion. Each is "file:function: expr -> type".
func conversions(p *typedPkg, file string, f *ast.File, from func(types.Type) bool, to func(types.Type) bool) (out []string) {
	typeOf := func(e ast.Expr) types.Type {
		if tv, ok := p.info.Types[e]; ok {
			return tv.Type
		}
		return nil
	}
	check := func(e ast.Expr, slot types.Type) {
		if e == nil || slot == nil {
			return
		}
		if src := typeOf(e); src != nil && from(src) && types.IsInterface(slot) && to(slot) {
			out = append(out, file+":"+funcAt(f, e.Pos())+": "+types.ExprString(e)+" -> "+slot.String())
		}
	}
	var results []*types.Tuple
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if fn, ok := p.info.Defs[x.Name].(*types.Func); ok {
				results = append(results[:0], fn.Type().(*types.Signature).Results())
			}
		case *ast.FuncLit:
			if sig, ok := typeOf(x).(*types.Signature); ok {
				results = append(results, sig.Results())
			}
		case *ast.CallExpr:
			if tv, ok := p.info.Types[x.Fun]; ok && tv.IsType() {
				if len(x.Args) == 1 {
					check(x.Args[0], tv.Type)
				}
				return true
			}
			sig, ok := typeOf(x.Fun).(*types.Signature)
			if !ok {
				return true
			}
			for i, a := range x.Args {
				var slot types.Type
				switch {
				case sig.Variadic() && i >= sig.Params().Len()-1 && !x.Ellipsis.IsValid():
					slot = sig.Params().At(sig.Params().Len() - 1).Type().(*types.Slice).Elem()
				case i < sig.Params().Len():
					slot = sig.Params().At(i).Type()
				}
				check(a, slot)
			}
		case *ast.AssignStmt:
			if len(x.Lhs) == len(x.Rhs) {
				for i := range x.Lhs {
					check(x.Rhs[i], typeOf(x.Lhs[i]))
				}
			}
		case *ast.ValueSpec:
			if x.Type != nil {
				for _, v := range x.Values {
					check(v, typeOf(x.Type))
				}
			}
		case *ast.CompositeLit:
			lit := typeOf(x)
			if lit == nil {
				return true
			}
			switch u := lit.Underlying().(type) {
			case *types.Struct:
				for i, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok {
							for j := 0; j < u.NumFields(); j++ {
								if u.Field(j).Name() == k.Name {
									check(kv.Value, u.Field(j).Type())
								}
							}
						}
					} else if i < u.NumFields() {
						check(e, u.Field(i).Type())
					}
				}
			case *types.Slice, *types.Array, *types.Map:
				var elem types.Type
				switch u := u.(type) {
				case *types.Slice:
					elem = u.Elem()
				case *types.Array:
					elem = u.Elem()
				case *types.Map:
					elem = u.Elem()
				}
				for _, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						e = kv.Value
					}
					check(e, elem)
				}
			}
		case *ast.ReturnStmt:
			if n := len(results); n > 0 && results[n-1] != nil && results[n-1].Len() == len(x.Results) {
				for i, r := range x.Results {
					check(r, results[n-1].At(i).Type())
				}
			}
		case *ast.SendStmt:
			if ch, ok := typeOf(x.Chan).Underlying().(*types.Chan); ok {
				check(x.Value, ch.Elem())
			}
		}
		return true
	})
	return out
}

// hasWrite reports a type whose method set has Write([]byte) (int, error), the method an io.Writer is called through.
func hasWrite(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Write")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Type().(*types.Signature)
	return sig.Params().Len() == 1 && sig.Results().Len() == 2 && types.Identical(sig.Params().At(0).Type(), types.NewSlice(types.Typ[types.Byte]))
}

// A session reaches a writer that is not the window's own session interface in one place: run.go hands the pane's
// wrapped connection to the terminal view (Options.Conn). Nothing else gives a session, a syncConn or keelclient's Conn
// to an io.Writer (io.WriteString, fmt.Fprint, io.Copy, bufio.NewWriter, …), where its Write would be called unseen.
func TestASessionBecomesAWriterOnlyAsTheTerminalViewsConnection(t *testing.T) {
	var native *typedPkg
	for _, p := range typedNative(t) {
		if p.path == "keel/internal/native" {
			native = p
		}
	}
	if native == nil {
		t.Fatal("internal/native was not type-checked")
	}
	sessionIface := native.pkg.Scope().Lookup("session").Type().Underlying().(*types.Interface)
	syncConn := types.NewPointer(native.pkg.Scope().Lookup("syncConn").Type())
	isSession := func(t types.Type) bool {
		if types.IsInterface(t) {
			return types.Identical(t.Underlying(), sessionIface)
		}
		return types.Implements(t, sessionIface) || types.Identical(t, syncConn)
	}
	foreignWriter := func(t types.Type) bool { return hasWrite(t) && !types.Identical(t.Underlying(), sessionIface) }
	var got []string
	for name, f := range native.files {
		got = append(got, conversions(native, name, f, isSession, foreignWriter)...)
	}
	if want := []string{"run.go:liveBodyT: newSyncConn(p) -> io.ReadWriteCloser"}; !slices.Equal(got, want) {
		t.Errorf("a session is given to a writer at %v; only %v may", got, want)
	}
	// A session's own Write is used only by syncinput.go, called: the wrapper's Write and the fan-out.
	var writes []string
	for name, f := range native.files {
		called := calledIdents(f)
		for sel, s := range native.info.Selections {
			if sel.Sel.Name != "Write" || !isSession(s.Recv()) || sel.Pos() < f.Pos() || sel.Pos() >= f.End() {
				continue
			}
			site := name + ":" + funcAt(f, sel.Pos())
			if !called[sel.Sel] {
				site += " (not a call)"
			}
			writes = append(writes, site)
		}
	}
	slices.Sort(writes)
	if want := []string{"syncinput.go:Write", "syncinput.go:fanOut"}; !slices.Equal(writes, want) {
		t.Errorf("a session's Write is used at %v; only %v may", writes, want)
	}
	// And the rule can fail: a session handed to io.WriteString is seen.
	src := `package x
import "io"
type sess interface{ io.ReadWriteCloser; Snapshot() int }
func bad(s sess) { _, _ = io.WriteString(s, "rm -rf /\n") }
func fine(s sess) { var t sess = s; _ = t }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &typedPkg{fset: fset, info: &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}}}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("x", fset, []*ast.File{f}, p.info)
	if err != nil {
		t.Fatal(err)
	}
	sess := pkg.Scope().Lookup("sess").Type()
	seen := conversions(p, "x.go", f, func(t types.Type) bool { return types.Identical(t, sess) }, func(t types.Type) bool { return hasWrite(t) && !types.Identical(t, sess) })
	if !slices.Equal(seen, []string{"x.go:bad: s -> io.Writer"}) {
		t.Fatalf("the conversion check saw %v in a file that hands a session to io.WriteString", seen)
	}
}

// typingWire are the names of internal/wire that make or mark a frame that can lead to bytes in a PTY: the frame types
// of the challenge, the inject and the suggestion questions and answers, the parts of an inject, and the fields of a
// wire.Control that carry them. Under internal/native only keelclient/inject.go uses them.
var typingWire = map[string]bool{
	"TypeInjectChallenge": true, "TypeInject": true, "TypeAgentDetect": true, "TypeAgentSuggest": true, "TypeAgentSuggestRisk": true,
	"TypeAgentType": true, "TypeAgentInsert": true, "TypeTypeDirect": true,
	"AgentType": true, "AgentInsert": true, "DirectType": true,
	"Control.Agent": true, "Control.Command": true, "Control.Direct": true, "Control.Line": true, "Control.Nonce": true,
	"Control.Notebook": true, "Control.Seq": true, "Control.Suggestion": true,
}

// wireName is obj's name in typingWire's terms: "Name" for a package-level object of internal/wire, "Control.Field" for
// a field of wire.Control, "" for anything else.
func wireName(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "keel/internal/wire" {
		return ""
	}
	if v, ok := obj.(*types.Var); ok && v.IsField() {
		ctl := obj.Pkg().Scope().Lookup("Control").Type().Underlying().(*types.Struct)
		for i := 0; i < ctl.NumFields(); i++ {
			if ctl.Field(i) == v {
				return "Control." + v.Name()
			}
		}
		return ""
	}
	if obj.Parent() == obj.Pkg().Scope() {
		return obj.Name()
	}
	return ""
}

// The frames that can lead to bytes in a PTY are built from internal/wire's names in keelclient/inject.go alone, however
// they are spelled (a constant, a field set by a key or by assignment, a part's type): the string and regexp rules of
// guard_test.go cannot see a wire.TypeAgentSuggest or a ctl.Nonce.
func TestOnlyInjectGoUsesTheTypingNamesOfTheWire(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range typedNative(t) {
		for name, f := range p.files {
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				w := wireName(p.info.Uses[id])
				if !typingWire[w] {
					return true
				}
				if name != "keelclient/inject.go" {
					t.Errorf("%s:%d (%s) uses wire.%s: only keelclient/inject.go builds or reads a frame that can lead to bytes in a PTY", name, p.fset.Position(id.Pos()).Line, funcAt(f, id.Pos()), w)
				}
				seen[w] = true
				return true
			})
		}
	}
	for w := range typingWire {
		if !seen[w] {
			t.Errorf("keelclient/inject.go no longer uses wire.%s; update typingWire", w)
		}
	}
}
