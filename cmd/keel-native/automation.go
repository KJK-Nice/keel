//go:build keelautomation

package main

import (
	"fmt"
	"os"

	_ "github.com/egoist/mygo/plugins/automation"

	"keel/internal/native"
)

// Test builds only. See cmd/keel/automation.go.
func init() {
	if err := native.CheckAutomationEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "keel-native: automation refused:", err)
		native.RemoveAutomationSocket()
		os.Exit(2)
	}
}
