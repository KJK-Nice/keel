package desktop

import (
	"os"
	stdruntime "runtime"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestDesktopURLMarksNativeWindow(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		got := desktopURL("http://127.0.0.1:47241/?name=ada&appearance=native", goos)
		if !strings.Contains(got, "native=1") || !strings.Contains(got, "name=ada") {
			t.Fatalf("%s url = %s", goos, got)
		}
		if strings.Contains(got, "chrome=titlebar") {
			t.Fatalf("non-mac url should not overlay tabs: %s", got)
		}
		// No runtime appearance setter here: the page drops translucency
		// itself when the theme differs from the OS.
		if strings.Contains(got, "appearance=") {
			t.Fatalf("%s window cannot take the theme as its appearance: %s", goos, got)
		}
	}
}

func TestDesktopURLOverlaysTabsOnMac(t *testing.T) {
	got := desktopURL("http://127.0.0.1:47241/?name=ada", "darwin")
	if !strings.Contains(got, "native=1") || !strings.Contains(got, "chrome=titlebar") || !strings.Contains(got, "appearance=native") {
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

// Menu items reach the page through the window's jsExec (evalJS), never
// through Wails' ExecJS, whose queue never flushes on the loopback page.
func TestMenuActionSendsShortcutThroughExec(t *testing.T) {
	var got []string
	w := &nativeWindow{exec: func(js string) { got = append(got, js) }}
	w.menuAction("toggle-agent")(nil)
	w.menuAction("new-tab")(nil)
	want := []string{shortcutJS("toggle-agent"), shortcutJS("new-tab")}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("exec got %q, want %q", got, want)
	}
	got = nil
	w.menuAction(actionQuit)(nil) // no app yet: nothing to quit, nothing sent
	if len(got) != 0 {
		t.Fatalf("Close Window sent script to the page: %q", got)
	}
}

func TestMenuActionWithoutWindowIsANoop(t *testing.T) {
	w := &nativeWindow{}
	w.menuAction("new-tab")(nil)
	windowJS(nil)(shortcutJS("new-tab"))
}

func TestEveryMenuEntryIsInTheMenu(t *testing.T) {
	m := (&nativeWindow{}).appMenu()
	for _, e := range menuEntries(stdruntime.GOOS) {
		item := m.FindByLabel(e.label)
		if item == nil {
			t.Errorf("menu missing %q", e.label)
			continue
		}
		if (item.GetAccelerator() != "") != (e.accel != "") {
			t.Errorf("%q accelerator = %q, want %q", e.label, item.GetAccelerator(), e.accel)
		}
	}
}

// windowDragJS goes in as the window's JS option: Wails runs that itself
// after every page load (macOS and Linux), outside the ExecJS queue.
func TestWindowDragJSIsTheWindowJSOption(t *testing.T) {
	if got := webviewOptions("http://127.0.0.1:47241/").JS; got != windowDragJS() {
		t.Fatal("webviewOptions JS is not windowDragJS")
	}
}

func TestAppMenuHasTabShortcuts(t *testing.T) {
	w := &nativeWindow{}
	m := w.appMenu()
	for _, want := range []string{"New Tab", "Close Tab", "Find Host", "Find in Terminal", "Toggle Vault Sidebar", "Split Right", "Split Notebook", "Close Window", "Stop Keel runtime", "Focus View", "Zoom In", "Zoom Out", "Actual Size"} {
		if m.FindByLabel(want) == nil {
			t.Errorf("menu missing %q", want)
		}
	}
	if m.FindByLabel("New Tab").GetAccelerator() == "" {
		t.Error("New Tab missing accelerator")
	}
	if acc := m.FindByLabel("Find Host").GetAccelerator(); acc != "" {
		t.Errorf("Find Host must not steal Cmd+K from the palette, got %q", acc)
	}
}

// The mac window is clear so Window blur can set its own radius; the page
// paints the tint (Window opacity).
func TestMacWindowUsesClearBackdrop(t *testing.T) {
	mac := macWindowChrome()
	if mac.Backdrop != application.MacBackdropTransparent {
		t.Fatalf("mac backdrop = %v, want transparent", mac.Backdrop)
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

// Since Wails v3.0.0 the call that clears the WKWebView background (so the
// backdrop shows through) is compiled only with private_mac_apis; without it
// the window is opaque whatever the backdrop. Every build reads its tags
// from scripts/go-tags.sh.
func TestBuildTagsKeepMacWebviewTransparent(t *testing.T) {
	b, err := os.ReadFile("../../scripts/go-tags.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "echo ") {
			if !strings.Contains(line, "private_mac_apis") {
				t.Fatalf("build tags %q lack private_mac_apis", line)
			}
			return
		}
	}
	t.Fatal("no echo line in scripts/go-tags.sh")
}

func TestMacTitleBarPresetMatchesHiddenInset(t *testing.T) {
	want := application.MacTitleBarHiddenInset
	got := overlayTitleBar()
	if got != want {
		t.Fatalf("overlayTitleBar = %+v, want HiddenInset %+v", got, want)
	}
}
