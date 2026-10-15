package agent_test

// Source checks for the agent invariants (docs/ui-redesign.md §6) and for
// "nothing leaves the machine in phase 6a". They parse the module's Go
// files with go/parser (no extra dependency) and look at imports, calls
// and field reads. Test files are skipped: tests may build a client
// against a loopback httptest server.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/runtime"
)

const module = "keel"

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

type goFile struct {
	rel  string // slash path from the module root
	pkg  string // import path of its directory
	ast  *ast.File
	src  []byte
	test bool
}

func parseModule(t *testing.T) []goFile {
	t.Helper()
	root := moduleRoot(t)
	var out []goFile
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "bin" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		pkg := module
		if dir := filepath.ToSlash(filepath.Dir(rel)); dir != "." {
			pkg = module + "/" + dir
		}
		out = append(out, goFile{rel: rel, pkg: pkg, ast: f, src: src, test: strings.HasSuffix(name, "_test.go")})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func imports(f goFile) []string {
	var out []string
	for _, im := range f.ast.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		out = append(out, p)
	}
	return out
}

func inAgentTree(pkg string) bool {
	return pkg == module+"/internal/agent" || strings.HasPrefix(pkg, module+"/internal/agent/")
}

// §6.1: the agent never writes to a PTY. No package under internal/agent
// can reach a PTY writer, directly or through any Keel package it imports.
func TestAgentCannotReachPTYWriters(t *testing.T) {
	files := parseModule(t)
	importsOf := map[string]map[string]bool{}
	for _, f := range files {
		if f.test {
			continue
		}
		if importsOf[f.pkg] == nil {
			importsOf[f.pkg] = map[string]bool{}
		}
		for _, p := range imports(f) {
			importsOf[f.pkg][p] = true
		}
	}
	forbidden := []string{
		module + "/internal/runtime", module + "/internal/session", module + "/internal/inject",
		module + "/internal/share", module + "/internal/tunnel", module + "/internal/termlife",
		module + "/internal/httpapi", module + "/internal/app", module + "/internal/signal",
		"github.com/creack/pty", "golang.org/x/crypto/ssh", "os/exec", "syscall",
	}
	for pkg := range importsOf {
		if !inAgentTree(pkg) {
			continue
		}
		// Walk the Keel packages it depends on.
		seen := map[string]bool{}
		stack := []string{pkg}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[cur] {
				continue
			}
			seen[cur] = true
			for dep := range importsOf[cur] {
				for _, bad := range forbidden {
					if dep == bad || strings.HasPrefix(dep, bad+"/") {
						t.Errorf("%s reaches %s (via %s)", pkg, dep, cur)
					}
				}
				if strings.HasPrefix(dep, module+"/") {
					stack = append(stack, dep)
				}
			}
		}
	}
}

// §6.1, belt and braces: no call in the agent tree is named like a PTY
// write path.
func TestAgentCallsNoWriteMethods(t *testing.T) {
	banned := map[string]bool{"Write": true, "Inject": true, "InjectTicket": true, "Keystroke": true, "IssueInjectTicket": true, "Spawn": true, "Resize": true}
	for _, f := range parseModule(t) {
		if f.test || !inAgentTree(f.pkg) {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && banned[sel.Sel.Name] {
				t.Errorf("%s calls .%s", f.rel, sel.Sel.Name)
			}
			return true
		})
	}
}

// §6.3: the agent never sees vault contents. In the agent tree, no code
// reads a secret field or calls a store method that returns secrets. The
// exceptions: KnownSecretValues, called once, in NewRedactor, whose result
// goes only into redact.Options.Known; and AgentKey, called twice in the
// agent service, in providerFor (the model provider's key, which goes only
// into the x-goog-api-key header) and in judgeFor (the detector's key,
// which goes only into Jev's Authorization header), never into a request
// body.
func TestAgentReadsNoVaultSecrets(t *testing.T) {
	secretFields := map[string]bool{"Password": true, "PrivateKey": true, "Passphrase": true, "Token": true}
	secretCalls := map[string]bool{"GetHost": true, "HostForConnect": true, "GetIdentity": true, "ListIdentities": true, "DuplicateHost": true}
	known, keys := 0, 0
	for _, f := range parseModule(t) {
		if f.test || !inAgentTree(f.pkg) || isClientPkg(f.pkg) {
			continue
		}
		for _, decl := range f.ast.Decls {
			fn := ""
			if fd, ok := decl.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name := sel.Sel.Name
				if secretFields[name] {
					t.Errorf("%s reads .%s", f.rel, name)
				}
				if secretCalls[name] {
					t.Errorf("%s uses %s", f.rel, name)
				}
				if name == "AgentKey" {
					keys++
					if f.pkg != module+"/internal/agent/service" || (fn != "providerFor" && fn != "judgeFor") {
						t.Errorf("%s: AgentKey used in %q, only service.providerFor and service.judgeFor may", f.rel, fn)
					}
				}
				if name == "KnownSecretValues" {
					known++
					if fn != "NewRedactor" {
						t.Errorf("%s: KnownSecretValues used in %q, only NewRedactor may", f.rel, fn)
					}
				}
				return true
			})
		}
	}
	if known != 1 {
		t.Errorf("KnownSecretValues used %d times in the agent tree, want exactly 1 (NewRedactor)", known)
	}
	if keys != 2 {
		t.Errorf("AgentKey used %d times in the agent tree, want exactly 2 (service.providerFor, service.judgeFor)", keys)
	}
}

// clientPkgs are the provider clients: the only packages in the agent tree
// that open a network connection or hold a key.
var clientPkgs = []string{module + "/internal/agent/provider/gemini", module + "/internal/agent/provider/jev"}

func isClientPkg(pkg string) bool {
	for _, p := range clientPkgs {
		if pkg == p {
			return true
		}
	}
	return false
}

// The agent's role names must match the runtime's, which the caller maps.
func TestRolesMatchRuntime(t *testing.T) {
	if string(agent.RoleOwner) != string(runtime.RoleOwner) || string(agent.RoleDriver) != string(runtime.RoleDriver) || string(agent.RoleFollower) != string(runtime.RoleFollower) {
		t.Fatal("agent roles drifted from runtime roles")
	}
}

// geminiImporters is the list of packages allowed to build the real Gemini
// client: exactly the agent service (phase 6b), which builds it only for a
// user's Ask (service.Real, called from providerFor).
var geminiImporters = []string{module + "/internal/agent/service"}

// Only the agent service imports the Gemini and Jev clients, so no other
// route, timer or UI path can construct or call them.
func TestNoRuntimePathBuildsAProviderClient(t *testing.T) {
	for _, client := range clientPkgs {
		var got []string
		for _, f := range parseModule(t) {
			if f.test || f.pkg == client {
				continue
			}
			for _, p := range imports(f) {
				if p == client {
					got = append(got, f.pkg)
				}
			}
		}
		sort.Strings(got)
		allowed := map[string]bool{}
		for _, p := range geminiImporters {
			allowed[p] = true
		}
		for _, p := range got {
			if !allowed[p] {
				t.Errorf("%s imports %s; only %v may", p, client, geminiImporters)
			}
		}
		if len(got) != len(geminiImporters) {
			t.Errorf("%s importers = %v, want exactly %v", client, got, geminiImporters)
		}
	}
}

// In the agent service, the Gemini client is built only in Real, the Jev
// client only in RealJudge, the test fakes only in FromEnv; a model
// provider is called (Generate) only from Ask, the in-pane model
// suggester and the key-scrubbing wrapper; and a judge (Jev) only from
// Detect, judgeRisks and its scrubbing wrapper. Ask is reached only from
// POST /api/agent/ask, the suggester and Detect only from the session
// socket (TestSuggestionsOnlyFromTheSessionSocket).
func TestOnlyAskCallsAProvider(t *testing.T) {
	const svc = module + "/internal/agent/service"
	seen := map[string]int{}
	for _, f := range parseModule(t) {
		if f.test {
			continue
		}
		for _, decl := range f.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			fn := ""
			if ok {
				fn = fd.Name.Name
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					typ := fd.Recv.List[0].Type
					if star, ok := typ.(*ast.StarExpr); ok {
						typ = star.X
					}
					if id, ok := typ.(*ast.Ident); ok {
						fn = id.Name + "." + fn
					}
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				x, _ := sel.X.(*ast.Ident)
				switch {
				case x != nil && x.Name == "gemini" && sel.Sel.Name == "New":
					seen["gemini.New"]++
					if f.pkg != svc || fn != "Real" {
						t.Errorf("%s: gemini.New called in %q; only service.Real may", f.rel, fn)
					}
				case x != nil && x.Name == "jev" && sel.Sel.Name == "New":
					seen["jev.New"]++
					if f.pkg != svc || fn != "RealJudge" {
						t.Errorf("%s: jev.New called in %q; only service.RealJudge may", f.rel, fn)
					}
				case x != nil && x.Name == "providertest" && (sel.Sel.Name == "NewScripted" || sel.Sel.Name == "NewScriptedJudge"):
					if f.pkg != svc || fn != "FromEnv" {
						t.Errorf("%s: the test provider is built in %q; only service.FromEnv may", f.rel, fn)
					}
				case sel.Sel.Name == "Generate" && f.pkg == svc:
					seen["Generate"]++
					if fn != "Service.Ask" && fn != "scrubbing.Generate" && fn != "modelSuggester.Suggest" {
						t.Errorf("%s: a provider is called in %q; only Service.Ask and modelSuggester.Suggest may", f.rel, fn)
					}
				case sel.Sel.Name == "Judge" && f.pkg == svc:
					seen["Judge"]++
					if fn != "Service.Detect" && fn != "Service.judgeRisks" && fn != "scrubbingJudge.Judge" {
						t.Errorf("%s: a judge is called in %q; only Service.Detect and Service.judgeRisks may", f.rel, fn)
					}
				}
				return true
			})
		}
	}
	if seen["gemini.New"] != 1 || seen["jev.New"] != 1 || seen["Generate"] != 3 || seen["Judge"] != 3 {
		t.Errorf("calls found: %v; want gemini.New and jev.New once, Generate 3 times (Ask, modelSuggester, scrubbing) and Judge 3 times (Detect, judgeRisks, scrubbing)", seen)
	}
}

// §6.1, §6.6: in-pane suggestions start only from the user's own session
// socket (where keel serve knows the client's role), and the model
// suggester is built only inside Service.Suggest (the user's ⇥). Outside
// tests, Detect, Suggest and AuthorizeInsert are called only from
// internal/httpapi/agentsuggest.go.
func TestSuggestionsOnlyFromTheSessionSocket(t *testing.T) {
	calls := map[string]int{}
	for _, f := range parseModule(t) {
		if f.test {
			continue
		}
		for _, decl := range f.ast.Decls {
			fd, _ := decl.(*ast.FuncDecl)
			fn := ""
			if fd != nil {
				fn = fd.Name.Name
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					typ := fd.Recv.List[0].Type
					if star, ok := typ.(*ast.StarExpr); ok {
						typ = star.X
					}
					if id, ok := typ.(*ast.Ident); ok {
						fn = id.Name + "." + fn
					}
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CompositeLit:
					if id, ok := n.Type.(*ast.Ident); ok && id.Name == "modelSuggester" {
						if f.pkg != module+"/internal/agent/service" || fn != "Service.Suggest" {
							t.Errorf("%s: modelSuggester built in %q; only Service.Suggest may", f.rel, fn)
						}
					}
				case *ast.CallExpr:
					sel, ok := n.Fun.(*ast.SelectorExpr)
					if !ok || f.pkg == module+"/internal/agent/service" {
						return true
					}
					switch sel.Sel.Name {
					case "Detect", "Suggest", "AuthorizeInsert":
						// Only calls on the agent service (s.agent / c.srv.agent).
						if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "agent" {
							calls[sel.Sel.Name]++
							if f.rel != "internal/httpapi/agentsuggest.go" {
								t.Errorf("%s: agent.%s called in %q; only internal/httpapi/agentsuggest.go may", f.rel, sel.Sel.Name, fn)
							}
						}
					}
				}
				return true
			})
		}
	}
	for _, name := range []string{"Detect", "Suggest", "AuthorizeInsert"} {
		if calls[name] != 1 {
			t.Errorf("agent.%s called %d times outside the service, want 1 (agentsuggest.go)", name, calls[name])
		}
	}
}

// The Gemini and Jev endpoints appear in no source but their client
// packages, so no other code can talk to them directly.
func TestProviderEndpointsOnlyInClients(t *testing.T) {
	root := moduleRoot(t)
	endpoints := map[string]string{
		"generativelanguage.googleapis.com": "internal/agent/provider/gemini/",
		"api.typesafe.ai":                   "internal/agent/provider/jev/",
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if path != root && (strings.HasPrefix(n, ".") || n == "node_modules" || n == "bin" || n == "docs") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".js" && ext != ".mjs" && ext != ".templ" && ext != ".html" && ext != ".ts" {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for host, home := range endpoints {
			if !strings.HasPrefix(rel, home) && strings.Contains(string(b), host) {
				t.Errorf("%s mentions %s", rel, host)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The agent reads panes through runtime + Redactor (Auditor.ReadPane),
// never through the HTTP scrollback route, which keeps a lighter mask. No
// string literal in the agent tree names that route or any loopback API.
func TestAgentNeverUsesHTTPScrollback(t *testing.T) {
	for _, f := range parseModule(t) {
		if f.test || !inAgentTree(f.pkg) {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, _ := strconv.Unquote(lit.Value)
			for _, bad := range []string{"/scrollback", "/api/sessions", "/api/", "/ws/session", "://127.0.0.1", "://localhost", "://[::1]"} {
				if strings.Contains(v, bad) {
					t.Errorf("%s: string %q names a loopback route (%s)", f.rel, v, bad)
				}
			}
			return true
		})
	}
}

// The agent core, redaction and provider interface packages open no
// network connection; only a client package (provider/gemini,
// provider/jev) may.
func TestAgentCoreHasNoNetwork(t *testing.T) {
	net := map[string]bool{"net": true, "net/http": true, "net/rpc": true, "net/smtp": true, "crypto/tls": true, "net/http/httputil": true}
	for _, f := range parseModule(t) {
		if f.test || !inAgentTree(f.pkg) || isClientPkg(f.pkg) {
			continue
		}
		for _, p := range imports(f) {
			if net[p] {
				t.Errorf("%s imports %s", f.rel, p)
			}
		}
	}
}
