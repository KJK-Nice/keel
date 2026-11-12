//go:build nativee2e

package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Plan 032 section 10: Settings · Appearance with the Window shadow row, both themes (the shots are opened by eye).
func TestE2ESettingsWindowShadowRow(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		t.Run(theme, func(t *testing.T) {
			dir := scratchDir(t)
			data := filepath.Join(dir, "data")
			if err := os.WriteFile(filepath.Join(data, "keel-native.json"), []byte(`{"opacity":100,"theme":"`+theme+`"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			addr, _ := startServeAt(t, dir, "serve", true)
			e := newWin(t, addr, data, nil)
			e.useSavedWindowPrefs()
			e.pump(func() bool { return e.hasLabel("host Local Shell") }, "the host tree")
			e.key(ui.Cmd, ui.KeyComma)
			e.pump(func() bool { return e.hasLabel("tab Settings") && e.hasLabel("Window opacity slider") }, "Settings · Appearance")
			if hasShadowSwitch() && !e.hasLabel("Window shadow switch") {
				t.Fatal("no Window shadow switch on the Appearance page")
			}
			e.shot("TestE2ESettingsWindowShadowRow-" + theme)
			e.w.closeWindow()
		})
	}
}
