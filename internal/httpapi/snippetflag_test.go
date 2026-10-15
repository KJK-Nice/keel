package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"keel/internal/store"
)

// "Let the agent suggest this snippet" changes only when a payload carries
// agentUsable: the old edit modal (which sends none) keeps it. Every
// change is an agent event that names the snippet, never its command.
func TestSnippetAgentFlagOnlyChangesWhenSent(t *testing.T) {
	f := newAgentFixture(t, testToken)
	code, b := f.do(t, "POST", "/api/snippets", map[string]any{"name": "deploy", "command": "echo keel-flag-body\n"})
	if code != 201 {
		t.Fatalf("POST %d %s", code, b)
	}
	var sn store.Snippet
	_ = json.Unmarshal(b, &sn)
	if sn.AgentUsable {
		t.Fatal("a new snippet is agent-usable by default")
	}
	flag := func() bool {
		all, _ := f.st.ListSnippets()
		for _, x := range all {
			if x.ID == sn.ID {
				return x.AgentUsable
			}
		}
		t.Fatal("snippet gone")
		return false
	}
	put := func(body map[string]any) {
		t.Helper()
		if code, b := f.do(t, "PUT", "/api/snippets/"+sn.ID, body); code != 200 {
			t.Fatalf("PUT %d %s", code, b)
		}
	}
	put(map[string]any{"id": sn.ID, "name": "deploy", "command": "echo keel-flag-body\n", "agentUsable": true})
	if !flag() {
		t.Fatal("explicit true not saved")
	}
	// The old modal: no agentUsable in the payload.
	put(map[string]any{"id": sn.ID, "name": "deploy 2", "command": "echo keel-flag-body\n"})
	if !flag() {
		t.Fatal("a payload without agentUsable cleared the flag")
	}
	put(map[string]any{"id": sn.ID, "name": "deploy 2", "command": "echo keel-flag-body\n", "agentUsable": false})
	if flag() {
		t.Fatal("explicit false not saved")
	}
	var agentEvents []string
	for _, e := range f.logs.Recent(100) {
		if e.Kind == "agent" && strings.Contains(e.Message, "snippet") {
			agentEvents = append(agentEvents, e.Message)
		}
		if strings.Contains(e.Message, "keel-flag-body") {
			t.Fatalf("keel.log holds the snippet body: %q", e.Message)
		}
	}
	if len(agentEvents) != 2 || !strings.Contains(agentEvents[0], "no longer") || !strings.Contains(agentEvents[1], "may be suggested") {
		t.Fatalf("agent events (newest first) = %q", agentEvents)
	}
}
