package native

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// What the Approve dialog hands the one writer, whole, for each of its four sources: the bytes, every field of the
// reference and the options. The other tests of each source say why; this one keeps the frames from drifting.

// injectFrames is every approved inject a connection was asked for: the bytes, the reference and the options.
func injectFrames(c *fakeConn) (out []string) {
	for i, cmd := range c.injected {
		out = append(out, fmt.Sprintf("%q %+v %v", cmd, c.agentRefs[i], c.injectOpts[i]))
	}
	return out
}

func wantFrames(t *testing.T, c *fakeConn, want ...string) {
	t.Helper()
	if got := injectFrames(c); !slices.Equal(got, want) {
		t.Errorf("frames\n %q\nwant\n %q", got, want)
	}
}

func TestASnippetsFrameIsCtrlUTheTextANewlineAndNothingElse(t *testing.T) {
	r := approveRig(t)
	r.w.askType(fixed("echo hi"))
	r.tt.Frame()
	if got := r.w.approve.shown(); got != "echo hi\n" {
		t.Fatalf("the dialog shows %q", got)
	}
	r.click(t, "Type")
	wantFrames(t, r.fb.conns[0], `"\x15echo hi\n" <nil> []`)
}

func TestANotebookFencesFrameIsTheLinesWithNoCtrlUAndTheNotebookOption(t *testing.T) {
	r := nbRig(t, "```sh\necho one\necho two\n```\n")
	np := r.openNB(t, "nb1")
	r.tt.Click(fenceName("Type into api-1", fences(np.nb.md)[0].Index))
	r.frames(3)
	if got := r.w.approve.shown(); got != "echo one\necho two\n" {
		t.Fatalf("the dialog shows %q", got)
	}
	r.click(t, "Type")
	r.frames(3)
	wantFrames(t, r.fb.conns[0], fmt.Sprintf(`"echo one\necho two\n" <nil> [%d]`, keelclient.FromNotebook))
}

func TestAnAgentProposalsFrameIsItsOwnBytesAndItsReference(t *testing.T) {
	r := agentRig(t)
	r.answers(okProposal())
	r.ask(t, "q")
	r.click(t, "Type into pane…")
	if got := r.w.approve.shown(); got != "\x15echo keel-agent-ok\n" {
		t.Fatalf("the dialog shows %q", got)
	}
	r.click(t, "Type")
	conv := r.fb.agent.asks[0].ConversationID
	wantFrames(t, r.fb.conns[0], fmt.Sprintf(`"\x15echo keel-agent-ok\n" &{ConversationID:%s GroupRunID: ProposalID:p1 Shift:false} []`, conv))

	// ⇧ held on the card travels in the reference.
	s := agentRig(t)
	s.answers(destructive())
	s.ask(t, "delete")
	if err := s.tt.ClickWith(ui.Shift, "Type into pane…"); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	shown := s.w.approve.shown()
	s.click(t, "Type")
	conv = s.fb.agent.asks[0].ConversationID
	wantFrames(t, s.fb.conns[0], fmt.Sprintf(`%q &{ConversationID:%s GroupRunID: ProposalID:p1 Shift:true} []`, shown, conv))
}

func TestAGroupRowsFrameIsItsOwnBytesItsProposalAndTheRun(t *testing.T) {
	r := runRig(t, 3)
	r.startRun(runOf("echo grp-1", "readonly", 3, nil))
	r.pressTypeAll(t, 3)
	r.click(t, "Type into wk-2")
	r.click(t, "Type into wk-3")
	r.click(t, "Type into 2 panes")
	wantFrames(t, r.fb.conns[0])
	wantFrames(t, r.fb.conns[1], fmt.Sprintf(`%q &{ConversationID: GroupRunID:gr1 ProposalID:p2 Shift:false} []`, rawBytes))
	wantFrames(t, r.fb.conns[2], fmt.Sprintf(`%q &{ConversationID: GroupRunID:gr1 ProposalID:p3 Shift:false} []`, rawBytes))

	// ⇧ held with Type travels in every row's reference.
	s := runRig(t, 1)
	s.startRun(runOf("sudo systemctl restart kubelet", "changes", 1, func(_ int, c *keelclient.GroupRunCell) {
		c.Proposal.Production, c.Proposal.NeedsShift = true, true
	}))
	s.pressTypeAll(t, 1)
	bytes := s.groupDlg(t).rows[0].bytes
	s.click(t, "Type")
	wantFrames(t, s.fb.conns[0])
	if err := s.tt.ClickWith(ui.Shift, "Type"); err != nil {
		t.Fatal(err)
	}
	s.tt.Frame()
	wantFrames(t, s.fb.conns[0], fmt.Sprintf(`%q &{ConversationID: GroupRunID:gr1 ProposalID:p1 Shift:true} []`, bytes))
}
