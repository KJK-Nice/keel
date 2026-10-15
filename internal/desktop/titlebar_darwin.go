//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework WebKit
#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

typedef struct {
	bool fullscreen;
	bool barSeparate;
	double screenTop;
	double menuBarHeight;
	double windowTop;
	double barTop;
	double barBottom;
	double titlebarBottom;
} keelChromeProbe;

// Public AppKit only. In full screen the traffic lights leave the window
// for the full-screen toolbar window, which slides down with the menu bar
// when the pointer reaches the top. The view holding the lights (the
// titlebar) says where that overlay ends. Main thread only.
static keelChromeProbe keelProbeChrome(void *ptr) {
	keelChromeProbe p = {0};
	NSWindow *win = (__bridge NSWindow *)ptr;
	if (win == nil) return p;
	p.fullscreen = ([win styleMask] & NSWindowStyleMaskFullScreen) != 0;
	p.windowTop = NSMaxY([win frame]);
	NSScreen *screen = [win screen];
	p.screenTop = screen != nil ? NSMaxY([screen frame]) : p.windowTop;
	p.menuBarHeight = [[NSApp mainMenu] menuBarHeight];
	NSButton *close = [win standardWindowButton:NSWindowCloseButton];
	NSWindow *bar = [close window];
	NSView *titlebar = [close superview];
	if (bar == nil || bar == win || titlebar == nil) return p;
	p.barSeparate = true;
	p.barTop = NSMaxY([bar frame]);
	p.barBottom = NSMinY([bar frame]);
	NSRect r = [bar convertRectToScreen:[titlebar convertRect:[titlebar bounds] toView:nil]];
	p.titlebarBottom = NSMinY(r);
	return p;
}

static WKWebView *keelFindWebView(NSView *v) {
	if ([v isKindOfClass:[WKWebView class]]) return (WKWebView *)v;
	for (NSView *sub in [v subviews]) {
		WKWebView *w = keelFindWebView(sub);
		if (w != nil) return w;
	}
	return nil;
}

// Wails' ExecJS waits for its JS runtime, which the loopback page never
// loads, so the chrome state goes straight to the WKWebView.
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

// probeChrome reads the titlebar geometry on the main thread.
func probeChrome(win *application.WebviewWindow) (chromeProbe, bool) {
	ptr := win.NativeWindow()
	if ptr == nil {
		return chromeProbe{}, false
	}
	p := application.InvokeSyncWithResult(func() C.keelChromeProbe {
		return C.keelProbeChrome(ptr)
	})
	return chromeProbe{
		Fullscreen:     bool(p.fullscreen),
		BarSeparate:    bool(p.barSeparate),
		ScreenTop:      float64(p.screenTop),
		MenuBarHeight:  float64(p.menuBarHeight),
		WindowTop:      float64(p.windowTop),
		BarTop:         float64(p.barTop),
		BarBottom:      float64(p.barBottom),
		TitlebarBottom: float64(p.titlebarBottom),
	}, true
}

// evalChromeJS runs js in the window's web view on the main thread.
func evalChromeJS(win *application.WebviewWindow, js string) {
	ptr := win.NativeWindow()
	if ptr == nil {
		return
	}
	application.InvokeAsync(func() {
		C.keelEvalJS(ptr, C.CString(js))
	})
}
