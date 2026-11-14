//go:build nativee2e

package native

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Notebooks and the agent against a real keel serve with the fake provider (KEEL_AGENT_PROVIDER=fake, made-up key only; no
// model is reached). The questions are: "read / edit / drop the notebook <title in
// lower case>" makes the fake read, edit (keel-edit-me becomes keel-edited-by-agent) or propose deleting it, and "write this
// up" makes it create a notebook (named agentDraftName) with a shell fence. The daemon decides everything; these tests look
// at the server for every effect.

const (
	agentDraftName = "keel-notebook-e2e draft"
	nbFakeToken    = "API_TOKEN=Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK" // secret-looking and made up: keel serve must redact it
)

// closeToasts closes the toasts, which sit over the composer.
func (e *win) closeToasts() {
	e.t.Helper()
	for range 6 {
		if e.tt.Click("Close notification") != nil {
			return
		}
		e.step()
	}
}

// setNotebookPrivate is the user's Private switch, through the API.
func (e *win) setNotebookPrivate(id string, private bool) {
	e.t.Helper()
	if code, body := e.raw("PUT", "/api/notebooks/"+id+"/private", map[string]bool{"private": private}); code != 200 {
		e.t.Fatalf("PUT private: %d %s", code, body)
	}
	e.w.reloadNotebooks()
	e.pump(func() bool { nb, ok := e.w.notebook(id); return ok && nb.Private == private }, "the window to know the notebook is private")
}

// trashNames lists the names in Recently deleted on the server.
func (e *win) trashNames() (out []string) {
	e.t.Helper()
	code, body := e.raw("GET", "/api/notebook-trash", nil)
	if code != 200 {
		e.t.Fatalf("GET /api/notebook-trash: %d %s", code, body)
	}
	var res struct {
		Versions []keelclient.NotebookVersion `json:"versions"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		e.t.Fatal(err)
	}
	for _, v := range res.Versions {
		out = append(out, v.Name)
	}
	return out
}

func (e *win) serverHas(id string) bool {
	e.t.Helper()
	for _, n := range e.serverNotebooks() {
		if n.ID == id {
			return true
		}
	}
	return false
}

func (e *win) serverByName(name string) (keelclient.Notebook, bool) {
	e.t.Helper()
	for _, n := range e.serverNotebooks() {
		if n.Name == name {
			return n, true
		}
	}
	return keelclient.Notebook{}, false
}

func (e *win) setupAgentNB() string {
	e.t.Helper()
	e.putAgentKey()
	sid := e.openLocal()
	e.openAgent()
	return sid
}

func TestE2EAgentNotebookAttach(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.setupAgentNB()
	id := e.apiNotebook("keel-nb-e2e attached", "## Deploy\n\n"+nbFakeToken+"\n\n```sh\nsystemctl status app\n```")
	e.apiNotebook("keel-nb-e2e other", "NOT-ATTACHED")
	priv := e.apiNotebook("keel-nb-e2e private", "PRIVATE-E2E-BODY")
	e.setNotebookPrivate(priv, true)

	// Asked with no notebook attached: none is sent.
	e.settledAsk("is this machine fine? no command")
	if strings.Contains(e.texts(), "attached notebook") {
		t.Fatalf("a notebook was sent with nothing attached: %q", e.texts())
	}

	// Attach notebook: the picker lists the saved ones, a private one cannot be ticked.
	e.click("Attach notebook")
	e.pump(func() bool {
		return e.hasLabel("pick notebook keel-nb-e2e attached") && e.hasLabel("pick notebook keel-nb-e2e private")
	}, "the picker")
	e.shot("TestE2EAgentNotebookAttach-picker")
	e.click("pick notebook keel-nb-e2e private")
	e.pump(func() bool { return e.tt.HasText(privateRefused) }, "the reason a private notebook is not attached")
	if len(e.w.agent.attached) != 0 {
		t.Fatalf("a private notebook was attached: %v", e.w.agent.attached)
	}
	e.click("pick notebook keel-nb-e2e attached")
	e.key(0, ui.KeyEscape)
	e.closeToasts()
	e.pump(func() bool { return e.hasLabel("Stop sending keel-nb-e2e attached") }, "the chip")

	e.settledAsk("what does this notebook do? no command")
	if !e.tt.HasText("I was also given 1 attached notebook.") && !strings.Contains(e.texts(), "I was also given 1 attached notebook.") {
		t.Fatalf("the fake was not given the attached notebook: %q", e.texts())
	}
	sent := regexp.MustCompile(`Sent notebook keel-nb-e2e attached \(\d+ bytes, [1-9]\d* redacted\)`)
	if !sent.MatchString(e.texts()) {
		t.Fatalf("no trace line for the notebook sent, redacted: %q", e.texts())
	}
	e.shot("TestE2EAgentNotebookAttach-trace")

	// keel.log: an agent.read with the notebook's id and size, never its name or text.
	read, leaked := false, false
	for _, l := range e.logEntries() {
		if l.Kind == "agent.read" && strings.Contains(l.Message, id) {
			read = true
		}
		if strings.HasPrefix(l.Kind, "agent.") && (strings.Contains(l.Message, "keel-nb-e2e attached") || strings.Contains(l.Message, "Zx81Kq0") || strings.Contains(l.Message, "PRIVATE-E2E-BODY")) {
			leaked = true
		}
	}
	if !read || leaked {
		t.Fatalf("log: read row %v, name or text in a row %v", read, leaked)
	}
	if strings.Contains(e.texts(), "Zx81Kq0") {
		t.Fatal("the secret-looking token is on screen")
	}

	// Removing the chip stops sending it.
	e.click("Stop sending keel-nb-e2e attached")
	n := strings.Count(e.texts(), "I was also given 1 attached notebook.")
	e.settledAsk("and now? no command")
	if strings.Count(e.texts(), "I was also given 1 attached notebook.") != n {
		t.Fatal("the notebook was sent after its chip was removed")
	}
}

func TestE2EAgentCreatesANotebook(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.setupAgentNB()
	e.settledAsk("write this up")
	e.pump(func() bool { return e.hasLabel("Created notebook") }, "the card Created notebook")
	nb, ok := e.serverByName(agentDraftName)
	if !ok || nb.Source != byAgent || !nb.AgentUnreviewed {
		t.Fatalf("the notebook the agent made: %+v (on the server %v)", nb, ok)
	}
	for _, s := range []string{"Created by the agent and saved to Notebooks, marked From agent.", "Open " + agentDraftName, "Undo " + agentDraftName} {
		if !e.tt.HasText(s) && !e.hasLabel(s) {
			t.Errorf("the card lacks %q: %q", s, e.tt.Texts())
		}
	}
	e.shot("TestE2EAgentCreatesANotebook-card")

	// In the sidebar and on the page, marked From agent.
	e.click("Snippets")
	e.pump(func() bool { return e.hasLabel("sidebar notebook " + agentDraftName) }, "the sidebar row")
	e.w.openPage("notebooks")
	e.pump(func() bool { return e.hasLabel("notebook "+agentDraftName) && e.hasLabel("badge From agent") }, "the page row marked From agent")

	// Open: the banner says the agent wrote it.
	e.click("Open " + agentDraftName)
	e.pump(func() bool { return e.tt.HasText("Written by the agent") && e.tt.HasText("Mark reviewed") }, "the banner")
	e.shot("TestE2EAgentCreatesANotebook-opened")
	if !e.serverHas(nb.ID) {
		t.Fatal("opening deleted it")
	}

	// Undo moves it to Recently deleted.
	e.click("Undo " + agentDraftName)
	e.pump(func() bool { return !e.serverHas(nb.ID) }, "the notebook gone from the server's list")
	if names := e.trashNames(); len(names) != 1 || names[0] != agentDraftName {
		t.Fatalf("Recently deleted holds %v", names)
	}
	e.pump(func() bool {
		return e.tt.HasText("Undone. The notebook is in Notebooks · Recently deleted.") && len(e.w.notebookPanes()) == 0
	}, "the card says so and the pane closed")
}

func TestE2EAgentEditsANotebookAndUndo(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.setupAgentNB()
	const original = "## Steps\n\nkeel-edit-me\n\n```sh\necho step\n```"
	id := e.apiNotebook("keel-nb-e2e edit", original)
	np := e.openNotebookFromSidebar("keel-nb-e2e edit")
	e.pump(func() bool { return e.tt.HasText("keel-edit-me") }, "the notebook on screen")

	e.settledAsk("edit the notebook keel-nb-e2e edit")
	e.pump(func() bool { return e.hasLabel("Edited notebook") }, "the card Edited notebook")
	if got := e.serverNotebook(id).Markdown(); !strings.Contains(got, "keel-edited-by-agent") || strings.Contains(got, "keel-edit-me") {
		t.Fatalf("the server holds %q", got)
	}
	// The open pane shows the new text and the banner.
	e.pump(func() bool {
		return strings.Contains(np.nb.md, "keel-edited-by-agent") && e.tt.HasText("Edited by the agent") && e.tt.HasText("Mark reviewed")
	}, "the pane showing the agent's edit")
	if !e.tt.HasText("Keel kept the version before it") && !e.onScreenText("Keel kept the version before it") {
		t.Errorf("the card lacks its status: %q", e.tt.Texts())
	}
	e.shot("TestE2EAgentEditsANotebookAndUndo-edited")

	// Undo on the card: the user's text is back on the server and on screen.
	e.click("Undo keel-nb-e2e edit")
	e.pump(func() bool { return e.serverNotebook(id).Markdown() == original }, "the old text on the server")
	e.pump(func() bool {
		return np.nb.md == original && e.tt.HasText("Undone. The notebook is back to the version before the agent's edit.")
	}, "the old text in the pane")
	e.shot("TestE2EAgentEditsANotebookAndUndo-undone")
}

// onScreenText says whether any text on screen holds s.
func (e *win) onScreenText(s string) bool { return strings.Contains(e.texts(), s) }

// nbSaveFail is a window's backend whose notebook saves fail while on is set: the typing then stays only in the window.
// Everything else, the ask included, goes to the real keel serve.
type nbSaveFail struct {
	backend
	on atomic.Bool
}

func (b *nbSaveFail) SaveNotebook(ctx context.Context, id string, in keelclient.NotebookSave) (keelclient.SavedNotebook, error) {
	if b.on.Load() {
		return keelclient.SavedNotebook{}, &keelclient.StatusError{Code: 503, Msg: "keel serve is busy"}
	}
	return b.backend.SaveNotebook(ctx, id, in)
}

func TestE2EAgentEditRefusedWhileTyping(t *testing.T) {
	addr, dir := startServe(t, true)
	var fail *nbSaveFail
	e := newWin(t, addr, dir, func(b backend) backend { fail = &nbSaveFail{backend: b}; return fail })
	e.setupAgentNB()
	const original = "## Steps\n\nkeel-edit-me\n"
	id := e.apiNotebook("keel-nb-e2e typing", original)
	np := e.openNotebookFromSidebar("keel-nb-e2e typing")
	e.pump(func() bool { return e.tt.HasText("keel-edit-me") }, "the notebook on screen")

	// Typing that cannot be saved right now stays only in the window. The ask names the notebook (Editing), and keel serve
	// refuses the agent's edit to it.
	fail.on.Store(true)
	e.click("Edit")
	e.pump(func() bool { return e.tt.Focused("Markdown document") }, "the text in Edit")
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type(original + "typed-by-user\n")
	e.step()
	e.pumpClock(func() bool { return np.nb.failed }, "the save to fail")
	e.closeToasts()

	e.settledAsk("edit the notebook keel-nb-e2e typing")
	e.pump(func() bool { return e.hasLabel("Notebook not changed") }, "the card Notebook not changed")
	if !e.tt.HasText("You were typing in it, so Keel did not apply the agent's edit.") {
		t.Fatalf("the card does not say why: %q", e.tt.Texts())
	}
	if got := e.serverNotebook(id).Markdown(); got != original {
		t.Fatalf("the server holds %q: the agent's edit, or the typing, landed", got)
	}
	if !strings.Contains(np.nb.md, "typed-by-user") {
		t.Fatal("the typing is gone from the pane")
	}
	e.shot("TestE2EAgentEditRefusedWhileTyping")

	// The typing is intact and saves once keel serve takes a save again: the next edit carries it.
	fail.on.Store(false)
	e.click("Markdown document") // the keyboard went to the composer with the ask
	e.tt.Type(" more")
	e.step()
	e.pumpClock(func() bool { return strings.Contains(e.serverNotebook(id).Markdown(), "typed-by-user") }, "the typing saved")
	if got := e.serverNotebook(id).Markdown(); strings.Contains(got, "keel-edited-by-agent") {
		t.Fatalf("the refused edit landed: %q", got)
	}
}

func TestE2EAgentProposesADelete(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.setupAgentNB()
	id := e.apiNotebook("keel-nb-e2e drop", "DROP-ME")

	e.settledAsk("drop the notebook keel-nb-e2e drop")
	e.pump(func() bool { return e.hasLabel("Delete notebook?") }, "the card Delete notebook?")
	if !e.tt.HasText("The agent asks to delete this notebook. Nothing is deleted unless you confirm.") || !e.serverHas(id) {
		t.Fatalf("the proposal deleted something, or the card lacks its words: %q", e.tt.Texts())
	}
	e.shot("TestE2EAgentProposesADelete-card")
	e.settle(time.Second)
	if !e.serverHas(id) {
		t.Fatal("the notebook went before any confirm")
	}

	// Delete… asks; Cancel keeps it.
	e.click("Delete… keel-nb-e2e drop")
	q := "Delete the notebook “keel-nb-e2e drop”? The agent asked for this. It goes to Notebooks · Recently deleted, where you can restore it for 30 days."
	e.pump(func() bool { return e.tt.HasText(q) }, "the question")
	e.shot("TestE2EAgentProposesADelete-confirm")
	e.click("Cancel")
	e.settle(500 * time.Millisecond)
	if !e.serverHas(id) {
		t.Fatal("Cancel deleted it")
	}
	// Keep: nothing is deleted and the card says so.
	e.click("Keep keel-nb-e2e drop")
	e.pump(func() bool { return e.tt.HasText("Kept. Nothing was deleted.") }, "Kept")
	if !e.serverHas(id) {
		t.Fatal("Keep deleted it")
	}

	// A second proposal: Delete, then Restore.
	e.settledAsk("drop the notebook keel-nb-e2e drop")
	e.pump(func() bool {
		return e.tt.HasText("The agent asks to delete this notebook. Nothing is deleted unless you confirm.")
	}, "the second card")
	e.click("Delete… keel-nb-e2e drop")
	e.pump(func() bool { return e.tt.HasText(q) }, "the question again")
	e.click("Delete")
	e.pump(func() bool { return !e.serverHas(id) }, "deleted on the server")
	e.pump(func() bool { return e.tt.HasText("Deleted. It is in Notebooks · Recently deleted.") }, "the card says so")
	if names := e.trashNames(); len(names) != 1 || names[0] != "keel-nb-e2e drop" {
		t.Fatalf("Recently deleted holds %v", names)
	}
	e.click("Restore keel-nb-e2e drop")
	e.pump(func() bool { return e.serverHas(id) }, "restored on the server")
	e.pump(func() bool { return e.tt.HasText("Restored to Notebooks.") }, "the card says so")
	if got := e.serverNotebook(id).Markdown(); got != "DROP-ME" {
		t.Fatalf("restored text %q", got)
	}
	found := false
	for _, l := range e.logEntries() {
		if l.Kind == "agent.notebook" && strings.Contains(l.Message, "deleted by you, as the agent proposed") && strings.Contains(l.Message, id) {
			found = true
		}
	}
	if !found {
		t.Error("no log row for the confirmed delete")
	}
}

func TestE2EAgentNeverSeesAPrivateNotebook(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.setupAgentNB()
	const marker = "PRIVATE-E2E-BODY-keel-edit-me-0451"
	const pubMarker = "PUBLIC-E2E-BODY"
	e.apiNotebook("keel-nb-e2e public", pubMarker)
	priv := e.apiNotebook("keel-nb-e2e private", marker)
	e.setNotebookPrivate(priv, true)

	// "Ask the agent about this notebook" does not exist for it; a forced attach is refused by the client.
	e.w.askAboutNotebook(priv)
	e.step()
	if len(e.w.agent.attached) != 0 {
		t.Fatalf("a private notebook was attached: %v", e.w.agent.attached)
	}
	e.closeToasts()

	// Listing and reading: the agent finds no such notebook, and the index counts only the public one.
	e.settledAsk("read the notebook keel-nb-e2e private")
	e.settledAsk("edit the notebook keel-nb-e2e private")
	for _, s := range []string{"found no such notebook", "I found no such notebook to edit.", "I was given the titles of 1 notebooks", "Sent the titles of 1 notebook (not private)"} {
		if !strings.Contains(e.texts(), s) {
			t.Errorf("the thread lacks %q: %q", s, e.texts())
		}
	}
	e.shot("TestE2EAgentNeverSeesAPrivateNotebook")
	if got := e.serverNotebook(priv).Markdown(); got != marker {
		t.Fatalf("the private notebook changed: %q", got)
	}
	// The marker is in no trace line, no card, no window text and no log row (the private notebook's id too).
	if all := e.texts(); strings.Contains(all, marker) || strings.Contains(all, "keel-nb-e2e private") && strings.Contains(all, "Sent notebook keel-nb-e2e private") {
		t.Fatalf("the private notebook is on screen: %q", all)
	}
	if len(e.w.agent.nbProps) != 0 {
		t.Fatalf("a card about a private notebook: %v", e.w.agent.nbProps)
	}
	for _, l := range e.logEntries() {
		if strings.Contains(l.Message, marker) || strings.HasPrefix(l.Kind, "agent.") && strings.Contains(l.Message, priv) {
			t.Fatalf("a log row names the private notebook: %+v", l)
		}
	}
	// A client that sent the id anyway (the test does, by hand) is refused by keel serve, and nothing of the notebook is sent.
	code, body := e.raw("POST", "/api/agent/ask", map[string]any{"conversationId": "conv-private-1", "question": "what is in it? no command", "panes": []string{}, "notebooks": []string{priv}})
	if code != 200 || strings.Contains(body, marker) || !strings.Contains(body, "notebook-private") || strings.Contains(body, "I was also given 1 attached notebook") {
		t.Fatalf("a forced ask: %d %s", code, body)
	}
	// The fake provider's own record of what it was given is in the daemon's memory and is not exposed through the API, so
	// the proof here is what the fake said about it (it was given no attached notebook, and an index of one title).
}

func TestE2ERunbookDraftFromANotebook(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.pump(func() bool { return e.tt.HasText("Local Shell") }, "the host tree")
	e.apiNotebook("keel-nb-e2e steps", "Intro words.\n\n## Check disk\n\n```sh\ndf -h\n```\n\n## Check load\n\n```sh\nuptime\n```\n")
	e.w.openPage("runbooks")
	e.pump(func() bool { return e.hasLabel("From a notebook…") }, "the Runbooks page")
	e.click("From a notebook…")
	e.pump(func() bool { return e.hasLabel("draft from notebook keel-nb-e2e steps") }, "the notebook list")
	e.click("draft from notebook keel-nb-e2e steps")
	e.pump(func() bool {
		p := e.w.runbooksPage()
		return p != nil && p.ed != nil && len(p.ed.steps) == 2
	}, "the draft in the editor")
	p := e.w.runbooksPage()
	if p.ed.steps[0].title != "Check disk" || p.ed.steps[0].command != "df -h" || p.ed.steps[1].title != "Check load" || p.ed.steps[1].command != "uptime" {
		t.Fatalf("the steps: %+v", p.ed.steps)
	}
	if p.ed.agentMayPick {
		t.Fatal("a draft starts with Agent may pick on")
	}
	runbooks := func() int {
		code, body := e.raw("GET", "/api/agent/runbooks", nil)
		if code != 200 {
			e.t.Fatalf("GET runbooks: %d %s", code, body)
		}
		var l keelclient.RunbookList
		if err := json.Unmarshal([]byte(body), &l); err != nil {
			e.t.Fatal(err)
		}
		return len(l.Runbooks)
	}
	e.settle(time.Second)
	if runbooks() != 0 {
		t.Fatal("the draft was saved before Save")
	}
	e.shot("TestE2ERunbookDraftFromANotebook")
	e.key(ui.Cmd, ui.KeyS)
	e.pump(func() bool { return runbooks() == 1 }, "the runbook saved by Save")
}

func TestE2EUnreviewedNotebookFenceGoesThroughTheDialog(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWinH(t, addr, dir, nil, 2400) // the fence's button is far down once the agent's banner and card are in
	sid := e.setupAgentNB()
	id := e.apiNotebook("keel-nb-e2e fence", "keel-edit-me\n\n```sh\ndf -h\n```\n")
	np := e.openNotebookFromSidebar("keel-nb-e2e fence")
	e.pump(func() bool { return e.tt.HasText("df -h") }, "the notebook beside the shell")

	e.settledAsk("edit the notebook keel-nb-e2e fence")
	e.pump(func() bool {
		return e.serverNotebook(id).AgentUnreviewed && strings.Contains(np.nb.md, "keel-edited-by-agent") && e.tt.HasText("Mark reviewed")
	}, "the agent's edit and the banner")
	fs := fences(np.nb.md)
	if len(fs) != 1 {
		t.Fatalf("fences %+v", fs)
	}

	// After an agent edit a one-line fence is not typed directly: the dialog opens with the note, and nothing is typed.
	e.click(fenceName("Type into Local Shell", fs[0].Index))
	e.waitApprove()
	if !e.tt.HasText(unreviewedNote) {
		t.Fatalf("no note in the dialog: %q", e.tt.Texts())
	}
	e.shot("TestE2EUnreviewedNotebookFenceGoesThroughTheDialog")
	e.settle(time.Second)
	if strings.Contains(e.scrollback(sid), "df -h") {
		t.Fatal("typed before Type")
	}
	e.click("Cancel")
	e.settle(500 * time.Millisecond)
	if strings.Contains(e.scrollback(sid), "df -h") {
		t.Fatal("Cancel typed")
	}

	// Mark reviewed clears it; the same fence then types directly, with no dialog and no Enter.
	e.click("Mark reviewed")
	e.pump(func() bool { return !e.serverNotebook(id).AgentUnreviewed }, "reviewed on the server")
	e.click(fenceName("Type into Local Shell", fs[0].Index))
	e.pump(func() bool { return strings.Contains(e.scrollback(sid), "df -h") }, "df -h on the prompt line")
	e.settle(time.Second)
	e.noDialog("a reviewed one-line fence")
	if strings.Contains(e.scrollback(sid), "Filesystem") {
		t.Fatal("the command ran without Enter")
	}
}
