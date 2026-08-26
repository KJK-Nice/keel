package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"
	"golang.org/x/term"

	"keel/internal/runtime"
	"keel/internal/store"
)

const sidebarWidth = 28

type focus int

const (
	focusHerd focus = iota
	focusTerm
)

type itemKind int

const (
	itemSession itemKind = iota
	itemHost
)

type item struct {
	kind    itemKind
	session runtime.Info
	host    store.Host
}

type streamMsg struct {
	id     string
	ev     runtime.Event
	closed bool
}

type dataMsg struct {
	sessions    []runtime.Info
	hosts       []store.Host
	snippets    []store.Snippet
	err         error
	hasSessions bool
}

type Options struct {
	Token string
	Name  string
}

type model struct {
	api      *API
	width    int
	height   int
	focus    focus
	prefix   bool
	cursor   int
	items    []item
	sessions []runtime.Info
	hosts    []store.Host
	snippets []store.Snippet
	active   string
	conn     *Conn
	herd     *HerdConn
	vt       vt10x.Terminal
	status   string
	err      error
	name     string
}

func Run(base string, opts Options) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("keel tui needs a real terminal; start the runtime with: keel server")
	}
	if opts.Name == "" {
		opts.Name = "guest"
	}
	m := model{
		api:    NewAPI(base, opts.Token, opts.Name),
		name:   opts.Name,
		status: "attach a session, or enter on a host to spawn one",
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), m.connectHerd(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type tickMsg struct{}

type herdMsg struct {
	ev HerdEvent
}

func (m model) connectHerd() tea.Cmd {
	api := m.api
	return func() tea.Msg {
		conn, err := api.Herd()
		if err != nil {
			return dataMsg{err: err}
		}
		return herdAttachedMsg{conn: conn}
	}
}

type herdAttachedMsg struct {
	conn *HerdConn
}

func listenHerd(ch <-chan HerdEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return herdMsg{ev: HerdEvent{Closed: true}}
		}
		return herdMsg{ev: ev}
	}
}

func (m model) refreshInventory() tea.Cmd {
	api := m.api
	return func() tea.Msg {
		hosts, err := api.Hosts()
		if err != nil {
			return dataMsg{err: err}
		}
		snippets, err := api.Snippets()
		if err != nil {
			return dataMsg{err: err}
		}
		return dataMsg{hosts: hosts, snippets: snippets}
	}
}

func (m *model) driving() bool {
	return m.conn != nil && m.conn.You.Role == runtime.RoleDriver
}

func (m *model) applyHerd(ev HerdEvent) {
	switch ev.Type {
	case "snapshot":
		m.sessions = ev.Sessions
	case "spawn":
		m.upsertSession(ev.Session)
	case "kill":
		id := ev.ID
		if id == "" {
			id = ev.Session.ID
		}
		out := m.sessions[:0]
		for _, s := range m.sessions {
			if s.ID != id {
				out = append(out, s)
			}
		}
		m.sessions = out
	case "update":
		m.upsertSession(ev.Session)
	}
}

func (m *model) upsertSession(info runtime.Info) {
	if info.ID == "" {
		return
	}
	for i, s := range m.sessions {
		if s.ID == info.ID {
			if info.Title == "" {
				info.Title = s.Title
			}
			if info.HostID == "" {
				info.HostID = s.HostID
				info.Color = s.Color
				info.Subtitle = s.Subtitle
			}
			m.sessions[i] = info
			return
		}
	}
	m.sessions = append(m.sessions, info)
}

func (m model) controlCmd() tea.Cmd {
	if m.conn == nil {
		return nil
	}
	conn := m.conn
	return func() tea.Msg {
		var err error
		if conn.You.Role == runtime.RoleOwner || conn.You.Role == runtime.RoleDriver {
			err = conn.TakeControl()
		} else {
			err = conn.RequestControl()
		}
		if err != nil {
			return dataMsg{err: err}
		}
		return nil
	}
}

func (m model) refresh() tea.Cmd {
	api := m.api
	return func() tea.Msg {
		sessions, err := api.Sessions()
		if err != nil {
			return dataMsg{err: err}
		}
		hosts, err := api.Hosts()
		if err != nil {
			return dataMsg{err: err}
		}
		snippets, err := api.Snippets()
		if err != nil {
			return dataMsg{err: err}
		}
		return dataMsg{sessions: sessions, hosts: hosts, snippets: snippets, hasSessions: true}
	}
}

func listen(id string, ch <-chan runtime.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamMsg{id: id, closed: true}
		}
		return streamMsg{id: id, ev: ev}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeVT()
		if m.conn != nil && m.driving() {
			c, r := m.termSize()
			_ = m.conn.Resize(c, r)
		}
		return m, nil
	case tickMsg:
		cmds := []tea.Cmd{m.refreshInventory(), tick()}
		if m.herd == nil {
			cmds = append(cmds, m.connectHerd())
		}
		return m, tea.Batch(cmds...)
	case herdAttachedMsg:
		if m.herd != nil {
			m.herd.Close()
		}
		m.herd = msg.conn
		return m, listenHerd(msg.conn.Events)
	case herdMsg:
		if msg.ev.Closed {
			if m.herd != nil {
				m.herd.Close()
			}
			m.herd = nil
			return m, nil
		}
		m.applyHerd(msg.ev)
		m.rebuildItems()
		if m.herd != nil {
			return m, listenHerd(m.herd.Events)
		}
		return m, nil
	case dataMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		if msg.hasSessions {
			m.sessions = msg.sessions
		}
		m.hosts = msg.hosts
		m.snippets = msg.snippets
		m.rebuildItems()
		return m, nil
	case spawnMsg:
		m.status = "spawned"
		return m.beginAttach(msg.id)
	case attachedMsg:
		m.conn = msg.conn
		m.status = "attached · ctrl+b d detaches the TUI, not the session"
		return m, listen(msg.id, msg.conn.Events)
	case killedMsg:
		if m.active == msg.id {
			if m.conn != nil {
				m.conn.Close()
			}
			m.conn = nil
			m.active = ""
			m.vt = nil
			m.focus = focusHerd
		}
		m.status = "killed session"
		return m, m.refresh()
	case streamMsg:
		if msg.id != m.active {
			return m, nil
		}
		if msg.closed {
			m.status = "detached from stream"
			return m, nil
		}
		if msg.ev.Kind == "bytes" && m.vt != nil {
			_, _ = m.vt.Write(msg.ev.Bytes)
		}
		if msg.ev.Kind == "status" && msg.ev.Message != "" {
			m.status = msg.ev.Message
		}
		if msg.ev.Kind == "presence" || msg.ev.Kind == "control" || msg.ev.Kind == "ready" {
			if msg.ev.Message != "" {
				m.status = msg.ev.Message
			}
			if len(msg.ev.Info.Attached) > 0 {
				m.upsertSession(msg.ev.Info)
				m.rebuildItems()
			}
		}
		if m.conn != nil {
			return m, listen(m.active, m.conn.Events)
		}
		return m, nil
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if msg.X >= sidebarWidth {
		m.focus = focusTerm
		m.prefix = false
		return m, nil
	}
	m.focus = focusHerd
	idx := msg.Y - 5
	if idx >= 0 && idx < len(m.items) {
		m.cursor = idx
		return m.activate()
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.prefix {
		m.prefix = false
		switch key {
		case "d", "q":
			return m, tea.Quit
		case "x":
			return m, m.killActive()
		case "h", "left":
			m.focus = focusHerd
			return m, nil
		case "c":
			return m, m.spawnLocal()
		case "t":
			return m, m.controlCmd()
		case "1", "2", "3", "4":
			return m, m.sendSnippet(int(key[0] - '1'))
		}
		return m, nil
	}
	if key == "ctrl+b" {
		m.prefix = true
		return m, nil
	}
	if m.focus == focusTerm && m.conn != nil {
		if key == "esc" {
			m.focus = focusHerd
			return m, nil
		}
		if m.driving() {
			_ = m.conn.Write(keyBytes(msg))
		} else {
			m.status = "following · ctrl+b t requests control"
		}
		return m, nil
	}
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter", "l", "right":
		return m.activate()
	case "x":
		return m, m.killSelected()
	case "tab":
		if m.active != "" {
			m.focus = focusTerm
		}
	case "n":
		return m, m.spawnLocal()
	}
	return m, nil
}

func (m *model) rebuildItems() {
	items := make([]item, 0, len(m.sessions)+len(m.hosts))
	for _, s := range m.sessions {
		items = append(items, item{kind: itemSession, session: s})
	}
	for _, h := range m.hosts {
		items = append(items, item{kind: itemHost, host: h})
	}
	m.items = items
	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
}

func (m model) activate() (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	if it.kind == itemSession {
		return m.beginAttach(it.session.ID)
	}
	return m, m.spawn(it.host.ID)
}

func (m model) spawnLocal() tea.Cmd {
	for _, h := range m.hosts {
		if h.AuthType == "local" {
			return m.spawn(h.ID)
		}
	}
	return nil
}

func (m model) spawn(hostID string) tea.Cmd {
	api := m.api
	cols, rows := m.termSize()
	return func() tea.Msg {
		info, err := api.Spawn(hostID, cols, rows)
		if err != nil {
			return dataMsg{err: err}
		}
		return spawnMsg{id: info.ID}
	}
}

type spawnMsg struct{ id string }

func (m model) beginAttach(id string) (tea.Model, tea.Cmd) {
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
	m.active = id
	c, r := m.termSize()
	m.vt = vt10x.New(vt10x.WithSize(max(c, 20), max(r, 8)))
	m.focus = focusTerm
	m.status = "attaching…"
	api := m.api
	cols, rows := c, r
	return m, func() tea.Msg {
		conn, err := api.Attach(id, cols, rows)
		if err != nil {
			return dataMsg{err: err}
		}
		return attachedMsg{id: id, conn: conn}
	}
}

type attachedMsg struct {
	id   string
	conn *Conn
}

func (m model) killSelected() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	it := m.items[m.cursor]
	if it.kind != itemSession {
		return nil
	}
	return m.kill(it.session.ID)
}

func (m model) killActive() tea.Cmd {
	if m.active == "" {
		return m.killSelected()
	}
	return m.kill(m.active)
}

func (m model) kill(id string) tea.Cmd {
	api := m.api
	return func() tea.Msg {
		if err := api.Kill(id); err != nil {
			return dataMsg{err: err}
		}
		return killedMsg{id: id}
	}
}

type killedMsg struct{ id string }

func (m model) sendSnippet(i int) tea.Cmd {
	if i < 0 || i >= len(m.snippets) || m.conn == nil || !m.driving() {
		return nil
	}
	cmd := m.snippets[i].Command
	if !strings.HasSuffix(cmd, "\n") {
		cmd += "\n"
	}
	_ = m.conn.Write([]byte(cmd))
	return nil
}

func (m model) termSize() (int, int) {
	cols := max(20, m.width-sidebarWidth-2)
	rows := max(8, m.height-3)
	return cols, rows
}

func (m *model) resizeVT() {
	if m.vt == nil {
		return
	}
	c, r := m.termSize()
	m.vt.Resize(c, r)
}

func (m model) View() string {
	if m.width == 0 {
		return "starting keel…"
	}
	sidebar := m.viewSidebar()
	main := m.viewMain()
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
	foot := m.viewFooter()
	return lipgloss.JoinVertical(lipgloss.Left, body, foot)
}

func (m model) viewSidebar() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5ee0b5")).Render("KEEL")
	sub := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b95a8")).Render("the herd")
	var b strings.Builder
	b.WriteString(" " + title + "\n")
	b.WriteString(" " + sub + "\n\n")

	section := func(label string) {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#8b95a8")).Render(" "+strings.ToUpper(label)) + "\n")
	}
	section("sessions")
	if len(m.sessions) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7385")).Render("  nobody attached") + "\n")
	}
	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("#5ee0b5")).Render("▸ ")
		}
		if it.kind == itemSession && (i == 0 || m.items[i-1].kind != itemSession) {
			// already heading
		}
		if it.kind == itemHost && (i == 0 || m.items[i-1].kind != itemHost) {
			b.WriteString("\n")
			section("hosts")
		}
		line := ""
		if it.kind == itemSession {
			dot := statusDot(it.session.Status)
			mark := ""
			if it.session.ID == m.active {
				mark = " ●"
			}
			line = fmt.Sprintf("%s%s %-12s %s%s", cursor, dot, trunc(it.session.Title, 12), people(it.session), mark)
		} else {
			line = fmt.Sprintf("%s+ %s", cursor, trunc(it.host.Name, 20))
		}
		if i == m.cursor {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("#e8edf7")).Render(line)
		} else {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b95a8")).Render(line)
		}
		b.WriteString(line + "\n")
	}

	style := lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(max(1, m.height-2)).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(lipgloss.Color("#1e2636"))
	return style.Render(b.String())
}

func (m model) viewMain() string {
	w := max(10, m.width-sidebarWidth-1)
	title := "no session"
	if m.active != "" {
		title = m.active[:min(8, len(m.active))]
		for _, s := range m.sessions {
			if s.ID == m.active {
				title = s.Title + " · " + string(s.Status)
				if m.driving() {
					title += " · driving"
				} else if m.conn != nil {
					title += " · following"
				}
				if names := attachedNames(s); names != "" {
					title += " · " + names
				}
			}
		}
	}
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b95a8")).Width(w).Render(" " + title)
	body := m.status
	if m.vt != nil {
		c, r := m.termSize()
		body = renderVT(m.vt, c, r-1)
	} else {
		body = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b95a8")).Render(
			"\n  The runtime owns the terminals.\n  Enter a host to spawn, enter a session to attach.\n  ctrl+b d detaches this TUI. Sessions keep running.\n",
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func (m model) viewFooter() string {
	help := "ctrl+b d detach  ctrl+b t control  enter attach/spawn  x kill  n local  j/k  q quit"
	if m.prefix {
		help = "PREFIX  d detach  t control  x kill  c local  h herd  1-4 snippet"
	}
	if m.err != nil {
		help = m.err.Error()
	} else if m.status != "" && m.prefix == false && m.focus == focusHerd {
		help = m.status + "  ·  " + help
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b95a8")).
		Background(lipgloss.Color("#121826")).
		Width(m.width).
		Render(" " + trunc(help, max(10, m.width-2)))
}

func people(s runtime.Info) string {
	if len(s.Attached) == 0 {
		return string(s.Status)
	}
	driver := ""
	for _, c := range s.Attached {
		if c.Role == runtime.RoleDriver {
			driver = c.Name
			break
		}
	}
	if driver == "" {
		return fmt.Sprintf("%s · %d", s.Status, len(s.Attached))
	}
	return trunc(driver+"*", 12)
}

func attachedNames(s runtime.Info) string {
	if len(s.Attached) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s.Attached))
	for _, c := range s.Attached {
		n := c.Name
		if c.Role == runtime.RoleDriver {
			n += "*"
		}
		parts = append(parts, n)
	}
	return strings.Join(parts, ", ")
}

func statusDot(st runtime.Status) string {
	c := "#6b7385"
	switch st {
	case runtime.StatusReady:
		c = "#5ee0b5"
	case runtime.StatusConnecting:
		c = "#f5c15d"
	case runtime.StatusError, runtime.StatusExited:
		c = "#ff6b7a"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:max(0, n-1)] + "…"
}

func keyBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeyTab:
		return []byte("\t")
	case tea.KeySpace:
		return []byte(" ")
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlA:
		return []byte{0x01}
	case tea.KeyCtrlE:
		return []byte{0x05}
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	default:
		if msg.Alt && len(msg.Runes) == 1 {
			return []byte{0x1b, byte(msg.Runes[0])}
		}
		return []byte(msg.String())
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
