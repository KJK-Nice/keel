package native

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Snippets on the fake backend: the page, the sidebar section, the palette group, and that every way to type one
// ends in the Approve before type dialog and goes no further.

func snippetBackend() *fakeBackend {
	fb := okBackend()
	fb.snippets = []keelclient.Snippet{
		{ID: "sn-1", Name: "disk usage", Command: "df -h\n", AgentUsable: true},
		{ID: "sn-2", Name: "two steps", Command: "cd /var/log\nls -la\n"},
		{ID: "sn-3", Name: "no newline", Command: "uptime"},
	}
	return fb
}

// snippetRig is a window with one session this client drives and the Snippets page open on top of it.
func snippetRig(t *testing.T, fb *fakeBackend) *rig {
	t.Helper()
	r := newRig(t, fb)
	r.tt.Frame()
	r.click(t, "host api-1")
	st := drivingState
	r.fb.conns[0].st = &st
	r.tt.Frame()
	return r
}

// The preview is lib/vaultview.js snippetPreview, with the cases of its test.
func TestSnippetPreviewIsTheBytesTypedWithTheirEnters(t *testing.T) {
	for _, c := range []struct {
		in      string
		lines   []string
		summary string
	}{
		{"", nil, "0 lines · 0 bytes. Typing always goes through Approve before type."},
		{"ls", []string{"ls"}, "1 line · 3 bytes. Typing always goes through Approve before type."},
		{"cd /\nls\n", []string{"cd /", "ls"}, "2 lines · 8 bytes. Typing always goes through Approve before type."},
	} {
		lines, summary := snippetPreview(c.in)
		if !slices.Equal(lines, c.lines) || summary != c.summary {
			t.Errorf("snippetPreview(%q) = %q, %q", c.in, lines, summary)
		}
	}
}

// The switch is saved with the snippet from the page's editor and by nothing else: the dialogs leave the flag to keel serve.
func TestTheEditorsAgentSwitchIsSavedWithThePageAndNotByTheDialog(t *testing.T) {
	fb := snippetBackend()
	r := snippetRig(t, fb)
	r.w.openPage("snippets")
	r.tt.Frame()
	r.click(t, "snippet two steps")
	e := &r.w.tab().pg.snippets.ed
	if e.agent {
		t.Fatal("the switch is on for a snippet that may not be suggested")
	}
	r.click(t, "Let the agent suggest this snippet switch")
	if !e.agent || !e.dirty() {
		t.Fatalf("the click did not turn it on: agent %v dirty %v", e.agent, e.dirty())
	}
	r.click(t, "Save")
	if len(fb.snipSaved) != 1 || fb.snipSaved[0].AgentUsable == nil || !*fb.snipSaved[0].AgentUsable {
		t.Fatalf("saved %+v", fb.snipSaved)
	}
	if got, _ := r.w.snippet("sn-2"); !got.AgentUsable || e.dirty() {
		t.Errorf("the saved snippet %+v, dirty %v", got, e.dirty())
	}
	// The row menu's Edit dialog sends no flag.
	sn, _ := r.w.snippet("sn-2")
	r.w.editSnippet(sn)
	r.tt.Frame()
	r.click(t, "Save snippet")
	if len(fb.snipSaved) != 2 || fb.snipSaved[1].AgentUsable != nil {
		t.Fatalf("the dialog's save %+v", fb.snipSaved)
	}
}

func TestSnippetsPageListsEditsAndSavesWithTheWebviewsRules(t *testing.T) {
	fb := snippetBackend()
	r := snippetRig(t, fb)
	r.w.openPage("snippets")
	r.tt.Frame()
	r.texts(t, "Snippets", "disk usage", "df -h", "two steps", "cd /var/log …", "Commands", "One command per line. Each line ends with Enter when typed.", "Type into pane…", "Save", "Delete snippet")
	// The first one is selected and the editor holds its command without the last newline.
	e := &r.w.tab().pg.snippets.ed
	if e.id != "sn-1" || e.name != "disk usage" || e.command != "df -h" {
		t.Fatalf("editor %+v", e)
	}
	// What the webview does not have is not here.
	for _, s := range []string{"Offer on hosts tagged", "Read-only", "Changes state"} {
		if r.tt.HasText(s) {
			t.Errorf("the page shows %q, which the app does not have", s)
		}
	}
	// What it has is (pages.templ): the preview of the bytes with the count, and the agent switch, on for this snippet.
	r.texts(t, "Preview of what gets typed", "1 line · 6 bytes. Typing always goes through Approve before type.", "Let the agent suggest this snippet",
		"⇥ can offer it when your words match. One-line commands without {{var}} fields only. When on, its text is sent, redacted, to TypeSafe Jev.")
	if !e.agent || e.dirty() {
		t.Fatalf("the switch is %v for a snippet that may be suggested (dirty %v)", e.agent, e.dirty())
	}
	r.click(t, "snippet two steps")
	if e.id != "sn-2" || e.command != "cd /var/log\nls -la" {
		t.Fatalf("editor %+v", e)
	}
	r.fill(t, "Snippet name", "  logs  ")
	r.fill(t, "Snippet commands", "tail -n 50 syslog")
	r.click(t, "Save")
	if len(fb.snipSaved) != 1 {
		t.Fatalf("saved %v", fb.snipSaved)
	}
	// The name is trimmed and the command ends with a newline, as /ui/forms/snippet-editor does; the form always posts
	// the agent switch (here off, as the snippet was).
	got := fb.snipSaved[0]
	if got.ID != "sn-2" || got.Name != "logs" || got.Command != "tail -n 50 syslog\n" || got.AgentUsable == nil || *got.AgentUsable {
		t.Fatalf("body %+v", got)
	}
	if e.dirty() {
		t.Error("the editor is still dirty after a save")
	}
	r.texts(t, "logs")
	// ⌘S saves too.
	r.fill(t, "Snippet commands", "ls")
	r.tt.Key(ui.Cmd, ui.KeyS)
	r.tt.Frame()
	if len(fb.snipSaved) != 2 || fb.snipSaved[1].Command != "ls\n" {
		t.Fatalf("saved %v", fb.snipSaved)
	}
}

func TestSnippetEditorKeepsUneditedWorkWhenTheListIsReadAgain(t *testing.T) {
	fb := snippetBackend()
	r := snippetRig(t, fb)
	r.w.openPage("snippets")
	r.tt.Frame()
	r.fill(t, "Snippet commands", "half typed")
	fb.snippets[0].Command = "changed elsewhere\n"
	r.w.reloadSnippets()
	r.tt.Frame()
	if e := r.w.tab().pg.snippets.ed; e.command != "half typed" {
		t.Fatalf("the poll replaced the user's text: %q", e.command)
	}
	r.w.tab().pg.snippets.ed.command = r.w.tab().pg.snippets.ed.origCommand // not dirty any more
	r.tt.Frame()
	if e := r.w.tab().pg.snippets.ed; e.command != "changed elsewhere" {
		t.Fatalf("a clean editor did not follow the server: %q", e.command)
	}
}

func TestNewSnippetDialogAndItsRefusals(t *testing.T) {
	fb := snippetBackend()
	r := snippetRig(t, fb)
	r.w.openPage("snippets")
	r.tt.Frame()
	r.click(t, "New snippet")
	r.texts(t, "New snippet", "Name", "Command", "Save snippet", "Cancel")
	r.click(t, "Save snippet")
	r.texts(t, "name and command are required")
	if len(fb.snipSaved) != 0 {
		t.Fatal("an empty form made a request")
	}
	r.fill(t, "Snippet dialog name", "who")
	r.fill(t, "Snippet dialog command", "whoami")
	r.click(t, "Save snippet")
	if len(fb.snipSaved) != 1 || fb.snipSaved[0] != (keelclient.SnippetInput{Name: "who", Command: "whoami\n"}) {
		t.Fatalf("saved %v", fb.snipSaved)
	}
	if r.w.snipDlg != nil || r.w.tab().pg.snippets.selected != "sn-new1" {
		t.Errorf("dialog %v selected %q", r.w.snipDlg, r.w.tab().pg.snippets.selected)
	}
	// A server refusal stays in the dialog with its words.
	fb.snipSaveErr = &keelclient.StatusError{Code: 400, Msg: "Give the snippet a name"}
	r.click(t, "New snippet")
	r.fill(t, "Snippet dialog name", "x")
	r.fill(t, "Snippet dialog command", "y")
	r.click(t, "Save snippet")
	r.texts(t, "Give the snippet a name")
	if r.w.snipDlg == nil {
		t.Fatal("the dialog closed on a refusal")
	}
	r.press(0, ui.KeyEscape)
	if r.w.snipDlg != nil {
		t.Error("Escape did not close")
	}
}

func TestEditSnippetFromTheRowMenuHoldsTheCommandAsSaved(t *testing.T) {
	fb := snippetBackend()
	r := snippetRig(t, fb)
	r.w.openPage("snippets")
	r.tt.Frame()
	if got := menuLabels(t, r, "snippet disk usage"); !slices.Equal(got, []string{"Type into pane…", "Copy", "-", "Edit", "-", "Delete"}) {
		t.Fatalf("row menu %v", got)
	}
	choose(t, r, "Edit")
	if r.w.snipDlg == nil || r.w.snipDlg.id != "sn-1" || r.w.snipDlg.command != "df -h\n" {
		t.Fatalf("dialog %+v", r.w.snipDlg)
	}
	r.texts(t, "Edit snippet")
	if len(r.fb.conns[0].injected) != 0 {
		t.Fatal("a menu typed")
	}
}

func TestDeleteSnippetAsksWithTheWebviewsWordsAndCancelDeletesNothing(t *testing.T) {
	fb := snippetBackend()
	r := snippetRig(t, fb)
	r.w.openPage("snippets")
	r.tt.Frame()
	r.click(t, "Delete snippet")
	if r.w.confirm == nil || !r.tt.HasText("Delete snippet “disk usage”?") {
		t.Fatal("no confirm")
	}
	r.click(t, "Cancel")
	if len(fb.snipDeleted) != 0 {
		t.Fatal("Cancel deleted")
	}
	r.click(t, "Delete snippet")
	r.click(t, "Remove")
	if !slices.Equal(fb.snipDeleted, []string{"sn-1"}) || r.tt.HasText("disk usage") {
		t.Fatalf("deleted %v", fb.snipDeleted)
	}
	// From the menu too.
	menuLabels(t, r, "snippet two steps")
	choose(t, r, "Delete")
	r.texts(t, "Delete snippet “two steps”?")
}

func TestEveryEntryPointOpensTheDialogAndNoneTypes(t *testing.T) {
	entries := map[string]func(t *testing.T, r *rig){
		"page row menu": func(t *testing.T, r *rig) {
			r.w.openPage("snippets")
			r.tt.Frame()
			menuLabels(t, r, "snippet two steps")
			choose(t, r, "Type into pane…")
		},
		"editor button": func(t *testing.T, r *rig) {
			r.w.openPage("snippets")
			r.tt.Frame()
			r.click(t, "snippet two steps")
			r.click(t, "Type into pane…")
		},
		"sidebar row": func(t *testing.T, r *rig) {
			r.click(t, "Snippets")
			r.click(t, "sidebar snippet two steps")
		},
		"sidebar row menu": func(t *testing.T, r *rig) {
			r.click(t, "Snippets")
			menuLabels(t, r, "sidebar snippet two steps")
			choose(t, r, "Type into pane…")
		},
		"palette": func(t *testing.T, r *rig) {
			r.openPal(t)
			r.typePal("two steps")
			r.press(0, ui.KeyEnter)
		},
	}
	for name, enter := range entries {
		r := snippetRig(t, snippetBackend())
		enter(t, r)
		if r.w.approve == nil {
			t.Errorf("%s: no Approve dialog", name)
			continue
		}
		if got, _ := r.w.approve.source(); got != "cd /var/log\nls -la\n" {
			t.Errorf("%s: dialog holds %q", name, got)
		}
		r.tt.Frame()
		if !r.tt.HasText("Type into the terminal?") || !r.tt.HasText("cd /var/log") {
			t.Errorf("%s: the dialog does not show the command", name)
		}
		if len(r.fb.conns[0].injected) != 0 || r.fb.conns[0].wrote != 0 {
			t.Errorf("%s typed %v", name, r.fb.conns[0].injected)
		}
		if r.fb.calls[len(r.fb.calls)-1] == "save snippet" {
			t.Errorf("%s saved something", name)
		}
	}
}

func TestTypeIntoPaneFromAPageNamesThePaneYouCanSeeAndSendsTheSnippet(t *testing.T) {
	r := snippetRig(t, snippetBackend())
	r.w.openPage("snippets")
	r.tt.Frame()
	if r.w.tab().panes != nil && len(r.w.tab().panes) != 0 {
		t.Fatal("the page has panes")
	}
	r.click(t, "snippet no newline")
	r.click(t, "Type into pane…")
	if r.w.active != 0 {
		t.Fatalf("active tab %d: the session tab was not selected first", r.w.active)
	}
	r.click(t, "Type")
	// A command with no newline is typed with one, behind Ctrl-U.
	if got := r.fb.conns[0].injected; !slices.Equal(got, []string{"\x15uptime\n"}) {
		t.Fatalf("sent %q", got)
	}
}

func TestSnippetBracesAreTypedAsTheyAreSaved(t *testing.T) {
	// {{var}} fields are not in the app: a snippet with braces is typed with them, unchanged.
	fb := snippetBackend()
	fb.snippets = []keelclient.Snippet{{ID: "sn-v", Name: "braces", Command: "echo {{name}} {{greeting}}\n"}}
	r := snippetRig(t, fb)
	r.w.typeSnippet("sn-v")
	r.tt.Frame()
	r.texts(t, "echo {{name}} {{greeting}}")
	r.click(t, "Type")
	if got := r.fb.conns[0].injected; !slices.Equal(got, []string{"\x15echo {{name}} {{greeting}}\n"}) {
		t.Fatalf("sent %q", got)
	}
}

func TestNoSessionMeansNoDialogFromASnippetRow(t *testing.T) {
	r := newRig(t, snippetBackend())
	r.tt.Frame()
	r.click(t, "Snippets")
	r.click(t, "sidebar snippet disk usage")
	if r.w.approve != nil || !r.tt.HasText("Open a session first") {
		t.Fatalf("dialog %v, toast shown %v", r.w.approve, r.tt.HasText("Open a session first"))
	}
}

func TestSidebarSnippetsSectionListsEverySnippetAndFilters(t *testing.T) {
	r := snippetRig(t, snippetBackend())
	if !r.has("Vault section") || r.has("Filter snippets") {
		t.Fatal("the segmented control is missing, or Snippets shows first")
	}
	r.click(t, "Snippets")
	r.texts(t, "disk usage", "df -h", "two steps", "no newline", "Manage snippets")
	if !r.has("Filter snippets") || r.has("Filter hosts") || r.tt.HasText("pg-primary") {
		t.Fatal("the sidebar did not switch to Snippets")
	}
	r.click(t, "Filter snippets")
	r.tt.Type("log")
	r.tt.Frame()
	if r.tt.HasText("disk usage") || !r.tt.HasText("two steps") { // matches the command, "cd /var/log"
		t.Error("the filter does not match name and command")
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("zzz")
	r.tt.Frame()
	r.texts(t, "Nothing matches “zzz”")
	r.click(t, "Manage snippets")
	if r.w.tab().pg == nil || r.w.tab().pg.snippets == nil {
		t.Fatal("Manage snippets did not open the page")
	}
	r.click(t, "Hosts")
	r.texts(t, "pg-primary")
}

func TestSidebarSnippetsEmpty(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.click(t, "Snippets")
	r.texts(t, "No snippets yet", "Manage snippets")
}

func TestPaletteSnippetsGroupFollowsActionsAndBadgesOnlyThoseThatWillAsk(t *testing.T) {
	r := snippetRig(t, snippetBackend())
	r.openPal(t)
	var groups []string
	for _, it := range r.rows() {
		if len(groups) == 0 || groups[len(groups)-1] != it.group {
			groups = append(groups, it.group)
		}
	}
	if !slices.Equal(groups, []string{"Open now", "Hosts", "Actions", "Snippets", "Notebooks"}) {
		t.Fatalf("groups %v", groups)
	}
	var rows []string
	for _, it := range r.rows() {
		if it.group == "Snippets" {
			// "Approve first" only where the dialog is certain (term.js snippetBadge): two lines. The one-line read-only
			// ones may type with no dialog.
			wantBadge := ""
			if it.title == "Type snippet: two steps" {
				wantBadge = "Approve first"
			}
			if it.badge != wantBadge || it.icon != "snippet" {
				t.Errorf("%s: badge %q (want %q) icon %q", it.title, it.badge, wantBadge, it.icon)
			}
			rows = append(rows, it.title+" | "+it.hint)
		}
	}
	want := []string{"Type snippet: disk usage | df -h", "Type snippet: two steps | cd /var/log", "Type snippet: no newline | uptime"}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows %q", rows)
	}
	r.tt.Frame()
	r.texts(t, "Approve first")
	// The Snippets page is an action of the Vault section, between Keychain and Logs.
	var vault []string
	for _, it := range r.w.pal.items {
		if it.group == "Actions" && it.hint == "Vault" {
			vault = append(vault, it.title)
		}
	}
	i, j, k := slices.Index(vault, "Keychain"), slices.Index(vault, "Snippets"), slices.Index(vault, "Logs")
	if !(i >= 0 && i < j && j < k) {
		t.Fatalf("vault actions %v", vault)
	}
	// ↵ on a snippet opens the dialog, not the terminal.
	r.typePal("Type snippet: disk")
	r.press(0, ui.KeyEnter)
	if r.w.approve == nil || r.w.pal != nil {
		t.Fatalf("dialog %v palette %v", r.w.approve, r.w.pal)
	}
	if len(r.fb.conns[0].injected) != 0 {
		t.Fatal("the palette typed")
	}
}

func TestSnippetsPageIsSavedAndComesBackAndFooterStaysKeychainAndLogs(t *testing.T) {
	r := snippetRig(t, snippetBackend())
	if !r.has("Keychain") || !r.has("Logs") {
		t.Fatal("footer")
	}
	r.openPal(t)
	r.typePal("Snippets")
	for i, it := range r.rows() {
		if it.group == "Actions" && it.title == "Snippets" {
			r.w.pal.cursor = i
		}
	}
	r.press(0, ui.KeyEnter)
	if r.w.tab().pg == nil || r.w.tab().pg.name != "snippets" || len(r.w.tabs) != 2 {
		t.Fatalf("tabs %d", len(r.w.tabs))
	}
	doc := r.w.snapshot()
	if !slices.Equal(doc.Pages, []string{"snippets"}) || doc.ActivePage != "snippets" {
		t.Fatalf("saved %+v", doc)
	}
	// ⌘N on the page is New snippet, not New host.
	r.press(ui.Cmd, ui.KeyN)
	if r.w.snipDlg == nil || r.w.hostDlg != nil {
		t.Fatal("⌘N on Snippets did not open New snippet")
	}
	if strings.Contains(r.w.tab().name(), "Snippet ") {
		t.Fatal("tab name")
	}
}
