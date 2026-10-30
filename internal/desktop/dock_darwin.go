//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

// Regular puts Keel in the Dock and ⌘Tab and brings it forward; Accessory
// leaves only the menu bar item (as Warp does with its window closed).
// Public AppKit only. Main thread only.
static void keelSetDockVisible(bool visible) {
	if (visible) {
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		[NSApp activateIgnoringOtherApps:YES];
	} else {
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	}
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// setDockVisible shows or hides Keel's Dock icon, on the main thread.
func setDockVisible(visible bool) {
	application.InvokeSync(func() {
		C.keelSetDockVisible(C.bool(visible))
	})
}
