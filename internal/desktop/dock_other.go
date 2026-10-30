//go:build !darwin

package desktop

// setDockVisible has nothing to do outside macOS: only the macOS window
// has a menu bar item (menuBarItem).
func setDockVisible(bool) {}
