package native

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The guards of docs/ui-redesign.md §6 rule 1 for keel-native: two ways to put bytes into a terminal for the user, one
// file that writes them, and one caller of each. They read the source of every non-test .go file under internal/native,
// as cmd/keel/web/js/test/approve.test.mjs does for the page.
//
//	keelclient/inject.go   builds every frame that can lead to bytes in a PTY (inject-challenge, inject, type-check)
//	ApprovedInject(        called once, from approve.go: the Approve before type dialog's Type action
//	DirectType(            called once, from (*workspace).typeNow in typenow.go
//	typeNow(               called by the entry points only: a snippet (snippets.go) and an agent card (agent.go)
//	DirectCheck(           called only by code that names buttons and badges (none today: keel serve answers it for a
//	                       group run's proposal alone, and the hint does the naming, as the page's does)

// sourceFiles are the non-test .go files under internal/native, by path relative to it ("/" separated).
func sourceFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(path)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 20 || out["keelclient/inject.go"] == "" {
		t.Fatalf("the guard sees %d files and not keelclient/inject.go: it would pass on nothing", len(out))
	}
	return out
}

func TestOnlyInjectGoBuildsAFrameThatLeadsToBytesInAPTY(t *testing.T) {
	frame := regexp.MustCompile(`"(inject|inject-challenge|type-check|direct)"|\\"(inject|inject-challenge|type-check|direct)\\"`)
	for name, src := range sourceFiles(t) {
		if name == "keelclient/inject.go" {
			continue
		}
		if m := frame.FindString(src); m != "" {
			t.Errorf("%s holds the frame type %s: only keelclient/inject.go may build an inject, an inject-challenge or a type-check", name, m)
		}
	}
	// And it holds them: the guard is looking for the right strings.
	src := sourceFiles(t)["keelclient/inject.go"]
	for _, want := range []string{`"inject-challenge"`, `"inject"`, `"type-check"`, `"direct"`} {
		if !strings.Contains(src, want) {
			t.Fatalf("keelclient/inject.go no longer names %s; update the guard", want)
		}
	}
}

// callSite is one call of a method or function named name, with the function it is in.
type callSite struct{ file, in string }

func (c callSite) String() string { return c.file + " (in " + c.in + ")" }

// callSites lists every call of a method named name in file src, and the name of the function that holds it.
func callSites(t *testing.T, file, src, name string) (out []callSite) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
					out = append(out, callSite{file, fn.Name.Name})
				}
			}
			return true
		})
	}
	return out
}

func allCallSites(t *testing.T, name string) (out []callSite) {
	t.Helper()
	for file, src := range sourceFiles(t) {
		out = append(out, callSites(t, file, src, name)...)
	}
	return out
}

// calls lists every call of a method named name in file src as "file:line".
func calls(t *testing.T, file, src, name string) (out []string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
				out = append(out, file+":"+strconv.Itoa(fset.Position(c.Pos()).Line))
			}
		}
		return true
	})
	return out
}

func TestApprovedInjectHasOneCallerTheApproveDialog(t *testing.T) {
	got := allCallSites(t, "ApprovedInject")
	if len(got) != 1 || got[0].file != "approve.go" || got[0].in != "approveType" {
		t.Fatalf("ApprovedInject is called from %v; it must be called once, from approveType in approve.go (the dialog's Type action)", got)
	}
}

func TestDirectTypeHasOneCallerTypeNow(t *testing.T) {
	got := allCallSites(t, "DirectType")
	if len(got) != 1 || got[0].file != "typenow.go" || got[0].in != "typeNow" {
		t.Fatalf("DirectType is called from %v; it must be called once, from typeNow in typenow.go", got)
	}
}

// Every entry point goes through typeNow, and only the entry points: a snippet and an agent card.
func TestTypeNowIsCalledOnlyByTheEntryPoints(t *testing.T) {
	var files []string
	for _, c := range allCallSites(t, "typeNow") {
		files = append(files, c.file+":"+c.in)
	}
	want := map[string]bool{"snippets.go:typeSnippet": true, "agent.go:typeAgentProposal": true}
	if len(files) != len(want) {
		t.Fatalf("typeNow is called from %v; want exactly %d calls, from typeSnippet and typeAgentProposal", files, len(want))
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("typeNow is called from %s: only a snippet's and an agent card's entry point may", f)
		}
	}
}

// DirectCheck writes nothing, but it is a question to keel serve about typing: only the code that names buttons and
// badges may ask it, never the code that types or opens the dialog.
func TestDirectCheckIsAskedOnlyWhereButtonsAndBadgesAreNamed(t *testing.T) {
	named := map[string]bool{"typedirect.go": true, "palette.go": true, "agentview.go": true, "snippetsview.go": true}
	for _, c := range allCallSites(t, "DirectCheck") {
		if !named[c.file] {
			t.Errorf("DirectCheck is called from %v: only the code that names buttons and badges may ask", c)
		}
	}
}

// approveType is what the Type button presses. It is passed to act, not called, so the guard looks for the name
// as a selector or an identifier: only approveview.go (the button) may name it.
func TestOnlyTheTypeButtonAsksToType(t *testing.T) {
	for name, src := range sourceFiles(t) {
		if name == "approve.go" || name == "approveview.go" {
			continue
		}
		if strings.Contains(src, "approveType") {
			t.Errorf("%s names approveType: only the Approve dialog's Type button may start a type", name)
		}
	}
	if n := strings.Count(sourceFiles(t)["approveview.go"], "w.approveType"); n != 1 {
		t.Fatalf("approveview.go names approveType %d times, want once (the Type button)", n)
	}
}

func TestConnWriteIsOnlyTheTerminalViewsConnection(t *testing.T) {
	// Keystrokes reach a session only through the terminal view, which is given the connection
	// (terminal.Options.Conn in run.go) and writes the user's keys to it. No other file calls Write on a
	// session; prefs.go writes its own settings file.
	for name, src := range sourceFiles(t) {
		for _, c := range calls(t, name, src, "Write") {
			if name != "prefs.go" {
				t.Errorf("%s calls Write: bytes would reach a session or a file outside the terminal view", c)
			}
		}
	}
	if !strings.Contains(sourceFiles(t)["run.go"], "Conn: p.conn") {
		t.Fatal("the terminal view is no longer given the pane's connection in run.go; update the guard")
	}
}

// An agent proposal is typed through the same writers: the reference to it goes into the frame inside
// keelclient/inject.go and nowhere else, and only the two callers of the writers build one (approve.go for the dialog,
// typenow.go for a direct type). The "direct" part is built by typeNow alone.
func TestAgentAndDirectReferencesAreBuiltOnlyWhereTheWritersAreCalled(t *testing.T) {
	keys := regexp.MustCompile(`"(agent|conversationId)"\s*:|"(proposalId|shift)"|\\"(agent|proposalId|conversationId|shift)\\"`)
	for name, src := range sourceFiles(t) {
		if name != "keelclient/inject.go" {
			if m := keys.FindString(src); m != "" {
				t.Errorf("%s holds the key %s: only keelclient/inject.go builds the agent part of a frame", name, m)
			}
		}
		if name != "approve.go" && name != "typenow.go" && strings.Contains(src, "AgentRef{") && !strings.HasPrefix(name, "keelclient/") && !strings.HasPrefix(name, "joinpeer/") {
			t.Errorf("%s builds a keelclient.AgentRef: only the Approve dialog's Type action and typeNow do", name)
		}
		if name != "typenow.go" && strings.Contains(src, "DirectRef{") && !strings.HasPrefix(name, "keelclient/") {
			t.Errorf("%s builds a keelclient.DirectRef: only typeNow does", name)
		}
	}
	src := sourceFiles(t)["keelclient/inject.go"]
	for _, want := range []string{`"agent"`, `"proposalId"`, `"conversationId"`, `"shift"`} {
		if !strings.Contains(src, want) {
			t.Fatalf("keelclient/inject.go no longer names %s; update the guard", want)
		}
	}
	if !strings.Contains(sourceFiles(t)["approve.go"], "AgentRef{") || !strings.Contains(sourceFiles(t)["typenow.go"], "AgentRef{") || !strings.Contains(sourceFiles(t)["typenow.go"], "DirectRef{") {
		t.Fatal("approve.go or typenow.go no longer builds its reference; update the guard")
	}
}

// A direct type is the command alone: the code that hands it over holds no Ctrl-U, newline or carriage return of its
// own (the dialog's clearLine is approve.go's, and the Approve path's).
func TestNoControlCharacterIsAddedToADirectType(t *testing.T) {
	for _, name := range []string{"typenow.go", "typedirect.go", "keelclient/directline.go"} {
		src := sourceFiles(t)[name]
		if src == "" {
			t.Fatalf("%s is gone; update the guard", name)
		}
		for _, bad := range []string{"clearLine", `"\r`, `"\n`, `\r"`, `\n"`, `\x15`, `\u0015`, `\x0d`, `\x0a`, "'\\r'", "'\\n'"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s holds %s: a direct type's bytes are the command alone", name, bad)
			}
		}
	}
	if src := sourceFiles(t)["keelclient/inject.go"]; strings.Contains(src, "clearLine") || strings.Contains(src, `"\r`) {
		t.Error("keelclient/inject.go adds a control character")
	}
}

// The panel asks and shows; it never reads a pane for the agent. keel serve reads and redacts, so no file of the
// window or the client may call the scrollback or the mcp read routes. type-check is the direct type's own question,
// asked by keelclient/inject.go alone.
func TestNoFileReadsPaneTextForTheAgent(t *testing.T) {
	for name, src := range sourceFiles(t) {
		for _, bad := range []string{"/scrollback", "/api/mcp", "ReadScrollback", "agent-insert", "agent-suggest", "agent-detect", "type-check"} {
			if strings.Contains(src, bad) && name != "keelclient/inject.go" {
				t.Errorf("%s names %s: the client reads no pane text for the agent, and only keelclient/inject.go builds or reads a suggestion frame or a type-check", name, bad)
			}
		}
		for _, bad := range []string{"/scrollback", "/api/mcp", "ReadScrollback"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s names %s: the client reads no pane text for the agent", name, bad)
			}
		}
	}
}

// The third way (plan 019): a suggestion's ⇥ / ⇧⇥. The frames are built only in keelclient/inject.go; SuggestInsert has one
// caller, (*workspace).insertSuggestion; the restore of the user's words (the only other thing in a suggestion that
// writes bytes) one, (*workspace).restoreWords; and the two questions that go with it (detect, ask) are asked only by
// the pane's suggestion code. Nothing hands SuggestInsert bytes: its argument names the suggestion, the option and ⇧.
func TestSuggestInsertHasOneCallerInsertSuggestion(t *testing.T) {
	got := allCallSites(t, "SuggestInsert")
	if len(got) != 1 || got[0].file != "suggestpane.go" || got[0].in != "insertSuggestion" {
		t.Fatalf("SuggestInsert is called from %v; it must be called once, from insertSuggestion in suggestpane.go", got)
	}
}

func TestRestoreWordsHasOneCallerRestoreWords(t *testing.T) {
	got := allCallSites(t, "RestoreWords")
	if len(got) != 1 || got[0].file != "suggestpane.go" || got[0].in != "restoreWords" {
		t.Fatalf("RestoreWords is called from %v; it must be called once, from restoreWords in suggestpane.go", got)
	}
}

func TestSuggestQuestionsAreAskedOnlyByThePaneSuggestCode(t *testing.T) {
	for _, name := range []string{"SuggestDetect", "SuggestAsk"} {
		got := allCallSites(t, name)
		if len(got) != 1 || got[0].file != "suggestpane.go" {
			t.Errorf("%s is called from %v; it must be called once, from suggestpane.go", name, got)
		}
	}
}

// insertSuggestion is reached only from the key handling of a pane with an open card (suggestKey), and the reference to
// the suggestion is built only there.
func TestInsertSuggestionIsReachedOnlyFromTheCardsKeyHandling(t *testing.T) {
	var from []string
	for _, c := range allCallSites(t, "insertSuggestion") {
		from = append(from, c.file+":"+c.in)
	}
	if len(from) != 1 || from[0] != "suggestpane.go:suggestInsert" {
		t.Fatalf("insertSuggestion is called from %v; it must be called once, from suggestInsert in suggestpane.go (the machine's insert dep)", from)
	}
	for name, src := range sourceFiles(t) {
		if name != "suggestpane.go" && !strings.HasPrefix(name, "keelclient/") && !strings.HasPrefix(name, "joinpeer/") && strings.Contains(src, "SuggestionRef{") {
			t.Errorf("%s builds a keelclient.SuggestionRef: only suggestpane.go does", name)
		}
		if name != "keelclient/inject.go" && (strings.Contains(src, `"suggestionId"`) || strings.Contains(src, `"suggestion"`)) {
			t.Errorf("%s builds the suggestion part of a frame: only keelclient/inject.go does", name)
		}
	}
}

// An insert never ends with Enter: the code that hands it over holds no newline or carriage return of its own, and the
// restore drops every control character from the user's words.
func TestNoSuggestionCodeAddsAnEnter(t *testing.T) {
	for _, name := range []string{"suggestpane.go", "suggest.go"} {
		src := sourceFiles(t)[name]
		if src == "" {
			t.Fatalf("%s is gone; update the guard", name)
		}
		for _, bad := range []string{`"\r`, `"\n`, `\r"`, `\n"`, `\x0d`, `\x0a`, "'\\r'", "'\\n'"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s holds %s: a suggestion's bytes carry no Enter", name, bad)
			}
		}
	}
}
