package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"keel/internal/agent"
	"keel/internal/agent/provider"
	"keel/internal/shellmark"
)

// cmdPanes is fakePanes with shell-integration marks: blocks holds each
// session's last command, and calls records which sessions were asked.
type cmdPanes struct {
	*fakePanes
	mu     sync.Mutex
	blocks map[string]shellmark.Block
	calls  []string
}

func (c *cmdPanes) LastCommand(id string) (shellmark.Block, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, id)
	b, ok := c.blocks[id]
	return b, ok
}

func newCmdFixture(t *testing.T, blocks map[string]shellmark.Block) (*fixture, *cmdPanes) {
	t.Helper()
	f := newFixture(t, "ok")
	panes := &cmdPanes{fakePanes: f.panes, blocks: blocks}
	svc, err := New(f.st, panes, f.log, Options{Factory: func(id, key string) (provider.Provider, error) { return f.fake, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f, panes
}

func TestAskPrefersLastCommandFromMarks(t *testing.T) {
	f, panes := newCmdFixture(t, map[string]shellmark.Block{
		"s-web": {
			Command: "make test\r\n",
			Output:  "\x1b[31mFAIL\x1b[0m pkg/db\r\nconnect with " + vaultPass + "\r\n",
			Done:    true, HasExit: true, Exit: 2,
			Cwd: "/srv/app",
		},
	})
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "why did it fail?", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.panes.reads) != 0 {
		t.Errorf("scrollback read %v although the pane has marks", f.panes.reads)
	}
	body := f.requestBodies(t)
	for _, want := range []string{"last command (from shell integration marks), exit 2", "(in /srv/app)", "$ make test", "FAIL pkg/db", "[exit 2]"} {
		if !strings.Contains(body, want) {
			t.Errorf("request lacks %q: %s", want, body)
		}
	}
	// The block goes through the same Redactor; the tail is not sent.
	for _, bad := range []string{vaultPass, "10:00 up 3 days", "\x1b[31m"} {
		if strings.Contains(body, bad) {
			t.Errorf("request holds %q", bad)
		}
	}
	if st := resp.Trace[0]; st.Status != "read" || st.Command != "exit 2" || st.Lines != 2 {
		t.Errorf("trace = %+v", st)
	}
	reads := f.logKinds(agent.EventRead)
	if len(reads) != 1 || !strings.Contains(reads[0].Message, "last command, exit 2") || reads[0].SessionID != "s-web" || reads[0].Redactions["known_secret"] == 0 {
		t.Fatalf("agent.read = %+v", reads)
	}
	if len(panes.calls) != 1 {
		t.Errorf("LastCommand calls = %v", panes.calls)
	}
}

// Scope comes first: a pane out of scope is not asked for its command.
func TestLastCommandRespectsScope(t *testing.T) {
	f, panes := newCmdFixture(t, map[string]shellmark.Block{
		"s-sec": {Command: "cat secret", Output: "UNREADABLE-HOST-MARKER", Done: true},
	})
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-sec"}}); err != nil {
		t.Fatal(err)
	}
	if len(panes.calls) != 0 {
		t.Fatalf("LastCommand asked for %v", panes.calls)
	}
	if strings.Contains(f.requestBodies(t), "UNREADABLE-HOST-MARKER") {
		t.Fatal("out-of-scope command reached the provider")
	}
}

// Without marks the tail is read as before.
func TestLastCommandFallsBackToTail(t *testing.T) {
	f, _ := newCmdFixture(t, nil)
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	body := f.requestBodies(t)
	if !strings.Contains(body, "last 6 lines") || !strings.Contains(body, "10:00 up 3 days") {
		t.Fatalf("no tail: %s", body)
	}
	if resp.Trace[0].Command != "" {
		t.Errorf("trace command %q", resp.Trace[0].Command)
	}
}

func TestLastCommandRunningAndLong(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&out, "line %d\r\n", i)
	}
	f, _ := newCmdFixture(t, map[string]shellmark.Block{
		"s-web": {Command: "tail -f log", Output: out.String()},
	})
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	body := f.requestBodies(t)
	for _, want := range []string{"still running", "(earlier output not included)", "line 80", "line 31"} {
		if !strings.Contains(body, want) {
			t.Errorf("request lacks %q", want)
		}
	}
	if strings.Contains(body, "line 30\\n") {
		t.Error("more than the scrollback-lines setting was sent")
	}
	if resp.Trace[0].Command != "still running" || resp.Trace[0].Lines != 50 {
		t.Errorf("trace = %+v", resp.Trace[0])
	}
}
