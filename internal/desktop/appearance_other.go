//go:build !darwin

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// setWindowAppearance has no runtime setter outside macOS in Wails v3: the
// page is never told to send one (followsThemeNatively), and drops
// translucency itself when the theme differs from the OS.
func setWindowAppearance(*application.WebviewWindow, application.MacAppearanceType) {}
