package native

import (
	"slices"
	"strings"
	"testing"
	"time"

	"keel/internal/native/keelclient"
	"keel/internal/runvars"
)

// The rules of cmd/keel/web/js/lib/grouprun.js (roles) and lib/groupvars.js, case for case from test/grouprun.test.mjs and
// test/groupvars.test.mjs. The group run's own rules are in grouprunlogic_test.go.

func TestGroupRolesCleanedAndCounted(t *testing.T) { // grouprun.test.mjs "roles: cleaned like keel serve keeps them, counted per role"
	if r, ok := cleanRole(" Control-Plane "); r != "control-plane" || !ok {
		t.Errorf("cleanRole = %q %v", r, ok)
	}
	if _, ok := cleanRole("two words"); ok {
		t.Error("two words are not a role")
	}
	if _, ok := cleanRole(""); !ok {
		t.Error("an empty role is no role, and fine")
	}
	members := []keelclient.GroupMember{{Role: "worker"}, {Role: "worker"}, {Role: "control-plane"}, {Role: ""}}
	want := []roleCount{{"worker", 2, false}, {"control-plane", 1, false}, {"no role", 1, true}}
	if got := roleCounts(members); !slices.Equal(got, want) {
		t.Errorf("roleCounts = %v, want %v", got, want)
	}
	if got := roleNames(members); !slices.Equal(got, []string{"control-plane", "worker"}) {
		t.Errorf("roleNames = %v", got)
	}
	yes := true
	hosts := []keelclient.Host{{ID: "a", Production: &yes}, {ID: "b", Production: &yes}}
	if got := groupLine([]keelclient.GroupMember{{HostID: "a"}, {HostID: "b"}}, hosts); got != "Folder · 2 hosts · production" {
		t.Errorf("groupLine = %q", got)
	}
	if got := groupLine([]keelclient.GroupMember{{HostID: "a"}, {HostID: "c"}}, hosts); got != "Folder · 2 hosts · 1 production" {
		t.Errorf("groupLine with one production host = %q", got)
	}
}

func TestGroupNotesAreApartFromTheUsersOwn(t *testing.T) { // "group notes are apart from the user's own notes"
	notes := []keelclient.Note{{ID: "1", Text: "mine"}, {ID: "2", GroupID: "g", Text: "group"}, {ID: "3", HostID: "h", Text: "host"}}
	ids := func(ns []keelclient.Note) (out []string) {
		for _, n := range ns {
			out = append(out, n.ID)
		}
		return out
	}
	if got := ids(userNotesOf(notes)); !slices.Equal(got, []string{"1"}) {
		t.Errorf("user notes %v", got)
	}
	if got := ids(groupNotesOf(notes, "g")); !slices.Equal(got, []string{"2"}) {
		t.Errorf("group notes %v", got)
	}
	if len(groupNotesOf(notes, "")) != 0 {
		t.Error("no group has no notes")
	}
	if got := traceText(keelclient.TraceStep{Status: "notes", NoteKind: "group", Host: "k8s", Notes: 3}); got != "Used group notes · k8s (3)" {
		t.Errorf("trace %q", got)
	}
}

func TestARunbookStepKeepsItsRoleAndOneHost(t *testing.T) { // "a runbook step keeps its role and one host"
	in := toRunbookInput(rbEdit{name: "r", steps: []rbEditStep{{title: "a", command: "x", role: " Worker ", oneHost: true}}})
	if in.Steps[0].Role != "worker" || !in.Steps[0].OneHost {
		t.Errorf("role %q oneHost %v", in.Steps[0].Role, in.Steps[0].OneHost)
	}
	if got := checkRunbookInput(in); got != "" {
		t.Errorf("check: %q", got)
	}
	bad := toRunbookInput(rbEdit{name: "r", steps: []rbEditStep{{title: "a", role: "two words"}}})
	if got := checkRunbookInput(bad); !strings.Contains(got, "role") {
		t.Errorf("a role of two words: %q", got)
	}
}

// ---- groupvars.test.mjs ----

func TestVarRefsLikeRunvarsNeverGoTemplatesJinjaOrReservedWords(t *testing.T) { // "refs: …"
	names := func(cmd string) (out []string) {
		for _, r := range runvars.Refs(cmd) {
			n := r.Name
			if r.Quoted {
				n += "|q"
			}
			out = append(out, n)
		}
		return out
	}
	for cmd, want := range map[string][]string{
		"kubeadm join --token {{join_token}} --hash {{ca_hash|q}}":                  {"join_token", "ca_hash|q"},
		"docker ps --format '{{.Names}}' {{ x }} {{json .}}":                        nil,
		"kubectl get po -o go-template='{{range .items}}{{.metadata.name}}{{end}}'": nil,
		"{{FOO}}": {"FOO"},
		"{{end}} {{else}} {{nil}} {{true}} {{false}} {{break}} {{continue}}": nil,
		"echo {{end|q}}": {"end|q"},
	} {
		if got := names(cmd); !slices.Equal(got, want) {
			t.Errorf("refs(%q) = %v, want %v", cmd, got, want)
		}
	}
	if !validVarName("join_token") {
		t.Error("join_token is a name")
	}
	for _, bad := range []string{"9x", "a-b", "end", "A", ""} {
		if validVarName(bad) {
			t.Errorf("%q is not a name", bad)
		}
	}
}

func TestCleanVarNamesAndOneLineValuesAsKeelServeKeepsThem(t *testing.T) { // "cleanVar: …"
	n, v, err := cleanVar(" API_VIP ", " 10.20.0.10 ")
	if n != "api_vip" || v != "10.20.0.10" || err != "" {
		t.Errorf("cleanVar = %q %q %q", n, v, err)
	}
	if _, _, err := cleanVar("9x", "a"); err != varNameHelp {
		t.Errorf("a bad name: %q", err)
	}
	// zero width space, line separator, right-to-left override, BOM: written as runes so that no invisible character is in this file
	for _, r := range []rune{0x200b, 0x2028, 0x202e, 0xfeff} {
		if _, _, err := cleanVar("v", "a"+string(r)+"b"); err == "" {
			t.Errorf("U+%04X was accepted in a value", r)
		}
	}
	for _, bad := range []string{"a\nb", "a\tb", strings.Repeat("x", 257)} {
		if _, _, err := cleanVar("v", bad); err == "" {
			t.Errorf("%q was accepted as a value", bad)
		}
	}
	if _, _, err := cleanVar("v", strings.Repeat("x", 256)); err != "" {
		t.Errorf("256 bytes is the limit and fine: %q", err)
	}
}

func TestSplitVarsDrawsNamesAsChipsAndKeepsEveryOtherByte(t *testing.T) { // "bytesWithVars: …"
	segs := splitVars("\x15echo <b>{{tok}}\nnext {{x|q}}\n")
	var got []string
	for _, s := range segs {
		if s.isName {
			got = append(got, "["+s.text+"]")
		} else {
			got = append(got, s.text)
		}
	}
	if want := []string{"\x15echo <b>", "[{{tok}}]", "\nnext ", "[{{x|q}}]", "\n"}; !slices.Equal(got, want) {
		t.Errorf("segments %q, want %q", got, want)
	}
	if segs := splitVars("plain\n"); len(segs) != 1 || segs[0].isName || segs[0].text != "plain\n" {
		t.Errorf("plain text is one segment: %+v", segs)
	}
}

func TestUsesLineAndCapturesNamesOnlyCapturedValuesNeverShown(t *testing.T) { // "usesLine and captures: …"
	steps := []keelclient.GroupRunStep{
		{Captures: []keelclient.StepCapture{{Name: "join_token", Pattern: `--token\s+(\S+)`}}},
		{Vars: []keelclient.VarRef{{Name: "join_token"}, {Name: "k8s_version"}, {Name: "join_token", Quoted: true}}},
	}
	if got := usesLine(steps[1], steps, 1); got != "Uses 1 group variable and 1 captured value · filled in at Type" {
		t.Errorf("usesLine = %q", got)
	}
	if got := usesLine(keelclient.GroupRunStep{}, steps, 1); got != "" {
		t.Errorf("a step with no variables says %q", got)
	}
	if mask, text, state := captureView(&keelclient.CaptureView{Name: "t", State: "captured", Bytes: 23}); mask != "••••••••••••" || text != "23 bytes" || state != "captured" {
		t.Errorf("captured: %q %q %q", mask, text, state)
	}
	for state, want := range map[string]string{"blocked": "vault", "too-short": "too short to keep from the agent", "full": "Restart Keel to clear them"} {
		if _, text, _ := captureView(&keelclient.CaptureView{State: state}); !strings.Contains(text, want) {
			t.Errorf("%s says %q", state, text)
		}
	}
	if _, text, _ := captureView(nil); text != "not captured yet" {
		t.Errorf("nothing says %q", text)
	}
	if got := expiryLine(keelclient.GroupRunStep{}); !strings.Contains(got, "after 1 hour") {
		t.Errorf("expiryLine = %q", got)
	}
	at := time.Date(2026, 10, 18, 11, 14, 0, 0, time.Local)
	step := keelclient.GroupRunStep{Cells: []keelclient.GroupRunCell{{Captures: []keelclient.CaptureView{{State: "captured", ExpiresAt: &at}}}}}
	if got := expiryLine(step); !strings.Contains(got, "or at 11:14.") {
		t.Errorf("expiryLine = %q", got)
	}
	used := []keelclient.FillUsed{{Name: "a", From: "group variable"}, {Name: "b", Quoted: true, From: "captured on cp-1 · step 1"}}
	if got := usedLine(used); got != "{{a}} group variable, {{b|q}} captured on cp-1 · step 1" {
		t.Errorf("usedLine = %q", got)
	}
}

func TestRunbookCapturesInTheEditorsInput(t *testing.T) { // "runbook captures in the editor's input"
	in := toRunbookInput(rbEdit{name: "k8s", steps: []rbEditStep{{title: "init", command: "kubeadm init",
		captures: []keelclient.StepCapture{{Name: " Join_Token ", Pattern: ` --token\s+(\S+) `}, {}}}}})
	if got := in.Steps[0].Captures; !slices.Equal(got, []keelclient.StepCapture{{Name: "join_token", Pattern: `--token\s+(\S+)`}}) {
		t.Errorf("captures %v", got)
	}
	if got := checkRunbookInput(in); got != "" {
		t.Errorf("check: %q", got)
	}
	dup := toRunbookInput(rbEdit{name: "k", steps: []rbEditStep{
		{title: "a", captures: []keelclient.StepCapture{{Name: "x", Pattern: "(a)"}}},
		{title: "b", captures: []keelclient.StepCapture{{Name: "x", Pattern: "(b)"}}}}})
	if got := checkRunbookInput(dup); !strings.Contains(got, "captured by another step") {
		t.Errorf("a name taken: %q", got)
	}
	named := toRunbookInput(rbEdit{name: "k", steps: []rbEditStep{{title: "a", captures: []keelclient.StepCapture{{Name: "x"}}}}})
	if got := checkRunbookInput(named); !strings.Contains(got, "needs a pattern") {
		t.Errorf("a name with no pattern: %q", got)
	}
	if got := checkRunbookInput(toRunbookInput(rbEdit{name: "k", steps: []rbEditStep{{title: "a", captures: []keelclient.StepCapture{{Name: "9", Pattern: "(a)"}}}}})); !strings.Contains(got, "name") {
		t.Errorf("a bad name: %q", got)
	}
	var many []keelclient.StepCapture
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, keelclient.StepCapture{Name: n, Pattern: "(x)"})
	}
	if got := checkRunbookInput(toRunbookInput(rbEdit{name: "k", steps: []rbEditStep{{title: "a", captures: many}}})); !strings.Contains(got, "at most 4 captures") {
		t.Errorf("five captures: %q", got)
	}
}

func TestExportQueryAndTheDryRunHint(t *testing.T) { // "exportQuery and the dry-run hint"
	for _, c := range []struct {
		q    keelclient.AnsibleQuery
		want string
	}{
		{keelclient.AnsibleQuery{Format: "yaml", User: true}, "format=yaml"},
		{keelclient.AnsibleQuery{Format: "ini", RunbookID: "rb_1", Mode: "parallel", User: false, Jump: true}, "format=ini&runbookId=rb_1&mode=parallel&user=0&jump=1"},
		{keelclient.AnsibleQuery{User: true}, "format=ini"},
		{keelclient.AnsibleQuery{Format: "x", RunbookID: "r", Mode: "x", User: true}, "format=ini&runbookId=r&mode=rolling"},
	} {
		if got := c.q.Values(); got != c.want {
			t.Errorf("%+v = %q, want %q", c.q, got, c.want)
		}
	}
	files := []keelclient.ExportFile{{Kind: "inventory", Name: "inventory.ini"}, {Kind: "playbook", Name: "playbooks/x.yml"}}
	if got := dryRun(files); got != "ansible-playbook -i inventory.ini playbooks/x.yml --check" {
		t.Errorf("dryRun = %q", got)
	}
	if got := dryRun(files[:1]); got != "ansible-inventory -i inventory.ini --graph" {
		t.Errorf("dryRun of the inventory = %q", got)
	}
	if got := dryRun(nil); got != "" {
		t.Errorf("dryRun of nothing = %q", got)
	}
	if got := fileMeta(keelclient.ExportFile{Name: "inventory.yml", Lines: 1, Kind: "inventory"}); !strings.HasPrefix(got, "1 line · YAML") {
		t.Errorf("fileMeta = %q", got)
	}
}

func TestTraceCountsValuesWithheldApartFromRedactions(t *testing.T) { // "trace: values sent as {{name}} are counted apart from redactions"
	if got := traceText(keelclient.TraceStep{Status: "read", Host: "cp-1", Lines: 40, Redactions: map[string]int{"captured": 2}}); got != "Read cp-1 · last 40 lines · 2 values withheld by Keel" {
		t.Errorf("trace %q", got)
	}
	if got := traceText(keelclient.TraceStep{Status: "read", Host: "cp-1", Lines: 1, Redactions: map[string]int{"aws_key": 1, "variable": 1}}); got != "Read cp-1 · last 1 line · 1 redacted · 1 value withheld by Keel" {
		t.Errorf("trace %q", got)
	}
}
