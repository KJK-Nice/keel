//go:build !darwin

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// setWindowBlur has no radius outside macOS: Windows keeps its Acrylic
// backdrop, and the page shows Window blur only on macOS.
func setWindowBlur(*application.WebviewWindow, int) {}
