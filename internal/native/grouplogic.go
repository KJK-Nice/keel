package native

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"keel/internal/native/keelclient"
	"keel/internal/runvars"
)

// The pure rules of the Group page and of group variables and captures (docs/ui-redesign.md §17): the words and decisions
// of cmd/keel/web/js/lib/grouprun.js (roles) and lib/groupvars.js, pinned there by test/grouprun.test.mjs and
// test/groupvars.test.mjs and here by grouplogic_test.go. They mirror internal/runvars, which keel serve enforces; these only
// shape what the window shows and decide whether a request is worth sending. Nothing here talks to keel serve, draws or
// types, and nothing here ever holds a variable's value except the one the user is typing.

// cleanRole is a typed role as keel serve keeps it: trimmed, lower case. ok is false for anything that is not one word of
// letters, digits, -, _ or . (empty is ok: no role).
func cleanRole(text string) (role string, ok bool) {
	role = strings.ToLower(strings.TrimSpace(text))
	return role, role == "" || validRole(role)
}

// roleCount is one chip of the page's head: "3 worker".
type roleCount struct {
	role  string
	count int
	none  bool
}

// roleCounts is the head's chips: hosts per role, most first, then "no role" when some hosts have none.
func roleCounts(members []keelclient.GroupMember) []roleCount {
	counts := map[string]int{}
	none := 0
	for _, m := range members {
		if m.Role != "" {
			counts[m.Role]++
		} else {
			none++
		}
	}
	var out []roleCount
	for r, n := range counts {
		out = append(out, roleCount{role: r, count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return strings.ToLower(out[i].role) < strings.ToLower(out[j].role)
	})
	if none > 0 {
		out = append(out, roleCount{role: "no role", count: none, none: true})
	}
	return out
}

// roleNames are the roles used in the group, for the role menus.
func roleNames(members []keelclient.GroupMember) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range members {
		if m.Role != "" && !seen[m.Role] {
			seen[m.Role] = true
			out = append(out, m.Role)
		}
	}
	sort.Strings(out)
	return out
}

// groupLine is the page's sub line: "Folder · 8 hosts · production".
func groupLine(members []keelclient.GroupMember, hosts []keelclient.Host) string {
	n := len(members)
	prod := 0
	for _, m := range members {
		for _, h := range hosts {
			if h.ID == m.HostID && h.IsProduction() {
				prod++
				break
			}
		}
	}
	parts := []string{"Folder", countWord(n, "host", "hosts")}
	switch {
	case prod > 0 && prod == n:
		parts = append(parts, "production")
	case prod > 0:
		parts = append(parts, fmt.Sprintf("%d production", prod))
	}
	return strings.Join(parts, " · ")
}

// ---- group variables (lib/groupvars.js) ----

// The limits and the reference syntax are internal/runvars's.
const (
	maxCaptures   = runvars.MaxCapturesPerStep
	maxPattern    = runvars.MaxPatternBytes
	maxVarValue   = runvars.MaxGroupValueBytes
	varNameHelp   = "A name is lower-case letters, digits and _, not starting with a digit (for example api_vip)."
	varValueOneLn = "A value is one line of visible characters."
)

var varValueTooBig = fmt.Sprintf("A value is at most %d bytes.", maxVarValue)

// validVarName is a variable name as keel serve accepts one.
func validVarName(name string) bool { return runvars.ValidName(name) }

// badValueChar: a control, C1, zero-width, bidi, separator or BOM character (keel serve refuses them; runvars.CheckValue).
func badValueChar(v string) bool {
	for _, r := range v {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || (r >= 0x200b && r <= 0x200f) || (r >= 0x2028 && r <= 0x202e) || (r >= 0x2060 && r <= 0x2064) || r == 0xfeff {
			return true
		}
	}
	return false
}

// cleanVar is a typed variable as keel serve keeps it: the name trimmed and lower case, the value trimmed. err is the
// words to show, "" when it can be sent.
func cleanVar(name, value string) (n, v, err string) {
	n = strings.ToLower(strings.TrimSpace(name))
	v = strings.TrimSpace(value)
	switch {
	case !validVarName(n):
		return n, v, varNameHelp
	case badValueChar(v):
		return n, v, varValueOneLn
	case len(v) > maxVarValue:
		return n, v, varValueTooBig
	}
	return n, v, ""
}

// varSeg is a piece of a command: plain text, or a reference drawn as a chip.
type varSeg struct {
	text   string
	isName bool // text is "{{name}}" or "{{name|q}}"
}

// splitVars cuts bytes at every {{name}}: the references are drawn as chips and every other piece as the app's bytes.
func splitVars(bytes string) (out []varSeg) {
	last := 0
	for _, r := range runvars.Refs(bytes) {
		start, end := r.Span()
		if start > last {
			out = append(out, varSeg{text: bytes[last:start]})
		}
		out = append(out, varSeg{text: bytes[start:end], isName: true})
		last = end
	}
	if last < len(bytes) {
		out = append(out, varSeg{text: bytes[last:]})
	}
	return out
}

// capturedBefore: the names steps before index i capture.
func capturedBefore(steps []keelclient.GroupRunStep, i int) map[string]bool {
	out := map[string]bool{}
	for k := 0; k < i && k < len(steps); k++ {
		for _, c := range steps[k].Captures {
			out[c.Name] = true
		}
	}
	return out
}

// usesLine is the step card's note: "Uses 2 captured values · filled in at Type", "Uses 1 group variable and 1 captured
// value · filled in at Type", or "" when the command names none.
func usesLine(step keelclient.GroupRunStep, steps []keelclient.GroupRunStep, i int) string {
	if len(step.Vars) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var names []string
	for _, v := range step.Vars {
		if !seen[v.Name] {
			seen[v.Name] = true
			names = append(names, v.Name)
		}
	}
	cap := capturedBefore(steps, i)
	nCap := 0
	for _, n := range names {
		if cap[n] {
			nCap++
		}
	}
	nVar := len(names) - nCap
	var parts []string
	if nVar > 0 {
		parts = append(parts, countWord(nVar, "group variable", "group variables"))
	}
	if nCap > 0 {
		parts = append(parts, countWord(nCap, "captured value", "captured values"))
	}
	return "Uses " + strings.Join(parts, " and ") + " · filled in at Type"
}

var captureText = map[string]string{
	"captured":  "captured",
	"no-match":  "no match in the output",
	"blocked":   "not kept: it holds a value from the vault",
	"refused":   "not kept: a line break, control character or over 512 bytes",
	"too-short": "not kept: under 3 bytes, too short to keep from the agent",
	"full":      "not kept: Keel already withholds 1024 captured values from the agent. Restart Keel to clear them",
	"no-output": "not captured: the shell sent no command marks (OSC 133)",
	"expired":   "expired",
}

// captureMask is what stands where a captured value would: never the value.
const captureMask = "••••••••••••"

// captureView is one host's capture as words: never the value. A captured value shows as a mask and its length.
func captureView(cv *keelclient.CaptureView) (mask, text, state string) {
	if cv == nil {
		return "", "not captured yet", "waiting"
	}
	if cv.State == "captured" {
		return captureMask, countWord(cv.Bytes, "byte", "bytes"), "captured"
	}
	if t, ok := captureText[cv.State]; ok {
		return "", t, cv.State
	}
	return "", cv.State, cv.State
}

// expiryLine is "Kept in keel serve's memory for this run only. Deleted when the runbook ends, or at 11:14."
func expiryLine(step keelclient.GroupRunStep) string {
	var first *keelclient.CaptureView
	for _, c := range step.Cells {
		for i := range c.Captures {
			cv := &c.Captures[i]
			if cv.State != "captured" || cv.ExpiresAt == nil {
				continue
			}
			if first == nil || cv.ExpiresAt.Before(*first.ExpiresAt) {
				first = cv
			}
		}
	}
	at := "after 1 hour"
	if first != nil {
		at = "at " + clockHM(*first.ExpiresAt)
	}
	return "Kept in keel serve's memory for this run only. Deleted when the runbook ends, or " + at + "."
}

// usedLine lists what went into one host's bytes: "{{join_token}} captured on cp-1 · step 1, {{ver}} group variable".
func usedLine(used []keelclient.FillUsed) string {
	var parts []string
	for _, u := range used {
		q := ""
		if u.Quoted {
			q = "|q"
		}
		parts = append(parts, "{{"+u.Name+q+"}} "+u.From)
	}
	return strings.Join(parts, ", ")
}

// cleanCaptures is the editor's captures as keel serve takes them (empty rows dropped, names lower case).
func cleanCaptures(list []keelclient.StepCapture) []keelclient.StepCapture {
	out := []keelclient.StepCapture{}
	for _, c := range list {
		n := strings.ToLower(strings.TrimSpace(c.Name))
		p := strings.TrimSpace(c.Pattern)
		if n != "" || p != "" {
			out = append(out, keelclient.StepCapture{Name: n, Pattern: p})
		}
	}
	return out
}

// checkCaptures says what must change in the steps' captures, or "".
func checkCaptures(steps []keelclient.RunbookStepInput) string {
	seen := map[string]bool{}
	for i, st := range steps {
		if len(st.Captures) > maxCaptures {
			return fmt.Sprintf("Step %d: at most %d captures.", i+1, maxCaptures)
		}
		for _, c := range st.Captures {
			switch {
			case !validVarName(c.Name):
				return fmt.Sprintf("Step %d: a capture's name is lower-case letters, digits and _ (for example join_token).", i+1)
			case c.Pattern == "":
				return fmt.Sprintf("Step %d: %s needs a pattern with a group around the value, such as --token\\s+(\\S+).", i+1, c.Name)
			case utf8.RuneCountInString(c.Pattern) > maxPattern: // JS .length counts UTF-16 units; keel serve checks bytes
				return fmt.Sprintf("Step %d: a pattern is at most %d characters.", i+1, maxPattern)
			case seen[c.Name]:
				return fmt.Sprintf("Step %d: %s is captured by another step already.", i+1, c.Name)
			}
			seen[c.Name] = true
		}
	}
	return ""
}

// fileMeta is "32 lines · YAML" / "15 lines · INI".
func fileMeta(f keelclient.ExportFile) string {
	kind := "INI"
	if strings.HasSuffix(f.Name, ".yml") || strings.HasSuffix(f.Name, ".yaml") {
		kind = "YAML"
	}
	extra := ""
	if f.Kind == "inventory" {
		extra = " · from this folder's roles and variables"
	}
	return fmt.Sprintf("%s · %s%s", countWord(f.Lines, "line", "lines"), kind, extra)
}

// dryRun is the board's hint after an export.
func dryRun(files []keelclient.ExportFile) string {
	var inv, pb *keelclient.ExportFile
	for i := range files {
		switch files[i].Kind {
		case "inventory":
			if inv == nil {
				inv = &files[i]
			}
		case "playbook":
			if pb == nil {
				pb = &files[i]
			}
		}
	}
	switch {
	case inv == nil:
		return ""
	case pb != nil:
		return "ansible-playbook -i " + inv.Name + " " + pb.Name + " --check"
	}
	return "ansible-inventory -i " + inv.Name + " --graph"
}
