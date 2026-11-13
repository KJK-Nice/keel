package native

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"keel/internal/native/keelclient"
)

// Type without the approve dialog (docs/ui-redesign.md §6 rule 13): pure helpers, no window and no session. They are
// cmd/keel/web/js/lib/typedirect.js (pinned there by test/typedirect.test.mjs and here by typedirect_test.go).
//
// keel serve decides whether a Type goes without the dialog (internal/httpapi/typedirect.go, agent.DirectSafe). These
// only skip the round trip when the answer is certainly "dialog", and name the button and the badge. A hint that says
// direct can still meet the dialog; one that says no never reaches keel serve. Nothing here types.

// directLine reports whether command stays one line on a shell prompt (service.DirectLine).
func directLine(command string) bool { return keelclient.DirectLine(command) }

// The direct grammar as agent.DirectSafe has it (internal/agent/directsafe.go). Used only as a hint.
var (
	directExact = []string{"pwd", "whoami", "id", "date", "uptime", "hostname", "uname -a", "df -h", "free -h", "ps aux", "docker ps", "docker ps -a"}
	kubeKinds   = strings.Fields("pods nodes svc services deployments ns namespaces ingress pvc events jobs cronjobs statefulsets daemonsets replicasets")
	homeDirs    = []string{"/root", "/home", "/users", "/var/root", "/private/var/root"}
	secretWords = []string{"/proc", "environ", "/.ssh", "/.aws", "/.kube", "/.gnupg", "/.config/gh", "/.docker", "/.netrc", "/.git-credentials", "/etc/shadow", "/etc/gshadow", "/etc/sudoers",
		"id_", ".pem", ".key", ".p12", ".pfx", "credential", "token", "secret", "password", ".env", "kubeconfig", "shadow", "_history", ".pgpass", ".npmrc", ".pypirc", ".my.cnf", ".ssh"}

	pathRe = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,128}$`)
	nsRe   = regexp.MustCompile(`^[a-z0-9-]{1,63}$`)
	unitRe = regexp.MustCompile(`^[A-Za-z0-9@._][A-Za-z0-9@._-]{0,127}$`)
)

func safePath(p string) bool {
	if !pathRe.MatchString(p) || strings.HasPrefix(p, "-") || strings.Contains(p, "//") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	c := p
	if len(p) > 1 {
		c = strings.ToLower(strings.TrimSuffix(p, "/"))
	}
	for _, h := range homeDirs {
		if c == h || strings.HasPrefix(c, h+"/") {
			return false
		}
	}
	for _, s := range secretWords {
		if strings.Contains("/"+c, s) {
			return false
		}
	}
	return true
}

func kubeGet(w []string) bool {
	if len(w) < 2 || w[0] != "get" || !slices.Contains(kubeKinds, w[1]) {
		return false
	}
	ns, wide := false, false
	for i := 2; i < len(w); i++ {
		switch {
		case w[i] == "-n" && !ns && i+1 < len(w) && nsRe.MatchString(w[i+1]):
			ns = true
			i++
		case w[i] == "-A" && !ns:
			ns = true
		case w[i] == "-o" && !wide && i+1 < len(w) && w[i+1] == "wide":
			wide = true
			i++
		default:
			return false
		}
	}
	return true
}

// directHint: could keel serve type this line without the dialog? False means it certainly will not, and the window
// goes straight to the dialog.
func directHint(command string) bool {
	if command == "" || len(command) > 256 {
		return false
	}
	words := strings.Split(command, " ")
	for _, w := range words {
		if w == "" {
			return false
		}
		for i := 0; i < len(w); i++ {
			if w[i] < 0x21 || w[i] > 0x7e {
				return false
			}
		}
	}
	if slices.Contains(directExact, command) {
		return true
	}
	switch words[0] {
	case "ls":
		rest := words[1:]
		if len(rest) > 0 && slices.Contains([]string{"-l", "-la", "-lah"}, rest[0]) {
			rest = rest[1:]
		}
		return len(rest) == 0 || (len(rest) == 1 && safePath(rest[0]))
	case "kubectl":
		return kubeGet(words[1:])
	case "systemctl":
		return len(words) == 4 && words[1] == "status" && unitRe.MatchString(words[2]) && words[3] == "--no-pager"
	}
	return false
}

// snippetLine is what a direct type of a snippet sends: its command without the newlines that would run it.
func snippetLine(command string) string { return strings.TrimRight(command, crlf) }

// snippetBadge is "Approve first" only on a snippet that will certainly open the dialog: one outside the grammar.
func snippetBadge(command string) string {
	if directHint(snippetLine(command)) {
		return ""
	}
	return "Approve first"
}

// proposalDirect: an agent proposal that keel serve should type without the dialog, as far as the window knows.
func proposalDirect(p keelclient.Proposal) bool {
	return p.Risk == keelclient.RiskReadOnly && !p.NeedsShift && !p.Placeholders && !strings.Contains(p.Command, "{{") && directHint(p.Command)
}

// typeWord adds the ellipsis when a dialog will follow the press.
func typeWord(label string, direct bool) string {
	if direct {
		return label
	}
	return label + "…"
}

// typedNote is the line shown after a direct type.
func typedNote(target string) string {
	if target == "" {
		target = "the pane"
	}
	return "Typed into " + target + ". Press Enter to run."
}

// ---- what the pane's terminal shows ----

// The terminal plugin has no call for the alternate screen or the cursor's line. Its Snapshot is the screen as escape
// sequences: the modes that are on (ESC [ ? 1049 h and the like), then ESC [ H, the scrollback and the screen row by
// row joined with CR LF, then ESC [ row ; col H for the cursor and ESC [ 0 m. screenOf reads those two facts from it.

var (
	snapModes  = regexp.MustCompile("\x1b\\[\\?([0-9;]+)h")
	snapCursor = regexp.MustCompile("\x1b\\[([0-9]+);([0-9]+)H(?:\x1b\\[0m)?$")
	snapEscape = regexp.MustCompile("\x1b\\[[0-9;:?]*[A-Za-z]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[^\\[\\]]")
)

// crlf is CR and LF. A snapshot puts them between rows, and snippetLine cuts them off a snippet. Nothing here ever adds
// one to what is typed (guard_test.go).
var crlf = string([]byte{13, 10})

// promptLineMax is the longest line sent, as the page's cursorLine (lib/typedirect.js) cuts it.
const promptLineMax = 512

// screenOf reads the pane's alternate screen and cursor line from a terminal Snapshot, with rows the terminal's height.
// It never guesses "normal": a snapshot it cannot read (empty, no home sequence) is the alternate screen, which sends
// the Type to the dialog. The line is the one the cursor is on, or when the snapshot does not place it, the last line
// with text; keel serve checks its own view of the output as well and only ever adds a dialog.
func screenOf(snap string, rows int) (altScreen bool, promptLine string) {
	alt, lines, top, row := readSnapshot(snap, rows)
	if alt {
		return true, ""
	}
	line := ""
	if row > 0 && top+row-1 < len(lines) {
		line = lines[top+row-1]
	}
	if line == "" { // blank or unplaced: the last line with text is where a prompt would be
		for i := len(lines) - 1; i >= 0 && line == ""; i-- {
			line = lines[i]
		}
	}
	if r := []rune(line); len(r) > promptLineMax {
		line = string(r[:promptLineMax])
	}
	return false, line
}

// readSnapshot is the reading under screenOf: whether the alternate screen shows, the text of every row (escapes and
// trailing spaces off), the index in lines of the screen's first row, and the cursor's row and column on the screen
// (1-based; 0 when the snapshot does not place it).
func readSnapshot(snap string, rows int) (alt bool, lines []string, top, row int) {
	home := strings.Index(snap, "\x1b[H")
	if home < 0 {
		return true, nil, 0, 0
	}
	for _, m := range snapModes.FindAllStringSubmatch(snap[:home], -1) {
		for _, n := range strings.Split(m[1], ";") {
			if n == "47" || n == "1047" || n == "1049" {
				return true, nil, 0, 0
			}
		}
	}
	body := snap[home+len("\x1b[H"):]
	if m := snapCursor.FindStringSubmatchIndex(body); m != nil {
		row, _ = strconv.Atoi(body[m[2]:m[3]])
		body = body[:m[0]]
	}
	for _, l := range strings.Split(body, crlf) {
		lines = append(lines, strings.TrimRight(snapEscape.ReplaceAllString(l, ""), " "))
	}
	return false, lines, max(0, len(lines)-rows), row
}
