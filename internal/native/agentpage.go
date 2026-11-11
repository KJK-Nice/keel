package native

import (
	"github.com/egoist/mygo/ui"
)

// The Agent tab page (docs/ui-redesign.md §5.4.2). The page has no conversation of its own: it shows the window's one
// conversation (agent.go) in a second thread, so the panel and the page never disagree.

// agentPage is the state of the Agent tab: only what the page itself holds. No conversation text is kept here.
type agentPage struct {
	_ ui.Handle
}

func (w *workspace) agentPageView(c *ui.Context, p *agentPage) {}
