package desktop

import (
	"os"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
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
	if tb == nil {
		t.Fatal("nil titlebar")
	}
	if !tb.TitlebarAppearsTransparent || !tb.HideTitle || !tb.FullSizeContent {
		t.Fatalf("want hidden title + full-size content, got %+v", tb)
	}
	if tb.UseToolbar || tb.HideTitleBar {
		t.Fatalf("toolbar/hidden bar would keep tabs out of the title: %+v", tb)
	}
}

func TestNavigateJS(t *testing.T) {
	js := navigateJS("http://127.0.0.1:47241/?name=ada")
	if !strings.Contains(js, `window.location.replace("http://127.0.0.1:47241/?name=ada")`) {
		t.Fatalf("js = %s", js)
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
	var labels []string
	var walk func(items []*menu.MenuItem)
	walk = func(items []*menu.MenuItem) {
		for _, it := range items {
			if it.Label != "" {
				labels = append(labels, it.Label)
			}
			if it.SubMenu != nil {
				walk(it.SubMenu.Items)
			}
		}
	}
	walk(m.Items)
	joined := strings.Join(labels, "\n")
	for _, want := range []string{"New Tab", "Close Tab", "Find Host", "Split Right", "Close Window", "Zoom In", "Zoom Out", "Actual Size"} {
		if !strings.Contains(joined, want) {
			t.Errorf("menu missing %q", want)
		}
	}
}

func TestSplashHasMark(t *testing.T) {
	b, err := os.ReadFile("shell/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`M4 25 L16 5 L28 25`, `Starting Keel`, `--wails-draggable`, `border-radius`} {
		if want == `border-radius` {
			if strings.Contains(s, want) {
				t.Error("splash icon should be a sharp square, no border-radius")
			}
			continue
		}
		if !strings.Contains(s, want) {
			t.Errorf("shell/index.html missing %q", want)
		}
	}
}
