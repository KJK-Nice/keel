// Command slime-native is the native (MyGo) client for a running slime serve.
package main

import (
	"fmt"
	"os"
	"runtime"

	"slime/internal/native"
)

func init() {
	// AppKit must run on thread 0, as in cmd/slime.
	runtime.LockOSThread()
}

func main() {
	if err := native.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "slime-native:", err)
		os.Exit(1)
	}
}
