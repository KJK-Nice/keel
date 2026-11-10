// Command keel-native is the native (MyGo) client for a running keel serve.
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/egoist/mygo/plugins/terminal"

	"keel/internal/native"
)

func init() {
	// AppKit must run on thread 0, as in cmd/keel.
	runtime.LockOSThread()
}

func main() {
	// Hidden: `make native-app` asks where MyGo's loader keeps libghostty-vt. Nothing else is parsed.
	if len(os.Args) == 2 && os.Args[1] == "--library-path" {
		p, err := terminal.LibraryPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, "keel-native:", err)
			os.Exit(1)
		}
		fmt.Println(p)
		return
	}
	if err := native.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "keel-native:", err)
		os.Exit(1)
	}
}
