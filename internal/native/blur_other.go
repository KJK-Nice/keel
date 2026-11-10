//go:build !darwin

package native

import "github.com/egoist/mygo"

// setWindowBlur: only macOS has the window server blur; elsewhere the window is translucent but not blurred.
func setWindowBlur(*mygo.Window, int) error { return nil }
