// Package limits holds the numbers, defaults and small validators that keel
// serve enforces and the window shows: how long a startup command may be,
// how deep folders nest, how long a notebook name is, what an agent setting
// defaults to. internal/store and the agent service export them under their
// own names; the window imports them here instead of keeping a copy. It is a
// leaf: it imports no keel package but wire (leaf_test.go checks).
package limits

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Hosts.
const (
	// DefaultPort is a host's SSH port when none is given.
	DefaultPort = 22
	// MaxPort is the highest TCP port.
	MaxPort = 65535
	// DefaultKeepAliveSec is the SSH keepalive interval when none is set.
	DefaultKeepAliveSec = 30
	// MaxKeepAliveSec is the longest keepalive interval the host form takes.
	MaxKeepAliveSec = 600
	// MaxStartupCommand is the longest startup command, in bytes.
	MaxStartupCommand = 1024
)

// StartupCommandProblem says why cmd is not a startup command, in the words
// the API answers with; "" when it is one: one line of printable text, no
// newline, carriage return, tab or other control character, at most
// MaxStartupCommand bytes. Keel adds the Enter itself.
func StartupCommandProblem(cmd string) string {
	if cmd == "" {
		return ""
	}
	if len(cmd) > MaxStartupCommand {
		return fmt.Sprintf("startup command is longer than %d bytes", MaxStartupCommand)
	}
	if !utf8.ValidString(cmd) {
		return "startup command is not valid text"
	}
	for _, r := range cmd {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return "startup command must be one line with no control characters"
		}
	}
	return ""
}

// HostColor is one swatch of the host label palette. Value is what a host
// stores in Color: a reference to an ANSI 9–14 token (docs/design/tokens.json),
// so the tile follows the theme instead of carrying a literal.
type HostColor struct {
	Name  string
	Value string
}

// HostColors is the host label palette, in picker order.
var HostColors = []HostColor{
	{"Green", "var(--ansi-10)"},
	{"Blue", "var(--ansi-12)"},
	{"Magenta", "var(--ansi-13)"},
	{"Yellow", "var(--ansi-11)"},
	{"Red", "var(--ansi-9)"},
	{"Cyan", "var(--ansi-14)"},
}

// Folders and roles.
const (
	// MaxGroupDepth is how many folders may nest.
	MaxGroupDepth = 8
	// MsgGroupCycle and MsgGroupDepth are the refusals of a folder that
	// would nest wrongly.
	MsgGroupCycle = "group cannot nest under itself"
	MsgGroupDepth = "group nest is deeper than 8"
	// MaxGroupRoles bounds the distinct roles of one group.
	MaxGroupRoles = 16
	// MaxRoleBytes is the longest role name.
	MaxRoleBytes = 32
)

var roleRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)

// ValidRole reports whether r, as it is, is one role word: no cleaning, and
// not empty.
func ValidRole(r string) bool { return roleRe.MatchString(r) }

// CleanRole lower-cases and trims a role name and reports whether it is
// valid. Empty is valid: no role.
func CleanRole(r string) (string, bool) {
	r = strings.ToLower(strings.TrimSpace(r))
	if r == "" {
		return "", true
	}
	return r, roleRe.MatchString(r)
}

// Notebooks and the agent's notes.
const (
	MaxNotebookNameBytes = 120
	MaxNotebookBytes     = 256 * 1024
	// MaxAttachedNotebooks is how many notebooks one question may carry.
	MaxAttachedNotebooks = 2
	// MaxAgentNoteBytes is the longest note for the agent.
	MaxAgentNoteBytes = 1000
)

// Runbooks.
const (
	MaxRunbooks            = 100
	MaxRunbookSteps        = 30
	MaxRunbookNameBytes    = 120
	MaxRunbookWhenBytes    = 300
	MaxRunbookTitleBytes   = 200
	MaxRunbookTextBytes    = 1000
	MaxRunbookExpectBytes  = 300
	MaxRunbookCommandBytes = 2048
	MaxRunbookCommandLines = 20
	MaxRunbookHosts        = 12
	MaxRunbookHostBytes    = 64
)

// Agent settings: the choices and what a fresh install has.
const (
	DetectionAuto     = "auto"      // a classifier notices plain language: opt in, each typing pause sends the line to Jev
	DetectionHashOnly = "hash-only" // only lines starting with #, or the user's own request (the default)

	SuggestionsOn  = "on"  // plain-language suggestions in panes
	SuggestionsOff = "off" // no notice, no hint, no detection call

	ScopeFocused = "focused" // the focused pane only
	ScopePick    = "pick"    // pick panes each conversation
	ScopeTab     = "tab"     // every pane in the tab

	MinScrollbackLines     = 50
	MaxScrollbackLines     = 1000
	DefaultScrollbackLines = 200
	DefaultHistoryDays     = 30
	DefaultAgentProvider   = "gemini"
)

// ClampScrollbackLines is a scrollback setting held to its range; 0 (unset)
// is the default.
func ClampScrollbackLines(n int) int {
	switch {
	case n == 0:
		return DefaultScrollbackLines
	case n < MinScrollbackLines:
		return MinScrollbackLines
	case n > MaxScrollbackLines:
		return MaxScrollbackLines
	}
	return n
}
