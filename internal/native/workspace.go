package native

import (
	"context"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/joinpeer"
	"keel/internal/native/keelclient"
	"keel/internal/native/layout"
)

const (
	callTimeout = 15 * time.Second
	startCols   = 80 // the size a session starts at; the terminal view resizes it
	startRows   = 24
)

// session is what a pane needs of a keel serve connection (*keelclient.Conn, or a fake in tests).
type session interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	SessionID() string
	Snapshot() keelclient.State
	TakeControl() error
	RequestControl() error
	GrantControl(peerID uint64) error // the driver or the owner makes that attached client the driver
	Events() []keelclient.Event       // what the share hub and the runtime announced since the last call
	OnChange(fn func())
	// ApprovedInject types bytes the user approved (keelclient/inject.go): a snippet's, or with a non-nil agent
	// an agent proposal's, which keel serve checks again and answers. Only the Approve before type dialog's Type
	// action calls it.
	ApprovedInject(ctx context.Context, command string, agent *keelclient.AgentRef, opts ...keelclient.InjectOption) error
	// DirectType asks keel serve to type a plainly read-only one-line command without the dialog and without Enter
	// (keelclient/inject.go). Only typeNow (typenow.go) calls it.
	DirectType(ctx context.Context, command string, ref keelclient.DirectRef, agent *keelclient.AgentRef) (keelclient.DirectResult, error)
	// In-pane suggestions (keelclient/inject.go, suggestpane.go). SuggestDetect and SuggestAsk are questions and write
	// nothing; SuggestEvents hands over keel serve's answers. SuggestInsert (only insertSuggestion calls it) and
	// RestoreWords (only restoreWords) are the two that put bytes on the prompt line.
	SuggestDetect(seq int, line string) error
	SuggestAsk(seq int, line string) error
	SuggestEvents() []keelclient.SuggestEvent
	SuggestInsert(ctx context.Context, ref keelclient.SuggestionRef) error
	RestoreWords(words string) error
}

// backend is keel serve as the workspace uses it.
type backend interface {
	Vault(ctx context.Context) (keelclient.Vault, error)
	Hosts(ctx context.Context) ([]keelclient.Host, error)
	Sessions(ctx context.Context) ([]keelclient.Session, error)
	Spawn(hostID string, cols, rows int) (session, error)
	Attach(sessionID string, cols, rows int) (session, error)
	Kill(ctx context.Context, id string) error
	Reconnect(ctx context.Context, id string) error
	TrustHostKey(ctx context.Context, k keelclient.HostKey, replace bool) error
	CreateHost(ctx context.Context, h keelclient.NewHost) (keelclient.Host, error)

	Unlock(ctx context.Context, passphrase string) error
	SaveHost(ctx context.Context, h keelclient.HostDetail) (keelclient.Host, error)
	DeleteHost(ctx context.Context, id string) error
	DuplicateHost(ctx context.Context, id string) (keelclient.Host, error)
	TestHost(ctx context.Context, h keelclient.HostDetail) (keelclient.TestResult, error)
	HostKey(ctx context.Context, id string) (keelclient.KnownKey, error)
	ForgetHostKey(ctx context.Context, id string) error
	SetAgentReadable(ctx context.Context, id string, readable bool) error
	Identities(ctx context.Context) ([]keelclient.Identity, error)
	Groups(ctx context.Context) ([]keelclient.Group, error)

	// Folders (folders.go): keel serve keeps them; the window reads them again after every write.
	SaveGroup(ctx context.Context, g keelclient.Group) (keelclient.Group, error)
	DeleteGroup(ctx context.Context, id string) error
	MoveHosts(ctx context.Context, ids []string, groupID string) error
	GroupMembers(ctx context.Context, id string) (keelclient.GroupInfo, error)

	// The Group page (grouppage.go): roles, variables and the Ansible export. keel serve keeps and builds all of it.
	SetGroupRole(ctx context.Context, id, hostID, role string) (keelclient.Group, error)
	SetGroupVar(ctx context.Context, id, name, value string) (keelclient.Group, error)
	GroupAnsible(ctx context.Context, id string, q keelclient.AnsibleQuery) ([]keelclient.ExportFile, error)

	// Group runs (grouprun.go): keel serve holds the run and makes every proposal; none of these writes to a session. A
	// proposal is typed only by the group approve dialog's Type action, through the one ApprovedInject call.
	StartGroupRun(ctx context.Context, runbookID, groupID, mode string) (keelclient.GroupRun, error)
	GroupRun(ctx context.Context, id string) (keelclient.GroupRun, error)
	StopGroupRun(ctx context.Context, id string) (keelclient.GroupRun, error)
	SetGroupRunMode(ctx context.Context, id, mode string) (keelclient.GroupRun, error)
	ProposeGroupStep(ctx context.Context, id string, panes []string) (keelclient.GroupRun, error)
	FillGroupStep(ctx context.Context, id string, proposalIDs []string) ([]keelclient.GroupFill, error)
	MarkGroupStep(ctx context.Context, id, stepID, hostID, action string) (keelclient.GroupRun, error)

	// First run and the pages.
	SetupVault(ctx context.Context, passphrase string) error
	SSHConfigPreview(ctx context.Context) (keelclient.SSHPreview, error)
	SSHConfigImport(ctx context.Context, req keelclient.ImportRequest) (keelclient.ImportResult, error)
	FirstRunDone(ctx context.Context) error
	SaveIdentity(ctx context.Context, in keelclient.IdentityInput) (keelclient.Identity, error)
	DeleteIdentity(ctx context.Context, id string) error
	Logs(ctx context.Context, limit int) ([]keelclient.LogEntry, error)

	// Snippets (they are typed only through Approve before type).
	Snippets(ctx context.Context) ([]keelclient.Snippet, error)
	SaveSnippet(ctx context.Context, in keelclient.SnippetInput) (keelclient.Snippet, error)
	DeleteSnippet(ctx context.Context, id string) error

	// Notebooks (notebooks.go): keel serve keeps them; the window holds the list and the open documents in memory only.
	// A fence is typed only through the snippet paths (notebooktype.go); none of these writes to a session.
	Notebooks(ctx context.Context) ([]keelclient.Notebook, error)
	CreateNotebook(ctx context.Context, in keelclient.NotebookSave) (keelclient.Notebook, error)
	SaveNotebook(ctx context.Context, id string, in keelclient.NotebookSave) (keelclient.SavedNotebook, error)
	PatchNotebook(ctx context.Context, id string, name *string, pinned *bool) (keelclient.Notebook, error)
	DeleteNotebook(ctx context.Context, id string, permanent bool) error
	SetNotebookPrivate(ctx context.Context, id string, private bool) (keelclient.Notebook, error)
	MarkNotebookReviewed(ctx context.Context, id string) (keelclient.Notebook, error)
	NotebookHistory(ctx context.Context, id string) ([]keelclient.NotebookVersion, error)
	NotebookVersion(ctx context.Context, id, vid string) (keelclient.NotebookVersion, error)
	RestoreNotebookVersion(ctx context.Context, id, vid string, rev int) (keelclient.Notebook, error)
	PurgeNotebookVersion(ctx context.Context, id, vid string) error
	DeletedNotebooks(ctx context.Context) ([]keelclient.NotebookVersion, error)
	DeletedNotebook(ctx context.Context, vid string) (keelclient.NotebookVersion, error)
	RestoreDeletedNotebook(ctx context.Context, vid string) (keelclient.Notebook, error)
	PurgeDeletedNotebook(ctx context.Context, vid string) error

	// Port forwarding.
	Tunnels(ctx context.Context) ([]keelclient.Tunnel, error)
	SaveTunnel(ctx context.Context, in keelclient.TunnelInput) (keelclient.Tunnel, error)
	DeleteTunnel(ctx context.Context, id string) error
	StartTunnel(ctx context.Context, id string) (keelclient.Tunnel, error)
	StopTunnel(ctx context.Context, id string) (keelclient.Tunnel, error)

	// Settings.
	Runtime(ctx context.Context) (keelclient.Runtime, error)
	SetSignal(ctx context.Context, url string) (keelclient.Runtime, error)
	StopRuntime(ctx context.Context) error

	// The agent panel (agent.go): keel serve reads the panes named in an ask and answers; nothing here types.
	AgentSettings(ctx context.Context) (keelclient.AgentSettings, error)
	AgentProviders(ctx context.Context) ([]keelclient.AgentProvider, error)
	SetAgentKey(ctx context.Context, in keelclient.AgentKeyInput) error
	Ask(ctx context.Context, req keelclient.AskRequest) (keelclient.AskResponse, error)
	EditProposal(ctx context.Context, conversationID, proposalID, command string) (keelclient.Proposal, error)

	// Saved conversations and Settings · Agent (agentconv.go, agentsettings.go): read into memory, never kept on disk.
	Conversations(ctx context.Context) (keelclient.ConversationList, error)
	Conversation(ctx context.Context, id string) (keelclient.Transcript, error)
	RenameConversation(ctx context.Context, id, title string) (string, error)
	DeleteConversation(ctx context.Context, id string) error
	DeleteAllConversations(ctx context.Context) error
	ProposeAgain(ctx context.Context, conversationID, proposalID string) (keelclient.Proposal, error)
	SaveAgentSettings(ctx context.Context, p keelclient.AgentSettingsPatch) (keelclient.AgentSettings, error)
	DeleteAgentKey(ctx context.Context, provider string) error

	// Notes for the agent and runbooks (notes.go, runbooks.go): keel serve keeps and redacts them; the client holds them in
	// memory only. A proposal is kept only by SaveProposedNote and SaveProposedRunbook, which the cards' Save buttons call.
	Notes(ctx context.Context) (keelclient.NoteList, error)
	SaveNote(ctx context.Context, in keelclient.NoteInput) (keelclient.SavedNote, error)
	DeleteNote(ctx context.Context, id string) error
	DeleteAllNotes(ctx context.Context) (int, error)
	SaveProposedNote(ctx context.Context, conversationID, noteID, text string) (keelclient.SavedNote, error)
	UndoProposedNote(ctx context.Context, conversationID, noteID string) (keelclient.NoteProposal, error)
	DismissProposedNote(ctx context.Context, conversationID, noteID string) (keelclient.NoteProposal, error)
	Runbooks(ctx context.Context) (keelclient.RunbookList, error)
	SaveRunbook(ctx context.Context, id string, in keelclient.RunbookInput) (keelclient.SavedRunbook, error)
	DeleteRunbook(ctx context.Context, id string) error
	SetRunbookPick(ctx context.Context, id string, on bool) (keelclient.Runbook, error)
	DraftRunbook(ctx context.Context, conversationID string) (keelclient.RunbookDraft, error)
	RunbookState(ctx context.Context, conversationID string) (keelclient.RunState, error)
	StepAction(ctx context.Context, conversationID, stepID, action string) (keelclient.RunState, error)
	StopRunbook(ctx context.Context, conversationID string) (keelclient.RunState, error)
	MoveRunbook(ctx context.Context, conversationID string) (keelclient.RunState, error)
	SaveProposedRunbook(ctx context.Context, conversationID, id string) (keelclient.SavedRunbook, error)
	UndoProposedRunbook(ctx context.Context, conversationID, id string) (keelclient.RunbookProposal, error)
	DismissProposedRunbook(ctx context.Context, conversationID, id string) (keelclient.RunbookProposal, error)
	DraftRunbookOfNotebook(ctx context.Context, notebookID string) (keelclient.RunbookDraft, error)

	// What the agent did to notebooks (agentnotebooks.go): each is a request keel serve acts on and answers; this client
	// changes no notebook itself.
	UndoNotebookChange(ctx context.Context, conversationID, id string) (keelclient.NotebookAction, error)
	ConfirmNotebookDelete(ctx context.Context, conversationID, id string) (keelclient.NotebookAction, error)
	DismissNotebookProposal(ctx context.Context, conversationID, id string) (keelclient.NotebookAction, error)
	RestoreNotebookChange(ctx context.Context, conversationID, id string) (keelclient.NotebookAction, error)

	// Share (share.go): the owner's side. The answer to a join request is AdmitShare, called only by Allow and Deny.
	Share(ctx context.Context, sessionID string) (string, error)
	AdmitShare(ctx context.Context, sessionID, peerID string, allow bool) error
	StopShare(ctx context.Context, sessionID string) error
}

// clientBackend adapts *keelclient.Client, whose Spawn and Attach return *Conn.
type clientBackend struct{ *keelclient.Client }

func (b clientBackend) Spawn(hostID string, cols, rows int) (session, error) {
	c, err := b.Client.Spawn(hostID, cols, rows)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (b clientBackend) Attach(id string, cols, rows int) (session, error) {
	c, err := b.Client.Attach(id, cols, rows)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// pane is one terminal of a tab.
type pane struct {
	id   string // local id
	host keelclient.Host
	conn session
	sid  string // session id when known before the ready frame (an attached session)

	// Set by workspace.mkBody: a live terminal, or a placeholder in tests.
	view    func(c *ui.Context) ui.Element
	size    func() (cols, rows int)
	release func()

	handle ui.Handle // the body's identity, for keyboard focus

	// What the terminal shows, for a direct type (typenow.go): whether it is on the alternate screen and the line the
	// cursor is on. nil for a placeholder body: then the screen is unknown, which is read as the alternate screen.
	screen func() (altScreen bool, promptLine string)
	typing bool // a direct type is on its way: another press gets the app's message

	sg      paneSuggest                              // in-pane suggestions (suggestpane.go)
	keyHook func(mods ui.Modifiers, key ui.Key) bool // the terminal's OnKey: set by newPane

	remote      bool   // a joined share (join.go): its connection is a WebRTC peer, not a keel serve session
	joinKey     string // which join it is (the origin and the room, never the key)
	blockedSeen uint64 // State.Blocked as the window last showed the notice for

	// Set by workspace.newPane and mkBody: the terminal font size to start at, what a zoom of the terminal
	// (the wheel) asks the workspace for, and the terminal's own search (nil for a placeholder).
	fontSize   float32
	fontFamily string // the terminal's family list (termfont.go)
	zoom       func(steps int, reset bool)
	search     *termSearch
	setFont    func(size float32)
	setTheme   func() // gives the terminal the colours of the theme in use

	// Set by newPane from the workspace's terminal preferences, and by mkBody: Copy on select (changes live) and
	// Right-click in the terminal (read when the terminal is made).
	copyOnSelect bool
	rightClick   string
	setCopy      func(on bool)

	declined keelclient.HostKey // the host-key offer the user cancelled or answered: its dialog stays closed

	nb *notebookPane // set for a notebook pane (notebooks.go): it has no session, and conn is a stub that refuses everything
}

func (p *pane) sessionID() string {
	if p.remote { // a joined share is not a session of this machine's keel serve: nothing may kill, reconnect or save it by id
		return ""
	}
	if id := p.conn.SessionID(); id != "" {
		return id
	}
	return p.sid
}

// status is the session's last reported state; empty until the server says.
func (p *pane) status() string { return p.conn.Snapshot().Status }

type tab struct {
	root    *layout.Node
	panes   map[string]*pane
	focused string
	zoomed  string // the pane that fills the tab (focus view), or ""
	title   string // the name the user gave the tab, or ""

	h ui.Handle // the tab in the strip, for where a dragged tab lands

	page *startPage // set while the tab is the New tab page: it has no panes
	pg   *tabPage   // set while the tab is a page (Keychain, Logs…): it has no panes either

	remote bool // the tab joined someone else's share: a terminal and nothing else, never saved
}

// isNew reports the New tab page.
func (t *tab) isNew() bool { return t.page != nil }

func (t *tab) name() string {
	if t.pg != nil { // a page is named by what it is: no rename
		switch { // the Group page by its folder, the group run page by its runbook (term.js setPageTitle)
		case t.pg.group != nil && t.pg.group.title != "":
			return t.pg.group.title
		case t.pg.grouprun != nil && t.pg.grouprun.title != "":
			return t.pg.grouprun.title
		}
		return pageDefs[t.pg.name].title
	}
	if t.title != "" {
		return t.title
	}
	if t.isNew() {
		return "New Tab"
	}
	if t.onlyNotebooks() { // a tab of notebooks is named after the one in front (term.js createNotebookTab)
		if p := t.panes[t.focused]; p != nil {
			return strings.TrimSpace(p.nb.title)
		}
	}
	if p := t.panes[t.focused]; p != nil {
		if t.remote { // a joined tab says so, whoever drives (term.js tabName)
			return p.title() + " · joined"
		}
		return p.host.Name
	}
	return ""
}

type workspace struct {
	be backend
	// work runs blocking calls off the UI thread; post runs fn on it and redraws. Both run
	// fn at once in tests.
	work   func(fn func())
	post   func(fn func())
	mkBody func(p *pane)

	capsSince time.Time // when the hold began; zero while ⌘ is up (keycaps.go)
	capsOff   bool      // a key or a click used this hold: no caps until it is let go
	capAlpha  float32   // 0 hides the caps, 1 shows them
	capsBase  []ui.Key  // keys already down when the hold began: stale, not a use of it

	hosts          []keelclient.Host
	snippets       []keelclient.Snippet
	notebooks      []keelclient.Notebook // GET /api/notebooks (notebooks.go)
	nbDlg          *nbDialog             // Rename notebook, History, Recently deleted (notebookdialogs.go)
	sideSeg        string                // the sidebar's section: "hosts" or "snippets" (term.js SIDEBAR_SEGS)
	snipQuery      string                // the sidebar's Snippets filter
	snipH          ui.Handle
	snipDlg        *snippetDialog      // New snippet / Edit snippet
	folders        []keelclient.Group  // the vault's folders, as GET /api/groups lists them (folders.go)
	folderDlg      *folderDialog       // New group / Edit group
	folderRen      *folderRename       // the folder whose name is being edited in the tree
	tunnels        []keelclient.Tunnel // the forwards, as GET /api/tunnels lists them (forwards.go)
	tunReading     bool                // the 2 s read of them is in flight
	fwdQuery       string              // the sidebar's Forwards filter
	paneQuery      string              // the sidebar's Panes filter
	win            prefs               // Window opacity and blur as saved (settings.go)
	term           termPrefs           // Copy on select, Right-click in the terminal, the display name
	translucentWin bool                // the window was created Transparent: only then can opacity change live
	blurHook       func(radius int)    // sets the window's blur; nil in tests
	appearanceHook func(choice string) // gives the window the theme as its own appearance (macOS chrome); nil where it cannot
	dark, osDark   bool                // the theme in use, and whether the OS appearance is dark (theme.go)
	themeSeen      bool                // the first frame has set the theme
	themeNative    bool                // the window takes the theme as its own appearance: no opaque rule (theme.go)
	paneH          ui.Handle
	fwdH           ui.Handle
	fwdDlg         *forwardDialog // New forward / Edit forward
	fwdKey         *fwdKeyOffer   // "Unknown host key" for a forward that could not start
	tabs           []*tab
	active         int
	filter         string
	sidebar        bool
	collapsed      map[string]bool
	queued         []ui.Toast  // toasts asked for, handed to MyGo by the next frame (toast.go)
	blocked        string      // a message that replaces the workspace (locked vault, no keel serve)
	gate           *unlockGate // the Unlock screen, over the whole window
	first          *firstRun   // the First run screens (or the palette's host import), over the whole window
	closed         bool
	sugNow         time.Time // the frame's time, the suggestion machines' clock

	rev      string        // GET /api/vault's rev at the last read: a change reads the hosts again
	polling  bool          // a poll is in flight
	pollStop chan struct{} // closed when the window closes

	seq       int
	wantFocus string                      // pane to give the keyboard to once it is on screen
	reviewing string                      // pane to give it to after a direct type, once no key is held (typenow.go)
	reviewAt  time.Time                   // when that began
	splits    map[*layout.Node]*ui.Handle // each split's container, for its size while dragging
	later     []func()                    // actions queued by the view, run when the frame is built

	alpha float32 // the window's opacity, 0–1; grounds are painted at it (see ground)

	dataDir  string // where keel-native.json and keel-native-workspace.json live; "" keeps nothing
	termSize int    // the terminal font size of every pane, termSizeMin–termSizeMax

	recent     []closedEntry // recently closed tabs and panes, newest first (⌘⇧T)
	renaming   *tab          // the tab whose name is being edited
	renameText string
	renameSeen bool // the rename field has had the keyboard
	renameH    ui.Handle

	findPane   *pane // the pane whose find bar is open
	findText   string
	findDone   string // the text the terminal's search was last given
	findFocus  bool
	findHandle ui.Handle

	saved saveState

	pal       *palette // the open command palette
	palH      ui.Handle
	recentIDs []string // host ids, newest first: the New tab page's recent hosts (keel-native.json)

	live            bool      // the first listing has been read: the window keeps a tab from now on
	newTabLockUntil time.Time // ⌘T repeats inside this are ignored
	quick           *quickDialog
	hostDlg         *hostDialog     // New host / Edit host
	identDlg        *identityDialog // New identity / Edit identity (the Keychain page)
	confirm         *hostConfirm    // a confirm dialog on its own (the host menu's Delete)
	approve         *approveDialog  // Approve before type: the one way Keel types a snippet or an agent proposal (approve.go)
	agent           *agentPanel     // the agent panel and its conversation (agent.go)
	copyOut         string          // text the next frame writes to the clipboard (a card menu's Copy)
	lastSession     *tab            // the session tab shown last: where a snippet typed from a page goes

	joinDlg       *joinDialog                                         // Join a session (join.go)
	joiner        func(l joinpeer.Link, name string) (session, error) // opens the peer of a joined share
	share         *sharePop                                           // the open Share popover (share.go)
	shareLinks    map[string]shareLink                                // the join link this window was given, by session id; never saved
	promptPressed bool                                                // a press this frame landed on a prompt: it does not dismiss the Share popover
	prompts       []*sharePrompt                                      // the region "Share requests"
}

// ground is c at the window's alpha: what every ground the view paints goes through. At 1 it is c.
func (w *workspace) ground(c ui.Color) ui.Color {
	if w.alpha >= 1 {
		return c
	}
	return c.Alpha(w.alpha)
}

// termGround is the terminal body's ground: the stage at the window's alpha. In the light theme the body keeps less of the
// see-through (vibrancy-term-share), so the blurred desktop does not show as dark shapes behind the text.
func (w *workspace) termGround() ui.Color {
	if !w.dark && w.alpha < 1 {
		return stage.Alpha(1 - (1-w.alpha)*termShareLight)
	}
	return w.ground(stage)
}

func newWorkspace(be backend) *workspace {
	return &workspace{
		be: be, agent: newAgentPanel(), alpha: 1, themeNative: followsNatively(), sidebar: true, collapsed: map[string]bool{}, termSize: termSizeDefault,
		win: prefs{Opacity: opacityMax, Blur: blurDefault, TermSize: termSizeDefault, Theme: themeDark, UISize: uiSizeDefault, TermFont: fontJetBrains}, term: termPrefs{CopyOnSelect: true, RightClick: rightMenu},
		splits: map[*layout.Node]*ui.Handle{},
		work:   func(fn func()) { go fn() },
		joiner: func(l joinpeer.Link, name string) (session, error) { return joinpeer.Open(l, name) },
	}
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), callTimeout)
}

func (w *workspace) tab() *tab {
	if w.active < 0 || w.active >= len(w.tabs) {
		return nil
	}
	return w.tabs[w.active]
}

func (w *workspace) focusedPane() *pane {
	if t := w.tab(); t != nil {
		return t.panes[t.focused]
	}
	return nil
}

func (w *workspace) paneCount() (n int) {
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if p.nb == nil { // a notebook is not a session
				n++
			}
		}
	}
	return n
}

// fail shows a failed call as a toast (toast(err.message, true)).
func (w *workspace) fail(what string, err error) { w.warn(errText(what, err)) }

// start asks keel serve what exists. The vault answers first: a locked vault stops here.
func (w *workspace) start() {
	if w.dataDir != "" && loadAgentOpen(w.dataDir) { // the panel stays as it was left
		w.agent.open = true
	}
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		v, err := w.be.Vault(c)
		if err != nil {
			w.post(func() { w.blocked = "Cannot reach keel serve: " + err.Error() })
			return
		}
		if v.Locked && !v.NeedsSetup {
			w.post(func() { w.blocked = ""; w.showUnlock(v) })
			return
		}
		if v.NeedsSetup || v.FirstRun { // gate.js update: step 1 of First run
			w.post(func() { w.showFirstRun(v) })
			return
		}
		hosts, err := w.be.Hosts(c)
		if err != nil {
			w.post(func() { w.fail("hosts", err) })
			return
		}
		sessions, err := w.be.Sessions(c)
		if err != nil {
			w.post(func() { w.hosts = hosts; w.fail("sessions", err) })
			return
		}
		snippets, snipErr := w.be.Snippets(c)
		folders, _ := w.be.Groups(c)  // a failed list leaves the flat tree: the next change reads it again
		tunnels, _ := w.be.Tunnels(c) // a failed read leaves none: the 2 s read and the next change fill them in
		notebooks, _ := w.be.Notebooks(c)
		type attached struct {
			host keelclient.Host
			conn session
			sid  string
		}
		var live []attached
		var failed error
		for _, s := range sessions {
			cols, rows := s.Cols, s.Rows
			if cols <= 0 || rows <= 0 {
				cols, rows = startCols, startRows
			}
			conn, err := w.be.Attach(s.ID, cols, rows)
			if err != nil {
				failed = err
				continue
			}
			h := keelclient.Host{ID: s.HostID, Name: s.Title, Color: s.Color}
			for _, k := range hosts {
				if k.ID == s.HostID {
					h = k
				}
			}
			live = append(live, attached{h, conn, s.ID})
		}
		doc := loadSaved(w.dataDir)
		var recent []string
		if w.dataDir != "" {
			recent = loadRecentHosts(w.dataDir)
		}
		w.post(func() {
			if w.closed {
				for _, a := range live {
					a.conn.Close()
				}
				return
			}
			w.hosts = hosts
			w.folders = folders
			w.snippets = snippets
			w.tunnels = tunnels
			w.notebooks = notebooks
			if snipErr != nil {
				w.fail("snippets", snipErr)
			}
			w.rev = v.Rev
			w.recentIDs = recent
			panes := make([]*pane, len(live))
			for i, a := range live {
				panes[i] = w.newPane(a.host, a.conn)
				panes[i].sid = a.sid
			}
			w.restore(doc, panes)
			if failed != nil {
				w.fail("attach", failed)
			}
			w.live = true
			w.settle() // an empty window shows a New tab
			w.saved.ready = true
		})
	})
}

func (w *workspace) newPane(h keelclient.Host, conn session) *pane {
	w.seq++
	p := &pane{id: strconv.Itoa(w.seq), host: h, conn: conn, fontSize: float32(w.termSize), fontFamily: termStack(w.win.TermFont), copyOnSelect: w.term.CopyOnSelect, rightClick: w.term.RightClick}
	p.zoom = func(steps int, reset bool) {
		if reset {
			w.setTermSize(termSizeDefault)
		} else {
			w.setTermSize(w.termSize + steps)
		}
	}
	p.keyHook = func(mods ui.Modifiers, key ui.Key) bool { return w.suggestKey(p, mods, key) }
	conn.OnChange(func() { w.post(func() {}) }) // redraw the dot and the bar
	w.mkBody(p)
	return p
}

// addTab puts p alone in a new tab and shows it.
func (w *workspace) addTab(p *pane) {
	w.tabs = append(w.tabs, &tab{root: layout.Leaf(p.id), panes: map[string]*pane{p.id: p}, focused: p.id})
	w.selectTab(len(w.tabs) - 1)
}

func (w *workspace) selectTab(i int) {
	if i < 0 || i >= len(w.tabs) {
		return
	}
	w.active = i
	if len(w.tabs[i].panes) > 0 && !w.tabs[i].onlyNotebooks() {
		w.lastSession = w.tabs[i]
	}
	w.wantFocus = w.tabs[i].focused
	if p := w.tabs[i].page; p != nil {
		p.focus = true // the New tab page's input has the keyboard
	}
	if w.tabs[i].pg != nil {
		w.loadPage(w.tabs[i]) // a page shows what the server has now
	}
}

// newLocalTab opens the first local host in a new tab (term.js newLocalTab).
func (w *workspace) newLocalTab() {
	for _, h := range w.hosts {
		if h.AuthType == "local" {
			w.openHost(h)
			return
		}
	}
	w.warn("Local shell is not available")
}

// split starts another session to the same host as target and inserts it after target.
func (w *workspace) split(t *tab, target string, d layout.Dir) {
	p := t.panes[target]
	if p == nil {
		return
	}
	if p.nb != nil { // a notebook splits into a session on the host of the tab's terminal, else the local shell (term.js splitPane)
		if term := t.firstTerm(); term != nil {
			w.splitOn(t, target, term.host, d)
			return
		}
		for _, h := range w.hosts {
			if h.AuthType == "local" {
				w.splitOn(t, target, h, d)
				return
			}
		}
		w.say("Split needs a local host session")
		return
	}
	w.splitOn(t, target, p.host, d)
}

// splitOn starts a session on h and inserts it after target.
func (w *workspace) splitOn(t *tab, target string, h keelclient.Host, d layout.Dir) {
	if t.panes[target] == nil {
		return
	}
	if t.remote || t.panes[target].remote {
		w.say("Split needs a local host session") // term.js splitPane
		return
	}
	w.work(func() {
		conn, err := w.be.Spawn(h.ID, startCols, startRows)
		w.post(func() {
			if err != nil {
				w.fail("split "+h.Name, err)
				return
			}
			if w.closed || t.panes[target] == nil { // the window or the pane went away meanwhile
				conn.Close()
				return
			}
			np := w.newPane(h, conn)
			t.root = t.root.Split(target, np.id, d)
			t.panes[np.id] = np
			t.focused = np.id
			t.zoomed = "" // a split leaves focus view (workspace.js splitPane)
			if t == w.tab() {
				w.wantFocus = np.id
			}
		})
	})
}

func (w *workspace) splitFocused(d layout.Dir) {
	if t := w.tab(); t != nil {
		w.split(t, t.focused, d)
	}
}

// closePane ends that pane's session (Kill) and removes it; the last pane closes the tab.
func (w *workspace) closePane(t *tab, id string) {
	p := t.panes[id]
	if p == nil {
		return
	}
	kind := "pane"
	if len(t.panes) == 1 {
		kind = "tab" // the last pane is recorded as the tab (term.js closePane)
	}
	w.recordClosed(p, kind)
	w.dropPane(p)
	delete(t.panes, id)
	t.root = t.root.Remove(id)
	if t.zoomed == id {
		t.zoomed = ""
	}
	if t.root == nil {
		w.removeTab(t)
		return
	}
	if t.focused == id {
		t.focused = t.root.Leaves()[0]
		if t == w.tab() {
			w.wantFocus = t.focused
		}
	}
}

// reconnect asks keel serve to start p's session again; the socket reports the new status.
func (w *workspace) reconnect(p *pane) {
	sid := p.sessionID()
	if sid == "" {
		return
	}
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		if err := w.be.Reconnect(c, sid); err != nil {
			w.post(func() { w.fail("reconnect", err) })
		}
	})
}

// trustHostKey is the host key dialog's trust button and nothing else: it sends back exactly the offer the
// server made for p's session, then reconnects.
func (w *workspace) trustHostKey(p *pane, k keelclient.HostKey) {
	p.declined = k // the dialog closes now; a new offer opens it again
	sid := p.sessionID()
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		err := w.be.TrustHostKey(c, k, k.Kind == "host-key-mismatch")
		if err == nil && sid != "" {
			err = w.be.Reconnect(c, sid)
		}
		if err != nil {
			w.post(func() { w.fail("trust host key", err) })
		}
	})
}

func (w *workspace) kill(sid string) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		if err := w.be.Kill(c, sid); err != nil {
			w.post(func() { w.fail("close pane", err) })
		}
	})
}

// killWhenNamed ends a session that was closed before the server's ready frame named it: it kills the
// session once the id arrives and then closes the connection. A connection that never learns its id
// is closed after callTimeout.
func (w *workspace) killWhenNamed(conn session) {
	var once sync.Once
	finish := func() {
		once.Do(func() {
			id := conn.SessionID()
			if id == "" {
				conn.Close()
				return
			}
			w.work(func() {
				c, cancel := ctx()
				defer cancel()
				if err := w.be.Kill(c, id); err != nil {
					w.post(func() { w.fail("close pane", err) })
				}
				conn.Close()
			})
		})
	}
	conn.OnChange(func() {
		if conn.SessionID() != "" {
			finish()
		}
	})
	if conn.SessionID() != "" {
		finish()
		return
	}
	time.AfterFunc(callTimeout, finish)
}

// closeFocused is ⌘W: the focused pane, whose last one closes the tab. The lone New tab is what an empty
// window shows: closing it would only open another (term.js closeActivePaneOrTab).
func (w *workspace) closeFocused() {
	t := w.tab()
	switch {
	case t == nil:
	case t.isNew():
		if len(w.tabs) > 1 {
			w.closeTab(t)
		}
	case t.isPage():
		w.closeTab(t)
	default:
		w.closePane(t, t.focused)
	}
}

func (w *workspace) release(p *pane) {
	if p.release != nil {
		p.release()
	}
	p.conn.Close()
}

// closeWindow detaches: every connection closes and no session is killed.
func (w *workspace) closeWindow() {
	w.saveNow()
	w.flushNotebooksNow() // what was typed in a notebook is saved before the window goes
	w.closed = true
	if w.agent.busy != nil { // an ask in flight stops with the window
		w.agent.busy()
	}
	if w.pollStop != nil {
		close(w.pollStop)
		w.pollStop = nil
	}
	w.saved.stop()
	for _, t := range w.tabs {
		for _, p := range t.panes {
			w.release(p)
		}
	}
}

// hostGroup is one group of the host tree.
type hostGroup struct {
	name  string
	hosts []keelclient.Host
}

const ungrouped = "Ungrouped"

// groups is the host tree for the current filter: groups and hosts by name, no group last.
func (w *workspace) groups() []hostGroup {
	q := strings.ToLower(strings.TrimSpace(w.filter))
	by := map[string][]keelclient.Host{}
	for _, h := range w.hosts {
		if q != "" && !strings.Contains(strings.ToLower(h.Name+"\x00"+h.Address+"\x00"+h.Username), q) {
			continue
		}
		g := h.Group
		if g == "" {
			g = ungrouped
		}
		by[g] = append(by[g], h)
	}
	var out []hostGroup
	for g, hs := range by {
		sort.Slice(hs, func(i, j int) bool { return strings.ToLower(hs[i].Name) < strings.ToLower(hs[j].Name) })
		out = append(out, hostGroup{g, hs})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].name == ungrouped) != (out[j].name == ungrouped) {
			return out[j].name == ungrouped
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

// panesOn counts the open panes on a host.
func (w *workspace) panesOn(hostID string) (n int) {
	for _, t := range w.tabs {
		for _, p := range t.panes {
			if p.host.ID == hostID {
				n++
			}
		}
	}
	return n
}

func (w *workspace) splitHandle(n *layout.Node) *ui.Handle {
	h := w.splits[n]
	if h == nil {
		h = new(ui.Handle)
		w.splits[n] = h
	}
	return h
}

// act queues a change the view asks for; it runs once the frame is built, never mid-tree.
func (w *workspace) act(fn func()) { w.later = append(w.later, fn) }
