// Command keel-native is the native (MyGo) client for a running keel serve.
package main

import (
	"fmt"
	"os"
	"runtime"


	"keel/internal/native"
)

func init() {
	// AppKit must run on thread 0, as in cmd/keel.
	runtime.LockOSThread()
}

func main() {
	if err := native.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "keel-native:", err)
		os.Exit(1)
	}
}
