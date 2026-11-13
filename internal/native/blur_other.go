//go:build !darwin

package native

import "github.com/egoist/mygo"

// setWindowBlur: only macOS has the window server blur; elsewhere the window is translucent but not blurred.
func setWindowBlur(*mygo.Window, int) error { return nil }

// windowCornerRadius: the system frame of a window off macOS is the system's, so Keel draws no border there.
func windowCornerRadius() float32 { return 0 }

// setWindowHasShadow: the window's shadow is the macOS window server's.
func setWindowHasShadow(*mygo.Window, bool) {}
