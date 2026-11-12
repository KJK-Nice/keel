//go:build nativee2e

package native

import (
	"strings"
	"testing"
	"time"
)

// Every Settings section the app has and this client builds, in both themes: the shots are judged against
// the rules and tokens (docs/native.md, "Compared with the app"); the old webview reference images are gone.
func TestE2ESettingsSections(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openLocal()
	e.openSettings()
	for _, theme := range []string{"Dark", "Light"} {
		e.click("settings nav Appearance")
		e.click(theme)
		e.pump(func() bool { return e.w.dark == (theme == "Dark") }, theme+" theme")
		for _, name := range []string{"Appearance", "Terminal", "Keyboard", "Sharing", "Vault & security", "Runtime", "About Keel"} {
			e.click("settings nav " + name)
			e.settle(250 * time.Millisecond)
			slug := strings.ToLower(strings.Fields(name)[0])
			e.shot("TestE2ESettingsSections-" + slug + "-" + strings.ToLower(theme))
		}
	}
	e.w.closeWindow()
}
