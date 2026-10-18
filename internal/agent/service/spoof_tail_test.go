package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"keel/internal/agent"
	"keel/internal/shellmark"
)

// What keel serve's tracker yields for `cat evil.log` where evil.log holds
// a DANGER line, then a fake end, a fake `ls` and `total 0`, and a fake
// end (runtime: TestLastCommandBeforePartHoldsSpoofedOutput). The DANGER
// line is in neither the command nor the part after it; the excerpt still
// carries it, because it always covers the pane's plain tail.
func TestSpoofedEndCannotHideEarlierOutput(t *testing.T) {
	f, _ := newCmdFixture(t, map[string]shellmark.Block{
		"s-web": {
			Before:  "old prompt line\r\n\x1b]133;A\x07$ \x1b]133;B\x07cat evil.log\r\n\x1b]133;C\x07DANGER rm -rf /srv\r\n\x1b]133;D;0\x07\x1b]133;A\x07$ ",
			Command: "ls\r\n",
			Output:  "total 0\r\n",
			Done:    true, HasExit: true,
			After: "\x1b]133;D;0\x07\x1b]133;A\x07$ ",
		},
	})
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "what did I run?", Panes: []string{"s-web"}}); err != nil {
		t.Fatal(err)
	}
	user := f.fake.Requests()[0].Messages[0].Text.String()
	for _, want := range []string{"pane text before the reported command (raw, as printed):", "DANGER rm -rf /srv", "cat evil.log", "reported command: ls"} {
		if !strings.Contains(user, want) {
			t.Errorf("excerpt lacks %q:\n%s", want, user)
		}
	}
	if strings.Index(user, "DANGER") > strings.Index(user, "reported command:") {
		t.Error("the earlier text should come before the reported parts")
	}
	reads := f.logKinds(agent.EventRead)
	if len(reads) != 1 || !strings.Contains(reads[0].Message, "before it") {
		t.Fatalf("agent.read = %+v", reads)
	}
}

// In the normal case the excerpt is the pane's tail split into parts:
// every line once, and no more than the lines setting allows.
func TestLastCommandExcerptSendsNothingTwice(t *testing.T) {
	var before strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&before, "old-%03d\r\n", i)
	}
	before.WriteString("$ ")
	f, _ := newCmdFixture(t, map[string]shellmark.Block{
		"s-web": {Before: before.String(), Command: "make\r\n", Output: "out-1\r\nout-2\r\n", Done: true, HasExit: true, Exit: 1, After: "$ "},
	})
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-web"}}); err != nil {
		t.Fatal(err)
	}
	user := f.fake.Requests()[0].Messages[0].Text.String()
	for _, line := range []string{"out-1", "out-2", "old-100", "make"} {
		if c := strings.Count(user, line); c != 1 {
			t.Errorf("%q appears %d times", line, c)
		}
	}
	// 50 lines (the fixture's setting): 1 command + 2 output + 1 after + 46 earlier (45 old lines and the prompt).
	if strings.Contains(user, "old-055") || !strings.Contains(user, "old-056") {
		t.Errorf("earlier part is not the rest of the tail:\n%s", user)
	}
}
