package native

import (
	"errors"
	"testing"
)

func TestAFailedRefreshAtANewRevIsReadAgainByTheNextPoll(t *testing.T) {
	r := hostRig(t)
	r.fb.vault.Rev = "e.1"
	r.w.rev = "e.1"
	base := r.fb.hostsCalls
	r.fb.vault.Rev = "e.2"
	r.fb.hostsErr = errors.New("busy")
	r.w.pollTick()
	r.w.pollTick()
	if r.fb.hostsCalls != base+2 {
		t.Fatalf("hosts calls %d: a failed read at a new rev was not tried again", r.fb.hostsCalls-base)
	}
	r.w.pollTick()
	if r.fb.hostsCalls != base+2 || r.w.rev != "e.2" {
		t.Errorf("hosts calls %d rev %q: a read that worked is read again", r.fb.hostsCalls-base, r.w.rev)
	}
}
