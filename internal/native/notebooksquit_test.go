package native

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"slime/internal/native/slimeclient"
)

// nbGate is a backend whose notebook saves can be held or made to meet, and which keeps the fake's shared state behind a lock
// so saves may overlap.
type nbGate struct {
	*fakeBackend
	mu      sync.Mutex
	base    sync.Mutex
	running int
	peak    int
	barrier int           // when set, a save waits until that many run at once, or 300 ms
	hold    chan struct{} // when set, the first save waits for it
	calls   int
	bodies  []string
	revs    []int
	hangFor context.Context // when set, a save waits for its own context to end
}

func (g *nbGate) SaveNotebook(ctx context.Context, id string, in slimeclient.NotebookSave) (slimeclient.SavedNotebook, error) {
	g.mu.Lock()
	g.running++
	g.peak = max(g.peak, g.running)
	g.calls++
	first := g.calls == 1
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.running--; g.mu.Unlock() }()
	if g.barrier > 0 {
		for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
			g.mu.Lock()
			met := g.running >= g.barrier
			g.mu.Unlock()
			if met {
				break
			}
		}
	}
	if g.hold != nil && first {
		<-g.hold
	}
	if g.hangFor != nil {
		<-ctx.Done()
		return slimeclient.SavedNotebook{}, ctx.Err()
	}
	g.base.Lock()
	defer g.base.Unlock()
	res, err := g.fakeBackend.SaveNotebook(ctx, id, in)
	g.mu.Lock()
	g.bodies = append(g.bodies, in.Cells[0].Body)
	g.revs = append(g.revs, in.Rev)
	g.mu.Unlock()
	return res, err
}

func twoDirtyNotebooks(t *testing.T) (*rig, *nbGate) {
	t.Helper()
	r := nbRig(t, "# a", "# b")
	r.openNB(t, "nb1")
	r.openNB(t, "nb2")
	g := &nbGate{fakeBackend: r.fb}
	r.w.be = g
	for _, p := range r.w.notebookPanes() {
		p.nb.md += " typed"
		p.nb.auto.edit(time.Now())
	}
	return r, g
}

func TestQuitSavesDirtyNotebooksAtTheSameTime(t *testing.T) {
	r, g := twoDirtyNotebooks(t)
	g.barrier = 2
	r.w.flushNotebooksNow()
	if g.peak != 2 || len(g.bodies) != 2 {
		t.Errorf("most saves at once %d, saved %d: the quit saved one notebook after another", g.peak, len(g.bodies))
	}
}

func TestQuitSavesTheNewerTextAfterTheRunningSaveEnds(t *testing.T) {
	r := nbRig(t, "# a")
	p := r.openNB(t, "nb1")
	g := &nbGate{fakeBackend: r.fb, hold: make(chan struct{})}
	r.w.be = g
	var mu sync.Mutex
	var later []func()
	r.w.work = func(fn func()) { go fn() }
	r.w.post = func(fn func()) { mu.Lock(); later = append(later, fn); mu.Unlock() }
	n := p.nb
	n.md = "one"
	n.auto.edit(time.Now())
	r.w.nbPump(p)
	n.md = "two"
	n.auto.edit(time.Now())
	time.AfterFunc(100*time.Millisecond, func() { close(g.hold) })
	r.w.flushNotebooksNow()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !slices.Equal(g.bodies, []string{"one", "two"}) {
		t.Fatalf("saved %q: the typing made during a running save was not saved at quit", g.bodies)
	}
	if g.revs[1] != g.revs[0]+1 {
		t.Errorf("revisions %v: the second save did not carry the revision the first made", g.revs)
	}
}

func TestQuitGivesUpOnAHungServerAfterOneDeadlineForAllNotebooks(t *testing.T) {
	r, g := twoDirtyNotebooks(t)
	g.hangFor = context.Background()
	was := quitSaveWait
	quitSaveWait = 150 * time.Millisecond
	t.Cleanup(func() { quitSaveWait = was })
	start := time.Now()
	r.w.flushNotebooksNow()
	if d := time.Since(start); d > time.Second {
		t.Errorf("the quit took %v for two hung saves", d)
	}
	for _, p := range r.w.notebookPanes() {
		if !p.nb.failed {
			t.Error("a save that did not finish is not marked as failed")
		}
	}
}
