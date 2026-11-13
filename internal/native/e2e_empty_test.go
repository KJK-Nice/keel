//go:build nativee2e

package native

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Plan 028 step 4 against a real daemon: the vault is emptied of its seeded snippets, then each list that can be empty is
// shot in both themes: the sidebar's segments, Keychain, Snippets, Notebooks, Runbooks, Logs, Port forwarding and the agent
// panel.
func TestE2EEmptyStates(t *testing.T) {
	addr, dir := startServe(t, true)
	e := newWin(t, addr, dir, nil)
	e.putAgentKey()
	for _, s := range e.snippetsOnServer() { // a new vault is seeded with four
		if code, body := e.raw("DELETE", "/api/snippets/"+s.ID, nil); code != 204 && code != 200 {
			t.Fatalf("DELETE snippet: %d %s", code, body)
		}
	}
	e.openPanes(2) // the Panes segment needs a split
	e.w.reloadSnippets()
	e.settle(300 * time.Millisecond)
	for _, th := range []string{themeDark, themeLight} {
		e.w.setTheme(th)
		e.pump(func() bool { return e.w.dark == (th == themeDark) }, th+" theme")
		shot := func(name string) {
			e.settle(300 * time.Millisecond)
			e.shot(fmt.Sprintf("TestE2EEmpty-%s-%s", name, th))
		}
		// The sidebar: Hosts filtered to nothing, Panes filtered to nothing, Snippets and Forwards with none.
		e.key(ui.Cmd, ui.Key1)
		e.fillField("Filter hosts", "zzz")
		shot("side-hosts")
		e.fillField("Filter hosts", "")
		e.click("Panes 2")
		e.fillField("Filter panes", "zzz")
		shot("side-panes")
		e.fillField("Filter panes", "")
		e.click("Snippets")
		shot("side-snippets")
		e.click("Forwards")
		shot("side-forwards")
		e.click("Hosts")
		// The pages.
		for _, p := range []struct{ page, wait string }{{"keychain", "No identities yet"}, {"snippets", "No snippets yet"}, {"notebooks", "No notebooks yet"},
			{"runbooks", "No runbooks yet"}, {"forwards", "No forwards yet"}} {
			e.w.openPage(p.page)
			e.pump(func() bool { return e.tt.HasText(p.wait) }, p.wait)
			shot("page-" + p.page)
		}
		e.w.openPage("logs")
		e.pump(func() bool { return e.hasLabel("Search events") }, "the Logs page")
		e.fillField("Search events", "zzzqqq")
		shot("page-logs")
		e.fillField("Search events", "")
		// The agent panel with a key and no conversation.
		e.key(ui.Cmd, ui.Key1)
		e.openAgent()
		shot("agent-panel")
		e.key(agentMods(), ui.KeyI)
		e.pump(func() bool { return !e.w.agent.open }, "the agent panel to close")
	}
}
