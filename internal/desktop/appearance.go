package desktop

import (
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The window's translucent backdrop (NSVisualEffectView on macOS, Acrylic
// on Windows) is tinted by the window's appearance, which follows the OS.
// keel.css lays the chrome over it at a partial tint, so a Keel theme that
// differs from the OS let the opposite vibrancy show through (#59).
//
// On macOS the page tells the window which theme the user chose, and the
// window takes the matching NSAppearance, so the backdrop follows Keel.
// Wails v3 (beta.15) sets an appearance only at window creation
// (MacWindow.Appearance) and has no runtime setter on any platform, so
// appearance_darwin.go sets it through public AppKit. Elsewhere the page
// drops translucency instead when the choice differs from the OS
// (data-vibrancy in page.templ).
//
// The channel is the WebKit message handler Wails already registers for
// window drag (windowDragJS): the page posts "keel:appearance:<choice>"
// and Wails hands any message without its "wails:" prefix to
// Options.RawMessageHandler. Nothing is added to the loopback server, and
// the handler accepts only the three fixed choices, from the main frame
// of the Keel page in Keel's own window.

// appearancePrefix starts the page's message; the choice follows it.
const appearancePrefix = "keel:appearance:"

// nativeAppearanceParam is the query flag that tells the page this window
// follows its theme natively, so it keeps translucency for every choice.
const nativeAppearanceParam = "appearance"

// followsThemeNatively reports whether the window can take Keel's theme as
// its own appearance on goos.
func followsThemeNatively(goos string) bool {
	return goos == "darwin"
}

// parseAppearance reads a page message. ok is false for anything that is
// not exactly an appearance message with a known choice.
func parseAppearance(msg string) (choice string, ok bool) {
	choice, found := strings.CutPrefix(msg, appearancePrefix)
	if !found {
		return "", false
	}
	switch choice {
	case "light", "dark", "system":
		return choice, true
	}
	return "", false
}

// macAppearance maps a theme choice to the NSAppearance the window takes.
// System clears the window's appearance so it follows the OS again.
func macAppearance(choice string) application.MacAppearanceType {
	switch choice {
	case "light":
		return application.NSAppearanceNameAqua
	case "dark":
		return application.NSAppearanceNameDarkAqua
	}
	return application.DefaultAppearance
}

// sameOrigin reports whether origin (a frame URL as Wails reports it) is
// the Keel page's scheme and host.
func sameOrigin(pageURL, origin string) bool {
	p, err := url.Parse(pageURL)
	if err != nil || p.Host == "" {
		return false
	}
	o, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return o.Scheme == p.Scheme && o.Host == p.Host
}

// rawMessage is the app's Options.RawMessageHandler.
func (w *nativeWindow) rawMessage(win application.Window, msg string, origin *application.OriginInfo) {
	if w.win == nil || win == nil || win.ID() != w.win.ID() {
		return
	}
	if origin == nil || !origin.IsMainFrame || !sameOrigin(w.url, origin.Origin) {
		return
	}
	if target, ok := parseOpenURL(msg); ok {
		if w.app != nil && w.app.Browser != nil {
			_ = w.app.Browser.OpenURL(target)
		}
		return
	}
	choice, ok := parseAppearance(msg)
	if !ok {
		return
	}
	setWindowAppearance(w.win, macAppearance(choice))
}
