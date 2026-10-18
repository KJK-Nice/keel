package agent_test

import (
	"errors"
	"go/ast"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/redact"
	"keel/internal/applog"
)

// Notes for the agent (docs/ui-redesign.md §15): the agent never writes a
// note on its own. Notes are added only from the user's own add or the
// user's Save on a proposal; they are read for a provider message only in
// noteContext, through the Auditor (scope check, redaction, agent.note).
func TestNotesOnlyThroughTheirPaths(t *testing.T) {
	allowed := map[string][]string{
		"ListAgentNotes":  {"Service.noteContext", "Service.ListNotes"},
		"AddAgentNote":    {"Service.AddNote", "Service.SaveNoteProposal"},
		"UpdateAgentNote": {"Service.EditNote"},
		"ReadHostNotes":   {"Service.noteContext"},
		"ReadUserNotes":   {"Service.noteContext"},
		"noteContext":     {"Service.Ask"},
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
				want := allowed[sel.Sel.Name]
				if want == nil {
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
	for name := range allowed {
		if seen[name] == 0 {
			t.Errorf("%s is never called: the check is out of date", name)
		}
	}
}

type memLog struct{ entries []applog.Entry }

func (m *memLog) Append(e applog.Entry) { m.entries = append(m.entries, e) }

// A host's notes are refused, before anything is redacted or logged, when
// the agent may not read that host.
func TestReadHostNotesChecksScope(t *testing.T) {
	log := &memLog{}
	a, err := agent.NewAuditor(log)
	if err != nil {
		t.Fatal(err)
	}
	scope := agent.NewScope([]agent.Host{{ID: "on", Name: "web-1", AgentReadable: true}, {ID: "off", Name: "db", AgentReadable: false}})
	r := redact.New(redact.Options{})
	if _, err := a.ReadHostNotes(scope, agent.Host{ID: "off", Name: "db"}, []string{"x"}, r); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("unreadable host: %v", err)
	}
	if _, err := a.ReadHostNotes(scope, agent.Host{ID: "gone"}, []string{"x"}, r); !errors.Is(err, agent.ErrNotReadable) {
		t.Fatalf("unknown host: %v", err)
	}
	if len(log.entries) != 0 {
		t.Fatalf("logged a refused read: %+v", log.entries)
	}
	ex, err := a.ReadHostNotes(scope, agent.Host{ID: "on", Name: "web-1"}, []string{"one", "  ", "token ghp_abcdefghijklmnopqrstuvwxyz0123456789"}, r)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Count != 2 || strings.Contains(ex.Text.String(), "ghp_") || !strings.HasPrefix(ex.Text.String(), "- one\n- token [secret:1]") {
		t.Fatalf("excerpt %+v %q", ex, ex.Text.String())
	}
	if len(log.entries) != 1 || log.entries[0].Kind != agent.EventNote || log.entries[0].HostID != "on" || strings.Contains(log.entries[0].Message, "one") {
		t.Fatalf("log %+v", log.entries)
	}
}
