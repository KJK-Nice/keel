//go:build darwin

package native

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// blurTimeout bounds the wait for the main thread to run the call.
const blurTimeout = 2 * time.Second

// windowCornerRadius is the radius macOS cuts a titled window to, which the window border follows: 10 pt up to macOS 15, and
// 16 pt from macOS 26 (Liquid Glass rounds windows without a toolbar more). Not read from the window: AppKit has no call for it.
func windowCornerRadius() float32 {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return 10
	}
	major, _, _ := strings.Cut(v, ".")
	if n, err := strconv.Atoi(major); err == nil && n >= 26 {
		return 16
	}
	return 10
}

// setWindowHasShadow gives the window its shadow or takes it away, on the main thread. Keel's window has none: a
// Transparent window's shadow is cut from its content's alpha once and shows through the translucent grounds as a ghost
// of the first page, so run.go takes it away when the window shows.
func setWindowHasShadow(win *mygo.Window, on bool) {
	go mygo.RunOnMain(func() {
		defer func() { recover() }()
		if h := win.NativeHandle(); h != 0 {
			objc.Send[objc.ID](objc.ID(h), objc.RegisterName("setHasShadow:"), on)
			objc.ID(h).Send(objc.RegisterName("invalidateShadow"))
		}
	})
}

// setWindowBlur sets the window server's background blur radius (the call Warp, iTerm2 and
// Ghostty use). It is a private API: it never panics and returns an error, so the app runs unblurred.
// Call it off the main thread (a goroutine): it waits for the main thread to run the call.
func setWindowBlur(win *mygo.Window, radius int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("blur panicked: %v", r)
		}
	}()
	var lib uintptr
	for _, p := range []string{
		"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		"/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight",
	} {
		h, e := purego.Dlopen(p, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if e != nil {
			continue
		}
		if _, e1 := purego.Dlsym(h, "CGSMainConnectionID"); e1 != nil {
			continue
		}
		if _, e2 := purego.Dlsym(h, "CGSSetWindowBackgroundBlurRadius"); e2 != nil {
			continue
		}
		lib = h
		break
	}
	if lib == 0 {
		return errors.New("CGSMainConnectionID / CGSSetWindowBackgroundBlurRadius not found")
	}
	var mainConn func() int32
	var setBlur func(cid int32, wid int, radius int32) int32
	purego.RegisterLibFunc(&mainConn, lib, "CGSMainConnectionID")
	purego.RegisterLibFunc(&setBlur, lib, "CGSSetWindowBackgroundBlurRadius")

	// The result travels on a channel so it cannot be lost; a panic on the main thread is caught there.
	res := make(chan error, 1)
	go mygo.RunOnMain(func() {
		var e error
		defer func() {
			if r := recover(); r != nil {
				e = fmt.Errorf("blur panicked: %v", r)
			}
			res <- e
		}()
		h := win.NativeHandle()
		if h == 0 {
			e = errors.New("no native window handle")
			return
		}
		n := objc.Send[int](objc.ID(h), objc.RegisterName("windowNumber"))
		if n <= 0 {
			return // not on screen yet; the OnShow call retries
		}
		if rc := setBlur(mainConn(), n, int32(radius)); rc != 0 {
			e = fmt.Errorf("CGSSetWindowBackgroundBlurRadius returned %d", rc)
		}
	})
	select {
	case err = <-res:
		return err
	case <-time.After(blurTimeout):
		return errors.New("the main thread did not run the blur call within 2s")
	}
}
