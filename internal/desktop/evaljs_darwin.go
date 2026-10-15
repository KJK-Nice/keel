//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework WebKit
#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

static WKWebView *keelFindWebView(NSView *v) {
	if ([v isKindOfClass:[WKWebView class]]) return (WKWebView *)v;
	for (NSView *sub in [v subviews]) {
		WKWebView *w = keelFindWebView(sub);
		if (w != nil) return w;
	}
	return nil;
}

// keelEvalJS runs js in the window's WKWebView, skipping Wails' ExecJS
// queue. Main thread only; frees js.
static void keelEvalJS(void *ptr, char *js) {
	NSWindow *win = (__bridge NSWindow *)ptr;
	WKWebView *web = win == nil ? nil : keelFindWebView([win contentView]);
	if (web != nil) {
		[web evaluateJavaScript:[NSString stringWithUTF8String:js] completionHandler:nil];
	}
	free(js);
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// evalJS runs js in the window's web view on the main thread, straight
// through WKWebView. See jsExec in evaljs.go for why not ExecJS.
func evalJS(win *application.WebviewWindow, js string) {
	if win == nil || js == "" {
		return
	}
	ptr := win.NativeWindow()
	if ptr == nil {
		return
	}
	application.InvokeAsync(func() {
		C.keelEvalJS(ptr, C.CString(js))
	})
}
