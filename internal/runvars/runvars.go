// Package runvars is the syntax and the safety rules of variables in
// runbook steps (docs/ui-redesign.md §17, groups part 2, board
// CaptureVars): `{{name}}` in a step's command names a group variable or a
// value Keel captured from an earlier step's output. It is pure: no I/O,
// no clock, no globals that change.
//
// A command keeps its `{{name}}` references wherever it is stored, shown
// to the model or logged. Values are put in (Fill) only by keel serve, at
// Type time, for one host of a group run, after the user approved the
// exact filled bytes.
//
// Shell safety. A value is typed into an interactive shell, so it is
// checked before it is put in:
//
//   - `{{name}}` (unquoted): the value must be one conservative shell word,
//     ConservativeRe: letters, digits and `_ . / : @ % + , = -`, not
//     starting with `-`, `=`, `%` or `+` (an option, zsh's `=cmd`, fish's
//     `%self`). No space, quote, `$`, backtick, glob, brace, `;`, `&`, `|`,
//     `<`, `>`, `#`, `~`, `!`, `^` or backslash, so it means the same in
//     sh, bash, zsh and fish.
//   - `{{name|q}}` (the step marks it quoted): the value is put in single
//     quotes. It may then hold spaces and shell characters, but never a
//     single quote or a backslash (fish reads `\'` and `\\` inside single
//     quotes, other shells do not), and never a control, format, line or
//     paragraph separator character (no newline can ever be typed from a
//     value).
//
// Anything else is refused with a message that says which variable and
// why; nothing is typed.
package runvars

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxNameBytes bounds a variable name.
	MaxNameBytes = 32
	// MaxValueBytes bounds a value that can be filled in (a captured value
	// or a group variable).
	MaxValueBytes = 512
	// MaxGroupValueBytes bounds a group variable's value.
	MaxGroupValueBytes = 256
	// MaxPatternBytes bounds a capture pattern.
	MaxPatternBytes = 256
	// MaxCapturesPerStep bounds the captures of one step.
	MaxCapturesPerStep = 4
	// QuoteFilter marks a reference to be single-quoted: {{name|q}}.
	QuoteFilter = "q"
)

var (
	// NameRe is a variable name as it is declared (a group variable or a
	// capture): lower-case letters, digits and "_", not starting with a
	// digit.
	NameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,31}$`)
	// refRe finds a reference. It accepts upper case too, so `{{FOO}}` is
	// a reference to a variable no one can declare (and is refused as
	// missing) instead of being typed as text. No space is allowed inside
	// the braces, so Go templates (`{{.Names}}`, `{{ json . }}`) and Jinja
	// (`{{ x }}`) are never references.
	refRe = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]{0,31})(\|q)?\}\}`)
	// ConservativeRe is the charset of a value filled in unquoted.
	ConservativeRe = regexp.MustCompile(`^[A-Za-z0-9_./:@][A-Za-z0-9_./:@%+,=-]*$`)

	// ErrMissing is a reference with no value.
	ErrMissing = errors.New("missing variable")
	// ErrUnsafe is a value that may not be filled in as it is.
	ErrUnsafe = errors.New("unsafe variable value")
)

// Ref is one reference in a command.
type Ref struct {
	Name   string `json:"name"`
	Quoted bool   `json:"quoted,omitempty"`
	start  int
	end    int
}

// reserved are words Go templates write alone in braces (kubectl
// -o go-template='{{range .items}}…{{end}}', helm): never references, never
// declarable, typed as they are.
var reserved = map[string]bool{"end": true, "else": true, "break": true, "continue": true, "nil": true, "true": true, "false": true}

// Refs lists the references in cmd, in order (a name may appear twice).
func Refs(cmd string) []Ref {
	var out []Ref
	for _, m := range refRe.FindAllStringSubmatchIndex(cmd, -1) {
		name := cmd[m[2]:m[3]]
		if reserved[name] && m[4] < 0 {
			continue
		}
		out = append(out, Ref{Name: name, Quoted: m[4] >= 0, start: m[0], end: m[1]})
	}
	return out
}

// Names lists the distinct names cmd references, in first-use order.
func Names(cmd string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range Refs(cmd) {
		if !seen[r.Name] {
			seen[r.Name] = true
			out = append(out, r.Name)
		}
	}
	return out
}

// HasRefs reports whether cmd references any variable.
func HasRefs(cmd string) bool { return len(Refs(cmd)) > 0 }

// ValidName reports whether name can be declared.
func ValidName(name string) bool { return NameRe.MatchString(name) && !reserved[name] }

// badRune is a rune no value may hold: controls (C0, DEL, C1), format,
// line and paragraph separators, and anything that is not printable.
func badRune(r rune) bool {
	return r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || !unicode.IsPrint(r) && r != ' '
}

// CheckValue checks a value for what holds whether it is quoted or not:
// valid UTF-8, 1 to max bytes, no control, format or separator character.
// The error says why, without the value.
func CheckValue(v string, max int) error {
	switch {
	case v == "":
		return fmt.Errorf("%w: it is empty", ErrUnsafe)
	case !utf8.ValidString(v):
		return fmt.Errorf("%w: it is not valid text", ErrUnsafe)
	case len(v) > max:
		return fmt.Errorf("%w: it is longer than %d bytes", ErrUnsafe, max)
	}
	for _, r := range v {
		if badRune(r) {
			if r == '\n' || r == '\r' {
				return fmt.Errorf("%w: it holds a line break", ErrUnsafe)
			}
			return fmt.Errorf("%w: it holds a control or invisible character", ErrUnsafe)
		}
	}
	return nil
}

// SafeUnquoted reports whether v may be filled in as a bare shell word.
func SafeUnquoted(v string) bool { return len(v) <= MaxValueBytes && ConservativeRe.MatchString(v) }

// Quote single-quotes v for a {{name|q}} reference. It fails for a value
// that single quotes cannot hold the same way in every shell.
func Quote(v string) (string, error) {
	if err := CheckValue(v, MaxValueBytes); err != nil {
		return "", err
	}
	if strings.ContainsAny(v, `'\`) {
		return "", fmt.Errorf("%w: it holds a single quote or a backslash, which shells read differently inside quotes", ErrUnsafe)
	}
	return "'" + v + "'", nil
}

// Value is what a lookup found for a name.
type Value struct {
	Value string
	// From says where it came from, for the user: "captured on cp-1 ·
	// step 1" or "group variable". Never the value.
	From string
}

// Used is one filled reference, without its value.
type Used struct {
	Name   string `json:"name"`
	From   string `json:"from"`
	Quoted bool   `json:"quoted,omitempty"`
}

// Fill puts the values into cmd. lookup returns a name's value, or an
// error that says why it has none (ErrMissing wrapped with the reason).
// Every reference must have a value that passes the shell-safety rules
// (see the package comment); otherwise Fill fails, naming the variable,
// and returns no command. used lists the filled names once each.
func Fill(cmd string, lookup func(name string) (Value, error)) (string, []Used, error) {
	refs := Refs(cmd)
	if len(refs) == 0 {
		return cmd, nil, nil
	}
	var b strings.Builder
	var used []Used
	seen := map[string]bool{}
	last := 0
	for _, r := range refs {
		if !ValidName(r.Name) {
			return "", nil, fmt.Errorf("%w: {{%s}} is not a variable name Keel knows (names are lower case)", ErrMissing, r.Name)
		}
		v, err := lookup(r.Name)
		if err != nil {
			return "", nil, err
		}
		if err := CheckValue(v.Value, MaxValueBytes); err != nil {
			return "", nil, fmt.Errorf("{{%s}} (%s) cannot be typed: %w", r.Name, v.From, err)
		}
		put := v.Value
		if r.Quoted {
			if put, err = Quote(v.Value); err != nil {
				return "", nil, fmt.Errorf("{{%s|q}} (%s) cannot be typed: %w", r.Name, v.From, err)
			}
		} else if !SafeUnquoted(v.Value) {
			return "", nil, fmt.Errorf("%w: {{%s}} (%s) holds characters a shell would read as more than one plain word (a space, quote, $, glob, ; or similar), or starts with -; write {{%s|q}} in the step to type it single-quoted", ErrUnsafe, r.Name, v.From, r.Name)
		}
		b.WriteString(cmd[last:r.start])
		b.WriteString(put)
		last = r.end
		key := r.Name
		if r.Quoted {
			key += "|q"
		}
		if !seen[key] {
			seen[key] = true
			used = append(used, Used{Name: r.Name, From: v.From, Quoted: r.Quoted})
		}
	}
	b.WriteString(cmd[last:])
	return b.String(), used, nil
}

// CompilePattern checks a capture pattern: one line, at most
// MaxPatternBytes, valid RE2 syntax (linear time, whatever the output), and
// at least one group. The value is the group named like the variable when
// there is one, else the first group.
func CompilePattern(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, errors.New("a capture needs a pattern")
	}
	if len(pattern) > MaxPatternBytes {
		return nil, fmt.Errorf("a pattern is at most %d bytes", MaxPatternBytes)
	}
	for _, r := range pattern {
		if badRune(r) {
			return nil, errors.New("a pattern is one line of visible characters")
		}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("the pattern is not a valid regular expression: %s", strings.TrimPrefix(err.Error(), "error parsing regexp: "))
	}
	if re.NumSubexp() < 1 {
		return nil, errors.New("the pattern needs a group around the value, such as --token\\s+(\\S+)")
	}
	return re, nil
}

// Extract applies a compiled pattern to text and returns the first match's
// group: the one named name, else the first. ok is false when nothing
// matched or the group is empty.
func Extract(re *regexp.Regexp, name, text string) (string, bool) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	g := 1
	if i := re.SubexpIndex(name); i > 0 {
		g = i
	}
	if g >= len(m) || m[g] == "" {
		return "", false
	}
	return m[g], true
}
