package runtime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"keel/internal/session"
	"keel/internal/store"
)

var seqToken = regexp.MustCompile(`<(\d{5})>`)

// Clients that attach while output streams must see every chunk exactly
// once: their snapshot followed by the events they receive has to read as
// one unbroken, non-repeating sequence.
func TestAttachDuringOutputSeesEachChunkOnce(t *testing.T) {
	rt, sess, fb := spawnReady(t)
	// Ends the session and its bridge so the writer can't block forever
	// if a viewer fails early.
	t.Cleanup(rt.KillAll)

	const total = 3000
	const viewers = 64
	type result struct {
		name string
		text string
	}
	results := make(chan result, viewers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for v := 0; v < viewers; v++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			<-start
			// Spread the attaches across the stream.
			time.Sleep(time.Duration(v) * 200 * time.Microsecond)
			cl, ch, snap := sess.Subscribe(Join{Name: fmt.Sprintf("v%d", v), Kind: "web"})
			defer sess.Unsubscribe(cl.ID)
			text := string(snap)
			end := fmt.Sprintf("<%05d>", total-1)
			deadline := time.After(10 * time.Second)
			for !strings.Contains(text, end) {
				select {
				case ev, ok := <-ch:
					if !ok {
						results <- result{fmt.Sprintf("v%d", v), text + " [channel closed]"}
						return
					}
					if ev.Kind == "bytes" {
						text += string(ev.Bytes)
					}
				case <-deadline:
					results <- result{fmt.Sprintf("v%d", v), text + " [timeout]"}
					return
				}
			}
			results <- result{fmt.Sprintf("v%d", v), text}
		}(v)
	}
	close(start)
	go func() {
		// 10 tokens per write keeps the stream to ~300 events, under the
		// 512-event client buffer, so no viewer is dropped as a slow
		// consumer however late it starts reading.
		for i := 0; i < total; i += 10 {
			var b strings.Builder
			for j := i; j < i+10 && j < total; j++ {
				fmt.Fprintf(&b, "<%05d>", j)
			}
			if _, err := fb.outW.Write([]byte(b.String())); err != nil {
				return
			}
		}
	}()
	wg.Wait()
	close(results)

	for r := range results {
		matches := seqToken.FindAllStringSubmatch(r.text, -1)
		if len(matches) == 0 {
			t.Fatalf("%s saw no output: %q", r.name, tail(r.text))
		}
		prev, _ := strconv.Atoi(matches[0][1])
		if prev != 0 {
			t.Fatalf("%s: stream starts at %05d, want 00000 (output lost before the snapshot)", r.name, prev)
		}
		for _, m := range matches[1:] {
			n, _ := strconv.Atoi(m[1])
			if n != prev+1 {
				t.Fatalf("%s: chunk %05d followed by %05d (repeated or missing output)", r.name, prev, n)
			}
			prev = n
		}
		if prev != total-1 {
			t.Fatalf("%s: stream ended at %05d: %q", r.name, prev, tail(r.text))
		}
	}
}

func tail(s string) string {
	if len(s) > 200 {
		return "…" + s[len(s)-200:]
	}
	return s
}

// Herd clients must hear "spawn" for a session before any "update" for it;
// runLoop used to start first, and a fast connect reported "ready" ahead
// of the spawn event (the intermittent TestHerdWS failure).
func TestHerdSpawnPrecedesUpdates(t *testing.T) {
	st := testStore(t)
	host := firstHost(t, st)
	rt := New(st, func(store.Host, int, int) (session.Bridge, error) {
		return newFakeBridge(), nil
	})
	t.Cleanup(rt.KillAll)
	hid, ch, _ := rt.SubscribeHerd()
	defer rt.UnsubscribeHerd(hid)

	const spawns = 25
	seen := map[string]bool{}
	for i := 0; i < spawns; i++ {
		info, err := rt.Spawn(host.ID, 80, 24)
		if err != nil {
			t.Fatal(err)
		}
		// Drain until this session's spawn event; nothing about it may come first.
		deadline := time.After(2 * time.Second)
		for !seen[info.ID] {
			select {
			case ev := <-ch:
				if ev.Type == "spawn" {
					seen[ev.ID] = true
					continue
				}
				if !seen[ev.ID] {
					t.Fatalf("herd %q for session %s arrived before its spawn", ev.Type, ev.ID)
				}
			case <-deadline:
				t.Fatalf("no spawn event for %s", info.ID)
			}
		}
	}
}
