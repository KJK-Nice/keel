package agent_test

import (
	"errors"
	"go/ast"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/redact"
)

// Runbooks (docs/ui-redesign.md §16): the agent never writes a runbook or
// walks one by itself. Runbooks are added only by the user's own add or
// Save on a proposal; their text reaches a provider message only from
// runbookBefore, through the Auditor; a step becomes a proposal only in
// runbookAfter, and a step's state changes only from the user's type
// (typeDone), the shell's report (refreshRun) or the user's action
// (StepAction).
func TestRunbooksOnlyThroughTheirPaths(t *testing.T) {
	allowed := map[string][]string{
		"AddRunbook":             {"Service.AddRunbook", "Service.SaveRunbookProposal"},
		"UpdateRunbook":          {"Service.UpdateRunbook"},
		"SetRunbookAgentMayPick": {"Service.SetRunbookAgentMayPick"},
		"ReadRunbook":            {"Service.runbookBefore"},
		"OfferRunbooks":          {"Service.runbookBefore"},
		"runbookBefore":          {"Service.Ask"},
		"runbookAfter":           {"Service.Ask"},
		"runbookFrame":           {"Service.runbookBefore"},
		"stepTypedLocked":        {"Service.typeDone"},
		"stepEditedLocked":       {"Service.EditProposal"},
		"startRunLocked":         {"Service.runbookBefore", "Service.runbookAfter"},
	}
	seen := map[string]int{}
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
				want := allowed[name]
				if want == nil {
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

// Runbook text is refused, before anything is redacted or logged, unless
// a pane the agent may read was read for the question; a send is logged
// with the id and counts only.
func TestReadRunbookNeedsAReadablePane(t *testing.T) {
	log := &memLog{}
	a, err := agent.NewAuditor(log)
	if err != nil {
		t.Fatal(err)
	}
	scope := agent.NewScope([]agent.Host{{ID: "on", Name: "web-1", AgentReadable: true}, {ID: "off", Name: "db", AgentReadable: false}})
	r := redact.New(redact.Options{})
	if _, err := a.ReadRunbook(scope, nil, "rb_1", 2, "SECRET-STEP", r); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("no pane: %v", err)
	}
	if _, err := a.OfferRunbooks(scope, []agent.Pane{{SessionID: "s", HostID: "off"}}, []string{"rb_1"}, "SECRET-STEP", r); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("unreadable pane: %v", err)
	}
	if len(log.entries) != 0 {
		t.Fatalf("logged a refused send: %+v", log.entries)
	}
	ex, err := a.ReadRunbook(scope, []agent.Pane{{SessionID: "s", HostID: "on"}}, "rb_1", 2, "step 1: token ghp_abcdefghijklmnopqrstuvwxyz0123456789", r)
	if err != nil || strings.Contains(ex.Text.String(), "ghp_") {
		t.Fatalf("excerpt %q %v", ex.Text.String(), err)
	}
	if len(log.entries) != 1 || log.entries[0].Kind != agent.EventRunbook || strings.Contains(log.entries[0].Message, "step 1") || !strings.Contains(log.entries[0].Message, "rb_1 (2 steps)") {
		t.Fatalf("log %+v", log.entries)
	}
}
