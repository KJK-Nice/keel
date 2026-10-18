// Package ansible renders an Ansible inventory (INI and YAML) and a playbook
// (YAML) from a Keel group and runbook. Keel never runs Ansible: the user
// downloads or copies these files. The package is pure: no I/O, no clock.
//
// Every user string is treated as hostile. Names are reduced to the
// characters Ansible accepts, YAML strings are always double-quoted with an
// escaper that also covers YAML 1.1 line breaks, strings holding Jinja
// delimiters are tagged !unsafe, INI values outside a conservative charset
// are left out with a comment, and no secret can reach an export because the
// input types have no field for one.
package ansible

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Host is one inventory host. It deliberately has NO field for passwords,
// keys or passphrases: secrets can never reach an export.
type Host struct {
	Name       string // Keel host name (any text; sanitized for Ansible)
	Address    string
	Port       int // 0 or 22: not written
	User       string
	Auth       string // "local" | "password" | "key" | "identity" | ""
	Role       string // "" = no role
	Production bool
	Jump       *Jump // nil = direct
}

// Jump is the jump host a Host connects through.
type Jump struct {
	Name    string
	Address string
	Port    int
	User    string
}

// Var is one group variable. Values are never secrets.
type Var struct{ Name, Value string }

// Inventory is a Keel group ready for export.
type Inventory struct {
	Group   string // the folder's name (any text)
	Hosts   []Host
	Vars    []Var
	UseUser bool   // write ansible_user from each host's saved user
	UseJump bool   // write the jump host as ansible_ssh_common_args (-o ProxyJump=user@addr:port)
	Stamp   string // a date line for the header comment, e.g. "2026-10-18"; may be empty
}

// Names maps Keel names to the Ansible names used in both files.
type Names struct {
	Group string            // the folder's Ansible group name
	Roles map[string]string // role -> Ansible group name
	Hosts []string          // Ansible inventory hostname per Inventory.Hosts index
}

var (
	hostOK    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	iniSafe   = regexp.MustCompile(`^[A-Za-z0-9._:@%+,/=-]+$`)
	varNameOK = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	jumpAddr  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:%-]*$`)
	jumpUser  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	refRE     = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]{0,31})(\|q)?\}\}`)
)

const maxHostname = 64

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// groupBase reduces s to a valid Ansible group name.
func groupBase(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isAlnum(r) || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	switch {
	case out == "":
		return "keel_group"
	case out[0] >= '0' && out[0] <= '9':
		out = "_" + out
	}
	if out == "all" || out == "ungrouped" {
		out += "_group"
	}
	return out
}

// hostBase reduces s to a safe inventory hostname.
func hostBase(s string) string {
	if len(s) <= maxHostname && hostOK.MatchString(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isAlnum(r) || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "host"
	}
	if !isAlnum(rune(out[0])) {
		out = "h" + out
	}
	if len(out) > maxHostname {
		out = out[:maxHostname]
	}
	return out
}

// unique returns base, or base_2, base_3… whichever is not in used, and
// marks it used.
func unique(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	used[name] = true
	return name
}

// NamesFor works out the Ansible names for a group, its roles and hosts.
// Roles are named in sorted order so the result is deterministic.
func NamesFor(inv Inventory) Names {
	n := Names{Group: groupBase(inv.Group), Roles: map[string]string{}, Hosts: make([]string, 0, len(inv.Hosts))}
	used := map[string]bool{n.Group: true}
	for _, role := range inventoryRoles(inv) {
		n.Roles[role] = unique(groupBase(role), used)
	}
	hostUsed := map[string]bool{}
	for _, h := range inv.Hosts {
		n.Hosts = append(n.Hosts, unique(hostBase(h.Name), hostUsed))
	}
	return n
}

// inventoryRoles lists the distinct non-empty roles of the hosts, sorted.
func inventoryRoles(inv Inventory) []string {
	seen := map[string]bool{}
	var roles []string
	for _, h := range inv.Hosts {
		if h.Role != "" && !seen[h.Role] {
			seen[h.Role] = true
			roles = append(roles, h.Role)
		}
	}
	sort.Strings(roles)
	return roles
}

// oneLine makes s safe for a single comment line: every rune that is not
// printable (controls, line and paragraph separators, format chars, odd
// spaces) becomes a space.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, s)
}

// hasJinja reports whether s holds a Jinja opening delimiter.
func hasJinja(s string) bool {
	return strings.Contains(s, "{{") || strings.Contains(s, "{%") || strings.Contains(s, "{#")
}

// yq writes s as a YAML double-quoted scalar that YAML 1.1 (PyYAML) and
// YAML 1.2 read back as exactly s.
func yq(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r == 0x2028, r == 0x2029, r == 0xfeff, !unicode.IsPrint(r):
			if r > 0xffff {
				fmt.Fprintf(&b, `\U%08X`, r)
			} else {
				fmt.Fprintf(&b, `\u%04X`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// yv is yq plus the !unsafe tag when Ansible would otherwise template s.
func yv(s string) string {
	if hasJinja(s) {
		return "!unsafe " + yq(s)
	}
	return yq(s)
}

func writeHeader(b *strings.Builder, inv Inventory, n Names) {
	group := oneLine(inv.Group)
	if strings.TrimSpace(group) == "" {
		group = n.Group
	}
	b.WriteString("# " + group + " · exported by Keel")
	if st := oneLine(inv.Stamp); strings.TrimSpace(st) != "" {
		b.WriteString(" · " + st)
	}
	b.WriteString("\n# Keel does not run Ansible. No passwords, keys or tokens are written here.\n")
}

func port(p int) int {
	if p <= 0 || p > 65535 || p == 22 {
		return 0
	}
	return p
}

// jumpArg composes the ProxyJump option for h, or "" when h has no jump
// host, the export does not use jumps, or the jump cannot be written safely
// (ssh_common_args is split by a shell lexer, so only plain names pass).
func jumpArg(inv Inventory, h Host) string {
	j := h.Jump
	if j == nil || !inv.UseJump || !jumpAddr.MatchString(j.Address) {
		return ""
	}
	if j.User != "" && !jumpUser.MatchString(j.User) {
		return ""
	}
	spec := j.Address
	if strings.Contains(spec, ":") {
		spec = "[" + spec + "]"
	}
	if j.User != "" {
		spec = j.User + "@" + spec
	}
	if j.Port > 0 && j.Port <= 65535 {
		spec += fmt.Sprintf(":%d", j.Port)
	}
	return "-o ProxyJump=" + spec
}

// hostHints are the comment lines written above a host: where its secret
// lives, production, and a jump host that is not written.
func hostHints(inv Inventory, h Host, name string) []string {
	var out []string
	switch h.Auth {
	case "key", "identity":
		out = append(out, name+": Keel keeps this host's key in its vault and does not export it. Use your SSH agent, or set ansible_ssh_private_key_file to a key file you keep.")
	case "password":
		out = append(out, name+": this host signs in with a password kept in Keel's vault. Run ansible-playbook with --ask-pass, or use ansible-vault.")
	}
	if h.Production {
		out = append(out, name+": production")
	}
	if h.Auth != "local" && h.Jump != nil && jumpArg(inv, h) == "" {
		jn := strings.TrimSpace(oneLine(h.Jump.Name))
		if jn == "" {
			jn = "a jump host"
		}
		out = append(out, name+": connects through "+jn+"; set ansible_ssh_common_args for it.")
	}
	return out
}

func leftOutINI(name, key string) string {
	return name + ": " + key + " left out: it has characters an INI inventory cannot hold safely; use the YAML inventory."
}

// usableVars splits the group vars into those written and comment lines for
// the rest (bad names, repeats).
func usableVars(vars []Var) (ok []Var, notes []string) {
	seen := map[string]bool{}
	for _, v := range vars {
		switch {
		case !varNameOK.MatchString(v.Name):
			notes = append(notes, oneLine(v.Name)+": left out: not a valid Ansible variable name.")
		case seen[v.Name]:
			notes = append(notes, v.Name+": left out: defined more than once; the first value is kept.")
		default:
			seen[v.Name] = true
			ok = append(ok, v)
		}
	}
	return ok, notes
}

func comment(b *strings.Builder, indent, text string) {
	b.WriteString(indent + "# " + oneLine(text) + "\n")
}

// INI renders the inventory in Ansible's INI format.
func INI(inv Inventory) string {
	n := NamesFor(inv)
	var sections []string

	var b strings.Builder
	writeHeader(&b, inv, n)
	sections = append(sections, b.String())

	b.Reset()
	b.WriteString("[" + n.Group + "]\n")
	for i, h := range inv.Hosts {
		name := n.Hosts[i]
		notes := hostHints(inv, h, name)
		line := name
		if h.Auth == "local" {
			line += " ansible_connection=local"
		} else {
			if h.Address != "" {
				if iniSafe.MatchString(h.Address) {
					line += ` ansible_host="` + h.Address + `"`
				} else {
					notes = append(notes, leftOutINI(name, "ansible_host"))
				}
			}
			if p := port(h.Port); p != 0 {
				line += fmt.Sprintf(" ansible_port=%d", p)
			}
			if inv.UseUser && h.User != "" {
				if iniSafe.MatchString(h.User) {
					line += ` ansible_user="` + h.User + `"`
				} else {
					notes = append(notes, leftOutINI(name, "ansible_user"))
				}
			}
			if arg := jumpArg(inv, h); arg != "" {
				line += ` ansible_ssh_common_args="` + arg + `"`
			}
		}
		for _, c := range notes {
			comment(&b, "", c)
		}
		b.WriteString(line + "\n")
	}
	sections = append(sections, b.String())

	roles := inventoryRoles(inv)
	for _, role := range roles {
		b.Reset()
		b.WriteString("[" + n.Roles[role] + "]\n")
		for i, h := range inv.Hosts {
			if h.Role == role {
				b.WriteString(n.Hosts[i] + "\n")
			}
		}
		sections = append(sections, b.String())
	}
	if len(roles) > 0 {
		b.Reset()
		b.WriteString("[" + n.Group + ":children]\n")
		for _, role := range roles {
			b.WriteString(n.Roles[role] + "\n")
		}
		sections = append(sections, b.String())
	}

	vars, notes := usableVars(inv.Vars)
	if len(vars) > 0 || len(notes) > 0 {
		b.Reset()
		b.WriteString("[" + n.Group + ":vars]\n")
		for _, c := range notes {
			comment(&b, "", c)
		}
		for _, v := range vars {
			if hasJinja(v.Value) || !iniSafe.MatchString(v.Value) {
				comment(&b, "", v.Name+": left out: it has characters an INI inventory cannot hold safely; use the YAML inventory.")
				continue
			}
			b.WriteString(v.Name + "=" + v.Value + "\n")
		}
		sections = append(sections, b.String())
	}
	return strings.Join(sections, "\n")
}

// YAML renders the inventory in Ansible's YAML format.
func YAML(inv Inventory) string {
	n := NamesFor(inv)
	var b strings.Builder
	writeHeader(&b, inv, n)
	b.WriteString("all:\n  children:\n")
	b.WriteString("    " + yq(n.Group) + ":\n")

	if len(inv.Hosts) == 0 {
		b.WriteString("      hosts: {}\n")
	} else {
		b.WriteString("      hosts:\n")
		for i, h := range inv.Hosts {
			name := n.Hosts[i]
			for _, c := range hostHints(inv, h, name) {
				comment(&b, "        ", c)
			}
			var kv []string
			if h.Auth == "local" {
				kv = append(kv, "ansible_connection: "+yq("local"))
			} else {
				if h.Address != "" {
					kv = append(kv, "ansible_host: "+yv(h.Address))
				}
				if p := port(h.Port); p != 0 {
					kv = append(kv, fmt.Sprintf("ansible_port: %d", p))
				}
				if inv.UseUser && h.User != "" {
					kv = append(kv, "ansible_user: "+yv(h.User))
				}
				if arg := jumpArg(inv, h); arg != "" {
					kv = append(kv, "ansible_ssh_common_args: "+yq(arg))
				}
			}
			if len(kv) == 0 {
				b.WriteString("        " + yq(name) + ": {}\n")
				continue
			}
			b.WriteString("        " + yq(name) + ":\n")
			for _, line := range kv {
				b.WriteString("          " + line + "\n")
			}
		}
	}

	vars, notes := usableVars(inv.Vars)
	if len(vars) > 0 {
		b.WriteString("      vars:\n")
		for _, c := range notes {
			comment(&b, "        ", c)
		}
		for _, v := range vars {
			b.WriteString("        " + yq(v.Name) + ": " + yv(v.Value) + "\n")
		}
	} else {
		for _, c := range notes {
			comment(&b, "      ", c)
		}
	}

	if roles := inventoryRoles(inv); len(roles) > 0 {
		b.WriteString("      children:\n")
		for _, role := range roles {
			b.WriteString("        " + yq(n.Roles[role]) + ":\n          hosts:\n")
			for i, h := range inv.Hosts {
				if h.Role == role {
					b.WriteString("            " + yq(n.Hosts[i]) + ": {}\n")
				}
			}
		}
	}
	return b.String()
}

// Capture is a value Keel extracts from a step's output at run time.
type Capture struct{ Name, Pattern string }

// Step is one runbook step.
type Step struct {
	Title       string
	Description string
	Command     string
	Role        string
	OneHost     bool
	ReadOnly    bool // Keel rated the command read-only
	Captures    []Capture
}

// Playbook is a runbook ready for export.
type Playbook struct {
	Name  string // runbook name (any text)
	Mode  string // "rolling" (default) | "parallel"
	Steps []Step
}

// stepRoles extends the inventory's role names with roles that steps target
// but no host has. Inventory names are fixed first so both files agree.
func stepRoles(n Names, pb Playbook) map[string]string {
	out := make(map[string]string, len(n.Roles))
	used := map[string]bool{n.Group: true}
	for role, name := range n.Roles {
		out[role] = name
		used[name] = true
	}
	var extra []string
	for _, s := range pb.Steps {
		if s.Role != "" {
			if _, ok := out[s.Role]; !ok {
				out[s.Role] = ""
				extra = append(extra, s.Role)
			}
		}
	}
	sort.Strings(extra)
	for _, role := range extra {
		out[role] = unique(groupBase(role), used)
	}
	return out
}

// needsRaw reports whether a literal piece of a command must be shielded
// from Jinja: it holds a delimiter, or would form one with a neighbouring
// {{ x | quote }}.
func needsRaw(s string) bool {
	for _, d := range []string{"{{", "{%", "{#", "}}", "%}", "#}"} {
		if strings.Contains(s, d) {
			return true
		}
	}
	return strings.HasPrefix(s, "}") || strings.HasSuffix(s, "{")
}

// goKeywords are Go template keywords. Written alone as {{end}} (no |q) they
// are template text, e.g. kubectl -o go-template='{{range .items}}…{{end}}',
// not Keel variable references.
var goKeywords = map[string]bool{
	"end": true, "else": true, "break": true, "continue": true,
	"nil": true, "true": true, "false": true,
}

// convertCommand turns Keel's {{name}} and {{name|q}} into
// {{ name | quote }} and wraps every other piece holding Jinja text in
// {% raw %}. It reports false when a piece cannot be wrapped (it holds
// "endraw").
func convertCommand(cmd string) (string, bool) {
	var b strings.Builder
	literal := func(s string) bool {
		if !needsRaw(s) {
			b.WriteString(s)
			return true
		}
		if strings.Contains(s, "endraw") {
			return false
		}
		b.WriteString("{% raw %}" + s + "{% endraw %}")
		return true
	}
	last := 0
	for _, m := range refRE.FindAllStringSubmatchIndex(cmd, -1) {
		if m[4] < 0 && goKeywords[cmd[m[2]:m[3]]] {
			continue // Go template text such as {{end}}: stays literal
		}
		if !literal(cmd[last:m[0]]) {
			return "", false
		}
		b.WriteString("{{ " + cmd[m[2]:m[3]] + " | quote }}")
		last = m[1]
	}
	if !literal(cmd[last:]) {
		return "", false
	}
	return b.String(), true
}

// PlaybookYAML renders the runbook as one play per step, in order, against
// the inventory's names (NamesFor(inv)).
func PlaybookYAML(pb Playbook, inv Inventory) string {
	n := NamesFor(inv)
	roles := stepRoles(n, pb)
	rolling := pb.Mode != "parallel"

	var b strings.Builder
	name := strings.TrimSpace(oneLine(pb.Name))
	if name == "" {
		name = "Runbook"
	}
	b.WriteString("# " + name + " · exported by Keel from a runbook. Review before use.\n")
	b.WriteString("# Keel does not run Ansible. Try a dry run first: ansible-playbook -i inventory.ini " + Slug(pb.Name) + ".yml --check\n")
	if len(pb.Steps) == 0 {
		b.WriteString("[]\n")
		return b.String()
	}

	for i, s := range pb.Steps {
		num := i + 1
		title := s.Title
		play := fmt.Sprintf("Step %d", num)
		if strings.TrimSpace(title) == "" {
			title = play
		} else {
			play += " · " + title
		}
		if d := strings.TrimSpace(oneLine(s.Description)); d != "" {
			b.WriteString("# " + d + "\n")
		}
		b.WriteString("- name: " + yv(play) + "\n")

		if strings.TrimSpace(s.Command) == "" {
			// A pause runs once per batch, so the play has no serial.
			b.WriteString("  hosts: " + yq(n.Group) + "\n")
			b.WriteString("  gather_facts: false\n")
			b.WriteString("  tasks:\n")
			b.WriteString("    - name: " + yv(title) + "\n")
			b.WriteString("      ansible.builtin.pause:\n")
			b.WriteString("        prompt: " + yv(fmt.Sprintf("Step %d: %s. Check it yourself, then press Enter.", num, title)) + "\n")
			b.WriteString("      run_once: true\n")
			continue
		}

		hosts := n.Group
		if s.Role != "" {
			hosts = roles[s.Role]
		}
		b.WriteString("  hosts: " + yq(hosts) + "\n")
		b.WriteString("  gather_facts: false\n")
		// run_once runs once per serial batch, so a one-host step has no serial.
		if rolling && !s.OneHost {
			b.WriteString("  serial: 1\n  max_fail_percentage: 0\n")
		}
		b.WriteString("  tasks:\n")
		b.WriteString("    - name: " + yv(title) + "\n")
		if cmd, ok := convertCommand(s.Command); ok {
			b.WriteString("      ansible.builtin.shell:\n")
			b.WriteString("        cmd: " + yq(cmd) + "\n")
			if len(s.Captures) > 0 {
				fmt.Fprintf(&b, "      register: \"keel_step_%d\"\n", num)
			}
			if s.ReadOnly {
				b.WriteString("      changed_when: false\n")
			}
		} else {
			b.WriteString("      ansible.builtin.fail:\n")
			b.WriteString("        msg: " + yq(fmt.Sprintf("Step %d's command holds Jinja text Keel cannot escape. Copy it from Keel by hand.", num)) + "\n")
		}
		if s.OneHost {
			b.WriteString("      run_once: true\n")
		}
		for _, c := range s.Captures {
			comment(&b, "    ", fmt.Sprintf("TODO: Keel captures %s from this step's output (pattern %s). Ansible does not: set it with ansible.builtin.set_fact from keel_step_%d.stdout, or pass -e %s=… .", c.Name, c.Pattern, num, c.Name))
		}
	}
	return b.String()
}

// Slug is a file-name-safe slug of a runbook name: lower-case [a-z0-9-],
// collapsed dashes, trimmed, at most 60 bytes, "runbook" when empty.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := b.String()
	if len(s) > 60 {
		s = s[:60]
	}
	s = strings.Trim(s, "-")
	if s == "" {
		return "runbook"
	}
	return s
}
