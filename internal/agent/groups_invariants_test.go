package agent_test

import (
	"errors"
	"go/ast"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/redact"
)

// Groups, part 1 (docs/ui-redesign.md §17): a group run's proposal is
// authorized only through AuthorizeType (the one gate every Type goes
// through), a host's step is recorded as typed only from the session
// socket's write (GroupTypeDoneAt, never called inside the agent tree), and
// a run's proposals are made only by ProposeGroupStep from the saved
// command.
func TestGroupRunsOnlyThroughTheirPaths(t *testing.T) {
	allowed := map[string][]string{
		"authorizeGroupType": {"Service.AuthorizeType"},
		"GroupTypeDoneAt":    {},
		"groupProposalLocked": {
			"Service.authorizeGroupType", "Service.GroupTypeDoneAt",
		},
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
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				want, tracked := allowed[sel.Sel.Name]
				if !tracked {
					return true
				}
				seen[sel.Sel.Name]++
				ok = false
				for _, w := range want {
					ok = ok || w == fn
				}
				if !ok {
					t.Errorf("%s: %s called in %q; only %v may", f.rel, sel.Sel.Name, fn, want)
				}
				return true
			})
		}
	}
	for _, name := range []string{"authorizeGroupType", "groupProposalLocked"} {
		if seen[name] == 0 {
			t.Errorf("%s is never called: the check is out of date", name)
		}
	}
	// A runbook step's proposal is built only in the two places that turn
	// a saved step into a proposal (and in expiredView, which shows a saved
	// one that can no longer be typed).
	for _, f := range parseModule(t) {
		if f.test || !inAgentTree(f.pkg) {
			continue
		}
		for _, decl := range f.ast.Decls {
			fd, _ := decl.(*ast.FuncDecl)
			if fd == nil {
				continue
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				id, ok := lit.Type.(*ast.Ident)
				if !ok || id.Name != "ProposalStep" {
					return true
				}
				if fd.Name.Name != "runbookAfter" && fd.Name.Name != "ProposeGroupStep" && fd.Name.Name != "expiredView" {
					t.Errorf("%s: a runbook step proposal is made in %s", f.rel, fd.Name.Name)
				}
				return true
			})
		}
	}
}

// A group's notes are refused, before anything is redacted or logged,
// unless the agent may read one of the group's hosts read for the
// question; a send is logged with the group's id and the count only.
func TestReadGroupNotesChecksScope(t *testing.T) {
	log := &memLog{}
	a, err := agent.NewAuditor(log)
	if err != nil {
		t.Fatal(err)
	}
	scope := agent.NewScope([]agent.Host{{ID: "on", Name: "wk-1", AgentReadable: true}, {ID: "off", Name: "db", AgentReadable: false}})
	r := redact.New(redact.Options{})
	if _, err := a.ReadGroupNotes(scope, "g1", nil, []string{"x"}, nil, r); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("no host read: %v", err)
	}
	if _, err := a.ReadGroupNotes(scope, "g1", []agent.Host{{ID: "off"}, {ID: "gone"}}, []string{"x"}, nil, r); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("unreadable hosts: %v", err)
	}
	if len(log.entries) != 0 {
		t.Fatalf("logged a refused read: %+v", log.entries)
	}
	ex, err := a.ReadGroupNotes(scope, "g1", []agent.Host{{ID: "on", Name: "wk-1"}}, []string{"Calico CIDR 10.244.0.0/16", "token ghp_abcdefghijklmnopqrstuvwxyz0123456789"}, []string{"role of wk-1 in this group: worker"}, r)
	if err != nil {
		t.Fatal(err)
	}
	text := ex.Text.String()
	if ex.Count != 2 || strings.Contains(text, "ghp_") || !strings.Contains(text, "- Calico") || !strings.HasSuffix(text, "role of wk-1 in this group: worker") {
		t.Fatalf("excerpt %+v %q", ex, text)
	}
	if len(log.entries) != 1 || log.entries[0].Kind != agent.EventNote || !strings.Contains(log.entries[0].Message, "2 group notes for group g1") || strings.Contains(log.entries[0].Message, "Calico") || strings.Contains(log.entries[0].Message, "wk-1") {
		t.Fatalf("log %+v", log.entries)
	}
}
