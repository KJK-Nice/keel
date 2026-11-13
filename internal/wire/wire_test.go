package wire

import (
	"encoding/json"
	"testing"
)

// A Control's bytes are those of a map with the same keys (the frames were maps once, and a test or a capture may hold
// their bytes): the fields of Control and of its parts are in key order.
func TestControlMarshalsInKeyOrder(t *testing.T) {
	ctl := Control{
		Type: TypeInject, Command: "ls", Nonce: "n1", Cols: 80, Rows: 24, TargetID: 7, Notebook: true, Seq: 3, Line: "show files",
		Agent:      &AgentType{ConversationID: "c1", GroupRunID: "g1", ProposalID: "p1", Shift: true},
		Suggestion: &AgentInsert{SuggestionID: "s1", Option: 2, Shift: true},
		Direct:     &DirectType{Ref: "d1", Kind: DirectNotebook, SnippetID: "sn1", NotebookID: "nb1", AltScreen: true, PromptLine: "$"},
	}
	got, err := json.Marshal(ctl)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(m) // a map marshals with its keys sorted
	if string(got) != string(want) {
		t.Fatalf("a Control marshals as\n%s\nwant its keys in order:\n%s", got, want)
	}
	if len(m) != 12 {
		t.Fatalf("%d keys, want every field of Control: %s", len(m), got)
	}
}

func TestRoleOfAndRiskRank(t *testing.T) {
	for id, want := range map[uint64]Role{1: RoleDriver, 2: RoleOwner, 3: RoleFollower} {
		if got := RoleOf(id, 1, 2); got != want {
			t.Errorf("client %d is %s, want %s", id, got, want)
		}
	}
	if RoleOf(1, 1, 1) != RoleDriver {
		t.Error("an owner who drives is the driver")
	}
	if !(RiskReadOnly.Rank() < RiskChanges.Rank() && RiskChanges.Rank() < RiskDestructive.Rank()) || Risk("other").Rank() != RiskChanges.Rank() {
		t.Error("risk rank")
	}
}
