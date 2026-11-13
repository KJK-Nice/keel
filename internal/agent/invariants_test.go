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
		module + "/internal/share", module + "/internal/tunnel",
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
var clientPkgs = []string{module + "/internal/agent/provider/gemini", module + "/internal/agent/provider/anthropic", module + "/internal/agent/provider/jev"}

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

// clientImporters is the list of packages allowed to build a real provider
// client (Gemini, Anthropic, Jev): exactly the agent service (phase 6b),
// which builds one only for a user's Ask or ⇥ (service.Real and
// service.RealJudge, called from providerFor and judgeFor).
var clientImporters = []string{module + "/internal/agent/service"}

// Only the agent service imports the Gemini, Anthropic and Jev clients, so
// no other route, timer or UI path can construct or call them.
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
		for _, p := range clientImporters {
			allowed[p] = true
		}
		for _, p := range got {
			if !allowed[p] {
				t.Errorf("%s imports %s; only %v may", p, client, clientImporters)
			}
		}
		if len(got) != len(clientImporters) {
			t.Errorf("%s importers = %v, want exactly %v", client, got, clientImporters)
		}
	}
}

// In the agent service, the Gemini and Anthropic clients are built only in
// Real, the Jev client only in RealJudge, the test fakes only in FromEnv.
//
// One sender (§6 rules 4 and 7): a model provider (Generate) and a judge
// (Judge) are each called exactly once in the service, both in
// Service.send, which writes the agent.read entry (Auditor.Sent) before
// the call and does not call when it cannot. So nothing is sent without a
// log entry, and only redact.Safe text can be sent (the request types).
// send itself is called only for the user's Ask, the in-pane model
// suggester, Detect, judgeRisks and the history pick. Ask is reached only
// from POST /api/agent/ask, the suggesters and Detect only from the
// session socket (TestSuggestionsOnlyFromTheSessionSocket).
func TestOnlyAskCallsAProvider(t *testing.T) {
	const svc = module + "/internal/agent/service"
	seen := map[string]int{}
	senders := map[string]bool{"Service.Ask": true, "modelSuggester.Suggest": true, "Service.Detect": true, "Service.judgeRisks": true, "historySuggester.Suggest": true}
	loggers := map[string]bool{"Service.send": true, "Service.noteDetect": true, "Service.flushDetect": true}
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
				case x != nil && (x.Name == "gemini" || x.Name == "anthropic") && sel.Sel.Name == "New":
					seen[x.Name+".New"]++
					if f.pkg != svc || fn != "Real" {
						t.Errorf("%s: %s.New called in %q; only service.Real may", f.rel, x.Name, fn)
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
				case (sel.Sel.Name == "Generate" || sel.Sel.Name == "Judge") && f.pkg == svc:
					seen[sel.Sel.Name]++
					if fn != "Service.send" {
						t.Errorf("%s: a provider is called (%s) in %q; only Service.send may", f.rel, sel.Sel.Name, fn)
					}
				case sel.Sel.Name == "send" && f.pkg == svc:
					seen["send"]++
					if !senders[fn] {
						t.Errorf("%s: Service.send is called in %q; only %v may", f.rel, fn, senders)
					}
				case sel.Sel.Name == "Sent" && inAgentTree(f.pkg):
					seen["Sent"]++
					if f.pkg != svc || !loggers[fn] {
						t.Errorf("%s: the egress entry is written in %q; only Service.send (and the detection window it logs through) may", f.rel, fn)
					}
				}
				return true
			})
		}
	}
	if seen["gemini.New"] != 1 || seen["anthropic.New"] != 1 || seen["jev.New"] != 1 || seen["Generate"] != 1 || seen["Judge"] != 1 || seen["send"] != len(senders) || seen["Sent"] != len(loggers) {
		t.Errorf("calls found: %v; want gemini.New, anthropic.New and jev.New once, Generate and Judge once each (Service.send), send %d times (one in each of %v) and Sent %d times", seen, len(senders), senders, len(loggers))
	}
}

// §6 rule 7: nothing in the agent tree writes keel.log but the Auditor,
// through gate (an entry for something about to happen: refused when it
// cannot be written) and note (something that happened: owed until it is
// written).
func TestOnlyTheAuditorWritesTheLog(t *testing.T) {
	appends := 0
	for _, f := range parseModule(t) {
		if f.test || !inAgentTree(f.pkg) {
			continue
		}
		for _, decl := range f.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fn := funcName(fd)
			ast.Inspect(fd, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Append" {
					return true
				}
				appends++
				if f.pkg != module+"/internal/agent" || (fn != "Auditor.gate" && fn != "Auditor.flushLocked") {
					t.Errorf("%s: keel.log is written in %q; only Auditor.gate and Auditor.flushLocked may", f.rel, fn)
				}
				return true
			})
		}
	}
	if appends != 2 {
		t.Errorf("Append called %d times in the agent tree, want 2", appends)
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
					if id, ok := n.Type.(*ast.Ident); ok && (id.Name == "modelSuggester" || id.Name == "historySuggester") {
						if f.pkg != module+"/internal/agent/service" || fn != "Service.Suggest" {
							t.Errorf("%s: %s built in %q; only Service.Suggest may", f.rel, id.Name, fn)
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

// §6.3: snippet bodies are sent only when the user marked the snippet
// agent-usable. In the agent tree, snippets are listed only in
// agentSnippets (which keeps the agent-usable ones), and that is called
// only from historySuggester.gather.
func TestAgentReadsOnlyAgentUsableSnippets(t *testing.T) {
	lists, gathers := 0, 0
	for _, f := range parseModule(t) {
		if f.test || !inAgentTree(f.pkg) {
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
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				case *ast.Ident:
					name = fun.Name
				}
				switch name {
				case "ListSnippets", "GetSnippet":
					lists++
					if fn != "agentSnippets" {
						t.Errorf("%s: snippets read in %q; only agentSnippets may", f.rel, fn)
					}
				case "agentSnippets":
					gathers++
					if fn != "historySuggester.gather" {
						t.Errorf("%s: agentSnippets called in %q; only historySuggester.gather may", f.rel, fn)
					}
				}
				return true
			})
		}
	}
	if lists != 1 || gathers != 1 {
		t.Errorf("ListSnippets calls %d, agentSnippets calls %d; want 1 each", lists, gathers)
	}
}

// §6 rules 5 and 9: no proposal exists without its rating, its ⇧ rule and
// its placeholder scan. In the agent service a Proposal is filled in only
// by newProposal (a new one), expiredView (a saved one, shown) and
// fillProposal (a group run's step with its values in); the bytes Type
// sends are made only by Proposal.seal; and the ⇧ rule is computed only
// there, in the one gate (authorize) and for in-pane suggestions.
func TestProposalsAreSealedInOnePlace(t *testing.T) {
	const svc = module + "/internal/agent/service"
	literals := map[string]bool{"Service.newProposal": true, "Service.expiredView": true, "Service.fillProposal": true}
	shift := map[string]bool{"Proposal.seal": true, "Service.authorize": true, "codeRatings": true, "RiskCheck.run": true}
	seals := 0
	for _, f := range parseModule(t) {
		if f.test || f.pkg != svc {
			continue
		}
		for _, decl := range f.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fn := funcName(fd)
			ast.Inspect(fd, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CompositeLit:
					if id, ok := n.Type.(*ast.Ident); ok && id.Name == "Proposal" && len(n.Elts) > 0 && !literals[fn] {
						t.Errorf("%s: a Proposal is filled in by hand in %s", f.rel, fn)
					}
				case *ast.CallExpr:
					switch fun := n.Fun.(type) {
					case *ast.Ident:
						if fun.Name == "typedBytes" && fn != "Proposal.seal" {
							t.Errorf("%s: typedBytes called in %s; only Proposal.seal may", f.rel, fn)
						}
					case *ast.SelectorExpr:
						if fun.Sel.Name == "NeedsShift" && !shift[fn] {
							t.Errorf("%s: the ⇧ rule is computed in %s", f.rel, fn)
						}
						if fun.Sel.Name == "seal" {
							seals++
						}
					}
				}
				return true
			})
		}
	}
	if seals < 4 {
		t.Errorf("Proposal.seal called %d times; the check is out of date", seals)
	}
}

// funcName is a function's name, "Type.Method" for a method.
func funcName(fd *ast.FuncDecl) string {
	fn := fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		typ := fd.Recv.List[0].Type
		if star, ok := typ.(*ast.StarExpr); ok {
			typ = star.X
		}
		if id, ok := typ.(*ast.Ident); ok {
			fn = id.Name + "." + fn
		}
	}
	return fn
}

// The Gemini, Anthropic and Jev endpoints appear in no source but their
// client packages, so no other code can talk to them directly.
func TestProviderEndpointsOnlyInClients(t *testing.T) {
	root := moduleRoot(t)
	endpoints := map[string]string{
		"generativelanguage.googleapis.com": "internal/agent/provider/gemini/",
		"api.anthropic.com":                 "internal/agent/provider/anthropic/",
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
// provider/anthropic, provider/jev) may.
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
