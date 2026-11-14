package native

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Saved conversations in the panel (docs/ui-redesign.md §5.4.1) on the fake backend: the list, rename, delete, resume
// and expired proposals. Nothing here reaches a model provider, and a proposal reaches a session only through typeNow
// or Approve before type.

func savedList() keelclient.ConversationList {
	now := time.Now()
	return keelclient.ConversationList{HistoryDays: 30, Sessions: []keelclient.ConversationSummary{
		{ID: "c-today", Title: "Why is pg-2 lagging", Hosts: []string{"prod-db-01"}, UpdatedAt: now, Turns: 2, Proposals: 2},
		{ID: "c-yday", Title: "TLS cert", Hosts: []string{"ceph-rgw-1"}, UpdatedAt: now.Add(-24 * time.Hour), Turns: 1, Proposals: 1},
		{ID: "c-old", Title: "Disk full", Hosts: []string{"gitlab-runner-3"}, UpdatedAt: now.Add(-10 * 24 * time.Hour), Turns: 1, Proposals: 0},
	}}
}

// savedRig is the agent rig with three saved conversations and a transcript for the first.
func savedRig(t *testing.T) *rig {
	t.Helper()
	r := agentRig(t)
	diagWindow(t, r) // issue #140: on a failure, what differs from macOS (linuxdiag_test.go)
	r.fb.convs.list = savedList()
	r.fb.convs.transcr = map[string]keelclient.Transcript{
		"c-today": {ID: "c-today", Title: "Why is pg-2 lagging", CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-30 * time.Minute), Saved: true, Turns: []keelclient.SavedTurn{
			{At: time.Now().Add(-time.Hour), Question: "why is replication slow", Answer: "The replica is behind.", Trace: []keelclient.TraceStep{{Host: "api-1", Status: "read", Lines: 14}},
				Proposals: []keelclient.Proposal{{ID: "old1", SessionID: "s1", Host: "api-1", Command: "df -h", Bytes: "\x15df -h\n", Risk: "readonly", Expired: true}}},
		}},
	}
	return r
}

func (r *rig) openRecent(t *testing.T) {
	t.Helper()
	r.click(t, "Recent conversations")
	r.tt.Frame()
	if !r.w.agentState().sessOpen || !r.has("dialog Recent conversations") {
		t.Fatal("the title button did not open Recent conversations")
	}
}

func TestTheTitleOpensRecentConversationsGroupedAndSearchable(t *testing.T) {
	r := savedRig(t)
	r.openRecent(t)
	for _, s := range []string{"Today", "Yesterday", "Older", "Why is pg-2 lagging", "TLS cert", "Disk full", "prod-db-01", "· 2 proposals", "· 1 proposal", "· no proposals",
		"Open all in a tab", "Kept 30 days", "Delete all"} {
		if !r.tt.HasText(s) {
			t.Errorf("Recent conversations lacks %q: %v", s, r.tt.Texts())
		}
	}
	// A search narrows by title or host, any case.
	r.tt.Type("RUNNER")
	r.tt.Frame()
	if r.tt.HasText("TLS cert") || !r.tt.HasText("Disk full") || r.tt.HasText("Yesterday") {
		t.Errorf("the search did not narrow the list: %v", r.tt.Texts())
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("nothing at all")
	r.tt.Frame()
	if !r.tt.HasText("Nothing matches “nothing at all”") {
		t.Errorf("no empty text for a search without a match: %v", r.tt.Texts())
	}
}

func TestRecentConversationsKeysMoveAndEscCloses(t *testing.T) {
	r := savedRig(t)
	r.openRecent(t)
	r.tt.Key(0, ui.KeyDown)
	r.tt.Frame()
	if r.w.agentState().sess.sel != 0 {
		t.Fatalf("↓ left the selection at %d", r.w.agentState().sess.sel)
	}
	r.tt.Key(0, ui.KeyDown)
	r.tt.Frame()
	r.tt.Key(0, ui.KeyUp)
	r.tt.Frame()
	if r.w.agentState().sess.sel != 0 {
		t.Errorf("↑ left the selection at %d", r.w.agentState().sess.sel)
	}
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if r.w.agentState().sessOpen {
		t.Error("Esc did not close Recent conversations")
	}
}

func TestRenameInlineEnterSendsAndEscLeavesTheTitle(t *testing.T) {
	r := savedRig(t)
	r.openRecent(t)
	r.click(t, "Rename TLS cert")
	r.tt.Frame()
	if r.w.agentState().renameID != "c-yday" || !r.has("Conversation title") {
		t.Fatal("Rename did not open the inline field")
	}
	r.tt.Type(" renewal")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if r.w.agentState().renameID != "" || len(r.fb.convs.renamed) != 0 || !r.w.agentState().sessOpen {
		t.Fatalf("Esc must leave the title as it was and stay in the list: %v open %v", r.fb.convs.renamed, r.w.agentState().sessOpen)
	}
	r.click(t, "Rename TLS cert")
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("TLS renewal")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if want := []string{"c-yday: TLS renewal"}; !slices.Equal(r.fb.convs.renamed, want) {
		t.Fatalf("renamed %v, want %v: rename acts on the id the server gave", r.fb.convs.renamed, want)
	}
	if !r.tt.HasText("TLS renewal") {
		t.Errorf("the row does not show the new title: %v", r.tt.Texts())
	}
}

func TestDeleteActsAtOnceAndDeleteAllAsksFirst(t *testing.T) {
	r := savedRig(t)
	r.openRecent(t)
	r.click(t, "Delete Disk full") // one conversation is deleted without asking
	r.tt.Frame()
	if r.w.confirmOpen() || !slices.Equal(r.fb.convs.deleted, []string{"c-old"}) || !r.tt.HasText("Conversation deleted") {
		t.Fatalf("deleted %v, confirm %v", r.fb.convs.deleted, r.w.confirmDlg())
	}
	if !r.w.agentState().sessOpen { // the list stays open and repaints
		t.Fatal("the list closed after a delete")
	}

	r.click(t, "Delete all")
	r.tt.Frame()
	if !r.w.confirmOpen() || r.w.confirmDlg().title != "Delete all conversations?" || r.fb.convs.deletedAll != 0 {
		t.Fatalf("Delete all did not ask first: %v", r.w.confirmDlg())
	}
	if r.w.confirmDlg().message != "Every saved agent conversation is deleted from the vault, in every window. The log entries that say what the agent read stay in Logs." || r.w.confirmDlg().action != "Delete all" {
		t.Errorf("the confirm's words are not the app's: %q %q", r.w.confirmDlg().message, r.w.confirmDlg().action)
	}
	r.click(t, "Cancel")
	r.tt.Frame()
	if r.fb.convs.deletedAll != 0 {
		t.Fatal("Cancel deleted everything")
	}
	r.openRecent(t)
	r.click(t, "Delete all")
	r.tt.Frame()
	r.click(t, "Delete all") // the dialog's button
	r.tt.Frame()
	if r.fb.convs.deletedAll != 1 {
		t.Fatalf("confirming did not delete all (%d)", r.fb.convs.deletedAll)
	}
	r.openRecent(t)
	if !r.tt.HasText("No saved conversations.") && !r.tt.HasText("History is off: conversations are kept only while the panel is open. Turn it on in Settings · Agent.") {
		t.Errorf("no empty text: %v", r.tt.Texts())
	}
}

func TestResumeShowsTheSavedThreadTheDividerAndTheNote(t *testing.T) {
	r := savedRig(t)
	r.openRecent(t)
	r.click(t, "conversation Why is pg-2 lagging")
	r.tt.Frame()
	a := r.w.agentState()
	if a.conv != "c-today" || a.title != "Why is pg-2 lagging" || a.sessOpen {
		t.Fatalf("conv %q title %q open %v", a.conv, a.title, a.sessOpen)
	}
	for _, s := range []string{"why is replication slow", "The replica is behind.", "Read api-1 · last 14 lines", "Resumed · 30 minutes ago",
		"Earlier reads stay in this conversation as text. New reads use only the panes in scope now, on hosts that still allow the agent."} {
		if !r.tt.HasText(s) {
			t.Errorf("the resumed thread lacks %q: %v", s, r.tt.Texts())
		}
	}
	// The next ask carries the saved conversation's id, and what any ask carries.
	r.answers()
	r.ask(t, "and now?")
	if n := len(r.fb.agent.asks); n != 1 || r.fb.agent.asks[0].ConversationID != "c-today" || r.fb.agent.asks[0].Question != "and now?" || len(r.fb.agent.asks[0].Panes) != 1 {
		t.Fatalf("asks %+v", r.fb.agent.asks)
	}
	if !r.tt.HasText("Resumed · 30 minutes ago") || !r.tt.HasText("and now?") {
		t.Error("the thread lost the divider or the new question")
	}
}

func TestNewConversationKeepsTheOldOneInTheList(t *testing.T) {
	r := savedRig(t)
	r.answers()
	r.ask(t, "first")
	if len(r.w.agentState().turns) == 0 {
		t.Fatal("no turn")
	}
	old := r.w.agentState().conv
	r.click(t, "New conversation")
	r.tt.Frame()
	if r.w.agentState().conv == old || len(r.w.agentState().turns) != 0 || r.w.agentState().title != "" || len(r.fb.convs.deleted) != 0 || r.fb.convs.deletedAll != 0 {
		t.Fatal("New conversation must start a new one and delete nothing")
	}
}

func TestAnExpiredCardHasOnlyCopyAndProposeAgainAndNoRequestForTypeOrEdit(t *testing.T) {
	r := savedRig(t)
	r.openRecent(t)
	r.click(t, "conversation Why is pg-2 lagging")
	r.tt.Frame()
	if !r.has("Proposal expired") || !r.tt.HasText("Expired") || !r.tt.HasText("Panes may have changed since this was proposed.") {
		t.Fatalf("no expired card: %v", r.tt.Texts())
	}
	for _, gone := range []string{"Type into pane", "Type into pane…", "Edit command", "Copy command", "Save command"} {
		if r.has(gone) {
			t.Errorf("an expired card offers %q", gone)
		}
	}
	for _, want := range []string{"Copy", "Propose again"} {
		if !r.has(want) {
			t.Errorf("an expired card lacks %q", want)
		}
	}
	// Even a call that tried would send nothing: no edit, no direct type, no frame.
	r.w.typeAgentProposal("old1", true)
	r.w.startEdit(r.w.agentState().props["old1"])
	r.w.editAgentProposal("old1")
	r.tt.Frame()
	if len(r.fb.agent.edits) != 0 || len(r.typed()) != 0 || len(r.directs()) != 0 || r.w.approveOpen() || r.w.agentState().props["old1"].editing {
		t.Fatalf("an expired proposal reached a request: edits %v typed %v", r.fb.agent.edits, r.typed())
	}
	// Copy puts the command on the clipboard and sends nothing.
	r.click(t, "Copy")
	r.tt.Frame()
	if len(r.fb.convs.again) != 0 {
		t.Fatal("Copy asked keel serve for a proposal")
	}
	// Propose again adds a live card: a new proposal handled like any other.
	r.click(t, "Propose again")
	r.tt.Frame()
	if !slices.Equal(r.fb.convs.again, []string{"c-today/old1"}) {
		t.Fatalf("propose again %v", r.fb.convs.again)
	}
	if !r.has("Proposal") || !r.has("Type into pane…") {
		t.Errorf("no live card after Propose again: %v", r.tt.Texts())
	}
	if r.w.agentState().props["again-old1"] == nil || r.w.agentState().props["again-old1"].p.Expired {
		t.Error("the new proposal is not live")
	}
}

func TestAProposalProposedAgainIsTypedOnlyThroughTheUsualPath(t *testing.T) {
	r := savedRig(t)
	r.openRecent(t)
	r.click(t, "conversation Why is pg-2 lagging")
	r.tt.Frame()
	r.click(t, "Propose again")
	r.tt.Frame()
	r.fb.convs.againReply = nil
	if len(r.typed()) != 0 || len(r.directs()) != 0 {
		t.Fatal("Propose again typed")
	}
	r.click(t, "Type into pane…") // the dialog follows: read-only one-liner or not, a press is the user's
	r.tt.Frame()
	if len(r.typed())+len(r.directs()) > 1 {
		t.Fatalf("typed %v direct %v", r.typed(), r.directs())
	}
}

func TestNotSavedShowsWhenHistoryIsOffAndSavedWhenItIsOn(t *testing.T) {
	r := savedRig(t)
	r.w.agentState().settings = nil
	r.fb.agent.settings.Settings.HistoryDays = 30
	r.w.loadAgent()
	r.tt.Frame()
	if !r.tt.HasText("Saved in the vault") {
		t.Errorf("history on: %v", r.tt.Texts())
	}
	days := 0
	r.fb.convs.list.HistoryDays = days
	r.fb.convs.list.Sessions = nil
	r.openRecent(t)
	if !r.tt.HasText("Not saved · kept while open") || !r.tt.HasText("History is off: conversations are kept only while the panel is open. Turn it on in Settings · Agent.") {
		t.Errorf("history off: %v", r.tt.Texts())
	}
	if strings.Contains(strings.Join(r.tt.Texts(), "\n"), "Saved in the vault") {
		t.Error("it still says Saved in the vault")
	}
}

func TestAConversationIsNeverWrittenToDisk(t *testing.T) {
	r := savedRig(t)
	dir := t.TempDir()
	r.w.useDataDir(t, dir)
	r.openRecent(t)
	r.click(t, "conversation Why is pg-2 lagging")
	r.tt.Frame()
	r.answers()
	r.ask(t, "a secret-looking question")
	r.openRecent(t)
	r.click(t, "Rename Disk full")
	r.tt.Type(" again")
	r.tt.Frame()
	r.w.setAgentOpen(true) // a prefs write after the conversation was on screen
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		for _, s := range []string{"why is replication slow", "The replica is behind", "a secret-looking question", "Why is pg-2 lagging", "df -h"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s holds conversation text %q", e.Name(), s)
			}
		}
	}
}
