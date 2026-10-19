package termlife

import (
	"slices"
	"testing"
)

func TestSoloNativeAttachesWebGLOnce(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(true, a)
	PaintLayout(tab)

	if a.Term.OpenCount != 1 {
		t.Fatalf("open = %d", a.Term.OpenCount)
	}
	if a.Term.WebGL == nil || a.Term.Loads != 1 {
		t.Fatal("solo native pane must attach WebGL")
	}
	if a.Wrap.Parent != tab.Stage {
		t.Fatal("wrap must sit on the stage")
	}
	if a.Term.Host != a.Host {
		t.Fatal("term opened on a different host")
	}
}

func TestNativeSplitKeepsWebGLWithoutRemountingTerm(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(true, a)
	PaintLayout(tab)
	wrap, term, host, addon := a.Wrap, a.Term, a.Host, a.Term.WebGL
	if addon == nil {
		t.Fatal("expected initial WebGL")
	}
	beforeSplit := len(tab.Log)

	b := NewPane("b")
	tab.Panes = append(tab.Panes, b)
	tab.SetVisible("a", "b")
	PaintLayout(tab)
	splitLog := tab.Log[beforeSplit:]

	if wrap != a.Wrap || term != a.Term || host != a.Host {
		t.Fatal("split remounted pane a wrap/term/host")
	}
	if a.Term.OpenCount != 1 || b.Term.OpenCount != 1 {
		t.Fatalf("open counts a=%d b=%d (term.open must not run again on remount)", a.Term.OpenCount, b.Term.OpenCount)
	}
	if a.Term.WebGL != addon || addon.Disposed != 0 {
		t.Fatal("native split must keep pane a's WebGL addon")
	}
	if b.Term.WebGL == nil || b.Term.Loads != 1 {
		t.Fatal("the new split pane must attach WebGL once")
	}
	if tab.Contexts() != 2 {
		t.Fatalf("contexts = %d, want one per visible pane", tab.Contexts())
	}
	if a.Wrap.Parent != tab.Stage || b.Wrap.Parent != tab.Stage {
		t.Fatal("both wraps must stay on the same stage")
	}
	if slices.Contains(splitLog, "open:a") || slices.Contains(splitLog, "reparent:a") || slices.Contains(splitLog, "drop:a") {
		t.Fatalf("pane a remounted or dropped during split: %v", splitLog)
	}
}

func TestZoomReleasesHiddenPanesAndUnzoomReattaches(t *testing.T) {
	a, b, c := NewPane("a"), NewPane("b"), NewPane("c")
	tab := NewTab(true, a, b, c)
	PaintLayout(tab)
	if tab.Contexts() != 3 {
		t.Fatalf("contexts = %d", tab.Contexts())
	}
	for cycle := 0; cycle < 4; cycle++ {
		tab.SetVisible("a")
		PaintLayout(tab)
		if tab.Contexts() != 1 || a.Term.WebGL == nil {
			t.Fatalf("zoomed: contexts = %d, want only the zoomed pane", tab.Contexts())
		}
		tab.SetVisible("a", "b", "c")
		PaintLayout(tab)
		if tab.Contexts() != 3 {
			t.Fatalf("unzoomed: contexts = %d", tab.Contexts())
		}
	}
	for _, p := range tab.Panes {
		if p.Term.OpenCount != 1 {
			t.Fatalf("pane %s opened %d times", p.ID, p.Term.OpenCount)
		}
	}
	// Every addon but the live ones was disposed: none leaked.
	if a.Term.Loads != 1 || b.Term.Loads != 5 || c.Term.Loads != 5 {
		t.Fatalf("loads a=%d b=%d c=%d", a.Term.Loads, b.Term.Loads, c.Term.Loads)
	}
}

func TestContextLossReattachesWhileSplit(t *testing.T) {
	a, b := NewPane("a"), NewPane("b")
	tab := NewTab(true, a, b)
	PaintLayout(tab)
	old := a.Term.WebGL
	ContextLost(tab, a)
	if old.Disposed != 1 || a.Term.WebGL == nil || a.Term.WebGL == old {
		t.Fatal("split pane must attach a new addon after a context loss")
	}
	if a.Term.OpenCount != 1 {
		t.Fatalf("open = %d", a.Term.OpenCount)
	}
}

func TestRepeatedContextLossFallsBackToDOM(t *testing.T) {
	a, b := NewPane("a"), NewPane("b")
	tab := NewTab(true, a, b)
	PaintLayout(tab)
	for i := 0; i < MaxLosses; i++ {
		ContextLost(tab, a)
	}
	if a.Term.WebGL != nil || a.Fallback == "" {
		t.Fatal("a pane that keeps losing its context must stay on the DOM renderer")
	}
	PaintLayout(tab)
	if a.Term.WebGL != nil {
		t.Fatal("a fallen-back pane must not attach again on the next layout")
	}
	if b.Term.WebGL == nil {
		t.Fatal("the other pane keeps WebGL")
	}
	if !slices.Contains(tab.Log, "fallback:a") {
		t.Fatalf("fallback must be noted: %v", tab.Log)
	}
}

func TestCreateFailureFallsBackToDOM(t *testing.T) {
	a := NewPane("a")
	a.FailCreate = true
	tab := NewTab(true, a)
	PaintLayout(tab)
	if a.Term.WebGL != nil || a.Fallback == "" || a.Term.Element != true {
		t.Fatal("a pane whose context cannot be created opens on the DOM renderer")
	}
}

func TestDisposePaneDropsAddonThenTerm(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(true, a)
	PaintLayout(tab)
	addon := a.Term.WebGL
	DisposePane(tab, a)

	if addon == nil || addon.Disposed != 1 {
		t.Fatal("dispose must drop WebGL")
	}
	if a.Term.WebGL != nil {
		t.Fatal("webgl pointer must be cleared")
	}
	if a.Term.Disposed != 1 {
		t.Fatalf("term disposed = %d", a.Term.Disposed)
	}
	if a.Wrap.Parent != nil {
		t.Fatal("wrap must be removed from the stage")
	}
	if !orderOK(tab.Log, "drop:a", "dispose-term:a", "remove-wrap:a") {
		t.Fatalf("expected drop → dispose term → remove wrap, got %v", tab.Log)
	}
}

func TestNonNativeSplitKeepsWebGL(t *testing.T) {
	a := NewPane("a")
	b := NewPane("b")
	tab := NewTab(false, a, b)
	PaintLayout(tab)
	if a.Term.WebGL == nil || b.Term.WebGL == nil {
		t.Fatal("browser split should keep WebGL")
	}
	if a.Term.OpenCount != 1 {
		t.Fatalf("open = %d", a.Term.OpenCount)
	}
}

func TestContextLossReattachesWhenSolo(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(true, a)
	PaintLayout(tab)
	old := a.Term.WebGL
	ContextLost(tab, a)
	if old.Disposed != 1 {
		t.Fatal("lost context must dispose the addon")
	}
	if a.Term.WebGL == nil || a.Term.WebGL == old {
		t.Fatal("solo pane must attach a new addon after context loss")
	}
	if a.Term.OpenCount != 1 {
		t.Fatalf("context loss remounted term, open = %d", a.Term.OpenCount)
	}
}

func TestEnsurePaneTermSkipsDisconnectedWrap(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(true, a)
	EnsurePaneTerm(tab, a)
	if a.Term.OpenCount != 0 || a.Term.WebGL != nil {
		t.Fatal("term must not open until the wrap is on the stage")
	}
	PaintLayout(tab)
	if a.Term.OpenCount != 1 || a.Term.WebGL == nil {
		t.Fatal("connected wrap should open and attach")
	}
}

func TestWriteInvalidatesNativeWithoutReflow(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(true, a)
	PaintLayout(tab)
	before := len(tab.Log)

	WriteBytes(tab, a)
	got := tab.Log[before:]
	if !slices.Contains(got, "write:a") || !slices.Contains(got, "invalidate:a") {
		t.Fatalf("PTY write must Relayout/Invalidate the native canvas, got %v", got)
	}
	if slices.Contains(got, "reflow:a") {
		t.Fatal("write path must not sendResize/reflow; that is the resize observer path")
	}

	RefreshPaneRender(tab, a)
	after := tab.Log[len(tab.Log)-2:]
	if !orderOK(after, "reflow:a", "invalidate:a") {
		t.Fatalf("resize path must invalidate after reflow, got %v", tab.Log)
	}
}

func TestWriteSkipsInvalidateWhenNotNative(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(false, a)
	PaintLayout(tab)
	before := len(tab.Log)
	WriteBytes(tab, a)
	got := tab.Log[before:]
	if slices.Contains(got, "invalidate:a") {
		t.Fatal("browser compositor does not need the native canvas invalidate")
	}
}

func orderOK(log []string, steps ...string) bool {
	last := -1
	for _, step := range steps {
		i := slices.Index(log, step)
		if i < 0 || i < last {
			return false
		}
		last = i
	}
	return true
}
