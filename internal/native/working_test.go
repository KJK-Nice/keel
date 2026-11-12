package native

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// Plan 030 step 4: loading.Blocks means "the model is working". It shows while an ask is in flight and stops on an answer, an
// error, Stop and a timeout; it is named for assistive technology; it never takes the input, so Stop stays clickable.

const workingLabel = "Agent is working"

// slowAsk starts an ask that holds until release is called; the work runs on its own goroutine, and what it posts back is
// run by finish on the test's goroutine.
func slowAsk(t *testing.T, r *rig, reply keelclient.AskResponse, err error) (release func(), finish func()) {
	t.Helper()
	posts := make(chan func(), 8)
	r.w.work = func(fn func()) { go fn() }
	r.w.post = func(fn func()) { posts <- fn }
	hold := make(chan struct{})
	started := make(chan struct{})
	r.fb.agent.askReply = func(req keelclient.AskRequest) (keelclient.AskResponse, error) {
		close(started)
		<-hold
		reply.ConversationID = req.ConversationID
		return reply, err
	}
	r.ask(t, "something slow")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the ask never started")
	}
	r.tt.Frame()
	return func() { close(hold) }, func() {
		t.Helper()
		select {
		case fn := <-posts:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("the ask never answered")
		}
		r.tt.Frame()
	}
}

func TestBlocksShowWhileTheModelWorksAndStopOnAnswerErrorAndTimeout(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{{"answer", nil}, {"error", errors.New("the provider said no")}, {"timeout", context.DeadlineExceeded}} {
		r := agentRig(t)
		release, finish := slowAsk(t, r, keelclient.AskResponse{Answer: "done"}, c.err)
		if !r.has(workingLabel) {
			t.Errorf("%s: no %q while the ask is in flight: %v", c.name, workingLabel, r.tt.Texts())
		}
		if !r.tt.HasText("Asking without reading a pane") && !r.tt.HasText("Reading 1 pane and asking") {
			t.Errorf("%s: the trace text changed: %v", c.name, r.tt.Texts())
		}
		if !r.tt.FramePending() {
			t.Errorf("%s: Blocks asks for no frame while it moves", c.name)
		}
		release()
		finish()
		if r.has(workingLabel) || r.w.agent.busy != nil {
			t.Errorf("%s: Blocks still shows after the ask ended", c.name)
		}
	}
}

func TestBlocksStopOnStopAndStopStaysClickable(t *testing.T) {
	r := agentRig(t)
	posts := make(chan func(), 8)
	r.w.work = func(fn func()) { go fn() }
	r.w.post = func(fn func()) { posts <- fn }
	r.fb.agent.askWait = make(chan struct{})
	r.ask(t, "slow one")
	select {
	case <-r.fb.agent.askWait:
	case <-time.After(2 * time.Second):
		t.Fatal("the ask never started")
	}
	r.tt.Frame()
	if !r.has(workingLabel) {
		t.Fatal("no Blocks while the ask is in flight")
	}
	r.click(t, "Stop") // Blocks sits beside the text and takes no input: the button works while it moves
	select {
	case fn := <-posts:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel the ask")
	}
	r.tt.Frame()
	if r.has(workingLabel) || !r.tt.HasText("Stopped") {
		t.Errorf("after Stop: %v", r.tt.Texts())
	}
}

// The indicator stands still and asks for no frames where the desktop asks for less motion (the plugin's own rule; this keeps
// our use of it from breaking it), and it is named.
func TestBlocksStandStillUnderReducedMotion(t *testing.T) {
	view := func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() { workingBlocks(c, ink, workingLabel) })
	}
	tt := ui.NewTesterWith(view, 200, 80, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true, TextScale: 1}})
	tt.Frame()
	if _, ok := tt.Find(workingLabel); !ok {
		t.Error("Blocks has no accessible name")
	}
	if tt.FramePending() {
		t.Error("Blocks asks for frames under reduced motion")
	}
	moving := ui.NewTester(view, 200, 80)
	moving.Frame()
	if !moving.FramePending() {
		t.Error("Blocks asks for no frames when motion is allowed")
	}
}
