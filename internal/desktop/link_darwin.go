//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"

// Wails WKWebView drag-and-drop references UTType. The wails CLI injects
// UniformTypeIdentifiers into CGO_LDFLAGS; plain `go build` does not, so the
// Darwin link fails with: Undefined symbols: "_OBJC_CLASS_$_UTType".
