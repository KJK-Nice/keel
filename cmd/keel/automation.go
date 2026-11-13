//go:build keelautomation

package main

import (
	"fmt"
	"os"

	_ "github.com/egoist/mygo/plugins/automation"

	"keel/internal/native"
)

// Test builds only (make native-automation-build). The plugin listens in its own init and answers nothing until
// the app has launched; this init runs before main, so a refused environment exits before any window or request.
func init() {
	if err := native.CheckAutomationEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "keel: automation refused:", err)
		native.RemoveAutomationSocket()
		os.Exit(2)
	}
}
