package service

import (
	"context"
	"strings"
	"testing"

	"keel/internal/agent/provider"
)

// A reply the provider cut (Gemini's MAX_TOKENS: a long think used up the
// output limit) is never shown as a complete answer: the response says
// it was cut and why, the saved turn keeps that, keel.log gets the reason
// and the counts (never the text), and the model hears it next turn.
func TestCutAnswerIsMarkedLoggedAndKept(t *testing.T) {
	const partial = "Free space on /var is low because the WAL archive"
	f := newFixture(t, "")
	f.fake.Reply = provider.Response{Text: partial, OutputTokens: 10, ThoughtTokens: 2038, Stop: provider.StopLength}
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "why is /var full?", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Cut != provider.StopLength || resp.Answer != partial {
		t.Fatalf("cut = %q, answer = %q", resp.Cut, resp.Answer)
	}
	var cut []string
	for _, e := range f.logKinds("agent") {
		if strings.Contains(e.Message, "cut off") {
			cut = append(cut, e.Message)
			if strings.Contains(e.Message, "WAL") || e.Source != "gemini" {
				t.Errorf("log entry = %+v", e)
			}
		}
	}
	if len(cut) != 1 || !strings.Contains(cut[0], "(length)") || !strings.Contains(cut[0], "10 answer tokens, 2038 thinking tokens") {
		t.Fatalf("cut log = %q", cut)
	}
	tr, err := f.svc.Transcript(conv)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Turns) != 1 || tr.Turns[0].Cut != provider.StopLength {
		t.Fatalf("transcript turns = %+v", tr.Turns)
	}
	// The next ask tells the model its last reply was cut.
	f.fake.Reply = provider.Response{Text: "the rest"}
	resp, err = f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Cut != "" {
		t.Fatalf("a finished reply is marked cut: %q", resp.Cut)
	}
	reqs := f.fake.Requests()
	prev := reqs[len(reqs)-1].Messages[1].Text.String()
	if !strings.HasPrefix(prev, partial) || !strings.Contains(prev, "cut off before it ended (length)") {
		t.Fatalf("history = %q", prev)
	}
}

// A finished reply is not marked and logs nothing about a cut.
func TestFinishedAnswerIsNotMarked(t *testing.T) {
	f := newFixture(t, "All good.")
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "ok?"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Cut != "" {
		t.Fatalf("cut = %q", resp.Cut)
	}
	for _, e := range f.logKinds("agent") {
		if strings.Contains(e.Message, "cut off") {
			t.Fatalf("log entry = %+v", e)
		}
	}
}

// The panel asks for room for a whole answer, not the old 2048 tokens
// that a thinking model spent before it wrote anything.
func TestAskSendsAnswerBudget(t *testing.T) {
	f := newFixture(t, "ok")
	if _, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "q"}); err != nil {
		t.Fatal(err)
	}
	reqs := f.fake.Requests()
	if len(reqs) != 1 || reqs[0].MaxOutputTokens != maxAnswerTokens {
		t.Fatalf("MaxOutputTokens = %d, want %d", reqs[0].MaxOutputTokens, maxAnswerTokens)
	}
	if maxAnswerTokens*4 < maxAnswerBytes {
		t.Fatalf("maxAnswerTokens (%d) is too small for maxAnswerBytes (%d)", maxAnswerTokens, maxAnswerBytes)
	}
}

// A proposal block the cut left open is dropped: no proposal, no raw JSON
// in the answer. A complete block before it still counts.
func TestCutProposalBlockIsNotActedOn(t *testing.T) {
	const reply = "Check the lag first:\n\n```keel-proposal\n" +
		`{"pane": 1, "command": "uptime", "why": "Shows load", "risk": "readonly"}` + "\n```\n\nThen clear the archive:\n\n```keel-proposal\n" +
		`{"pane": 1, "command": "rm -rf /var/lib/postgresql/wal`
	f := newFixture(t, "")
	f.fake.Reply = provider.Response{Text: reply, Stop: provider.StopLength}
	resp, err := f.svc.Ask(context.Background(), AskRequest{ConversationID: conv, Question: "why lag", Panes: []string{"s-web"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Proposals) != 1 || resp.Proposals[0].Command != "uptime" {
		t.Fatalf("proposals = %+v", resp.Proposals)
	}
	if strings.Contains(resp.Answer, "rm -rf") || strings.Contains(resp.Answer, "keel-proposal") || !strings.HasSuffix(resp.Answer, "Then clear the archive:") {
		t.Fatalf("answer = %q", resp.Answer)
	}
	if resp.Cut != provider.StopLength {
		t.Fatalf("cut = %q", resp.Cut)
	}
}

// Cut notebook blocks are dropped the same way: a create, an edit or a
// read request that never closed is not parsed.
func TestCutNotebookBlocksAreDropped(t *testing.T) {
	for _, reply := range []string{
		"Here it is.\n\n```keel-notebook\n{\"title\": \"Failover\", \"body\": \"## Steps\\n",
		"Editing.\n\n```keel-notebook-edit\n{\"notebook\": 1, \"rev\": 1, \"edits\": [{\"find\": \"a\", \"replace\": \"",
		"Reading.\n\n```keel-notebook-read\n{\"notebooks\": [1",
		"A note.\n\n```keel-note\n{\"for\": \"user\", \"text\": \"prefer",
		"A runbook.\n\n````keel-runbook\n{\"name\": \"x\", \"steps\": [\n```sh\nls\n```\n",
	} {
		got := dropOpenBlock(reply)
		if strings.Contains(got, "keel-") || strings.Contains(got, "{") {
			t.Errorf("dropOpenBlock(%q) = %q", reply, got)
		}
		if _, nums := parseNotebookReads(got); len(nums) != 0 {
			t.Errorf("read request parsed from %q", got)
		}
		if _, nb := parseNotebookBlock(got); nb != nil {
			t.Errorf("notebook parsed from %q", got)
		}
	}
}

func TestDropOpenBlockLeavesClosedAndOtherFences(t *testing.T) {
	for _, s := range []string{
		"done",
		"```keel-proposal\n{\"command\": \"ls\"}\n```\nok",
		"Run:\n\n```sh\ndf -h /var",                               // a cut shell fence is text the user reads
		"```keel-notebook\n{\"body\": \"```sh\\nls\\n```\"}\n```", // inner fences escaped in one-line JSON
	} {
		if got := dropOpenBlock(s); got != s {
			t.Errorf("dropOpenBlock(%q) = %q", s, got)
		}
	}
	// An info string on a backtick line does not close a fence.
	in := "Text.\n```keel-notebook\n{\"body\": \"\n```sh\nls\n"
	if got := dropOpenBlock(in); got != "Text." {
		t.Errorf("dropOpenBlock(%q) = %q", in, got)
	}
}
