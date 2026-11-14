// Package sshparse reads a pasted `ssh …` command, an ssh:// URL or `user@host:port` into the few fields
// Keel's host model has (host, port, user).
//
// Pure functions of a string: nothing here starts a process, opens a file, expands a variable or looks a
// name up, and the package imports only the standard library (never os, os/exec or net). The text is split
// into words the way a shell splits quotes, but never expanded. Input that holds a shell operator is
// refused outright, so it can neither yield a host nor be half-read. Options that run commands, change
// trust, open forwards or are not part of the host model are dropped and listed in Result.Dropped, so the
// UI can show what was left out. A key file named by -i is never opened: it only becomes a note.
package sshparse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// MaxInput is the longest text ParseConnect reads (UTF-16 units, as the JS counts them).
const MaxInput = 2048

// Dropped is one option that was left out, and why.
type Dropped struct{ Option, Reason string }

// Result is what ParseConnect read. When OK is false only Error and Source are set (Host is "" and Port 0):
// there is nothing to connect to.
type Result struct {
	OK           bool
	Error        string
	Source       string // "command" | "url" | "target"
	Host         string
	Port         int
	User         string
	IdentityFile string
	Jump         string
	Dropped      []Dropped
	Notes        []string // strings the user should read (for example the key file to pick)
}

// ws is JavaScript's \s: the characters a regular expression there treats as white space.
const ws = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var (
	hostRE   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_.-]*[A-Za-z0-9])?$`)
	userRE   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
	ipv6RE   = regexp.MustCompile(`^[0-9A-Fa-f:]*:[0-9A-Fa-f:.]*$`)
	ipv6Only = regexp.MustCompile(`^[0-9A-Fa-f:.]+$`)
	portRE   = regexp.MustCompile(`^[0-9]{1,5}$`)

	continuationRE = regexp.MustCompile(`\\\r?\n`)
	promptRE       = regexp.MustCompile(`^\$[ \t\x{a0}]+`)
	commandRE      = regexp.MustCompile(`^(?:/[^` + ws + `]*/)?ssh(?:[ \t\x{a0}]|$)`)
	urlRE          = regexp.MustCompile(`(?i)^ssh://`)
	targetRE       = regexp.MustCompile(`^[^` + ws + `@:/]+@[^` + ws + `]+$`)
	lineBreakRE    = regexp.MustCompile(`[\r\n]`)
	controlRE      = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
	optionRE       = regexp.MustCompile(`^[` + ws + `]*([A-Za-z][A-Za-z0-9]*)[` + ws + `]*(?:=|[` + ws + `])[` + ws + `]*([^\n\r\x{2028}\x{2029}]*)$`)
)

// ValidHost accepts a DNS name, an IPv4 address or an IPv6 address (no brackets). A leading "-" is refused so
// a host can never read as an option.
func ValidHost(host string) bool {
	if host == "" || len(utf16.Encode([]rune(host))) > 253 {
		return false
	}
	if strings.Contains(host, ":") {
		return ipv6RE.MatchString(host) && ipv6Only.MatchString(host) && len(strings.Split(host, ":")) <= 8 && !strings.Contains(host, ":::")
	}
	if strings.Contains(host, "..") {
		return false
	}
	if !hostRE.MatchString(host) {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) > 63 {
			return false
		}
	}
	return true
}

func ValidUser(user string) bool { return userRE.MatchString(user) }

// ValidPort accepts a whole number 1 to 65535 (the New host form allows 0 to 65535; 0 is not a port you can
// connect to).
func ValidPort(port string) bool {
	if !portRE.MatchString(port) {
		return false
	}
	n, _ := strconv.Atoi(port)
	return n >= 1 && n <= 65535
}

// ---- words ----

func isOperator(r rune) bool { return strings.ContainsRune(";|&<>()`\n\r", r) }

// jsSpace is what JavaScript's String.prototype.trim removes.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', 0xfeff, 0x2028, 0x2029:
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

func trim(s string) string { return strings.TrimFunc(s, jsSpace) }

// splitWords splits like a POSIX shell for quotes and backslashes only. The error is "" when it worked.
func splitWords(text string) (words []string, errText string) {
	rs := []rune(text)
	var cur []rune
	inWord := false
	i := 0
	for i < len(rs) {
		c := rs[i]
		switch {
		case c == ' ' || c == '\t' || c == 0xa0:
			if inWord {
				words = append(words, string(cur))
				cur, inWord = nil, false
			}
			i++
		case c == '\'':
			end := -1
			for j := i + 1; j < len(rs); j++ {
				if rs[j] == '\'' {
					end = j
					break
				}
			}
			if end < 0 {
				return nil, "Unclosed quote"
			}
			cur = append(cur, rs[i+1:end]...)
			inWord = true
			i = end + 1
		case c == '"':
			i++
			closed := false
			for i < len(rs) {
				d := rs[i]
				if d == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\') {
					cur = append(cur, rs[i+1])
					i += 2
				} else if d == '"' {
					closed = true
					i++
					break
				} else {
					cur = append(cur, d)
					i++
				}
			}
			if !closed {
				return nil, "Unclosed quote"
			}
			inWord = true
		case c == '\\':
			if i+1 >= len(rs) {
				return nil, "Trailing backslash"
			}
			cur = append(cur, rs[i+1])
			inWord = true
			i += 2
		case isOperator(c):
			return nil, "Shell operators are not allowed"
		default:
			cur = append(cur, c)
			inWord = true
			i++
		}
	}
	if inWord {
		words = append(words, string(cur))
	}
	return words, ""
}

// ---- options ----

type withArg struct{ kind, reason string }

// Short options that take a value, and what Keel does with them.
var withArgs = map[rune]withArg{
	'p': {"port", ""}, 'l': {"user", ""}, 'i': {"identity", ""}, 'J': {"jump", ""}, 'o': {"o", ""},
	'L': {"forward", "opens a port forward; add it in Forwards"},
	'R': {"forward", "opens a port forward; add it in Forwards"},
	'D': {"forward", "opens a port forward; add it in Forwards"},
	'W': {"forward", "forwards standard input and output"},
	'w': {"forward", "opens a tunnel device"},
	'F': {"unsupported", "reads another config file"},
	'b': {"unsupported", "not part of the host model"},
	'B': {"unsupported", "not part of the host model"},
	'c': {"unsupported", "not part of the host model"},
	'E': {"unsupported", "writes a log file"},
	'e': {"unsupported", "not part of the host model"},
	'I': {"unsupported", "loads a PKCS#11 library"},
	'm': {"unsupported", "not part of the host model"},
	'O': {"unsupported", "controls a running connection"},
	'P': {"unsupported", "not part of the host model"},
	'Q': {"unsupported", "not part of the host model"},
	'S': {"unsupported", "uses a control socket"},
}

// Short options without a value that are dropped. Everything else is refused as an unknown option.
var flags = map[rune]string{
	'A': "forwards your agent", 'a': "not part of the host model", 'X': "forwards X11", 'x': "not part of the host model",
	'Y': "forwards X11", 'N': "runs no shell", 'f': "goes to the background", 'n': "not part of the host model",
	's': "starts a subsystem", 'T': "not part of the host model", 't': "not part of the host model",
	'q': "not part of the host model", 'v': "not part of the host model", 'C': "not part of the host model",
	'g': "not part of the host model", 'G': "prints config instead of connecting", 'K': "not part of the host model",
	'k': "not part of the host model", 'M': "controls a running connection", 'V': "not part of the host model",
	'y': "not part of the host model", '4': "not part of the host model", '6': "not part of the host model",
	'1': "not part of the host model", '2': "not part of the host model",
}

// -o keys Keel can use, and keys it names a reason for. Any other key is dropped as "not part of the host
// model". "jump" and "identity" are not reasons: they are the two keys that fill a field.
var oReason = map[string]string{
	"proxycommand": "runs a command", "localcommand": "runs a command", "permitlocalcommand": "runs a command",
	"remotecommand": "runs a command", "knownhostscommand": "runs a command", "match": "runs a command",
	"include":               "reads another config file",
	"stricthostkeychecking": "changes host key trust", "userknownhostsfile": "changes host key trust",
	"globalknownhostsfile": "changes host key trust", "hostkeyalgorithms": "changes host key trust",
	"verifyhostkeydns": "changes host key trust", "updatehostkeys": "changes host key trust",
	"checkhostip": "changes host key trust", "hostkeyalias": "changes host key trust",
	"proxyjump": "jump", "identityfile": "identity",
	"localforward":   "opens a port forward; add it in Forwards",
	"remoteforward":  "opens a port forward; add it in Forwards",
	"dynamicforward": "opens a port forward; add it in Forwards",
	"forwardagent":   "forwards your agent", "forwardx11": "forwards X11", "hostname": "replaces the host",
}

const jumpReason = "jump hosts are not supported yet"

func clip(s string, n int) string {
	rs := []rune(s)
	if len(rs) > n {
		return string(rs[:n-1]) + "…"
	}
	return s
}

// splitO splits "Key=Value" / "Key Value" into key and value.
func splitO(arg string) (key, value string) {
	if m := optionRE.FindStringSubmatch(arg); m != nil {
		return m[1], m[2]
	}
	return trim(arg), ""
}

// ---- destination ----

type dest struct{ user, host, port string }

// parseDest reads "[user@]host[:port]", "[user@][v6][:port]" and a bare IPv6 address.
func parseDest(raw string) (dest, string) {
	rest, user := raw, ""
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		user, rest = rest[:at], rest[at+1:]
		if !ValidUser(user) {
			return dest{}, "User name has characters Keel does not accept"
		}
	}
	host, port := rest, ""
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return dest{}, "Unclosed [ in address"
		}
		host = rest[1:end]
		if tail := rest[end+1:]; tail != "" {
			if !strings.HasPrefix(tail, ":") {
				return dest{}, "Unexpected text after the address"
			}
			port = tail[1:]
		}
		if !strings.Contains(host, ":") {
			return dest{}, "Brackets are for IPv6 addresses"
		}
	} else if strings.Count(rest, ":") == 1 {
		host, port, _ = strings.Cut(rest, ":")
	}
	if !ValidHost(host) {
		if host == "" {
			return dest{}, "Host is missing"
		}
		return dest{}, "Host name has characters Keel does not accept"
	}
	if port != "" && !ValidPort(port) {
		return dest{}, "Port must be 1 to 65535"
	}
	return dest{user, host, port}, ""
}

// ---- entry points ----

func newResult(source string) *Result { return &Result{OK: true, Source: source, Port: 22} }

func fail(msg string) *Result { return &Result{Error: msg} }

func normalize(raw string) string {
	text := continuationRE.ReplaceAllString(raw, " ") // a line continuation joins lines, as a shell would
	text = trim(text)
	text = promptRE.ReplaceAllString(text, "") // a copied prompt: "$ ssh host"
	return trim(text)
}

// refuse says why text can never be read, or "" when it can be.
func refuse(text string) string {
	switch {
	case len(utf16.Encode([]rune(text))) > MaxInput:
		return "Too long"
	case strings.Contains(text, "$(") || strings.Contains(text, "${") || strings.Contains(text, "`"):
		return "Shell substitution is not allowed"
	case lineBreakRE.MatchString(text):
		return "One command only"
	case controlRE.MatchString(text):
		return "Control characters are not allowed"
	}
	return ""
}

// ParseConnect reads one pasted line. It returns nil when the text does not look like a connection (it is
// just a filter for saved hosts). Otherwise the Result is OK, or not OK with an Error and a Source. Nothing
// is ever run: the caller shows Dropped and Notes.
func ParseConnect(raw string) *Result {
	text := normalize(raw)
	if text == "" {
		return nil
	}
	isCommand := commandRE.MatchString(text)
	isURL := urlRE.MatchString(text)
	isTarget := !isCommand && !isURL && targetRE.MatchString(text)
	if !isCommand && !isURL && !isTarget {
		// "host:port" alone is a plain connect only with a user; otherwise it is a filter for saved hosts.
		return nil
	}
	source := "command"
	switch {
	case isURL:
		source = "url"
	case isTarget:
		source = "target"
	}
	if why := refuse(text); why != "" {
		return &Result{Error: why, Source: source}
	}
	var r *Result
	switch source {
	case "url":
		r = parseURL(text)
	case "target":
		r = parseTarget(text)
	default:
		r = parseCommand(text)
	}
	if !r.OK {
		r.Source = source
	}
	return r
}

func parseURL(text string) *Result {
	words, e := splitWords(text)
	if e != "" {
		return fail(e)
	}
	if len(words) != 1 {
		return fail("Expected one ssh:// address")
	}
	rs := []rune(words[0])
	rest := ""
	if len(rs) > len("ssh://") {
		rest = string(rs[len("ssh://"):])
	}
	if slash := strings.Index(rest, "/"); slash >= 0 {
		if rest[slash+1:] != "" {
			return fail("A path after the address is not used")
		}
		rest = rest[:slash]
	}
	d, e := parseDest(rest)
	if e != "" {
		return fail(e)
	}
	r := newResult("url")
	r.Host, r.Port, r.User = d.host, portOr22(d.port), d.user
	return r
}

func parseTarget(text string) *Result {
	words, e := splitWords(text)
	if e != "" {
		return fail(e)
	}
	if len(words) != 1 {
		return fail("Expected user@host:port")
	}
	d, e := parseDest(words[0])
	if e != "" {
		return fail(e)
	}
	r := newResult("target")
	r.Host, r.Port, r.User = d.host, portOr22(d.port), d.user
	return r
}

func portOr22(s string) int {
	if s == "" {
		return 22
	}
	n, _ := strconv.Atoi(s)
	return n
}

// opt is a string that may be unset (JavaScript's null), because "" is a value there: -p ” is refused.
type opt struct {
	v   string
	set bool
}

func (o *opt) put(v string) { o.v, o.set = v, true }

func parseCommand(text string) *Result {
	all, e := splitWords(text)
	if e != "" {
		return fail(e)
	}
	words := all[1:] // drop "ssh"
	out := newResult("command")
	drop := func(option, reason string) { out.Dropped = append(out.Dropped, Dropped{clip(option, 80), reason}) }

	var port, loginUser, oPort, oUser opt
	var dst, jump, identity string

	applyO := func(arg string) {
		key, value := splitO(arg)
		k := strings.ToLower(key)
		switch k {
		case "port":
			oPort.put(trim(value))
			return
		case "user":
			oUser.put(trim(value))
			return
		}
		reason := oReason[k]
		switch reason {
		case "jump":
			jump = trim(value)
		case "identity":
			if identity == "" {
				identity = trim(value)
			}
		default:
			if reason == "" {
				reason = "not part of the host model"
			}
			shown := "-o " + key
			if value != "" {
				shown += "=" + value
			}
			drop(shown, reason)
		}
	}

	i := 0
	afterOpts := false
	for i < len(words) {
		w := words[i]
		if !afterOpts && w == "--" {
			afterOpts = true
			i++
			continue
		}
		if wr := []rune(w); !afterOpts && len(wr) > 1 && wr[0] == '-' {
			// A cluster of short options, such as -vvv, -4N or -p2222.
			j := 1
			i++
			for j < len(wr) {
				ch := wr[j]
				if spec, ok := withArgs[ch]; ok {
					arg := string(wr[j+1:])
					if arg == "" {
						if i >= len(words) {
							return fail(fmt.Sprintf("-%c needs a value", ch))
						}
						arg = words[i]
						i++
					}
					switch spec.kind {
					case "port":
						port.put(arg)
					case "user":
						loginUser.put(arg)
					case "identity":
						identity = arg
					case "jump":
						jump = arg
					case "o":
						applyO(arg)
					default:
						drop(fmt.Sprintf("-%c %s", ch, arg), spec.reason)
					}
					break
				}
				if reason, ok := flags[ch]; ok {
					drop(fmt.Sprintf("-%c", ch), reason)
					j++
					continue
				}
				return fail(fmt.Sprintf("Unknown option -%c", ch))
			}
			continue
		}
		if dst == "" {
			dst = w
			i++
			continue
		}
		// The first word after the destination that is not an option starts a remote command. It is never run.
		drop(strings.Join(words[i:], " "), "remote command; Keel opens a shell and does not run it")
		break
	}

	if dst == "" {
		return fail("No host in the command")
	}
	if strings.HasPrefix(dst, "-") {
		return fail("Host looks like an option")
	}

	var d dest
	if urlRE.MatchString(dst) {
		u := parseURL(dst)
		if !u.OK {
			return u
		}
		d = dest{u.User, u.Host, strconv.Itoa(u.Port)}
	} else if d, e = parseDest(dst); e != "" {
		return fail(e)
	}

	// The destination's own user and port win over -l and -p, as in ssh.
	portText := opt{d.port, d.port != ""}
	if d.port == "" {
		portText = port
		if !port.set {
			portText = oPort
		}
	}
	if portText.set && !ValidPort(portText.v) {
		return fail("Port must be 1 to 65535")
	}
	userText := opt{d.user, d.user != ""}
	if d.user == "" {
		userText = loginUser
		if !loginUser.set {
			userText = oUser
		}
	}
	if userText.set && !ValidUser(userText.v) {
		return fail("User name has characters Keel does not accept")
	}

	out.Host = d.host
	out.Port = 22
	if portText.v != "" {
		out.Port = portOr22(portText.v)
	}
	out.User = userText.v

	if jump != "" {
		out.Jump = clip(jump, 120)
		drop("-J "+jump, jumpReason)
	}
	if identity != "" {
		out.IdentityFile = clip(identity, 160)
		out.Notes = append(out.Notes, "Key file "+clip(identity, 160)+" was not read. Add the key in Keychain, then pick it as the host's identity.")
	}
	return out
}

// DescribeDropped turns the dropped list into one line per entry for the note under the input.
func DescribeDropped(dropped []Dropped) []string {
	lines := make([]string, 0, len(dropped))
	for _, d := range dropped {
		lines = append(lines, d.Option+": "+d.Reason)
	}
	return lines
}
