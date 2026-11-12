//go:build !darwin

package native

import "github.com/egoist/mygo"

// setWindowBlur: only macOS has the window server blur; elsewhere the window is translucent but not blurred.
func setWindowBlur(*mygo.Window, int) error { return nil }

// invalidateWindowShadow: the shadow problem it answers is the macOS window server's.
func invalidateWindowShadow(*mygo.Window) {}

// setWindowHasShadow: the Window shadow switch is macOS only.
func setWindowHasShadow(*mygo.Window, bool) {}
