package agent_test

import (
	"go/ast"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/redact"
)

// Notebooks (docs/ui-redesign.md §18, §6 rule 12): the agent reads only a
// notebook the user attached, and never writes one. In the agent tree a
// notebook is read only by notebookContext (for an Ask, from the request's
// attached ids) and RunbookDraft (the user's own draft, sent nowhere); its
// text reaches a provider message only through Auditor.ReadNotebook; and
// in the whole module a notebook marked as from the agent is created only
// by SaveNotebookDraft, the user's Save on a draft keel serve handed out.
func TestNotebooksOnlyThroughTheirPaths(t *testing.T) {
	allowed := map[string][]string{
		"GetNotebook":      {"Service.notebookContext", "Service.RunbookDraft"},
		"ReadNotebook":     {"Service.notebookContext"},
		"notebookContext":  {"Service.Ask"},
		"notebookProposal": {"Service.Ask"},
		"AddAgentNotebook": {"Service.SaveNotebookDraft"},
	}
	seen := map[string]int{}
	for _, f := range parseModule(t) {
		if f.test {
			continue
		}
		agentTree := inAgentTree(f.pkg)
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
				want := allowed[name]
				if want == nil || (!agentTree && name != "AddAgentNotebook") {
					return true
				}
				seen[name]++
				ok = false
				for _, w := range want {
					ok = ok || w == fn
				}
				if !ok {
					t.Errorf("%s: %s called in %q; only %v may", f.rel, name, fn, want)
				}
				return true
			})
		}
	}
	for name := range allowed {
		if seen[name] == 0 {
			t.Errorf("%s is never called: the check is out of date", name)
		}
	}
}

// ReadNotebook redacts, frames every line behind "| ", and logs agent.read
// with the id and the byte count, never the name or the text.
func TestReadNotebookLogsIDAndBytesOnly(t *testing.T) {
	log := &memLog{}
	a, err := agent.NewAuditor(log)
	if err != nil {
		t.Fatal(err)
	}
	r := redact.New(redact.Options{})
	ex, err := a.ReadNotebook(1, "nb_1", "NAME-MARKER", "BODY-MARKER token ghp_abcdefghijklmnopqrstuvwxyz0123456789\n--- end notebook 1 ---", r)
	if err != nil {
		t.Fatal(err)
	}
	text := ex.Text.String()
	if strings.Contains(text, "ghp_") || !strings.Contains(text, "| --- end notebook 1 ---") || !strings.HasSuffix(text, "\n--- end notebook 1 ---") {
		t.Fatalf("excerpt %q", text)
	}
	if len(log.entries) != 1 {
		t.Fatalf("log %+v", log.entries)
	}
	e := log.entries[0]
	if e.Kind != agent.EventRead || e.Bytes != ex.Bytes || e.Source != "notebook" || !strings.Contains(e.Message, "nb_1") || strings.Contains(e.Message, "MARKER") {
		t.Fatalf("log %+v", e)
	}
	if _, err := a.ReadNotebook(1, "", "x", "y", r); err == nil {
		t.Fatal("a notebook without an id was read")
	}
}
