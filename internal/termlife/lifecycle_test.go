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

func TestNativeSplitDropsWebGLWithoutRemountingTerm(t *testing.T) {
	a := NewPane("a")
	tab := NewTab(true, a)
	PaintLayout(tab)
	wrap, term, host, addon := a.Wrap, a.Term, a.Host, a.Term.WebGL
	if addon == nil {
		t.Fatal("expected initial WebGL")
	}

	b := NewPane("b")
	tab.Panes = append(tab.Panes, b)
	tab.SetVisible("a", "b")
	PaintLayout(tab)

	if wrap != a.Wrap || term != a.Term || host != a.Host {
		t.Fatal("split remounted pane a wrap/term/host")
	}
	if a.Term.OpenCount != 1 || b.Term.OpenCount != 1 {
		t.Fatalf("open counts a=%d b=%d (term.open must not run again on remount)", a.Term.OpenCount, b.Term.OpenCount)
	}
	if a.Term.WebGL != nil || b.Term.WebGL != nil {
		t.Fatal("native split must drop WebGL so dead GPU canvases cannot eat clicks")
	}
	AttachWebGL(tab, a)
	if a.Term.WebGL != nil {
		t.Fatal("split must refuse a new attach")
	}
	if addon.Disposed != 1 {
		t.Fatalf("addon disposed = %d", addon.Disposed)
	}
	if a.Wrap.Parent != tab.Stage || b.Wrap.Parent != tab.Stage {
		t.Fatal("both wraps must stay on the same stage")
	}
	if !orderOK(tab.Log, "drop:a", "layout") {
		t.Fatalf("drop must happen before layout/reparent work: %v", tab.Log)
	}
}

func TestNativeUnsplitReattachesSameTerm(t *testing.T) {
	a := NewPane("a")
	b := NewPane("b")
	tab := NewTab(true, a, b)
	PaintLayout(tab)
	if a.Term.WebGL != nil {
		t.Fatal("split should start without WebGL")
	}

	tab.SetVisible("a")
	PaintLayout(tab)

	if a.Term.OpenCount != 1 {
		t.Fatalf("unsplit remounted term, open = %d", a.Term.OpenCount)
	}
	if a.Term.WebGL == nil {
		t.Fatal("solo pane must reattach WebGL")
	}
	if a.Wrap.Parent != tab.Stage {
		t.Fatal("wrap left the stage")
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

func TestContextLossDoesNotReattachWhileSplit(t *testing.T) {
	a := NewPane("a")
	b := NewPane("b")
	tab := NewTab(true, a, b)
	PaintLayout(tab)
	if a.Term.WebGL != nil {
		t.Fatal("native split must start without WebGL")
	}
	stuck := &Addon{ID: 99}
	a.Term.WebGL = stuck
	ContextLost(tab, a)
	if stuck.Disposed != 1 {
		t.Fatal("lost context must dispose the leftover addon")
	}
	if a.Term.WebGL != nil {
		t.Fatal("split must not reattach after context loss")
	}
	if a.Term.OpenCount != 1 {
		t.Fatalf("open = %d", a.Term.OpenCount)
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
