package native

import (
	"context"
	"testing"
	"time"
)

// typingDuringDelete is a backend that types into the notebook pane, and lets the autosave run, while its delete is in flight.
type typingDuringDelete struct {
	*fakeBackend
	typed func()
}

func (b *typingDuringDelete) DeleteNotebook(ctx context.Context, id string, permanent bool) error {
	b.typed()
	return b.fakeBackend.DeleteNotebook(ctx, id, permanent)
}

func TestTypingDuringADeleteDoesNotSaveTheNotebookBeingDeleted(t *testing.T) {
	r := nbRig(t, "# a")
	p := r.openNB(t, "nb1")
	r.w.be = &typingDuringDelete{fakeBackend: r.fb, typed: func() {
		p.nb.md += " late"
		p.nb.auto.edit(time.Now())
		r.w.nbPump(p)
	}}
	puts := r.fb.nb.reqsWith("put")
	r.w.deleteNotebook("nb1", false)
	if got := r.fb.nb.reqsWith("put") - puts; got != 0 {
		t.Errorf("%d saves of the notebook while it was being deleted", got)
	}
}
