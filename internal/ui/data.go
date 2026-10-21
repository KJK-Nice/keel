package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/a-h/templ"

	"keel/internal/agent/provider"
	"keel/internal/applog"
	"keel/internal/store"
	"keel/internal/tunnel"
)

var hostColors = []string{"#5ee0b5", "#6ea8ff", "#c084fc", "#f5c15d", "#ff6b7a", "#67e8f9"}

func mergeAttrs(parts ...templ.Attributes) templ.Attributes {
	out := templ.Attributes{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

type PageProps struct {
	Locked     bool
	NeedsSetup bool
	// KeychainName is what this OS calls the keychain that can hold the
	// vault key ("macOS Keychain" on a Mac).
	KeychainName string
	Titlebar     bool
	Native       bool
	Name         string
	SignalURL    string
	Query        string
	LogFilter    string
	Hosts        []Host
	Identities   []store.Identity
	Tunnels      []tunnel.Info
	Snippets     []store.Snippet
	Notebooks    []store.Notebook
	Groups       []store.Group
	GroupFocus   string
	Logs         []applog.Entry
}

func (p PageProps) SignalsJSON() string {
	locked, setup := "false", "false"
	if p.Locked {
		locked = "true"
	}
	if p.NeedsSetup {
		setup = "true"
	}
	q, _ := json.Marshal(p.Query)
	lf, _ := json.Marshal(p.LogFilter)
	gf, _ := json.Marshal(p.GroupFocus)
	return fmt.Sprintf(
		`{"vault":"hosts","query":%s,"logFilter":%s,"groupFocus":%s,"locked":%s,"needsSetup":%s,"authType":"password","identKind":"password","tunnelKind":"local",`+
			// Page-local filters (docs/ui-redesign.md §12, D2). The leading
			// underscore keeps Datastar from sending them to the server.
			`"_identityFilter":"","_snippetFilter":"","_tunnelFilter":"","_notebookFilter":"","_tunnelState":"all","_logKind":"all"}`,
		q, lf, gf, locked, setup,
	)
}

// filterShow is a data-show expression for a page row: shown while the
// page's filter signal is empty or the row's data-filter-text contains it.
func filterShow(signal string) string {
	return "!$" + signal + ".trim() || el.dataset.filterText.toLowerCase().includes($" + signal + ".trim().toLowerCase())"
}

func hostSubtitle(h Host) string {
	if h.AuthType == "local" {
		return "this machine"
	}
	user := h.Username
	if user == "" {
		user = "user"
	}
	addr := h.Address
	if h.Port > 0 && h.Port != 22 {
		addr = fmt.Sprintf("%s:%d", addr, h.Port)
	}
	return user + "@" + addr
}

func hostProtocol(h Host) string {
	if h.AuthType == "local" {
		return "local"
	}
	return "ssh"
}

func hostCountLabel(n int) string {
	if n == 1 {
		return "1 Host"
	}
	return fmt.Sprintf("%d Hosts", n)
}

func folderGroups(groups []hostGroup) []hostGroup {
	out := make([]hostGroup, 0)
	for _, g := range groups {
		if g.Name != "" && g.Name != "Hosts" {
			out = append(out, g)
		}
	}
	return out
}

func flatHosts(groups []hostGroup) []Host {
	n := 0
	for _, g := range groups {
		n += len(g.Hosts)
	}
	out := make([]Host, 0, n)
	for _, g := range groups {
		out = append(out, g.Hosts...)
	}
	return out
}

func groupHosts(hosts []Host, query string) []hostGroup {
	q := strings.ToLower(strings.TrimSpace(query))
	order := make([]string, 0)
	by := map[string][]Host{}
	for _, h := range hosts {
		if q != "" {
			blob := strings.ToLower(h.Name + " " + h.Address + " " + h.Username + " " + h.Group + " " + strings.Join(h.Tags, " "))
			if !strings.Contains(blob, q) {
				continue
			}
		}
		g := h.Group
		if g == "" {
			g = "Hosts"
		}
		if _, ok := by[g]; !ok {
			order = append(order, g)
		}
		by[g] = append(by[g], h)
	}
	out := make([]hostGroup, 0, len(order))
	for _, g := range order {
		out = append(out, hostGroup{Name: g, Hosts: by[g]})
	}
	return out
}

type hostGroup struct {
	Name  string
	Hosts []Host
}

// groupNode is one Termius-style folder in the vault tree.
type groupNode struct {
	Group store.Group
	Hosts []Host
	Kids  []groupNode
	Count int
	Depth int
}

func nestGroupTree(groups []store.Group, hosts []Host, query, parentID string) []groupNode {
	byID := map[string]store.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	byParent := map[string][]store.Group{}
	for _, g := range groups {
		parent := g.ParentID
		if parent != "" {
			if _, ok := byID[parent]; !ok {
				parent = ""
			}
		}
		byParent[parent] = append(byParent[parent], g)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	hostsByGID := map[string][]Host{}
	for _, h := range hosts {
		if q != "" && !hostMatchesGroupQuery(h, byID, q) {
			continue
		}
		if h.GroupID == "" {
			continue
		}
		hostsByGID[h.GroupID] = append(hostsByGID[h.GroupID], h)
	}
	var build func(parentID string, depth int) []groupNode
	build = func(parentID string, depth int) []groupNode {
		kids := append([]store.Group(nil), byParent[parentID]...)
		sort.SliceStable(kids, func(i, j int) bool {
			return strings.ToLower(kids[i].Name) < strings.ToLower(kids[j].Name)
		})
		out := make([]groupNode, 0, len(kids))
		for _, g := range kids {
			n := groupNode{
				Group: g,
				Hosts: hostsByGID[g.ID],
				Kids:  build(g.ID, depth+1),
				Depth: depth,
			}
			n.Count = len(n.Hosts)
			for _, k := range n.Kids {
				n.Count += k.Count
			}
			if q != "" && n.Count == 0 && !strings.Contains(strings.ToLower(g.Name), q) {
				continue
			}
			out = append(out, n)
		}
		return out
	}
	return build(strings.TrimSpace(parentID), 1)
}

func subtreeGroupIDs(groups []store.Group, root string) map[string]struct{} {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	kids := map[string][]string{}
	for _, g := range groups {
		kids[g.ParentID] = append(kids[g.ParentID], g.ID)
	}
	out := map[string]struct{}{root: {}}
	var walk func(id string)
	walk = func(id string) {
		for _, child := range kids[id] {
			if _, ok := out[child]; ok {
				continue
			}
			out[child] = struct{}{}
			walk(child)
		}
	}
	walk(root)
	return out
}

// hostsInCurrentAndSubgroups is the Termius Hosts list: current group plus
// every nested subgroup. An empty focus shows every matching host (root).
// Tags stay in the query blob and are not group membership.
func hostsInCurrentAndSubgroups(groups []store.Group, hosts []Host, focus, query string) []Host {
	byID := map[string]store.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	q := strings.ToLower(strings.TrimSpace(query))
	allow := subtreeGroupIDs(groups, focus)
	out := make([]Host, 0)
	for _, h := range hosts {
		if allow != nil {
			if _, ok := allow[h.GroupID]; !ok {
				continue
			}
		}
		if q != "" && !hostMatchesGroupQuery(h, byID, q) {
			continue
		}
		out = append(out, h)
	}
	return out
}

func groupFocusPath(groups []store.Group, id string) []store.Group {
	byID := map[string]store.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	var path []store.Group
	seen := map[string]struct{}{}
	cur := strings.TrimSpace(id)
	for cur != "" {
		if _, ok := seen[cur]; ok {
			break
		}
		seen[cur] = struct{}{}
		g, ok := byID[cur]
		if !ok {
			break
		}
		path = append([]store.Group{g}, path...)
		cur = g.ParentID
	}
	return path
}

func ungroupedHosts(hosts []Host, groups []store.Group, query string) []Host {
	byID := map[string]store.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Host, 0)
	for _, h := range hosts {
		// A host whose group is gone sits at the root of the tree, not
		// nowhere.
		if _, ok := byID[h.GroupID]; h.GroupID != "" && ok {
			continue
		}
		if q != "" && !hostMatchesGroupQuery(h, byID, q) {
			continue
		}
		out = append(out, h)
	}
	return out
}

func hostMatchesGroupQuery(h Host, byID map[string]store.Group, q string) bool {
	blob := strings.ToLower(h.Name + " " + h.Address + " " + h.Username + " " + h.Group + " " + strings.Join(h.Tags, " "))
	gid := h.GroupID
	seen := map[string]struct{}{}
	for gid != "" {
		if _, ok := seen[gid]; ok {
			break
		}
		seen[gid] = struct{}{}
		g, ok := byID[gid]
		if !ok {
			break
		}
		blob += " " + strings.ToLower(g.Name)
		gid = g.ParentID
	}
	return strings.Contains(blob, q)
}

func nestDepthAttr(depth int) string {
	return fmt.Sprintf("%d", depth)
}

func vaultTreeEmpty(tree []groupNode, hosts []Host) bool {
	return len(tree) == 0 && len(hosts) == 0
}

func filterLogs(logs []applog.Entry, query string) []applog.Entry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return logs
	}
	out := make([]applog.Entry, 0, len(logs))
	for _, e := range logs {
		if strings.Contains(strings.ToLower(e.Kind+" "+e.Message), q) {
			out = append(out, e)
		}
	}
	return out
}

func tunnelDest(t tunnel.Info) string {
	if t.Kind == "dynamic" {
		return "SOCKS5"
	}
	return fmt.Sprintf("%s:%d", t.DestHost, t.DestPort)
}

func tunnelListen(t tunnel.Info) string {
	if t.ListenAddr != "" {
		return t.ListenAddr
	}
	if t.BindPort == 0 {
		return t.BindAddr + ":auto"
	}
	return fmt.Sprintf("%s:%d", t.BindAddr, t.BindPort)
}

func hostNameByID(hosts []Host, id string) string {
	for _, h := range hosts {
		if h.ID == id {
			return h.Name
		}
	}
	return id
}

func snippetCommand(s store.Snippet) string {
	return strings.TrimSpace(s.Command)
}

func vaultNotebooks(nbs []store.Notebook) []store.Notebook {
	out := append([]store.Notebook(nil), nbs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func notebookPreview(nb store.Notebook) string {
	for _, cell := range nb.Cells {
		line := strings.TrimSpace(cell.Body)
		if line == "" {
			continue
		}
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line != "" {
			return line
		}
	}
	return "Empty notebook"
}

func logDateLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("Jan 2, 2006")
}

func logTimeLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("15:04:05")
}

// countLabel is a bare count for tree and list badges.
func countLabel(n int) string {
	return fmt.Sprintf("%d", n)
}

func identitiesOfKind(ids []store.Identity, kind string) []store.Identity {
	out := make([]store.Identity, 0, len(ids))
	for _, id := range ids {
		k := id.Kind
		if k != "key" {
			k = "password"
		}
		if k == kind {
			out = append(out, id)
		}
	}
	return out
}

// identityHostCount is how many saved hosts authenticate with id.
func identityHostCount(hosts []Host, id string) int {
	n := 0
	for _, h := range hosts {
		if h.AuthType == "identity" && h.IdentityID == id {
			n++
		}
	}
	return n
}

func identityKindLabel(kind string) string {
	if kind == "key" {
		return "SSH key"
	}
	return "Password"
}

// identitySubtitle is the list line under an identity: kind, user, hosts.
func identitySubtitle(hosts []Host, id store.Identity) string {
	parts := []string{identityKindLabel(id.Kind)}
	if id.Username != "" {
		parts = append(parts, id.Username)
	}
	n := identityHostCount(hosts, id.ID)
	if n == 1 {
		parts = append(parts, "1 host")
	} else {
		parts = append(parts, fmt.Sprintf("%d hosts", n))
	}
	return strings.Join(parts, " · ")
}

// tunnelFlag is the ssh flag a forward kind maps to.
func tunnelFlag(kind string) string {
	switch kind {
	case "remote":
		return "-R"
	case "dynamic":
		return "-D"
	default:
		return "-L"
	}
}

// tunnelStatus is how a forward's state reads in the table and sidebar:
// a dot and a word, never colour alone.
type tunnelStatus struct {
	Label      string
	DotClass   string
	StateClass string
	Filter     string // running | stopped
	On         bool
}

func tunnelState(t tunnel.Info) tunnelStatus {
	switch t.Status {
	case tunnel.StatusReady:
		return tunnelStatus{Label: "Running", DotClass: "k-dot--live", StateClass: "k-state--live", Filter: "running", On: true}
	case tunnel.StatusConnecting:
		return tunnelStatus{Label: "Connecting", DotClass: "k-dot--connecting", StateClass: "k-state--connecting", Filter: "running", On: true}
	case tunnel.StatusError:
		label := "Failed"
		if t.Error != "" {
			label = "Failed · " + t.Error
		}
		return tunnelStatus{Label: label, DotClass: "k-dot--dead", StateClass: "k-state--dead", Filter: "stopped"}
	default:
		return tunnelStatus{Label: "Stopped", DotClass: "k-dot--idle", Filter: "stopped"}
	}
}

// tunnelEveryInterface reports a forward that listens on every interface
// rather than on loopback only.
func tunnelEveryInterface(t tunnel.Info) bool {
	switch strings.TrimSpace(t.BindAddr) {
	case "0.0.0.0", "::", "[::]", "*":
		return true
	}
	return false
}

// snippetFirstLine is a snippet's first command, with an ellipsis when
// more lines follow.
func snippetFirstLine(s store.Snippet) string {
	line := strings.TrimSpace(s.Command)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		return strings.TrimSpace(line[:i]) + " …"
	}
	return line
}

// logGroup is the Logs filter chip a log kind belongs to.
func logGroup(kind string) string {
	k := strings.ToLower(kind)
	if k == "agent" || strings.HasPrefix(k, "agent.") {
		// agent.read, agent.suggest, agent.insert, agent.type and agent
		// settings changes (the Agent chip on the Logs page).
		return "agent"
	}
	switch k {
	case "session":
		return "sessions"
	case "tunnel":
		return "forwards"
	case "vault", "host", "group", "identity", "snippet", "notebook":
		return "vault"
	default:
		return "other"
	}
}

// agentModels are the models Settings · Agent offers for the first model
// provider (the descriptor's list); agentDefaultModel is the one selected
// before the page loads the saved choice.
func agentModels() []string {
	for _, d := range provider.Descriptors() {
		if d.Kind == provider.KindModel && len(d.Models) > 0 {
			return d.Models
		}
	}
	return []string{store.DefaultAgentModel}
}

func agentDefaultModel() string { return store.DefaultAgentModel }

// logFacts lists the counts an agent entry carries (lines, bytes, how many
// prompt lines one entry stands for, redactions, where a suggestion came
// from, and how long the model and Jev took), as short labels in this
// order. The message already says it in words; these make it scannable.
// js/lib/vaultview.js logFacts is the same list for rows built in the
// browser. Entries from other kinds carry none of these fields.
func logFacts(e applog.Entry) []string {
	var out []string
	if e.Count > 1 {
		out = append(out, fmt.Sprintf("%d sent", e.Count))
	} else if e.Lines > 0 {
		out = append(out, fmt.Sprintf("%d %s", e.Lines, pluralWord(e.Lines, "line", "lines")))
	}
	if e.Bytes > 0 {
		out = append(out, fmt.Sprintf("%d %s", e.Bytes, pluralWord(e.Bytes, "byte", "bytes")))
	}
	if n := redactionTotal(e.Redactions); n > 0 {
		out = append(out, fmt.Sprintf("%d redacted", n))
	}
	if e.Source != "" {
		out = append(out, "via "+e.Source)
	}
	if e.ModelMs > 0 {
		out = append(out, fmt.Sprintf("model %d ms", e.ModelMs))
	}
	if e.JevMs > 0 {
		out = append(out, fmt.Sprintf("Jev %d ms", e.JevMs))
	}
	return out
}

// logFactsTitle spells out the redactions by kind for a tooltip.
func logFactsTitle(e applog.Entry) string {
	if redactionTotal(e.Redactions) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(e.Redactions))
	for k := range e.Redactions {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s %d", k, e.Redactions[k]))
	}
	return "Redacted: " + strings.Join(parts, ", ")
}

func redactionTotal(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// logBadgeClass marks failures with flare; every other kind stays neutral.
func logBadgeClass(kind string) string {
	switch strings.ToLower(kind) {
	case "error", "fail", "denied":
		return "k-badge--flare"
	default:
		return ""
	}
}

// logTarget names what a log entry is about: its forward or host, or a dash.
func logTarget(p PageProps, e applog.Entry) string {
	if e.TunnelID != "" {
		for _, t := range p.Tunnels {
			if t.ID == e.TunnelID {
				return t.Name
			}
		}
	}
	if e.HostID != "" {
		for _, h := range p.Hosts {
			if h.ID == e.HostID {
				return h.Name
			}
		}
	}
	return "—"
}

// KeychainName is the name the unlock and first-run screens use for the OS
// keychain that go-keyring talks to on goos.
func KeychainName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "the system keyring"
	}
}

// keelMark is the in-app mark (the favicon), used as supplied on the
// unlock and first-run screens.
const keelMark = "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'><rect width='32' height='32' rx='7.2' fill='%23ffffff'/><path fill='%23141414' transform='translate(4,5.5) scale(0.24)' d='m90.3 25.8-37.6-21.8c-1.5-0.9-3.3-1-4.9-0.2l-33.2 18.9c-1.5 0.8-1.5 3 0 3.8l36.6 21.4c1.7 0.9 1.7 3.3 0 4.2-0.7 0.5-1.6 0.4-2.4 0l-36.8-21.7c-1.5-0.9-3.2 0.2-3.2 1.8v40.1c0 1 0.6 2.1 1.5 2.7l38.1 21.4c1 0.6 2.2 0.7 3.2 0.1l38.6-21.4c0.9-0.6 1.3-1.6 1.3-2.7v-44.5c-0.1-0.8-0.4-1.5-1.2-2.1z'/></svg>"

// privateAttr is a notebook row's data-private value for the Private menu
// item: what the item turns it to is the opposite.
func privateAttr(private bool) string {
	if private {
		return "on"
	}
	return "off"
}
