package native

// The tests' one view into the window's internals. A test asks the window through these methods (tabs, the open
// dialogs, the agent panel, the open page) and never reads the workspace's fields itself; when a field moves, only
// this file changes. They sit on workspace, not on a rig, so the e2e windows (win) and the join rigs share them.

// tabCount is how many tabs the window has.
func (w *workspace) tabCount() int { return len(w.tabs) }

// tabAt is the i-th tab, left to right.
func (w *workspace) tabAt(i int) *tab { return w.tabs[i] }

// activeTab is the tab on show, nil when there is none.
func (w *workspace) activeTab() *tab { return w.tab() }

// approveOpen is whether the Approve before type dialog is showing; approveDlg is that dialog, nil when it is not.
func (w *workspace) approveOpen() bool          { return w.approve != nil }
func (w *workspace) approveDlg() *approveDialog { return w.approve }

// confirmOpen is whether a confirm dialog (the host menu's Delete, a delete in the agent panel...) is showing;
// confirmDlg is it, nil when it is not; dismissConfirm closes it without running its action.
func (w *workspace) confirmOpen() bool        { return w.confirm != nil }
func (w *workspace) confirmDlg() *hostConfirm { return w.confirm }
func (w *workspace) dismissConfirm()          { w.confirm = nil }

// allTabs is every tab, left to right; appendTab adds one a test built by hand, without selecting it.
func (w *workspace) allTabs() []*tab  { return w.tabs }
func (w *workspace) appendTab(t *tab) { w.tabs = append(w.tabs, t) }

// showPage opens the named page (keychain, logs, settings, snippets...) as a tab, or focuses its tab.
func (w *workspace) showPage(name string) { w.openPage(name) }

// agentState is the agent panel's state: its scope, conversation, turns, proposals and pickers.
func (w *workspace) agentState() *agentPanel { return w.agent }
