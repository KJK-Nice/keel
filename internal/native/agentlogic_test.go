package native

import (
	"regexp"
	"slices"
	"testing"

	"keel/internal/native/keelclient"
)

var testAgentPanes = []agentPane{
	{sessionID: "s1", label: "web-1", focused: true, inActiveTab: true, readable: true, driving: true},
	{sessionID: "s2", label: "prod-db-01", inActiveTab: true, readable: true, production: true, driving: true},
	{sessionID: "s3", label: "secret-box", inActiveTab: true, driving: true},
	{sessionID: "s4", label: "other-tab", readable: true},
	{sessionID: "r1", label: "joined", readable: true, remote: true},
}

func ids(ps []agentPane) (out []string) {
	for _, p := range ps {
		out = append(out, p.sessionID)
	}
	return out
}

func TestScopeChipText(t *testing.T) {
	for _, c := range []struct {
		n, m int
		want string
	}{{2, 3, "Reads 2 of 3 panes"}, {1, 1, "Reads 1 of 1 pane"}, {0, 0, "Reads 0 of 0 panes"}} {
		if got := scopeLabel(c.n, c.m); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
}

func TestJoinedPanesAreNeverCountedOrPicked(t *testing.T) {
	if got := ids(candidatePanes(testAgentPanes)); !slices.Equal(got, []string{"s1", "s2", "s3", "s4"}) {
		t.Errorf("candidates %v", got)
	}
	if got := toggleScope(nil, &testAgentPanes[4]); len(got) != 0 {
		t.Errorf("a joined pane was added: %v", got)
	}
}

func TestDefaultScopeFollowsTheSettingAndSkipsUnreadableHosts(t *testing.T) {
	for mode, want := range map[string][]string{"focused": {"s1"}, "tab": {"s1", "s2"}, "pick": {}} {
		if got := defaultScope(mode, testAgentPanes); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", mode, got, want)
		}
	}
	unreadable := testAgentPanes[2]
	unreadable.focused = true
	if got := defaultScope("focused", []agentPane{unreadable}); len(got) != 0 {
		t.Errorf("a focused pane the agent cannot read was picked: %v", got)
	}
}

func TestScopeKeepsOnlyOpenReadablePanesAndNeverAddsAnUnreadableOne(t *testing.T) {
	if got := reconcileScope([]string{"s2", "gone", "s3", "s2"}, testAgentPanes); !slices.Equal(got, []string{"s2"}) {
		t.Errorf("reconcile %v", got)
	}
	if got := toggleScope([]string{"s1"}, &testAgentPanes[2]); !slices.Equal(got, []string{"s1"}) {
		t.Errorf("an unreadable pane was added: %v", got)
	}
	if got := toggleScope([]string{"s1"}, &testAgentPanes[1]); !slices.Equal(got, []string{"s1", "s2"}) {
		t.Errorf("add %v", got)
	}
	if got := toggleScope([]string{"s1", "s2"}, &testAgentPanes[0]); !slices.Equal(got, []string{"s2"}) {
		t.Errorf("remove %v", got)
	}
}

func TestBytesShowEveryControlCharacterAsAGlyph(t *testing.T) {
	for in, want := range map[string]string{
		"ls -la\n":          "ls -la↵",
		"a\tb\x1b[0m\x03\n": "a⇥b⎋[0m^C↵",
		"\x15echo hi\n":     "^Uecho hi↵",
		"one\ntwo\n":        "one↵\ntwo↵",
		"x\u0085y":          `x\x85y`,
	} {
		if got := bytesText(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	runs := bytesRuns("\x15ls\n")
	if len(runs) != 3 || !runs[0].ctl || runs[1].ctl || !runs[2].ctl {
		t.Errorf("runs %+v: the glyphs are not set apart from the text", runs)
	}
}

func TestByteAndLineCountsMatchWhatIsTyped(t *testing.T) {
	for in, want := range map[string]string{"uptime\n": "1 line · 7 bytes", "a\nb\n": "2 lines · 4 bytes", "echo é\n": "1 line · 8 bytes", "\x15echo keel-agent-ok\n": "1 line · 20 bytes"} {
		if got := countLabel(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestRiskBadges(t *testing.T) {
	for risk, want := range map[string]string{"readonly": "Read-only", "changes": "Changes state", "destructive": "Destructive", "???": "Changes state"} {
		if got, _ := riskBadge(risk); got != want {
			t.Errorf("%q: %q, want %q", risk, got, want)
		}
	}
}

func TestTraceLinesSaySayWhatWasRead(t *testing.T) {
	for _, c := range []struct {
		step keelclient.TraceStep
		want string
	}{
		{keelclient.TraceStep{Status: "read", Host: "prod-db-01", Lines: 120}, "Read prod-db-01 · last 120 lines"},
		{keelclient.TraceStep{Status: "read", Host: "web", Lines: 1, Redactions: map[string]int{"password_prompt": 1, "aws_key": 2}}, "Read web · last 1 line · 3 redacted"},
		{keelclient.TraceStep{Status: "read", Host: "web", Lines: 2, Command: "exit 2"}, "Read web · last command, exit 2 · 2 lines of output"},
		{keelclient.TraceStep{Status: "read", Host: "web", Lines: 1, Command: "still running", Redactions: map[string]int{"secret": 1}}, "Read web · last command, still running · 1 line of output · 1 redacted"},
		{keelclient.TraceStep{Status: "skipped", Reason: "not-readable", Host: "box"}, "Skipped box · the agent can't read this host"},
		{keelclient.TraceStep{Status: "skipped", Reason: "not-open"}, "Skipped a pane · it is closed"},
		{keelclient.TraceStep{Status: "skipped", Reason: "other", Host: "box"}, "Skipped box · could not read it"},
		{keelclient.TraceStep{Status: "notes", NoteKind: "host", Host: "web", Notes: 2}, "Used host notes · web (2)"},
	} {
		if got := traceText(c.step); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
}

func TestTypeIsOfferedOnlyToTheDriverOfAnOpenLocalPane(t *testing.T) {
	p := keelclient.Proposal{SessionID: "s1"}
	if got := typeOffer(p, &testAgentPanes[0]); got != offerType {
		t.Errorf("driver: %q", got)
	}
	for name, c := range map[string]struct {
		p    keelclient.Proposal
		pane *agentPane
		want string
	}{
		"following": {keelclient.Proposal{SessionID: "s4"}, &testAgentPanes[3], offerFollowing},
		"joined":    {keelclient.Proposal{SessionID: "r1"}, &testAgentPanes[4], offerFollowing},
		"closed":    {keelclient.Proposal{SessionID: "gone"}, nil, offerClosed},
		"no pane":   {keelclient.Proposal{}, &testAgentPanes[0], offerNone},
		"expired":   {keelclient.Proposal{SessionID: "s1", Expired: true}, &testAgentPanes[0], offerExpired},
		"secret":    {keelclient.Proposal{SessionID: "s1", Placeholders: true}, &testAgentPanes[0], offerPlaceholder},
	} {
		if got := typeOffer(c.p, c.pane); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
		if ok, reason := typeGate(c.p, c.pane, true); ok || reason != c.want {
			t.Errorf("%s: the gate let ⇧ through (%v %q)", name, ok, reason)
		}
	}
}

func TestDestructiveOrChangesOnProductionNeedsShift(t *testing.T) {
	de := keelclient.Proposal{SessionID: "s2", NeedsShift: true, Risk: "destructive"}
	if got := typeOffer(de, &testAgentPanes[1]); got != offerShift {
		t.Fatalf("offer %q", got)
	}
	if ok, reason := typeGate(de, &testAgentPanes[1], false); ok || reason != gateShift {
		t.Errorf("plain Type: %v %q", ok, reason)
	}
	if ok, _ := typeGate(de, &testAgentPanes[1], true); !ok {
		t.Error("⇧ + Type is refused")
	}
	if shiftReason(de) != "Destructive" || shiftReason(keelclient.Proposal{Risk: "changes"}) != "Changes state on a production host" {
		t.Error("the reasons are not the app's")
	}
	if ok, _ := typeGate(keelclient.Proposal{SessionID: "s1"}, &testAgentPanes[0], false); !ok {
		t.Error("a plain proposal is refused")
	}
}

func TestAtOpensThePickerOnlyAtTheStartOfAWord(t *testing.T) {
	for in, want := range map[string]bool{"@": true, "look at @": true, "a\n@": true, "deploy@host": false, "abc": false, "user@": false, "": false} {
		if got := atTrigger(in); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}

func TestConversationIDsMatchTheServersPattern(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	seen := map[string]bool{}
	for range 20 {
		id := newConversationID()
		if !re.MatchString(id) || seen[id] {
			t.Fatalf("%q", id)
		}
		seen[id] = true
	}
}

func TestACutAnswerGetsANoticeAFinishedOneNone(t *testing.T) {
	if cutNotice("") != "" || cutNotice("length") != "Answer cut off at the length limit. Ask the agent to continue." ||
		cutNotice("safety") != "Answer cut off by the provider's safety filter." || cutNotice("???") != "Answer cut off by the provider before it ended." {
		t.Error("the notices are not the app's")
	}
}

func TestTheAnswerIsParagraphsCodeBlocksAndInlineCode(t *testing.T) {
	blocks := mdBlocks("First **bold** and `code`.\nSame paragraph.\n\nSecond.\n\n```sh\ndf -h\n\nls\n```\nAfter.")
	if len(blocks) != 4 || blocks[0].text != "First **bold** and `code`.\nSame paragraph." || blocks[2].text != "df -h\n\nls" || !blocks[2].code || blocks[3].text != "After." {
		t.Fatalf("%+v", blocks)
	}
	got := mdInline("**Add your user** to `docker` and `a **b** c` then a ** b")
	want := []mdSpan{{text: "Add your user", bold: true}, {text: " to "}, {text: "docker", code: true}, {text: " and "}, {text: "a **b** c", code: true}, {text: " then a ** b"}}
	if !slices.Equal(got, want) {
		t.Errorf("%+v", got)
	}
	if b := mdBlocks("```\nopen"); len(b) != 1 || !b[0].code || b[0].text != "open" {
		t.Errorf("an unclosed fence: %+v", b)
	}
}

func TestPaneTargetNamesThePaneWithItsTab(t *testing.T) {
	for _, c := range [][3]string{{"prod-db-01", "deploy", "prod-db-01 · deploy"}, {"web", "web", "web"}, {"web", "", "web"}, {"", "t", "t"}} {
		if got := paneTarget(c[0], c[1]); got != c[2] {
			t.Errorf("%q", got)
		}
	}
}
