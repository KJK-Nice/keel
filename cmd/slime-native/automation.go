//go:build slimeautomation

package main

import (
	"fmt"
	"os"

	_ "github.com/egoist/mygo/plugins/automation"

	"slime/internal/native"
)

// Test builds only. See cmd/slime/automation.go.
func init() {
	if err := native.CheckAutomationEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "slime-native: automation refused:", err)
		native.RemoveAutomationSocket()
		os.Exit(2)
	}
}
