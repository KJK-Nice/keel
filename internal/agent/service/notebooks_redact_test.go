package service

import (
	"errors"
	"strings"
	"testing"

	"keel/internal/agent/redact"
	"keel/internal/store"
)

// An agent edit never changes, unmasks or probes a value Keel redacts
// (§6 rule 12). The model sees the notebook with such values replaced, so
// a find that touches one, or an edit that would stop one being redacted,
// is refused with the same answer as a find that is not there.

func nbOf(bodies ...string) store.Notebook {
	nb := store.Notebook{ID: "nb", Name: "Ops"}
	for i, b := range bodies {
		nb.Cells = append(nb.Cells, store.NotebookCell{ID: string(rune('a' + i)), Body: b})
	}
	return nb
}

func tryEdit(t *testing.T, nb store.Notebook, r *redact.Redactor, find, replace string) (store.Notebook, error) {
	t.Helper()
	return applyAgentEdit(nb, []findReplace{{find, replace}}, nil, "", r)
}

func TestAgentEditCannotTouchRedactedValues(t *testing.T) {
	r := redact.New(redact.Options{Known: []string{vaultPass}})
	mysql := nbOf("## DB\n\nmysql -u root -phunter2x\n")
	if len(r.Matches(mysql.Cells[0].Body)) == 0 {
		t.Fatal("the test notebook holds no redacted value")
	}
	// 1. Unmask: drop the context that makes the detector see the value.
	if _, err := tryEdit(t, mysql, r, "mysql -u root -p", "echo "); !errors.Is(err, errNoMatch) {
		t.Fatalf("unmask by replacing the context → %v", err)
	}
	if _, err := tryEdit(t, mysql, r, "mysql -u root", "echo"); !errors.Is(err, errNoMatch) {
		t.Fatalf("unmask by replacing context that does not touch the value → %v", err)
	}
	// 2. Oracle: a right and a wrong guess get the same answer.
	_, right := tryEdit(t, mysql, r, "-phunt", "x")
	_, wrong := tryEdit(t, mysql, r, "-phunq", "x")
	if !errors.Is(right, errNoMatch) || !errors.Is(wrong, errNoMatch) {
		t.Fatalf("guesses → %v / %v; want the same no-match", right, wrong)
	}
	// 3. Data loss: part of the value replaced.
	if _, err := tryEdit(t, mysql, r, "-phunter2", "-pX"); !errors.Is(err, errNoMatch) {
		t.Fatalf("partial overwrite → %v", err)
	}
	// Adjacent on either side: refused; one character away and harmless: fine.
	adj := nbOf("token=" + nbGitHub + "; next")
	if _, err := tryEdit(t, adj, r, "token=", "tok="); !errors.Is(err, errNoMatch) {
		t.Fatalf("touching before → %v", err)
	}
	if _, err := tryEdit(t, adj, r, "; next", "!"); !errors.Is(err, errNoMatch) {
		t.Fatalf("touching after → %v", err)
	}
	if got, err := tryEdit(t, adj, r, "next", "then"); err != nil || !strings.Contains(got.Cells[0].Body, nbGitHub+"; then") {
		t.Fatalf("an edit away from the value → %+v %v", got, err)
	}
	// Several detectors in one cell, and a vault value: each guarded.
	multi := nbOf("curl -H 'Authorization: Bearer abcdefghijklmnop0123456789' -u admin:" + vaultPass + " https://x.example/\nexport API_KEY=Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK\nls -la\n")
	for _, find := range []string{"Bearer ", "admin:", "API_KEY=", vaultPass[:8], "6789'"} {
		if _, err := tryEdit(t, multi, r, find, "Z"); !errors.Is(err, errNoMatch) {
			t.Errorf("find %q → %v", find, err)
		}
	}
	if got, err := tryEdit(t, multi, r, "ls -la", "ls -lah"); err != nil || !strings.Contains(got.Cells[0].Body, vaultPass) {
		t.Fatalf("a plain edit → %v", err)
	}
	// A cell Keel cannot map exactly (control characters) takes no edit.
	ctl := nbOf("ls\x1b[31m -la")
	if _, err := tryEdit(t, ctl, r, "ls", "dir"); !errors.Is(err, errNoMatch) {
		t.Fatalf("an inexact cell → %v", err)
	}
}

// Edits keep cells and their ids, and a find must match within one cell.
func TestAgentEditKeepsCells(t *testing.T) {
	r := redact.New(redact.Options{})
	nb := nbOf("first cell end", "start second cell")
	if _, err := tryEdit(t, nb, r, "end\n\nstart", "x"); !errors.Is(err, errNoMatch) {
		t.Fatalf("a find across cells → %v", err)
	}
	got, err := tryEdit(t, nb, r, "second", "2nd")
	if err != nil || len(got.Cells) != 2 || got.Cells[0].ID != "a" || got.Cells[1].ID != "b" || got.Cells[1].Body != "start 2nd cell" || got.Cells[0].Body != "first cell end" {
		t.Fatalf("edit → %+v %v", got, err)
	}
	if _, err := tryEdit(t, nbOf("same", "same"), r, "same", "x"); !errors.Is(err, errNoMatch) {
		t.Fatalf("a find in two cells → %v", err)
	}
}
