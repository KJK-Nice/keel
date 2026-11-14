package native

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Type without the dialog on the fake backend (typeNow): what is asked of keel serve, with which bytes, and what the
// window does with each answer. The fake session records every DirectType and every ApprovedInject apart.

// screen gives the focused pane the terminal state a real one would read (a placeholder body has none).
func (r *rig) screen(alt bool, line string) {
	r.w.focusedPane().screen = func() (bool, string) { return alt, line }
}

// directRig is a window with one session this client drives, at its shell's prompt, and the four snippets below.
func directRig(t *testing.T) *rig {
	t.Helper()
	fb := snippetBackend()
	fb.snippets = append(fb.snippets,
		keelclient.Snippet{ID: "sn-4", Name: "echo one", Command: "echo not-in-grammar\n"},
		keelclient.Snippet{ID: "sn-5", Name: "ls home", Command: "ls /home/me\n"},
	)
	r := snippetRig(t, fb)
	r.screen(false, "user@h $")
	r.fb.conns[0].directRes = keelclient.DirectResult{Typed: true}
	return r
}

func (r *rig) directs() []fakeDirect { return r.fb.conns[0].directs }

func TestAOneLineReadOnlySnippetIsTypedWithNoDialogAndTheBareCommand(t *testing.T) {
	r := directRig(t)
	r.w.typeSnippet("sn-1") // "df -h\n"
	r.tt.Frame()
	got := r.directs()
	if len(got) != 1 {
		t.Fatalf("directs %v", got)
	}
	d := got[0]
	if d.command != "df -h" || strings.ContainsAny(d.command, "\x15\r\n") || d.agent != nil {
		t.Errorf("command %q agent %v: the snippet's text, with no Ctrl-U and no newline", d.command, d.agent)
	}
	if d.ref.Kind != "snippet" || d.ref.SnippetID != "sn-1" || d.ref.AltScreen || d.ref.PromptLine != "user@h $" {
		t.Errorf("ref %+v", d.ref)
	}
	if r.w.approve != nil || len(r.typed()) != 0 || r.fb.conns[0].wrote != 0 {
		t.Errorf("a dialog or another writer was used: approve %v typed %v wrote %d", r.w.approve != nil, r.typed(), r.fb.conns[0].wrote)
	}
	if !r.tt.HasText("Typed into api-1. Press Enter to run.") {
		t.Errorf("no note: %v", r.tt.Texts())
	}
	r.tt.Frame()
	if !r.tt.Focused("body 1") {
		t.Error("the terminal does not have the keyboard to review the line")
	}
	// A snippet with no trailing newline is the same text.
	r.w.typeSnippet("sn-3")
	if got := r.directs(); len(got) != 2 || got[1].command != "uptime" {
		t.Fatalf("directs %v", got)
	}
}

func TestEveryEntryPointOfASnippetGoesThroughTheSameDirectPath(t *testing.T) {
	r := directRig(t)
	var run func(bool)
	for _, it := range r.w.paletteItems() {
		if it.title == "Type snippet: disk usage" {
			run = it.run
		}
	}
	if run == nil {
		t.Fatal("no palette row")
	}
	run(false)
	r.w.openPage("snippets")
	r.tt.Frame()
	r.click(t, "Type into pane…") // the page's button, for the selected snippet
	if got := r.directs(); len(got) != 2 || got[0].command != "df -h" || got[1].command != "df -h" || r.w.approve != nil {
		t.Fatalf("directs %v approve %v", got, r.w.approve != nil)
	}
}

func TestASnippetOutsideTheGrammarOrOnSeveralLinesOpensTheDialogAndAsksNothing(t *testing.T) {
	r := directRig(t)
	for _, id := range []string{"sn-2", "sn-4", "sn-5"} { // two lines; echo; a home directory
		r.w.typeSnippet(id)
		r.tt.Frame()
		if r.w.approve == nil || !r.has("dialog "+approveTitle) {
			t.Fatalf("%s: no dialog", id)
		}
		r.click(t, "Cancel")
	}
	if len(r.directs()) != 0 || len(r.typed()) != 0 {
		t.Fatalf("directs %v typed %v: nothing is asked or typed before Type", r.directs(), r.typed())
	}
	// Type in the dialog is the old path, with Ctrl-U.
	r.w.typeSnippet("sn-4")
	r.tt.Frame()
	r.click(t, "Type")
	if got := r.typed(); !slices.Equal(got, []string{"\x15echo not-in-grammar\n"}) {
		t.Fatalf("typed %q", got)
	}
}

func TestAServerDialogAnswerOpensTheDialogAndTypesNothingItself(t *testing.T) {
	r := directRig(t)
	r.fb.conns[0].directRes = keelclient.DirectResult{Dialog: true, Reason: "Keel cannot check this command here"}
	r.w.typeSnippet("sn-1")
	r.tt.Frame()
	if len(r.directs()) != 1 || r.w.approve == nil || len(r.typed()) != 0 {
		t.Fatalf("directs %v approve %v typed %v", r.directs(), r.w.approve != nil, r.typed())
	}
	if r.tt.HasText("Press Enter to run.") {
		t.Error("it says it typed")
	}
}

func TestAServerRefusalShowsItsMessageAndNothingElseHappens(t *testing.T) {
	r := directRig(t)
	r.fb.conns[0].directRes = keelclient.DirectResult{Message: "you follow this pane: take or request control to type"}
	r.w.typeSnippet("sn-1")
	r.tt.Frame()
	if !r.tt.HasText("you follow this pane: take or request control to type") || r.w.approve != nil || len(r.typed()) != 0 || len(r.directs()) != 1 {
		t.Errorf("approve %v typed %v directs %v: %v", r.w.approve != nil, r.typed(), r.directs(), r.tt.Texts())
	}
}

func TestNoAnswerShowsThePagesMessageOpensNoDialogAndIsNotRetried(t *testing.T) {
	r := directRig(t)
	r.fb.conns[0].directErr = keelclient.ErrNoDirectAnswer
	r.w.typeSnippet("sn-1")
	r.tt.Frame()
	if !r.tt.HasText("keel serve did not answer. Check the pane before you press Type again.") {
		t.Errorf("%v", r.tt.Texts())
	}
	if r.w.approve != nil || len(r.typed()) != 0 || len(r.directs()) != 1 {
		t.Errorf("approve %v typed %v directs %v: a dialog could type it a second time", r.w.approve != nil, r.typed(), r.directs())
	}
	r.tt.Frame()
	if len(r.directs()) != 1 {
		t.Error("it was retried")
	}
}

func TestAFullScreenProgramOrAnUnknownScreenGoesToTheDialogWithoutAskingKeelServe(t *testing.T) {
	r := directRig(t)
	r.screen(true, "")
	r.w.typeSnippet("sn-1")
	r.tt.Frame()
	if r.w.approve == nil || len(r.directs()) != 0 {
		t.Fatalf("alternate screen: approve %v directs %v", r.w.approve != nil, r.directs())
	}
	r.click(t, "Cancel")
	r.w.focusedPane().screen = nil // a terminal that cannot say is never read as a prompt
	r.w.typeSnippet("sn-1")
	r.tt.Frame()
	if r.w.approve == nil || len(r.directs()) != 0 {
		t.Fatalf("unknown screen: approve %v directs %v", r.w.approve != nil, r.directs())
	}
}

func TestThePromptLineTravelsWithTheRequest(t *testing.T) {
	r := directRig(t)
	r.screen(false, "[sudo] password for me:")
	r.w.typeSnippet("sn-1") // keel serve decides what to do with it; the client only reports what the pane shows
	if got := r.directs(); len(got) != 1 || got[0].ref.PromptLine != "[sudo] password for me:" || got[0].ref.AltScreen {
		t.Fatalf("directs %+v", got)
	}
}

func TestASecondPressWhileOneIsOnItsWayGetsThePagesMessageAndIsNeverQueued(t *testing.T) {
	r := directRig(t)
	posts := make(chan func(), 4)
	r.w.work = func(fn func()) { go fn() }
	r.w.post = func(fn func()) { posts <- fn }
	r.fb.conns[0].directGo = make(chan struct{})
	r.w.typeSnippet("sn-1")
	r.w.typeSnippet("sn-3")
	r.tt.Frame()
	if !r.tt.HasText("Still typing the last command into this pane.") {
		t.Errorf("%v", r.tt.Texts())
	}
	close(r.fb.conns[0].directGo)
	select {
	case fn := <-posts:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("the first never finished")
	}
	r.tt.Frame()
	if got := r.directs(); len(got) != 1 || got[0].command != "df -h" {
		t.Fatalf("directs %v: the second was queued", got)
	}
	// And the pane takes the next one after the first is done.
	r.w.work = func(fn func()) { fn() }
	r.w.post = func(fn func()) { fn() }
	r.w.typeSnippet("sn-3")
	if got := r.directs(); len(got) != 2 {
		t.Fatalf("directs %v", got)
	}
}

func TestAFollowerAndASessionThatIsNotReadyGoToTheDialogsRefusalAsBefore(t *testing.T) {
	for name, st := range map[string]keelclient.State{
		"follower":  {Status: "ready", Role: "follower", YouID: 3, DriverID: 2},
		"not ready": {Status: "connecting", Role: "driver"},
	} {
		r := directRig(t)
		st := st
		r.fb.conns[0].st = &st
		r.tt.Frame()
		r.w.typeSnippet("sn-1")
		r.tt.Frame()
		if len(r.directs()) != 0 || len(r.typed()) != 0 || r.w.approve == nil {
			t.Errorf("%s: directs %v typed %v approve %v", name, r.directs(), r.typed(), r.w.approve != nil)
			continue
		}
		r.click(t, "Type")
		want := msgFollowing
		if name == "not ready" {
			want = msgNotConnected
		}
		if !r.tt.HasText(want) || len(r.typed()) != 0 {
			t.Errorf("%s: %v typed %v", name, r.tt.Texts(), r.typed())
		}
	}
}

// ---- agent proposals ----

func directProposal() keelclient.Proposal {
	return keelclient.Proposal{ID: "p1", SessionID: "s1", HostID: "h-api", Host: "api-1", Command: "df -h", Bytes: "\x15df -h\n", Why: "Disk use.", Risk: "readonly"}
}

func TestAReadOnlyProposalOfOneLineIsTypedDirectWithTheAgentReference(t *testing.T) {
	r := agentRig(t)
	r.screen(false, "user@h $")
	r.fb.conns[0].directRes = keelclient.DirectResult{Typed: true}
	r.answers(directProposal())
	r.ask(t, "disk?")
	if !r.has("Type into pane") || r.has("Type into pane…") {
		t.Fatalf("the button says it will open a dialog: %v", r.tt.Texts())
	}
	r.click(t, "Type into pane")
	got := r.directs()
	if len(got) != 1 {
		t.Fatalf("directs %v", got)
	}
	d := got[0]
	ref := d.agent
	if d.command != "df -h" || ref == nil || ref.ProposalID != "p1" || ref.ConversationID != r.fb.agent.asks[0].ConversationID || ref.Shift {
		t.Fatalf("command %q ref %+v: the proposal's command and its reference", d.command, ref)
	}
	if d.ref.Kind != "agent" || d.ref.SnippetID != "" {
		t.Errorf("direct part %+v", d.ref)
	}
	if r.w.approve != nil || len(r.typed()) != 0 {
		t.Error("a dialog opened or the dialog's writer was used")
	}
	if !r.tt.HasText("Typed into api-1. Press Enter to run.") {
		t.Errorf("the card does not say Enter is the user's: %v", r.tt.Texts())
	}
}

func TestAProposalOutsideTheGrammarKeepsTheDialogAndItsEllipsis(t *testing.T) {
	r := agentRig(t)
	r.screen(false, "user@h $")
	r.answers(okProposal()) // echo keel-agent-ok
	r.ask(t, "q")
	if !r.has("Type into pane…") {
		t.Fatalf("no ellipsis: %v", r.tt.Texts())
	}
	r.click(t, "Type into pane…")
	if r.w.approve == nil || len(r.directs()) != 0 {
		t.Fatalf("approve %v directs %v", r.w.approve != nil, r.directs())
	}
}

func TestADestructiveOrStateChangingProposalNeverGoesDirect(t *testing.T) {
	destructive := directProposal()
	destructive.Command, destructive.Bytes, destructive.Risk, destructive.NeedsShift = "rm -rf /tmp/x", "\x15rm -rf /tmp/x\n", "destructive", true
	changes := directProposal()
	changes.Command, changes.Bytes, changes.Risk = "systemctl restart nginx", "\x15systemctl restart nginx\n", "changes"
	// A rating that is wrong for the command: read-only on a command outside the grammar, and in the grammar but rated as a change.
	liar := directProposal()
	liar.Command, liar.Bytes = "ls -l /tmp | sh", "\x15ls -l /tmp | sh\n"
	rated := directProposal()
	rated.Risk = "changes"
	for name, p := range map[string]keelclient.Proposal{"destructive": destructive, "changes": changes, "outside the grammar": liar, "rated as a change": rated} {
		r := agentRig(t)
		r.screen(false, "user@h $")
		r.fb.conns[0].directRes = keelclient.DirectResult{Typed: true}
		r.answers(p)
		r.ask(t, "q")
		r.click(t, "Type into pane…")
		if p.NeedsShift {
			if r.w.approve != nil {
				t.Errorf("%s: a plain press opened the dialog", name)
			}
			if err := r.tt.ClickWith(ui.Shift, "Type into pane…"); err != nil {
				t.Fatal(err)
			}
			r.tt.Frame()
		}
		if len(r.directs()) != 0 || len(r.typed()) != 0 || r.w.approve == nil {
			t.Errorf("%s: directs %v typed %v approve %v", name, r.directs(), r.typed(), r.w.approve != nil)
		}
	}
}

func TestAProposalRefusedByTheServerShowsItsWordsOnTheCard(t *testing.T) {
	r := agentRig(t)
	r.screen(false, "user@h $")
	r.fb.conns[0].directRes = keelclient.DirectResult{Message: "the bytes are not the ones shown"}
	r.answers(directProposal())
	r.ask(t, "q")
	r.click(t, "Type into pane")
	if len(r.directs()) != 1 || !r.tt.HasText("the bytes are not the ones shown") || r.tt.HasText("Press Enter to run.") || r.w.approve != nil {
		t.Errorf("directs %v approve %v: %v", r.directs(), r.w.approve != nil, r.tt.Texts())
	}
	// A server dialog answer opens the dialog with the proposal's own bytes.
	r2 := agentRig(t)
	r2.screen(false, "user@h $")
	r2.fb.conns[0].directRes = keelclient.DirectResult{Dialog: true}
	r2.answers(directProposal())
	r2.ask(t, "q")
	r2.click(t, "Type into pane")
	if r2.w.approve == nil || r2.w.approve.agent() == nil || len(r2.typed()) != 0 {
		t.Fatal("the dialog did not open for the proposal")
	}
	if got := r2.w.approve.shown(); got != "\x15df -h\n" {
		t.Errorf("shown %q", got)
	}
}

func TestAFollowerPaneGetsNoDirectTypeFromAProposalEither(t *testing.T) {
	r := agentRig(t)
	r.screen(false, "user@h $")
	r.answers(directProposal())
	r.ask(t, "q")
	st := keelclient.State{Status: "ready", Role: "follower", YouID: 3, DriverID: 2, Subtitle: "deploy@10.0.0.1"}
	r.fb.conns[0].st = &st
	r.tt.Frame()
	if r.has("Type into pane") || r.has("Type into pane…") {
		t.Fatalf("a follower is offered Type: %v", r.tt.Texts())
	}
	if len(r.directs()) != 0 {
		t.Fatal("asked")
	}
}
