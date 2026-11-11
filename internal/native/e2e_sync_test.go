//go:build nativee2e

package native

import (
	"strings"
	"testing"
	"time"
)

// Sync input on two real local shells: what is typed in one is typed in both; with it off, only the focused one gets the next
// command.
func TestE2ESyncInput(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.openPanes(2)
	a, b := e.paneSID("1"), e.paneSID("2")
	e.waitScrollback(a, "$ ")
	e.waitScrollback(b, "$ ")

	e.click("Sync input") // the first pane bar's tool
	if !e.w.tab().sync || !e.tt.HasText("Sync on") || !e.hasLabel("Stop syncing input") {
		t.Fatalf("sync did not turn on: %v", e.tt.Texts())
	}
	e.clickBody("1")
	e.typeLine("echo sync-$((6*7))")
	e.waitScrollback(a, "sync-42")
	e.waitScrollback(b, "sync-42")
	e.settle(300 * time.Millisecond)
	e.shot("TestE2ESyncInput")

	e.click("Stop syncing input")
	if e.w.tab().sync || !e.tt.HasText("Sync off") {
		t.Fatalf("sync did not turn off: %v", e.tt.Texts())
	}
	e.clickBody("1")
	e.typeLine("echo solo-$((1+1))")
	e.waitScrollback(a, "solo-2")
	e.settle(500 * time.Millisecond)
	if strings.Contains(e.scrollback(b), "solo-") {
		t.Error("with sync off the line typed in pane 1 reached pane 2")
	}
}
