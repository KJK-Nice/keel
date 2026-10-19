//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>

// keelSetAppearance sets the window's NSAppearance by name; an empty name
// clears it, so the window follows the OS again. The NSVisualEffectView
// behind the page takes the window's effective appearance. Public AppKit
// only. Main thread only; frees name.
static void keelSetAppearance(void *ptr, char *name) {
	NSWindow *win = (__bridge NSWindow *)ptr;
	if (win != nil) {
		NSAppearance *a = nil;
		if (name[0] != 0) a = [NSAppearance appearanceNamed:[NSString stringWithUTF8String:name]];
		[win setAppearance:a];
	}
	free(name);
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// setWindowAppearance gives the window an NSAppearance on the main thread.
func setWindowAppearance(win *application.WebviewWindow, name application.MacAppearanceType) {
	if win == nil {
		return
	}
	ptr := win.NativeWindow()
	if ptr == nil {
		return
	}
	application.InvokeAsync(func() {
		C.keelSetAppearance(ptr, C.CString(string(name)))
	})
}
