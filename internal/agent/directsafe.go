package agent

import (
	"regexp"
	"strings"
	"unicode"
)

// DirectSafe reports whether a command line may be typed without the
// approve dialog (docs/ui-redesign.md §6 rule 13). It is not Classify:
// Classify rates commands for the ⇧ rule and calls anything it reads as a
// reader "read-only", including wrappers that run another command
// (watch, env -S, ssh -o ProxyCommand), process substitution, pagers and
// tools with a write or exec flag. DirectSafe is an allowlist instead: a
// small set of commands that only read and print, each with the flags it
// may take and the shape of its other words. Anything it does not know is
// not safe, and the dialog decides.
//
// The line must be plain: a bare command word (no quotes, no path, no
// expansion), arguments made of plain characters or quoted strings with
// no expansion in them, and nothing a shell would treat as syntax: no $,
// backticks, !, ;, &, (, ), {, }, <, >, a backslash, globs, comments or
// NAME= assignments. A pipe is allowed only into the filters in
// pipeFilters, which read stdin and print.
func DirectSafe(line string) bool {
	if line == "" || len(line) > 1024 || Classify(line) != RiskReadOnly {
		return false
	}
	segs, ok := directSegments(line)
	if !ok || len(segs) == 0 || len(segs) > 4 {
		return false
	}
	for i, words := range segs {
		if len(words) == 0 {
			return false
		}
		for _, w := range words[1:] {
			if secretWord(w) {
				return false
			}
		}
		name := words[0]
		if !words0Plain(name) {
			return false
		}
		check := directCommands[name]
		if i > 0 {
			check = pipeFilters[name]
		}
		if check == nil || !check(words[1:]) {
			return false
		}
	}
	return true
}

// directSegments splits a line on unquoted pipes into words. It refuses
// every character that could be shell syntax or an expansion.
func directSegments(line string) ([][]string, bool) {
	var segs [][]string
	var cur []string
	var w strings.Builder
	inWord, quotedWord := false, false
	flush := func() {
		if inWord {
			s := w.String()
			if quotedWord {
				// A quoted word is marked so that a quoted command
				// word is refused (words0Plain checks the raw form).
				s = "\x00q" + s
			}
			cur = append(cur, s)
		}
		w.Reset()
		inWord, quotedWord = false, false
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == ' ':
			flush()
		case r == '|':
			flush()
			if i+1 < len(rs) && (rs[i+1] == '|' || rs[i+1] == '&') {
				return nil, false
			}
			if len(cur) == 0 {
				return nil, false
			}
			segs = append(segs, cur)
			cur = nil
		case r == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j >= len(rs) {
				return nil, false
			}
			// No backslash even here: echo in zsh and dash turns \e, \033
			// and \a into escapes the terminal obeys (OSC 52 writes the
			// clipboard).
			if strings.ContainsRune(string(rs[i+1:j]), '\\') {
				return nil, false
			}
			w.WriteString(string(rs[i+1 : j]))
			inWord, quotedWord = true, true
			i = j
		case r == '"':
			j := i + 1
			for j < len(rs) && rs[j] != '"' {
				// No expansion or escape inside double quotes.
				if strings.ContainsRune("$`\\!", rs[j]) {
					return nil, false
				}
				j++
			}
			if j >= len(rs) {
				return nil, false
			}
			w.WriteString(string(rs[i+1 : j]))
			inWord, quotedWord = true, true
			i = j
		case plainRune(r):
			w.WriteRune(r)
			inWord = true
		default:
			return nil, false
		}
	}
	flush()
	if len(cur) == 0 {
		return nil, false
	}
	segs = append(segs, cur)
	// ! is history expansion in bash and zsh, even inside double quotes
	// and, in zsh, in some single-quoted contexts: refused anywhere.
	if strings.ContainsRune(line, '!') {
		return nil, false
	}
	for _, s := range segs {
		for k, x := range s {
			s[k] = strings.TrimPrefix(x, "\x00q")
			if k == 0 && strings.HasPrefix(x, "\x00q") {
				s[k] = "\x00" // a quoted command word: never plain
			}
		}
	}
	return segs, true
}

// plainRune is a character that is never shell syntax outside quotes.
// Missing on purpose: $ ` ! ; & ( ) { } < > \ * ? [ ] # ~ ^ and space.
func plainRune(r rune) bool {
	if r < 0x80 {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:@%+,=", r)
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// words0Plain: the command word is a bare name (no path, no quote, no =).
func words0Plain(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// args describes what a command may take: flags from an allowlist (short
// letters that combine, long names), flags that take a value (the next
// word or attached), and positional words.
type args struct {
	short    string          // short flags that take no value, combinable (-la)
	shortVal string          // short flags that take a value (-n 5, -n5)
	long     map[string]bool // long flags with no value
	longVal  map[string]bool // long flags with a value (--since=x, --since x)
	numFlag  bool            // -NUM (head -20)
	maxPos   int             // how many positional words (-1: any)
	posOK    func(string) bool
	firstBSD string // ps: a first word of these letters with no dash (ps aux)
	valueOK  func(flag, v string) bool
}

// check reads ws against the allowlist. A positional word never starts
// with "-" unless after "--".
func (a args) check(ws []string) bool {
	pos := 0
	dashdash := false
	for i := 0; i < len(ws); i++ {
		w := ws[i]
		if w == "\x00" {
			return false
		}
		if i == 0 && a.firstBSD != "" && w != "" && strings.Trim(w, a.firstBSD) == "" {
			continue
		}
		switch {
		case dashdash || !strings.HasPrefix(w, "-") || w == "-":
			if a.maxPos >= 0 && pos >= a.maxPos {
				return false
			}
			if a.posOK != nil && !a.posOK(w) {
				return false
			}
			pos++
		case w == "--":
			dashdash = true
		case strings.HasPrefix(w, "--"):
			name, val, hasVal := strings.Cut(w[2:], "=")
			switch {
			case a.long[name] && !hasVal:
			case a.longVal[name]:
				if !hasVal {
					if i+1 >= len(ws) {
						return false
					}
					i++
					val = ws[i]
				}
				if a.valueOK != nil && !a.valueOK(name, val) {
					return false
				}
			default:
				return false
			}
		default:
			flags := w[1:]
			if a.numFlag && isDigits(flags) {
				continue
			}
			for k := 0; k < len(flags); k++ {
				c := flags[k]
				if strings.IndexByte(a.short, c) >= 0 {
					continue
				}
				if strings.IndexByte(a.shortVal, c) < 0 {
					return false
				}
				val := flags[k+1:]
				if val == "" {
					if i+1 >= len(ws) {
						return false
					}
					i++
					val = ws[i]
				}
				if a.valueOK != nil && !a.valueOK(string(c), val) {
					return false
				}
				break
			}
		}
	}
	return true
}

func (a args) fn() func([]string) bool { return a.check }

func none(ws []string) bool { return len(ws) == 0 }

// numeric values only (head -n 5, grep -A 3).
func numericVal(_, v string) bool { return v != "" && isDigits(v) }

func notFlagLike(s string) bool { return !strings.HasPrefix(s, "-") }

var (
	grepArgs = args{
		// No -r / -R / --recursive: it walks into ~/.ssh and the like.
		short: "invcwlLHhoEFxsqIPzZTU", shortVal: "ABCmef", maxPos: -1,
		long:    set("color", "colour", "ignore-case", "invert-match", "count", "line-number", "files-with-matches", "only-matching", "extended-regexp", "fixed-strings", "word-regexp", "no-messages", "quiet", "silent"),
		longVal: set("color", "colour", "include", "exclude", "exclude-dir", "max-count", "context", "after-context", "before-context"),
	}
	headArgs = args{short: "qvz", shortVal: "nc", numFlag: true, maxPos: -1, valueOK: func(f, v string) bool {
		return isDigits(strings.TrimPrefix(v, "-")) || isDigits(strings.TrimPrefix(v, "+"))
	}, long: set("quiet", "silent", "verbose"), longVal: set("lines", "bytes")}
	// tail: no -f / -F / --follow (it never ends).
	tailArgs = args{short: "qvz", shortVal: "nc", numFlag: true, maxPos: -1, valueOK: func(f, v string) bool {
		return isDigits(strings.TrimPrefix(v, "+")) || isDigits(strings.TrimPrefix(v, "-"))
	}, long: set("quiet", "silent", "verbose"), longVal: set("lines", "bytes")}
	wcArgs = args{short: "lwcmL", maxPos: -1, long: set("lines", "words", "bytes", "chars", "max-line-length")}
	// sort: no -o / --output (writes a file), no --compress-program (runs one),
	// no -T (temporary files) and no --files0-from.
	sortArgs = args{short: "bdfghiMnRrsuVz", shortVal: "kt", maxPos: -1, long: set("numeric-sort", "reverse", "unique", "human-numeric-sort", "version-sort", "ignore-case", "stable")}
	// uniq: a second word is an output file it writes.
	uniqArgs = args{short: "cdiuz", shortVal: "fsw", maxPos: 1, valueOK: numericVal, long: set("count", "repeated", "unique", "ignore-case")}
	cutArgs  = args{short: "sz", shortVal: "bcdf", maxPos: -1, long: set("only-delimited", "complement")}
	trArgs   = args{short: "cdsC", maxPos: 2}
)

// pipeFilters may stand after a pipe: they read stdin and print.
var pipeFilters = map[string]func([]string) bool{
	"grep":  grepArgs.fn(),
	"egrep": grepArgs.fn(),
	"fgrep": grepArgs.fn(),
	"head":  headArgs.fn(),
	"tail":  tailArgs.fn(),
	"wc":    wcArgs.fn(),
	"sort":  sortArgs.fn(),
	"uniq":  uniqArgs.fn(),
	"cut":   cutArgs.fn(),
	"tr":    trArgs.fn(),
}

// directCommands are the commands a direct type may start with. Not here,
// besides the list below: git (a checkout's own config, core.fsmonitor,
// diff.external, textconv or filters, runs code from git status, diff and
// log -p), history, kubectl config and docker inspect (they print
// secrets), and every follow or watch flag. Not here,
// on purpose: wrappers (sudo, env, watch, timeout, nice, xargs, exec,
// eval, command, builtin, sh/bash -c, ssh), pagers and interactive
// programs (less, more, man, top, htop, vim, fzf), interpreters (awk, sed,
// perl, python, node), tools with a write or exec flag (find, tee, dd,
// tar, curl, wget, yq, xxd, tcpdump, rsync), and every system setter
// (date with an argument, hostname with one, ip/ifconfig/route changes).
var directCommands = map[string]func([]string) bool{
	"ls":       args{short: "1aAbBcCdfFgGhHiklLmnNopqQrRsStuUvwxXZ", maxPos: -1, long: set("all", "almost-all", "human-readable", "color", "classify", "directory", "inode", "long", "reverse", "recursive", "size"), longVal: set("color", "sort", "time-style")}.fn(),
	"pwd":      args{short: "LP", maxPos: 0}.fn(),
	"whoami":   none,
	"id":       args{short: "unGgrz", maxPos: 1, posOK: notFlagLike}.fn(),
	"groups":   args{maxPos: 1}.fn(),
	"uptime":   args{short: "psV", maxPos: 0, long: set("pretty", "since")}.fn(),
	"uname":    args{short: "asnrvmpio", maxPos: 0, long: set("all")}.fn(),
	"hostname": args{short: "fsiIAd", maxPos: 0, long: set("fqdn", "short", "ip-address", "all-ip-addresses", "domain")}.fn(),
	// date: only a +FORMAT word; any other word sets the clock.
	"date":  args{short: "uR", shortVal: "I", maxPos: 1, posOK: func(s string) bool { return strings.HasPrefix(s, "+") }, long: set("utc", "rfc-email"), valueOK: func(f, v string) bool { return set("date", "hours", "minutes", "seconds", "ns")[v] }}.fn(),
	"df":    args{short: "ahHiklPTx", shortVal: "t", maxPos: -1, long: set("all", "human-readable", "inodes", "local", "portability", "print-type", "si")}.fn(),
	"du":    args{short: "achHkLmsSx", shortVal: "d", maxPos: -1, valueOK: numericVal, long: set("all", "human-readable", "summarize", "total", "apparent-size", "one-file-system", "si"), longVal: set("max-depth")}.fn(),
	"free":  args{short: "bkmghltw", maxPos: 0, long: set("human", "total", "wide", "bytes", "kibi", "mebi", "gibi", "si")}.fn(),
	"cat":   args{short: "AbeEnstTuv", maxPos: -1, long: set("number", "number-nonblank", "show-all", "show-ends", "show-tabs", "squeeze-blank")}.fn(),
	"head":  headArgs.fn(),
	"tail":  tailArgs.fn(),
	"wc":    wcArgs.fn(),
	"grep":  grepArgs.fn(),
	"egrep": grepArgs.fn(),
	"fgrep": grepArgs.fn(),
	"sort":  sortArgs.fn(),
	"uniq":  uniqArgs.fn(),
	"cut":   cutArgs.fn(),
	// echo: no -e, which turns \e into an escape the terminal obeys.
	"echo":     args{short: "nE", maxPos: -1}.fn(),
	"ps":       args{short: "AaefFHjlLMrTuwxyZ", shortVal: "opuUgGCt", maxPos: 0, firstBSD: "auxwfjlvTHrc", long: set("forest", "headers", "no-headers"), longVal: set("sort", "format", "pid", "ppid", "user")}.fn(),
	"pgrep":    args{short: "aflnoxcivw", shortVal: "uUgGPt", maxPos: 1, long: set("list-name", "list-full", "full", "newest", "oldest", "exact", "count")}.fn(),
	"pidof":    args{short: "sx", maxPos: -1}.fn(),
	"ss":       args{short: "tulpnaexmoiswr46H", maxPos: -1, posOK: ssFilterWord, long: set("tcp", "udp", "listening", "processes", "numeric", "all", "extended", "memory", "options", "info", "summary", "no-header")}.fn(),
	"netstat":  args{short: "tulpnaerisgWw46", maxPos: 0, long: set("tcp", "udp", "listening", "program", "numeric", "all", "extend", "route", "interfaces", "statistics")}.fn(),
	"lsblk":    args{short: "afmpbdlnrStJ", maxPos: -1, longVal: set("output")}.fn(),
	"lscpu":    args{short: "eapJxyB", maxPos: 0}.fn(),
	"nproc":    args{maxPos: 0, long: set("all")}.fn(),
	"arch":     none,
	"tty":      none,
	"w":        args{short: "hsfiou", maxPos: 1}.fn(),
	"who":      args{short: "abdHlmpqrstTu", maxPos: 0}.fn(),
	"last":     args{short: "aFiwx", shortVal: "n", numFlag: true, maxPos: 1, valueOK: numericVal}.fn(),
	"dmesg":    args{short: "dekHLPrTtux", shortVal: "l", maxPos: 0, long: set("ctime", "human", "kernel", "userspace", "decode", "notime", "reltime", "color"), longVal: set("level", "facility")}.fn(),
	"stat":     args{short: "fLt", shortVal: "c", maxPos: -1, long: set("dereference", "file-system", "terse"), longVal: set("format", "printf")}.fn(),
	"which":    args{short: "a", maxPos: -1}.fn(),
	"type":     args{short: "aPpt", maxPos: -1}.fn(),
	"basename": args{short: "asz", shortVal: "s", maxPos: -1}.fn(),
	"dirname":  args{short: "z", maxPos: -1}.fn(),
	"realpath": args{short: "eLmPqsz", maxPos: -1}.fn(),
	"readlink": args{short: "efmnqsvz", maxPos: -1}.fn(),
	// printenv: only named variables, never the whole environment.
	"printenv": func(ws []string) bool {
		return len(ws) > 0 && args{maxPos: -1, posOK: func(s string) bool { return words0Plain(strings.ToLower(s)) }}.check(ws)
	},
	"tree": args{short: "adfFhlpsugDCnrtx", shortVal: "LIP", maxPos: -1}.fn(),
	// lsof: no +r / +d / +D words (repeat mode never ends; +D walks trees).
	"lsof": args{short: "nPlt", shortVal: "ipuc", maxPos: -1, posOK: func(s string) bool { return !strings.HasPrefix(s, "+") }}.fn(),
	"dig":  args{short: "46", shortVal: "tcp", maxPos: -1, posOK: func(s string) bool { return !strings.HasPrefix(s, "-") }}.fn(),
	"host": args{short: "46aCdlrv", shortVal: "tcW", maxPos: 2}.fn(),
	// ping: only with a count, so it ends.
	"ping": pingArgs,
	// systemctl and journalctl page through less unless told not to.
	"systemctl":  systemctlArgs,
	"journalctl": journalctlArgs,
	"docker":     dockerArgs,
	"kubectl":    kubectlArgs,
	"ip":         ipArgs,
}

func ssFilterWord(s string) bool {
	// ss filter words such as "state listening" or "sport = :5432".
	return !strings.HasPrefix(s, "-")
}

func pingArgs(ws []string) bool {
	counted := false
	for i, w := range ws {
		if w == "-c" && i+1 < len(ws) && isDigits(ws[i+1]) {
			counted = true
		}
	}
	return counted && args{short: "46nqv", shortVal: "ciW", maxPos: 1, valueOK: numericVal}.check(ws)
}

func hasWord(ws []string, w string) bool {
	for _, x := range ws {
		if x == w {
			return true
		}
	}
	return false
}

var systemctlRead = set("status", "is-active", "is-enabled", "is-failed", "list-units", "list-unit-files", "list-timers", "list-sockets", "list-dependencies", "show", "cat")

func systemctlArgs(ws []string) bool {
	if !hasWord(ws, "--no-pager") {
		return false
	}
	// The verb is the first word (after --no-pager): a word after another
	// flag may be that flag's value ("-p status stop sshd").
	for len(ws) > 0 && ws[0] == "--no-pager" {
		ws = ws[1:]
	}
	if len(ws) == 0 {
		return false
	}
	sub, rest := ws[0], ws[1:]
	if !systemctlRead[sub] {
		return false
	}
	return args{short: "alq", shortVal: "tp", maxPos: -1, long: set("no-pager", "all", "failed", "user", "system", "quiet", "full", "plain", "no-legend"), longVal: set("type", "state", "property")}.check(rest)
}

func journalctlArgs(ws []string) bool {
	if !hasWord(ws, "--no-pager") {
		return false
	}
	return args{short: "abekqrx", shortVal: "unpo", numFlag: true, maxPos: 0, long: set("no-pager", "reverse", "boot", "dmesg", "utc", "catalog", "quiet", "no-hostname"), longVal: set("unit", "lines", "priority", "since", "until", "output", "grep", "identifier")}.check(ws)
}

func dockerArgs(ws []string) bool {
	if len(ws) == 0 {
		return false
	}
	sub, rest := ws[0], ws[1:]
	name := func(s string) bool { return notFlagLike(s) }
	switch sub {
	case "ps":
		return args{short: "aqsl", shortVal: "fn", maxPos: 0, long: set("all", "quiet", "size", "latest", "no-trunc"), longVal: set("filter", "format", "last")}.check(rest)
	case "images":
		return args{short: "aq", maxPos: 1, posOK: name, long: set("all", "quiet", "no-trunc", "digests"), longVal: set("filter", "format")}.check(rest)
	case "logs":
		return args{short: "t", shortVal: "n", maxPos: 1, posOK: name, long: set("timestamps"), longVal: set("tail", "since", "until")}.check(rest)
	case "version", "info":
		return args{maxPos: 0, longVal: set("format")}.check(rest)
	case "stats":
		return hasWord(rest, "--no-stream") && args{short: "a", maxPos: -1, posOK: name, long: set("no-stream", "all", "no-trunc"), longVal: set("format")}.check(rest)
	}
	return false
}

var kubectlRead = set("get", "describe", "logs", "top", "version", "cluster-info", "api-resources", "api-versions", "explain")

// kubectlArgs: read verbs only; no config verb; no --context,
// --kubeconfig, --token, --server or --as (another context or config's
// credential plugin runs code; the others change who the call is made
// as); no -f, --follow, -w or --watch (they never end).
func kubectlArgs(ws []string) bool {
	// The verb is the first word: a word after a flag may be its value
	// ("-n get delete ns prod").
	if len(ws) == 0 {
		return false
	}
	verb, rest := ws[0], ws[1:]
	if !kubectlRead[verb] {
		return false
	}
	return args{short: "Ap", shortVal: "nolc", maxPos: -1, posOK: notFlagLike, long: set("all-namespaces", "previous", "show-labels", "no-headers", "wide"), longVal: set("namespace", "output", "selector", "container", "tail", "since", "sort-by", "field-selector")}.check(rest)
}

// ipArgs: ip OBJECT [show|list] [dev NAME]: no add, del, set, flush.
func ipArgs(ws []string) bool {
	i := 0
	for i < len(ws) && set("-br", "-brief", "-c", "-color", "-4", "-6", "-s", "-d", "-j")[ws[i]] {
		i++
	}
	if i >= len(ws) || !set("a", "addr", "address", "r", "route", "l", "link", "n", "neigh", "neighbor")[ws[i]] {
		return false
	}
	rest := ws[i+1:]
	if len(rest) > 0 && set("show", "list", "ls")[rest[0]] {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return true
	}
	return len(rest) == 2 && rest[0] == "dev" && notFlagLike(rest[1]) && words0Plain(strings.ReplaceAll(strings.ReplaceAll(rest[1], ".", "-"), "@", "-"))
}

// secretPaths are the places that hold keys and tokens: a word naming one,
// as a path or inside one, is never typed with one click.
var secretPaths = []string{"/proc/", "environ", "/.ssh", "/.aws", "/.kube", "/.gnupg", "/.config/gh", "/.docker/config", "/.netrc", "/.git-credentials", "/etc/shadow", "/etc/gshadow", "/etc/sudoers"}

// secretNames are parts of a file or word name that suggest a secret. A
// grep pattern such as "token" is refused too: the dialog decides.
var secretNames = []string{"id_", ".pem", ".key", ".p12", ".pfx", "credential", "token", "secret", "password", ".env", "kubeconfig", "shadow", "_history", ".pgpass", ".npmrc", ".pypirc", ".my.cnf"}

// homeRoot matches a home directory itself (/root, /home, /home/NAME,
// /Users, /Users/NAME): cat, ls -R or grep there reaches its dot files.
var homeRoot = regexp.MustCompile(`^/(root|home|Users)(/[^/]+)?/?$`)

// secretWord reports whether an argument names a secret's place or file.
func secretWord(w string) bool {
	if _, v, ok := strings.Cut(w, "="); ok && homeRoot.MatchString(v) || homeRoot.MatchString(w) {
		return true
	}
	l := "/" + strings.ToLower(w)
	if _, v, ok := strings.Cut(l, "="); ok {
		l = "/" + v
	}
	for _, p := range secretPaths {
		if strings.Contains(l, p) {
			return true
		}
	}
	for _, n := range secretNames {
		if strings.Contains(l, n) {
			return true
		}
	}
	return false
}
