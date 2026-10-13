package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/a-h/templ"

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
	Titlebar   bool
	Native     bool
	Name       string
	SignalURL  string
	Query      string
	LogFilter  string
	Hosts      []store.Host
	Identities []store.Identity
	Tunnels    []tunnel.Info
	Snippets   []store.Snippet
	Notebooks  []store.Notebook
	Groups     []store.Group
	GroupFocus string
	Logs       []applog.Entry
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
		`{"vault":"hosts","query":%s,"logFilter":%s,"groupFocus":%s,"locked":%s,"needsSetup":%s,"authType":"password","identKind":"password","tunnelKind":"local"}`,
		q, lf, gf, locked, setup,
	)
}

func hostSubtitle(h store.Host) string {
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

func hostProtocol(h store.Host) string {
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

func flatHosts(groups []hostGroup) []store.Host {
	n := 0
	for _, g := range groups {
		n += len(g.Hosts)
	}
	out := make([]store.Host, 0, n)
	for _, g := range groups {
		out = append(out, g.Hosts...)
	}
	return out
}

func groupHosts(hosts []store.Host, query string) []hostGroup {
	q := strings.ToLower(strings.TrimSpace(query))
	order := make([]string, 0)
	by := map[string][]store.Host{}
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
	Hosts []store.Host
}

// groupNode is one Termius-style folder in the vault tree.
type groupNode struct {
	Group store.Group
	Hosts []store.Host
	Kids  []groupNode
	Count int
	Depth int
}

func nestGroupTree(groups []store.Group, hosts []store.Host, query, parentID string) []groupNode {
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
	hostsByGID := map[string][]store.Host{}
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
func hostsInCurrentAndSubgroups(groups []store.Group, hosts []store.Host, focus, query string) []store.Host {
	byID := map[string]store.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	q := strings.ToLower(strings.TrimSpace(query))
	allow := subtreeGroupIDs(groups, focus)
	out := make([]store.Host, 0)
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

func ungroupedHosts(hosts []store.Host, groups []store.Group, query string) []store.Host {
	byID := map[string]store.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]store.Host, 0)
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

func hostMatchesGroupQuery(h store.Host, byID map[string]store.Group, q string) bool {
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

func vaultTreeEmpty(tree []groupNode, hosts []store.Host) bool {
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

func hostNameByID(hosts []store.Host, id string) string {
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
func identityHostCount(hosts []store.Host, id string) int {
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
func identitySubtitle(hosts []store.Host, id store.Identity) string {
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
	switch strings.ToLower(kind) {
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
