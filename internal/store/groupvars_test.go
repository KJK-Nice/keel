package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Group variables (§17, part 2): set, updated in place, removed by an
// empty value, validated, kept across a reload and a folder form's save.
func TestGroupVarsSetAndKept(t *testing.T) {
	st, k8s, workers, _, _, _ := roleFixture(t)
	if _, err := st.SetGroupVar(k8s.ID, " K8S_Version ", " 1.31.2 "); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetGroupVar(k8s.ID, "api_vip", "10.20.0.10"); err != nil {
		t.Fatal(err)
	}
	g, err := st.SetGroupVar(k8s.ID, "k8s_version", "1.31.3")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(g.Vars) != "[{k8s_version 1.31.3} {api_vip 10.20.0.10}]" {
		t.Fatalf("vars %+v", g.Vars)
	}
	// A folder form's save cannot change them.
	if _, err := st.UpsertGroup(Group{ID: k8s.ID, Name: "k8s", Vars: []GroupVar{{Name: "evil", Value: "x"}}}); err != nil {
		t.Fatal(err)
	}
	// A new folder starts with none, whatever it sends.
	n, err := st.UpsertGroup(Group{Name: "new", Vars: []GroupVar{{Name: "evil", Value: "x"}}})
	if err != nil || n.Vars != nil {
		t.Fatalf("new folder %+v %v", n, err)
	}
	st2, err := New(st.path)
	if err != nil {
		t.Fatal(err)
	}
	g, _, _ = st2.GroupMembers(k8s.ID)
	if fmt.Sprint(g.Vars) != "[{k8s_version 1.31.3} {api_vip 10.20.0.10}]" {
		t.Fatalf("after reload %+v", g.Vars)
	}
	all, _ := st2.AllGroupVars()
	if len(all) != 1 || len(all[k8s.ID]) != 2 || all[workers.ID] != nil {
		t.Fatalf("all %+v", all)
	}
	// An empty value removes one.
	if g, _ = st2.SetGroupVar(k8s.ID, "api_vip", ""); len(g.Vars) != 1 {
		t.Fatalf("removed %+v", g.Vars)
	}
	if _, err := st2.SetGroupVar("nope", "a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
}

func TestGroupVarsValidated(t *testing.T) {
	st, k8s, _, _, _, _ := roleFixture(t)
	for _, c := range [][2]string{{"9x", "v"}, {"a-b", "v"}, {"end", "v"}, {"", "v"}, {"a", "x\ny"}, {"a", "x\x1by"}, {"a", "x​y"}, {"a", strings.Repeat("v", 257)}} {
		if _, err := st.SetGroupVar(k8s.ID, c[0], c[1]); !errors.Is(err, ErrGroupVar) {
			t.Errorf("%q=%q: %v", c[0], c[1], err)
		}
	}
	for i := 0; i < MaxGroupVars; i++ {
		if _, err := st.SetGroupVar(k8s.ID, fmt.Sprintf("v%d", i), "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SetGroupVar(k8s.ID, "one_more", "x"); !errors.Is(err, ErrGroupVar) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := st.SetGroupVar(k8s.ID, "v0", "y"); err != nil {
		t.Fatalf("update at the limit: %v", err)
	}
}

// A step's captures: valid names, a compiling pattern with a group, at most
// four, each name captured by one step only.
func TestRunbookCaptures(t *testing.T) {
	st := testStore(t)
	step := func(caps ...StepCapture) RunbookStep {
		return RunbookStep{Title: "init", Command: "kubeadm init", Captures: caps}
	}
	b, err := st.AddRunbook(Runbook{Name: "k8s", Steps: []RunbookStep{step(StepCapture{Name: " Join_Token ", Pattern: `--token\s+(\S+)`}), {Title: "join", Command: "kubeadm join --token {{join_token}}"}}})
	if err != nil {
		t.Fatal(err)
	}
	if c := b.Steps[0].Captures; len(c) != 1 || c[0].Name != "join_token" {
		t.Fatalf("captures %+v", c)
	}
	got, _ := st.GetRunbook(b.ID)
	got.Steps[0].Captures[0].Name = "changed"
	if again, _ := st.GetRunbook(b.ID); again.Steps[0].Captures[0].Name != "join_token" {
		t.Fatal("GetRunbook shares the captures slice")
	}
	bad := []Runbook{
		{Name: "x", Steps: []RunbookStep{step(StepCapture{Name: "9x", Pattern: `(a)`})}},
		{Name: "x", Steps: []RunbookStep{step(StepCapture{Name: "a", Pattern: `no group`})}},
		{Name: "x", Steps: []RunbookStep{step(StepCapture{Name: "a", Pattern: `(`})}},
		{Name: "x", Steps: []RunbookStep{step(StepCapture{Name: "a", Pattern: `(a)`}), step(StepCapture{Name: "a", Pattern: `(b)`})}},
		{Name: "x", Steps: []RunbookStep{step(StepCapture{Name: "a", Pattern: "(a)"}, StepCapture{Name: "b", Pattern: "(a)"}, StepCapture{Name: "c", Pattern: "(a)"}, StepCapture{Name: "d", Pattern: "(a)"}, StepCapture{Name: "e", Pattern: "(a)"})}},
	}
	for i, r := range bad {
		if _, err := st.AddRunbook(r); !errors.Is(err, ErrRunbook) {
			t.Errorf("bad %d kept: %v", i, err)
		}
	}
}
