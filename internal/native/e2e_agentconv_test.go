//go:build nativee2e

package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Saved conversations, the Agent page and Settings · Agent against a real keel serve that answers from its in-process fake
// (KEEL_AGENT_PROVIDER=fake, set for every daemon by startServeAt): nothing here reaches a model provider, no test calls keel
// mcp's routes, and the only key ever stored is made up.

func (e *win) convList() keelclient.ConversationList {
	e.t.Helper()
	c, cancel := ctx()
	defer cancel()
	l, err := e.cl.Conversations(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func (e *win) serverSettings() (s struct {
	Settings struct {
		DefaultScope    string `json:"defaultScope"`
		ScrollbackLines int    `json:"scrollbackLines"`
		HistoryDays     int    `json:"historyDays"`
		ClaudeCode      bool   `json:"claudeCode"`
	} `json:"settings"`
	Keys map[string]struct {
		Configured bool `json:"configured"`
	} `json:"keys"`
}) {
	e.t.Helper()
	code, body := e.raw("GET", "/api/agent/settings", nil)
	if code != 200 {
		e.t.Fatalf("GET /api/agent/settings: %d", code)
	}
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// settledAsk asks and waits until every turn has its answer.
func (e *win) settledAsk(q string) {
	e.t.Helper()
	n := len(e.w.agent.turns)
	e.agentAsk(q)
	e.pump(func() bool {
		a := e.w.agent
		if len(a.turns) <= n || a.busy != nil {
			return false
		}
		for _, t := range a.turns {
			if t.pending {
				return false
			}
		}
		return true
	}, "the answer to "+q)
}

// openRecent opens Recent conversations.
func (e *win) openRecent() {
	e.t.Helper()
	e.click("Recent conversations")
	e.pump(func() bool { return e.hasLabel("dialog Recent conversations") }, "Recent conversations")
}

func (e *win) titleOf(l keelclient.ConversationList, part string) keelclient.ConversationSummary {
	e.t.Helper()
	for _, s := range l.Sessions {
		if strings.Contains(s.Title, part) {
			return s
		}
	}
	e.t.Fatalf("no saved conversation titled with %q: %+v", part, l.Sessions)
	return keelclient.ConversationSummary{}
}

func TestE2EAgentConversationsSavedAndResumed(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	e.openLocal()
	e.openAgent()

	e.settledAsk("what should I run? alpha")
	e.waitCard(1)
	e.settledAsk("and no command here")
	e.click("New conversation")
	e.settledAsk("what should I run? beta")
	l := e.convList()
	if len(l.Sessions) != 2 {
		t.Fatalf("%d saved conversations, want 2: %+v", len(l.Sessions), l.Sessions)
	}
	alpha, beta := e.titleOf(l, "alpha"), e.titleOf(l, "beta")
	if alpha.Turns != 2 || alpha.Proposals != 1 || beta.Turns != 1 || beta.Proposals != 1 {
		t.Fatalf("alpha %+v beta %+v", alpha, beta)
	}

	e.openRecent()
	for _, s := range []string{"Today", alpha.Title, beta.Title, "· 1 proposal", "Kept 30 days", "Open all in a tab", "Delete all"} {
		if !e.tt.HasText(s) {
			t.Errorf("Recent conversations lacks %q: %v", s, e.tt.Texts())
		}
	}
	e.shot("TestE2EAgentConversationsSavedAndResumed-recent")

	// Rename the first: ↵ saves, and keel serve keeps the new title.
	e.click("Rename " + alpha.Title)
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type("Renamed alpha")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool {
		for _, s := range e.convList().Sessions {
			if s.ID == alpha.ID && s.Title == "Renamed alpha" {
				return true
			}
		}
		return false
	}, "GET /api/agent/conversations to have the new title")

	// Close the window; a new one still lists both.
	e.w.closeWindow()
	e.settle(300 * time.Millisecond)
	e2 := newWin(t, addr, dir, nil)
	e2.pump(func() bool {
		return e2.w.agent.open && e2.w.agent.keyConfigured != nil && e2.hasLabel("Message the agent")
	}, "the panel in the new window")
	e2.openRecent()
	e2.pump(func() bool { return e2.tt.HasText("Renamed alpha") && e2.tt.HasText(beta.Title) }, "both conversations listed in the new window")

	// Resume the first: the saved thread, the divider and the note; an ask carries on under its id.
	e2.click("conversation Renamed alpha")
	e2.pump(func() bool { return e2.w.agent.conv == alpha.ID && e2.tt.HasText("what should I run? alpha") }, "the saved thread")
	for _, s := range []string{"and no command here", "I was given 1 pane excerpt and 0 redacted values.", "Earlier reads stay in this conversation as text. New reads use only the panes in scope now, on hosts that still allow the agent."} {
		if !e2.tt.HasText(s) {
			t.Errorf("the resumed thread lacks %q: %v", s, e2.tt.Texts())
		}
	}
	if !strings.Contains(e2.texts(), "Resumed · ") {
		t.Errorf("no Resumed divider: %v", e2.tt.Texts())
	}
	e2.shot("TestE2EAgentConversationsSavedAndResumed-resumed")
	e2.settledAsk("tell me more, no command")
	e2.pump(func() bool { return e2.titleOf(e2.convList(), "Renamed alpha").Turns == 3 }, "the conversation's turn count on keel serve to grow by one")
	if !e2.tt.HasText("tell me more, no command") {
		t.Error("the new question is not on the thread")
	}
}

// ageOut starts n conversations so keel serve, which keeps 16 in memory, drops the oldest: its proposals are then not held.
func (e *win) fillConversations(n int) {
	e.t.Helper()
	for i := 1; i <= n; i++ {
		if i > 1 {
			e.click("New conversation")
		}
		e.settledAsk(fmt.Sprintf("what should I run? #%02d", i))
	}
}

func TestE2EAgentExpiredProposal(t *testing.T) {
	t.Run("restart", func(t *testing.T) {
		// A daemon stopped and started again on the same data dir no longer holds the proposal: the saved card is Expired.
		dir := scratchDir(t)
		addr, stop := startServeAt(t, dir, "serve", true)
		dataDir := filepath.Join(dir, "data")
		e := newWin(t, addr, dataDir, nil)
		e.putAgentKey()
		e.openLocal()
		e.openAgent()
		e.settledAsk("what should I run?")
		e.waitCard(1)
		id := e.titleOf(e.convList(), "what should I run?").ID
		e.w.closeWindow()
		stop()
		addr2, _ := startServeAt(t, dir, "serve2", true)
		e2 := newWin(t, addr2, dataDir, nil)
		e2.pump(func() bool {
			return e2.w.agent.open && e2.w.agent.keyConfigured != nil && e2.hasLabel("Message the agent")
		}, "the panel on the new daemon")
		e2.openRecent()
		e2.click("conversation what should I run?")
		e2.pump(func() bool { return e2.w.agent.conv == id && e2.hasLabel("Proposal expired") }, "the expired card")
		for _, s := range []string{"Expired", "Panes may have changed since this was proposed.", "Copy", "Propose again"} {
			if !e2.tt.HasText(s) && !e2.hasLabel(s) {
				t.Errorf("the expired card lacks %q: %v", s, e2.tt.Texts())
			}
		}
		for _, gone := range []string{"Type into pane", "Type into pane…", "Edit command", "Copy command", "Save command"} {
			if e2.hasLabel(gone) {
				t.Errorf("an expired card offers %q", gone)
			}
		}
		e2.shot("TestE2EAgentExpiredProposal-expired")
		e2.click("Propose again")
		e2.pump(func() bool { return e2.cards() >= 1 }, "a live card")
		if !e2.tt.HasText("echo keel-agent-ok") {
			t.Errorf("the new card does not show the command: %v", e2.tt.Texts())
		}
		// The session the proposal named died with the old daemon: its card is Copy only, and nothing was typed.
		if e2.hasLabel("Type into pane…") || e2.logKinds()["agent.type"] != 0 {
			t.Error("a card for a pane that is gone offers Type")
		}
	})
	t.Run("evicted", func(t *testing.T) {
		// keel serve keeps 16 conversations in memory: with a 17th the first is dropped and its saved proposal is Expired
		// while its pane is still open, so Propose again gives a proposal that can be typed, through the dialog.
		addr, dataDir := startServe(t, true)
		e := newWin(t, addr, dataDir, nil)
		e.putAgentKey()
		sid := e.openLocal()
		e.openAgent()
		e.fillConversations(17)
		first := e.titleOf(e.convList(), "#01")
		e.openRecent()
		e.click("Search conversations")
		e.tt.Type("#01")
		e.step()
		e.click("conversation " + first.Title)
		e.pump(func() bool { return e.w.agent.conv == first.ID && e.hasLabel("Proposal expired") }, "the expired card")
		if e.hasLabel("Type into pane…") || e.hasLabel("Edit command") {
			t.Fatal("an expired card offers Type or Edit")
		}
		e.click("Propose again")
		e.pump(func() bool { return e.cards() >= 1 && e.hasLabel("Type into pane…") }, "a live card with Type")
		if strings.Contains(e.scrollback(sid), "keel-agent-ok") {
			t.Fatal("Propose again typed")
		}
		e.click("Type into pane…")
		e.waitApprove()
		e.click("Type")
		e.pump(func() bool { return e.w.approve == nil }, "the dialog to close after Type")
		e.waitScrollback(sid, "\nkeel-agent-ok")
		if e.logKinds()["agent.type"] != 1 {
			t.Fatalf("log rows: %v", e.logKinds())
		}
	})
}

func TestE2EAgentPage(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWinH(t, addr, dir, nil, 800)
	e.putAgentKey()
	e.openLocal()
	e.openAgent()
	e.settledAsk("what should I run? page-one")
	// A second conversation that read no pane: take the pane out of scope first.
	e.click("New conversation")
	e.click("Stop reading Local Shell")
	e.settledAsk("no command, page-two")
	l := e.convList()
	one, two := e.titleOf(l, "page-one"), e.titleOf(l, "page-two")
	if len(one.Hosts) != 1 || len(two.Hosts) != 0 {
		t.Fatalf("hosts %v %v", one.Hosts, two.Hosts)
	}

	// Open the page from the palette; with the panel closed it has the three columns.
	e.w.setAgentOpen(false)
	e.step()
	e.key(ui.Cmd, ui.KeyK)
	e.tt.Type("agent conversations")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return e.w.agentPageShown() && e.tt.HasText("In scope") }, "the Agent page")
	e.settle(300 * time.Millisecond)
	for _, s := range []string{"Conversations", "Read so far", "Proposals", "Retention settings", "Continue in panel"} {
		if !e.tt.HasText(s) && !e.hasLabel(s) {
			t.Errorf("the page lacks %q: %v", s, e.tt.Texts())
		}
	}
	e.shot("TestE2EAgentPage-page")

	// The host chip filters.
	e.click("host filter Local Shell")
	e.settle(200 * time.Millisecond)
	if !e.hasLabel("conversation "+one.Title) || e.hasLabel("conversation "+two.Title) {
		t.Errorf("the chip did not filter: %v", e.tt.Texts())
	}
	e.click("host filter All")
	e.settle(200 * time.Millisecond)

	// Pick a conversation: the panel shows it too.
	e.click("conversation " + one.Title)
	e.pump(func() bool { return e.w.agent.conv == one.ID && e.hasLabel("Message the agent on the page") }, "the page to resume it")
	e.w.setAgentOpen(true)
	e.pump(func() bool { return e.hasLabel("Message the agent") }, "the panel")
	// The page chooses its layout from its width in the frame before, so it follows the panel opening one frame later: wait for
	// it, as a person's click comes later (with the page 648 px wide the wide layout left its thread 68 px for one frame).
	e.pump(func() bool {
		r, ok := e.tt.Find("Message the agent on the page")
		return ok && r.W > 100
	}, "the page's composer to have room beside the panel")
	count := func(s string) (n int) {
		for _, x := range e.tt.Texts() {
			if x == s {
				n++
			}
		}
		return n
	}
	if count("what should I run? page-one") < 2 {
		t.Errorf("the panel and the page do not both show the conversation: %d", count("what should I run? page-one"))
	}

	// Ask from the page: both threads grow.
	e.click("Message the agent on the page")
	e.tt.Type("and then, no command from the page")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool { return count("and then, no command from the page") >= 2 && e.w.agent.busy == nil }, "both threads to show the page's ask")

	// Rename on the page: keel serve and the panel follow.
	e.click("Rename this conversation")
	e.key(ui.Cmd, ui.KeyA)
	e.tt.Type("Page renamed")
	e.step()
	e.key(0, ui.KeyEnter)
	e.pump(func() bool {
		for _, s := range e.convList().Sessions {
			if s.ID == one.ID && s.Title == "Page renamed" {
				return true
			}
		}
		return false
	}, "keel serve to hold the new title")
	e.pump(func() bool { return e.w.agent.title == "Page renamed" && e.hasLabel("Recent conversations") }, "the panel to follow")

	// Delete on the page acts at once, as the app's does; keel serve and the panel follow.
	e.click("Delete this conversation")
	e.pump(func() bool { return len(e.convList().Sessions) == 1 && len(e.w.agent.turns) == 0 }, "the conversation to be gone on keel serve and in the panel")
	if e.w.agent.title != "" || e.w.agent.conv == one.ID {
		t.Error("the panel still has the deleted conversation")
	}
}

func TestE2EAgentDeleteAll(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	e.openLocal()
	e.openAgent()
	e.settledAsk("what should I run? one")
	e.click("New conversation")
	e.settledAsk("what should I run? two")
	if n := len(e.convList().Sessions); n != 2 {
		t.Fatalf("%d saved", n)
	}
	e.openRecent()
	e.click("Delete all")
	e.pump(func() bool { return e.w.confirm != nil }, "the confirm")
	if !e.tt.HasText("Delete all conversations?") || !e.tt.HasText("Every saved agent conversation is deleted from the vault, in every window. The log entries that say what the agent read stay in Logs.") {
		t.Errorf("the confirm is not the app's: %v", e.tt.Texts())
	}
	e.click("Cancel")
	e.settle(300 * time.Millisecond)
	if n := len(e.convList().Sessions); n != 2 {
		t.Fatalf("Cancel left %d", n)
	}
	e.openRecent()
	e.click("Delete all")
	e.pump(func() bool { return e.w.confirm != nil }, "the confirm again")
	e.click("Delete all")
	e.pump(func() bool { return len(e.convList().Sessions) == 0 }, "keel serve to hold no conversation")
	e.pump(func() bool { return e.tt.HasText("Agent history deleted") && len(e.w.agent.turns) == 0 }, "the window to start over")
	e.openRecent()
	e.pump(func() bool { return e.tt.HasText("No saved conversations.") }, "the app's empty text")
}

// recBackend records what a window sent to keel serve for the agent's settings: the patches, which name every field they
// change. The window has no method that calls keel mcp's routes, and a test that logged one would fail here.
type recBackend struct {
	backend
	mu      sync.Mutex
	patches []keelclient.AgentSettingsPatch
	calls   []string
}

func (r *recBackend) SaveAgentSettings(ctx context.Context, p keelclient.AgentSettingsPatch) (keelclient.AgentSettings, error) {
	r.mu.Lock()
	r.patches, r.calls = append(r.patches, p), append(r.calls, "PUT /api/agent/settings")
	r.mu.Unlock()
	return r.backend.SaveAgentSettings(ctx, p)
}

func (r *recBackend) SetAgentKey(ctx context.Context, in keelclient.AgentKeyInput) error {
	r.mu.Lock()
	r.calls = append(r.calls, "PUT /api/agent/keys/"+in.Provider)
	r.mu.Unlock()
	return r.backend.SetAgentKey(ctx, in)
}

func (r *recBackend) saved() []keelclient.AgentSettingsPatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]keelclient.AgentSettingsPatch(nil), r.patches...)
}

func TestE2EAgentSettings(t *testing.T) {
	addr, dir := startServe(t, true)
	var rec *recBackend
	e := newWinH(t, addr, dir, func(b backend) backend { rec = &recBackend{backend: b}; return rec }, 720)
	e.putAgentKey()
	e.openLocal()
	e.openSettings()
	e.click("settings nav Agent")
	e.pump(func() bool {
		return e.hasLabel("agent card Google Gemini") && e.hasLabel("badge Connected Google Gemini") && e.hasLabel("agent card TypeSafe Jev")
	}, "the provider cards")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EAgentSettings-dark")
	e.w.setTheme(themeLight)
	e.pump(func() bool { return !e.w.dark }, "the light theme")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2EAgentSettings-light")
	e.w.setTheme(themeDark)
	e.pump(func() bool { return e.w.dark }, "the dark theme")

	e.tt.SetSize(1280, 3200) // the section is long: every control is on screen
	e.settle(300 * time.Millisecond)
	if s := e.serverSettings(); s.Settings.ClaudeCode || s.Settings.ScrollbackLines != 200 || s.Settings.DefaultScope != "focused" || s.Settings.HistoryDays != 30 {
		t.Fatalf("the defaults: %+v", s.Settings)
	}

	// Scrollback lines and the default scope.
	p := e.w.pageTab("settings").pg.settings
	p.linesV = 400
	e.pump(func() bool { return e.serverSettings().Settings.ScrollbackLines == 400 }, "Scrollback lines on keel serve")
	e.click("Default panes per conversation Every pane in the tab")
	e.pump(func() bool { return e.serverSettings().Settings.DefaultScope == "tab" }, "the default scope on keel serve")

	// Keep conversations: a saved conversation, then "only while open" removes it on keel serve, and the panel says so.
	e.openAgent()
	e.settledAsk("what should I run? settings")
	if n := len(e.convList().Sessions); n != 1 {
		t.Fatalf("%d saved", n)
	}
	e.click("Keep conversations picker")
	if err := e.tt.ChooseMenuItem("Only while the panel is open"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.serverSettings().Settings.HistoryDays == 0 }, "Keep conversations on keel serve")
	e.pump(func() bool { return e.tt.HasText("Not saved · kept while open") }, "the panel to say Not saved · kept while open")
	e.click("Keep conversations picker")
	if err := e.tt.ChooseMenuItem("30 days"); err != nil {
		t.Fatal(err)
	}
	e.pump(func() bool { return e.serverSettings().Settings.HistoryDays == 30 }, "Keep conversations back on")
	if n := len(e.convList().Sessions); n != 0 {
		t.Fatalf("the saved conversation is still on keel serve (%d)", n)
	}

	// Claude Code: off, on, off; only claudeCode is sent, and the window has no call that reaches keel mcp.
	e.click("Allow Claude Code (keel mcp) switch")
	e.pump(func() bool { return e.serverSettings().Settings.ClaudeCode }, "Claude Code on")
	e.click("Allow Claude Code (keel mcp) switch")
	e.pump(func() bool { return !e.serverSettings().Settings.ClaudeCode }, "Claude Code off")
	var claude []keelclient.AgentSettingsPatch
	for _, p := range rec.saved() {
		if p.ClaudeCode != nil {
			claude = append(claude, p)
			if !(p.Provider == nil && p.Model == nil && p.Detection == nil && p.Suggestions == nil && p.HistorySuggestions == nil && p.DefaultScope == nil &&
				p.ScrollbackLines == nil && p.Redact == nil && p.HistoryDays == nil) {
				t.Errorf("the Claude Code switch sent more than claudeCode: %+v", p)
			}
		}
	}
	if len(claude) != 2 || !*claude[0].ClaudeCode || *claude[1].ClaudeCode {
		t.Fatalf("Claude Code patches: %d", len(claude))
	}
	for _, c := range rec.calls {
		if strings.Contains(c, "mcp") {
			t.Errorf("the window made a request named %s", c)
		}
	}

	// A fake key through the Anthropic card: masked, sent once, then Connected, and in no log, pref, shot text or screen.
	var d keelclient.AgentProvider
	for _, x := range e.w.agent.provs {
		if x.ID == "anthropic" {
			d = x
		}
	}
	if d.ID == "" {
		t.Fatal("no Anthropic provider card")
	}
	e.click(orElse(d.KeyLabel, "API key") + " input")
	e.tt.Type(fakeAgentKey)
	e.step()
	if strings.Contains(e.texts(), fakeAgentKey) {
		t.Fatal("the key is drawn in the clear")
	}
	e.click("Connect " + d.Name)
	e.pump(func() bool {
		return e.serverSettings().Keys["anthropic"].Configured && e.hasLabel("badge Connected "+d.Name)
	}, "the card to turn Connected")
	if p.card("anthropic").field != "" {
		t.Error("the field still holds the key")
	}
	if strings.Contains(e.texts(), fakeAgentKey) {
		t.Fatal("the key is on screen")
	}
	for _, f := range []string{"keel.log", "keel-native.json", "keel-native-workspace.json"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && strings.Contains(string(b), fakeAgentKey) {
			t.Fatalf("the key is in %s", f)
		}
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "serve.log")); err == nil && strings.Contains(string(b), fakeAgentKey) {
		t.Fatal("the key is in the daemon's log")
	}
	for _, l := range e.logEntries() {
		if strings.Contains(l.Message, fakeAgentKey) {
			t.Fatal("the key is in GET /api/logs")
		}
	}
	if fmt.Sprintf("%v %+v", rec.calls, rec.saved()) == "" || strings.Contains(fmt.Sprintf("%v %+v", rec.calls, rec.saved()), fakeAgentKey) {
		t.Error("the request log holds the key")
	}
}
