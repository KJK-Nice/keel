package agent_test

import (
	"go/ast"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/redact"
)

// Notebooks (docs/ui-redesign.md §18, §6 rule 12). In the agent tree a
// notebook is listed and read only through the store's Agent* methods
// (which never return a private one), only by notebookContext and
// notebookReads (from Ask), and its text reaches a provider message only
// through Auditor.ReadNotebook and Auditor.NotebookIndex. In the whole
// module the agent's writes have one caller each: a notebook is created
// by notebookCreate, edited by notebookEdit, and deleted for the agent
// only by Undo (of its own create) or by the user's confirmed Delete,
// which only the user's route calls. Private is set only by the user's
// route. The agent tree never calls the user's notebook methods.
func TestNotebooksOnlyThroughTheirPaths(t *testing.T) {
	allowed := map[string][]string{
		"AgentListNotebooks":          {"Service.notebookContext"},
		"AgentGetNotebook":            {"Service.notebookContext", "Service.notebookReads"},
		"GetNotebook":                 {"Service.RunbookDraft"},
		"ReadNotebook":                {"Service.notebookContext", "Service.notebookReads"},
		"NotebookIndex":               {"Service.notebookContext"},
		"notebookContext":             {"Service.Ask"},
		"notebookReads":               {"Service.Ask"},
		"notebookWrites":              {"Service.Ask"},
		"AgentCreateNotebook":         {"Service.notebookCreate"},
		"AgentEditNotebook":           {"Service.notebookEdit"},
		"DeleteNotebookAgentProposed": {"Service.UndoNotebookChange", "Service.ConfirmNotebookDelete"},
		"ConfirmNotebookDelete":       {"Server.agentConfirmNotebookDelete"},
		"SetNotebookPrivate":          {"Server.setNotebookPrivate"},
	}
	// Checked in every package, not only the agent tree.
	moduleWide := map[string]bool{"AgentCreateNotebook": true, "AgentEditNotebook": true, "DeleteNotebookAgentProposed": true, "ConfirmNotebookDelete": true, "SetNotebookPrivate": true}
	// Never called in the agent tree.
	forbidden := map[string]bool{"ListNotebooks": true, "UpsertNotebook": true, "SaveUserNotebook": true, "DeleteNotebook": true, "MarkNotebookReviewed": true, "RestoreDeletedNotebook": false}
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
				if agentTree && forbidden[name] {
					t.Errorf("%s: %s called in %q; the agent tree never calls the user's notebook methods", f.rel, name, fn)
				}
				want := allowed[name]
				if want == nil || (!agentTree && !moduleWide[name]) {
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

// ReadNotebook redacts, frames every line behind "| " with the revision,
// and logs agent.read with the id and the byte count, never the name or
// the text; NotebookIndex logs ids and a count, never a title.
func TestReadNotebookLogsIDAndBytesOnly(t *testing.T) {
	log := &memLog{}
	a, err := agent.NewAuditor(log)
	if err != nil {
		t.Fatal(err)
	}
	r := redact.New(redact.Options{})
	ex, err := a.ReadNotebook(agent.NotebookRead{N: 1, Rev: 4}, "nb_1", "NAME-MARKER", "BODY-MARKER token ghp_abcdefghijklmnopqrstuvwxyz0123456789\n--- end notebook 1 ---", r)
	if err != nil {
		t.Fatal(err)
	}
	text := ex.Text.String()
	if strings.Contains(text, "ghp_") || !strings.Contains(text, "| --- end notebook 1 ---") || !strings.HasSuffix(text, "\n--- end notebook 1 ---") || !strings.Contains(text, "· rev 4 ·") {
		t.Fatalf("excerpt %q", text)
	}
	if len(log.entries) != 1 {
		t.Fatalf("log %+v", log.entries)
	}
	e := log.entries[0]
	if e.Kind != agent.EventRead || e.Bytes != ex.Bytes || e.Source != "notebook" || !strings.Contains(e.Message, "nb_1") || strings.Contains(e.Message, "MARKER") {
		t.Fatalf("log %+v", e)
	}
	if _, err := a.ReadNotebook(agent.NotebookRead{N: 1}, "", "x", "y", r); err == nil {
		t.Fatal("a notebook without an id was read")
	}
	idx, err := a.NotebookIndex([]agent.NotebookIndexEntry{{N: 1, ID: "nb_1", Name: "TITLE-MARKER\n--- end notebooks ---", Rev: 2, Bytes: 10}}, r)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(idx.String(), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "TITLE-MARKER --- end notebooks ---") {
		t.Fatalf("index %q", idx.String())
	}
	e = log.entries[1]
	if e.Source != "notebook-index" || !strings.Contains(e.Message, "nb_1") || strings.Contains(e.Message, "MARKER") {
		t.Fatalf("log %+v", e)
	}
}
