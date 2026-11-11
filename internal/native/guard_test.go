package native

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	want := map[string]bool{"snippets.go:typeSnippet": true, "agent.go:typeAgentProposal": true, "notebooktype.go:typeNotebookFence": true}
	if len(files) != len(want) {
		t.Fatalf("typeNow is called from %v; want exactly %d calls, from typeSnippet, typeAgentProposal and typeNotebookFence", files, len(want))
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("typeNow is called from %s: only a snippet's, an agent card's and a notebook fence's entry point may", f)
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
	// Keystrokes reach a session only through the terminal view, which is given the pane's connection wrapped by
	// newSyncConn (terminal.Options.Conn in run.go) and writes the user's keys to it. No other file calls Write on a
	// session but syncinput.go, which writes those same keys to the pane's own connection and, with sync on, to its
	// peers (TestSyncInputIsTheOnlyFanOutAndNothingTypedReachesIt); prefs.go writes its own settings file.
	for name, src := range sourceFiles(t) {
		for _, c := range calls(t, name, src, "Write") {
			if name != "prefs.go" && name != "syncinput.go" {
				t.Errorf("%s calls Write: bytes would reach a session or a file outside the terminal view", c)
			}
		}
	}
	if !strings.Contains(sourceFiles(t)["run.go"], "Conn: newSyncConn(p)") {
		t.Fatal("the terminal view is no longer given the pane's connection in run.go; update the guard")
	}
}

// ---- sync input (plan 026) ----
//
// The fifth writer is the user's own keys, fanned out to the other terminals of a tab with sync on. It lives in syncinput.go
// alone, behind the terminal view's own write, and nothing that types for the user can reach it.
func TestSyncInputIsTheOnlyFanOutAndNothingTypedReachesIt(t *testing.T) {
	src := sourceFiles(t)
	// 1. Outside keelclient and joinpeer (and the stubs' own method declarations, which are not calls), only syncinput.go calls
	// Write on a connection; prefs.go's call writes a file.
	for name, s := range src {
		if strings.HasPrefix(name, "keelclient/") || strings.HasPrefix(name, "joinpeer/") || name == "prefs.go" {
			continue
		}
		for _, c := range calls(t, name, s, "Write") {
			if name != "syncinput.go" {
				t.Errorf("%s calls Write: only syncinput.go writes to a session", c)
			}
		}
	}
	if len(calls(t, "syncinput.go", src["syncinput.go"], "Write")) < 2 {
		t.Fatal("syncinput.go no longer writes the pane's own connection and its peers; update the guard")
	}
	// 2. The wrapper is made in one place: liveBodyT in run.go, as the terminal view's connection.
	for name, s := range src {
		if name != "syncinput.go" && strings.Contains(s, "syncConn{") {
			t.Errorf("%s builds a syncConn: only newSyncConn does", name)
		}
		if name != "run.go" && name != "syncinput.go" && strings.Contains(s, "newSyncConn") {
			t.Errorf("%s names newSyncConn: only run.go hands the wrapper to the terminal view", name)
		}
	}
	if n := strings.Count(src["run.go"], "newSyncConn("); n != 1 {
		t.Errorf("run.go names newSyncConn %d times, want once (Options.Conn)", n)
	}
	// 3. Nothing names the fan-out but syncinput.go, and only the wrapper's Write calls it there.
	for name, s := range src {
		if name != "syncinput.go" && strings.Contains(s, "fanOut") {
			t.Errorf("%s names fanOut: only syncinput.go does", name)
		}
	}
	f, err := parser.ParseFile(token.NewFileSet(), "syncinput.go", src["syncinput.go"], 0)
	if err != nil {
		t.Fatal(err)
	}
	var from []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "fanOut" {
					from = append(from, fn.Name.Name)
				}
			}
			return true
		})
	}
	if !slices.Equal(from, []string{"Write"}) {
		t.Errorf("fanOut is called from %v in syncinput.go, want only (*syncConn).Write", from)
	}
	// 4. syncinput.go names none of the ways Keel types for the user.
	for _, bad := range []string{"ApprovedInject", "DirectType", "SuggestInsert", "RestoreWords", "AgentRef", "typeNow", "approveType", "openApprove"} {
		if strings.Contains(src["syncinput.go"], bad) {
			t.Errorf("syncinput.go names %s: sync input fans out the user's own keys and nothing Keel types", bad)
		}
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

// Notes and runbooks (plan 020). The agent never writes either: a proposal is kept by one request, sent by one function,
// reached only from the card's Save button; a runbook step is an ordinary proposal and is typed only by the Type path that
// every proposal takes. None of the files of these two features holds a writer, a way to open the Approve dialog, or a
// second route to the typing path.
var memoryFiles = []string{"notes.go", "notesview.go", "noteslogic.go", "runbooks.go", "runbooksview.go", "runbookslogic.go"}

func TestNoteAndRunbookCodeHasNoWriterAndNoWayToType(t *testing.T) {
	banned := []string{"ApprovedInject", "DirectType", "DirectCheck", "SuggestInsert", "RestoreWords", "approveType", "openApprove", "typeNow", "typeAgentProposal", "AgentRef{", "DirectRef{"}
	for _, name := range memoryFiles {
		src := sourceFiles(t)[name]
		if src == "" {
			t.Fatalf("%s is gone; update the guard", name)
		}
		for _, bad := range banned {
			if strings.Contains(src, bad) {
				t.Errorf("%s names %s: notes and runbooks never type; a runbook step is typed by typeAgentProposal like any proposal", name, bad)
			}
		}
	}
}

// The requests that keep a proposal each have one caller, and nothing calls those callers but a button's handler in the
// view: no timer, no load, no ask, no retry.
func TestAProposalIsKeptOnlyByItsCardsSaveButton(t *testing.T) {
	for _, c := range []struct{ request, in, view string }{
		{"SaveProposedNote", "saveNoteProposal", "notesview.go"},
		{"SaveProposedRunbook", "saveBookProposal", "runbooksview.go"},
	} {
		if got := allCallSites(t, c.request); len(got) != 1 || got[0].in != c.in {
			t.Errorf("%s is called from %v; it must be called once, from %s", c.request, got, c.in)
		}
		for _, site := range allCallSites(t, c.in) {
			if site.file != c.view {
				t.Errorf("%s is called from %v: only the card's Save button in %s may", c.in, site, c.view)
			}
		}
		if n := len(allCallSites(t, c.in)); n != 1 {
			t.Errorf("%s is called %d times in the view; the card has one Save button", c.in, n)
		}
	}
	// A card's Save is pressed by the user: it is inside a button's click, never run on its own.
	src := sourceFiles(t)["notesview.go"]
	if !strings.Contains(src, `textBtnOff(c, name, kind, rec.busy)`) {
		t.Error("the note card's Save is no longer a button; update the guard")
	}
}

// Runbook steps are proposals made by keel serve: this client holds no step's bytes and builds no proposal.
func TestARunbookStepIsNeverBuiltAsAProposalByTheClient(t *testing.T) {
	for _, name := range memoryFiles {
		src := sourceFiles(t)[name]
		for _, bad := range []string{"keelclient.Proposal{", "Bytes:", "\\x15"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s holds %s: a step's proposal is made by keel serve, for the current step only", name, bad)
			}
		}
	}
}

// ---- Groups (plan 021; docs/ui-redesign.md §6 rules 10 and 11) ----
//
// One approval may type into several hosts' terminals, so the guards below are the ones a reviewer reads first. There is still
// exactly one writer for a dialog's approval, ApprovedInject, called from the one place it was (approveType in approve.go);
// the group approval is that same call made once per ticked row, inside a loop in that function and nowhere else. The group's
// reference to its run is built only in the dialog's file and in keelclient/inject.go, no file of the Group page, the group
// run page or the folders holds a writer or a way to open a dialog that types, and no file puts a value into a command: the
// bytes of a row are set from keel serve's answers alone.

// groupFiles are the files of the Group page, the group run page, the folders and the group approve dialog's view.
var groupFiles = []string{"grouplogic.go", "grouppage.go", "grouppageview.go", "grouprun.go", "grouprunview.go", "grouprunlogic.go",
	"groupapproveview.go", "folders.go", "foldersact.go", "foldersview.go"}

// enclosing lists, for each call of a method named name in src, the kinds of statement it sits in inside its function.
func callContexts(t *testing.T, file, src, name string) (out []string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		var stack []ast.Node
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
					ctx := fn.Name.Name
					for _, s := range stack {
						switch s.(type) {
						case *ast.RangeStmt:
							ctx += "/range"
						case *ast.ForStmt:
							ctx += "/for"
						}
					}
					out = append(out, ctx)
				}
			}
			return true
		})
	}
	return out
}

func TestTheGroupApprovalIsOneApprovedInjectPerTickedRowInsideTheDialogsAction(t *testing.T) {
	ctxs := callContexts(t, "approve.go", sourceFiles(t)["approve.go"], "ApprovedInject")
	if len(ctxs) != 1 || ctxs[0] != "approveType/range" {
		t.Fatalf("ApprovedInject is called as %v in approve.go; it must be called once, in a loop over the jobs, in approveType", ctxs)
	}
	if got := allCallSites(t, "ApprovedInject"); len(got) != 1 {
		t.Fatalf("ApprovedInject is called from %v", got)
	}
	// Every job is built from one row of the dialog (groupJobs) or is the one-pane dialog's own: nothing else makes one.
	src := sourceFiles(t)["approve.go"]
	if n := strings.Count(src, "typeJob{"); n != 3 { // the type, the one-pane job, the group job
		t.Errorf("approve.go builds %d typeJob values, want 3 (its declaration, the one-pane job, a group row's job)", n)
	}
	if !strings.Contains(src, "command: r.bytes,") {
		t.Error("a group job no longer types the row's bytes as they are; update the guard")
	}
	if got := allCallSites(t, "groupJobs"); len(got) != 1 || got[0].file != "approve.go" || got[0].in != "approveType" {
		t.Errorf("groupJobs is called from %v; it must be called once, from approveType", got)
	}
	// The jobs come from rows the user ticked: effective() reads the tick, and nothing ticks a row but the user's click on its
	// check box (a toggle in groupapproveview.go) and the one-host case (approveRows). Everything else only clears it.
	toggle := 0
	for name, src := range sourceFiles(t) {
		if strings.Contains(name, "/") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.AssignStmt:
					for i, l := range x.Lhs {
						sel, ok := l.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "checked" || len(x.Lhs) != len(x.Rhs) {
							continue
						}
						switch r := x.Rhs[i].(type) {
						case *ast.Ident:
							if r.Name != "false" {
								t.Errorf("%s: %s ticks a row (%s): only the user's click does", name, fn.Name.Name, r.Name)
							}
						case *ast.UnaryExpr:
							if name != "groupapproveview.go" {
								t.Errorf("%s: %s toggles a row's tick: only the row's check box does", name, fn.Name.Name)
							}
							toggle++
						default:
							t.Errorf("%s: %s sets a row's tick to something other than false", name, fn.Name.Name)
						}
					}
				case *ast.CompositeLit:
					for _, e := range x.Elts {
						if kv, ok := e.(*ast.KeyValueExpr); ok {
							if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "checked" && fn.Name.Name != "approveRows" {
								t.Errorf("%s: %s builds a row already ticked: only approveRows does, and only for one host", name, fn.Name.Name)
							}
						}
					}
				}
				return true
			})
		}
	}
	if toggle != 1 {
		t.Errorf("%d places toggle a row's tick, want the one check box in groupapproveview.go", toggle)
	}
	if !strings.Contains(sourceFiles(t)["grouprunlogic.go"], "checked: one") {
		t.Error("approveRows no longer ticks only the one-host case; update the guard")
	}
}

func TestGroupRunIDIsBuiltOnlyInInjectGoAndTheDialog(t *testing.T) {
	for name, src := range sourceFiles(t) {
		if strings.Contains(src, "GroupRunID") && name != "keelclient/inject.go" && name != "approve.go" {
			t.Errorf("%s names GroupRunID: only keelclient/inject.go and the Approve dialog (approve.go) do", name)
		}
		if strings.Contains(src, "groupRunId") && name != "keelclient/inject.go" {
			t.Errorf("%s holds the key groupRunId: only keelclient/inject.go builds the agent part of a frame", name)
		}
	}
	if !strings.Contains(sourceFiles(t)["keelclient/inject.go"], `"groupRunId"`) || !strings.Contains(sourceFiles(t)["approve.go"], "GroupRunID: g.runID") {
		t.Fatal("inject.go or approve.go no longer names the run; update the guard")
	}
}

// pressType names approveType for the two dialogs; only their views name it, once each.
func TestOnlyTheTwoDialogsTypeButtonsPressType(t *testing.T) {
	for name, src := range sourceFiles(t) {
		if strings.Contains(src, "pressType") && name != "approveview.go" && name != "groupapproveview.go" {
			t.Errorf("%s names pressType: only the Approve dialogs' Type buttons do", name)
		}
	}
	if n := strings.Count(sourceFiles(t)["groupapproveview.go"], "w.pressType("); n != 1 {
		t.Fatalf("groupapproveview.go presses Type %d times, want once (the Type button)", n)
	}
	if n := strings.Count(sourceFiles(t)["approveview.go"], "w.pressType(false)"); n != 1 {
		t.Fatalf("approveview.go presses Type %d times, want once (the Type button)", n)
	}
}

func TestGroupCodeHasNoWriterAndNoWayToTypeExceptOpeningTheDialog(t *testing.T) {
	banned := []string{"ApprovedInject", "DirectType", "DirectCheck", "SuggestInsert", "RestoreWords", "approveType", "pressType", "typeNow", "typeAgentProposal",
		"AgentRef{", "DirectRef{", "SuggestionRef{", "clearLine", "DirectLine"}
	for _, name := range groupFiles {
		src := sourceFiles(t)[name]
		if src == "" {
			t.Fatalf("%s is gone; update the guard", name)
		}
		for _, bad := range banned {
			if name == "groupapproveview.go" && bad == "pressType" {
				continue
			}
			if strings.Contains(src, bad) {
				t.Errorf("%s names %s: group code never types; the dialog's Type action does, once per ticked row", name, bad)
			}
		}
		if strings.Contains(src, "openApprove") && name != "grouprun.go" {
			t.Errorf("%s opens an Approve dialog: only the run page's Type into N panes does", name)
		}
	}
	// Opening the group dialog is "Type into N panes" and nothing else.
	if got := allCallSites(t, "openApproveGroup"); len(got) != 1 || got[0].file != "grouprun.go" || got[0].in != "typeGroupRun" {
		t.Errorf("openApproveGroup is called from %v; it must be called once, from typeGroupRun", got)
	}
	// The fill and the propose are keel serve's questions, asked from the run page alone, never from the dialog.
	for name, want := range map[string]string{"FillGroupStep": "typeGroupRun", "ProposeGroupStep": "groupRunTick"} {
		if got := allCallSites(t, name); len(got) != 1 || got[0].file != "grouprun.go" || got[0].in != want {
			t.Errorf("%s is called from %v; it must be called once, from %s in grouprun.go", name, got, want)
		}
	}
}

// groupvars.test.mjs "run page: fill before approve, type the approved bytes": the fill is asked for after Type into N panes, its
// answer goes onto the rows, and only then does the dialog open; Copy writes the saved command with the names in, never a value.
func TestTheRunPageFillsBeforeTheDialogOpensAndCopyNeverWritesAValue(t *testing.T) {
	src := sourceFiles(t)["grouprun.go"]
	i := strings.Index(src, "func (w *workspace) typeGroupRun()")
	if i < 0 {
		t.Fatal("typeGroupRun is gone; update the guard")
	}
	body := src[i:]
	fill, rows, open := strings.Index(body, "FillGroupStep("), strings.Index(body, "fillRows(rows, fills)"), strings.LastIndex(body, "\t\t\topen()")
	if !(fill > 0 && rows > fill && open > rows) {
		t.Errorf("typeGroupRun: FillGroupStep at %d, fillRows at %d, open at %d: want fill, then rows, then the dialog", fill, rows, open)
	}
	if !strings.Contains(sourceFiles(t)["grouprunview.go"], "c.WriteClipboard(st.Command)") {
		t.Error("Copy on the run page no longer writes the step's saved command; update the guard")
	}
	for _, name := range []string{"grouprunview.go", "grouprun.go"} {
		if strings.Contains(sourceFiles(t)[name], "WriteClipboard(r.bytes") || strings.Contains(sourceFiles(t)[name], "WriteClipboard(fill") {
			t.Errorf("%s puts filled bytes on the clipboard", name)
		}
	}
}

// §6 rule 11: the client never puts a value into a command. A row's bytes are set from keel serve's answers only (the proposal's
// own bytes, or the fill's), and no group file or the dialog replaces, expands or formats text around a {{name}}.
func TestTheClientNeverSubstitutesAValue(t *testing.T) {
	for _, name := range append(slices.Clone(groupFiles), "approve.go") {
		src := sourceFiles(t)[name]
		for _, bad := range []string{"strings.Replace", "strings.NewReplacer", "ReplaceAll", "ReplaceAllString", ".Expand", "ReplaceAllStringFunc", "text/template"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s uses %s: the client puts no value into a command; keel serve fills it in at Type", name, bad)
			}
		}
	}
	for name, src := range sourceFiles(t) {
		if strings.Contains(name, "/") { // the window's own files; the client package has no rows
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.AssignStmt:
					for _, l := range x.Lhs {
						if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "bytes" && fn.Name.Name != "fillRows" {
							t.Errorf("%s: %s assigns a row's bytes: only fillRows does (from keel serve's fill)", name, fn.Name.Name)
						}
					}
				case *ast.CompositeLit:
					for _, e := range x.Elts {
						if kv, ok := e.(*ast.KeyValueExpr); ok {
							if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "bytes" && fn.Name.Name != "approveRows" && name != "grouplogic_test.go" {
								t.Errorf("%s: %s builds a row with bytes: only approveRows does (from the proposal keel serve made)", name, fn.Name.Name)
							}
						}
					}
				}
				return true
			})
		}
	}
}

// ---- notebooks (plan 023) ----

var notebookFiles = []string{"notebooks.go", "notebookslogic.go", "notebooksview.go", "notebookpane.go", "notebookdialogs.go", "notebooktype.go"}

// A notebook fence is typed through the two paths a snippet has and adds no writer and no call site: the notebook files call
// none of the four writers, build no agent or direct reference, and name neither the Approve action nor Ctrl-U.
func TestNotebookCodeHasNoWriterAndNoCallSiteOfItsOwn(t *testing.T) {
	src := sourceFiles(t)
	for _, name := range notebookFiles {
		s := src[name]
		if s == "" {
			t.Fatalf("%s is gone; update the guard", name)
		}
		for _, w := range []string{"ApprovedInject", "DirectType", "SuggestInsert", "RestoreWords", "DirectCheck", "Write", "approveType"} {
			if name == "notebooks.go" && w == "Write" { // WriteClipboard is not this; nbConn defines Write, it does not call it
				continue
			}
			for _, c := range calls(t, name, s, w) {
				t.Errorf("%s calls %s: a notebook reaches a terminal only through typeNow and the Approve dialog", c, w)
			}
		}
		for _, bad := range []string{"AgentRef{", "DirectRef{", "clearLine", `\x15`, `\u0015`} {
			if strings.Contains(s, bad) {
				t.Errorf("%s holds %s: only typenow.go and approve.go build a reference or add Ctrl-U", name, bad)
			}
		}
	}
	// The only notebook call into the typing paths is typeNotebookFence -> typeNow, and askNotebookType -> openApprove.
	var got []string
	for _, name := range notebookFiles {
		for _, w := range []string{"typeNow", "openApprove", "askType", "openApproveAgent"} {
			for _, c := range callSites(t, name, src[name], w) {
				got = append(got, c.in+"->"+w)
			}
		}
	}
	slices.Sort(got)
	if want := []string{"askNotebookType->openApprove", "typeNotebookFence->typeNow"}; !slices.Equal(got, want) {
		t.Errorf("notebook code reaches the typing paths from %v, want %v", got, want)
	}
}

// The notebook field of an inject frame is written in keelclient/inject.go and nowhere else.
func TestTheNotebookInjectFieldIsWrittenOnlyByInjectGo(t *testing.T) {
	key := regexp.MustCompile(`\["notebook"\]|"notebook"\s*:\s*true|"notebookId"`)
	for name, src := range sourceFiles(t) {
		if name == "keelclient/inject.go" || strings.HasSuffix(name, "notebooks.go") && strings.HasPrefix(name, "keelclient/") {
			continue
		}
		if m := key.FindString(src); m != "" {
			t.Errorf("%s holds %s: only keelclient/inject.go writes the notebook part of a frame", name, m)
		}
	}
	s := sourceFiles(t)["keelclient/inject.go"]
	for _, want := range []string{`"notebook"`, `"notebookId"`, "FromNotebook"} {
		if !strings.Contains(s, want) {
			t.Fatalf("keelclient/inject.go no longer names %s; update the guard", want)
		}
	}
	// typeNow is the one place a notebook's direct reference is made; approve.go passes the option and nothing else.
	if !strings.Contains(sourceFiles(t)["typenow.go"], "NotebookID") || !strings.Contains(sourceFiles(t)["approve.go"], "FromNotebook") {
		t.Fatal("typenow.go or approve.go no longer builds its notebook reference; update the guard")
	}
	for name, src := range sourceFiles(t) {
		if strings.Contains(src, "FromNotebook") && name != "approve.go" && !strings.HasPrefix(name, "keelclient/") {
			t.Errorf("%s names FromNotebook: only the Approve dialog's Type action marks bytes as a notebook's", name)
		}
	}
}

// ---- notebooks and the agent (plan 024) ----

var agentNotebookFiles = []string{"agentnotebooks.go", "agentnotebookslogic.go", "agentnotebooksview.go"}

// The agent's notebook code (the attach picker, the chips, the cards, the banner's Undo) adds no writer and no call site:
// it calls none of the four writers or the typing paths, builds no reference to a proposal, reads no pane and no
// scrollback, and never reaches keel mcp's routes. It also holds no notebook text to send: an ask names ids.
func TestAgentNotebookCodeHasNoWriterNoPaneReadAndSendsNoText(t *testing.T) {
	src := sourceFiles(t)
	for _, name := range agentNotebookFiles {
		s := src[name]
		if s == "" {
			t.Fatalf("%s is gone; update the guard", name)
		}
		for _, w := range []string{"ApprovedInject", "DirectType", "DirectCheck", "SuggestInsert", "RestoreWords", "approveType", "openApprove", "openApproveAgent",
			"typeNow", "typeAgentProposal", "typeNotebookFence", "Write", "Inject", "Snapshot"} {
			for _, c := range calls(t, name, s, w) {
				t.Errorf("%s calls %s: what the agent does to a notebook never types and never reads a pane", c, w)
			}
		}
		for _, bad := range []string{"AgentRef{", "DirectRef{", "/api/mcp", "scrollback", "Scrollback", `\x15`} {
			if strings.Contains(s, bad) {
				t.Errorf("%s holds %s", name, bad)
			}
		}
		// The text of a notebook is never read here: an ask carries ids (invariant 2).
		for _, w := range []string{"Markdown", "WithMarkdown"} {
			for _, c := range calls(t, name, s, w) {
				t.Errorf("%s calls %s: the client sends notebook ids with a question, never notebook text", c, w)
			}
		}
		for _, bad := range []string{".Cells", ".Body"} {
			if name != "agentnotebooksview.go" && strings.Contains(s, bad) {
				t.Errorf("%s reads %s: the client sends notebook ids with a question, never notebook text", name, bad)
			}
		}
	}
	// The ask names ids, taken from the attached list and nowhere else.
	ask := src["agent.go"]
	if !strings.Contains(ask, "req.Notebooks, req.Editing = slices.Clone(a.attached), w.editingNotebooks()") {
		t.Error("agent.go no longer builds the notebooks of an ask from the attached ids; update the guard")
	}
	if n := strings.Count(ask, "req.Notebooks"); n != 1 {
		t.Errorf("agent.go names AskRequest.Notebooks %d times; once, from the attached ids", n)
	}
	for name, s := range src {
		if name != "agent.go" && !strings.HasPrefix(name, "keelclient/") && (strings.Contains(s, "AskRequest{") || strings.Contains(s, "req.Notebooks")) {
			t.Errorf("%s builds an ask: only agent.go does", name)
		}
	}
}

// A card's action is a request: each of the four routes has one caller, nbPropAction, which only askNbPropAction and the
// card buttons reach; a delete the agent proposed posts only from the confirm dialog's action (invariants 5 and 6).
func TestACardActionIsOneRequestAndADeleteWaitsForTheConfirm(t *testing.T) {
	for _, route := range []string{"UndoNotebookChange", "ConfirmNotebookDelete", "DismissNotebookProposal", "RestoreNotebookChange"} {
		if got := allCallSites(t, route); len(got) != 1 || got[0].file != "agentnotebooks.go" || got[0].in != "nbPropAction" {
			t.Errorf("%s is called from %v; once, from nbPropAction in agentnotebooks.go", route, got)
		}
	}
	// nbPropAction is called by askNbPropAction (the confirm's action for Delete, at once for the rest) and by nothing else.
	var in []string
	for _, c := range allCallSites(t, "nbPropAction") {
		in = append(in, c.file+":"+c.in)
	}
	slices.Sort(in)
	if want := []string{"agentnotebooks.go:askNbPropAction", "agentnotebooks.go:askNbPropAction"}; !slices.Equal(in, want) {
		t.Errorf("nbPropAction is called from %v, want %v", in, want)
	}
	// askNbPropAction is reached only by the card's own buttons and menu (cardAction).
	for _, c := range allCallSites(t, "askNbPropAction") {
		if c.file != "agentnotebooksview.go" || c.in != "cardAction" {
			t.Errorf("askNbPropAction is called from %v: only a card's button or menu item may", c)
		}
	}
	s := sourceFiles(t)["agentnotebooks.go"]
	if !strings.Contains(s, "if action == actDelete {\n\t\tw.askConfirm(") {
		t.Error("askNbPropAction no longer asks before a delete; update the guard")
	}
	// No other file posts a card route by path.
	for name, src := range sourceFiles(t) {
		if name != "keelclient/agentnotebooks.go" && strings.Contains(src, "/notebooks/") && strings.Contains(src, "conversations/") {
			t.Errorf("%s names a card route: only keelclient/agentnotebooks.go does", name)
		}
	}
}

// Nothing marks a notebook reviewed but the user's Mark reviewed button (invariant 8), and the runbook draft from a notebook
// is one request whose result is saved only by the Runbooks editor's existing Save (invariant 9).
func TestOnlyMarkReviewedMarksReviewedAndADraftIsNeverSavedByTheClient(t *testing.T) {
	if got := allCallSites(t, "MarkNotebookReviewed"); len(got) != 1 || got[0].in != "markNotebookReviewed" {
		t.Errorf("MarkNotebookReviewed is called from %v", got)
	}
	for _, c := range allCallSites(t, "markNotebookReviewed") {
		if c.file != "notebookpane.go" || c.in != "notebookReview" {
			t.Errorf("markNotebookReviewed is called from %v: only the banner's Mark reviewed button may", c)
		}
	}
	if got := allCallSites(t, "DraftRunbookOfNotebook"); len(got) != 1 || got[0].file != "runbooks.go" || got[0].in != "draftRunbookOfNotebook" {
		t.Errorf("DraftRunbookOfNotebook is called from %v; once, from draftRunbookOfNotebook", got)
	}
	if got := allCallSites(t, "SaveRunbook"); len(got) != 1 || got[0].file != "runbooks.go" || got[0].in != "saveRunbook" {
		t.Errorf("SaveRunbook is called from %v; once, from saveRunbook (the editor's Save)", got)
	}
}
