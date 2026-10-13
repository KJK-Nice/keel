package runtime

import (
	"bytes"
	"testing"
	"time"
)

func TestRingBytesSince(t *testing.T) {
	r := newRing(8)
	r.Write([]byte("abc"))
	m := r.Mark()
	if got := r.BytesSince(m); got != nil {
		t.Fatalf("nothing written since mark, got %q", got)
	}
	r.Write([]byte("de"))
	if got := r.BytesSince(m); string(got) != "de" {
		t.Fatalf("since mark = %q, want de", got)
	}
	r.Write([]byte("fghijk")) // ring now holds the last 8 bytes: "defghijk"
	if got := r.BytesSince(m); string(got) != "defghijk" {
		t.Fatalf("since mark after wrap = %q, want defghijk", got)
	}
	r.Write([]byte("lmnop"))
	if got := r.BytesSince(m); string(got) != "ijklmnop" {
		t.Fatalf("mark older than the ring = %q, want the whole ring", got)
	}
	if got := r.BytesSince(0); string(got) != "ijklmnop" {
		t.Fatalf("since 0 = %q", got)
	}
}

func TestSubscribeSinceHidesEarlierOutput(t *testing.T) {
	_, sess, fb := spawnReady(t)
	local, ch, _ := sess.Subscribe(Join{Name: "me", Kind: "web"})
	defer sess.Unsubscribe(local.ID)
	if _, err := fb.outW.Write([]byte("secret-before-share")); err != nil {
		t.Fatal(err)
	}
	waitBytes(t, ch, "secret-before-share")
	mark := sess.Mark()
	if _, err := fb.outW.Write([]byte("after-share")); err != nil {
		t.Fatal(err)
	}
	waitBytes(t, ch, "after-share")

	viewer, _, snap := sess.SubscribeSince(Join{Name: "bob", Kind: "webrtc"}, mark)
	defer sess.Unsubscribe(viewer.ID)
	if bytes.Contains(snap, []byte("secret-before-share")) {
		t.Fatalf("viewer snapshot leaked output from before the share: %q", snap)
	}
	if !bytes.Contains(snap, []byte("after-share")) {
		t.Fatalf("viewer snapshot %q missing output since the share", snap)
	}
}

func TestAnnounceSkipsShareViewers(t *testing.T) {
	_, sess, _ := spawnReady(t)
	local, localCh, _ := sess.Subscribe(Join{Name: "me", Kind: "web"})
	defer sess.Unsubscribe(local.ID)
	viewer, viewerCh, _ := sess.Subscribe(Join{Name: "bob", Kind: "webrtc"})
	defer sess.Unsubscribe(viewer.ID)
	drain := func(ch <-chan Event) {
		for {
			select {
			case <-ch:
			case <-time.After(50 * time.Millisecond):
				return
			}
		}
	}
	drain(localCh)
	drain(viewerCh)

	sess.Announce(Event{Kind: "join-request", Message: "carol", Peer: "p1"})
	select {
	case ev := <-localCh:
		if ev.Kind != "join-request" || ev.Peer != "p1" || ev.Message != "carol" {
			t.Fatalf("local got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("local client did not get the announcement")
	}
	select {
	case ev := <-viewerCh:
		t.Fatalf("share viewer got a host-only announcement: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}
