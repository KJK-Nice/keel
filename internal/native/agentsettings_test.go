package native

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Settings · Agent (docs/ui-redesign.md §5.9) on the fake backend: each control shows what keel serve holds and sends the
// app's request when it changes; a key is masked, sent once and never kept; Disconnect and Delete all history ask first;
// Claude Code is off until it is turned on and sends claudeCode and nothing else.

const fakeProviderKey = "fake-key-for-tests-only-0000" // obviously made up; no test uses a real one

type keyFlag = struct {
	Configured bool `json:"configured"`
}

func agentSettingsBackend() *fakeBackend {
	fb := settingsBackend()
	fb.agent.providers = []keelclient.AgentProvider{
		{ID: "gemini", Name: "Google Gemini", ShortName: "Gemini", Kind: "model", KeyLabel: "Gemini API key", Models: []string{"gm-1", "gm-2"},
			Warning: "On the free tier of the Gemini API, Google may use the content you submit.", TermsURL: "https://ai.google.dev/terms", KeyHelpURL: "https://aistudio.google.com/apikey"},
		{ID: "anthropic", Name: "Anthropic Claude", ShortName: "Claude", Kind: "model", KeyLabel: "Anthropic API key", Models: []string{"cl-1"}},
		{ID: "jev", Name: "TypeSafe Jev", Kind: "detector", KeyLabel: "Jev API key"},
	}
	s := keelclient.AgentSettings{Keys: map[string]keyFlag{"gemini": {true}}}
	s.Settings.Provider, s.Settings.Model, s.Settings.Detection, s.Settings.Suggestions, s.Settings.HistorySuggestions = "gemini", "gm-1", "auto", "on", "on"
	s.Settings.DefaultScope, s.Settings.ScrollbackLines, s.Settings.HistoryDays = "focused", 200, 30
	s.Settings.Redact.Secrets = true
	fb.agent.settings = s
	return fb
}

func agentSettingsRig(t *testing.T) *rig {
	t.Helper()
	r := settingsRig(t, agentSettingsBackend())
	r.tt.SetSize(1280, 3200) // the section is long: every control is on screen
	r.tt.Frame()
	r.section(t, "Agent")
	r.settleFrames()
	return r
}

// fields names the fields a patch sends.
func fields(p keelclient.AgentSettingsPatch) (out []string) {
	v := reflect.ValueOf(p)
	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).IsNil() {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	return out
}

func (r *rig) lastSave(t *testing.T) keelclient.AgentSettingsPatch {
	t.Helper()
	if len(r.fb.convs.saves) == 0 {
		t.Fatal("nothing was saved")
	}
	return r.fb.convs.saves[len(r.fb.convs.saves)-1]
}

func TestSettingsAgentIsTheAppsGroupsRowsAndWords(t *testing.T) {
	r := agentSettingsRig(t)
	r.texts(t, "The agent reads only the panes you allow and suggests commands. It never types into a pane on its own.",
		"Provider", "Model", "Used by the agent panel. Suggestions in panes use the provider's small, fast model.",
		"Suggestions in panes", "Suggest commands in panes", "Start a line with # and press ⇥, or press "+suggestKeyHere()+" on any line. Nothing is inserted until you accept, and nothing runs until you press ↵.",
		"Suggest from this pane's scrollback", "How to notice plain language",
		"Agent panel", "Default panes per conversation", "Which panes the agent may read when you start a conversation.",
		"Scrollback lines per pane", "50–1000. How much of each pane the agent reads. Default 200.",
		"Claude Code", "Allow Claude Code (keel mcp)", "Lets Claude Code list and read panes on hosts with Agent can read, each read logged, and propose commands that wait in the agent panel. It never types.",
		"Notes for the agent", "Your notes", "How you like to work, for every host. Sent with questions that read a pane on a host the agent may read, redacted like pane text. A host's own notes are in Edit host.",
		"Notes about you", "The agent may propose a note; nothing is saved until you press Save. Stored encrypted in the vault.",
		"Delete all notes", "Forget every host note and every note about you. Conversations stay.",
		"Safety", "The agent never types into a pane", "Secrets are always redacted", "Risky commands need ⇧⇥", "Redact IP addresses", "Redact hostnames",
		"History", "Keep conversations", "Delete all history", "Forget every conversation now. The Agent log keeps what was read.")
	// The always-on rows wear the badge; nothing says "coming soon".
	if got := strings.Count(strings.Join(r.tt.Texts(), "\n"), "Always on"); got < 3 {
		t.Errorf("%d Always on badges", got)
	}
	if strings.Contains(strings.ToLower(strings.Join(r.tt.Texts(), "\n")), "coming soon") {
		t.Error("a setting is marked coming soon")
	}
	// Notes for the agent sit after Claude Code and before Safety (notes_test.go has the rest).
	all := strings.Join(r.tt.Texts(), "\n")
	if !(strings.Index(all, "Claude Code") < strings.Index(all, "Notes for the agent") && strings.Index(all, "Notes for the agent") < strings.Index(all, "Safety")) {
		t.Error("Notes for the agent is not between Claude Code and Safety")
	}
}

func TestSettingsAgentControlsShowKeelServesValueAndSendOnlyWhatChanged(t *testing.T) {
	r := agentSettingsRig(t)
	for _, c := range []struct {
		name string
		want []string
		chk  func(p keelclient.AgentSettingsPatch) bool
	}{
		{"Suggest commands switch", []string{"Suggestions"}, func(p keelclient.AgentSettingsPatch) bool { return *p.Suggestions == "off" }},
		{"Suggest from this pane's scrollback switch", []string{"HistorySuggestions"}, func(p keelclient.AgentSettingsPatch) bool { return *p.HistorySuggestions == "off" }},
		{"Redact IP addresses switch", []string{"Redact"}, func(p keelclient.AgentSettingsPatch) bool {
			return *p.Redact == keelclient.AgentRedact{Secrets: true, IPs: true}
		}},
		{"Redact hostnames switch", []string{"Redact"}, func(p keelclient.AgentSettingsPatch) bool {
			return *p.Redact == keelclient.AgentRedact{Secrets: true, IPs: true, Hostnames: true}
		}},
		{"Default panes per conversation Every pane in the tab", []string{"DefaultScope"}, func(p keelclient.AgentSettingsPatch) bool { return *p.DefaultScope == "tab" }},
		{"Default panes per conversation Panes I pick each time", []string{"DefaultScope"}, func(p keelclient.AgentSettingsPatch) bool { return *p.DefaultScope == "pick" }},
	} {
		n := len(r.fb.convs.saves)
		r.click(t, c.name)
		r.settleFrames()
		if len(r.fb.convs.saves) != n+1 {
			t.Fatalf("%s: %d requests", c.name, len(r.fb.convs.saves)-n)
		}
		p := r.lastSave(t)
		if !slices.Equal(fields(p), c.want) || !c.chk(p) {
			t.Errorf("%s sent %v %+v", c.name, fields(p), p)
		}
	}
	// What keel serve answered is what the controls show.
	if v := r.fb.agent.settings.Settings; v.Suggestions != "off" || v.HistorySuggestions != "off" || !v.Redact.IPs || !v.Redact.Hostnames || v.DefaultScope != "pick" {
		t.Errorf("keel serve holds %+v", v)
	}
	// Suggestions off greys the detection radios: choosing one sends nothing.
	n := len(r.fb.convs.saves)
	r.tt.Click("How to notice plain language Only lines starting with #")
	r.settleFrames()
	if len(r.fb.convs.saves) != n {
		t.Error("a detection choice was sent while suggestions are off")
	}
	r.click(t, "Suggest commands switch")
	r.settleFrames()
	r.click(t, "How to notice plain language Only lines starting with #")
	r.settleFrames()
	if p := r.lastSave(t); !slices.Equal(fields(p), []string{"Detection"}) || *p.Detection != "hash-only" {
		t.Errorf("detection %v", p)
	}
	r.texts(t, "Pressing ↵ by mistake is harmless: the shell treats a line that starts with # as a comment.")
}

func TestSettingsAgentDetectionNoteNeedsJev(t *testing.T) {
	r := agentSettingsRig(t)
	r.texts(t, "Jev is not connected, so Keel notices only lines that start with # for now. Connect Jev above to notice plain language automatically.", "Needs a Jev key")
}

func TestSettingsAgentModelPicksTheProviderToo(t *testing.T) {
	r := agentSettingsRig(t)
	r.click(t, "Model picker")
	for _, m := range []string{"gm-1 · Gemini", "gm-2 · Gemini", "cl-1 · Claude"} {
		if !slices.Contains(r.tt.Menu(), m) {
			t.Fatalf("the model menu lacks %q: %v", m, r.tt.Menu())
		}
	}
	if err := r.tt.ChooseMenuItem("cl-1 · Claude"); err != nil {
		t.Fatal(err)
	}
	r.settleFrames()
	if p := r.lastSave(t); !slices.Equal(fields(p), []string{"Provider", "Model"}) || *p.Provider != "anthropic" || *p.Model != "cl-1" {
		t.Errorf("picking another provider's model sent %+v", p)
	}
	r.click(t, "Model picker")
	r.tt.ChooseMenuItem("cl-1 · Claude")
	r.settleFrames()
	n := len(r.fb.convs.saves)
	r.click(t, "Model picker")
	r.tt.ChooseMenuItem("cl-1 · Claude") // the one already chosen: nothing to save
	r.settleFrames()
	if len(r.fb.convs.saves) != n {
		t.Error("choosing the model that is chosen sent a request")
	}
}

func TestSettingsAgentScrollbackLinesRangeAndRequest(t *testing.T) {
	r := agentSettingsRig(t)
	p := pageAs[*settingsPage](r.w.pageTab("settings"))
	if p.linesV != 200 {
		t.Fatalf("the slider starts at %v: it shows what keel serve holds", p.linesV)
	}
	r.texts(t, "200")
	p.linesV = 20 // below the range
	r.settleFrames()
	if got := r.lastSave(t); !slices.Equal(fields(got), []string{"ScrollbackLines"}) || *got.ScrollbackLines != 50 {
		t.Errorf("below the range: %+v", got)
	}
	p.linesV = 5000
	r.settleFrames()
	if got := r.lastSave(t); *got.ScrollbackLines != 1000 || r.fb.agent.settings.Settings.ScrollbackLines != 1000 {
		t.Errorf("above the range: %+v, keel serve holds %d", got, r.fb.agent.settings.Settings.ScrollbackLines)
	}
	r.click(t, "Scrollback lines slider")
	r.settleFrames()
	if r.fb.agent.settings.Settings.ScrollbackLines < 50 || r.fb.agent.settings.Settings.ScrollbackLines > 1000 {
		t.Errorf("the slider sent %d", r.fb.agent.settings.Settings.ScrollbackLines)
	}
}

func TestSettingsAgentKeepConversationsAndDeleteAllHistoryAsksFirst(t *testing.T) {
	r := agentSettingsRig(t)
	r.fb.convs.list = savedList()
	r.click(t, "Keep conversations picker")
	for _, m := range []string{"Only while the panel is open", "7 days", "30 days", "90 days", "1 year"} {
		if !slices.Contains(r.tt.Menu(), m) {
			t.Errorf("the history menu lacks %q: %v", m, r.tt.Menu())
		}
	}
	if err := r.tt.ChooseMenuItem("Only while the panel is open"); err != nil {
		t.Fatal(err)
	}
	r.settleFrames()
	if p := r.lastSave(t); !slices.Equal(fields(p), []string{"HistoryDays"}) || *p.HistoryDays != 0 {
		t.Errorf("history %+v", p)
	}
	if r.w.agent.savedNote() != "Not saved · kept while open" {
		t.Errorf("the panel says %q", r.w.agent.savedNote())
	}
	// Delete all history asks first, and Cancel keeps everything.
	r.click(t, "Delete all history…")
	r.settleFrames()
	cf := r.w.confirm
	if cf == nil || cf.title != "Delete all history?" || cf.action != "Delete all history" || r.fb.convs.deletedAll != 0 ||
		cf.message != "Every agent conversation is forgotten, including the ones saved in the vault, in every window. The log entries that say what the agent read stay in Logs." {
		t.Fatalf("no confirm, or not the app's: %+v", cf)
	}
	r.click(t, "Cancel")
	r.settleFrames()
	if r.fb.convs.deletedAll != 0 || r.w.confirm != nil {
		t.Fatal("Cancel deleted the history")
	}
	r.click(t, "Delete all history…")
	r.settleFrames()
	r.w.confirm.run() // the dialog's action (its button shares its words with the row's)
	r.settleFrames()
	if r.fb.convs.deletedAll != 1 {
		t.Fatalf("confirming did not delete the history (%d)", r.fb.convs.deletedAll)
	}
}

func TestSettingsAgentClaudeCodeIsOffByDefaultAndSendsOnlyClaudeCode(t *testing.T) {
	r := agentSettingsRig(t)
	if r.w.agentVal().Settings.ClaudeCode {
		t.Fatal("Claude Code starts on")
	}
	r.click(t, "Allow Claude Code (keel mcp) switch")
	r.settleFrames()
	p := r.lastSave(t)
	if !slices.Equal(fields(p), []string{"ClaudeCode"}) || !*p.ClaudeCode || !r.fb.agent.settings.Settings.ClaudeCode {
		t.Fatalf("on: %v %+v", fields(p), p)
	}
	r.click(t, "Allow Claude Code (keel mcp) switch")
	r.settleFrames()
	p = r.lastSave(t)
	if !slices.Equal(fields(p), []string{"ClaudeCode"}) || *p.ClaudeCode || r.fb.agent.settings.Settings.ClaudeCode {
		t.Fatalf("off: %v %+v", fields(p), p)
	}
	for _, call := range r.fb.calls {
		if strings.Contains(call, "mcp") {
			t.Errorf("a call named mcp: %s", call)
		}
	}
}

func TestSettingsAgentKeyFieldIsMaskedSentOnceAndNeverKept(t *testing.T) {
	r := agentSettingsRig(t)
	// Gemini is connected: Change key opens the field; Anthropic is not: its field is open.
	if !r.has("badge Connected Google Gemini") || !r.has("badge Not connected Anthropic Claude") || r.has("Gemini API key input") {
		t.Fatal("the cards do not show who is connected")
	}
	for _, s := range []string{"Connect Anthropic Claude", "Change key Google Gemini", "Disconnect Google Gemini"} {
		if !r.has(s) {
			t.Errorf("no %q", s)
		}
	}
	r.click(t, "Anthropic API key input")
	r.tt.Type(fakeProviderKey)
	r.tt.Frame()
	p := pageAs[*settingsPage](r.w.pageTab("settings"))
	if p.card("anthropic").field != fakeProviderKey {
		t.Fatal("the field did not take the key")
	}
	if strings.Contains(strings.Join(r.tt.Texts(), "\n"), fakeProviderKey) {
		t.Fatal("the key is drawn")
	}
	r.click(t, "Connect Anthropic Claude")
	r.settleFrames()
	if len(r.fb.agent.keys) != 1 || r.fb.agent.keys[0].Provider != "anthropic" || r.fb.agent.keys[0].Key != fakeProviderKey {
		t.Fatalf("keys sent %v", r.fb.agent.keys)
	}
	if p.card("anthropic").field != "" {
		t.Error("the field still holds the key")
	}
	// Connected now; the key is nowhere: not in a toast, a label, a log line, a printed value, or a file in the data dir.
	if !r.has("badge Connected Anthropic Claude") {
		t.Error("the card did not turn Connected")
	}
	all := strings.Join(r.tt.Texts(), "\n") + fmt.Sprintf("%v %+v %#v", r.fb.agent.keys[0], r.fb.agent.keys[0], r.fb.agent.keys[0]) + fmt.Sprint(r.w.queued) + fmt.Sprintf("%+v", p.card("anthropic"))
	if strings.Contains(all, fakeProviderKey) {
		t.Error("the key is shown or printed")
	}
	entries, _ := os.ReadDir(r.w.dataDir)
	for _, e := range entries {
		if b, _ := os.ReadFile(filepath.Join(r.w.dataDir, e.Name())); strings.Contains(string(b), fakeProviderKey) {
			t.Errorf("%s holds the key", e.Name())
		}
	}
	// A refused key: the card says why, the field is empty, the key is not in the words.
	r.fb.agent.keyErr = &keelclient.StatusError{Code: 400, Msg: "the key is empty, too long, or has spaces"}
	r.click(t, "Change key Google Gemini")
	r.settleFrames()
	r.click(t, "Gemini API key input")
	r.tt.Type(fakeProviderKey)
	r.tt.Frame()
	r.click(t, "Save key Google Gemini")
	r.settleFrames()
	if !r.tt.HasText("the key is empty, too long, or has spaces") || p.card("gemini").field != "" || strings.Contains(strings.Join(r.tt.Texts(), "\n"), fakeProviderKey) {
		t.Errorf("a refused key: %v", r.tt.Texts())
	}
	// An empty field is not sent.
	n := len(r.fb.agent.keys)
	r.click(t, "Save key Google Gemini")
	r.settleFrames()
	if len(r.fb.agent.keys) != n || !r.tt.HasText("Paste a key first.") {
		t.Error("an empty key was sent")
	}
}

func TestSettingsAgentDisconnectAsksFirstWithTheAppsWords(t *testing.T) {
	r := agentSettingsRig(t)
	r.click(t, "Disconnect Google Gemini")
	r.settleFrames()
	cf := r.w.confirm
	if cf == nil || cf.title != "Disconnect Google Gemini?" || cf.action != "Disconnect" || len(r.fb.convs.keysGone) != 0 ||
		cf.message != "The agent panel and suggestions will stop working until you connect a key again. The Google Gemini key is removed from the vault." {
		t.Fatalf("no confirm, or not the app's: %+v", cf)
	}
	r.click(t, "Cancel")
	r.settleFrames()
	if len(r.fb.convs.keysGone) != 0 {
		t.Fatal("Cancel removed the key")
	}
	r.click(t, "Disconnect Google Gemini")
	r.settleFrames()
	r.w.confirm.run() // the dialog's action (its button shares its words with the card's)
	r.w.confirm = nil
	r.settleFrames()
	if !slices.Equal(r.fb.convs.keysGone, []string{"gemini"}) || !r.has("badge Not connected Google Gemini") {
		t.Fatalf("removed %v", r.fb.convs.keysGone)
	}
}

func TestSettingsSearchFindsAgentRowsAndAgentHasNoSecretRowText(t *testing.T) {
	r := agentSettingsRig(t)
	r.section(t, "Appearance")
	r.click(t, "Search settings")
	r.tt.Type("claude code")
	r.settleFrames()
	if !r.tt.HasText("Allow Claude Code (keel mcp)") || r.tt.HasText("Window opacity") {
		t.Errorf("the search did not find the Agent row: %v", r.tt.Texts())
	}
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("scrollback")
	r.settleFrames()
	if !r.tt.HasText("Scrollback lines per pane") || !r.tt.HasText("Suggest from this pane's scrollback") {
		t.Errorf("scrollback: %v", r.tt.Texts())
	}
}

func TestRetentionSettingsOpensSettingsAgent(t *testing.T) {
	r := savedRig(t)
	r.openPageSettled()
	r.click(t, "Retention settings")
	r.settleFrames()
	tb := r.w.tab()
	if tb == nil || tb.pg == nil || pageAs[*settingsPage](tb) == nil || pageAs[*settingsPage](tb).section != "agent" || !r.tt.HasText("Keep conversations") {
		t.Fatalf("Retention settings did not open Settings · Agent: %v", r.tt.Texts())
	}
}
