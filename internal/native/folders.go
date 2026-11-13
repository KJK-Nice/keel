package native

import (
	"slices"
	"sort"
	"strings"

	"keel/internal/limits"
	"keel/internal/native/keelclient"
)

// Folders (docs/ui-redesign.md §9 Group, §17): a vault folder is a keelclient.Group with a parent, a host points at one with
// its GroupID, and a folder's members are the hosts in it and in every folder below it. This file is the pure rules of
// cmd/keel/web/js/lib/groups.js (pinned there by test/groups.test.mjs and here by folders_test.go), the tree the sidebar
// draws (nestGroupTree in internal/ui/data.go) and the requests the folder menus make (term.js createGroup, renameGroup,
// deleteGroup, assignHostsGroup and the drop handlers). Every walk up a parent chain stops at a cycle, so bad data cannot
// hang the window. Nothing here talks to a session.

// maxGroupDepth is how many folders may nest, and the two refusals of a folder that would nest wrongly: keel serve's
// own (internal/limits).
const (
	maxGroupDepth = limits.MaxGroupDepth
	msgGroupCycle = limits.MsgGroupCycle
	msgGroupDepth = limits.MsgGroupDepth
)

func groupByID(groups []keelclient.Group, id string) (keelclient.Group, bool) {
	for _, g := range groups {
		if g.ID == id {
			return g, true
		}
	}
	return keelclient.Group{}, false
}

// ancestry is the folder and its parents, nearest first.
func ancestry(groups []keelclient.Group, id string) (out []keelclient.Group) {
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur]; {
		seen[cur] = true
		g, ok := groupByID(groups, cur)
		if !ok {
			break
		}
		out = append(out, g)
		cur = g.ParentID
	}
	return out
}

// groupDepth is 1 for a top-level folder, 0 for no folder.
func groupDepth(groups []keelclient.Group, id string) (depth int) {
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur]; {
		seen[cur] = true
		depth++
		g, _ := groupByID(groups, cur)
		cur = g.ParentID
	}
	return depth
}

// groupDescendantDepth is how many levels of folders sit below id.
func groupDescendantDepth(groups []keelclient.Group, id string) int {
	return descendantDepth(groups, id, map[string]bool{})
}

func descendantDepth(groups []keelclient.Group, id string, seen map[string]bool) int {
	if seen[id] {
		return 0
	}
	seen[id] = true
	max := 0
	for _, g := range groups {
		if g.ParentID == id {
			max = maxInt(max, 1+descendantDepth(groups, g.ID, seen))
		}
	}
	return max
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// isGroupUnder reports whether id is ancestorID or lies below it.
func isGroupUnder(groups []keelclient.Group, id, ancestorID string) bool {
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur]; {
		if cur == ancestorID {
			return true
		}
		seen[cur] = true
		g, _ := groupByID(groups, cur)
		cur = g.ParentID
	}
	return false
}

// subtreeGroupIDs is root and every folder below it; nil for no root.
func subtreeGroupIDs(groups []keelclient.Group, root string) map[string]bool {
	if root == "" {
		return nil
	}
	ids := map[string]bool{root: true}
	var walk func(id string)
	walk = func(id string) {
		for _, g := range groups {
			if g.ParentID == id && !ids[g.ID] {
				ids[g.ID] = true
				walk(g.ID)
			}
		}
	}
	walk(root)
	return ids
}

// hostSearchHay is the lower-cased text a host search matches: the host's own fields and the names of every folder above it.
func hostSearchHay(groups []keelclient.Group, h keelclient.Host) string {
	var path []string
	for _, g := range ancestry(groups, h.GroupID) {
		path = append(path, g.Name)
	}
	return strings.ToLower(h.Name + " " + h.Group + " " + strings.Join(path, " ") + " " + h.Address + " " + h.Username + " " + strings.Join(h.Tags, " "))
}

// hostsInSubtree is the hosts in focus's subtree (all hosts with no focus) that match query.
func hostsInSubtree(groups []keelclient.Group, hosts []keelclient.Host, focus, query string) (out []keelclient.Host) {
	q := strings.ToLower(strings.TrimSpace(query))
	allow := subtreeGroupIDs(groups, focus)
	for _, h := range hosts {
		if allow != nil && !allow[h.GroupID] {
			continue
		}
		if q == "" || strings.Contains(hostSearchHay(groups, h), q) {
			out = append(out, h)
		}
	}
	return out
}

// groupOptionLabel is "Parent / Child / Group".
func groupOptionLabel(groups []keelclient.Group, g keelclient.Group) string {
	var parts []string
	for _, a := range ancestry(groups, g.ID) {
		parts = append(parts, a.Name)
	}
	slices.Reverse(parts)
	return strings.Join(parts, " / ")
}

// groupOptionEntries lists folders for a parent picker, leaving out excludeID and everything under it (a folder cannot
// move into itself).
func groupOptionEntries(groups []keelclient.Group, excludeID string) []pickEntry {
	skip := subtreeGroupIDs(groups, excludeID)
	var out []pickEntry
	for _, g := range groups {
		if !skip[g.ID] {
			out = append(out, pickEntry{value: g.ID, label: groupOptionLabel(groups, g)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].label) < strings.ToLower(out[j].label) })
	return out
}

// ---- the tree ----

// folderNode is one folder of the sidebar tree: its hosts, its subfolders and how many hosts it holds in all.
type folderNode struct {
	g     keelclient.Group
	hosts []keelclient.Host
	kids  []folderNode
	count int
	depth int
}

// hostMatchesFolderQuery is internal/ui/data.go hostMatchesGroupQuery: the sidebar's filter reads the host's own fields,
// its tags and the names of the folders above it.
func hostMatchesFolderQuery(h keelclient.Host, byID map[string]keelclient.Group, q string) bool {
	blob := strings.ToLower(h.Name + " " + h.Address + " " + h.Username + " " + h.Group + " " + strings.Join(h.Tags, " "))
	seen := map[string]bool{}
	for gid := h.GroupID; gid != "" && !seen[gid]; {
		seen[gid] = true
		g, ok := byID[gid]
		if !ok {
			break
		}
		blob += " " + strings.ToLower(g.Name)
		gid = g.ParentID
	}
	return strings.Contains(blob, q)
}

func byHostName(hs []keelclient.Host) []keelclient.Host {
	out := slices.Clone(hs)
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// nestFolderTree is nestGroupTree: the folders by name, each with its hosts and subfolders. A folder with nothing in it
// that the filter did not name is left out while filtering.
func nestFolderTree(groups []keelclient.Group, hosts []keelclient.Host, query string) []folderNode {
	byID := map[string]keelclient.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	byParent := map[string][]keelclient.Group{}
	for _, g := range groups {
		parent := g.ParentID
		if _, ok := byID[parent]; parent != "" && !ok {
			parent = "" // a folder whose parent is gone sits at the root
		}
		byParent[parent] = append(byParent[parent], g)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	byGID := map[string][]keelclient.Host{}
	for _, h := range hosts {
		if h.GroupID == "" || (q != "" && !hostMatchesFolderQuery(h, byID, q)) {
			continue
		}
		byGID[h.GroupID] = append(byGID[h.GroupID], h)
	}
	var build func(parent string, depth int, seen map[string]bool) []folderNode
	build = func(parent string, depth int, seen map[string]bool) []folderNode {
		kids := slices.Clone(byParent[parent])
		sort.SliceStable(kids, func(i, j int) bool { return strings.ToLower(kids[i].Name) < strings.ToLower(kids[j].Name) })
		var out []folderNode
		for _, g := range kids {
			if seen[g.ID] {
				continue
			}
			sub := maps1(seen, g.ID)
			n := folderNode{g: g, hosts: byHostName(byGID[g.ID]), kids: build(g.ID, depth+1, sub), depth: depth}
			n.count = len(n.hosts)
			for _, k := range n.kids {
				n.count += k.count
			}
			if q != "" && n.count == 0 && !strings.Contains(strings.ToLower(g.Name), q) {
				continue
			}
			out = append(out, n)
		}
		return out
	}
	return build("", 1, map[string]bool{})
}

// maps1 is seen plus id, as a new set (a cycle in bad data ends the walk).
func maps1(seen map[string]bool, id string) map[string]bool {
	out := make(map[string]bool, len(seen)+1)
	for k := range seen {
		out[k] = true
	}
	out[id] = true
	return out
}

// looseHosts are the hosts in no folder (or in one that is gone): they sit at the root of the tree, before the folders.
func looseHosts(groups []keelclient.Group, hosts []keelclient.Host, query string) (out []keelclient.Host) {
	byID := map[string]keelclient.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, h := range hosts {
		if _, ok := byID[h.GroupID]; h.GroupID != "" && ok {
			continue
		}
		if q != "" && !hostMatchesFolderQuery(h, byID, q) {
			continue
		}
		out = append(out, h)
	}
	return byHostName(out)
}

// folderTreeEmpty is vaultTreeEmpty: nothing to draw.
func folderTreeEmpty(tree []folderNode, loose []keelclient.Host) bool {
	return len(tree) == 0 && len(loose) == 0
}

// ---- what a drop or a menu may do (term.js drop handler, createGroup) ----

// folderMoveRefusal says why folder id cannot go under dest ("" is the root): the app's own words, or "".
func folderMoveRefusal(groups []keelclient.Group, id, dest string) string {
	if id == "" || id == dest || isGroupUnder(groups, dest, id) {
		return msgGroupCycle
	}
	if dest != "" && groupDepth(groups, dest)+1+groupDescendantDepth(groups, id) > maxGroupDepth {
		return msgGroupDepth
	}
	return ""
}

// folderCreateRefusal says why a new folder cannot go under parent (term.js createGroup), or "".
func folderCreateRefusal(groups []keelclient.Group, parent string) string {
	if parent == "" {
		return ""
	}
	if _, ok := groupByID(groups, parent); !ok {
		return "Group not found"
	}
	if groupDepth(groups, parent) >= maxGroupDepth {
		return msgGroupDepth
	}
	return ""
}

// treeDrag is what a dragged row carries: the hosts of a host row, or the folder of a folder heading. One type for both,
// because an element takes values of one type only (ui.Drop): a folder or the root takes either.
type treeDrag struct {
	hosts  []string
	folder string
}
