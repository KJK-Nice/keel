package native

import (
	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The group run page (docs/ui-redesign.md §17, board GroupRunbook): one runbook walked across a folder's hosts. This file
// holds its state; step 3 of the build fills it in.

// groupRunPage is the state of the Group run tab.
type groupRunPage struct {
	id    string
	title string // "Run · <runbook>"
	run   *keelclient.GroupRun
}

func (w *workspace) loadGroupRunPage() {}

// startGroupRun starts a run of the runbook on this folder, rolling (the app's default), and opens its page. keel serve fixes
// the hosts per step; nothing is typed and no pane is read.
func (w *workspace) startGroupRun(p *groupPage, runbookID string) {
	if p.run.busy {
		return
	}
	p.run.open, p.run.busy = false, true
	id := p.id
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		run, err := w.be.StartGroupRun(c, runbookID, id, "rolling")
		w.post(func() {
			p.run.busy = false
			if w.closed {
				return
			}
			if err != nil {
				w.warn(hostError(err))
				return
			}
			w.openGroupRun(run)
		})
	})
}

// openGroupRun opens the run's page, or shows it in the tab that is open: one instance.
func (w *workspace) openGroupRun(run keelclient.GroupRun) {
	if run.ID == "" {
		return
	}
	t := w.pageTab("grouprun")
	if t == nil {
		t = w.addPageTab("grouprun")
	}
	p := t.pg.grouprun
	p.id, p.title, p.run = run.ID, "Run · "+run.Name, &run
	w.openPage("grouprun")
}

func (w *workspace) groupRunView(c *ui.Context, p *groupRunPage) {
	ui.Column(c).Grow(1).FillWidth().Padding(space8).AlignItems(ui.Center).Children(func() {
		help(c, "Start a runbook from a group's page: Run a runbook on this group.")
	})
}
