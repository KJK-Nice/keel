package ansible

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// decode parses YAML into plain Go values, keeping a !unsafe tag visible as
// unsafe{...} so tests can assert both the tag and the exact string.
type unsafe struct{ S string }

func decode(t *testing.T, src string) any {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("YAML does not parse: %v\n%s", err, src)
	}
	return nodeValue(t, doc.Content[0])
}

func nodeValue(t *testing.T, n *yaml.Node) any {
	t.Helper()
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				t.Fatalf("key %q has tag %s", k.Value, k.Tag)
			}
			if _, dup := m[k.Value]; dup {
				t.Fatalf("duplicate key %q", k.Value)
			}
			m[k.Value] = nodeValue(t, n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		var s []any
		for _, c := range n.Content {
			s = append(s, nodeValue(t, c))
		}
		return s
	case yaml.ScalarNode:
		switch n.Tag {
		case "!unsafe":
			return unsafe{n.Value}
		case "!!str":
			if n.Style != yaml.DoubleQuotedStyle {
				t.Fatalf("string %q is not double-quoted", n.Value)
			}
			return n.Value
		case "!!int":
			v, err := strconv.Atoi(n.Value)
			if err != nil {
				t.Fatal(err)
			}
			return v
		case "!!bool":
			return n.Value == "true"
		}
		t.Fatalf("unexpected tag %s for %q", n.Tag, n.Value)
	}
	t.Fatalf("unexpected node kind %v", n.Kind)
	return nil
}

func path(t *testing.T, v any, keys ...string) any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("at %q: not a map: %#v", k, v)
		}
		v, ok = m[k]
		if !ok {
			t.Fatalf("missing key %q in %#v", k, m)
		}
	}
	return v
}

func endsWithOneNewline(t *testing.T, s string) {
	t.Helper()
	if !strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\n\n") {
		t.Errorf("output must end with exactly one newline: %q", s[max(0, len(s)-20):])
	}
}

// tricky strings that must survive YAML exactly.
var tricky = []string{
	"web 1", `say "hi"`, "a#b", "a;b", "a=b", "line\nbreak", "tab\there",
	"ls\u2028ps\u2029", "nel\u0085x", "bom\ufeff", "del\x7f", "c1\u009b",
	"emoji 🚀", "back\\slash", "{{ lookup('pipe','id') }}", "{% if x %}y{% endif %}",
	"{# c #}", ": colon", "- dash", "yes", "null", "1.30", "~", "'single'", "\x00nul",
}

func TestHostHasNoSecretField(t *testing.T) {
	want := []string{"Name", "Address", "Port", "User", "Auth", "Role", "Production", "Jump"}
	var got []string
	ht := reflect.TypeOf(Host{})
	for i := 0; i < ht.NumField(); i++ {
		got = append(got, ht.Field(i).Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Host fields = %v, want %v", got, want)
	}
	var jf []string
	jt := reflect.TypeOf(Jump{})
	for i := 0; i < jt.NumField(); i++ {
		jf = append(jf, jt.Field(i).Name)
	}
	if !reflect.DeepEqual(jf, []string{"Name", "Address", "Port", "User"}) {
		t.Fatalf("Jump fields = %v", jf)
	}
}

func TestGroupNames(t *testing.T) {
	cases := []struct{ in, want string }{
		{"web", "web"},
		{"Web Servers", "Web_Servers"},
		{"control-plane", "control_plane"},
		{"1st", "_1st"},
		{"", "keel_group"},
		{"all", "all_group"},
		{"ungrouped", "ungrouped_group"},
		{"🚀x", "_x"},
		{"a\nb", "a_b"},
		{"prod;rm -rf", "prod_rm__rf"},
	}
	for _, c := range cases {
		if got := groupBase(c.in); got != c.want {
			t.Errorf("groupBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNamesForCollisions(t *testing.T) {
	inv := Inventory{
		Group: "worker",
		Hosts: []Host{
			{Name: "n1", Role: "a.b"},
			{Name: "n1", Role: "a-b"},
			{Name: "n1", Role: "worker"},
			{Name: "web 1"},
			{Name: "-x"},
			{Name: ""},
			{Name: "n1_2"},
			{Name: strings.Repeat("a", 70)},
		},
	}
	n := NamesFor(inv)
	if n.Group != "worker" {
		t.Errorf("group = %q", n.Group)
	}
	wantRoles := map[string]string{"a-b": "a_b", "a.b": "a_b_2", "worker": "worker_2"}
	if !reflect.DeepEqual(n.Roles, wantRoles) {
		t.Errorf("roles = %v, want %v", n.Roles, wantRoles)
	}
	wantHosts := []string{"n1", "n1_2", "n1_3", "web_1", "h-x", "host", "n1_2_2", strings.Repeat("a", 64)}
	if !reflect.DeepEqual(n.Hosts, wantHosts) {
		t.Errorf("hosts = %v, want %v", n.Hosts, wantHosts)
	}
	// Deterministic.
	for range 5 {
		if !reflect.DeepEqual(NamesFor(inv), n) {
			t.Fatal("NamesFor is not deterministic")
		}
	}
}

func sampleInventory() Inventory {
	return Inventory{
		Group: "Prod web",
		Stamp: "2026-10-18",
		Hosts: []Host{
			{Name: "web-1", Address: "10.0.0.1", Port: 2222, User: "ops", Auth: "key", Role: "web", Production: true},
			{Name: "db 1", Address: "db.internal", Port: 22, User: "root", Auth: "password", Role: "db"},
			{Name: "local", Auth: "local", Address: "ignored", User: "me"},
			{Name: "behind", Address: "10.0.1.5", User: "ops", Auth: "identity", Role: "web",
				Jump: &Jump{Name: "bastion", Address: "10.0.0.254", Port: 22, User: "jump"}},
		},
		Vars: []Var{{"k8s_version", "1.30"}, {"cluster", "main"}},
	}
}

func TestINI(t *testing.T) {
	inv := sampleInventory()
	inv.UseUser = true
	inv.UseJump = true
	got := INI(inv)
	want := `# Prod web · exported by Keel · 2026-10-18
# Keel does not run Ansible. No passwords, keys or tokens are written here.

[Prod_web]
# web-1: Keel keeps this host's key in its vault and does not export it. Use your SSH agent, or set ansible_ssh_private_key_file to a key file you keep.
# web-1: production
web-1 ansible_host="10.0.0.1" ansible_port=2222 ansible_user="ops"
# db_1: this host signs in with a password kept in Keel's vault. Run ansible-playbook with --ask-pass, or use ansible-vault.
db_1 ansible_host="db.internal" ansible_user="root"
local ansible_connection=local
# behind: Keel keeps this host's key in its vault and does not export it. Use your SSH agent, or set ansible_ssh_private_key_file to a key file you keep.
behind ansible_host="10.0.1.5" ansible_user="ops" ansible_ssh_common_args="-o ProxyJump=jump@10.0.0.254:22"

[db]
db_1

[web]
web-1
behind

[Prod_web:children]
db
web

[Prod_web:vars]
k8s_version=1.30
cluster=main
`
	if got != want {
		t.Errorf("INI mismatch\n--- got\n%s\n--- want\n%s", got, want)
	}
	endsWithOneNewline(t, got)
}

func TestINIWithoutUserOrJump(t *testing.T) {
	got := INI(sampleInventory())
	if strings.Contains(got, "ansible_user") || strings.Contains(got, "ProxyJump") {
		t.Errorf("user or jump written without opt-in:\n%s", got)
	}
	if !strings.Contains(got, "# behind: connects through bastion; set ansible_ssh_common_args for it.\n") {
		t.Errorf("missing jump hint:\n%s", got)
	}
}

func TestINILeavesOutUnsafeValues(t *testing.T) {
	inv := Inventory{
		Group:   "g",
		UseUser: true,
		UseJump: true,
		Hosts: []Host{
			{Name: "a", Address: "10.0.0.1 extra=1", User: `o"ps`},
			{Name: "b", Address: "h#x", User: "ok", Jump: &Jump{Name: "j\nx", Address: "bad host", User: "u"}},
			{Name: "c", Address: "c.example", Jump: &Jump{Name: "j", Address: "-oProxyCommand=x"}},
		},
		Vars: []Var{
			{"spaced", "a b"},
			{"quoted", `x"y`},
			{"jinja", "{{ lookup('pipe','id') }}"},
			{"plain", "v1.2,3:4/5"},
			{"Bad-Name", "x"},
			{"plain", "dup"},
		},
	}
	got := INI(inv)
	for _, want := range []string{
		"# a: ansible_host left out: it has characters an INI inventory cannot hold safely; use the YAML inventory.\n",
		"# a: ansible_user left out: it has characters an INI inventory cannot hold safely; use the YAML inventory.\n",
		"\na\n",
		"# b: ansible_host left out:",
		`b ansible_user="ok"` + "\n",
		"# b: connects through j x; set ansible_ssh_common_args for it.\n",
		"# c: connects through j; set ansible_ssh_common_args for it.\n",
		"# spaced: left out: it has characters an INI inventory cannot hold safely; use the YAML inventory.\n",
		"# quoted: left out:",
		"# jinja: left out:",
		"plain=v1.2,3:4/5\n",
		"# Bad-Name: left out: not a valid Ansible variable name.\n",
		"# plain: left out: defined more than once; the first value is kept.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("INI missing %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"extra=1", `o"ps`, "lookup", "a b", "ProxyJump", "ProxyCommand", "dup"} {
		if strings.Contains(got, bad) {
			t.Errorf("INI holds unsafe %q:\n%s", bad, got)
		}
	}
	// No line other than comments, sections and host/var lines.
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*( [a-z_]+=("[^"]*"|[^ "]+))*$|^[a-z_]+=[^ ]*$`).MatchString(line) {
			t.Errorf("unexpected INI line %q", line)
		}
	}
}

func TestYAMLStructure(t *testing.T) {
	inv := sampleInventory()
	inv.UseUser = true
	inv.UseJump = true
	src := YAML(inv)
	endsWithOneNewline(t, src)
	if !strings.HasPrefix(src, "# Prod web · exported by Keel · 2026-10-18\n# Keel does not run Ansible.") {
		t.Errorf("header:\n%s", src)
	}
	got := decode(t, src)
	want := map[string]any{
		"all": map[string]any{
			"children": map[string]any{
				"Prod_web": map[string]any{
					"hosts": map[string]any{
						"web-1":  map[string]any{"ansible_host": "10.0.0.1", "ansible_port": 2222, "ansible_user": "ops"},
						"db_1":   map[string]any{"ansible_host": "db.internal", "ansible_user": "root"},
						"local":  map[string]any{"ansible_connection": "local"},
						"behind": map[string]any{"ansible_host": "10.0.1.5", "ansible_user": "ops", "ansible_ssh_common_args": "-o ProxyJump=jump@10.0.0.254:22"},
					},
					"vars": map[string]any{"k8s_version": "1.30", "cluster": "main"},
					"children": map[string]any{
						"db":  map[string]any{"hosts": map[string]any{"db_1": map[string]any{}}},
						"web": map[string]any{"hosts": map[string]any{"web-1": map[string]any{}, "behind": map[string]any{}}},
					},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("YAML structure\n got %#v\nwant %#v\n%s", got, want, src)
	}
	for _, c := range []string{
		"        # web-1: Keel keeps this host's key in its vault and does not export it.",
		"        # web-1: production\n",
		"        # db_1: this host signs in with a password kept in Keel's vault. Run ansible-playbook with --ask-pass, or use ansible-vault.\n",
	} {
		if !strings.Contains(src, c) {
			t.Errorf("YAML missing comment %q", c)
		}
	}
}

func TestYAMLRoundTripsTrickyStrings(t *testing.T) {
	inv := Inventory{Group: "g", UseUser: true}
	for i, s := range tricky {
		inv.Hosts = append(inv.Hosts, Host{Name: "h" + strconv.Itoa(i), Address: s, User: s})
		inv.Vars = append(inv.Vars, Var{"v" + strconv.Itoa(i), s})
	}
	src := YAML(inv)
	got := decode(t, src)
	hosts := path(t, got, "all", "children", "g", "hosts").(map[string]any)
	vars := path(t, got, "all", "children", "g", "vars").(map[string]any)
	for i, s := range tricky {
		var want any = s
		if hasJinja(s) {
			want = unsafe{s}
		}
		h := hosts["h"+strconv.Itoa(i)].(map[string]any)
		if h["ansible_host"] != want || h["ansible_user"] != want {
			t.Errorf("host %d: got %#v, want %#v", i, h, want)
		}
		if vars["v"+strconv.Itoa(i)] != want {
			t.Errorf("var %d: got %#v, want %#v", i, vars["v"+strconv.Itoa(i)], want)
		}
	}
	// YAML 1.1 line breaks and BOM never appear raw.
	for _, r := range []string{"\u2028", "\u2029", "\u0085", "\ufeff", "\x7f", "\x00", "\t"} {
		if strings.Contains(src, r) {
			t.Errorf("raw %q in YAML output", r)
		}
	}
}

func TestYQEscapes(t *testing.T) {
	cases := map[string]string{
		"plain":      `"plain"`,
		`a"b\c`:      `"a\"b\\c"`,
		"a\nb\tc":    `"a\nb\tc"`,
		"\u2028":     `"\u2028"`,
		"\u0085":     `"\u0085"`,
		"\ufeff":     `"\uFEFF"`,
		"\x01\x7f":   `"\u0001\u007F"`,
		"🚀":          `"🚀"`,
		"\U000E0001": `"\U000E0001"`,
	}
	for in, want := range cases {
		if got := yq(in); got != want {
			t.Errorf("yq(%q) = %s, want %s", in, got, want)
		}
	}
	if got := yv("{{ x }}"); got != `!unsafe "{{ x }}"` {
		t.Errorf("yv jinja = %s", got)
	}
}

func TestTrickyNamesInBothFormats(t *testing.T) {
	inv := Inventory{Group: "all", UseUser: true}
	for i, s := range tricky {
		inv.Hosts = append(inv.Hosts, Host{Name: s, Address: "10.0.0." + strconv.Itoa(i), Role: s, Production: true})
	}
	inv.Hosts = append(inv.Hosts, Host{Name: "web 1"}, Host{Name: "web 1"})
	n := NamesFor(inv)
	if n.Group != "all_group" {
		t.Errorf("group = %q", n.Group)
	}
	groupRE := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	seen := map[string]bool{n.Group: true}
	for role, g := range n.Roles {
		if !groupRE.MatchString(g) || g == "all" || g == "ungrouped" {
			t.Errorf("role %q -> bad group %q", role, g)
		}
		if seen[g] {
			t.Errorf("group %q used twice", g)
		}
		seen[g] = true
	}
	hs := map[string]bool{}
	for _, h := range n.Hosts {
		if !hostOK.MatchString(h) || hs[h] {
			t.Errorf("bad or duplicate hostname %q", h)
		}
		hs[h] = true
	}
	ini := INI(inv)
	for _, line := range strings.Split(ini, "\n") {
		if strings.ContainsAny(line, "\r\u2028\u2029\u0085") {
			t.Errorf("INI line holds a break: %q", line)
		}
	}
	got := decode(t, YAML(inv))
	children := path(t, got, "all", "children", "all_group", "children").(map[string]any)
	if len(children) != len(n.Roles) {
		t.Errorf("children = %d, want %d", len(children), len(n.Roles))
	}
}

func TestSecretHints(t *testing.T) {
	inv := Inventory{Group: "g", Hosts: []Host{
		{Name: "k", Address: "1.1.1.1", Auth: "key"},
		{Name: "i", Address: "1.1.1.2", Auth: "identity"},
		{Name: "p", Address: "1.1.1.3", Auth: "password"},
	}}
	for name, out := range map[string]string{"INI": INI(inv), "YAML": YAML(inv)} {
		for _, h := range []string{"k", "i"} {
			if !strings.Contains(out, "# "+h+": Keel keeps this host's key in its vault and does not export it.") {
				t.Errorf("%s: no key hint for %s", name, h)
			}
		}
		if !strings.Contains(out, "# p: this host signs in with a password kept in Keel's vault.") {
			t.Errorf("%s: no password hint", name)
		}
		for _, bad := range []string{"ansible_password", "ansible_ssh_pass", "private_key_file:", "private_key_file=", "become_pass"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s holds %q", name, bad)
			}
		}
	}
}

func TestCommentsAreOneLine(t *testing.T) {
	inv := Inventory{Group: "g\nall:\n  x", Stamp: "a\u2028b", UseJump: false, Hosts: []Host{
		{Name: "h", Address: "1.1.1.1", Jump: &Jump{Name: "evil\nall:\u0085x\u2029y"}},
	}}
	for _, out := range []string{INI(inv), YAML(inv)} {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "all:") && !strings.HasPrefix(line, "all:") {
				t.Errorf("comment text broke onto its own line: %q", line)
			}
		}
		if strings.ContainsAny(out, "\u2028\u2029\u0085") {
			t.Errorf("separator survived: %q", out)
		}
	}
	decode(t, YAML(inv))
}

func TestEmptyInventory(t *testing.T) {
	inv := Inventory{}
	ini := INI(inv)
	if !strings.Contains(ini, "[keel_group]\n") {
		t.Errorf("INI:\n%s", ini)
	}
	endsWithOneNewline(t, ini)
	got := decode(t, YAML(inv))
	if h := path(t, got, "all", "children", "keel_group", "hosts"); !reflect.DeepEqual(h, map[string]any{}) {
		t.Errorf("hosts = %#v", h)
	}
}

func TestConvertCommand(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"echo hi", "echo hi", true},
		{"kubectl get {{ns}}", "kubectl get {{ ns | quote }}", true},
		{"echo {{x|q}} {{y}}", "echo {{ x | quote }} {{ y | quote }}", true},
		{"echo {{ x }}", "{% raw %}echo {{ x }}{% endraw %}", true},
		{"docker ps --format '{{.Names}}'", "{% raw %}docker ps --format '{{.Names}}'{% endraw %}", true},
		{"docker ps --format '{{.Names}}' {{f}}", "{% raw %}docker ps --format '{{.Names}}' {% endraw %}{{ f | quote }}", true},
		{"{% if %}", "{% raw %}{% if %}{% endraw %}", true},
		{"echo {# x #}", "{% raw %}echo {# x #}{% endraw %}", true},
		{"{{{x}}}", "{% raw %}{{% endraw %}{{ x | quote }}{% raw %}}{% endraw %}", true},
		{"echo {{ lookup('pipe','id') }}", "{% raw %}echo {{ lookup('pipe','id') }}{% endraw %}", true},
		{"echo {% endraw %}{{ lookup('pipe','id') }}", "", false},
		{"echo endraw", "echo endraw", true},
		{"a\nb {{v}}", "a\nb {{ v | quote }}", true},
		{"kubectl get pods -o go-template='{{range .items}}{{.metadata.name}}{{end}}'",
			"{% raw %}kubectl get pods -o go-template='{{range .items}}{{.metadata.name}}{{end}}'{% endraw %}", true},
		{"echo {{end}} {{ns}} {{else}}", "{% raw %}echo {{end}} {% endraw %}{{ ns | quote }}{% raw %} {{else}}{% endraw %}", true},
		{"echo {{true|q}} {{nil}}", "echo {{ true | quote }}{% raw %} {{nil}}{% endraw %}", true},
		{"{{" + strings.Repeat("a", 33) + "}}", "{% raw %}{{" + strings.Repeat("a", 33) + "}}{% endraw %}", true},
	}
	for _, c := range cases {
		got, ok := convertCommand(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("convertCommand(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func playbookInv() Inventory {
	return Inventory{Group: "cluster", Hosts: []Host{
		{Name: "cp1", Address: "10.0.0.1", Role: "control-plane"},
		{Name: "w1", Address: "10.0.0.2", Role: "worker"},
	}}
}

func TestPlaybookRolling(t *testing.T) {
	pb := Playbook{Name: "Upgrade K8s!", Steps: []Step{
		{Title: "Check", Description: "Look first\nthen act", Command: "kubectl get nodes", ReadOnly: true, Role: "control-plane", OneHost: true},
		{Title: "Drain", Command: "kubectl drain {{node}} --ignore-daemonsets", Role: "worker",
			Captures: []Capture{{Name: "ver", Pattern: "v(\\d+)\n#x"}}},
		{Title: "Eyeball it"},
		{Title: "Ghost", Command: "true", Role: "ghost role"},
	}}
	src := PlaybookYAML(pb, playbookInv())
	endsWithOneNewline(t, src)
	for _, want := range []string{
		"# Upgrade K8s! · exported by Keel from a runbook. Review before use.\n",
		"# Keel does not run Ansible. Try a dry run first: ansible-playbook -i inventory.ini upgrade-k8s.yml --check\n",
		"# Look first then act\n- name: \"Step 1 · Check\"\n",
		"    # TODO: Keel captures ver from this step's output (pattern v(\\d+) #x). Ansible does not: set it with ansible.builtin.set_fact from keel_step_2.stdout, or pass -e ver=… .\n",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("playbook missing %q:\n%s", want, src)
		}
	}
	plays := decode(t, src).([]any)
	if len(plays) != 4 {
		t.Fatalf("plays = %d", len(plays))
	}
	want := []any{
		map[string]any{"name": "Step 1 · Check", "hosts": "control_plane", "gather_facts": false, "tasks": []any{
			map[string]any{"name": "Check", "ansible.builtin.shell": map[string]any{"cmd": "kubectl get nodes"}, "changed_when": false, "run_once": true},
		}},
		map[string]any{"name": "Step 2 · Drain", "hosts": "worker", "gather_facts": false, "serial": 1, "max_fail_percentage": 0, "tasks": []any{
			map[string]any{"name": "Drain", "ansible.builtin.shell": map[string]any{"cmd": "kubectl drain {{ node | quote }} --ignore-daemonsets"}, "register": "keel_step_2"},
		}},
		map[string]any{"name": "Step 3 · Eyeball it", "hosts": "cluster", "gather_facts": false, "tasks": []any{
			map[string]any{"name": "Eyeball it", "ansible.builtin.pause": map[string]any{"prompt": "Step 3: Eyeball it. Check it yourself, then press Enter."}, "run_once": true},
		}},
		map[string]any{"name": "Step 4 · Ghost", "hosts": "ghost_role", "gather_facts": false, "serial": 1, "max_fail_percentage": 0, "tasks": []any{
			map[string]any{"name": "Ghost", "ansible.builtin.shell": map[string]any{"cmd": "true"}},
		}},
	}
	if !reflect.DeepEqual(plays, want) {
		t.Errorf("plays\n got %#v\nwant %#v\n%s", plays, want, src)
	}
}

func TestPlaybookParallelAndUnknownMode(t *testing.T) {
	steps := []Step{{Title: "x", Command: "uptime"}}
	par := PlaybookYAML(Playbook{Name: "p", Mode: "parallel", Steps: steps}, playbookInv())
	if strings.Contains(par, "serial") || strings.Contains(par, "max_fail_percentage") {
		t.Errorf("parallel has serial:\n%s", par)
	}
	for _, mode := range []string{"", "rolling", "weird"} {
		out := PlaybookYAML(Playbook{Name: "p", Mode: mode, Steps: steps}, playbookInv())
		if !strings.Contains(out, "  serial: 1\n  max_fail_percentage: 0\n") {
			t.Errorf("mode %q is not rolling:\n%s", mode, out)
		}
	}
}

func TestPlaybookRoleNamesMatchInventory(t *testing.T) {
	inv := Inventory{Group: "worker", Hosts: []Host{{Name: "a", Role: "worker"}, {Name: "b", Role: "a-b"}}}
	n := NamesFor(inv)
	pb := Playbook{Steps: []Step{
		{Title: "1", Command: "x", Role: "worker"},
		{Title: "2", Command: "x", Role: "a.b"}, // no host has it: must not steal a_b
		{Title: "3", Command: "x", Role: "a-b"},
	}}
	plays := decode(t, PlaybookYAML(pb, inv)).([]any)
	hosts := func(i int) any { return plays[i].(map[string]any)["hosts"] }
	if hosts(0) != n.Roles["worker"] || hosts(0) != "worker_2" {
		t.Errorf("worker -> %v", hosts(0))
	}
	if hosts(2) != n.Roles["a-b"] || hosts(1) == hosts(2) {
		t.Errorf("a.b -> %v, a-b -> %v", hosts(1), hosts(2))
	}
	if !strings.Contains(INI(inv), "[worker_2]\n") {
		t.Error("inventory does not use worker_2")
	}
}

func TestPlaybookJinja(t *testing.T) {
	pb := Playbook{Name: "{{ lookup('pipe','id') }}", Steps: []Step{
		{Title: "{{ lookup('pipe','id') }}", Command: "docker ps --format '{{.Names}}' | grep {{svc|q}}"},
		{Title: "bad", Command: "echo '{% endraw %}{{ lookup(\"pipe\",\"id\") }}'"},
		{Title: "{% if 1 %}pause{% endif %}"},
	}}
	src := PlaybookYAML(pb, playbookInv())
	if !strings.HasPrefix(src, "# {{ lookup('pipe','id') }} · exported by Keel") {
		t.Errorf("header:\n%s", src)
	}
	if !strings.Contains(src, "--check\n") || !strings.Contains(src, "inventory.ini lookup-pipe-id.yml") {
		t.Errorf("slugged file name:\n%s", src)
	}
	plays := decode(t, src).([]any)
	p1 := plays[0].(map[string]any)
	if p1["name"] != (unsafe{"Step 1 · {{ lookup('pipe','id') }}"}) {
		t.Errorf("play name = %#v", p1["name"])
	}
	t1 := p1["tasks"].([]any)[0].(map[string]any)
	if t1["name"] != (unsafe{"{{ lookup('pipe','id') }}"}) {
		t.Errorf("task name = %#v", t1["name"])
	}
	cmd := t1["ansible.builtin.shell"].(map[string]any)["cmd"]
	if cmd != "{% raw %}docker ps --format '{{.Names}}' | grep {% endraw %}{{ svc | quote }}" {
		t.Errorf("cmd = %#v", cmd)
	}
	if strings.Contains(src, "cmd: !unsafe") {
		t.Error("cmd tagged !unsafe")
	}
	t2 := plays[1].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	if _, ok := t2["ansible.builtin.shell"]; ok {
		t.Errorf("endraw command emitted as shell: %#v", t2)
	}
	if t2["ansible.builtin.fail"].(map[string]any)["msg"] != "Step 2's command holds Jinja text Keel cannot escape. Copy it from Keel by hand." {
		t.Errorf("fail task = %#v", t2)
	}
	if strings.Contains(src, "lookup(\"pipe\"") {
		t.Error("endraw command leaked into the playbook")
	}
	t3 := plays[2].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	if _, ok := t3["ansible.builtin.pause"].(map[string]any)["prompt"].(unsafe); !ok {
		t.Errorf("pause prompt not !unsafe: %#v", t3)
	}
}

func TestPlaybookTrickyRoundTrip(t *testing.T) {
	var steps []Step
	for _, s := range tricky {
		steps = append(steps, Step{Title: s, Description: s, Command: "echo x", Captures: []Capture{{Name: "c", Pattern: s}}})
	}
	src := PlaybookYAML(Playbook{Name: "line\nbreak\u2028x", Steps: steps}, playbookInv())
	plays := decode(t, src).([]any)
	for i, s := range tricky {
		task := plays[i].(map[string]any)["tasks"].([]any)[0].(map[string]any)
		var want any = s
		if hasJinja(s) {
			want = unsafe{s}
		}
		if task["name"] != want {
			t.Errorf("title %d: %#v, want %#v", i, task["name"], want)
		}
	}
}

func TestPlaybookMultilineCommand(t *testing.T) {
	src := PlaybookYAML(Playbook{Steps: []Step{{Title: "m", Command: "set -e\necho \"{{a}}\"\n\techo 'b'"}}}, playbookInv())
	plays := decode(t, src).([]any)
	cmd := plays[0].(map[string]any)["tasks"].([]any)[0].(map[string]any)["ansible.builtin.shell"].(map[string]any)["cmd"]
	if cmd != "set -e\necho \"{{ a | quote }}\"\n\techo 'b'" {
		t.Errorf("cmd = %q", cmd)
	}
}

func TestPlaybookEmpty(t *testing.T) {
	src := PlaybookYAML(Playbook{}, Inventory{})
	if !strings.Contains(src, "-i inventory.ini runbook.yml --check\n[]\n") {
		t.Errorf("empty playbook:\n%s", src)
	}
	if v, ok := decode(t, src).([]any); !ok || len(v) != 0 {
		t.Errorf("decoded %#v", v)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Upgrade K8s!":                  "upgrade-k8s",
		"  --Hello__World--  ":          "hello-world",
		"":                              "runbook",
		"🚀🚀":                            "runbook",
		"a/../b":                        "a-b",
		"Ünïcode café":                  "n-code-caf",
		strings.Repeat("ab-", 40):       strings.TrimRight(strings.Repeat("ab-", 20), "-"),
		strings.Repeat("x", 59) + " yz": strings.Repeat("x", 59),
	}
	for in, want := range cases {
		got := Slug(in)
		if got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if len(got) > 60 || !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(got) {
			t.Errorf("Slug(%q) = %q is not a clean slug", in, got)
		}
	}
}

func TestDeterministic(t *testing.T) {
	inv := sampleInventory()
	inv.UseJump, inv.UseUser = true, true
	pb := Playbook{Name: "x", Steps: []Step{{Title: "a", Command: "b", Role: "zz"}, {Title: "c", Command: "d", Role: "aa"}}}
	a, b, c := INI(inv), YAML(inv), PlaybookYAML(pb, inv)
	for range 10 {
		if INI(inv) != a || YAML(inv) != b || PlaybookYAML(pb, inv) != c {
			t.Fatal("output differs between runs")
		}
	}
	for _, s := range []string{a, b, c} {
		endsWithOneNewline(t, s)
	}
}
