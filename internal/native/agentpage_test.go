package native

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The Agent tab page (docs/ui-redesign.md §5.4.2) on the fake backend: it shows the window's one conversation, so the
// panel and the page never disagree.

// openPageSettled opens the Agent page and draws until its layout stops moving (the list loads and the width is known
// after the first frame), as a click needs.
func (r *rig) openPageSettled() {
	r.w.openAgentPage()
	r.settleFrames()
}

// settleFrames draws at least four frames and then until the texts and the page's rect stop changing (at most twelve): after a
// resize or a page opening, how many frames a layout needs is not the same on every platform (on Ubuntu CI the Agent page's
// width was one resize behind after two frames, issue #140), so the tests wait for the layout, not for a count.
func (r *rig) settleFrames() {
	prev := ""
	for i := 0; i < 12; i++ {
		r.tt.Frame()
		rc, _ := r.tt.Find("agent page")
		cur := fmt.Sprint(rc, r.tt.Texts())
		if i >= 3 && cur == prev {
			return
		}
		prev = cur
	}
}

func pageOf(t *testing.T, r *rig) *agentPage {
	t.Helper()
	tb := r.w.pageTab(pageAgent)
	if tb == nil || tb.pg.agent == nil {
		t.Fatal("no Agent page tab")
	}
	return tb.pg.agent
}

func TestTheAgentPageOpensFromThePaletteAndFromOpenAllInATab(t *testing.T) {
	r := savedRig(t)
	r.openPal(t)
	r.typePal("agent conversations")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if tb := r.w.tab(); tb == nil || tb.pg == nil || tb.pg.agent == nil {
		t.Fatal("the palette's Agent conversations did not open the page")
	}
	for _, s := range []string{"Conversations", "In scope", "Read so far", "Proposals", "Retention settings", "Every read and every typed proposal is also in Logs, under Agent.",
		"Continue in panel", "Why is pg-2 lagging", "Today", "Yesterday", "Older", "Saved encrypted in the vault · kept 30 days", "No questions yet"} {
		if !r.tt.HasText(s) && !r.has(s) {
			t.Errorf("the page lacks %q: %v", s, r.tt.Texts())
		}
	}
	// One instance: opening it again selects it.
	r.w.openAgentPage()
	r.tt.Frame()
	n := 0
	for _, tb := range r.w.tabs {
		if tb.pg != nil && tb.pg.agent != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d Agent pages", n)
	}
	// The panel's Open all in a tab selects the same one.
	r.w.selectTab(0)
	r.tt.Frame()
	r.openRecent(t)
	r.click(t, "Open all in a tab")
	r.tt.Frame()
	if tb := r.w.tab(); tb == nil || tb.pg == nil || tb.pg.agent == nil || r.w.agent.sessOpen {
		t.Fatal("Open all in a tab did not open the page")
	}
}

func TestPickingARowResumesForThePanelAndThePage(t *testing.T) {
	r := savedRig(t)
	r.openPageSettled()
	r.click(t, "conversation Why is pg-2 lagging")
	r.settleFrames()
	if r.w.agent.conv != "c-today" || !r.tt.HasText("why is replication slow") || !strings.Contains(strings.Join(r.tt.Texts(), "\n"), "1 question") {
		t.Fatalf("the page did not resume: %v", r.tt.Texts())
	}
	// The panel shows the same conversation.
	r.w.selectTab(0)
	r.tt.Frame()
	if !r.tt.HasText("Why is pg-2 lagging") || !r.tt.HasText("The replica is behind.") {
		t.Errorf("the panel does not show it: %v", r.tt.Texts())
	}
}

func TestAnAskFromEitherShowsInBoth(t *testing.T) {
	r := savedRig(t)
	r.answers()
	r.openPageSettled()
	p := pageOf(t, r)
	r.click(t, "Message the agent on the page")
	r.tt.Type("from the page")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if len(r.fb.agent.asks) != 1 || r.fb.agent.asks[0].Question != "from the page" || p.input != "" {
		t.Fatalf("asks %+v input %q", r.fb.agent.asks, p.input)
	}
	if !r.tt.HasText("from the page") || !r.tt.HasText("What to run") {
		t.Errorf("the page does not show its ask: %v", r.tt.Texts())
	}
	// Selecting the session tab: the panel (open) has the same turn.
	r.w.selectTab(0)
	r.tt.Frame()
	if !r.tt.HasText("from the page") {
		t.Errorf("the panel does not show the page's ask: %v", r.tt.Texts())
	}
	r.ask(t, "from the panel")
	r.w.openAgentPage()
	r.tt.Frame()
	if !r.tt.HasText("from the panel") || !r.tt.HasText("from the page") {
		t.Errorf("the page does not show the panel's ask: %v", r.tt.Texts())
	}
	for _, a := range r.fb.agent.asks {
		if a.ConversationID != r.w.agent.conv {
			t.Errorf("an ask went out under another conversation: %+v", a)
		}
	}
}

func TestRenameAndDeleteOnThePageChangeThePanel(t *testing.T) {
	r := savedRig(t)
	r.openPageSettled()
	r.click(t, "conversation Why is pg-2 lagging")
	r.settleFrames()
	p := pageOf(t, r)
	r.click(t, "Rename this conversation")
	r.tt.Frame()
	if !p.renaming || !r.has("Conversation title") {
		t.Fatal("Rename did not open the heading field")
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("Replication lag")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if want := []string{"c-today: Replication lag"}; !slices.Equal(r.fb.convs.renamed, want) || r.w.agent.title != "Replication lag" || p.renaming {
		t.Fatalf("renamed %v, title %q", r.fb.convs.renamed, r.w.agent.title)
	}
	// Esc leaves the title.
	r.click(t, "Rename this conversation")
	r.tt.Frame()
	r.tt.Type("zzz")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if p.renaming || len(r.fb.convs.renamed) != 1 {
		t.Fatalf("Esc renamed or kept the field: %v", r.fb.convs.renamed)
	}
	// Delete acts at once, as the app's does, and the panel starts over.
	r.click(t, "Delete this conversation")
	r.tt.Frame()
	if r.w.confirm != nil {
		t.Fatal("Delete on the page asked: the app deletes one conversation without asking")
	}
	if !slices.Equal(r.fb.convs.deleted, []string{"c-today"}) || r.w.agent.conv == "c-today" || len(r.w.agent.turns) != 0 || r.w.agent.title != "" {
		t.Fatalf("deleted %v, conv %q", r.fb.convs.deleted, r.w.agent.conv)
	}
	if r.tt.HasText("Why is pg-2 lagging") {
		t.Error("the deleted conversation is still listed")
	}
	// Rename and Delete are off for a conversation keel serve does not know.
	if !r.has("Rename this conversation") || !r.has("Delete this conversation") {
		t.Error("the header buttons are gone")
	}
}

func TestTheHostChipsFilterTheList(t *testing.T) {
	r := savedRig(t)
	r.openPageSettled()
	for _, h := range []string{"All", "prod-db-01", "ceph-rgw-1", "gitlab-runner-3"} {
		if !r.has("host filter " + h) {
			t.Errorf("no chip %q", h)
		}
	}
	r.click(t, "host filter ceph-rgw-1")
	r.settleFrames()
	if r.has("conversation Why is pg-2 lagging") || !r.has("conversation TLS cert") || r.has("conversation Disk full") {
		t.Errorf("the chip did not filter: %v", r.tt.Texts())
	}
	r.click(t, "host filter All")
	r.settleFrames()
	if !r.has("conversation Disk full") {
		t.Error("All did not clear the filter")
	}
	// At most six hosts.
	var many []keelclient.ConversationSummary
	for i := 0; i < 9; i++ {
		many = append(many, keelclient.ConversationSummary{ID: "c" + string(rune('a'+i)), Title: "t", Hosts: []string{"h" + string(rune('a'+i))}, UpdatedAt: time.Now()})
	}
	if got := hostFilters(many, hostChipMax); len(got) != 6 {
		t.Errorf("%d chips", len(got))
	}
}

func TestTheContextColumnAddsUpFromTheTrace(t *testing.T) {
	r := savedRig(t)
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		return keelclient.AskResponse{ConversationID: req.ConversationID, Title: "Counts", Answer: "ok",
			Trace: []keelclient.TraceStep{{Host: "api-1", Status: "read", Lines: 100, Redactions: map[string]int{"secret": 2}}, {Host: "api-1", Status: "read", Lines: 20},
				{Host: "db", Status: "skipped", Reason: "not-readable"}},
			Proposals: []keelclient.Proposal{okProposal()}}, nil
	}
	r.openPageSettled()
	r.ask2(t, "q")
	for _, s := range []string{"120 lines", "2 redacted", "api-1", "echo keel-agent-ok", "Not typed"} {
		if !r.tt.HasText(s) {
			t.Errorf("the context column lacks %q: %v", s, r.tt.Texts())
		}
	}
	if rs := readSummary(r.w.agent.convTurns()); len(rs) != 1 || rs[0].lines != 120 || rs[0].redacted != 2 {
		t.Errorf("read so far %+v: skipped panes must be left out", rs)
	}
	if ps := r.w.agent.pageProposals(); len(ps) != 1 || ps[0].p.ID != "p1" {
		t.Errorf("proposals %+v", ps)
	}
	if !r.tt.HasText("Reads only panes in scope now · ↵ send") {
		t.Error("no composer hint")
	}
}

// ask2 asks from the page's composer.
func (r *rig) ask2(t *testing.T, q string) {
	t.Helper()
	r.click(t, "Message the agent on the page")
	r.tt.Type(q)
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
}

func TestTheNarrowLayoutsFollowTheTabWidth(t *testing.T) {
	for _, c := range []struct {
		width float32
		want  agentLayout
	}{{0, layoutWide}, {1200, layoutWide}, {961, layoutWide}, {960, layoutMid}, {800, layoutMid}, {661, layoutMid}, {660, layoutNarrow}, {400, layoutNarrow}} {
		if got := pageLayout(c.width); got != c.want {
			t.Errorf("width %v: layout %v, want %v", c.width, got, c.want)
		}
	}
	r := savedRig(t)
	r.w.setAgentOpen(false)
	r.w.openAgentPage()
	r.tt.Frame()
	r.tt.Frame()
	p := pageOf(t, r)
	if pageLayout(p.width) != layoutWide || !r.tt.HasText("In scope") {
		t.Fatalf("width %v: not the three columns", p.width)
	}
	r.tt.SetSize(1100, 700) // 1100 - the sidebar is in the 660-960 range
	r.settleFrames()
	if pageLayout(p.width) != layoutMid {
		t.Errorf("width %v: not the context under the list", p.width)
	}
	if !r.tt.HasText("In scope") || !r.tt.HasText("Conversations") || !r.tt.HasText("Continue in panel") {
		t.Errorf("the mid layout lost a column: %v", r.tt.Texts())
	}
	r.tt.SetSize(850, 700)
	r.settleFrames()
	if pageLayout(p.width) != layoutNarrow {
		t.Errorf("width %v: not the narrow layout", p.width)
	}
	if !r.tt.HasText("In scope") || !r.tt.HasText("Conversations") || !r.tt.HasText("Continue in panel") {
		t.Errorf("the narrow layout lost a column: %v", r.tt.Texts())
	}
}

func TestTheNewConversationKeyOnThePageStartsOne(t *testing.T) {
	r := savedRig(t)
	r.answers()
	r.ask(t, "one")
	old := r.w.agent.conv
	r.w.openAgentPage()
	r.tt.Frame()
	r.tt.Key(ui.Cmd, ui.KeyN) // ⌘N on a page tab runs the page's New
	r.tt.Frame()
	if r.w.agent.conv == old || len(r.w.agent.turns) != 0 || r.w.agent.title != "" {
		t.Fatalf("⌘N did not start a new conversation: %q", r.w.agent.conv)
	}
	if strings.Contains(strings.Join(r.fb.calls, ","), "delete conversation") {
		t.Error("New conversation deleted something")
	}
}

func TestATypeOnThePageNamesThePaneAndGoesTheSamePath(t *testing.T) {
	r := savedRig(t)
	r.answers(okProposal())
	r.openPageSettled()
	r.ask2(t, "list")
	if !r.has("Type into api-1 · api-1…") && !r.has("Type into api-1…") {
		t.Fatalf("the Type button does not name the pane: %v", r.tt.Texts())
	}
	if len(r.typed()) != 0 || len(r.directs()) != 0 {
		t.Fatal("asking typed")
	}
}
