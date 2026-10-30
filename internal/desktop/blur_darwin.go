//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework CoreGraphics
#import <AppKit/AppKit.h>

// The window server's background blur, the call Warp, iTerm2 and Ghostty
// use for an adjustable blur behind a clear window: AppKit has no public
// radius (NSVisualEffectView's blur is fixed). Weak-linked, so a macOS
// without it leaves the window unblurred instead of failing to launch.
typedef int CGSConnectionID;
extern CGSConnectionID CGSMainConnectionID(void) __attribute__((weak_import));
extern CGError CGSSetWindowBackgroundBlurRadius(CGSConnectionID cid, NSInteger wid, int radius) __attribute__((weak_import));

// Main thread only.
static void keelSetBlur(void *ptr, int radius) {
	NSWindow *win = (__bridge NSWindow *)ptr;
	if (win == nil || [win windowNumber] <= 0) return;
	if (CGSMainConnectionID == NULL || CGSSetWindowBackgroundBlurRadius == NULL) return;
	CGSSetWindowBackgroundBlurRadius(CGSMainConnectionID(), [win windowNumber], radius);
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// setWindowBlur blurs what is behind the window by radius, on the main
// thread.
func setWindowBlur(win *application.WebviewWindow, radius int) {
	if win == nil {
		return
	}
	ptr := win.NativeWindow()
	if ptr == nil {
		return
	}
	application.InvokeAsync(func() {
		C.keelSetBlur(ptr, C.int(radius))
	})
}
