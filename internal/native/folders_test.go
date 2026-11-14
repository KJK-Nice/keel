package native

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"keel/internal/native/keelclient"
)

// The folder rules, and the folder tree, menus, dialog
// and drops of the window.

// infra > db > replicas ; apps (the JS test's forest)
var jsGroups = []keelclient.Group{
	{ID: "infra", Name: "Infra"},
	{ID: "db", Name: "db", ParentID: "infra"},
	{ID: "rep", Name: "Replicas", ParentID: "db"},
	{ID: "apps", Name: "Apps"},
}

var jsHosts = []keelclient.Host{
	{ID: "h1", Name: "pg-1", Address: "10.0.0.1", Username: "root", GroupID: "db", Tags: []string{"postgres"}},
	{ID: "h2", Name: "pg-2", Address: "10.0.0.2", Username: "root", GroupID: "rep"},
	{ID: "h3", Name: "web", Address: "10.1.0.1", Username: "deploy", GroupID: "apps"},
	{ID: "h4", Name: "laptop", Address: "127.0.0.1", Username: "me"},
}

func hostIDs(hs []keelclient.Host) (out []string) {
	for _, h := range hs {
		out = append(out, h.ID)
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestFolderRulesDepthsAndAncestry(t *testing.T) {
	if got := groupDepth(jsGroups, "rep"); got != 3 {
		t.Errorf("groupDepth rep = %d", got)
	}
	if got := groupDepth(jsGroups, ""); got != 0 {
		t.Errorf("groupDepth of no folder = %d", got)
	}
	if got := groupDescendantDepth(jsGroups, "infra"); got != 2 {
		t.Errorf("groupDescendantDepth infra = %d", got)
	}
	if !isGroupUnder(jsGroups, "rep", "infra") {
		t.Error("rep is under infra")
	}
	if isGroupUnder(jsGroups, "apps", "infra") {
		t.Error("apps is not under infra")
	}
	if got := sortedKeys(subtreeGroupIDs(jsGroups, "db")); !slices.Equal(got, []string{"db", "rep"}) {
		t.Errorf("subtree of db %v", got)
	}
	if subtreeGroupIDs(jsGroups, "") != nil {
		t.Error("no root has no subtree")
	}
}

func TestFolderRulesCyclesDoNotHang(t *testing.T) { // "cycles in group data don't hang"
	loop := []keelclient.Group{{ID: "a", Name: "A", ParentID: "b"}, {ID: "b", Name: "B", ParentID: "a"}}
	if got := groupDepth(loop, "a"); got != 2 {
		t.Errorf("groupDepth in a loop = %d", got)
	}
	if isGroupUnder(loop, "a", "zzz") {
		t.Error("a loop is under nothing")
	}
	if got := groupOptionLabel(loop, loop[0]); got != "B / A" {
		t.Errorf("label %q", got)
	}
	// And the rest of the walks end too.
	groupDescendantDepth(loop, "a")
	subtreeGroupIDs(loop, "a")
	ancestry(loop, "a")
	nestFolderTree(loop, nil, "")
}

func TestFolderRulesSearchMatchesFoldersAboveTheHostAndTags(t *testing.T) { // "search matches group names above the host and tags"
	if got := hostSearchHay(jsGroups, jsHosts[1]); !strings.Contains(got, "replicas db infra") {
		t.Errorf("hay %q", got)
	}
	for _, c := range []struct {
		focus, query string
		want         []string
	}{
		{"infra", "", []string{"h1", "h2"}},
		{"", "postgres", []string{"h1"}},
		{"", "INFRA", []string{"h1", "h2"}},
	} {
		if got := hostIDs(hostsInSubtree(jsGroups, jsHosts, c.focus, c.query)); !slices.Equal(got, c.want) {
			t.Errorf("hostsInSubtree(%q, %q) = %v, want %v", c.focus, c.query, got, c.want)
		}
	}
}

func TestFolderRulesParentPickerLabelsPathsAndExcludesTheSubtree(t *testing.T) { // "parent picker labels paths and excludes …"
	got := groupOptionEntries(jsGroups, "db")
	want := []pickEntry{{value: "apps", label: "Apps"}, {value: "infra", label: "Infra"}}
	if !slices.Equal(got, want) {
		t.Errorf("entries %v, want %v", got, want)
	}
	found := false
	for _, o := range groupOptionEntries(jsGroups, "") {
		found = found || o.label == "Infra / db / Replicas"
	}
	if !found {
		t.Error("no entry labelled Infra / db / Replicas")
	}
}

func TestFolderRulesTagsAndSubtitles(t *testing.T) { // "tags and subtitles" (parseTags and hostSubtitle are the host dialog's and the palette's)
	if got := parseTags(" a, ,b ,c"); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("tags %v", got)
	}
	if got := hostSubtitle(keelclient.Host{Username: "u", Address: "h", Port: 2222, IdentityID: "i"}); got != "u@h:2222 · identity" {
		t.Errorf("subtitle %q", got)
	}
	if got := hostSubtitle(keelclient.Host{Username: "u", Address: "h", Port: 22}); got != "u@h" {
		t.Errorf("subtitle %q", got)
	}
	if got := hostSubtitle(keelclient.Host{AuthType: "local"}); got != "this machine" {
		t.Errorf("subtitle %q", got)
	}
}

func TestFolderRulesRefusals(t *testing.T) {
	// A folder cannot go under itself or under one of its own, and the nest stops at 8.
	for _, c := range []struct{ id, dest, want string }{
		{"infra", "infra", msgGroupCycle},
		{"infra", "rep", msgGroupCycle},
		{"rep", "infra", ""},
		{"rep", "", ""},
		{"apps", "rep", ""},
	} {
		if got := folderMoveRefusal(jsGroups, c.id, c.dest); got != c.want {
			t.Errorf("move %s -> %q: %q, want %q", c.id, c.dest, got, c.want)
		}
	}
	var deep []keelclient.Group
	parent := ""
	for i := 0; i < maxGroupDepth; i++ {
		id := string(rune('a' + i))
		deep = append(deep, keelclient.Group{ID: id, Name: id, ParentID: parent})
		parent = id
	}
	if folderCreateRefusal(deep, "h") != msgGroupDepth || folderCreateRefusal(deep, "g") != "" || folderCreateRefusal(deep, "zz") != "Group not found" {
		t.Error("the depth rule of New subgroup is not the app's")
	}
	if got := folderMoveRefusal(append(deep, jsGroups...), "infra", "h"); got != msgGroupDepth {
		t.Errorf("a move past 8 levels says %q", got)
	}
}

// ---- the window ----

func folderRig(t *testing.T) *rig {
	t.Helper()
	fb := okBackend()
	fb.groups = slices.Clone(jsGroups)
	fb.hosts = append(slices.Clone(jsHosts), keelclient.Host{ID: "h-local", Name: "Local Shell", AuthType: "local"})
	for i := range fb.hosts {
		fb.hosts[i].Group = fb.groupName(fb.hosts[i].GroupID)
	}
	r := newRig(t, fb)
	r.tt.Frame()
	return r
}

func (r *rig) menuOf(t *testing.T, label string) []string {
	t.Helper()
	if err := r.tt.RightClick(label); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	return slices.DeleteFunc(r.tt.Menu(), func(s string) bool { return s == "-" })
}

func (r *rig) pick(t *testing.T, item string) {
	t.Helper()
	if err := r.tt.ChooseMenuItem(item); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
}

func TestSidebarDrawsTheNestedTreeLooseHostsFirst(t *testing.T) {
	r := folderRig(t)
	if !r.w.hasFolders() {
		t.Fatal("the window did not read the folders")
	}
	for _, s := range []string{"laptop", "Local Shell", "folder Apps", "folder Infra", "folder db", "folder Replicas", "host pg-2", "host web"} {
		if _, ok := r.tt.Find(s); !ok && !r.tt.HasText(s) {
			t.Errorf("no %q in the tree: %v", s, r.tt.Texts())
		}
	}
	// Loose hosts are above the folders; a folder's hosts above its subfolders; folders by name.
	y := func(label string) float32 { rc, _ := r.tt.Find(label); return rc.Y }
	if !(y("host laptop") < y("folder Apps") && y("folder Apps") < y("folder Infra") && y("folder Infra") < y("folder db") && y("folder db") < y("host pg-1") && y("host pg-1") < y("folder Replicas") && y("folder Replicas") < y("host pg-2")) {
		t.Errorf("order wrong: laptop %v Apps %v Infra %v db %v pg-1 %v Replicas %v pg-2 %v", y("host laptop"), y("folder Apps"), y("folder Infra"), y("folder db"), y("host pg-1"), y("folder Replicas"), y("host pg-2"))
	}
	// A folder's count is every host in it and below it.
	nodes := nestFolderTree(r.w.folders, r.w.hosts, "")
	if len(nodes) != 2 || nodes[0].g.ID != "apps" || nodes[1].g.ID != "infra" || nodes[1].count != 2 || nodes[1].kids[0].count != 2 || nodes[1].kids[0].kids[0].count != 1 {
		t.Errorf("tree %+v", nodes)
	}
	// Clicking a folder collapses it, and again opens it.
	r.click(t, "folder Infra")
	if _, ok := r.tt.Find("host pg-1"); ok || !r.w.prefs.Collapsed["infra"] {
		t.Error("Infra did not collapse")
	}
	r.click(t, "folder Infra")
	if _, ok := r.tt.Find("host pg-1"); !ok {
		t.Error("Infra did not open again")
	}
}

// The closed folders are kept across a restart as groupCollapsed in keel-native.json; the file's other fields stay as they are.
func TestTheClosedFoldersComeBackAfterARestart(t *testing.T) {
	r := folderRig(t)
	r.w.useDataDir(t, t.TempDir())
	if err := savePrefs(r.w.dataDir, map[string]any{"termSize": 14}); err != nil {
		t.Fatal(err)
	}
	r.click(t, "folder Infra")
	r.click(t, "folder Apps")
	if got := loadPrefs(r.w.dataDir).Collapsed; len(got) != 2 || !got["infra"] || !got["apps"] {
		t.Fatalf("kept %v", got)
	}
	r.click(t, "folder Apps") // opened again: not kept
	if got := loadPrefs(r.w.dataDir).Collapsed; len(got) != 1 || !got["infra"] {
		t.Fatalf("kept after opening Apps: %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(r.w.dataDir, "keel-native.json")); !strings.Contains(string(b), `"termSize": 14`) {
		t.Errorf("another field was lost: %s", b)
	}
	// A new window on the same data folder shows Infra closed and the rest open.
	fb := r.fb
	w2 := newRig(t, fb)
	w2.w.useDataDir(t, r.w.dataDir)
	w2.w.prefs = loadPrefs(w2.w.dataDir) // what RunWith does
	w2.tt.Frame()
	if _, ok := w2.tt.Find("host pg-1"); ok {
		t.Error("Infra is open again after a restart")
	}
	if _, ok := w2.tt.Find("host laptop"); !ok {
		t.Error("a loose host is gone")
	}
	// No data folder (a test, or KEEL_DATA unset): nothing is written and nothing breaks.
	r2 := folderRig(t)
	r2.click(t, "folder Infra")
}

func TestSidebarFilterReadsFoldersAboveTheHost(t *testing.T) {
	r := folderRig(t)
	r.w.filter = "infra" // pg-1 and pg-2 are under Infra; their folders stay, the others go
	r.tt.Frame()
	if _, ok := r.tt.Find("host pg-2"); !ok {
		t.Error("pg-2 is under Infra: the filter must keep it")
	}
	if _, ok := r.tt.Find("host web"); ok {
		t.Error("web is not under Infra")
	}
	if _, ok := r.tt.Find("host laptop"); ok {
		t.Error("laptop is in no folder")
	}
	r.w.filter = "zzz"
	r.tt.Frame()
	if !r.tt.HasText("Nothing matches “zzz”") {
		t.Error("nothing matches: the tree says so")
	}
}

func TestFolderMenuItemsAndOrder(t *testing.T) {
	r := folderRig(t)
	want := []string{"Roles and runbooks", "Run a runbook…", "New host", "New subgroup", "Edit", "Rename", "Move into…", "Delete"}
	if got := r.menuOf(t, "folder Infra"); !slices.Equal(got, want) {
		t.Fatalf("menu %v, want %v", got, want)
	}
	r.tt.CloseMenu()
	// The more-host-actions menu has New group, between Join session and Local terminal.
	if err := r.tt.Click("More host actions"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if got := r.tt.Menu(); !slices.Equal(got, []string{"Quick connect", "Join session", "New group", "Local terminal"}) {
		t.Fatalf("more host actions %v", got)
	}
}

func TestNewGroupDialogSavesALabelAndAParent(t *testing.T) {
	r := folderRig(t)
	if err := r.tt.Click("More host actions"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.pick(t, "New group")
	d := r.w.folderDlg
	if d == nil || d.id != "" || !r.tt.HasText("New group") || !r.tt.HasText(folderDlgHint) {
		t.Fatalf("New group did not open its dialog: %v", r.tt.Texts())
	}
	// An empty label is the server's own word and no request.
	r.click(t, "Save group")
	if d.err != "name is required" || len(r.fb.fold.reqs) != 0 {
		t.Fatalf("empty label: err %q, requests %v", d.err, r.fb.fold.reqs)
	}
	r.fill(t, "Group label", "  Staging ")
	d.parent = "apps" // the Parent Group picker's choice
	r.click(t, "Save group")
	if want := []string{"post Staging apps"}; !slices.Equal(r.fb.fold.reqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.fold.reqs, want)
	}
	if r.w.folderDlg != nil || !r.tt.HasText(folderSavedToast) {
		t.Error("the dialog stays, or no Group saved toast")
	}
	if _, ok := r.tt.Find("folder Staging"); !ok {
		t.Error("the new folder is not in the tree")
	}
}

func TestParentPickerOffersNoFolderUnderItself(t *testing.T) {
	r := folderRig(t)
	r.menuOf(t, "folder Infra")
	r.pick(t, "Edit")
	d := r.w.folderDlg
	if d == nil || d.id != "infra" || d.name != "Infra" || !r.tt.HasText(folderEditTitle) {
		t.Fatalf("Edit opened %+v", d)
	}
	entries := groupOptionEntries(r.w.folders, d.id)
	for _, e := range entries {
		if strings.HasPrefix(e.label, "Infra") {
			t.Errorf("the picker offers %q for Infra", e.label)
		}
	}
	// Move into… is the same dialog.
	r.click(t, "Cancel")
	r.menuOf(t, "folder Replicas")
	r.pick(t, "Move into…")
	if r.w.folderDlg == nil || r.w.folderDlg.id != "rep" || r.w.folderDlg.parent != "db" {
		t.Fatalf("Move into… opened %+v", r.w.folderDlg)
	}
	r.w.folderDlg.parent = ""
	r.click(t, "Save group")
	if want := []string{"put rep Replicas "}; !slices.Equal(r.fb.fold.reqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.fold.reqs, want)
	}
}

func TestNewSubgroupAndNewHostStartInTheFolder(t *testing.T) {
	r := folderRig(t)
	r.menuOf(t, "folder Apps")
	r.pick(t, "New subgroup")
	if r.w.folderDlg == nil || r.w.folderDlg.parent != "apps" {
		t.Fatalf("New subgroup opened %+v", r.w.folderDlg)
	}
	r.click(t, "Cancel")
	r.menuOf(t, "folder Apps")
	r.pick(t, "New host")
	if d := r.w.hostDlg; d == nil || d.id != "" || d.f.groupID != "apps" {
		t.Fatalf("New host opened %+v", d)
	}
}

func TestSubgroupBelowTheDepthLimitIsRefused(t *testing.T) {
	r := folderRig(t)
	for i := 0; i < maxGroupDepth; i++ { // a chain eight deep under Apps' sibling
		parent := ""
		if i > 0 {
			parent = "deep" + string(rune('0'+i-1))
		}
		r.fb.groups = append(r.fb.groups, keelclient.Group{ID: "deep" + string(rune('0'+i)), Name: "deep" + string(rune('0'+i)), ParentID: parent})
	}
	r.w.reloadFolders()
	r.tt.Frame()
	r.menuOf(t, "folder deep7")
	r.pick(t, "New subgroup")
	if r.w.folderDlg != nil {
		t.Fatal("a ninth level opened a dialog")
	}
	r.tt.Frame()
	if _, ok := r.tt.Find("toast " + msgGroupDepth); !ok {
		t.Errorf("no refusal toast: %v", r.tt.Texts())
	}
}

func TestRenameFolderInTheTree(t *testing.T) {
	r := folderRig(t)
	r.menuOf(t, "folder Apps")
	r.pick(t, "Rename")
	r.tt.Frame()
	if r.w.folderRen == nil || r.w.folderRen.id != "apps" {
		t.Fatal("Rename did not turn the name into a field")
	}
	r.tt.Frame() // the field takes the keyboard
	r.tt.Key(ui.Cmd, ui.KeyA)
	r.tt.Type("Applications")
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if want := []string{"put apps Applications "}; !slices.Equal(r.fb.fold.reqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.fold.reqs, want)
	}
	if _, ok := r.tt.Find("folder Applications"); !ok {
		t.Error("the renamed folder is not in the tree")
	}
	// An empty name, Esc and an unchanged name leave it.
	r.fb.fold.reqs = nil
	r.w.renameFolder("apps")
	r.w.folderRen.text = "  "
	r.w.endFolderRename(true)
	r.w.renameFolder("apps")
	r.w.folderRen.text = "Nope"
	r.w.endFolderRename(false)
	r.w.renameFolder("apps")
	r.w.folderRen.text = "Applications"
	r.w.endFolderRename(true)
	if len(r.fb.fold.reqs) != 0 {
		t.Errorf("a name that was left sent %v", r.fb.fold.reqs)
	}
}

func TestDeleteFolderAsksWithTheAppsWordsAndHostsMoveUp(t *testing.T) {
	r := folderRig(t)
	r.menuOf(t, "folder db")
	r.pick(t, "Delete")
	if !r.hasText("Remove group db? Hosts and subgroups move up one level.") || !r.hasText("Confirm") || len(r.fb.fold.reqs) != 0 {
		t.Fatalf("no confirm, or a request before it: %v %v", r.tt.Texts(), r.fb.fold.reqs)
	}
	r.click(t, "Cancel")
	if len(r.fb.fold.reqs) != 0 {
		t.Fatalf("Cancel sent %v", r.fb.fold.reqs)
	}
	r.menuOf(t, "folder db")
	r.pick(t, "Delete")
	r.click(t, "Remove")
	if want := []string{"delete db"}; !slices.Equal(r.fb.fold.reqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.fold.reqs, want)
	}
	if !r.tt.HasText(folderGoneToast) {
		t.Error("no Group removed toast")
	}
	// The hosts and the subfolder moved up one level: pg-1 is under Infra now, Replicas is a subfolder of Infra.
	if _, ok := r.tt.Find("folder db"); ok {
		t.Error("db is still in the tree")
	}
	if g, _ := groupByID(r.w.folders, "rep"); g.ParentID != "infra" {
		t.Errorf("Replicas is under %q", g.ParentID)
	}
	for _, h := range r.w.hosts {
		if h.ID == "h1" && h.GroupID != "infra" {
			t.Errorf("pg-1 is in %q", h.GroupID)
		}
	}
}

func TestHostMenuMoveToFolderOpensEditHost(t *testing.T) {
	r := folderRig(t)
	r.menuOf(t, "host pg-1")
	r.pick(t, "Move to folder…")
	if d := r.w.hostDlg; d == nil || d.id != "h1" || d.f.groupID != "db" {
		t.Fatalf("Move to folder… opened %+v", d)
	}
}

// drag presses on one labelled element and lets go over another.
func (r *rig) drag(t *testing.T, from, to string) {
	t.Helper()
	a, ok := r.tt.Find(from)
	if !ok {
		t.Fatalf("no %q", from)
	}
	b, ok := r.tt.Find(to)
	if !ok {
		t.Fatalf("no %q", to)
	}
	r.tt.Press(a.X+a.W/2, a.Y+a.H/2)
	r.tt.Move(a.X+a.W/2+12, a.Y+a.H/2+12)
	r.tt.Move(b.X+b.W/2, b.Y+b.H/2)
	r.tt.Frame()
	r.tt.Release(b.X+b.W/2, b.Y+b.H/2)
	r.tt.Frame()
}

func TestDraggingAHostOntoAFolderMovesIt(t *testing.T) {
	r := folderRig(t)
	r.drag(t, "host laptop", "folder Apps")
	if want := []string{"move h4 -> apps"}; !slices.Equal(r.fb.fold.reqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.fold.reqs, want)
	}
	for _, h := range r.w.hosts {
		if h.ID == "h4" && h.GroupID != "apps" {
			t.Errorf("laptop is in %q", h.GroupID)
		}
	}
	// Dropped on the tree (not on a folder) it leaves its folder.
	r.fb.fold.reqs = nil
	r.drag(t, "host web", "host Local Shell")
	if want := []string{"move h3 -> "}; !slices.Equal(r.fb.fold.reqs, want) {
		t.Fatalf("a drop on the root: requests %v, want %v", r.fb.fold.reqs, want)
	}
}

func TestDraggingAFolderOntoAFolderNestsItAndTheRefusalsAreTheApps(t *testing.T) {
	r := folderRig(t)
	r.drag(t, "folder Apps", "folder Infra")
	if want := []string{"put apps Apps infra"}; !slices.Equal(r.fb.fold.reqs, want) {
		t.Fatalf("requests %v, want %v", r.fb.fold.reqs, want)
	}
	// A folder onto its own descendant, and onto itself: refused, no request.
	r.fb.fold.reqs = nil
	r.drag(t, "folder Infra", "folder Replicas")
	r.tt.Frame()
	if _, ok := r.tt.Find("toast " + msgGroupCycle); !ok || len(r.fb.fold.reqs) != 0 {
		t.Errorf("no refusal, or a request: %v %v", r.tt.Texts(), r.fb.fold.reqs)
	}
}

func TestFolderRequestsFailWithTheServersWords(t *testing.T) {
	r := folderRig(t)
	r.fb.fold.err = &keelclient.StatusError{Code: 400, Msg: "group nest is deeper than 8"}
	r.w.moveFolderTo("apps", "infra")
	r.tt.Frame()
	if _, ok := r.tt.Find("toast group nest is deeper than 8"); !ok {
		t.Errorf("no toast: %v", r.tt.Texts())
	}
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
