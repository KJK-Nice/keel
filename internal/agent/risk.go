package agent

import (
	"path"
	"regexp"
	"strings"
)

// Risk is how much a command can change on the host it runs on. It is the
// label shown on every proposal (§5.4) and the input to the ⇧ gate
// (§6.5). Phase 6b rates commands with the code rule below; a model's
// judgment (6c) may only make a rating stricter, never looser.
type Risk string

const (
	RiskReadOnly    Risk = "readonly"    // reads state only
	RiskChanges     Risk = "changes"     // changes state (files, services, packages, config)
	RiskDestructive Risk = "destructive" // deletes data or takes a host or service down
)

func (r Risk) rank() int {
	switch r {
	case RiskReadOnly:
		return 0
	case RiskDestructive:
		return 2
	default:
		return 1
	}
}

// Stricter returns the stricter of a and b.
func Stricter(a, b Risk) Risk {
	if b.rank() > a.rank() {
		return b
	}
	if a == "" {
		return RiskChanges
	}
	return a
}

// NeedsShift reports whether typing a command needs the ⇧ modifier on top
// of the approve dialog: anything destructive, and anything that is not
// read-only on a host marked production (§6.5).
func NeedsShift(r Risk, production bool) bool {
	switch {
	case r == RiskDestructive:
		return true
	case production && r != RiskReadOnly:
		return true
	}
	return false
}

// Classify rates a shell command line. It is deliberately conservative: a
// command it does not know is "changes", a pipeline or list takes the
// worst of its parts, command substitutions are rated too, and writing to
// a file with > is at least "changes". It parses like a shell only far
// enough to find words, quotes, separators and redirections.
func Classify(line string) Risk {
	return classifyDepth(line, 0)
}

const maxClassifyDepth = 4

func classifyDepth(line string, depth int) Risk {
	if depth > maxClassifyDepth {
		return RiskChanges
	}
	segs, subs, ok := splitShell(line)
	if !ok {
		return RiskChanges
	}
	worst := RiskReadOnly
	for _, s := range subs {
		worst = Stricter(worst, classifyDepth(s, depth+1))
	}
	for _, seg := range segs {
		worst = Stricter(worst, classifySegment(seg, depth))
		if worst == RiskDestructive {
			return worst
		}
	}
	return worst
}

// segment is one simple command: its words, and the targets of its output
// redirections.
type segment struct {
	words   []string
	redirTo []string
}

// splitShell splits a command line into simple commands. It returns the
// bodies of $(…) and `…` substitutions separately. ok is false for an
// unterminated quote or substitution.
func splitShell(line string) (segs []segment, subs []string, ok bool) {
	var cur segment
	var word strings.Builder
	inWord := false
	redirNext := false
	flushWord := func() {
		if !inWord {
			return
		}
		w := word.String()
		word.Reset()
		inWord = false
		if redirNext {
			cur.redirTo = append(cur.redirTo, w)
			redirNext = false
			return
		}
		cur.words = append(cur.words, w)
	}
	flushSeg := func() {
		flushWord()
		if redirNext {
			// "cmd >" with no target: treat as a write to an unknown file.
			cur.redirTo = append(cur.redirTo, "")
			redirNext = false
		}
		if len(cur.words) > 0 || len(cur.redirTo) > 0 {
			segs = append(segs, cur)
		}
		cur = segment{}
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\\':
			if i+1 < len(rs) {
				i++
				if rs[i] != '\n' {
					word.WriteRune(rs[i])
					inWord = true
				}
			}
		case c == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j >= len(rs) {
				return nil, nil, false
			}
			word.WriteString(string(rs[i+1 : j]))
			inWord = true
			i = j
		case c == '"':
			j := i + 1
			for j < len(rs) && rs[j] != '"' {
				if rs[j] == '\\' && j+1 < len(rs) {
					word.WriteRune(rs[j+1])
					j += 2
					continue
				}
				if rs[j] == '`' || (rs[j] == '$' && j+1 < len(rs) && rs[j+1] == '(') {
					body, end, ok := substitution(rs, j)
					if !ok {
						return nil, nil, false
					}
					subs = append(subs, body)
					word.WriteString("$SUB")
					j = end + 1
					continue
				}
				word.WriteRune(rs[j])
				j++
			}
			if j >= len(rs) {
				return nil, nil, false
			}
			inWord = true
			i = j
		case c == '`' || (c == '$' && i+1 < len(rs) && rs[i+1] == '('):
			body, end, ok := substitution(rs, i)
			if !ok {
				return nil, nil, false
			}
			subs = append(subs, body)
			word.WriteString("$SUB")
			inWord = true
			i = end
		case c == ';' || c == '\n' || c == '|' || c == '&':
			// 2>&1 and >&2 are redirections, not separators.
			if c == '&' && i > 0 && rs[i-1] == '>' {
				word.WriteRune(c)
				inWord = true
				continue
			}
			flushSeg()
			if (c == '|' || c == '&') && i+1 < len(rs) && rs[i+1] == c {
				i++
			}
		case c == '>':
			// A digit word right before it ("2>") is a file descriptor.
			if inWord && isDigits(word.String()) {
				word.Reset()
				inWord = false
			} else {
				flushWord()
			}
			if i+1 < len(rs) && (rs[i+1] == '>' || rs[i+1] == '|') {
				i++
			}
			if i+1 < len(rs) && rs[i+1] == '&' {
				// >&2, 2>&1: duplicate a descriptor, no file.
				i++
				for i+1 < len(rs) && (rs[i+1] >= '0' && rs[i+1] <= '9' || rs[i+1] == '-') {
					i++
				}
				continue
			}
			redirNext = true
		case c == '<':
			flushWord()
			// Input redirection and here-docs read; skip the operator.
			for i+1 < len(rs) && (rs[i+1] == '<' || rs[i+1] == '-') {
				i++
			}
		case c == ' ' || c == '\t' || c == '\r':
			flushWord()
		case c == '#' && !inWord:
			// A comment runs to the end of the line.
			for i+1 < len(rs) && rs[i+1] != '\n' {
				i++
			}
		default:
			word.WriteRune(c)
			inWord = true
		}
	}
	flushSeg()
	return segs, subs, true
}

// substitution returns the body of a $(…) or `…` starting at rs[i] and the
// index of its closing character.
func substitution(rs []rune, i int) (string, int, bool) {
	if rs[i] == '`' {
		for j := i + 1; j < len(rs); j++ {
			if rs[j] == '\\' {
				j++
				continue
			}
			if rs[j] == '`' {
				return string(rs[i+1 : j]), j, true
			}
		}
		return "", 0, false
	}
	depth := 0
	var quote rune
	for j := i + 1; j < len(rs); j++ {
		c := rs[j]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				j++
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '\\':
			j++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if i+2 < len(rs) && rs[i+2] == '(' {
					// $(( … )) is arithmetic, not a command.
					return "", j, true
				}
				return string(rs[i+2 : j]), j, true
			}
		}
	}
	return "", 0, false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

var (
	blockDevice = regexp.MustCompile(`^/dev/(sd|hd|vd|xvd|nvme|mmcblk|disk|rdisk|md|dm-|mapper/)`)
	sqlDestroy  = regexp.MustCompile(`(?i)\b(drop\s+(table|database|schema|index|view|user|role)|truncate\b|delete\s+from)\b`)
	sqlChange   = regexp.MustCompile(`(?i)\b(insert\s+into|update\s+\S+\s+set|alter|create|grant|revoke|vacuum|reindex|cluster|copy\s+\S+\s+from|set\s+|reset\s+|kill|shutdown|flush|optimize|repair|load\s+data|replace\s+into|call|do)\b`)
)

func classifyRedirect(target string) Risk {
	switch {
	case target == "/dev/null" || target == "/dev/stdout" || target == "/dev/stderr" || target == "/dev/tty":
		return RiskReadOnly
	case blockDevice.MatchString(target):
		return RiskDestructive
	}
	return RiskChanges
}

// Commands that only read. Some are checked further below for flags that
// make them write.
var readOnlyCmds = set(
	"ls", "ll", "la", "dir", "cat", "tac", "head", "tail", "less", "more", "most", "grep", "egrep", "fgrep", "rg", "ag", "ack",
	"wc", "df", "du", "free", "uptime", "uname", "whoami", "id", "groups", "hostname", "hostnamectl", "date", "cal",
	"ps", "top", "htop", "btop", "atop", "pgrep", "pidof", "pstree", "lsof", "ss", "netstat", "ifconfig", "route",
	"ping", "ping6", "traceroute", "tracepath", "mtr", "dig", "nslookup", "host", "whois", "arp",
	"echo", "printf", "pwd", "env", "printenv", "which", "whereis", "type", "stat", "file", "readlink", "realpath",
	"basename", "dirname", "md5sum", "sha1sum", "sha256sum", "sha512sum", "cksum", "b2sum", "tree", "sort", "uniq",
	"cut", "awk", "gawk", "mawk", "tr", "jq", "yq", "column", "nl", "fold", "fmt", "paste", "join", "comm", "diff", "cmp",
	"od", "xxd", "hexdump", "strings", "nproc", "lscpu", "lsblk", "blkid", "lsusb", "lspci", "lsmod", "findmnt", "vmstat",
	"iostat", "mpstat", "pidstat", "sar", "getent", "last", "lastlog", "w", "who", "users", "history", "test", "[", "true",
	"false", "sleep", "man", "info", "help", "cd", "pushd", "popd", "seq", "yes", "zcat", "zgrep", "zless", "bzcat", "xzcat",
	"tput", "clear", "reset", "locale", "timedatectl", "loginctl", "uptime", "dmidecode", "sensors", "nvidia-smi",
	"iotop", "iftop", "nload", "nethogs", "tcpdump", "ncdu", "exa", "eza", "bat", "fd", "fzf", "watch", "base64",
	"dpkg-query", "lsattr", "getfacl", "ldd", "nm", "objdump", "readelf", "size", "ulimit", "umask", "id",
)

// Commands that delete data or take the machine down whatever their
// arguments.
var destructiveCmds = set(
	"rm", "rmdir", "unlink", "shred", "srm", "dd", "wipefs", "fdisk", "sfdisk", "gdisk", "cfdisk", "parted",
	"mkswap", "reboot", "shutdown", "halt", "poweroff", "truncate", "blkdiscard", "zpool", "lvremove", "vgremove", "pvremove",
)

// Wrappers run the command that follows them. flagArgs lists their flags
// that take a value.
var wrappers = map[string]map[string]bool{
	"sudo":    set("-u", "-g", "-C", "-h", "-p", "-U", "-r", "-t", "-D", "-R", "-T"),
	"doas":    set("-u", "-C"),
	"env":     set("-u", "-C", "-S"),
	"time":    set("-f", "-o"),
	"nice":    set("-n"),
	"ionice":  set("-c", "-n", "-p"),
	"nohup":   {},
	"timeout": set("-s", "-k", "--signal", "--kill-after"),
	"watch":   set("-n", "-d", "--interval"),
	"stdbuf":  set("-i", "-o", "-e"),
	"command": {},
	"builtin": {},
	"exec":    {},
	"chrt":    {},
	"taskset": {},
	"xargs":   set("-I", "-n", "-P", "-d", "-L", "-s", "-E", "-a"),
	"strace":  set("-e", "-o", "-p", "-s"),
	"ltrace":  set("-e", "-o", "-p"),
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func classifySegment(seg segment, depth int) Risk {
	worst := RiskReadOnly
	for _, t := range seg.redirTo {
		worst = Stricter(worst, classifyRedirect(t))
	}
	w := seg.words
	// Leading assignments: NAME=value cmd …
	for len(w) > 0 && isAssignment(w[0]) {
		w = w[1:]
	}
	w = unwrap(w)
	if len(w) == 0 {
		return worst
	}
	return Stricter(worst, classifyWords(w, depth))
}

func isAssignment(s string) bool {
	i := strings.IndexByte(s, '=')
	if i <= 0 {
		return false
	}
	for j, c := range s[:i] {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || j > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func cmdName(s string) string {
	return path.Base(strings.TrimPrefix(s, "\\"))
}

// unwrap drops sudo, env, timeout and similar wrappers (and their flags)
// from the front of a command.
func unwrap(w []string) []string {
	for guard := 0; len(w) > 0 && guard < 8; guard++ {
		name := cmdName(w[0])
		flags, ok := wrappers[name]
		if !ok {
			return w
		}
		w = w[1:]
	flagLoop:
		for len(w) > 0 {
			a := w[0]
			switch {
			case name == "env" && isAssignment(a):
				w = w[1:]
			case a == "--":
				w = w[1:]
				break flagLoop
			case strings.HasPrefix(a, "-") && a != "-":
				w = w[1:]
				if flags[a] && len(w) > 0 {
					w = w[1:]
				}
			case name == "timeout" && a != "" && a[0] >= '0' && a[0] <= '9':
				w = w[1:]
				break flagLoop
			default:
				break flagLoop
			}
		}
	}
	return w
}

func hasAny(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f || strings.HasPrefix(f, "--") && strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

// hasShort reports whether a short flag letter appears in any -abc group.
func hasShort(args []string, letter byte) bool {
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.IndexByte(a[1:], letter) >= 0 {
			return true
		}
	}
	return false
}

// firstArg returns the first argument that is not a flag.
func firstArg(args []string) (string, []string) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a, args[i+1:]
		}
	}
	return "", nil
}

func classifyWords(w []string, depth int) Risk {
	name := cmdName(w[0])
	args := w[1:]
	switch {
	case destructiveCmds[name], strings.HasPrefix(name, "mkfs"):
		return RiskDestructive
	}
	switch name {
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		for i, a := range args {
			if a == "-c" && i+1 < len(args) {
				return classifyDepth(args[i+1], depth+1)
			}
		}
		return RiskChanges
	case "eval":
		return classifyDepth(strings.Join(args, " "), depth+1)
	case "ssh":
		// ssh [opts] host [command…]: rate the remote command.
		rest := args
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			opt := rest[0]
			rest = rest[1:]
			if len(opt) == 2 && strings.ContainsRune("bcDEeFIiJLlmOopQRSWw", rune(opt[1])) && len(rest) > 0 {
				rest = rest[1:]
			}
		}
		if len(rest) < 2 {
			return RiskChanges
		}
		return classifyDepth(strings.Join(rest[1:], " "), depth+1)
	case "find":
		if hasAny(args, "-delete") {
			return RiskDestructive
		}
		for i, a := range args {
			if (a == "-exec" || a == "-execdir" || a == "-ok" || a == "-okdir") && i+1 < len(args) {
				end := len(args)
				for j := i + 1; j < len(args); j++ {
					if args[j] == ";" || args[j] == "+" || args[j] == "{}" && j == len(args)-1 {
						end = j
						break
					}
				}
				return Stricter(RiskReadOnly, classifyWords(args[i+1:end], depth))
			}
		}
		if hasAny(args, "-fprint", "-fprintf", "-fls") {
			return RiskChanges
		}
		return RiskReadOnly
	case "sed":
		if hasAny(args, "--in-place") || hasShort(args, 'i') {
			return RiskChanges
		}
		return RiskReadOnly
	case "awk", "gawk", "mawk":
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "system(") || strings.Contains(joined, "print >") || strings.Contains(joined, "inplace") {
			return RiskChanges
		}
		return RiskReadOnly
	case "curl":
		if hasAny(args, "-d", "--data", "--data-raw", "--data-binary", "--data-urlencode", "-F", "--form", "-T", "--upload-file", "--json", "-o", "-O", "--output", "--remote-name", "--remote-name-all") {
			return RiskChanges
		}
		for i, a := range args {
			m := ""
			switch {
			case (a == "-X" || a == "--request") && i+1 < len(args):
				m = args[i+1]
			case strings.HasPrefix(a, "-X") && len(a) > 2:
				m = a[2:]
			case strings.HasPrefix(a, "--request="):
				m = strings.TrimPrefix(a, "--request=")
			}
			if m != "" && !strings.EqualFold(m, "GET") && !strings.EqualFold(m, "HEAD") && !strings.EqualFold(m, "OPTIONS") {
				return RiskChanges
			}
		}
		return RiskReadOnly
	case "wget":
		if hasAny(args, "--spider", "-qO-", "-O-") || (hasAny(args, "-O", "--output-document") && hasAny(args, "-")) {
			return RiskReadOnly
		}
		return RiskChanges
	case "journalctl":
		if hasAny(args, "--vacuum-size", "--vacuum-time", "--vacuum-files", "--rotate", "--flush", "--sync", "--relinquish-var") {
			return RiskChanges
		}
		return RiskReadOnly
	case "dmesg":
		if hasAny(args, "--clear", "--read-clear") || hasShort(args, 'c') || hasShort(args, 'C') {
			return RiskChanges
		}
		return RiskReadOnly
	case "sysctl":
		for _, a := range args {
			if a == "-w" || a == "--write" || a == "-p" || a == "--load" || strings.Contains(a, "=") {
				return RiskChanges
			}
		}
		return RiskReadOnly
	case "crontab":
		if hasShort(args, 'r') {
			return RiskDestructive
		}
		if hasShort(args, 'l') {
			return RiskReadOnly
		}
		return RiskChanges
	case "git":
		return classifyGit(args)
	case "docker", "podman", "nerdctl":
		return classifyDocker(args)
	case "docker-compose":
		return classifyCompose(args)
	case "kubectl", "oc", "k":
		return classifyKubectl(args)
	case "helm":
		sub, _ := firstArg(args)
		switch sub {
		case "list", "ls", "status", "get", "history", "show", "search", "version", "template", "lint", "env", "dependency":
			return RiskReadOnly
		case "uninstall", "delete", "del", "un":
			return RiskDestructive
		}
		return RiskChanges
	case "systemctl":
		sub, _ := firstArg(args)
		switch sub {
		case "", "status", "show", "cat", "list-units", "list-unit-files", "list-timers", "list-sockets", "list-dependencies", "list-jobs",
			"is-active", "is-enabled", "is-failed", "is-system-running", "get-default", "help":
			return RiskReadOnly
		}
		return RiskChanges
	case "service":
		if len(args) >= 2 && args[1] == "status" || hasAny(args, "--status-all") {
			return RiskReadOnly
		}
		return RiskChanges
	case "psql", "mysql", "mariadb", "clickhouse-client", "cockroach", "mongosh", "mongo":
		return classifyDBClient(name, args)
	case "sqlite3":
		pos := []string{}
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				pos = append(pos, a)
			}
		}
		if len(pos) >= 2 {
			return classifySQL(strings.Join(pos[1:], " "))
		}
		return RiskChanges
	case "redis-cli", "valkey-cli":
		return classifyRedis(args)
	case "iptables", "ip6tables", "nft":
		if hasAny(args, "-F", "--flush", "-X", "--delete-chain", "flush") {
			return RiskDestructive
		}
		if hasAny(args, "-L", "--list", "-S", "--list-rules", "list") {
			return RiskReadOnly
		}
		return RiskChanges
	case "ufw":
		sub, _ := firstArg(args)
		switch sub {
		case "status", "show", "version":
			return RiskReadOnly
		case "reset":
			return RiskDestructive
		}
		return RiskChanges
	case "ip":
		for _, a := range args {
			switch a {
			case "add", "del", "delete", "set", "flush", "change", "replace", "append", "prepend", "attach", "detach":
				return RiskChanges
			}
		}
		return RiskReadOnly
	case "terraform", "tofu":
		sub, rest := firstArg(args)
		switch sub {
		case "plan", "show", "output", "validate", "version", "providers", "graph", "fmt":
			if sub == "fmt" && !hasAny(rest, "-check") {
				return RiskChanges
			}
			return RiskReadOnly
		case "state":
			s2, _ := firstArg(rest)
			if s2 == "list" || s2 == "show" || s2 == "pull" {
				return RiskReadOnly
			}
			if s2 == "rm" {
				return RiskDestructive
			}
		case "destroy":
			return RiskDestructive
		}
		return RiskChanges
	case "rsync":
		if hasAny(args, "--delete", "--delete-before", "--delete-after", "--delete-during", "--delete-excluded", "--remove-source-files") {
			return RiskDestructive
		}
		return RiskChanges
	case "tee":
		for _, a := range args {
			if !strings.HasPrefix(a, "-") && classifyRedirect(a) != RiskReadOnly {
				return classifyRedirect(a)
			}
		}
		return RiskReadOnly
	case "dpkg":
		if hasAny(args, "-l", "-L", "-s", "-S", "--list", "--listfiles", "--status", "--search", "--get-selections", "--print-architecture") {
			return RiskReadOnly
		}
		return RiskChanges
	case "rpm":
		if hasShort(args, 'q') || hasAny(args, "--query", "--verify") || hasShort(args, 'V') {
			if hasShort(args, 'e') {
				return RiskChanges
			}
			return RiskReadOnly
		}
		return RiskChanges
	case "apt", "apt-cache":
		sub, _ := firstArg(args)
		switch sub {
		case "list", "show", "search", "policy", "depends", "rdepends", "showpkg", "madison":
			return RiskReadOnly
		case "purge", "autoremove", "remove":
			return RiskChanges
		}
		return RiskChanges
	case "openssl":
		sub, rest := firstArg(args)
		switch sub {
		case "version", "s_client", "ciphers", "list", "verify", "speed", "dgst":
			return RiskReadOnly
		case "x509", "req", "rsa", "ec", "pkey", "crl", "pkcs12":
			if hasAny(rest, "-out", "-new", "-newkey", "-signkey") {
				return RiskChanges
			}
			return RiskReadOnly
		}
		return RiskChanges
	case "nginx", "apachectl", "apache2ctl", "httpd":
		if hasAny(args, "-t", "-T", "-v", "-V", "configtest", "-S") {
			return RiskReadOnly
		}
		return RiskChanges
	case "ssh-keygen":
		if hasShort(args, 'l') || hasShort(args, 'F') || hasShort(args, 'y') {
			return RiskReadOnly
		}
		return RiskChanges
	case "tmux":
		sub, _ := firstArg(args)
		switch sub {
		case "ls", "list-sessions", "list-windows", "list-panes", "info", "display-message", "show-options":
			return RiskReadOnly
		case "kill-server", "kill-session":
			return RiskDestructive
		}
		return RiskChanges
	case "screen":
		if hasAny(args, "-ls", "-list") {
			return RiskReadOnly
		}
		return RiskChanges
	case "top":
		return RiskReadOnly
	case "mount":
		if len(args) == 0 || hasAny(args, "-l") {
			return RiskReadOnly
		}
		return RiskChanges
	case "kill", "pkill", "killall", "umount", "swapoff", "chmod", "chown", "chgrp", "setfacl", "chattr":
		return RiskChanges
	}
	if readOnlyCmds[name] {
		return RiskReadOnly
	}
	return RiskChanges
}

func classifyGit(args []string) Risk {
	// Skip global options: git -C dir, git -c k=v, git --no-pager.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if (args[0] == "-C" || args[0] == "-c") && len(args) > 1 {
			args = args[2:]
			continue
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return RiskReadOnly
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "status", "log", "diff", "show", "blame", "describe", "rev-parse", "rev-list", "ls-files", "ls-tree", "ls-remote",
		"grep", "shortlog", "reflog", "cat-file", "whatchanged", "version", "help", "config":
		if sub == "config" && !hasAny(rest, "--get", "--get-all", "--list", "-l", "--get-regexp") {
			return RiskChanges
		}
		if sub == "reflog" && len(rest) > 0 && (rest[0] == "expire" || rest[0] == "delete") {
			return RiskDestructive
		}
		return RiskReadOnly
	case "branch":
		if hasAny(rest, "-D", "--delete", "-d") && hasAny(rest, "--force", "-f", "-D") {
			return RiskDestructive
		}
		if hasAny(rest, "-d", "--delete", "-m", "-M", "--move", "-c", "-C", "--set-upstream-to", "-u") {
			return RiskChanges
		}
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				return RiskChanges // creates a branch
			}
		}
		return RiskReadOnly
	case "tag":
		if len(rest) == 0 || hasAny(rest, "-l", "--list") {
			return RiskReadOnly
		}
		return RiskChanges
	case "remote":
		if len(rest) == 0 || rest[0] == "-v" || rest[0] == "show" || rest[0] == "get-url" {
			return RiskReadOnly
		}
		return RiskChanges
	case "stash":
		if len(rest) > 0 && (rest[0] == "list" || rest[0] == "show") {
			return RiskReadOnly
		}
		if len(rest) > 0 && (rest[0] == "drop" || rest[0] == "clear") {
			return RiskDestructive
		}
		return RiskChanges
	case "reset":
		if hasAny(rest, "--hard", "--merge", "--keep") {
			return RiskDestructive
		}
		return RiskChanges
	case "clean":
		if hasAny(rest, "--force") || hasShort(rest, 'f') {
			return RiskDestructive
		}
		return RiskChanges
	case "push":
		if hasAny(rest, "--force", "--force-with-lease", "--delete", "--mirror", "--prune") || hasShort(rest, 'f') || hasShort(rest, 'd') {
			return RiskDestructive
		}
		for _, a := range rest {
			if strings.HasPrefix(a, "+") || strings.HasPrefix(a, ":") {
				return RiskDestructive
			}
		}
		return RiskChanges
	case "checkout":
		// checkout -- file, checkout . and checkout -f throw away local edits.
		if hasAny(rest, "--", ".", "--force", "-f") {
			return RiskDestructive
		}
		return RiskChanges
	case "restore":
		if hasAny(rest, "--staged", "-S") && !hasAny(rest, "--worktree", "-W") {
			return RiskChanges
		}
		return RiskDestructive
	case "filter-branch", "filter-repo", "gc", "prune", "update-ref":
		if sub == "update-ref" && !hasAny(rest, "-d") {
			return RiskChanges
		}
		return RiskDestructive
	}
	return RiskChanges
}

func classifyDocker(args []string) Risk {
	// Skip global flags with values: --context x, -H x, --host x.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if (args[0] == "-H" || args[0] == "--host" || args[0] == "--context" || args[0] == "-c" || args[0] == "--config") && len(args) > 1 {
			args = args[2:]
			continue
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return RiskReadOnly
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ps", "logs", "inspect", "images", "stats", "version", "info", "top", "port", "diff", "history", "events", "search":
		return RiskReadOnly
	case "rm", "rmi", "kill":
		return RiskDestructive
	case "compose":
		return classifyCompose(rest)
	case "system", "volume", "image", "container", "network", "builder", "buildx", "context":
		obj, _ := firstArg(rest)
		switch obj {
		case "ls", "list", "inspect", "df", "info", "show", "history", "events":
			return RiskReadOnly
		case "rm", "remove", "prune", "kill":
			return RiskDestructive
		}
		return RiskChanges
	}
	return RiskChanges
}

func classifyCompose(args []string) Risk {
	sub, rest := firstArg(args)
	switch sub {
	case "ps", "logs", "config", "top", "images", "ls", "version", "port", "events":
		return RiskReadOnly
	case "rm", "kill":
		return RiskDestructive
	case "down":
		if hasAny(rest, "-v", "--volumes", "--rmi") {
			return RiskDestructive
		}
		return RiskChanges
	}
	return RiskChanges
}

func classifyKubectl(args []string) Risk {
	sub, rest := firstArg(args)
	switch sub {
	case "get", "describe", "logs", "top", "version", "explain", "api-resources", "api-versions", "cluster-info", "diff", "events", "wait":
		return RiskReadOnly
	case "auth":
		s2, _ := firstArg(rest)
		if s2 == "can-i" || s2 == "whoami" {
			return RiskReadOnly
		}
	case "config":
		s2, _ := firstArg(rest)
		switch s2 {
		case "view", "get-contexts", "current-context", "get-clusters", "get-users":
			return RiskReadOnly
		}
	case "rollout":
		s2, _ := firstArg(rest)
		if s2 == "status" || s2 == "history" {
			return RiskReadOnly
		}
	case "delete", "drain":
		return RiskDestructive
	}
	return RiskChanges
}

func classifyDBClient(name string, args []string) Risk {
	var sql []string
	for i, a := range args {
		switch {
		case (a == "-c" || a == "--command" || a == "-e" || a == "--execute" || a == "--eval" || a == "--query" || a == "-q") && i+1 < len(args):
			sql = append(sql, args[i+1])
		case strings.HasPrefix(a, "--command=") || strings.HasPrefix(a, "--execute=") || strings.HasPrefix(a, "--query="):
			sql = append(sql, a[strings.IndexByte(a, '=')+1:])
		case name == "psql" && strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.HasSuffix(a, "c") && i+1 < len(args):
			// psql -xc "select …"
			sql = append(sql, args[i+1])
		case (name == "mysql" || name == "mariadb") && strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.HasSuffix(a, "e") && i+1 < len(args):
			sql = append(sql, args[i+1])
		}
	}
	if len(sql) == 0 {
		// An interactive client can do anything once it is open.
		return RiskChanges
	}
	return classifySQL(strings.Join(sql, ";"))
}

func classifySQL(sql string) Risk {
	switch {
	case sqlDestroy.MatchString(sql):
		return RiskDestructive
	case sqlChange.MatchString(sql):
		return RiskChanges
	}
	return RiskReadOnly
}

func classifyRedis(args []string) Risk {
	// Skip connection flags: -h host, -p port, -a pass, -n db, -u url.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if len(args) > 1 && strings.Contains("-h -p -a -n -u -s --user --pass", args[0]) {
			args = args[2:]
			continue
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return RiskChanges
	}
	switch strings.ToLower(args[0]) {
	case "flushall", "flushdb", "del", "unlink", "shutdown":
		return RiskDestructive
	case "get", "mget", "info", "keys", "scan", "ttl", "pttl", "type", "exists", "hget", "hgetall", "hkeys", "hlen", "lrange", "llen",
		"smembers", "scard", "zrange", "zcard", "ping", "dbsize", "monitor", "slowlog", "memory", "latency", "strlen", "lastsave", "time", "role":
		return RiskReadOnly
	case "client", "config":
		if len(args) > 1 && (strings.EqualFold(args[1], "list") || strings.EqualFold(args[1], "get") || strings.EqualFold(args[1], "info")) {
			return RiskReadOnly
		}
	}
	return RiskChanges
}
