package native

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The fake records attaches in the order they arrive and many tests read that order, so they attach one at a time.
func init() { attachWorkers = 1 }

// attachGate is a backend whose Attach waits for another to be under way, and keeps the fake's state behind a lock.
type attachGate struct {
	*fakeBackend
	mu      sync.Mutex
	base    sync.Mutex
	running int
	peak    int
}

func (g *attachGate) Attach(id string, cols, rows int) (session, error) {
	g.mu.Lock()
	g.running++
	g.peak = max(g.peak, g.running)
	g.mu.Unlock()
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
		g.mu.Lock()
		met := g.running >= 2
		g.mu.Unlock()
		if met {
			break
		}
	}
	g.mu.Lock()
	g.running--
	g.mu.Unlock()
	g.base.Lock()
	defer g.base.Unlock()
	return g.fakeBackend.Attach(id, cols, rows)
}

func TestStartAttachesSavedSessionsAtTheSameTimeAndKeepsTheirOrder(t *testing.T) {
	fb := okBackend()
	fb.sessions = []keelclient.Session{{ID: "s-a", HostID: "h-api"}, {ID: "s-b", HostID: "h-api"}, {ID: "s-c", HostID: "h-api"}, {ID: "s-d", HostID: "h-api"}}
	attachWorkers = 4
	t.Cleanup(func() { attachWorkers = 1 })
	g := &attachGate{fakeBackend: fb}
	w := newWorkspace(g)
	w.work = func(fn func()) { fn() }
	w.post = func(fn func()) { fn() }
	var released int
	w.mkBody = placeholder(&released)
	w.start()
	tt := ui.NewTester(w.view, 1280, 720)
	tt.Frame()
	if g.peak < 2 {
		t.Errorf("most attaches at once %d: the sessions were attached one after another", g.peak)
	}
	var got []string
	for i := 0; i < w.tabCount(); i++ {
		for _, p := range w.tabAt(i).panes {
			got = append(got, p.sessionID())
		}
	}
	if !slices.Equal(got, []string{"s-a", "s-b", "s-c", "s-d"}) {
		t.Errorf("tabs hold %v", got)
	}
}
