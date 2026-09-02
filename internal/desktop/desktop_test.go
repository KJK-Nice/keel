package desktop

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestDesktopURLMarksNativeWindow(t *testing.T) {
	got := desktopURL("http://127.0.0.1:47241/?name=ada", false)
	if !strings.Contains(got, "native=1") || !strings.Contains(got, "name=ada") {
		t.Fatalf("js url = %s", got)
	}
	if strings.Contains(got, "chrome=titlebar") {
		t.Fatalf("non-mac url should not overlay tabs: %s", got)
	}
}

func TestDesktopURLOverlaysTabsOnMac(t *testing.T) {
	got := desktopURL("http://127.0.0.1:47241/?name=ada", true)
	if !strings.Contains(got, "native=1") || !strings.Contains(got, "chrome=titlebar") {
		t.Fatalf("mac url = %s", got)
	}
}

func TestOverlayTitleBarLetsTabsSitBesideTrafficLights(t *testing.T) {
	tb := overlayTitleBar()
	if !tb.AppearsTransparent || !tb.HideTitle || !tb.FullSizeContent {
		t.Fatalf("want hidden title + full-size content, got %+v", tb)
	}
	if !tb.UseToolbar || !tb.HideToolbarSeparator || tb.Hide {
		t.Fatalf("want inset toolbar titlebar so traffic lights drop, got %+v", tb)
	}
}

func TestWindowDragJSRebindsAfterNavigation(t *testing.T) {
	js := windowDragJS()
	for _, want := range []string{
		`window.__keelWindowDrag`,
		`--wails-draggable`,
		`invoke("wails:drag")`,
		`invoke("wails:drag:doubleclick")`,
		`is-window-dragging`,
		`webkit.messageHandlers.external`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("windowDragJS missing %q", want)
		}
	}
}

func TestShortcutJS(t *testing.T) {
	js := shortcutJS("close-tab")
	if !strings.Contains(js, `window.__keelShortcut("close-tab")`) {
		t.Fatalf("js = %s", js)
	}
}

func TestAppMenuHasTabShortcuts(t *testing.T) {
	w := &nativeWindow{}
	m := w.appMenu()
	for _, want := range []string{"New Tab", "Close Tab", "Find Host", "Find in Terminal", "Split Right", "Close Window", "Stop Keel runtime", "Focus Pane", "Zoom In", "Zoom Out", "Actual Size"} {
		if m.FindByLabel(want) == nil {
			t.Errorf("menu missing %q", want)
		}
	}
	if m.FindByLabel("New Tab").GetAccelerator() == "" {
		t.Error("New Tab missing accelerator")
	}
}

func TestMacWindowUsesTranslucentBackdrop(t *testing.T) {
	mac := macWindowChrome()
	if mac.Backdrop != application.MacBackdropTranslucent {
		t.Fatalf("mac backdrop = %v, want translucent", mac.Backdrop)
	}
	opts := webviewOptions("http://127.0.0.1:47241/?native=1")
	if opts.BackgroundType != application.BackgroundTypeTranslucent {
		t.Fatalf("background type = %v, want translucent", opts.BackgroundType)
	}
	if opts.BackgroundColour.Alpha != 0 {
		t.Fatalf("background alpha = %d, want 0", opts.BackgroundColour.Alpha)
	}
	if opts.Windows.BackdropType != application.Acrylic {
		t.Fatalf("windows backdrop = %v, want acrylic", opts.Windows.BackdropType)
	}
}

func TestMacTitleBarPresetMatchesHiddenInset(t *testing.T) {
	want := application.MacTitleBarHiddenInset
	got := overlayTitleBar()
	if got != want {
		t.Fatalf("overlayTitleBar = %+v, want HiddenInset %+v", got, want)
	}
}
