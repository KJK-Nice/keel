package httpapi

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
	"strconv"
	"strings"
	"testing"
)

// typedAPI is this package's non-test source, type-checked against the export data the go command builds for its
// imports, so a rule can follow what each name refers to.
type typedAPI struct {
	fset  *token.FileSet
	files map[string]*ast.File
	info  *types.Info
	pkg   *types.Package
}

func loadTypedAPI(t *testing.T) *typedAPI {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the route rules cannot type-check the package: %v", err)
	}
	out, err := exec.Command(goBin, "list", "-export", "-deps", "-json=ImportPath,Export,Dir,GoFiles", ".").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			err = errors.New(string(ee.Stderr))
		}
		t.Fatalf("go list: %v", err)
	}
	type listed struct {
		ImportPath, Export, Dir string
		GoFiles                 []string
	}
	exports := map[string]string{}
	var self listed
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p listed
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if p.ImportPath == "keel/internal/httpapi" {
			self = p
		}
	}
	if len(self.GoFiles) < 10 {
		t.Fatalf("go list found %d files in internal/httpapi", len(self.GoFiles))
	}
	a := &typedAPI{fset: token.NewFileSet(), files: map[string]*ast.File{}, info: &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{},
	}}
	var files []*ast.File
	for _, name := range self.GoFiles {
		f, err := parser.ParseFile(a.fset, filepath.Join(self.Dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		a.files[name] = f
		files = append(files, f)
	}
	imp := importer.ForCompiler(a.fset, "gc", func(path string) (io.ReadCloser, error) {
		if exports[path] == "" {
			return nil, errors.New("no export data for " + path)
		}
		return os.Open(exports[path])
	})
	if a.pkg, err = (&types.Config{Importer: imp}).Check("keel/internal/httpapi", a.fset, files, a.info); err != nil {
		t.Fatal(err)
	}
	return a
}

// route is one registration in Handler: its pattern and the function that serves it.
type route struct {
	pattern string
	handler *types.Func
}

// routes reads every registration in Handler (mux.HandleFunc, anytime, agentRoute): the pattern, a string literal, and
// the method that serves it.
func (a *typedAPI) routes(t *testing.T) (out []route) {
	t.Helper()
	for _, f := range a.files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "Handler" || fd.Recv == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok || len(c.Args) != 2 {
					return true
				}
				switch fun := c.Fun.(type) {
				case *ast.Ident:
					if fun.Name != "anytime" && fun.Name != "agentRoute" {
						return true
					}
				case *ast.SelectorExpr:
					if fun.Sel.Name != "HandleFunc" {
						return true
					}
				default:
					return true
				}
				if _, ok := c.Args[0].(*ast.Ident); ok {
					return true // inside anytime or agentRoute: the registration of its own argument
				}
				lit, ok := c.Args[0].(*ast.BasicLit)
				if !ok {
					t.Errorf("a route in Handler is registered with a pattern that is not a literal: %s", types.ExprString(c.Args[0]))
					return true
				}
				pattern, _ := strconv.Unquote(lit.Value)
				var h *types.Func
				if sel, ok := c.Args[1].(*ast.SelectorExpr); ok {
					h, _ = a.info.Uses[sel.Sel].(*types.Func)
				}
				if h == nil {
					t.Errorf("%s is served by %s, not a method of the server", pattern, types.ExprString(c.Args[1]))
				}
				out = append(out, route{pattern, h})
				return true
			})
		}
	}
	if len(out) < 80 {
		t.Fatalf("Handler registers %d routes: the reader no longer sees them", len(out))
	}
	return out
}

// decls maps each function and method of the package to its declaration.
func (a *typedAPI) decls() map[*types.Func]*ast.FuncDecl {
	out := map[*types.Func]*ast.FuncDecl{}
	for _, f := range a.files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if fn, ok := a.info.Defs[fd.Name].(*types.Func); ok {
					out[fn] = fd
				}
			}
		}
	}
	return out
}

// writers are what puts bytes in a PTY, starts or ends a session or moves control: the runtime's methods of that kind,
// and every name of the packages that type for a client.
var writers = map[string]bool{
	"Session.Write": true, "Session.Inject": true, "Session.IssueInjectTicket": true, "Session.InjectTicket": true, "Session.Resize": true,
	"Session.RequestControl": true, "Session.GrantControl": true, "Session.TakeControl": true,
	"Runtime.Spawn": true, "Runtime.Reconnect": true, "Runtime.Kill": true, "Runtime.KillAll": true,
}

// writerName is obj as a writer ("Session.Write", "inject.X", "an interface's Write"), or "".
func writerName(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	switch path := obj.Pkg().Path(); path {
	case "keel/internal/inject", "keel/internal/sessionsock":
		return path + "." + obj.Name()
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return ""
	}
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return ""
	}
	rt := recv.Type()
	if p, ok := rt.(*types.Pointer); ok {
		rt = p.Elem()
	}
	if types.IsInterface(rt) {
		// A call through an interface of Keel's own with a writer's name could land on a session.
		if strings.HasPrefix(fn.Pkg().Path(), "keel/") {
			for w := range writers {
				if strings.HasSuffix(w, "."+fn.Name()) {
					return "an interface's " + fn.Name()
				}
			}
		}
		return ""
	}
	if named, ok := rt.(*types.Named); ok && fn.Pkg().Path() == "keel/internal/runtime" && writers[named.Obj().Name()+"."+fn.Name()] {
		return named.Obj().Name() + "." + fn.Name()
	}
	return ""
}

// reach follows every function and method of this package that roots use (called or as a value), and every method of a
// type of this package they name (its methods may be called through an interface, unseen), and returns each writer
// used on the way as "writer in function". Server is the exception: every handler names it, so only the methods of it
// that are used are followed.
func (a *typedAPI) reach(roots []*types.Func) (found []string, visited map[string]bool) {
	decls := a.decls()
	methodsOf := map[*types.TypeName][]*types.Func{}
	for fn := range decls {
		if r := fn.Type().(*types.Signature).Recv(); r != nil {
			rt := r.Type()
			if p, ok := rt.(*types.Pointer); ok {
				rt = p.Elem()
			}
			if named, ok := rt.(*types.Named); ok {
				methodsOf[named.Obj()] = append(methodsOf[named.Obj()], fn)
			}
		}
	}
	visited = map[string]bool{}
	queue := slices.Clone(roots)
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		fd := decls[fn]
		if fd == nil || visited[fn.FullName()] {
			continue
		}
		visited[fn.FullName()] = true
		ast.Inspect(fd, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := a.info.Uses[id]
			if w := writerName(obj); w != "" {
				found = append(found, w+" in "+fn.FullName())
			}
			if f, ok := obj.(*types.Func); ok && f.Pkg() == a.pkg {
				queue = append(queue, f)
			}
			if tn, ok := obj.(*types.TypeName); ok && tn.Pkg() == a.pkg && tn.Name() != "Server" {
				queue = append(queue, methodsOf[tn]...)
			}
			return true
		})
	}
	return found, visited
}

// §6 rules 1 and 14: no agent route and no keel mcp route reaches a writer. Every handler Handler registers under
// /api/agent/ or /api/mcp/, the service's setup (initAgent) and the panes it is given (agentPanes) are followed through
// every function of this package they use, in any file, called or passed as a value; none uses a runtime method that
// writes, spawns, kills or moves control, a name of internal/inject or internal/sessionsock, or a Keel interface method
// with a writer's name.
func TestAgentAndMCPRoutesReachNoWriter(t *testing.T) {
	a := loadTypedAPI(t)
	var roots []*types.Func
	agentRoutes := 0
	for _, r := range a.routes(t) {
		_, path, _ := strings.Cut(r.pattern, " ")
		if strings.HasPrefix(path, "/api/agent/") || strings.HasPrefix(path, "/api/mcp/") {
			roots = append(roots, r.handler)
			agentRoutes++
		}
	}
	if agentRoutes < 40 {
		t.Fatalf("%d agent and mcp routes: the reader no longer sees them", agentRoutes)
	}
	for fn := range a.decls() {
		sig := fn.Type().(*types.Signature)
		if fn.Name() == "initAgent" {
			roots = append(roots, fn)
		}
		if r := sig.Recv(); r != nil && types.TypeString(r.Type(), nil) == "keel/internal/httpapi.agentPanes" {
			roots = append(roots, fn)
		}
	}
	found, visited := a.reach(roots)
	for _, f := range found {
		t.Errorf("an agent or mcp route reaches a writer: %s", f)
	}
	for _, want := range []string{"(*keel/internal/httpapi.Server).agentAsk", "(*keel/internal/httpapi.Server).mcpRead", "(*keel/internal/httpapi.Server).mcpPropose",
		"(*keel/internal/httpapi.Server).initAgent", "(keel/internal/httpapi.agentPanes).ReadScrollback"} {
		if !visited[want] {
			t.Errorf("the walk never reached %s: the check is out of date", want)
		}
	}
	// The same walk from the session socket finds its writers: the check can fail.
	var ws []*types.Func
	for _, r := range a.routes(t) {
		if r.pattern == "GET /ws/session" {
			ws = append(ws, r.handler)
		}
	}
	if found, _ := a.reach(ws); !slices.ContainsFunc(found, func(s string) bool { return strings.HasPrefix(s, "Session.InjectTicket in ") }) {
		t.Fatalf("the walk from /ws/session finds %v, not Session.InjectTicket: it would pass on anything", found)
	}
}
