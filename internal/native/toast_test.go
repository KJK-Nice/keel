package native

import (
	"errors"
	"testing"
	"time"

	"keel/internal/native/keelclient"
)

func TestToastShowsAndExpiresAfterFiveSeconds(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.w.say("Host saved")
	r.tt.Frame()
	if !r.tt.HasText("Host saved") {
		t.Fatal("the toast is not shown")
	}
	r.tt.Advance(toastTimeout - time.Second)
	r.tt.Frame()
	if !r.tt.HasText("Host saved") {
		t.Fatal("the toast went before its time")
	}
	r.tt.Advance(2 * time.Second)
	r.tt.Frame()
	if r.tt.HasText("Host saved") {
		t.Fatal("the toast stays past 5 s")
	}
}

func TestToastErrorHasAStatusIconAndTheServersWords(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.w.warn("Host not found")
	r.w.say("Copied")
	r.tt.Frame()
	if _, ok := r.tt.Find("toast Host not found"); !ok || !r.tt.HasText("Copied") {
		t.Fatalf("toasts %v", r.tt.Texts())
	}
	if got := errText("save", &keelclient.StatusError{Msg: "name is required"}); got != "name is required" {
		t.Errorf("a refusal says %q", got)
	}
	if got := errText("save", errors.New("boom")); got != "save: boom" {
		t.Errorf("a failure says %q", got)
	}
}

func TestToastShowsTheNewestThreeAndTheSameMessageOnce(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	for _, m := range []string{"one", "two", "two", "three", "four"} {
		r.w.say(m)
	}
	r.tt.Frame()
	if r.tt.HasText("one") || !r.tt.HasText("two") || !r.tt.HasText("three") || !r.tt.HasText("four") {
		t.Fatalf("toasts %v", r.tt.Texts())
	}
}

func TestToastActionRunsAndCloses(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	ran := 0
	r.w.sayAction("Forward stopped", "Undo", func() { ran++ })
	r.tt.Frame()
	if !r.tt.HasText("Forward stopped") {
		t.Fatal("the toast is not shown")
	}
	r.click(t, "Undo")
	if ran != 1 {
		t.Fatalf("the action ran %d times", ran)
	}
	r.tt.Advance(time.Second) // MyGo closes it with an exit transition
	r.tt.Frame()
	if r.tt.HasText("Forward stopped") {
		t.Error("the toast stays after its action")
	}
}

func TestResultsThatWereInTheStatusBarAreToasts(t *testing.T) {
	r := newRig(t, okBackend())
	r.tt.Frame()
	r.w.newLocalTab()
	r.w.fail("hosts", errors.New("down"))
	r.tt.Frame()
	if !r.tt.HasText("hosts: down") {
		t.Fatalf("texts %v", r.tt.Texts())
	}
	for _, s := range r.tt.Texts() {
		if s == "hosts: down" {
			if _, ok := r.tt.Find("toast hosts: down"); !ok {
				t.Error("a failure is not in a toast")
			}
		}
	}
}
